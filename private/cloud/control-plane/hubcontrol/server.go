package hubcontrol

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	cloudtopology "github.com/lozzow/termx/private/cloud/control-plane/topology"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const (
	// ChallengePath 是 Hub control fresh challenge 的内部 listener 路径。
	ChallengePath = "/v1/hub/control/challenge"
	// OpenPath 是 Controller 到 Hub 的 length-prefixed Proto stream。
	OpenPath = "/v1/hub/control/open"
	// ReportPath 是 Hub 到 Controller 的有界 Proto batch。
	ReportPath        = "/v1/hub/control/report"
	protobufMediaType = "application/x-protobuf"
	streamMediaType   = "application/x-termx-cloud-stream"
	maxProtoBytes     = 4 << 20
	challengeDomain   = "termx-hub-control-challenge-v1\x00"
)

// CursorStore 持久化 Hub 上行 sender sequence；不同 sender role 使用不同 key。
type CursorStore interface {
	ControlCursor(context.Context, string, uint64, cloudpb.ControlSenderRole) (uint64, []byte, error)
	PutControlCursor(context.Context, string, uint64, cloudpb.ControlSenderRole, uint64, []byte, time.Time) error
}

// ServerConfig 装配 internal Hub control listener 的领域依赖。
type ServerConfig struct {
	Registry     *hubregistry.Registry
	CursorStore  CursorStore
	Publisher    *Publisher
	Topology     *cloudtopology.Service
	Results      RuntimeResultSink
	Clock        func() time.Time
	Random       io.Reader
	ChallengeTTL time.Duration
	EnvelopeTTL  time.Duration
}

// RuntimeResultSink 在 receive cursor ack 前持久化 Hub/daemon 独立 command receipt。
// exact replay 必须幂等，冲突 digest 必须 fail closed。
type RuntimeResultSink interface {
	IngestHubResult(context.Context, *cloudpb.HubCommandResult, time.Time) error
	IngestDaemonResult(context.Context, *cloudpb.DaemonCommandResult, time.Time) error
}

// Server 是 Hub control HTTP handler；public Web/Hub signaling listener 不得挂载这些路径。
type Server struct {
	registry     *hubregistry.Registry
	cursors      CursorStore
	publisher    *Publisher
	topology     *cloudtopology.Service
	results      RuntimeResultSink
	now          func() time.Time
	random       io.Reader
	challengeTTL time.Duration
	envelopeTTL  time.Duration

	mu          sync.Mutex
	challenges  map[string]pendingChallenge
	attachments map[string]attachment
	reportMu    sync.Mutex
}

type pendingChallenge struct {
	value       *cloudpb.HubControlChallenge
	hubID       string
	deployment  string
	fingerprint string
}

type attachment struct {
	generation uint64
	cancel     context.CancelFunc
	attachedAt time.Time
}

// AttachmentStatus 返回当前进程是否仍持有精确 Hub generation 的 active stream。
func (server *Server) AttachmentStatus(hubID string) (uint64, time.Time, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()
	value, ok := server.attachments[hubID]
	return value.generation, value.attachedAt, ok
}

// NewServer 创建 Hub control handler。
func NewServer(config ServerConfig) (*Server, error) {
	if config.Registry == nil || config.CursorStore == nil || config.Publisher == nil || config.Topology == nil {
		return nil, fmt.Errorf("Hub control registry, cursor store and publisher are required")
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.ChallengeTTL <= 0 || config.ChallengeTTL > time.Minute || config.EnvelopeTTL <= 0 || config.EnvelopeTTL > time.Hour {
		return nil, fmt.Errorf("invalid Hub control TTL")
	}
	return &Server{registry: config.Registry, cursors: config.CursorStore, publisher: config.Publisher, topology: config.Topology, results: config.Results, now: config.Clock, random: config.Random, challengeTTL: config.ChallengeTTL, envelopeTTL: config.EnvelopeTTL, challenges: make(map[string]pendingChallenge), attachments: make(map[string]attachment)}, nil
}

// Handler 返回只包含 internal Hub control API 的 mux。
func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(ChallengePath, server.handleChallenge)
	mux.HandleFunc(OpenPath, server.handleOpen)
	mux.HandleFunc(ReportPath, server.handleReport)
	return mux
}

