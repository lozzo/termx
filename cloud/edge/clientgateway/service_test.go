package clientgateway

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"strconv"
	"strings"
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

func TestPairingAdmissionRequiresClientProofAndOnlineDaemonIdentity(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	daemonPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientPublicKey, clientPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	admission := &cloudv1.PairingAdmission{
		DaemonId: "daemon-1", DeviceId: "device-1", DevicePublicKey: daemonPublicKey,
		PairingClaimSha256: bytesOf(0x61, sha256.Size), ExpiresAtUnixNano: now.Add(time.Minute).UnixNano(),
	}
	challenge := &cloudv1.EdgeChallenge{Nonce: bytesOf(0x51, ticket.EdgeChallengeNonceSize), EdgeId: "edge-1", EdgeBootId: "edge-boot-1", StreamId: "edge-stream-1", IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(ticket.EdgeChallengeLifetime)), Target: cloudv1.EdgeChallengeTarget_EDGE_CHALLENGE_TARGET_CLIENT_GATEWAY}
	event := &cloudv1.ClientSignal{
		ProtocolVersion: ProtocolVersion, MessageId: "message-1", SenderId: remoteauth.Fingerprint(clientPublicKey), BootId: "boot-1", ConnectionId: "session-1", StreamSeq: 1, SentAt: timestamppb.New(now),
		Payload: &cloudv1.ClientSignal_Hello{Hello: &cloudv1.ClientHello{
			ClientPublicKey: clientPublicKey, Product: cloudv1.ClientProduct_CLIENT_PRODUCT_CLI, SoftwareVersion: "client-v2",
			AttemptGeneration: 7, RelayPreference: cloudv1.RelayPreference_RELAY_PREFERENCE_AUTO,
			Authorization: &cloudv1.ClientHello_PairingAdmission{PairingAdmission: admission},
		}},
	}
	proofBytes, err := ticket.ClientHelloProofBytes(challenge, event, now)
	if err != nil {
		t.Fatal(err)
	}
	event.GetHello().ClientProof = ed25519.Sign(clientPrivateKey, proofBytes)
	service := &Service{config: Config{
		EdgeID: "edge-1", EdgeBootID: "edge-boot-1", Now: func() time.Time { return now },
		Runtime: &pairingAdmissionRuntime{claims: &cloudv1.DaemonBindingClaims{AccountId: "account-1", DaemonId: "daemon-1", DeviceId: "device-1", DevicePublicKey: daemonPublicKey, EdgeId: "edge-1"}},
	}}
	claims, err := service.admit(context.Background(), event, challenge)
	if err != nil || claims.accessMode != cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_PAIRING || !proto.Equal(admission, event.GetHello().GetPairingAdmission()) {
		t.Fatalf("pairing admission claims=%#v err=%v", claims, err)
	}

	tamperedIdentity := proto.Clone(event).(*cloudv1.ClientSignal)
	tamperedIdentity.GetHello().GetPairingAdmission().DevicePublicKey[0] ^= 0xff
	if _, err := service.admit(context.Background(), tamperedIdentity, challenge); err == nil {
		t.Fatal("pairing admission accepted another daemon identity")
	}
	expired := proto.Clone(event).(*cloudv1.ClientSignal)
	expired.GetHello().GetPairingAdmission().ExpiresAtUnixNano = now.UnixNano()
	if _, err := service.admit(context.Background(), expired, challenge); err == nil {
		t.Fatal("expired pairing admission was accepted")
	}
	wrongProof := proto.Clone(event).(*cloudv1.ClientSignal)
	wrongProof.GetHello().ClientProof[0] ^= 0xff
	if _, err := service.admit(context.Background(), wrongProof, challenge); err == nil {
		t.Fatal("pairing admission accepted an invalid client proof")
	}
}

