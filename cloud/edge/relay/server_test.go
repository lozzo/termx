package relay

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/edge/policy"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

func TestCloseSessionAllocationsStopsSocketBeforeRecordingCounters(t *testing.T) {
	runtime := &recordingRuntime{}
	firstSocket := &recordingPacketConn{events: &runtime.events, name: "first"}
	otherSocket := &recordingPacketConn{events: &runtime.events, name: "other"}
	server := &Server{
		runtime: runtime, now: time.Now, closed: make(chan struct{}), pending: make(map[string]pendingReservation), callbackFIFO: make(map[string][]string),
		active: map[string]activeAllocation{
			"allocation-first": {id: "allocation-first", sessionID: "session-first", conn: &trackedPacketConn{PacketConn: firstSocket, ingress: 7, egress: 9}},
			"allocation-other": {id: "allocation-other", sessionID: "session-other", conn: &trackedPacketConn{PacketConn: otherSocket}},
		},
	}
	if err := server.CloseSessionAllocations(context.Background(), "session-first"); err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	events := append([]string(nil), runtime.events...)
	runtime.mu.Unlock()
	want := []string{"begin:allocation-first", "socket-close:first", "counters:allocation-first:7:9"}
	if len(events) != len(want) {
		t.Fatalf("close events = %v", events)
	}
	for index := range want {
		if events[index] != want[index] {
			t.Fatalf("close events = %v, want %v", events, want)
		}
	}
	server.mu.Lock()
	_, firstExists := server.active["allocation-first"]
	_, otherExists := server.active["allocation-other"]
	server.mu.Unlock()
	if firstExists || !otherExists || otherSocket.closed {
		t.Fatal("session cleanup changed an allocation owned by another session")
	}
}

func TestCloseSessionAllocationsWaitsForAllPhysicalAllocations(t *testing.T) {
	runtime := &recordingRuntime{}
	server := &Server{runtime: runtime, now: time.Now, closed: make(chan struct{}), pending: make(map[string]pendingReservation), callbackFIFO: make(map[string][]string), active: make(map[string]activeAllocation)}
	for index := 0; index < 4; index++ {
		id := "allocation-" + string(rune('a'+index))
		server.active[id] = activeAllocation{id: id, sessionID: "session", conn: &trackedPacketConn{PacketConn: &recordingPacketConn{events: &runtime.events, name: id}, ingress: uint64(index + 1)}}
	}
	if err := server.CloseSessionAllocations(context.Background(), "session"); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	remaining := len(server.active)
	server.mu.Unlock()
	if remaining != 0 || runtime.closed != 4 {
		t.Fatalf("remaining=%d recorded=%d", remaining, runtime.closed)
	}
}

func TestCloseReturnsAtDeadlineWhileStopWaitsForWork(t *testing.T) {
	server := &Server{
		runtime: &recordingRuntime{}, now: time.Now, closed: make(chan struct{}),
		pending: make(map[string]pendingReservation), active: make(map[string]activeAllocation), callbackFIFO: make(map[string][]string),
	}
	server.work.Add(1)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := server.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		server.work.Done()
		t.Fatalf("Close error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		server.work.Done()
		t.Fatalf("Close returned after %v", elapsed)
	}
	if server.StateCloseSafe() {
		server.work.Done()
		t.Fatal("Relay became close-safe while stop still had work")
	}
	server.work.Done()
	retryCtx, retryCancel := context.WithTimeout(context.Background(), time.Second)
	defer retryCancel()
	if err := server.Close(retryCtx); err != nil {
		t.Fatalf("retry Close: %v", err)
	}
	if !server.StateCloseSafe() {
		t.Fatal("Relay did not become close-safe after work exited")
	}
}

func TestCloseRetainsFailedPendingReservationForRetry(t *testing.T) {
	cancelCalls := 0
	runtime := &recordingRuntime{cancelReservation: func(context.Context, string) error {
		cancelCalls++
		if cancelCalls == 1 {
			return errors.New("injected cancellation failure")
		}
		return nil
	}}
	server := &Server{
		runtime: runtime, now: time.Now, closed: make(chan struct{}),
		pending: map[string]pendingReservation{"reservation": {id: "reservation"}}, active: make(map[string]activeAllocation), callbackFIFO: make(map[string][]string),
	}
	if err := server.Close(context.Background()); err == nil {
		t.Fatal("Close succeeded after reservation cancellation failure")
	}
	server.mu.Lock()
	_, retained := server.pending["reservation"]
	server.mu.Unlock()
	if !retained || server.StateCloseSafe() {
		t.Fatal("failed reservation cancellation was not retained")
	}
	if err := server.Close(context.Background()); err != nil {
		t.Fatalf("retry Close: %v", err)
	}
	if cancelCalls != 2 || !server.StateCloseSafe() {
		t.Fatalf("cancel calls = %d, close safe = %v", cancelCalls, server.StateCloseSafe())
	}
}