func (server *Server) handleChallenge(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	input := &cloudpb.HubControlChallengeRequest{}
	if err := readProto(request, input); err != nil {
		http.Error(writer, "invalid challenge request", http.StatusBadRequest)
		return
	}
	deployment, err := server.registry.Deployment(request.Context(), input.GetHubId())
	if err != nil || !deployment.Enabled || deployment.Metadata.GetEdgeDeploymentId() != input.GetEdgeDeploymentId() || deployment.Metadata.GetHubControlIdentityFingerprint() != input.GetHubControlIdentityFingerprint() {
		http.Error(writer, "Hub identity rejected", http.StatusUnauthorized)
		return
	}
	challengeBytes := make([]byte, 32)
	if _, err := io.ReadFull(server.random, challengeBytes); err != nil {
		http.Error(writer, "challenge unavailable", http.StatusServiceUnavailable)
		return
	}
	idDigest := sha256.Sum256(append(append([]byte(nil), challengeBytes...), []byte(input.GetHubId())...))
	now := server.now().UTC()
	challenge := &cloudpb.HubControlChallenge{ChallengeId: base64.RawURLEncoding.EncodeToString(idDigest[:18]), Challenge: challengeBytes, ExpiresAtUnixMillis: now.Add(server.challengeTTL).UnixMilli()}
	server.mu.Lock()
	server.cleanupChallengesLocked(now)
	server.challenges[challenge.GetChallengeId()] = pendingChallenge{value: proto.Clone(challenge).(*cloudpb.HubControlChallenge), hubID: input.GetHubId(), deployment: input.GetEdgeDeploymentId(), fingerprint: input.GetHubControlIdentityFingerprint()}
	server.mu.Unlock()
	writeProto(writer, http.StatusOK, &cloudpb.HubControlChallengeResponse{Challenge: challenge})
}