func TestClientHelloReplayFailsUnderSerialAndConcurrentFreshChallenges(t *testing.T) {
	service, clientPrivateKey, admission, now := newClientGatewayFixture(t)
	_, oldChallenge, err := service.challengeSignal()
	if err != nil {
		t.Fatal(err)
	}
	oldHello := signedClientHello(t, clientPrivateKey, admission, oldChallenge, now)
	if _, err := service.admit(context.Background(), oldHello, oldChallenge); err != nil {
		t.Fatalf("original ClientHello rejected: %v", err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		_, fresh, err := service.challengeSignal()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.admit(context.Background(), oldHello, fresh); err == nil {
			t.Fatal("serial replay accepted under a fresh challenge")
		}
	}

	const parallel = 16
	challenges := make([]*cloudv1.EdgeChallenge, parallel)
	for index := range challenges {
		_, challenges[index], err = service.challengeSignal()
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
			if _, err := service.admit(context.Background(), oldHello, challenge); err == nil {
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

func TestClientGatewaySendsOneChallengeBeforeHelloAndRejectsDuplicateHello(t *testing.T) {
	service, clientPrivateKey, admission, now := newClientGatewayFixture(t)
	stream := &clientGatewayTestStream{ctx: context.Background()}
	stream.hello = func(challenge *cloudv1.EdgeChallenge) *cloudv1.ClientSignal {
		return signedClientHello(t, clientPrivateKey, admission, challenge, now)
	}
	err := service.Connect(stream)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("duplicate ClientHello error=%v want InvalidArgument", err)
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.recvBeforeSend {
		t.Fatal("ClientGateway received Hello before sending EdgeChallenge")
	}
	challengeCount := 0
	for _, signal := range stream.sent {
		if signal.GetChallenge() != nil {
			challengeCount++
		}
	}
	if challengeCount != 1 || stream.recvCount != 2 {
		t.Fatalf("challenge count=%d Recv count=%d want 1 challenge and duplicate Hello rejection", challengeCount, stream.recvCount)
	}
}

func TestClientGatewayDoesNotReceiveHelloAfterChallengeDeadline(t *testing.T) {
	service, _, _, now := newClientGatewayFixture(t)
	calls := 0
	service.config.Now = func() time.Time {
		calls++
		if calls == 1 {
			return now
		}
		return now.Add(ticket.EdgeChallengeLifetime + time.Nanosecond)
	}
	stream := &clientGatewayTestStream{ctx: context.Background()}
	if err := service.Connect(stream); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("expired ClientGateway challenge error=%v want DeadlineExceeded", err)
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.recvCount != 0 || len(stream.sent) != 1 || stream.sent[0].GetChallenge() == nil {
		t.Fatalf("expired challenge sent=%d Recv=%d", len(stream.sent), stream.recvCount)
	}
}

func newClientGatewayFixture(t *testing.T) (*Service, ed25519.PrivateKey, *cloudv1.PairingAdmission, time.Time) {
	t.Helper()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	daemonPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, clientPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	admission := &cloudv1.PairingAdmission{
		DaemonId: "daemon-replay", DeviceId: "device-replay", DevicePublicKey: daemonPublicKey,
		PairingClaimSha256: bytesOf(0x71, sha256.Size), ExpiresAtUnixNano: now.Add(time.Minute).UnixNano(),
	}
	runtime := &clientGatewayRuntime{claims: &cloudv1.DaemonBindingClaims{AccountId: "account-replay", DaemonId: admission.GetDaemonId(), DeviceId: admission.GetDeviceId(), DevicePublicKey: daemonPublicKey, EdgeId: "edge-client"}}
	service, err := NewService(Config{EdgeID: "edge-client", EdgeBootID: "edge-client-boot", Runtime: runtime, SignalTimeout: time.Second, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return service, clientPrivateKey, admission, now
}

func signedClientHello(t *testing.T, privateKey ed25519.PrivateKey, admission *cloudv1.PairingAdmission, challenge *cloudv1.EdgeChallenge, now time.Time) *cloudv1.ClientSignal {
	t.Helper()
	publicKey := privateKey.Public().(ed25519.PublicKey)
	event := &cloudv1.ClientSignal{
		ProtocolVersion: ProtocolVersion, MessageId: "client-message", SenderId: remoteauth.Fingerprint(publicKey), BootId: "client-process-boot", ConnectionId: "client-session", StreamSeq: 1, SentAt: timestamppb.New(now),
		Payload: &cloudv1.ClientSignal_Hello{Hello: &cloudv1.ClientHello{
			ClientPublicKey: publicKey, Product: cloudv1.ClientProduct_CLIENT_PRODUCT_CLI, SoftwareVersion: "client-v2", AttemptGeneration: 7, RelayPreference: cloudv1.RelayPreference_RELAY_PREFERENCE_DIRECT_ONLY,
			Authorization: &cloudv1.ClientHello_PairingAdmission{PairingAdmission: proto.Clone(admission).(*cloudv1.PairingAdmission)},
		}},
	}
	canonical, err := ticket.ClientHelloProofBytes(challenge, event, now)
	if err != nil {
		t.Fatal(err)
	}
	event.GetHello().ClientProof = ed25519.Sign(privateKey, canonical)
	return event
}

type clientGatewayRuntime struct{ claims *cloudv1.DaemonBindingClaims }

func (*clientGatewayRuntime) UpsertSession(context.Context, *cloudv1.ClientSessionSummary) error {
	return nil
}
func (*clientGatewayRuntime) RemoveSession(context.Context, string, uint64) error { return nil }
func (*clientGatewayRuntime) BeginAgentSignal(context.Context, string, string, string) (uint64, <-chan *cloudv1.AgentEvent, error) {
	response := make(chan *cloudv1.AgentEvent, 1)
	response <- &cloudv1.AgentEvent{Payload: &cloudv1.AgentEvent_Authorization{Authorization: &cloudv1.AgentAuthorizationResult{Authorized: true}}}
	return 1, response, nil
}
func (*clientGatewayRuntime) CancelAgentSignal(context.Context, string) error { return nil }
func (*clientGatewayRuntime) SendAgentCommand(context.Context, string, uint64, *cloudv1.EdgeCommand) error {
	return nil
}
func (runtime *clientGatewayRuntime) AuthenticatedAgentClaims(context.Context, string) (*cloudv1.DaemonBindingClaims, error) {
	return proto.Clone(runtime.claims).(*cloudv1.DaemonBindingClaims), nil
}

type clientGatewayTestStream struct {
	ctx            context.Context
	mu             sync.Mutex
	sent           []*cloudv1.EdgeSignal
	hello          func(*cloudv1.EdgeChallenge) *cloudv1.ClientSignal
	firstHello     *cloudv1.ClientSignal
	recvCount      int
	recvBeforeSend bool
}

func (stream *clientGatewayTestStream) Send(signal *cloudv1.EdgeSignal) error {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	stream.sent = append(stream.sent, proto.Clone(signal).(*cloudv1.EdgeSignal))
	return nil
}

func (stream *clientGatewayTestStream) Recv() (*cloudv1.ClientSignal, error) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	stream.recvCount++
	if len(stream.sent) == 0 || stream.sent[0].GetChallenge() == nil {
		stream.recvBeforeSend = true
		return nil, io.EOF
	}
	if stream.firstHello == nil {
		stream.firstHello = stream.hello(stream.sent[0].GetChallenge())
		return proto.Clone(stream.firstHello).(*cloudv1.ClientSignal), nil
	}
	return proto.Clone(stream.firstHello).(*cloudv1.ClientSignal), nil
}

func (stream *clientGatewayTestStream) SetHeader(metadata.MD) error  { return nil }
func (stream *clientGatewayTestStream) SendHeader(metadata.MD) error { return nil }
func (stream *clientGatewayTestStream) SetTrailer(metadata.MD)       {}
func (stream *clientGatewayTestStream) Context() context.Context     { return stream.ctx }
func (stream *clientGatewayTestStream) SendMsg(any) error            { return nil }
func (stream *clientGatewayTestStream) RecvMsg(any) error            { return nil }

func bytesOf(value byte, size int) []byte {
	result := make([]byte, size)
	for index := range result {
		result[index] = value
	}
	return result
}

type pairingAdmissionRuntime struct{ claims *cloudv1.DaemonBindingClaims }

func (*pairingAdmissionRuntime) UpsertSession(context.Context, *cloudv1.ClientSessionSummary) error {
	return nil
}
func (*pairingAdmissionRuntime) RemoveSession(context.Context, string, uint64) error { return nil }
func (*pairingAdmissionRuntime) BeginAgentSignal(context.Context, string, string, string) (uint64, <-chan *cloudv1.AgentEvent, error) {
	return 0, nil, nil
}
func (*pairingAdmissionRuntime) CancelAgentSignal(context.Context, string) error { return nil }
func (*pairingAdmissionRuntime) SendAgentCommand(context.Context, string, uint64, *cloudv1.EdgeCommand) error {
	return nil
}
func (runtime *pairingAdmissionRuntime) AuthenticatedAgentClaims(context.Context, string) (*cloudv1.DaemonBindingClaims, error) {
	return proto.Clone(runtime.claims).(*cloudv1.DaemonBindingClaims), nil
}

func TestCleanupSessionClosesRelayBeforeRemovingRuntimeSession(t *testing.T) {
	order := make([]string, 0, 2)
	runtime := &cleanupRuntime{order: &order}
	relay := &cleanupRelay{order: &order}
	service := &Service{config: Config{Runtime: runtime, Relay: relay}}

	service.cleanupSession("session-1", 7)

	if len(order) != 2 || order[0] != "relay:session-1" || order[1] != "runtime:session-1:7" {
		t.Fatalf("cleanup order = %#v", order)
	}
}

type cleanupRuntime struct{ order *[]string }

func (*cleanupRuntime) UpsertSession(context.Context, *cloudv1.ClientSessionSummary) error {
	return nil
}
func (runtime *cleanupRuntime) RemoveSession(_ context.Context, sessionID string, generation uint64) error {
	*runtime.order = append(*runtime.order, "runtime:"+sessionID+":"+strconv.FormatUint(generation, 10))
	return nil
}
func (*cleanupRuntime) BeginAgentSignal(context.Context, string, string, string) (uint64, <-chan *cloudv1.AgentEvent, error) {
	return 0, nil, nil
}
func (*cleanupRuntime) CancelAgentSignal(context.Context, string) error { return nil }
func (*cleanupRuntime) SendAgentCommand(context.Context, string, uint64, *cloudv1.EdgeCommand) error {
	return nil
}
func (*cleanupRuntime) AuthenticatedAgentClaims(context.Context, string) (*cloudv1.DaemonBindingClaims, error) {
	return nil, nil
}

type cleanupRelay struct{ order *[]string }

func (*cleanupRelay) RequestRelayLease(context.Context, *cloudv1.RelayLeaseSpec) (*cloudv1.RelayICEConfig, error) {
	return nil, nil
}
func (*cleanupRelay) RenewRelayLease(context.Context, *cloudv1.RelayLeaseSpec, *cloudv1.RelayICEConfig) (*cloudv1.RelayICEConfig, error) {
	return nil, nil
}
func (relay *cleanupRelay) CloseRelaySession(_ context.Context, sessionID string) error {
	*relay.order = append(*relay.order, "relay:"+sessionID)
	return nil
}

func TestMaintainRelayLeaseRenewsWithoutChangingCredential(t *testing.T) {
	broker := &recordingRenewalBroker{calls: make(chan renewalCall, 1)}
	service := &Service{config: Config{Now: time.Now, Relay: broker}}
	now := time.Now().UTC()
	initial := &cloudv1.RelayICEConfig{
		LeaseId: "lease-renew", Username: "username-renew", Credential: "credential-renew", ExpiresAt: timestamppb.New(now.Add(80 * time.Millisecond)),
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan struct{})
	go func() {
		service.maintainRelayLease(ctx, &cloudv1.RelayLeaseSpec{SessionId: "session-renew"}, initial, cancel)
		close(done)
	}()

	select {
	case call := <-broker.calls:
		if call.request.GetRenewLeaseId() != initial.GetLeaseId() || call.current.GetUsername() != initial.GetUsername() {
			t.Fatalf("renewal call = %+v", call)
		}
	case <-time.After(time.Second):
		t.Fatal("RelayLease renewal was not requested")
	}
	cancel(context.Canceled)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RelayLease renewal loop did not stop with its session")
	}
}

func TestMaintainRelayLeaseCancelsSessionWhenRenewalCannotComplete(t *testing.T) {
	broker := &recordingRenewalBroker{renewErr: errors.New("controller unavailable")}
	service := &Service{config: Config{Now: time.Now, Relay: broker}}
	now := time.Now().UTC()
	initial := &cloudv1.RelayICEConfig{
		LeaseId: "lease-expire", Username: "username-expire", Credential: "credential-expire", ExpiresAt: timestamppb.New(now.Add(80 * time.Millisecond)),
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	go service.maintainRelayLease(ctx, &cloudv1.RelayLeaseSpec{SessionId: "session-expire"}, initial, cancel)

	select {
	case <-ctx.Done():
		if cause := context.Cause(ctx); cause == nil || !strings.Contains(cause.Error(), "expired") {
			t.Fatalf("session cancellation cause = %v", cause)
		}
	case <-time.After(time.Second):
		t.Fatal("expired RelayLease did not cancel its session")
	}
}

func TestMaintainRelayLeaseStopsBeforeRenewalAfterSessionClose(t *testing.T) {
	broker := &recordingRenewalBroker{calls: make(chan renewalCall, 1)}
	service := &Service{config: Config{Now: time.Now, Relay: broker}}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(context.Canceled)
	done := make(chan struct{})
	go func() {
		service.maintainRelayLease(ctx, &cloudv1.RelayLeaseSpec{SessionId: "session-closed"}, &cloudv1.RelayICEConfig{
			LeaseId: "lease-closed", Username: "username-closed", Credential: "credential-closed", ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
		}, cancel)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closed session left its RelayLease renewal loop running")
	}
	select {
	case call := <-broker.calls:
		t.Fatalf("closed session renewed its RelayLease: %+v", call)
	default:
	}
}

type renewalCall struct {
	request *cloudv1.RelayLeaseSpec
	current *cloudv1.RelayICEConfig
}

type recordingRenewalBroker struct {
	mu       sync.Mutex
	calls    chan renewalCall
	renewErr error
}

func (*recordingRenewalBroker) RequestRelayLease(context.Context, *cloudv1.RelayLeaseSpec) (*cloudv1.RelayICEConfig, error) {
	return nil, nil
}

func (broker *recordingRenewalBroker) RenewRelayLease(_ context.Context, request *cloudv1.RelayLeaseSpec, current *cloudv1.RelayICEConfig) (*cloudv1.RelayICEConfig, error) {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	if broker.calls != nil {
		broker.calls <- renewalCall{request: proto.Clone(request).(*cloudv1.RelayLeaseSpec), current: cloneRelay(current)}
	}
	if broker.renewErr != nil {
		return nil, broker.renewErr
	}
	renewed := cloneRelay(current)
	renewed.ExpiresAt = timestamppb.New(time.Now().UTC().Add(time.Minute))
	return renewed, nil
}
