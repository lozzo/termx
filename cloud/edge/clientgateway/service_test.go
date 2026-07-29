package clientgateway

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
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
	proofBytes, err := ticket.PairingHelloProofBytes(admission, "edge-1", "session-1", 7, cloudv1.ClientProduct_CLIENT_PRODUCT_CLI)
	if err != nil {
		t.Fatal(err)
	}
	event := &cloudv1.ClientSignal{
		ProtocolVersion: ProtocolVersion, MessageId: "message-1", SenderId: remoteauth.Fingerprint(clientPublicKey), BootId: "boot-1", ConnectionId: "session-1", StreamSeq: 1,
		Payload: &cloudv1.ClientSignal_Hello{Hello: &cloudv1.ClientHello{
			ClientPublicKey: clientPublicKey, ClientProof: ed25519.Sign(clientPrivateKey, proofBytes), Product: cloudv1.ClientProduct_CLIENT_PRODUCT_CLI,
			AttemptGeneration: 7, RelayPreference: cloudv1.RelayPreference_RELAY_PREFERENCE_AUTO,
			Authorization: &cloudv1.ClientHello_PairingAdmission{PairingAdmission: admission},
		}},
	}
	service := &Service{config: Config{
		EdgeID: "edge-1", Now: func() time.Time { return now },
		Runtime: &pairingAdmissionRuntime{claims: &cloudv1.DaemonBindingClaims{AccountId: "account-1", DaemonId: "daemon-1", DeviceId: "device-1", DevicePublicKey: daemonPublicKey, EdgeId: "edge-1"}},
	}}
	claims, err := service.admit(context.Background(), event)
	if err != nil || claims.accessMode != cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_PAIRING || !proto.Equal(admission, event.GetHello().GetPairingAdmission()) {
		t.Fatalf("pairing admission claims=%#v err=%v", claims, err)
	}

	tamperedIdentity := proto.Clone(event).(*cloudv1.ClientSignal)
	tamperedIdentity.GetHello().GetPairingAdmission().DevicePublicKey[0] ^= 0xff
	if _, err := service.admit(context.Background(), tamperedIdentity); err == nil {
		t.Fatal("pairing admission accepted another daemon identity")
	}
	expired := proto.Clone(event).(*cloudv1.ClientSignal)
	expired.GetHello().GetPairingAdmission().ExpiresAtUnixNano = now.UnixNano()
	if _, err := service.admit(context.Background(), expired); err == nil {
		t.Fatal("expired pairing admission was accepted")
	}
	wrongProof := proto.Clone(event).(*cloudv1.ClientSignal)
	wrongProof.GetHello().ClientProof[0] ^= 0xff
	if _, err := service.admit(context.Background(), wrongProof); err == nil {
		t.Fatal("pairing admission accepted an invalid client proof")
	}
}

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