func TestCloseSessionAllocationsRetainsFailedPendingReservation(t *testing.T) {
	cancelCalls := 0
	runtime := &recordingRuntime{cancelReservation: func(context.Context, string) error {
		cancelCalls++
		if cancelCalls == 1 {
			return errors.New("injected cancellation failure")
		}
		return nil
	}}
	server := &Server{
		runtime: runtime, now: time.Now, closed: make(chan struct{}),
		pending: map[string]pendingReservation{"reservation": {id: "reservation", admission: policy.RelayAdmission{SessionID: "session"}}},
		active:  make(map[string]activeAllocation), callbackFIFO: make(map[string][]string),
	}
	if err := server.CloseSessionAllocations(context.Background(), "session"); err == nil {
		t.Fatal("session cleanup succeeded after reservation cancellation failure")
	}
	server.mu.Lock()
	_, retained := server.pending["reservation"]
	server.mu.Unlock()
	if !retained {
		t.Fatal("session cleanup discarded the failed reservation cancellation")
	}
	if err := server.CloseSessionAllocations(context.Background(), "session"); err != nil {
		t.Fatalf("retry session cleanup: %v", err)
	}
	server.mu.Lock()
	remaining := len(server.pending)
	server.mu.Unlock()
	if cancelCalls != 2 || remaining != 0 {
		t.Fatalf("cancel calls = %d, pending = %d", cancelCalls, remaining)
	}
}

func TestCloseCancellationReleasesOnlyCurrentAllocationClaim(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	canceled := false
	runtime := &recordingRuntime{}
	runtime.beginClose = func(callCtx context.Context, id string) error {
		if id == "allocation-b" && !canceled {
			canceled = true
			cancel()
			return callCtx.Err()
		}
		return nil
	}
	server := &Server{
		runtime: runtime, now: time.Now, closed: make(chan struct{}), pending: make(map[string]pendingReservation), callbackFIFO: make(map[string][]string),
		active: map[string]activeAllocation{
			"allocation-c": {id: "allocation-c", conn: &trackedPacketConn{PacketConn: &recordingPacketConn{events: &runtime.events, name: "allocation-c"}}},
			"allocation-a": {id: "allocation-a", conn: &trackedPacketConn{PacketConn: &recordingPacketConn{events: &runtime.events, name: "allocation-a"}}},
			"allocation-b": {id: "allocation-b", conn: &trackedPacketConn{PacketConn: &recordingPacketConn{events: &runtime.events, name: "allocation-b"}}},
		},
	}
	if err := server.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close error = %v", err)
	}
	server.mu.Lock()
	_, firstRemains := server.active["allocation-a"]
	second := server.active["allocation-b"]
	third := server.active["allocation-c"]
	server.mu.Unlock()
	if firstRemains || second.settling || third.settling {
		t.Fatalf("active after cancellation: first=%v second settling=%v third settling=%v", firstRemains, second.settling, third.settling)
	}
	if err := server.Close(context.Background()); err != nil {
		t.Fatalf("retry Close: %v", err)
	}
	runtime.mu.Lock()
	events := append([]string(nil), runtime.events...)
	closed := runtime.closed
	runtime.mu.Unlock()
	want := []string{
		"begin:allocation-a", "socket-close:allocation-a", "counters:allocation-a:0:0",
		"begin:allocation-b",
		"begin:allocation-b", "socket-close:allocation-b", "counters:allocation-b:0:0",
		"begin:allocation-c", "socket-close:allocation-c", "counters:allocation-c:0:0",
	}
	if !slices.Equal(events, want) || closed != 3 || !server.StateCloseSafe() {
		t.Fatalf("events = %v, closed = %d, close safe = %v", events, closed, server.StateCloseSafe())
	}
}

