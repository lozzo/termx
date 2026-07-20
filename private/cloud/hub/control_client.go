package hub

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const (
	hubControlChallengePath = "/v1/hub/control/challenge"
	hubControlOpenPath      = "/v1/hub/control/open"
	hubControlReportPath    = "/v1/hub/control/report"
	hubControlProtoMedia    = "application/x-protobuf"
	hubControlMaxFrame      = 4 << 20
	hubChallengeDomain      = "termx-hub-control-challenge-v1\x00"
)

// ControlClientConfig 固定 Edge Hub control identity、Controller internal origin 与纯内存 projection owner。
type ControlClientConfig struct {
	ControllerURL   string
	Metadata        *cloudpb.EdgeDeploymentMetadata
	PrivateKey      ed25519.PrivateKey
	SoftwareVersion string
	HTTPClient      *http.Client
	Projection      *Projection
	Topology        TopologySource
	Clock           Clock
	MinBackoff      time.Duration
	MaxBackoff      time.Duration
}

// TopologySource 是 ControlClient 读取 Hub 纯内存完整拓扑的窄端口。
// 通知允许合并；每次上行都必须重新读取完整 snapshot，不能把通知当作增量事件。
type TopologySource interface {
	TopologyChanges() <-chan struct{}
	TopologySnapshot(controlGeneration uint64, observedAt time.Time) *cloudpb.HubTopologySnapshot
}

// ControlClientState 是 Edge health endpoint 可读取的 attachment 状态。
type ControlClientState struct {
	Attached          bool
	ControlGeneration uint64
	LastSequence      uint64
	LastError         string
}

// ControlClient 维护一个 Hub identity 的唯一 control generation。
// 断线不会从磁盘恢复 projection；重连后 Controller 必须重新发送 full snapshot。
type ControlClient struct {
	controllerURL   string
	metadata        *cloudpb.EdgeDeploymentMetadata
	privateKey      ed25519.PrivateKey
	softwareVersion string
	httpClient      *http.Client
	projection      *Projection
	topology        TopologySource
	clock           Clock
	minBackoff      time.Duration
	maxBackoff      time.Duration

	mu    sync.RWMutex
	state ControlClientState
}

// NewControlClient 创建 Edge Hub control client。
func NewControlClient(config ControlClientConfig) (*ControlClient, error) {
	parsed, err := url.Parse(strings.TrimRight(config.ControllerURL, "/"))
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" || config.Metadata == nil || config.Metadata.GetHubId() == "" || config.Metadata.GetEdgeDeploymentId() == "" || len(config.PrivateKey) != ed25519.PrivateKeySize || config.Projection == nil || config.Topology == nil {
		return nil, fmt.Errorf("invalid Hub control client configuration")
	}
	publicKey := config.PrivateKey.Public().(ed25519.PublicKey)
	if config.Metadata.GetHubControlIdentityFingerprint() != hubIdentityFingerprint(publicKey) {
		return nil, fmt.Errorf("Hub control private key does not match metadata fingerprint")
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{}
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	if config.MinBackoff <= 0 {
		config.MinBackoff = 100 * time.Millisecond
	}
	if config.MaxBackoff < config.MinBackoff {
		config.MaxBackoff = 2 * time.Second
	}
	return &ControlClient{controllerURL: parsed.String(), metadata: proto.Clone(config.Metadata).(*cloudpb.EdgeDeploymentMetadata), privateKey: append(ed25519.PrivateKey(nil), config.PrivateKey...), softwareVersion: config.SoftwareVersion, httpClient: config.HTTPClient, projection: config.Projection, topology: config.Topology, clock: config.Clock, minBackoff: config.MinBackoff, maxBackoff: config.MaxBackoff}, nil
}

// Run 持续重连直到 context 结束；每次连接都重新完成 challenge 并接受 Controller 新 generation。
func (client *ControlClient) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("Hub control context is required")
	}
	backoff := client.minBackoff
	for {
		err := client.runConnection(ctx)
		if ctx.Err() != nil {
			client.setDetached("")
			return ctx.Err()
		}
		client.setDetached(err.Error())
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		backoff *= 2
		if backoff > client.maxBackoff {
			backoff = client.maxBackoff
		}
	}
}

// State 返回 attachment 状态副本。
func (client *ControlClient) State() ControlClientState {
	client.mu.RLock()
	defer client.mu.RUnlock()
	return client.state
}

