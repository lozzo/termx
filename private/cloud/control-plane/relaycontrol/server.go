package relaycontrol

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

	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const (
	// ChallengePath、OpenPath、ReportPath 是 Relay 独立 control transport 路径。
	ChallengePath = "/v1/relay/control/challenge"
	OpenPath      = "/v1/relay/control/open"
	ReportPath    = "/v1/relay/control/report"
	protoMedia    = "application/x-protobuf"
	streamMedia   = "application/x-muxvia-cloud-stream"
	maxFrame      = 4 << 20
	proofDomain   = "muxvia-relay-control-challenge-v1\x00"
)

// CursorStore 持久保存 Relay runtime sender sequence。
type CursorStore interface {
	ControlCursor(context.Context, string, uint64, cloudpb.ControlSenderRole) (uint64, []byte, error)
	PutControlCursor(context.Context, string, uint64, cloudpb.ControlSenderRole, uint64, []byte, time.Time) error
}

// ResultSink 在 ack sender sequence 前持久推进 CommandOutbox。
type ResultSink interface {
	IngestRelayResult(context.Context, *cloudpb.RelayCommandResult, time.Time) error
}

// ServerConfig 装配 Relay control handler。
type ServerConfig struct {
	Registry     *hubregistry.Registry
	CursorStore  CursorStore
	Publisher    *Publisher
	Results      ResultSink
	Clock        func() time.Time
	Random       io.Reader
	ChallengeTTL time.Duration
	EnvelopeTTL  time.Duration
}

// Server 拥有 Relay challenge、attachment、generation fencing 和 report cursor。
type Server struct {
	registry     *hubregistry.Registry
	cursors      CursorStore
	publisher    *Publisher
	results      ResultSink
	now          func() time.Time
	random       io.Reader
	challengeTTL time.Duration
	envelopeTTL  time.Duration
	mu           sync.Mutex
	challenges   map[string]pendingChallenge
	attachments  map[string]attachment
	reportMu     sync.Mutex
}

type pendingChallenge struct {
	value        *cloudpb.RelayControlChallenge
	relayID      string
	deploymentID string
	fingerprint  string
}

type attachment struct {
	generation uint64
	cancel     context.CancelFunc
	attachedAt time.Time
}

// AttachmentStatus 返回当前进程是否仍持有精确 Relay generation 的 active stream。
func (server *Server) AttachmentStatus(relayID string) (uint64, time.Time, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()
	value, ok := server.attachments[relayID]
	return value.generation, value.attachedAt, ok
}

// NewServer 创建独立 Relay control server。
func NewServer(config ServerConfig) (*Server, error) {
	if config.Registry == nil || config.CursorStore == nil || config.Publisher == nil || config.Results == nil {
		return nil, fmt.Errorf("Relay control dependencies are required")
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.ChallengeTTL <= 0 || config.ChallengeTTL > time.Minute || config.EnvelopeTTL <= 0 || config.EnvelopeTTL > time.Hour {
		return nil, fmt.Errorf("invalid Relay control TTL")
	}
	return &Server{registry: config.Registry, cursors: config.CursorStore, publisher: config.Publisher, results: config.Results, now: config.Clock, random: config.Random, challengeTTL: config.ChallengeTTL, envelopeTTL: config.EnvelopeTTL, challenges: make(map[string]pendingChallenge), attachments: make(map[string]attachment)}, nil
}

// Handler 返回仅包含 Relay control API 的 mux。
func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(ChallengePath, server.handleChallenge)
	mux.HandleFunc(OpenPath, server.handleOpen)
	mux.HandleFunc(ReportPath, server.handleReport)
	return mux
}

