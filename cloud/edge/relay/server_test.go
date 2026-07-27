package relay

import (
	"context"
	"net"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/muxvia/muxvia/cloud/edge/policy"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
)

func TestCloseSessionAllocationsReleasesOnlyOwningSession(t *testing.T) {
	runtime := &relayCleanupRuntime{}
	outbox := &relayCleanupOutbox{}
	server := &Server{
		runtime: runtime,
		outbox:  outbox,
		now:     time.Now,
		pending: map[string]pendingReservation{
			"pending-a": {id: "pending-a", admission: policy.RelayAdmission{SessionID: "session-a"}},
			"pending-b": {id: "pending-b", admission: policy.RelayAdmission{SessionID: "session-b"}},
		},
		active: make(map[string]activeAllocation),
	}
	first := testTrackedPacketConn(t)
	second := testTrackedPacketConn(t)
	other := testTrackedPacketConn(t)
	server.active["a-1"] = activeAllocation{id: "allocation-a-1", sessionID: "session-a", conn: first}
	server.active["a-2"] = activeAllocation{id: "allocation-a-2", sessionID: "session-a", conn: second}
	server.active["b-1"] = activeAllocation{id: "allocation-b-1", sessionID: "session-b", conn: other}

	if err := server.CloseSessionAllocations(context.Background(), "session-a"); err != nil {
		t.Fatal(err)
	}
	if len(server.active) != 1 || server.active["b-1"].sessionID != "session-b" {
		t.Fatalf("remaining active allocations = %#v", server.active)
	}
	if len(server.pending) != 1 || server.pending["pending-b"].admission.SessionID != "session-b" {
		t.Fatalf("remaining reservations = %#v", server.pending)
	}
	gotClosed := runtime.closedIDs()
	sort.Strings(gotClosed)
	if len(gotClosed) != 2 || gotClosed[0] != "allocation-a-1" || gotClosed[1] != "allocation-a-2" {
		t.Fatalf("closed allocation IDs = %#v", gotClosed)
	}
	if got := runtime.canceledIDs(); len(got) != 1 || got[0] != "pending-a" {
		t.Fatalf("canceled reservation IDs = %#v", got)
	}
	if outbox.count() != 2 {
		t.Fatalf("usage events = %d", outbox.count())
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
}

func testTrackedPacketConn(t *testing.T) *trackedPacketConn {
	t.Helper()
	connection, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return &trackedPacketConn{PacketConn: connection}
}

type relayCleanupRuntime struct {
	mu       sync.Mutex
	closed   []string
	canceled []string
}

func (*relayCleanupRuntime) RelayAuth(context.Context, string, time.Time) (*cloudv1.RelayLeaseClaims, string, bool, error) {
	return nil, "", false, nil
}
func (*relayCleanupRuntime) ReserveRelayAllocation(context.Context, string, string, time.Time) (policy.RelayAdmission, error) {
	return policy.RelayAdmission{}, nil
}
func (*relayCleanupRuntime) ActivateRelayAllocation(context.Context, string, string, cloudv1.RelayTransport, time.Time) error {
	return nil
}
func (runtime *relayCleanupRuntime) CancelRelayAllocationReservation(_ context.Context, id string) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.canceled = append(runtime.canceled, id)
	return nil
}
func (runtime *relayCleanupRuntime) CloseRelayAllocation(_ context.Context, id string, _, _ uint64, now time.Time) (*cloudv1.UsageEvent, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.closed = append(runtime.closed, id)
	return &cloudv1.UsageEvent{EventId: id}, nil
}
func (runtime *relayCleanupRuntime) closedIDs() []string {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return append([]string(nil), runtime.closed...)
}
func (runtime *relayCleanupRuntime) canceledIDs() []string {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return append([]string(nil), runtime.canceled...)
}

type relayCleanupOutbox struct {
	mu     sync.Mutex
	events []*cloudv1.UsageEvent
}

func (outbox *relayCleanupOutbox) Put(event *cloudv1.UsageEvent) error {
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	outbox.events = append(outbox.events, event)
	return nil
}
func (outbox *relayCleanupOutbox) count() int {
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	return len(outbox.events)
}

func TestTrackedPacketConnEnforcesCombinedByteAndRateLimits(t *testing.T) {
	now := time.Now()
	limiter, err := testAdmissionLimiter(now, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	connection := &trackedPacketConn{limiter: limiter}
	if !connection.allow(60, true, now) {
		t.Fatal("first ingress within lease was rejected")
	}
	if connection.allow(41, false, now) {
		t.Fatal("combined ingress and egress exceeded max_bytes")
	}
	if !connection.allow(40, false, now.Add(time.Second)) {
		t.Fatal("remaining bytes within lease were rejected after rate refill")
	}
	ingress, egress := connection.counts()
	if ingress != 60 || egress != 40 {
		t.Fatalf("counters ingress=%d egress=%d", ingress, egress)
	}
}

func TestTrackedGeneratorDropsClosedUnclaimedSocket(t *testing.T) {
	generator := newTrackedGenerator(net.ParseIP("127.0.0.1"), "127.0.0.1")
	if err := generator.Validate(); err != nil {
		t.Fatal(err)
	}
	connection, _, err := generator.AllocatePacketConn("udp4", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(generator.conns) != 1 {
		t.Fatalf("tracked sockets=%d", len(generator.conns))
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if len(generator.conns) != 0 {
		t.Fatalf("closed unclaimed socket remained tracked: %d", len(generator.conns))
	}
}

func TestTrackedPacketConnRejectsBurstAboveRate(t *testing.T) {
	now := time.Now()
	limiter, err := testAdmissionLimiter(now, 1000, 100)
	if err != nil {
		t.Fatal(err)
	}
	connection := &trackedPacketConn{limiter: limiter}
	if !connection.allow(80, true, now) {
		t.Fatal("initial rate budget was rejected")
	}
	if connection.allow(30, true, now) {
		t.Fatal("burst above rate budget was accepted")
	}
	if !connection.allow(30, true, now.Add(time.Second)) {
		t.Fatal("rate budget did not refill")
	}
}

func TestTrackedPacketConnsShareLeaseBudget(t *testing.T) {
	now := time.Now()
	limiter, err := testAdmissionLimiter(now, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	first, second := &trackedPacketConn{limiter: limiter}, &trackedPacketConn{limiter: limiter}
	if !first.allow(60, true, now) || second.allow(41, true, now) {
		t.Fatal("multiple allocations did not share one RelayLease budget")
	}
}

func testAdmissionLimiter(now time.Time, maxBytes, rate uint64) (*policy.AdmissionLimiter, error) {
	account, err := policy.NewRateLimiter(now.Add(time.Minute), rate, now)
	if err != nil {
		return nil, err
	}
	session, err := policy.NewRateLimiter(now.Add(time.Minute), rate, now)
	if err != nil {
		return nil, err
	}
	lease, err := policy.NewLeaseLimiter(now.Add(time.Minute), maxBytes, rate, now)
	if err != nil {
		return nil, err
	}
	return policy.NewAdmissionLimiter(account, session, lease)
}