func (server *Server) handleOpen(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hello := &cloudpb.HubHello{}
	if err := readProto(request, hello); err != nil || hello.GetDeployment() == nil {
		http.Error(writer, "invalid Hub hello", http.StatusBadRequest)
		return
	}
	now := server.now().UTC()
	challenge, deployment, err := server.consumeChallenge(request.Context(), hello, now)
	if err != nil {
		http.Error(writer, "Hub challenge rejected", http.StatusUnauthorized)
		return
	}
	proof, err := ChallengeProofBytes(challenge, hello.GetDeployment())
	if err != nil || !ed25519.Verify(deployment.ControlPublicKey, proof, hello.GetChallengeSignature()) {
		http.Error(writer, "Hub proof rejected", http.StatusUnauthorized)
		return
	}
	full, ok := server.publisher.CurrentFull(hello.GetDeployment().GetHubId())
	if !ok {
		http.Error(writer, "Hub projection unavailable", http.StatusServiceUnavailable)
		return
	}
	attached, err := server.registry.AttachHub(request.Context(), hello, now)
	if err != nil {
		http.Error(writer, "Hub attach rejected", http.StatusConflict)
		return
	}
	streamContext, cancel := context.WithCancel(request.Context())
	defer cancel()
	server.mu.Lock()
	if previous := server.attachments[attached.Metadata.GetHubId()]; previous.cancel != nil {
		previous.cancel()
	}
	server.attachments[attached.Metadata.GetHubId()] = attachment{generation: attached.ControlGeneration, cancel: cancel, attachedAt: now}
	server.mu.Unlock()
	defer server.detach(attached.Metadata.GetHubId(), attached.ControlGeneration)

	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unavailable", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", streamMediaType)
	writer.WriteHeader(http.StatusOK)
	sequence := uint64(1)
	ready := &cloudpb.HubControlEnvelope{HubId: attached.Metadata.GetHubId(), EdgeDeploymentId: attached.Metadata.GetEdgeDeploymentId(), ControlGeneration: attached.ControlGeneration, SenderRole: cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_CONTROLLER, SenderSequence: sequence, IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(server.envelopeTTL).UnixMilli(), Payload: &cloudpb.HubControlEnvelope_Ready{Ready: &cloudpb.ControlStreamReady{HubId: attached.Metadata.GetHubId(), ControlGeneration: attached.ControlGeneration, FullSnapshotRequired: true, IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(server.envelopeTTL).UnixMilli()}}}
	if writeFrame(writer, ready) != nil {
		return
	}
	sequence++
	if writeFrame(writer, wrapControlMessage(attached, sequence, now, server.envelopeTTL, full)) != nil {
		return
	}
	flusher.Flush()
	updates, unsubscribe := server.publisher.Subscribe(attached.Metadata.GetHubId())
	defer unsubscribe()
	for {
		select {
		case <-streamContext.Done():
			return
		case update := <-updates:
			if err := server.registry.RequireCurrentGeneration(streamContext, attached.Metadata.GetHubId(), attached.ControlGeneration); err != nil {
				return
			}
			sequence++
			envelope := wrapControlMessage(attached, sequence, server.now().UTC(), server.envelopeTTL, update)
			if envelope == nil || writeFrame(writer, envelope) != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (server *Server) handleReport(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	input := &cloudpb.ReportHubRuntimeRequest{}
	if err := readProto(request, input); err != nil || len(input.GetEnvelopes()) == 0 {
		http.Error(writer, "invalid Hub runtime batch", http.StatusBadRequest)
		return
	}
	server.reportMu.Lock()
	defer server.reportMu.Unlock()
	first := input.GetEnvelopes()[0]
	if first.GetHubId() == "" || first.GetControlGeneration() == 0 || first.GetSenderRole() != cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_HUB {
		http.Error(writer, "invalid Hub runtime envelope", http.StatusBadRequest)
		return
	}
	if err := server.registry.RequireCurrentGeneration(request.Context(), first.GetHubId(), first.GetControlGeneration()); err != nil {
		http.Error(writer, "stale Hub generation", http.StatusConflict)
		return
	}
	accepted, acceptedDigest, err := server.cursors.ControlCursor(request.Context(), first.GetHubId(), first.GetControlGeneration(), first.GetSenderRole())
	if err != nil {
		http.Error(writer, "cursor unavailable", http.StatusServiceUnavailable)
		return
	}
	fullRequired := false
	now := server.now().UTC()
	for _, envelope := range input.GetEnvelopes() {
		if envelope.GetHubId() != first.GetHubId() || envelope.GetEdgeDeploymentId() != first.GetEdgeDeploymentId() || envelope.GetControlGeneration() != first.GetControlGeneration() || envelope.GetSenderRole() != first.GetSenderRole() || envelope.GetExpiresAtUnixMillis() <= now.UnixMilli() || envelope.GetIssuedAtUnixMillis() > now.UnixMilli() || envelope.GetReconciliation() == nil && envelope.GetTopology() == nil && envelope.GetHubCommandResult() == nil && envelope.GetDaemonCommandResult() == nil {
			http.Error(writer, "Hub runtime envelope rejected", http.StatusBadRequest)
			return
		}
		digest, _ := deterministicDigest(envelope)
		if envelope.GetSenderSequence() == accepted {
			if !bytesEqual(digest, acceptedDigest) {
				http.Error(writer, "Hub runtime replay conflict", http.StatusConflict)
				return
			}
			continue
		}
		if envelope.GetSenderSequence() != accepted+1 {
			http.Error(writer, "Hub runtime sequence gap", http.StatusConflict)
			return
		}
		accepted, acceptedDigest = envelope.GetSenderSequence(), digest
		if reconciliation := envelope.GetReconciliation(); reconciliation != nil {
			head, ok := server.publisher.Head(first.GetHubId())
			if !ok || reconciliation.GetProjectionRevision() != head.Revision || !bytesEqual(reconciliation.GetProjectionDigest(), head.Digest) {
				fullRequired = true
			}
		} else if topology := envelope.GetTopology(); topology != nil {
			if topology.GetControlGeneration() != first.GetControlGeneration() || topology.GetHubId() != first.GetHubId() || server.topology.Ingest(request.Context(), topology, now) != nil {
				http.Error(writer, "Hub topology snapshot rejected", http.StatusConflict)
				return
			}
		} else if result := envelope.GetHubCommandResult(); result != nil {
			if server.results == nil || result.GetHubId() != first.GetHubId() || result.GetControlGeneration() != first.GetControlGeneration() || server.results.IngestHubResult(request.Context(), result, now) != nil {
				http.Error(writer, "Hub command result rejected", http.StatusConflict)
				return
			}
		} else if result := envelope.GetDaemonCommandResult(); result != nil {
			if server.results == nil || server.results.IngestDaemonResult(request.Context(), result, now) != nil {
				http.Error(writer, "daemon command result rejected", http.StatusConflict)
				return
			}
		}
	}
	if err := server.cursors.PutControlCursor(request.Context(), first.GetHubId(), first.GetControlGeneration(), first.GetSenderRole(), accepted, acceptedDigest, now); err != nil {
		http.Error(writer, "persist Hub runtime cursor", http.StatusServiceUnavailable)
		return
	}
	writeProto(writer, http.StatusOK, &cloudpb.ReportHubRuntimeResponse{AcceptedSenderSequence: accepted, FullSnapshotRequired: fullRequired})
}

// ChallengeProofBytes 返回 Edge 与 Controller 共用的 deterministic Proto 签名输入。
func ChallengeProofBytes(challenge *cloudpb.HubControlChallenge, metadata *cloudpb.EdgeDeploymentMetadata) ([]byte, error) {
	if challenge == nil || metadata == nil {
		return nil, errors.New("Hub challenge proof input is incomplete")
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&cloudpb.HubControlChallengeProofInput{ChallengeId: challenge.GetChallengeId(), Challenge: append([]byte(nil), challenge.GetChallenge()...), HubId: metadata.GetHubId(), EdgeDeploymentId: metadata.GetEdgeDeploymentId(), HubControlIdentityFingerprint: metadata.GetHubControlIdentityFingerprint()})
	if err != nil {
		return nil, err
	}
	return append([]byte(challengeDomain), payload...), nil
}

func (server *Server) consumeChallenge(ctx context.Context, hello *cloudpb.HubHello, now time.Time) (*cloudpb.HubControlChallenge, hubregistry.Deployment, error) {
	metadata := hello.GetDeployment()
	server.mu.Lock()
	server.cleanupChallengesLocked(now)
	pending, ok := server.challenges[hello.GetChallengeId()]
	delete(server.challenges, hello.GetChallengeId())
	server.mu.Unlock()
	if !ok || pending.hubID != metadata.GetHubId() || pending.deployment != metadata.GetEdgeDeploymentId() || pending.fingerprint != metadata.GetHubControlIdentityFingerprint() || pending.value.GetExpiresAtUnixMillis() <= now.UnixMilli() {
		return nil, hubregistry.Deployment{}, hubregistry.ErrDeploymentIdentity
	}
	deployment, err := server.registry.Deployment(ctx, metadata.GetHubId())
	if err != nil {
		return nil, hubregistry.Deployment{}, err
	}
	return pending.value, deployment, nil
}

func (server *Server) cleanupChallengesLocked(now time.Time) {
	for id, challenge := range server.challenges {
		if challenge.value.GetExpiresAtUnixMillis() <= now.UnixMilli() {
			delete(server.challenges, id)
		}
	}
}

func (server *Server) detach(hubID string, generation uint64) {
	server.mu.Lock()
	detached := false
	if current := server.attachments[hubID]; current.generation == generation {
		delete(server.attachments, hubID)
		detached = true
	}
	server.mu.Unlock()
	if detached {
		_ = server.topology.MarkHubUnknown(context.Background(), hubID, generation, server.now().UTC())
	}
}

func wrapControlMessage(deployment hubregistry.Deployment, sequence uint64, now time.Time, ttl time.Duration, message proto.Message) *cloudpb.HubControlEnvelope {
	envelope := &cloudpb.HubControlEnvelope{HubId: deployment.Metadata.GetHubId(), EdgeDeploymentId: deployment.Metadata.GetEdgeDeploymentId(), ControlGeneration: deployment.ControlGeneration, SenderRole: cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_CONTROLLER, SenderSequence: sequence, IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(ttl).UnixMilli()}
	switch value := message.(type) {
	case *cloudpb.FullProjectionSnapshot:
		envelope.Payload = &cloudpb.HubControlEnvelope_FullProjection{FullProjection: proto.Clone(value).(*cloudpb.FullProjectionSnapshot)}
	case *cloudpb.PolicyDelta:
		envelope.Payload = &cloudpb.HubControlEnvelope_PolicyDelta{PolicyDelta: proto.Clone(value).(*cloudpb.PolicyDelta)}
	case *cloudpb.HubCommand:
		envelope.Payload = &cloudpb.HubControlEnvelope_Command{Command: proto.Clone(value).(*cloudpb.HubCommand)}
	default:
		return nil
	}
	return envelope
}

func readProto(request *http.Request, target proto.Message) error {
	if request.Header.Get("Content-Type") != protobufMediaType {
		return errors.New("invalid protobuf content type")
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxProtoBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxProtoBytes {
		return errors.New("invalid protobuf body")
	}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, target); err != nil || len(target.ProtoReflect().GetUnknown()) != 0 {
		return errors.New("invalid protobuf body")
	}
	return nil
}

func writeProto(writer http.ResponseWriter, status int, message proto.Message) {
	payload, err := proto.Marshal(message)
	if err != nil {
		http.Error(writer, "encode protobuf response", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", protobufMediaType)
	writer.WriteHeader(status)
	_, _ = writer.Write(payload)
}

func writeFrame(writer io.Writer, message proto.Message) error {
	payload, err := proto.Marshal(message)
	if err != nil || len(payload) == 0 || len(payload) > maxProtoBytes {
		return errors.New("invalid Hub control frame")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := writer.Write(header[:]); err != nil {
		return err
	}
	_, err = writer.Write(payload)
	return err
}

func deterministicDigest(message proto.Message) ([]byte, error) {
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(message)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(payload)
	return digest[:], nil
}

func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var result byte
	for index := range left {
		result |= left[index] ^ right[index]
	}
	return result == 0
}