func (server *Server) handleChallenge(writer http.ResponseWriter, request *http.Request) {
	input := &cloudpb.RelayControlChallengeRequest{}
	if request.Method != http.MethodPost || readProto(request, input) != nil {
		http.Error(writer, "invalid Relay challenge request", http.StatusBadRequest)
		return
	}
	deployment, err := server.registry.DeploymentByRelay(request.Context(), input.GetRelayId())
	if err != nil || !deployment.Enabled || deployment.Metadata.GetEdgeDeploymentId() != input.GetEdgeDeploymentId() || deployment.Metadata.GetRelayControlIdentityFingerprint() != input.GetRelayControlIdentityFingerprint() {
		http.Error(writer, "Relay identity rejected", http.StatusUnauthorized)
		return
	}
	value := make([]byte, 32)
	if _, err := io.ReadFull(server.random, value); err != nil {
		http.Error(writer, "challenge unavailable", http.StatusServiceUnavailable)
		return
	}
	digest := sha256.Sum256(append(append([]byte(nil), value...), []byte(input.GetRelayId())...))
	now := server.now().UTC()
	challenge := &cloudpb.RelayControlChallenge{ChallengeId: base64.RawURLEncoding.EncodeToString(digest[:18]), Challenge: value, ExpiresAtUnixMillis: now.Add(server.challengeTTL).UnixMilli()}
	server.mu.Lock()
	server.cleanupChallengesLocked(now)
	server.challenges[challenge.GetChallengeId()] = pendingChallenge{value: proto.Clone(challenge).(*cloudpb.RelayControlChallenge), relayID: input.GetRelayId(), deploymentID: input.GetEdgeDeploymentId(), fingerprint: input.GetRelayControlIdentityFingerprint()}
	server.mu.Unlock()
	writeProto(writer, http.StatusOK, &cloudpb.RelayControlChallengeResponse{Challenge: challenge})
}