func (client *ControlClient) runConnection(ctx context.Context) error {
	connectionContext, cancelConnection := context.WithCancel(ctx)
	defer cancelConnection()
	challenge, err := client.challenge(connectionContext)
	if err != nil {
		return err
	}
	proof, err := hubChallengeProofBytes(challenge, client.metadata)
	if err != nil {
		return err
	}
	hello := &cloudpb.HubHello{Deployment: proto.Clone(client.metadata).(*cloudpb.EdgeDeploymentMetadata), ChallengeId: challenge.GetChallengeId(), ChallengeSignature: ed25519.Sign(client.privateKey, proof), SoftwareVersion: client.softwareVersion, LastProjectionRevision: client.projection.Snapshot().Revision, LastProjectionDigest: client.projection.Snapshot().Digest}
	payload, _ := proto.Marshal(hello)
	request, _ := http.NewRequestWithContext(connectionContext, http.MethodPost, client.controllerURL+hubControlOpenPath, bytes.NewReader(payload))
	request.Header.Set("Content-Type", hubControlProtoMedia)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/x-termx-cloud-stream" {
		return fmt.Errorf("Hub control open failed with status %d", response.StatusCode)
	}
	var generation, controllerSequence uint64
	var sender *hubRuntimeSender
	reporterError := make(chan error, 1)
	for {
		envelope := &cloudpb.HubControlEnvelope{}
		if err := readControlFrame(response.Body, envelope); err != nil {
			select {
			case reportErr := <-reporterError:
				return reportErr
			default:
			}
			return err
		}
		now := client.clock.Now().UTC()
		if envelope.GetHubId() != client.metadata.GetHubId() || envelope.GetEdgeDeploymentId() != client.metadata.GetEdgeDeploymentId() || envelope.GetSenderRole() != cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_CONTROLLER || envelope.GetIssuedAtUnixMillis() > now.UnixMilli() || envelope.GetExpiresAtUnixMillis() <= now.UnixMilli() {
			return ErrProjectionConflict
		}
		if generation == 0 {
			ready := envelope.GetReady()
			if ready == nil || ready.GetControlGeneration() == 0 || envelope.GetControlGeneration() != ready.GetControlGeneration() || envelope.GetSenderSequence() != 1 {
				return ErrProjectionConflict
			}
			generation, controllerSequence = ready.GetControlGeneration(), 1
			sender = &hubRuntimeSender{client: client, generation: generation}
			go func() {
				if reportErr := client.runTopologyReporter(connectionContext, sender); reportErr != nil && connectionContext.Err() == nil {
					select {
					case reporterError <- reportErr:
					default:
					}
					cancelConnection()
				}
			}()
			client.setAttached(generation, controllerSequence)
			continue
		}
		if envelope.GetControlGeneration() != generation || envelope.GetSenderSequence() != controllerSequence+1 {
			return ErrProjectionConflict
		}
		controllerSequence = envelope.GetSenderSequence()
		switch {
		case envelope.GetFullProjection() != nil:
			err = client.projection.ApplyFull(envelope.GetFullProjection())
		case envelope.GetPolicyDelta() != nil:
			err = client.projection.ApplyDelta(envelope.GetPolicyDelta())
		default:
			err = ErrProjectionConflict
		}
		if err != nil {
			return err
		}
		client.setAttached(generation, controllerSequence)
		if err := client.reportReconciliation(connectionContext, sender); err != nil {
			return err
		}
	}
}

func (client *ControlClient) challenge(ctx context.Context) (*cloudpb.HubControlChallenge, error) {
	request := &cloudpb.HubControlChallengeRequest{HubId: client.metadata.GetHubId(), EdgeDeploymentId: client.metadata.GetEdgeDeploymentId(), HubControlIdentityFingerprint: client.metadata.GetHubControlIdentityFingerprint()}
	response := &cloudpb.HubControlChallengeResponse{}
	if err := client.unary(ctx, hubControlChallengePath, request, response); err != nil {
		return nil, err
	}
	if response.GetChallenge() == nil || response.GetChallenge().GetExpiresAtUnixMillis() <= client.clock.Now().UTC().UnixMilli() {
		return nil, errors.New("Hub control challenge is invalid")
	}
	return response.GetChallenge(), nil
}

func (client *ControlClient) reportReconciliation(ctx context.Context, sender *hubRuntimeSender) error {
	snapshot := client.projection.Snapshot()
	now := client.clock.Now().UTC()
	return sender.report(ctx, now, &cloudpb.HubRuntimeEnvelope{Payload: &cloudpb.HubRuntimeEnvelope_Reconciliation{Reconciliation: &cloudpb.ReconciliationDigest{HubId: client.metadata.GetHubId(), ProjectionRevision: snapshot.Revision, ProjectionDigest: snapshot.Digest, ObservedAtUnixMillis: now.UnixMilli()}}})
}

