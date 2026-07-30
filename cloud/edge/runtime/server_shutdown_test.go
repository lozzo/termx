package runtime

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/anytty/anytty/cloud/processhealth"
	"google.golang.org/grpc"
	grpc_health "google.golang.org/grpc/health"
)

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
	if err := <-first; err == nil {
		t.Fatal("Shutdown succeeded with unfrozen Relay usage")
	}
	if _, err := state.Snapshot(context.Background()); err != nil {
		t.Fatalf("State closed after failed Relay freeze: %v", err)
	}
	if !health.Alive() {
		t.Fatal("Runtime was marked dead while State retained unfrozen usage")
	}

	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("retry Shutdown after successful Relay freeze: %v", err)
	}
	if _, err := state.Snapshot(context.Background()); !errors.Is(err, ErrStateClosed) {
		t.Fatalf("State remained open after Relay usage froze: %v", err)
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

func (relay *shutdownRelay) Close() error {
	relay.mu.Lock()
	relay.closeN++
	call := relay.closeN
	relay.mu.Unlock()
	if call == 1 {
		close(relay.entered)
		<-relay.release
		return errors.New("injected Relay freeze failure")
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
