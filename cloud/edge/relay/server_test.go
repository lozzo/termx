package relay

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/edge/policy"
	"github.com/anytty/anytty/cloud/edge/usage"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
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
func (runtime *relayCleanupRuntime) FreezeRelayAllocationUsage(_ context.Context, id string, _, _ uint64) (*cloudv1.UsageEvent, error) {
	return &cloudv1.UsageEvent{EventId: id, AllocationId: id}, nil
}
func (runtime *relayCleanupRuntime) FinalizeRelayAllocation(_ context.Context, event *cloudv1.UsageEvent) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.closed = append(runtime.closed, event.GetAllocationId())
	return nil
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

func TestCloseSessionAllocationPutFailureIsFailClosed(t *testing.T) {
	runtime := &settlementRuntime{allocation: true, counters: 1, deltas: 1, event: validUsageEvent("event-failed")}
	connection := testTrackedPacketConn(t)
	server := &Server{
		runtime: runtime,
		outbox:  failingUsageOutbox{err: errors.New("disk full")},
		pending: make(map[string]pendingReservation),
		active: map[string]activeAllocation{
			"allocation": {id: runtime.event.GetAllocationId(), sessionID: runtime.event.GetSessionId(), conn: connection},
		},
	}
	err := server.CloseSessionAllocations(context.Background(), runtime.event.GetSessionId())
	if err == nil {
		t.Fatal("settlement succeeded despite outbox failure")
	}
	assertFailedSettlementIsFailClosed(t, server, runtime)
}

func TestAllocationDeletedPutFailureIsFailClosed(t *testing.T) {
	runtime := &settlementRuntime{allocation: true, counters: 1, deltas: 1, event: validUsageEvent("event-deleted")}
	source := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 10000}
	destination := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 20000}
	server := &Server{
		runtime: runtime,
		outbox:  failingUsageOutbox{err: errors.New("disk full")},
		pending: make(map[string]pendingReservation),
		active: map[string]activeAllocation{
			allocationKey(source, destination, "udp"): {id: runtime.event.GetAllocationId(), sessionID: runtime.event.GetSessionId(), conn: &trackedPacketConn{ingress: 10, egress: 20}},
		},
	}
	server.allocationDeleted(source, destination, "udp", "", "")
	assertFailedSettlementIsFailClosed(t, server, runtime)
}

func assertFailedSettlementIsFailClosed(t *testing.T, server *Server, runtime *settlementRuntime) {
	t.Helper()
	if len(server.active) != 0 {
		t.Fatalf("closed physical allocations remain tracked: %v", server.active)
	}
	if !server.Degraded() || server.reserve("new-user", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30000}) {
		t.Fatalf("Relay did not reject new work after settlement failure: degraded=%v", server.Degraded())
	}
	if !runtime.frozen || !runtime.allocation || runtime.counters != 1 || runtime.deltas != 1 || runtime.finalizeCalls != 0 {
		t.Fatalf("frozen=%v allocation=%v counters=%d deltas=%d finalize=%d", runtime.frozen, runtime.allocation, runtime.counters, runtime.deltas, runtime.finalizeCalls)
	}
}

func TestSettleAllocationPersistsBeforeBlockedFinalize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	outbox, err := usage.Open(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &settlementRuntime{allocation: true, counters: 1, deltas: 1, event: validUsageEvent("event-blocked"), finalizeEntered: make(chan struct{}), finalizeRelease: make(chan struct{})}
	server := &Server{runtime: runtime, outbox: outbox}
	settled := make(chan error, 1)
	go func() {
		settled <- server.settleAllocation(context.Background(), activeAllocation{id: runtime.event.GetAllocationId(), conn: &trackedPacketConn{ingress: 10, egress: 20}})
	}()
	<-runtime.finalizeEntered
	if err := outbox.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := usage.Open(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := reopened.Batch(10)
	if err != nil || len(batch) != 1 || batch[0].GetEventId() != runtime.event.GetEventId() {
		t.Fatalf("durable batch=%v err=%v", batch, err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	close(runtime.finalizeRelease)
	if err := <-settled; err != nil {
		t.Fatal(err)
	}
}

type failingUsageOutbox struct{ err error }

func (outbox failingUsageOutbox) Put(*cloudv1.UsageEvent) error { return outbox.err }

type settlementRuntime struct {
	frozen          bool
	allocation      bool
	counters        int
	deltas          int
	finalizeCalls   int
	event           *cloudv1.UsageEvent
	finalizeEntered chan struct{}
	finalizeRelease chan struct{}
}

func (*settlementRuntime) RelayAuth(context.Context, string, time.Time) (*cloudv1.RelayLeaseClaims, string, bool, error) {
	return nil, "", false, nil
}
func (*settlementRuntime) ReserveRelayAllocation(context.Context, string, string, time.Time) (policy.RelayAdmission, error) {
	return policy.RelayAdmission{}, nil
}
func (*settlementRuntime) ActivateRelayAllocation(context.Context, string, string, cloudv1.RelayTransport, time.Time) error {
	return nil
}
func (*settlementRuntime) CancelRelayAllocationReservation(context.Context, string) error { return nil }
func (runtime *settlementRuntime) FreezeRelayAllocationUsage(context.Context, string, uint64, uint64) (*cloudv1.UsageEvent, error) {
	runtime.frozen = true
	return runtime.event, nil
}
func (runtime *settlementRuntime) FinalizeRelayAllocation(_ context.Context, event *cloudv1.UsageEvent) error {
	runtime.finalizeCalls++
	if runtime.finalizeEntered != nil {
		close(runtime.finalizeEntered)
		<-runtime.finalizeRelease
	}
	if event.GetEventId() != runtime.event.GetEventId() {
		return errors.New("unexpected frozen event")
	}
	runtime.allocation = false
	runtime.counters--
	runtime.deltas++
	return nil
}

func validUsageEvent(eventID string) *cloudv1.UsageEvent {
	started := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	return &cloudv1.UsageEvent{
		SchemaVersion: 1, EventId: eventID, EdgeId: "edge", LeaseId: "lease", AccountId: "account", DaemonId: "daemon", ClientId: "client", SessionId: "session",
		AllocationId: "allocation-" + eventID, Transport: cloudv1.RelayTransport_RELAY_TRANSPORT_UDP, IngressBytes: 10, EgressBytes: 20,
		StartedAt: timestamppb.New(started), EndedAt: timestamppb.New(started.Add(time.Second)),
	}
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
