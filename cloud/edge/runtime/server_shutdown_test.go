package runtime

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/processhealth"
	"google.golang.org/grpc"
	grpc_health "google.golang.org/grpc/health"
)

var errInjectedRelayFreeze = errors.New("injected Relay freeze failure")

func TestRuntimeShutdownConcurrentWaiterHonorsOwnDeadline(t *testing.T) {
	runtime := newShutdownRuntime(t)
	relay := &shutdownRelay{
		unfrozen: true,
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	runtime.relayServer = relay
	var releaseOnce sync.Once
	releaseRelay := func() { releaseOnce.Do(func() { close(relay.release) }) }
	t.Cleanup(releaseRelay)

	firstDone := make(chan error, 1)
	go func() { firstDone <- runtime.Shutdown(context.Background()) }()
	<-relay.entered

	waiterCtx, cancelWaiter := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelWaiter()
	waiterDone := make(chan error, 1)
	go func() { waiterDone <- runtime.Shutdown(waiterCtx) }()
	select {
	case err := <-waiterDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("concurrent Shutdown error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		releaseRelay()
		t.Fatal("concurrent Shutdown ignored its own deadline while waiting for the owner")
	}

	releaseRelay()
	if err := <-firstDone; !errors.Is(err, errInjectedRelayFreeze) {
		t.Fatalf("owner Shutdown error = %v", err)
	}
	if err := runtime.Shutdown(context.Background()); !errors.Is(err, errInjectedRelayFreeze) {
		t.Fatalf("retry Shutdown error = %v", err)
	}
	if !runtime.shutdownComplete {
		t.Fatal("owner retry did not complete the staged shutdown")
	}
}

func TestWaitRelayReplayReusesWaiterAcrossDeadlineRetries(t *testing.T) {
	runtime := &Runtime{}
	runtime.replayWait.Add(1)
	var releaseOnce sync.Once
	releaseReplay := func() { releaseOnce.Do(runtime.replayWait.Done) }
	t.Cleanup(releaseReplay)
	runtime.replayGate.Lock()
	runtime.replayClosing = true
	runtime.replayGate.Unlock()

	firstCtx, cancelFirst := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelFirst()
	if err := runtime.waitRelayReplay(firstCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first replay wait error = %v", err)
	}
	firstDone := runtime.replayDone
	if firstDone == nil {
		t.Fatal("first replay wait did not publish a completion channel")
	}

	secondCtx, cancelSecond := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelSecond()
	if err := runtime.waitRelayReplay(secondCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second replay wait error = %v", err)
	}
	if runtime.replayDone != firstDone {
		t.Fatal("deadline retry created a second replay waiter")
	}

	releaseReplay()
	finalCtx, cancelFinal := context.WithTimeout(context.Background(), time.Second)
	defer cancelFinal()
	if err := runtime.waitRelayReplay(finalCtx); err != nil {
		t.Fatalf("completed replay wait error = %v", err)
	}
	if runtime.replayDone != firstDone {
		t.Fatal("completed replay wait replaced the shared completion channel")
	}
}

func TestRuntimeShutdownRetainsStateUntilRelayUsageIsFrozen(t *testing.T) {
	state, err := NewState(StateConfig{MailboxSize: 8, DeltaBuffer: 8})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancelRun := context.WithCancel(context.Background())
	health := &processhealth.State{}
	health.SetAlive(true)
	relay := &shutdownRelay{
		unfrozen: true,
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	runtime := &Runtime{
		ctx: runCtx, cancel: cancelRun, readyChanges: make(chan struct{}, 1), state: state, health: health,
		grpcHealth: grpc_health.NewServer(), grpcServer: grpc.NewServer(), httpServer: &http.Server{}, relayServer: relay,
	}

	first := make(chan error, 1)
	go func() { first <- runtime.Shutdown(context.Background()) }()
	<-relay.entered
	if _, err := state.Snapshot(context.Background()); err != nil {
		t.Fatalf("State closed while Relay freeze was still executing: %v", err)
	}
	select {
	case err := <-first:
		t.Fatalf("Shutdown returned before Relay freeze completed: %v", err)
	default:
	}
	close(relay.release)
	if err := <-first; !errors.Is(err, errInjectedRelayFreeze) {
		t.Fatalf("Shutdown error = %v", err)
	}
	if _, err := state.Snapshot(context.Background()); err != nil {
		t.Fatalf("State closed after failed Relay freeze: %v", err)
	}
	if !health.Alive() {
		t.Fatal("Runtime was marked dead while State retained unfrozen usage")
	}

	if err := runtime.Shutdown(context.Background()); !errors.Is(err, errInjectedRelayFreeze) {
		t.Fatalf("retry Shutdown error = %v", err)
	}
	if _, err := state.Snapshot(context.Background()); !errors.Is(err, ErrStateClosed) {
		t.Fatalf("State remained open after Relay usage froze: %v", err)
	}
	if calls := relay.calls(); calls != 2 {
		t.Fatalf("Relay Close calls = %d", calls)
	}
}

func TestRuntimeShutdownStateOwnerDeadlineResumesWithoutRepeatingRelay(t *testing.T) {
	runtime := newShutdownRuntime(t)
	relay := &countingShutdownRelay{safe: true}
	runtime.relayServer = relay
	ownerStarted := make(chan struct{})
	releaseOwner := make(chan struct{})
	ownerDone := make(chan error, 1)
	go func() {
		ownerDone <- runtime.state.call(context.Background(), func(*stateData) error {
			close(ownerStarted)
			<-releaseOwner
			return nil
		})
	}()
	<-ownerStarted

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := runtime.Shutdown(shutdownCtx); !errors.Is(err, context.DeadlineExceeded) {
		close(releaseOwner)
		t.Fatalf("Shutdown error = %v", err)
	}
	if !runtime.teardownStarted || runtime.shutdownComplete || !runtime.health.Alive() {
		close(releaseOwner)
		t.Fatalf("deadline state: teardown=%v complete=%v alive=%v", runtime.teardownStarted, runtime.shutdownComplete, runtime.health.Alive())
	}
	close(releaseOwner)
	if err := <-ownerDone; err != nil {
		t.Fatal(err)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("retry Shutdown: %v", err)
	}
	if calls := relay.calls(); calls != 1 {
		t.Fatalf("Relay Close calls = %d", calls)
	}
}

func TestRuntimeShutdownWorkersDeadlineDoesNotFinalizeOrCacheContextError(t *testing.T) {
	runtime := newReservationRuntime(t, time.Date(2026, 7, 31, 2, 3, 4, 0, time.UTC))
	health := &processhealth.State{}
	health.SetAlive(true)
	persistentErr := errors.New("injected persistent Relay close error")
	relay := &countingShutdownRelay{safe: true, closeErr: persistentErr}
	runtime.health = health
	runtime.grpcHealth = grpc_health.NewServer()
	runtime.grpcServer = grpc.NewServer()
	runtime.httpServer = &http.Server{}
	runtime.relayServer = relay
	releaseWorker := make(chan struct{})
	runtime.waitGroup.Add(1)
	go func() {
		defer runtime.waitGroup.Done()
		<-releaseWorker
	}()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	firstErr := runtime.Shutdown(shutdownCtx)
	if !errors.Is(firstErr, context.DeadlineExceeded) || !errors.Is(firstErr, persistentErr) {
		close(releaseWorker)
		t.Fatalf("Shutdown error = %v", firstErr)
	}
	if runtime.shutdownComplete || !health.Alive() || runtime.relayJournal == nil {
		close(releaseWorker)
		t.Fatalf("deadline finalized Runtime: complete=%v alive=%v journal=%v", runtime.shutdownComplete, health.Alive(), runtime.relayJournal)
	}
	if depth, err := runtime.RelayJournalDepth(); err != nil || depth != 0 {
		close(releaseWorker)
		t.Fatalf("live journal depth=%d err=%v", depth, err)
	}

	close(releaseWorker)
	finalErr := runtime.Shutdown(context.Background())
	if !errors.Is(finalErr, persistentErr) || errors.Is(finalErr, context.DeadlineExceeded) || errors.Is(finalErr, context.Canceled) {
		t.Fatalf("retry Shutdown error = %v", finalErr)
	}
	if !runtime.shutdownComplete || health.Alive() || runtime.relayJournal != nil {
		t.Fatalf("retry did not finalize: complete=%v alive=%v journal=%v", runtime.shutdownComplete, health.Alive(), runtime.relayJournal)
	}
	if calls := relay.calls(); calls != 1 {
		t.Fatalf("Relay Close calls = %d", calls)
	}
}

func TestRuntimeShutdownRelayDeadlineKeepsRuntimeLiveForRetry(t *testing.T) {
	runtime := newReservationRuntime(t, time.Date(2026, 7, 31, 2, 3, 4, 0, time.UTC))
	health := &processhealth.State{}
	health.SetAlive(true)
	relay := &deadlineShutdownRelay{stopDone: make(chan struct{})}
	runtime.health = health
	runtime.grpcHealth = grpc_health.NewServer()
	runtime.grpcServer = grpc.NewServer()
	runtime.httpServer = &http.Server{}
	runtime.relayServer = relay

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := runtime.Shutdown(shutdownCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v", err)
	}
	if _, err := runtime.state.Snapshot(context.Background()); err != nil {
		t.Fatalf("State closed after Relay deadline: %v", err)
	}
	if !health.Alive() {
		t.Fatal("health was marked dead after Relay deadline")
	}
	if runtime.relayJournal == nil {
		t.Fatal("Relay journal was detached after Relay deadline")
	}
	if depth, err := runtime.RelayJournalDepth(); err != nil || depth != 0 {
		t.Fatalf("live Relay journal depth = %d, error = %v", depth, err)
	}
	if runtime.shutdownComplete {
		t.Fatal("deadline marked Runtime shutdown complete")
	}
	select {
	case <-runtime.ctx.Done():
		t.Fatal("Runtime background context was canceled after Relay deadline")
	default:
	}

	close(relay.stopDone)
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("retry Shutdown: %v", err)
	}
	if _, err := runtime.state.Snapshot(context.Background()); !errors.Is(err, ErrStateClosed) {
		t.Fatalf("State remained open after retry: %v", err)
	}
	if health.Alive() || !runtime.shutdownComplete || runtime.relayJournal != nil {
		t.Fatalf("retry completion: alive=%v complete=%v journal=%v", health.Alive(), runtime.shutdownComplete, runtime.relayJournal)
	}
	if calls := relay.calls(); calls != 2 {
		t.Fatalf("Relay Close calls = %d", calls)
	}
}

type shutdownRelay struct {
	mu       sync.Mutex
	unfrozen bool
	closeN   int
	entered  chan struct{}
	release  chan struct{}
}

func (*shutdownRelay) Address() string { return "" }

func (*shutdownRelay) Degraded() bool { return false }

func (*shutdownRelay) CloseSessionAllocations(context.Context, string) error { return nil }

func (relay *shutdownRelay) Close(context.Context) error {
	relay.mu.Lock()
	relay.closeN++
	call := relay.closeN
	relay.mu.Unlock()
	if call == 1 {
		close(relay.entered)
		<-relay.release
		return errInjectedRelayFreeze
	}
	relay.mu.Lock()
	relay.unfrozen = false
	relay.mu.Unlock()
	return nil
}

func (relay *shutdownRelay) StateCloseSafe() bool {
	relay.mu.Lock()
	defer relay.mu.Unlock()
	return !relay.unfrozen
}

func (relay *shutdownRelay) calls() int {
	relay.mu.Lock()
	defer relay.mu.Unlock()
	return relay.closeN
}

type deadlineShutdownRelay struct {
	mu       sync.Mutex
	closeN   int
	stopDone chan struct{}
	stopped  bool
}

func (*deadlineShutdownRelay) Address() string { return "" }

func (*deadlineShutdownRelay) Degraded() bool { return false }

func (*deadlineShutdownRelay) CloseSessionAllocations(context.Context, string) error { return nil }

func (relay *deadlineShutdownRelay) Close(ctx context.Context) error {
	relay.mu.Lock()
	relay.closeN++
	relay.mu.Unlock()
	select {
	case <-relay.stopDone:
		relay.mu.Lock()
		relay.stopped = true
		relay.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (relay *deadlineShutdownRelay) StateCloseSafe() bool {
	relay.mu.Lock()
	defer relay.mu.Unlock()
	return relay.stopped
}

func (relay *deadlineShutdownRelay) calls() int {
	relay.mu.Lock()
	defer relay.mu.Unlock()
	return relay.closeN
}

type countingShutdownRelay struct {
	mu       sync.Mutex
	closeN   int
	closeErr error
	safe     bool
}

func (*countingShutdownRelay) Address() string { return "" }

func (*countingShutdownRelay) Degraded() bool { return false }

func (*countingShutdownRelay) CloseSessionAllocations(context.Context, string) error { return nil }

func (relay *countingShutdownRelay) Close(context.Context) error {
	relay.mu.Lock()
	defer relay.mu.Unlock()
	relay.closeN++
	return relay.closeErr
}

func (relay *countingShutdownRelay) StateCloseSafe() bool {
	relay.mu.Lock()
	defer relay.mu.Unlock()
	return relay.safe
}

func (relay *countingShutdownRelay) calls() int {
	relay.mu.Lock()
	defer relay.mu.Unlock()
	return relay.closeN
}

func newShutdownRuntime(t *testing.T) *Runtime {
	t.Helper()
	state, err := NewState(StateConfig{MailboxSize: 8, DeltaBuffer: 8})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancelRun := context.WithCancel(context.Background())
	health := &processhealth.State{}
	health.SetAlive(true)
	runtime := &Runtime{
		ctx: runCtx, cancel: cancelRun, readyChanges: make(chan struct{}, 1), state: state, health: health,
		grpcHealth: grpc_health.NewServer(), grpcServer: grpc.NewServer(), httpServer: &http.Server{},
	}
	t.Cleanup(func() {
		cancelRun()
		runtime.grpcServer.Stop()
		state.Close()
	})
	return runtime
}
