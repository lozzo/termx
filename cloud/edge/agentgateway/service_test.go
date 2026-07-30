package agentgateway

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAgentHelloReplayFailsUnderSerialAndConcurrentFreshChallenges(t *testing.T) {
	service, identity, binding, now := newAgentGatewayFixture(t)
	_, oldChallenge, err := service.challengeCommand()
	if err != nil {
		t.Fatal(err)
	}
	oldHello := signedAgentHello(t, identity, binding, oldChallenge, now)
	if _, err := service.admit(oldHello, oldChallenge); err != nil {
		t.Fatalf("original AgentHello rejected: %v", err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		_, fresh, err := service.challengeCommand()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.admit(oldHello, fresh); err == nil {
			t.Fatal("serial replay accepted under a fresh challenge")
		}
	}

	const parallel = 16
	challenges := make([]*cloudv1.EdgeChallenge, parallel)
	for index := range challenges {
		_, challenges[index], err = service.challengeCommand()
		if err != nil {
			t.Fatal(err)
		}
	}
	var wait sync.WaitGroup
	failures := make(chan int, parallel)
	for index, challenge := range challenges {
		wait.Add(1)
		go func(index int, challenge *cloudv1.EdgeChallenge) {
			defer wait.Done()
			if _, err := service.admit(oldHello, challenge); err == nil {
				failures <- index
			}
		}(index, challenge)
	}
	wait.Wait()
	close(failures)
	for index := range failures {
		t.Errorf("concurrent replay %d was accepted", index)
	}
}

func TestAgentGatewaySendsOneChallengeBeforeHelloAndRejectsDuplicateHello(t *testing.T) {
	service, identity, binding, now := newAgentGatewayFixture(t)
	stream := &agentGatewayTestStream{ctx: context.Background()}
	stream.hello = func(challenge *cloudv1.EdgeChallenge) *cloudv1.AgentEvent {
		return signedAgentHello(t, identity, binding, challenge, now)
	}
	err := service.Connect(stream)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("duplicate AgentHello error=%v want InvalidArgument", err)
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.recvBeforeSend {
		t.Fatal("AgentGateway received Hello before sending EdgeChallenge")
	}
	challengeCount := 0
	for _, command := range stream.sent {
		if command.GetChallenge() != nil {
			challengeCount++
		}
	}
	if challengeCount != 1 || stream.recvCount != 2 {
		t.Fatalf("challenge count=%d Recv count=%d want 1 challenge and duplicate Hello rejection", challengeCount, stream.recvCount)
	}
}

func TestAgentGatewayDoesNotReceiveHelloAfterChallengeDeadline(t *testing.T) {
	service, _, _, now := newAgentGatewayFixture(t)
	calls := 0
	service.config.Now = func() time.Time {
		calls++
		if calls == 1 {
			return now
		}
		return now.Add(ticket.EdgeChallengeLifetime + time.Nanosecond)
	}
	stream := &agentGatewayTestStream{ctx: context.Background()}
	if err := service.Connect(stream); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("expired AgentGateway challenge error=%v want DeadlineExceeded", err)
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.recvCount != 0 || len(stream.sent) != 1 || stream.sent[0].GetChallenge() == nil {
		t.Fatalf("expired challenge sent=%d Recv=%d", len(stream.sent), stream.recvCount)
	}
}

func newAgentGatewayFixture(t *testing.T) (*Service, remoteauth.Identity, *cloudv1.SignedEnvelope, time.Time) {
	t.Helper()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	controllerPublic, controllerPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, daemonPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteauth.NewIdentity("agent-device", daemonPrivate)
	if err != nil {
		t.Fatal(err)
	}
	claims := &cloudv1.DaemonBindingClaims{
		BindingId: "binding-agent", DaemonId: "daemon-agent", AccountId: "account-agent", EdgeId: "edge-agent", DeviceId: identity.DeviceID, DevicePublicKey: identity.PublicKey,
		Capabilities: []cloudv1.DaemonCapability{cloudv1.DaemonCapability_DAEMON_CAPABILITY_SIGNALING}, IssuedAt: timestamppb.New(now.Add(-time.Minute)), ExpiresAt: timestamppb.New(now.Add(time.Minute)), Revision: 1, EdgeLocatorSha256: make([]byte, sha256.Size),
	}
	binding, err := ticket.SignDaemonBinding("binding-key", controllerPrivate, claims)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Config{
		EdgeID: "edge-agent", EdgeBootID: "edge-agent-boot", Runtime: &agentGatewayRuntime{}, VerificationKeys: func() ticket.KeySet { return ticket.KeySet{"binding-key": controllerPublic} },
		Heartbeat: time.Second, HeartbeatTimeout: 2 * time.Second, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, identity, binding, now
}

func signedAgentHello(t *testing.T, identity remoteauth.Identity, binding *cloudv1.SignedEnvelope, challenge *cloudv1.EdgeChallenge, now time.Time) *cloudv1.AgentEvent {
	t.Helper()
	event := &cloudv1.AgentEvent{
		ProtocolVersion: ProtocolVersion, MessageId: "agent-message", SenderId: "daemon-agent", BootId: "daemon-process-boot", ConnectionId: "daemon-session", StreamSeq: 1, SentAt: timestamppb.New(now),
		Payload: &cloudv1.AgentEvent_Hello{Hello: &cloudv1.AgentHello{DaemonBinding: proto.Clone(binding).(*cloudv1.SignedEnvelope), SoftwareVersion: "agent-v2", AttemptGeneration: 1}},
	}
	proof, err := ticket.SignAgentHelloProof(identity, challenge, event, now)
	if err != nil {
		t.Fatal(err)
	}
	event.GetHello().DeviceProof = proof
	return event
}

type agentGatewayRuntime struct{}

func (*agentGatewayRuntime) AttachAuthenticatedAgent(context.Context, *cloudv1.AgentPresence, *cloudv1.DaemonBindingClaims, func(*cloudv1.EdgeCommand) bool, func()) (uint64, error) {
	return 1, nil
}
func (*agentGatewayRuntime) DetachAgent(context.Context, string, uint64) error { return nil }
func (*agentGatewayRuntime) ResolveAgentSignal(context.Context, string, uint64, *cloudv1.AgentEvent) error {
	return nil
}

type agentGatewayTestStream struct {
	ctx            context.Context
	mu             sync.Mutex
	sent           []*cloudv1.EdgeCommand
	hello          func(*cloudv1.EdgeChallenge) *cloudv1.AgentEvent
	firstHello     *cloudv1.AgentEvent
	recvCount      int
	recvBeforeSend bool
}

func (stream *agentGatewayTestStream) Send(command *cloudv1.EdgeCommand) error {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	stream.sent = append(stream.sent, proto.Clone(command).(*cloudv1.EdgeCommand))
	return nil
}

func (stream *agentGatewayTestStream) Recv() (*cloudv1.AgentEvent, error) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	stream.recvCount++
	if len(stream.sent) == 0 || stream.sent[0].GetChallenge() == nil {
		stream.recvBeforeSend = true
		return nil, io.EOF
	}
	if stream.firstHello == nil {
		stream.firstHello = stream.hello(stream.sent[0].GetChallenge())
		return proto.Clone(stream.firstHello).(*cloudv1.AgentEvent), nil
	}
	return proto.Clone(stream.firstHello).(*cloudv1.AgentEvent), nil
}

func (stream *agentGatewayTestStream) SetHeader(metadata.MD) error  { return nil }
func (stream *agentGatewayTestStream) SendHeader(metadata.MD) error { return nil }
func (stream *agentGatewayTestStream) SetTrailer(metadata.MD)       {}
func (stream *agentGatewayTestStream) Context() context.Context     { return stream.ctx }
func (stream *agentGatewayTestStream) SendMsg(any) error            { return nil }
func (stream *agentGatewayTestStream) RecvMsg(any) error            { return nil }
