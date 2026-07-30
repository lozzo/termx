package relay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/edge/policy"
	"github.com/anytty/anytty/cloud/edge/usage"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
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
	server.active["allocation-a-1"] = activeAllocation{id: "allocation-a-1", sessionID: "session-a", conn: first}
	server.active["allocation-a-2"] = activeAllocation{id: "allocation-a-2", sessionID: "session-a", conn: second}
	server.active["allocation-b-1"] = activeAllocation{id: "allocation-b-1", sessionID: "session-b", conn: other}

	if err := server.CloseSessionAllocations(context.Background(), "session-a"); err != nil {
		t.Fatal(err)
	}
	if len(server.active) != 1 || server.active["allocation-b-1"].sessionID != "session-b" {
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

func TestServerCloseSettlesActiveAndCancelsPendingExactlyOnce(t *testing.T) {
	runtime := &relayCleanupRuntime{}
	outbox := &relayCleanupOutbox{}
	connection := testTrackedPacketConn(t)
	source, destination := testAllocationAddresses()
	server := &Server{
		runtime: runtime, outbox: outbox, now: time.Now, closed: make(chan struct{}), pending: map[string]pendingReservation{
			"pending": {id: "pending", admission: policy.RelayAdmission{SessionID: "session"}},
		},
		active: map[string]activeAllocation{
			"active": {id: "active", sessionID: "session", conn: connection},
		},
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	server.allocationCreated(source, destination, "udp", "late", "", nil, 0)
	server.allocationDeleted(source, destination, "udp", "", "")
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if got := runtime.canceledIDs(); len(got) != 1 || got[0] != "pending" {
		t.Fatalf("canceled reservations = %v", got)
	}
	if got := runtime.closedIDs(); len(got) != 1 || got[0] != "active" {
		t.Fatalf("finalized allocations = %v", got)
	}
	if outbox.count() != 1 || len(server.pending) != 0 || len(server.active) != 0 {
		t.Fatalf("outbox=%d pending=%d active=%d", outbox.count(), len(server.pending), len(server.active))
	}
}

func TestServerCloseWaitsForClaimedDeletionCallback(t *testing.T) {
	runtime := &relayBarrierRuntime{event: validUsageEvent("event-close-race")}
	outbox := &barrierUsageOutbox{entered: make(chan struct{}), release: make(chan struct{})}
	source, destination := testAllocationAddresses()
	server := &Server{runtime: runtime, outbox: outbox, now: time.Now, closed: make(chan struct{}), pending: make(map[string]pendingReservation)}
	seedActiveAllocation(server, allocationKey(source, destination, "udp"), activeAllocation{id: runtime.event.GetAllocationId(), sessionID: runtime.event.GetSessionId(), conn: &trackedPacketConn{}})
	deleted := make(chan struct{})
	go func() {
		server.allocationDeleted(source, destination, "udp", "", "")
		close(deleted)
	}()
	<-outbox.entered
	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()
	waitForServerClosing(t, server)
	select {
	case err := <-closed:
		t.Fatalf("Close returned before claimed callback completed: %v", err)
	default:
	}
	close(outbox.release)
	<-deleted
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	server.allocationDeleted(source, destination, "udp", "", "")
	stats := runtime.stats()
	if stats.freezeCalls != 1 || stats.finalizeCalls != 1 {
		t.Fatalf("freeze=%d finalize=%d", stats.freezeCalls, stats.finalizeCalls)
	}
}

func TestServerCloseWaitsForActivateAndSettlesIt(t *testing.T) {
	runtime := &relayBarrierRuntime{event: validUsageEvent("event-activate"), activateEntered: make(chan struct{}), activateRelease: make(chan struct{})}
	outbox := &relayCleanupOutbox{}
	source, destination := testAllocationAddresses()
	relayAddress, generator := testRelayGenerator(t)
	username := "username-activate"
	server := &Server{
		runtime: runtime, outbox: outbox, now: time.Now, closed: make(chan struct{}), generator: generator,
		pending: map[string]pendingReservation{
			reservationKey(username, source): {id: "reservation-activate", admission: policy.RelayAdmission{SessionID: "session"}},
		}, active: make(map[string]activeAllocation),
	}
	created := make(chan struct{})
	go func() {
		server.allocationCreated(source, destination, "udp", username, "", relayAddress, 0)
		close(created)
	}()
	<-runtime.activateEntered
	closed := make(chan error, 1)
	closeStarted := make(chan struct{})
	go func() {
		close(closeStarted)
		closed <- server.Close()
	}()
	<-closeStarted
	select {
	case err := <-closed:
		t.Fatalf("Close returned while Activate was executing: %v", err)
	default:
	}
	close(runtime.activateRelease)
	<-created
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	stats := runtime.stats()
	if stats.activateCalls != 1 || stats.freezeCalls != 1 || stats.finalizeCalls != 1 || outbox.count() != 1 {
		t.Fatalf("activate=%d freeze=%d finalize=%d outbox=%d", stats.activateCalls, stats.freezeCalls, stats.finalizeCalls, outbox.count())
	}
}

func TestServerClosePutFailureKeepsFrozenRuntimeOwner(t *testing.T) {
	runtime := &settlementRuntime{allocation: true, counters: 1, deltas: 1, event: validUsageEvent("event-close-failed")}
	connection := testTrackedPacketConn(t)
	server := &Server{
		runtime: runtime, outbox: failingUsageOutbox{err: errors.New("disk full")}, now: time.Now, closed: make(chan struct{}), pending: make(map[string]pendingReservation),
		active: map[string]activeAllocation{
			runtime.event.GetAllocationId(): {id: runtime.event.GetAllocationId(), sessionID: runtime.event.GetSessionId(), conn: connection},
		},
	}
	if err := server.Close(); err == nil {
		t.Fatal("Close succeeded despite outbox failure")
	}
	assertFailedSettlementIsFailClosed(t, server, runtime)
	if err := server.Close(); err == nil || runtime.freezeCalls != 1 {
		t.Fatalf("repeated Close error=%v freeze calls=%d", err, runtime.freezeCalls)
	}
}

func TestServerCloseRetriesFrozenPendingAfterOutboxRecovery(t *testing.T) {
	allocationID := "allocation-put-retry"
	runtime := newControlledSettlementRuntime(allocationID)
	eventID := runtime.events[allocationID].GetEventId()
	outbox := &scriptedUsageOutbox{failures: map[string]int{eventID: 1}}
	server, _, _ := retryableAllocationServer(t, runtime, outbox, allocationID, "session")

	if err := server.Close(); err == nil {
		t.Fatal("first Close succeeded despite injected Put failure")
	}
	assertFrozenPending(t, server, eventID, runtime.events[allocationID])
	if server.StateCloseSafe() {
		t.Fatal("State was disposable while Relay owned an undurable frozen event")
	}
	if err := server.Close(); err == nil {
		t.Fatal("degraded retry Close unexpectedly lost failure history")
	}
	assertFrozenRetryCompleted(t, server, runtime, outbox, eventID, 2)
}

func TestServerCloseKeepsFrozenPendingAcrossContinuousPutFailures(t *testing.T) {
	allocationID := "allocation-put-continuous"
	runtime := newControlledSettlementRuntime(allocationID)
	event := runtime.events[allocationID]
	outbox := &scriptedUsageOutbox{failures: map[string]int{event.GetEventId(): -1}}
	server, _, _ := retryableAllocationServer(t, runtime, outbox, allocationID, "session")

	for attempt := 0; attempt < 3; attempt++ {
		if err := server.Close(); err == nil {
			t.Fatalf("Close attempt %d succeeded during continuous Put failure", attempt+1)
		}
		assertFrozenPending(t, server, event.GetEventId(), event)
	}
	calls, durable := outbox.stats()
	stats := runtime.stats()
	if len(calls) != 3 || len(durable) != 0 || len(stats.freezeCalls) != 1 || stats.finalizeCalls != 0 || server.StateCloseSafe() {
		t.Fatalf("puts=%d durable=%d freeze=%d finalize=%d safe=%v", len(calls), len(durable), len(stats.freezeCalls), stats.finalizeCalls, server.StateCloseSafe())
	}
}

func TestServerCloseRetriesMultipleFrozenEventsInDeterministicOrder(t *testing.T) {
	allocationIDs := []string{"allocation-pending-a", "allocation-pending-b", "allocation-pending-c"}
	runtime := newControlledSettlementRuntime(allocationIDs...)
	outbox := &scriptedUsageOutbox{failures: make(map[string]int)}
	server := &Server{
		runtime: runtime, outbox: outbox, now: time.Now, closed: make(chan struct{}), pending: make(map[string]pendingReservation), active: make(map[string]activeAllocation),
	}
	for index, allocationID := range allocationIDs {
		eventID := runtime.events[allocationID].GetEventId()
		outbox.failures[eventID] = 1
		if index == 1 {
			outbox.failures[eventID] = 2
		}
		connection := testTrackedPacketConn(t)
		connection.ingress, connection.egress = uint64(index+1), uint64(index+11)
		server.active[allocationID] = activeAllocation{id: allocationID, key: fmt.Sprintf("key-%d", index), sessionID: "session", conn: connection}
	}

	if err := server.Close(); err == nil {
		t.Fatal("first Close succeeded despite all initial Put failures")
	}
	if got := frozenPendingCount(server); got != 3 {
		t.Fatalf("initial frozen pending = %d", got)
	}
	if err := server.Close(); err == nil {
		t.Fatal("partial retry Close unexpectedly lost failure history")
	}
	if got := frozenPendingCount(server); got != 1 {
		t.Fatalf("partial frozen pending = %d", got)
	}
	calls, durable := outbox.stats()
	secondPass := []string{calls[3].GetEventId(), calls[4].GetEventId(), calls[5].GetEventId()}
	if !sort.StringsAreSorted(secondPass) || len(durable) != 2 || runtime.stats().finalizeCalls != 2 || server.StateCloseSafe() {
		t.Fatalf("retry order=%v durable=%d finalize=%d safe=%v", secondPass, len(durable), runtime.stats().finalizeCalls, server.StateCloseSafe())
	}
	if err := server.Close(); err == nil {
		t.Fatal("degraded final retry Close unexpectedly lost failure history")
	}
	if !server.StateCloseSafe() || frozenPendingCount(server) != 0 || runtime.stats().finalizeCalls != 3 {
		t.Fatalf("safe=%v pending=%d finalize=%d", server.StateCloseSafe(), frozenPendingCount(server), runtime.stats().finalizeCalls)
	}
}

func TestConcurrentAllocationDeletedAndCloseRetryFrozenEventExactlyOnce(t *testing.T) {
	allocationID := "allocation-delete-close-put"
	runtime := newControlledSettlementRuntime(allocationID)
	eventID := runtime.events[allocationID].GetEventId()
	outbox := &scriptedUsageOutbox{
		failures: map[string]int{eventID: 1}, firstEntered: make(chan struct{}), firstRelease: make(chan struct{}),
	}
	server, _, _ := retryableAllocationServer(t, runtime, outbox, allocationID, "session")
	source, destination := testAllocationAddresses()
	deleted := make(chan struct{})
	go func() {
		server.allocationDeleted(source, destination, "udp", "", "")
		close(deleted)
	}()
	<-outbox.firstEntered
	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()
	waitForServerClosing(t, server)
	select {
	case err := <-closed:
		t.Fatalf("Close returned before callback Put completed: %v", err)
	default:
	}
	close(outbox.firstRelease)
	<-deleted
	if err := <-closed; err == nil {
		t.Fatal("degraded Close unexpectedly lost callback Put failure")
	}
	assertFrozenRetryCompleted(t, server, runtime, outbox, eventID, 2)
}

func TestFinalizeFailureDoesNotBlockStateCloseOrRepeatDurableWork(t *testing.T) {
	allocationID := "allocation-finalize-failed"
	runtime := newControlledSettlementRuntime(allocationID)
	runtime.finalizeErr = errors.New("injected finalize failure")
	outbox := &scriptedUsageOutbox{}
	server, _, _ := retryableAllocationServer(t, runtime, outbox, allocationID, "session")

	if err := server.Close(); err == nil {
		t.Fatal("Close succeeded despite Finalize failure")
	}
	if !server.StateCloseSafe() || frozenPendingCount(server) != 0 {
		t.Fatalf("safe=%v pending=%d", server.StateCloseSafe(), frozenPendingCount(server))
	}
	if err := server.Close(); err == nil {
		t.Fatal("degraded repeated Close unexpectedly lost failure history")
	}
	calls, durable := outbox.stats()
	stats := runtime.stats()
	if len(calls) != 1 || len(durable) != 1 || len(stats.freezeCalls) != 1 || stats.finalizeCalls != 1 {
		t.Fatalf("puts=%d durable=%d freeze=%d finalize=%d", len(calls), len(durable), len(stats.freezeCalls), stats.finalizeCalls)
	}
}

func TestDurableOutboxAcceptsSameFrozenEventIdempotently(t *testing.T) {
	allocationID := "allocation-idempotent"
	runtime := newControlledSettlementRuntime(allocationID)
	event := runtime.events[allocationID]
	outbox, err := usage.Open(filepath.Join(t.TempDir(), "usage.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outbox.Close() }()
	if err := outbox.Put(event); err != nil {
		t.Fatal(err)
	}
	server, _, _ := retryableAllocationServer(t, runtime, outbox, allocationID, "session")
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	depth, err := outbox.Len()
	if err != nil || depth != 1 || !server.StateCloseSafe() || runtime.stats().finalizeCalls != 1 {
		t.Fatalf("depth=%d err=%v safe=%v finalize=%d", depth, err, server.StateCloseSafe(), runtime.stats().finalizeCalls)
	}
}

func TestPutFailureSealsInFlightReserveBeforePendingPublish(t *testing.T) {
	runtime := &relayBarrierRuntime{event: validUsageEvent("event-reserve-race"), reserveEntered: make(chan struct{}), reserveRelease: make(chan struct{})}
	source, destination := testAllocationAddresses()
	server := &Server{runtime: runtime, outbox: failingUsageOutbox{err: errors.New("disk full")}, now: time.Now, closed: make(chan struct{}), pending: make(map[string]pendingReservation)}
	seedActiveAllocation(server, allocationKey(source, destination, "udp"), activeAllocation{id: runtime.event.GetAllocationId(), sessionID: runtime.event.GetSessionId(), conn: &trackedPacketConn{}})
	reserved := make(chan bool, 1)
	go func() { reserved <- server.reserve("username-race", source) }()
	<-runtime.reserveEntered
	server.allocationDeleted(source, destination, "udp", "", "")
	close(runtime.reserveRelease)
	if <-reserved {
		t.Fatal("in-flight Reserve published pending after degraded seal")
	}
	reservationID := reservationKey("username-race", source)
	stats := runtime.stats()
	if len(server.pending) != 0 || !contains(stats.canceled, reservationID) || !server.Degraded() {
		t.Fatalf("pending=%d canceled=%v degraded=%v", len(server.pending), stats.canceled, server.Degraded())
	}
	_ = server.Close()
}

func TestPutFailureAndAllocationCreatedShareActivateGate(t *testing.T) {
	runtime := &relayBarrierRuntime{event: validUsageEvent("event-created-race"), activateEntered: make(chan struct{}), activateRelease: make(chan struct{})}
	outbox := &barrierUsageOutbox{entered: make(chan struct{}), release: make(chan struct{}), err: errors.New("disk full")}
	source, destination := testAllocationAddresses()
	relayAddress, generator := testRelayGenerator(t)
	username := "username-created"
	existingSource := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 11000}
	server := &Server{
		runtime: runtime, outbox: outbox, now: time.Now, closed: make(chan struct{}), generator: generator,
		pending: map[string]pendingReservation{
			reservationKey(username, source): {id: "reservation-created", admission: policy.RelayAdmission{SessionID: "session"}},
		},
	}
	seedActiveAllocation(server, allocationKey(existingSource, destination, "udp"), activeAllocation{id: runtime.event.GetAllocationId(), sessionID: runtime.event.GetSessionId(), conn: &trackedPacketConn{}})
	deleted := make(chan struct{})
	go func() {
		server.allocationDeleted(existingSource, destination, "udp", "", "")
		close(deleted)
	}()
	<-outbox.entered
	created := make(chan struct{})
	go func() {
		server.allocationCreated(source, destination, "udp", username, "", relayAddress, 0)
		close(created)
	}()
	<-runtime.activateEntered
	close(outbox.release)
	select {
	case <-deleted:
		t.Fatal("Put failure returned while Activate still held the linearization gate")
	default:
	}
	close(runtime.activateRelease)
	<-created
	<-deleted
	if !server.Degraded() {
		t.Fatal("Put failure did not seal Relay admission")
	}
	lateAddress, lateGenerator := testRelayGenerator(t)
	server.mu.Lock()
	server.generator = lateGenerator
	server.pending[reservationKey("username-late", source)] = pendingReservation{id: "reservation-late", admission: policy.RelayAdmission{SessionID: "session"}}
	server.mu.Unlock()
	server.allocationCreated(source, destination, "udp", "username-late", "", lateAddress, 0)
	stats := runtime.stats()
	if stats.activateCalls != 1 || !contains(stats.canceled, "reservation-late") {
		t.Fatalf("activate=%d canceled=%v", stats.activateCalls, stats.canceled)
	}
	_ = server.Close()
}

func TestAllocationCallbacksPreserveFIFOAcrossFiveTupleReplacement(t *testing.T) {
	runtime := &relayBarrierRuntime{event: validUsageEvent("event-old-five-tuple")}
	outbox := &relayCleanupOutbox{}
	source, destination := testAllocationAddresses()
	relayAddress, generator := testRelayGenerator(t)
	username := "username-reused-five-tuple"
	key := allocationKey(source, destination, "udp")
	oldConnection := &trackedPacketConn{ingress: 10, egress: 20}
	server := &Server{
		runtime: runtime, outbox: outbox, now: time.Now, closed: make(chan struct{}), generator: generator,
		pending: map[string]pendingReservation{
			reservationKey(username, source): {id: "reservation-reused-five-tuple", admission: policy.RelayAdmission{SessionID: "session"}},
		},
	}
	seedActiveAllocation(server, key, activeAllocation{id: runtime.event.GetAllocationId(), sessionID: runtime.event.GetSessionId(), conn: oldConnection})

	server.allocationCreated(source, destination, "udp", username, "", relayAddress, 0)
	server.mu.Lock()
	queue := append([]string(nil), server.callbackFIFO[key]...)
	active := len(server.active)
	var newConnection *trackedPacketConn
	if len(queue) == 2 {
		newConnection = server.active[queue[1]].conn
	}
	server.mu.Unlock()
	stats := runtime.stats()
	if len(queue) != 2 || queue[0] != runtime.event.GetAllocationId() || active != 2 || newConnection == nil || stats.activateCalls != 1 || len(stats.canceled) != 0 || server.Degraded() {
		t.Fatalf("queue=%v active=%d activate=%d canceled=%v degraded=%v", queue, active, stats.activateCalls, stats.canceled, server.Degraded())
	}
	defer func() { _ = newConnection.Close() }()

	server.allocationDeleted(source, destination, "udp", "", "")
	server.mu.Lock()
	remaining := len(server.active)
	remainingQueue := append([]string(nil), server.callbackFIFO[key]...)
	server.mu.Unlock()
	if remaining != 1 || len(remainingQueue) != 1 || remainingQueue[0] != queue[1] {
		t.Fatalf("remaining=%d queue=%v", remaining, remainingQueue)
	}
	server.allocationDeleted(source, destination, "udp", "", "")
	stats = runtime.stats()
	if stats.freezeCalls != 2 || stats.finalizeCalls != 2 || outbox.count() != 2 || !server.StateCloseSafe() {
		t.Fatalf("freeze=%d finalize=%d outbox=%d safe=%v", stats.freezeCalls, stats.finalizeCalls, outbox.count(), server.StateCloseSafe())
	}
}

func TestSessionSettlementKeepsOldDeleteAheadOfReplacement(t *testing.T) {
	runtime := &relayBarrierRuntime{event: validUsageEvent("event-session-old-five-tuple")}
	outbox := &barrierUsageOutbox{entered: make(chan struct{}), release: make(chan struct{})}
	source, destination := testAllocationAddresses()
	relayAddress, generator := testRelayGenerator(t)
	username := "username-session-replacement"
	key := allocationKey(source, destination, "udp")
	oldConnection := testTrackedPacketConn(t)
	server := &Server{
		runtime: runtime, outbox: outbox, now: time.Now, closed: make(chan struct{}), generator: generator,
		pending: map[string]pendingReservation{
			reservationKey(username, source): {id: "reservation-session-replacement", admission: policy.RelayAdmission{SessionID: "new-session"}},
		},
	}
	seedActiveAllocation(server, key, activeAllocation{id: runtime.event.GetAllocationId(), sessionID: "old-session", conn: oldConnection})
	settled := make(chan error, 1)
	go func() { settled <- server.CloseSessionAllocations(context.Background(), "old-session") }()
	<-outbox.entered

	server.allocationCreated(source, destination, "udp", username, "", relayAddress, 0)
	server.mu.Lock()
	queue := append([]string(nil), server.callbackFIFO[key]...)
	var newAllocation activeAllocation
	if len(queue) == 2 {
		newAllocation = server.active[queue[1]]
	}
	server.mu.Unlock()
	if len(queue) != 2 || queue[0] != runtime.event.GetAllocationId() || newAllocation.id != queue[1] {
		t.Fatalf("queue=%v new=%#v", queue, newAllocation)
	}
	defer func() { _ = newAllocation.conn.Close() }()
	close(outbox.release)
	if err := <-settled; err != nil {
		t.Fatal(err)
	}

	server.allocationDeleted(source, destination, "udp", "", "")
	if stats := runtime.stats(); stats.freezeCalls != 1 {
		t.Fatalf("old tombstone delete froze replacement: %v", stats.freezeIDs)
	}
	server.allocationDeleted(source, destination, "udp", "", "")
	stats := runtime.stats()
	if stats.freezeCalls != 2 || stats.finalizeCalls != 2 || !server.StateCloseSafe() {
		t.Fatalf("freeze=%v finalize=%v safe=%v", stats.freezeIDs, stats.finalizedIDs, server.StateCloseSafe())
	}
}

func TestFiveTupleReplacementFreezeFailureRetriesOldOnShutdown(t *testing.T) {
	runtime := &relayBarrierRuntime{event: validUsageEvent("event-old-retry-five-tuple"), freezeFailures: 1}
	outbox := &relayCleanupOutbox{}
	source, destination := testAllocationAddresses()
	relayAddress, generator := testRelayGenerator(t)
	username := "username-old-retry"
	key := allocationKey(source, destination, "udp")
	oldConnection := testTrackedPacketConn(t)
	server := &Server{
		runtime: runtime, outbox: outbox, now: time.Now, closed: make(chan struct{}), generator: generator,
		pending: map[string]pendingReservation{
			reservationKey(username, source): {id: "reservation-old-retry", admission: policy.RelayAdmission{SessionID: "new-session"}},
		},
	}
	seedActiveAllocation(server, key, activeAllocation{id: runtime.event.GetAllocationId(), sessionID: "old-session", conn: oldConnection})
	server.allocationCreated(source, destination, "udp", username, "", relayAddress, 0)
	server.mu.Lock()
	queue := append([]string(nil), server.callbackFIFO[key]...)
	var newConnection *trackedPacketConn
	if len(queue) == 2 {
		newConnection = server.active[queue[1]].conn
	}
	server.mu.Unlock()
	if len(queue) != 2 || newConnection == nil {
		t.Fatalf("replacement queue=%v connection=%v", queue, newConnection)
	}
	defer func() { _ = newConnection.Close() }()

	server.allocationDeleted(source, destination, "udp", "", "")
	server.allocationDeleted(source, destination, "udp", "", "")
	if err := server.Close(); err == nil {
		t.Fatal("degraded shutdown unexpectedly lost old Freeze failure")
	}
	stats := runtime.stats()
	wantFreeze := []string{queue[0], queue[1], queue[0]}
	wantFinalized := []string{queue[1], queue[0]}
	if !slices.Equal(stats.freezeIDs, wantFreeze) || !slices.Equal(stats.finalizedIDs, wantFinalized) || outbox.count() != 2 || !server.StateCloseSafe() {
		t.Fatalf("freeze=%v finalized=%v outbox=%d safe=%v", stats.freezeIDs, stats.finalizedIDs, outbox.count(), server.StateCloseSafe())
	}
}

func TestCloseSessionAllocationPutFailureIsFailClosed(t *testing.T) {
	runtime := &settlementRuntime{allocation: true, counters: 1, deltas: 1, event: validUsageEvent("event-failed")}
	connection := testTrackedPacketConn(t)
	server := &Server{
		runtime: runtime,
		outbox:  failingUsageOutbox{err: errors.New("disk full")},
		pending: make(map[string]pendingReservation),
		active: map[string]activeAllocation{
			runtime.event.GetAllocationId(): {id: runtime.event.GetAllocationId(), sessionID: runtime.event.GetSessionId(), conn: connection},
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
	server := &Server{runtime: runtime, outbox: failingUsageOutbox{err: errors.New("disk full")}, pending: make(map[string]pendingReservation)}
	seedActiveAllocation(server, allocationKey(source, destination, "udp"), activeAllocation{id: runtime.event.GetAllocationId(), sessionID: runtime.event.GetSessionId(), conn: &trackedPacketConn{ingress: 10, egress: 20}})
	server.allocationDeleted(source, destination, "udp", "", "")
	assertFailedSettlementIsFailClosed(t, server, runtime)
}

func assertFailedSettlementIsFailClosed(t *testing.T, server *Server, runtime *settlementRuntime) {
	t.Helper()
	server.mu.Lock()
	active := len(server.active)
	frozen := server.frozenPending[runtime.event.GetEventId()]
	server.mu.Unlock()
	if active != 0 || frozen == nil || !proto.Equal(frozen, runtime.event) {
		t.Fatalf("active=%d frozen=%v", active, frozen)
	}
	if server.StateCloseSafe() {
		t.Fatal("Runtime State was considered disposable after frozen usage missed the outbox")
	}
	if !server.Degraded() || server.reserve("new-user", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30000}) {
		t.Fatalf("Relay did not reject new work after settlement failure: degraded=%v", server.Degraded())
	}
	if !runtime.frozen || runtime.freezeCalls != 1 || !runtime.allocation || runtime.counters != 1 || runtime.deltas != 1 || runtime.finalizeCalls != 0 {
		t.Fatalf("frozen=%v freeze=%d allocation=%v counters=%d deltas=%d finalize=%d", runtime.frozen, runtime.freezeCalls, runtime.allocation, runtime.counters, runtime.deltas, runtime.finalizeCalls)
	}
}

func TestCanceledBeforeFreezeRetainsAllocationForRetry(t *testing.T) {
	runtime := newControlledSettlementRuntime("allocation-canceled")
	outbox := &relayCleanupOutbox{}
	server, key, connection := retryableAllocationServer(t, runtime, outbox, "allocation-canceled", "session")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := server.CloseSessionAllocations(ctx, "session"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled settlement error = %v", err)
	}
	assertRetryableAllocation(t, server, key, connection, 11, 22)
	if stats := runtime.stats(); stats.freezeSuccesses != 0 || stats.finalizeCalls != 0 || outbox.count() != 0 {
		t.Fatalf("successes=%d finalize=%d outbox=%d", stats.freezeSuccesses, stats.finalizeCalls, outbox.count())
	}

	if err := server.CloseSessionAllocations(context.Background(), "session"); err != nil {
		t.Fatal(err)
	}
	assertSettledExactlyOnce(t, server, runtime, outbox, 2, 1)
}

func TestQueuedFreezeCancellationReleasesClaim(t *testing.T) {
	runtime := newControlledSettlementRuntime("allocation-queued")
	runtime.setWaitForContext(true)
	runtime.freezeEntered = make(chan string, 1)
	outbox := &relayCleanupOutbox{}
	server, key, connection := retryableAllocationServer(t, runtime, outbox, "allocation-queued", "session")
	ctx, cancel := context.WithCancel(context.Background())
	settled := make(chan error, 1)
	go func() { settled <- server.CloseSessionAllocations(ctx, "session") }()
	if id := <-runtime.freezeEntered; id != "allocation-queued" {
		t.Fatalf("queued allocation = %q", id)
	}
	source, destination := testAllocationAddresses()
	server.allocationDeleted(source, destination, "udp", "", "")
	cancel()
	if err := <-settled; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued cancellation error = %v", err)
	}
	assertRetryableAllocation(t, server, key, connection, 11, 22)

	runtime.setWaitForContext(false)
	if err := server.CloseSessionAllocations(context.Background(), "session"); err != nil {
		t.Fatal(err)
	}
	assertSettledExactlyOnce(t, server, runtime, outbox, 2, 1)
}

func TestAllocationDeletedFreezeDeadlineRetainsAllocation(t *testing.T) {
	runtime := newControlledSettlementRuntime("allocation-deadline")
	runtime.setWaitForContext(true)
	outbox := &relayCleanupOutbox{}
	server, key, connection := retryableAllocationServer(t, runtime, outbox, "allocation-deadline", "session")
	server.settlementTimeout = time.Millisecond
	source, destination := testAllocationAddresses()

	server.allocationDeleted(source, destination, "udp", "", "")
	assertRetryableAllocation(t, server, key, connection, 11, 22)
	runtime.setWaitForContext(false)
	if err := server.CloseSessionAllocations(context.Background(), "session"); err != nil {
		t.Fatal(err)
	}
	assertSettledExactlyOnce(t, server, runtime, outbox, 2, 1)
}

func TestFreezeFailureThenRetryUsesSameAllocationAndBytes(t *testing.T) {
	runtime := newControlledSettlementRuntime("allocation-retry")
	runtime.freezeFailures = 1
	outbox := &relayCleanupOutbox{}
	server, key, connection := retryableAllocationServer(t, runtime, outbox, "allocation-retry", "session")
	source, destination := testAllocationAddresses()

	server.allocationDeleted(source, destination, "udp", "", "")
	assertRetryableAllocation(t, server, key, connection, 11, 22)
	if err := server.CloseSessionAllocations(context.Background(), "session"); err != nil {
		t.Fatal(err)
	}
	assertSettledExactlyOnce(t, server, runtime, outbox, 2, 1)
	stats := runtime.stats()
	if len(stats.freezeCalls) != 2 || stats.freezeCalls[0] != stats.freezeCalls[1] {
		t.Fatalf("freeze calls = %#v", stats.freezeCalls)
	}
}

func TestServerCloseRetriesRetainedAllocationAfterFreezeFailure(t *testing.T) {
	runtime := newControlledSettlementRuntime("allocation-close-retry")
	runtime.freezeFailures = 1
	outbox := &relayCleanupOutbox{}
	server, key, connection := retryableAllocationServer(t, runtime, outbox, "allocation-close-retry", "session")

	if err := server.Close(); err == nil {
		t.Fatal("first Close succeeded despite freeze failure")
	}
	assertRetryableAllocation(t, server, key, connection, 11, 22)
	if server.StateCloseSafe() {
		t.Fatal("State was disposable after shutdown freeze failure")
	}
	if err := server.Close(); err == nil {
		t.Fatal("degraded retry Close unexpectedly lost failure history")
	}
	assertSettledExactlyOnce(t, server, runtime, outbox, 2, 1)
}

func TestCloseSessionExpiredContextRetainsEveryAllocationForShutdown(t *testing.T) {
	const allocationCount = 3
	runtime := &controlledSettlementRuntime{events: make(map[string]*cloudv1.UsageEvent), waitForContext: true, freezeEntered: make(chan string, allocationCount)}
	outbox := &relayCleanupOutbox{}
	server := &Server{
		runtime: runtime, outbox: outbox, now: time.Now, closed: make(chan struct{}), settlementTimeout: 50 * time.Millisecond,
		pending: make(map[string]pendingReservation), active: make(map[string]activeAllocation),
	}
	connections := make(map[string]*trackedPacketConn, allocationCount)
	for index := 0; index < allocationCount; index++ {
		id := fmt.Sprintf("allocation-shared-%d", index)
		connection := testTrackedPacketConn(t)
		connection.ingress, connection.egress = uint64(10+index), uint64(20+index)
		server.active[id] = activeAllocation{id: id, key: fmt.Sprintf("key-%d", index), sessionID: "session", conn: connection}
		runtime.events[id] = usageEventForAllocation(id)
		connections[id] = connection
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := server.CloseSessionAllocations(ctx, "session"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shared deadline error = %v", err)
	}
	for key, connection := range connections {
		server.mu.Lock()
		allocation, exists := server.active[key]
		server.mu.Unlock()
		if !exists || allocation.settling || allocation.conn != connection {
			t.Fatalf("allocation %s retry owner = %#v", key, allocation)
		}
	}

	runtime.setWaitForContext(false)
	runtime.mu.Lock()
	runtime.freezeEntered = nil
	runtime.mu.Unlock()
	if err := server.Close(); err == nil {
		t.Fatal("degraded shutdown unexpectedly returned no failure history")
	}
	if !server.StateCloseSafe() {
		t.Fatal("shutdown local contexts did not freeze every retained allocation")
	}
	stats := runtime.stats()
	if stats.freezeSuccesses != allocationCount || stats.finalizeCalls != allocationCount || outbox.count() != allocationCount {
		t.Fatalf("successes=%d finalize=%d outbox=%d", stats.freezeSuccesses, stats.finalizeCalls, outbox.count())
	}
}

func TestConcurrentSettlementClaimsFreezePutAndFinalizeExactlyOnce(t *testing.T) {
	runtime := newControlledSettlementRuntime("allocation-exact-once")
	runtime.freezeEntered = make(chan string, 1)
	runtime.freezeRelease = make(chan struct{})
	outbox := &relayCleanupOutbox{}
	server, _, _ := retryableAllocationServer(t, runtime, outbox, "allocation-exact-once", "session")
	settled := make(chan error, 1)
	go func() { settled <- server.CloseSessionAllocations(context.Background(), "session") }()
	<-runtime.freezeEntered
	source, destination := testAllocationAddresses()
	server.allocationDeleted(source, destination, "udp", "", "")
	close(runtime.freezeRelease)
	if err := <-settled; err != nil {
		t.Fatal(err)
	}
	assertSettledExactlyOnce(t, server, runtime, outbox, 1, 1)
}

func TestSettleAllocationPersistsBeforeBlockedFinalize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	outbox, err := usage.Open(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &settlementRuntime{allocation: true, counters: 1, deltas: 1, event: validUsageEvent("event-blocked"), finalizeEntered: make(chan struct{}), finalizeRelease: make(chan struct{})}
	server := &Server{runtime: runtime, outbox: outbox, active: map[string]activeAllocation{
		runtime.event.GetAllocationId(): {id: runtime.event.GetAllocationId(), conn: &trackedPacketConn{ingress: 10, egress: 20}},
	}}
	claimed, exists := server.claimAllocation(runtime.event.GetAllocationId())
	if !exists {
		t.Fatal("active allocation was not claimable")
	}
	settled := make(chan error, 1)
	go func() {
		settled <- server.settleClaimedAllocation(context.Background(), claimed)
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

type scriptedUsageOutbox struct {
	mu           sync.Mutex
	failures     map[string]int
	calls        []*cloudv1.UsageEvent
	durable      map[string]*cloudv1.UsageEvent
	firstEntered chan struct{}
	firstRelease chan struct{}
}

func (outbox *scriptedUsageOutbox) Put(event *cloudv1.UsageEvent) error {
	event = proto.Clone(event).(*cloudv1.UsageEvent)
	outbox.mu.Lock()
	if outbox.durable == nil {
		outbox.durable = make(map[string]*cloudv1.UsageEvent)
	}
	outbox.calls = append(outbox.calls, event)
	callNumber := len(outbox.calls)
	remaining := outbox.failures[event.GetEventId()]
	shouldFail := remaining != 0
	if remaining > 0 {
		outbox.failures[event.GetEventId()]--
	}
	entered, release := outbox.firstEntered, outbox.firstRelease
	outbox.mu.Unlock()
	if callNumber == 1 && entered != nil {
		close(entered)
		<-release
	}
	if shouldFail {
		return errors.New("injected outbox failure")
	}
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	if existing := outbox.durable[event.GetEventId()]; existing != nil {
		if proto.Equal(existing, event) {
			return nil
		}
		return usage.ErrEventConflict
	}
	outbox.durable[event.GetEventId()] = proto.Clone(event).(*cloudv1.UsageEvent)
	return nil
}

func (outbox *scriptedUsageOutbox) stats() ([]*cloudv1.UsageEvent, map[string]*cloudv1.UsageEvent) {
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	calls := make([]*cloudv1.UsageEvent, 0, len(outbox.calls))
	for _, event := range outbox.calls {
		calls = append(calls, proto.Clone(event).(*cloudv1.UsageEvent))
	}
	durable := make(map[string]*cloudv1.UsageEvent, len(outbox.durable))
	for eventID, event := range outbox.durable {
		durable[eventID] = proto.Clone(event).(*cloudv1.UsageEvent)
	}
	return calls, durable
}

type settlementRuntime struct {
	frozen          bool
	freezeCalls     int
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
	runtime.freezeCalls++
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

type freezeCall struct {
	allocationID string
	ingress      uint64
	egress       uint64
}

type controlledSettlementRuntime struct {
	mu              sync.Mutex
	events          map[string]*cloudv1.UsageEvent
	waitForContext  bool
	freezeFailures  int
	freezeEntered   chan string
	freezeRelease   chan struct{}
	freezeCalls     []freezeCall
	freezeSuccesses int
	finalizeCalls   int
	finalizeErr     error
}

type controlledSettlementStats struct {
	freezeCalls     []freezeCall
	freezeSuccesses int
	finalizeCalls   int
}

func newControlledSettlementRuntime(allocationIDs ...string) *controlledSettlementRuntime {
	runtime := &controlledSettlementRuntime{events: make(map[string]*cloudv1.UsageEvent, len(allocationIDs))}
	for _, allocationID := range allocationIDs {
		runtime.events[allocationID] = usageEventForAllocation(allocationID)
	}
	return runtime
}

func (*controlledSettlementRuntime) RelayAuth(context.Context, string, time.Time) (*cloudv1.RelayLeaseClaims, string, bool, error) {
	return nil, "", false, nil
}

func (*controlledSettlementRuntime) ReserveRelayAllocation(context.Context, string, string, time.Time) (policy.RelayAdmission, error) {
	return policy.RelayAdmission{}, nil
}

func (*controlledSettlementRuntime) ActivateRelayAllocation(context.Context, string, string, cloudv1.RelayTransport, time.Time) error {
	return nil
}

func (*controlledSettlementRuntime) CancelRelayAllocationReservation(context.Context, string) error {
	return nil
}

func (runtime *controlledSettlementRuntime) FreezeRelayAllocationUsage(ctx context.Context, allocationID string, ingress, egress uint64) (*cloudv1.UsageEvent, error) {
	runtime.mu.Lock()
	runtime.freezeCalls = append(runtime.freezeCalls, freezeCall{allocationID: allocationID, ingress: ingress, egress: egress})
	callNumber := len(runtime.freezeCalls)
	waitForContext := runtime.waitForContext
	freezeFailures := runtime.freezeFailures
	entered := runtime.freezeEntered
	release := runtime.freezeRelease
	event := runtime.events[allocationID]
	runtime.mu.Unlock()
	if entered != nil {
		entered <- allocationID
	}
	if waitForContext {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if callNumber <= freezeFailures {
		return nil, errors.New("injected freeze failure")
	}
	if event == nil {
		return nil, errors.New("missing controlled usage event")
	}
	runtime.mu.Lock()
	runtime.freezeSuccesses++
	runtime.mu.Unlock()
	return event, nil
}

func (runtime *controlledSettlementRuntime) FinalizeRelayAllocation(context.Context, *cloudv1.UsageEvent) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.finalizeCalls++
	return runtime.finalizeErr
}

func (runtime *controlledSettlementRuntime) setWaitForContext(wait bool) {
	runtime.mu.Lock()
	runtime.waitForContext = wait
	runtime.mu.Unlock()
}

func (runtime *controlledSettlementRuntime) stats() controlledSettlementStats {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return controlledSettlementStats{
		freezeCalls:     append([]freezeCall(nil), runtime.freezeCalls...),
		freezeSuccesses: runtime.freezeSuccesses,
		finalizeCalls:   runtime.finalizeCalls,
	}
}

func usageEventForAllocation(allocationID string) *cloudv1.UsageEvent {
	event := validUsageEvent("event-" + allocationID)
	event.AllocationId = allocationID
	return event
}

func seedActiveAllocation(server *Server, key string, allocation activeAllocation) {
	if server.active == nil {
		server.active = make(map[string]activeAllocation)
	}
	if server.callbackFIFO == nil {
		server.callbackFIFO = make(map[string][]string)
	}
	allocation.key = key
	server.active[allocation.id] = allocation
	server.callbackFIFO[key] = append(server.callbackFIFO[key], allocation.id)
}

func retryableAllocationServer(t *testing.T, runtime Runtime, outbox UsageOutbox, allocationID, sessionID string) (*Server, string, *trackedPacketConn) {
	t.Helper()
	source, destination := testAllocationAddresses()
	key := allocationKey(source, destination, "udp")
	connection := testTrackedPacketConn(t)
	connection.ingress, connection.egress = 11, 22
	server := &Server{
		runtime: runtime, outbox: outbox, now: time.Now, closed: make(chan struct{}), pending: make(map[string]pendingReservation),
		active:       map[string]activeAllocation{allocationID: {id: allocationID, key: key, sessionID: sessionID, conn: connection}},
		callbackFIFO: map[string][]string{key: {allocationID}},
	}
	return server, allocationID, connection
}

func assertRetryableAllocation(t *testing.T, server *Server, key string, connection *trackedPacketConn, ingress, egress uint64) {
	t.Helper()
	server.mu.Lock()
	allocation, exists := server.active[key]
	server.mu.Unlock()
	actualIngress, actualEgress := connection.counts()
	if !exists || allocation.settling || allocation.conn != connection || actualIngress != ingress || actualEgress != egress {
		t.Fatalf("retry owner=%#v exists=%v bytes=%d/%d", allocation, exists, actualIngress, actualEgress)
	}
}

func assertFrozenPending(t *testing.T, server *Server, eventID string, expected *cloudv1.UsageEvent) {
	t.Helper()
	server.mu.Lock()
	event := server.frozenPending[eventID]
	active := len(server.active)
	pending := len(server.frozenPending)
	server.mu.Unlock()
	if active != 0 || pending != 1 || event == nil || !proto.Equal(event, expected) {
		t.Fatalf("active=%d pending=%d event=%v", active, pending, event)
	}
}

func frozenPendingCount(server *Server) int {
	server.mu.Lock()
	defer server.mu.Unlock()
	return len(server.frozenPending)
}

func assertFrozenRetryCompleted(t *testing.T, server *Server, runtime *controlledSettlementRuntime, outbox *scriptedUsageOutbox, eventID string, putCalls int) {
	t.Helper()
	calls, durable := outbox.stats()
	stats := runtime.stats()
	for _, event := range calls {
		if durableEvent := durable[eventID]; event.GetEventId() != eventID || durableEvent == nil || !proto.Equal(event, durableEvent) {
			t.Fatalf("Put payload %v did not match durable event %v", event, durableEvent)
		}
	}
	if len(calls) != putCalls || len(durable) != 1 || len(stats.freezeCalls) != 1 || stats.freezeSuccesses != 1 || stats.finalizeCalls != 1 || frozenPendingCount(server) != 0 || !server.StateCloseSafe() {
		t.Fatalf("puts=%d durable=%d freeze=%d successes=%d finalize=%d pending=%d safe=%v", len(calls), len(durable), len(stats.freezeCalls), stats.freezeSuccesses, stats.finalizeCalls, frozenPendingCount(server), server.StateCloseSafe())
	}
}

func assertSettledExactlyOnce(t *testing.T, server *Server, runtime *controlledSettlementRuntime, outbox *relayCleanupOutbox, freezeAttempts, freezeSuccesses int) {
	t.Helper()
	server.mu.Lock()
	active := len(server.active)
	server.mu.Unlock()
	stats := runtime.stats()
	if active != 0 || !server.StateCloseSafe() || len(stats.freezeCalls) != freezeAttempts || stats.freezeSuccesses != freezeSuccesses || stats.finalizeCalls != 1 || outbox.count() != 1 {
		t.Fatalf("active=%d safe=%v attempts=%d successes=%d finalize=%d outbox=%d", active, server.StateCloseSafe(), len(stats.freezeCalls), stats.freezeSuccesses, stats.finalizeCalls, outbox.count())
	}
}

func validUsageEvent(eventID string) *cloudv1.UsageEvent {
	started := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	return &cloudv1.UsageEvent{
		SchemaVersion: 1, EventId: eventID, EdgeId: "edge", LeaseId: "lease", AccountId: "account", DaemonId: "daemon", ClientId: "client", SessionId: "session",
		AllocationId: "allocation-" + eventID, Transport: cloudv1.RelayTransport_RELAY_TRANSPORT_UDP, IngressBytes: 10, EgressBytes: 20,
		StartedAt: timestamppb.New(started), EndedAt: timestamppb.New(started.Add(time.Second)),
	}
}

type relayBarrierRuntime struct {
	mu              sync.Mutex
	event           *cloudv1.UsageEvent
	reserveEntered  chan struct{}
	reserveRelease  chan struct{}
	activateEntered chan struct{}
	activateRelease chan struct{}
	reserveCalls    int
	activateCalls   int
	freezeCalls     int
	freezeFailures  int
	finalizeCalls   int
	freezeIDs       []string
	finalizedIDs    []string
	canceled        []string
}

type relayBarrierStats struct {
	reserveCalls, activateCalls, freezeCalls, finalizeCalls int
	canceled                                                []string
	freezeIDs, finalizedIDs                                 []string
}

func (*relayBarrierRuntime) RelayAuth(context.Context, string, time.Time) (*cloudv1.RelayLeaseClaims, string, bool, error) {
	return nil, "", false, nil
}

func (runtime *relayBarrierRuntime) ReserveRelayAllocation(ctx context.Context, _, _ string, _ time.Time) (policy.RelayAdmission, error) {
	runtime.mu.Lock()
	runtime.reserveCalls++
	runtime.mu.Unlock()
	if runtime.reserveEntered != nil {
		close(runtime.reserveEntered)
		select {
		case <-runtime.reserveRelease:
		case <-ctx.Done():
			return policy.RelayAdmission{}, ctx.Err()
		}
	}
	return policy.RelayAdmission{SessionID: "session"}, nil
}

func (runtime *relayBarrierRuntime) ActivateRelayAllocation(ctx context.Context, _, _ string, _ cloudv1.RelayTransport, _ time.Time) error {
	runtime.mu.Lock()
	runtime.activateCalls++
	runtime.mu.Unlock()
	if runtime.activateEntered != nil {
		close(runtime.activateEntered)
		select {
		case <-runtime.activateRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (runtime *relayBarrierRuntime) CancelRelayAllocationReservation(_ context.Context, reservationID string) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.canceled = append(runtime.canceled, reservationID)
	return nil
}

func (runtime *relayBarrierRuntime) FreezeRelayAllocationUsage(_ context.Context, allocationID string, _, _ uint64) (*cloudv1.UsageEvent, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.freezeCalls++
	runtime.freezeIDs = append(runtime.freezeIDs, allocationID)
	if runtime.freezeCalls <= runtime.freezeFailures {
		return nil, errors.New("injected freeze failure")
	}
	event := proto.Clone(runtime.event).(*cloudv1.UsageEvent)
	event.EventId = "event-" + allocationID
	event.AllocationId = allocationID
	return event, nil
}

func (runtime *relayBarrierRuntime) FinalizeRelayAllocation(_ context.Context, event *cloudv1.UsageEvent) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.finalizeCalls++
	runtime.finalizedIDs = append(runtime.finalizedIDs, event.GetAllocationId())
	return nil
}

func (runtime *relayBarrierRuntime) stats() relayBarrierStats {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return relayBarrierStats{
		reserveCalls: runtime.reserveCalls, activateCalls: runtime.activateCalls, freezeCalls: runtime.freezeCalls, finalizeCalls: runtime.finalizeCalls,
		canceled: append([]string(nil), runtime.canceled...), freezeIDs: append([]string(nil), runtime.freezeIDs...), finalizedIDs: append([]string(nil), runtime.finalizedIDs...),
	}
}

type barrierUsageOutbox struct {
	entered     chan struct{}
	release     chan struct{}
	err         error
	enteredOnce sync.Once
}

func (outbox *barrierUsageOutbox) Put(*cloudv1.UsageEvent) error {
	outbox.enteredOnce.Do(func() { close(outbox.entered) })
	<-outbox.release
	return outbox.err
}

func testAllocationAddresses() (*net.UDPAddr, *net.UDPAddr) {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 10000}, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 20000}
}

func testRelayGenerator(t *testing.T) (net.Addr, *trackedGenerator) {
	t.Helper()
	connection := testTrackedPacketConn(t)
	address := connection.LocalAddr()
	return address, &trackedGenerator{conns: map[string]*trackedPacketConn{address.String(): connection}}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func waitForServerClosing(t *testing.T, server *Server) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		server.mu.Lock()
		closing := server.closing
		server.mu.Unlock()
		if closing {
			return
		}
		select {
		case <-timer.C:
			t.Fatal("timed out waiting for Relay server to enter closing")
		default:
		}
		goruntime.Gosched()
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