func (client *ControlClient) runTopologyReporter(ctx context.Context, sender *hubRuntimeSender) error {
	for {
		now := client.clock.Now().UTC()
		snapshot := client.topology.TopologySnapshot(sender.generation, now)
		if snapshot == nil || snapshot.GetHubId() != client.metadata.GetHubId() || snapshot.GetControlGeneration() != sender.generation || len(snapshot.GetTopologyDigest()) == 0 {
			return ErrProjectionConflict
		}
		if err := sender.report(ctx, now, &cloudpb.HubRuntimeEnvelope{Payload: &cloudpb.HubRuntimeEnvelope_Topology{Topology: snapshot}}); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-client.topology.TopologyChanges():
		}
	}
}

type hubRuntimeSender struct {
	client     *ControlClient
	generation uint64
	mu         sync.Mutex
	sequence   uint64
}

func (sender *hubRuntimeSender) report(ctx context.Context, now time.Time, envelope *cloudpb.HubRuntimeEnvelope) error {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if envelope == nil || envelope.GetPayload() == nil {
		return ErrProjectionConflict
	}
	sequence := sender.sequence + 1
	envelope.HubId = sender.client.metadata.GetHubId()
	envelope.EdgeDeploymentId = sender.client.metadata.GetEdgeDeploymentId()
	envelope.ControlGeneration = sender.generation
	envelope.SenderRole = cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_HUB
	envelope.SenderSequence = sequence
	envelope.IssuedAtUnixMillis = now.UnixMilli()
	envelope.ExpiresAtUnixMillis = now.Add(time.Minute).UnixMilli()
	response := &cloudpb.ReportHubRuntimeResponse{}
	if err := sender.client.unary(ctx, hubControlReportPath, &cloudpb.ReportHubRuntimeRequest{Envelopes: []*cloudpb.HubRuntimeEnvelope{envelope}}, response); err != nil {
		return err
	}
	if response.GetAcceptedSenderSequence() != sequence || response.GetFullSnapshotRequired() {
		return ErrProjectionConflict
	}
	sender.sequence = sequence
	return nil
}

func (client *ControlClient) unary(ctx context.Context, path string, input, output proto.Message) error {
	payload, err := proto.Marshal(input)
	if err != nil {
		return err
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, client.controllerURL+path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", hubControlProtoMedia)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != hubControlProtoMedia {
		return fmt.Errorf("Hub control unary %s failed with status %d", path, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, hubControlMaxFrame+1))
	if err != nil || len(body) == 0 || len(body) > hubControlMaxFrame {
		return errors.New("Hub control unary response is invalid")
	}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, output); err != nil || len(output.ProtoReflect().GetUnknown()) != 0 {
		return errors.New("Hub control unary response is invalid")
	}
	return nil
}

func (client *ControlClient) setAttached(generation, sequence uint64) {
	client.mu.Lock()
	client.state = ControlClientState{Attached: true, ControlGeneration: generation, LastSequence: sequence}
	client.mu.Unlock()
}

func (client *ControlClient) setDetached(message string) {
	client.mu.Lock()
	client.state.Attached = false
	client.state.LastError = message
	client.mu.Unlock()
}

func readControlFrame(reader io.Reader, target proto.Message) error {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > hubControlMaxFrame {
		return errors.New("Hub control frame size is invalid")
	}
	payload := make([]byte, int(size))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return err
	}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, target); err != nil || len(target.ProtoReflect().GetUnknown()) != 0 {
		return errors.New("Hub control frame is invalid")
	}
	return nil
}

func hubChallengeProofBytes(challenge *cloudpb.HubControlChallenge, metadata *cloudpb.EdgeDeploymentMetadata) ([]byte, error) {
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&cloudpb.HubControlChallengeProofInput{ChallengeId: challenge.GetChallengeId(), Challenge: append([]byte(nil), challenge.GetChallenge()...), HubId: metadata.GetHubId(), EdgeDeploymentId: metadata.GetEdgeDeploymentId(), HubControlIdentityFingerprint: metadata.GetHubControlIdentityFingerprint()})
	if err != nil {
		return nil, err
	}
	return append([]byte(hubChallengeDomain), payload...), nil
}

func hubIdentityFingerprint(publicKey ed25519.PublicKey) string {
	digest := sha256.Sum256(publicKey)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
