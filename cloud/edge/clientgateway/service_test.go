package clientgateway

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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
func (*cleanupRuntime) AuthenticatedAgentClaims(context.Context, string) (*cloudv1.AgentTicketClaims, error) {
	return nil, nil
}

type cleanupRelay struct{ order *[]string }

func (*cleanupRelay) RequestRelayLease(context.Context, *cloudv1.RelayLeaseRequest) (*cloudv1.RelayICEConfig, error) {
	return nil, nil
}
func (*cleanupRelay) RenewRelayLease(context.Context, *cloudv1.RelayLeaseRequest, *cloudv1.RelayICEConfig) (*cloudv1.RelayICEConfig, error) {
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
		service.maintainRelayLease(ctx, &cloudv1.RelayLeaseRequest{SessionId: "session-renew"}, initial, cancel)
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
	go service.maintainRelayLease(ctx, &cloudv1.RelayLeaseRequest{SessionId: "session-expire"}, initial, cancel)

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
		service.maintainRelayLease(ctx, &cloudv1.RelayLeaseRequest{SessionId: "session-closed"}, &cloudv1.RelayICEConfig{
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
	request *cloudv1.RelayLeaseRequest
	current *cloudv1.RelayICEConfig
}

type recordingRenewalBroker struct {
	mu       sync.Mutex
	calls    chan renewalCall
	renewErr error
}

func (*recordingRenewalBroker) RequestRelayLease(context.Context, *cloudv1.RelayLeaseRequest) (*cloudv1.RelayICEConfig, error) {
	return nil, nil
}

func (broker *recordingRenewalBroker) RenewRelayLease(_ context.Context, request *cloudv1.RelayLeaseRequest, current *cloudv1.RelayICEConfig) (*cloudv1.RelayICEConfig, error) {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	if broker.calls != nil {
		broker.calls <- renewalCall{request: proto.Clone(request).(*cloudv1.RelayLeaseRequest), current: cloneRelay(current)}
	}
	if broker.renewErr != nil {
		return nil, broker.renewErr
	}
	renewed := cloneRelay(current)
	renewed.ExpiresAt = timestamppb.New(time.Now().UTC().Add(time.Minute))
	return renewed, nil
}