func TestConcurrentCloseWaitsForDrainWithContext(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	runtime := &recordingRuntime{cancelReservation: func(context.Context, string) error {
		once.Do(func() {
			close(started)
			<-release
		})
		return nil
	}}
	server := &Server{
		runtime: runtime, now: time.Now, closed: make(chan struct{}),
		pending: map[string]pendingReservation{"reservation": {id: "reservation"}}, active: make(map[string]activeAllocation), callbackFIFO: make(map[string][]string),
	}
	first := make(chan error, 1)
	go func() { first <- server.Close(context.Background()) }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := server.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		close(release)
		t.Fatalf("concurrent Close error = %v", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first Close: %v", err)
	}
}

func TestTrackedPacketConnsShareReservationBudget(t *testing.T) {
	now := time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)
	limiter, err := policy.NewGroupLimiter(now.Add(time.Minute), 100, 1000, now)
	if err != nil {
		t.Fatal(err)
	}
	first := &trackedPacketConn{PacketConn: &recordingPacketConn{}, limiter: limiter}
	second := &trackedPacketConn{PacketConn: &recordingPacketConn{}, limiter: limiter}
	if !first.allow(70, true, now) || second.allow(31, true, now) || !second.allow(30, true, now) {
		t.Fatal("physical allocations did not share one reservation byte budget")
	}
	if ingress, _ := first.counts(); ingress != 70 {
		t.Fatalf("first ingress = %d", ingress)
	}
	if ingress, _ := second.counts(); ingress != 30 {
		t.Fatalf("second ingress = %d", ingress)
	}
}

type recordingRuntime struct {
	mu                 sync.Mutex
	events             []string
	closed             int
	cancelReservation  func(context.Context, string) error
	beginClose         func(context.Context, string) error
	completeAllocation func(context.Context, string, uint64, uint64) error
}

func (*recordingRuntime) RelayAuth(context.Context, string, time.Time) (*cloudv1.RelayGrant, string, bool, error) {
	return nil, "", false, nil
}
func (*recordingRuntime) ReserveRelayAllocation(context.Context, string, string, time.Time) (policy.RelayAdmission, error) {
	return policy.RelayAdmission{}, errors.New("unused")
}
func (*recordingRuntime) ActivateRelayAllocation(context.Context, string, string, cloudv1.RelayTransport, time.Time) error {
	return nil
}
func (runtime *recordingRuntime) CancelRelayAllocationReservation(ctx context.Context, id string) error {
	if runtime.cancelReservation != nil {
		return runtime.cancelReservation(ctx, id)
	}
	return nil
}
func (runtime *recordingRuntime) BeginRelayAllocationClose(ctx context.Context, id string) error {
	runtime.mu.Lock()
	runtime.events = append(runtime.events, "begin:"+id)
	runtime.mu.Unlock()
	if runtime.beginClose != nil {
		return runtime.beginClose(ctx, id)
	}
	return nil
}
func (runtime *recordingRuntime) CloseRelayAllocation(ctx context.Context, id string, ingress, egress uint64) error {
	runtime.mu.Lock()
	runtime.events = append(runtime.events, "counters:"+id+":"+itoa(ingress)+":"+itoa(egress))
	runtime.mu.Unlock()
	if runtime.completeAllocation != nil {
		if err := runtime.completeAllocation(ctx, id, ingress, egress); err != nil {
			return err
		}
	}
	runtime.mu.Lock()
	runtime.closed++
	runtime.mu.Unlock()
	return nil
}

type recordingPacketConn struct {
	events *[]string
	name   string
	closed bool
}

func (*recordingPacketConn) ReadFrom([]byte) (int, net.Addr, error) {
	return 0, nil, errors.New("unused")
}
func (*recordingPacketConn) WriteTo(payload []byte, _ net.Addr) (int, error) {
	return len(payload), nil
}
func (connection *recordingPacketConn) Close() error {
	connection.closed = true
	if connection.events != nil {
		*connection.events = append(*connection.events, "socket-close:"+connection.name)
	}
	return nil
}
func (*recordingPacketConn) LocalAddr() net.Addr              { return testAddress("local") }
func (*recordingPacketConn) SetDeadline(time.Time) error      { return nil }
func (*recordingPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (*recordingPacketConn) SetWriteDeadline(time.Time) error { return nil }

type testAddress string

func (address testAddress) Network() string { return "udp" }
func (address testAddress) String() string  { return string(address) }

func itoa(value uint64) string {
	if value == 0 {
		return "0"
	}
	buffer := [20]byte{}
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