func (server *Server) handleOpen(writer http.ResponseWriter, request *http.Request) {
	hello := &cloudpb.RelayHello{}
	if request.Method != http.MethodPost || readProto(request, hello) != nil || hello.GetDeployment() == nil {
		http.Error(writer, "invalid Relay hello", http.StatusBadRequest)
		return
	}
	now := server.now().UTC()
	challenge, deployment, err := server.consumeChallenge(request.Context(), hello, now)
	proof, proofErr := ChallengeProofBytes(challenge, hello.GetDeployment())
	if err != nil || proofErr != nil || !ed25519.Verify(deployment.RelayControlPublicKey, proof, hello.GetChallengeSignature()) {
		http.Error(writer, "Relay proof rejected", http.StatusUnauthorized)
		return
	}
	attached, err := server.registry.AttachRelay(request.Context(), hello, now)
	if err != nil {
		http.Error(writer, "Relay attach rejected", http.StatusConflict)
		return
	}
	streamContext, cancel := context.WithCancel(request.Context())
	defer cancel()
	server.mu.Lock()
	if previous := server.attachments[attached.Metadata.GetRelayId()]; previous.cancel != nil {
		previous.cancel()
	}
	server.attachments[attached.Metadata.GetRelayId()] = attachment{generation: attached.RelayControlGeneration, cancel: cancel, attachedAt: now}
	server.mu.Unlock()
	defer server.detach(attached.Metadata.GetRelayId(), attached.RelayControlGeneration)
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unavailable", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", streamMedia)
	writer.WriteHeader(http.StatusOK)
	sequence := uint64(1)
	ready := &cloudpb.RelayControlEnvelope{RelayId: attached.Metadata.GetRelayId(), EdgeDeploymentId: attached.Metadata.GetEdgeDeploymentId(), RelayControlGeneration: attached.RelayControlGeneration, SenderRole: cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_CONTROLLER, SenderSequence: sequence, IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(server.envelopeTTL).UnixMilli(), Payload: &cloudpb.RelayControlEnvelope_Ready{Ready: &cloudpb.RelayControlReady{RelayId: attached.Metadata.GetRelayId(), RelayControlGeneration: attached.RelayControlGeneration, IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(server.envelopeTTL).UnixMilli()}}}
	if writeFrame(writer, ready) != nil {
		return
	}
	flusher.Flush()
	updates, unsubscribe := server.publisher.Subscribe(attached.Metadata.GetRelayId())
	defer unsubscribe()
	for {
		select {
		case <-streamContext.Done():
			return
		case command := <-updates:
			if server.registry.RequireCurrentRelayGeneration(streamContext, attached.Metadata.GetRelayId(), attached.RelayControlGeneration) != nil {
				return
			}
			sequence++
			command = proto.Clone(command).(*cloudpb.RelayControlCommand)
			command.RelayControlGeneration = attached.RelayControlGeneration
			timestamp := server.now().UTC()
			envelope := &cloudpb.RelayControlEnvelope{RelayId: attached.Metadata.GetRelayId(), EdgeDeploymentId: attached.Metadata.GetEdgeDeploymentId(), RelayControlGeneration: attached.RelayControlGeneration, SenderRole: cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_CONTROLLER, SenderSequence: sequence, IssuedAtUnixMillis: timestamp.UnixMilli(), ExpiresAtUnixMillis: timestamp.Add(server.envelopeTTL).UnixMilli(), Payload: &cloudpb.RelayControlEnvelope_Command{Command: command}}
			if writeFrame(writer, envelope) != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (server *Server) handleReport(writer http.ResponseWriter, request *http.Request) {
	input := &cloudpb.ReportRelayRuntimeRequest{}
	if request.Method != http.MethodPost || readProto(request, input) != nil || len(input.GetEnvelopes()) == 0 {
		http.Error(writer, "invalid Relay runtime batch", http.StatusBadRequest)
		return
	}
	server.reportMu.Lock()
	defer server.reportMu.Unlock()
	first := input.GetEnvelopes()[0]
	if first.GetRelayId() == "" || first.GetRelayControlGeneration() == 0 || first.GetSenderRole() != cloudpb.ControlSenderRole_CONTROL_SENDER_ROLE_RELAY || server.registry.RequireCurrentRelayGeneration(request.Context(), first.GetRelayId(), first.GetRelayControlGeneration()) != nil {
		http.Error(writer, "stale Relay generation", http.StatusConflict)
		return
	}
	deployment, err := server.registry.DeploymentByRelay(request.Context(), first.GetRelayId())
	if err != nil || deployment.Metadata.GetEdgeDeploymentId() != first.GetEdgeDeploymentId() {
		http.Error(writer, "Relay deployment rejected", http.StatusConflict)
		return
	}
	accepted, acceptedDigest, err := server.cursors.ControlCursor(request.Context(), first.GetRelayId(), first.GetRelayControlGeneration(), first.GetSenderRole())
	if err != nil {
		http.Error(writer, "cursor unavailable", http.StatusServiceUnavailable)
		return
	}
	now := server.now().UTC()
	for _, envelope := range input.GetEnvelopes() {
		if envelope.GetRelayId() != first.GetRelayId() || envelope.GetEdgeDeploymentId() != first.GetEdgeDeploymentId() || envelope.GetRelayControlGeneration() != first.GetRelayControlGeneration() || envelope.GetSenderRole() != first.GetSenderRole() || envelope.GetExpiresAtUnixMillis() <= now.UnixMilli() || envelope.GetIssuedAtUnixMillis() > now.UnixMilli() || envelope.GetCommandResult() == nil {
			http.Error(writer, "Relay runtime envelope rejected", http.StatusBadRequest)
			return
		}
		digest, digestErr := deterministicDigest(envelope)
		if digestErr != nil {
			http.Error(writer, "Relay runtime envelope rejected", http.StatusBadRequest)
			return
		}
		if envelope.GetSenderSequence() == accepted {
			if !bytesEqual(digest, acceptedDigest) {
				http.Error(writer, "Relay runtime replay conflict", http.StatusConflict)
				return
			}
			continue
		}
		if envelope.GetSenderSequence() != accepted+1 {
			http.Error(writer, "Relay runtime sequence gap", http.StatusConflict)
			return
		}
		result := envelope.GetCommandResult()
		if result.GetRelayId() != first.GetRelayId() || result.GetRelayControlGeneration() != first.GetRelayControlGeneration() || server.results.IngestRelayResult(request.Context(), result, now) != nil {
			http.Error(writer, "Relay command result rejected", http.StatusConflict)
			return
		}
		accepted, acceptedDigest = envelope.GetSenderSequence(), digest
	}
	if err := server.cursors.PutControlCursor(request.Context(), first.GetRelayId(), first.GetRelayControlGeneration(), first.GetSenderRole(), accepted, acceptedDigest, now); err != nil {
		http.Error(writer, "persist Relay runtime cursor", http.StatusServiceUnavailable)
		return
	}
	writeProto(writer, http.StatusOK, &cloudpb.ReportRelayRuntimeResponse{AcceptedSenderSequence: accepted})
}

// ChallengeProofBytes 返回 Relay challenge 的确定性签名输入。
func ChallengeProofBytes(challenge *cloudpb.RelayControlChallenge, metadata *cloudpb.EdgeDeploymentMetadata) ([]byte, error) {
	if challenge == nil || metadata == nil {
		return nil, errors.New("Relay challenge proof input is incomplete")
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&cloudpb.RelayControlChallengeProofInput{ChallengeId: challenge.GetChallengeId(), Challenge: append([]byte(nil), challenge.GetChallenge()...), RelayId: metadata.GetRelayId(), EdgeDeploymentId: metadata.GetEdgeDeploymentId(), RelayControlIdentityFingerprint: metadata.GetRelayControlIdentityFingerprint()})
	if err != nil {
		return nil, err
	}
	return append([]byte(proofDomain), payload...), nil
}

func (server *Server) consumeChallenge(ctx context.Context, hello *cloudpb.RelayHello, now time.Time) (*cloudpb.RelayControlChallenge, hubregistry.Deployment, error) {
	metadata := hello.GetDeployment()
	server.mu.Lock()
	server.cleanupChallengesLocked(now)
	pending, ok := server.challenges[hello.GetChallengeId()]
	delete(server.challenges, hello.GetChallengeId())
	server.mu.Unlock()
	if !ok || pending.relayID != metadata.GetRelayId() || pending.deploymentID != metadata.GetEdgeDeploymentId() || pending.fingerprint != metadata.GetRelayControlIdentityFingerprint() || pending.value.GetExpiresAtUnixMillis() <= now.UnixMilli() {
		return nil, hubregistry.Deployment{}, hubregistry.ErrDeploymentIdentity
	}
	deployment, err := server.registry.DeploymentByRelay(ctx, metadata.GetRelayId())
	return pending.value, deployment, err
}

func (server *Server) cleanupChallengesLocked(now time.Time) {
	for id, value := range server.challenges {
		if value.value.GetExpiresAtUnixMillis() <= now.UnixMilli() {
			delete(server.challenges, id)
		}
	}
}
func (server *Server) detach(relayID string, generation uint64) {
	server.mu.Lock()
	if current := server.attachments[relayID]; current.generation == generation {
		delete(server.attachments, relayID)
	}
	server.mu.Unlock()
}

func readProto(request *http.Request, target proto.Message) error {
	if request.Header.Get("Content-Type") != protoMedia {
		return errors.New("invalid protobuf content type")
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxFrame+1))
	defer clear(body)
	if err != nil || len(body) == 0 || len(body) > maxFrame {
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
	writer.Header().Set("Content-Type", protoMedia)
	writer.WriteHeader(status)
	_, _ = writer.Write(payload)
}
func writeFrame(writer io.Writer, message proto.Message) error {
	payload, err := proto.Marshal(message)
	if err != nil || len(payload) == 0 || len(payload) > maxFrame {
		return errors.New("invalid Relay control frame")
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
