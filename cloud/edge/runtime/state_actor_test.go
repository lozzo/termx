package runtime

import (
	"context"
	"errors"
	"fmt"
	goruntime "runtime"
	"sync/atomic"
	"testing"
	"time"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestStateCallCanceledWhileQueuedNeverExecutes(t *testing.T) {
	state := actorTestState(t, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	defer closeIfOpen(release)
	blockingDone := make(chan error, 1)
	go func() {
		blockingDone <- state.call(context.Background(), func(*stateData) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	var mutated atomic.Bool
	canceledDone := make(chan error, 1)
	go func() {
		canceledDone <- state.call(ctx, func(*stateData) error {
			mutated.Store(true)
			return nil
		})
	}()
	waitMailboxLength(t, state, 1)
	cancel()
	if err := <-canceledDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued call error = %v", err)
	}
	close(release)
	if err := <-blockingDone; err != nil {
		t.Fatal(err)
	}
	if err := state.call(context.Background(), func(*stateData) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if mutated.Load() {
		t.Fatal("canceled queued request executed")
	}
}

func TestStateCallCancellationAfterExecutionReturnsCommittedResult(t *testing.T) {
	state := actorTestState(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	defer closeIfOpen(release)
	result := make(chan error, 1)
	committed := errors.New("committed business result")
	go func() {
		result <- state.call(ctx, func(*stateData) error {
			close(started)
			<-release
			return committed
		})
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		t.Fatalf("executing call returned before commit: %v", err)
	default:
	}
	close(release)
	if err := <-result; !errors.Is(err, committed) {
		t.Fatalf("executing call returned %v", err)
	}
}

func TestStateCloseAcknowledgesAllAcceptedCalls(t *testing.T) {
	const accepted = 6
	state := actorTestState(t, accepted+1)
	started := make(chan struct{})
	release := make(chan struct{})
	defer closeIfOpen(release)
	blockingDone := make(chan error, 1)
	go func() {
		blockingDone <- state.call(context.Background(), func(*stateData) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	var mutations atomic.Int32
	results := make(chan error, accepted)
	for range accepted {
		go func() {
			results <- state.call(context.Background(), func(*stateData) error {
				mutations.Add(1)
				return nil
			})
		}()
	}
	waitMailboxLength(t, state, accepted)
	closed := make(chan struct{})
	closeStarted := make(chan struct{})
	go func() {
		close(closeStarted)
		state.Close()
		close(closed)
	}()
	<-closeStarted
	waitForStateCloseGate(t, state)
	select {
	case <-closed:
		t.Fatal("Close returned while an accepted call was executing")
	default:
	}
	close(release)
	if err := <-blockingDone; err != nil {
		t.Fatal(err)
	}
	for range accepted {
		if err := <-results; err != nil {
			t.Fatalf("accepted call error = %v", err)
		}
	}
	<-closed
	if mutations.Load() != accepted {
		t.Fatalf("committed mutations = %d", mutations.Load())
	}
	if err := state.call(context.Background(), func(*stateData) error { return nil }); !errors.Is(err, ErrStateClosed) {
		t.Fatalf("post-close call error = %v", err)
	}
}

func TestStateCloseWaitsForAcceptedSendAction(t *testing.T) {
	state := actorTestState(t, 4)
	entered := make(chan struct{})
	release := make(chan struct{})
	defer closeIfOpen(release)
	if err := state.call(context.Background(), func(data *stateData) error {
		data.agentWriters["agent"] = agentWriter{generation: 1, send: func(*cloudv1.EdgeCommand) bool {
			close(entered)
			<-release
			return true
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sent := make(chan error, 1)
	go func() {
		sent <- state.SendAgentCommand(context.Background(), "agent", 1, &cloudv1.EdgeCommand{})
	}()
	<-entered
	closed := make(chan struct{})
	go func() {
		state.Close()
		close(closed)
	}()
	waitForStateCloseGate(t, state)
	select {
	case <-closed:
		t.Fatal("Close returned before accepted send action completed")
	default:
	}
	close(release)
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	<-closed
}

func TestStatePublicCloserIsConsumedOnceAndCloseWaits(t *testing.T) {
	state := actorTestState(t, 4)
	entered := make(chan struct{})
	release := make(chan struct{})
	defer closeIfOpen(release)
	var calls atomic.Int32
	if err := state.call(context.Background(), func(data *stateData) error {
		data.agentWriters["agent"] = agentWriter{generation: 1, close: func() {
			calls.Add(1)
			close(entered)
			<-release
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() { first <- state.CloseAgentConnection(context.Background(), "agent", 1) }()
	<-entered
	if err := state.CloseAgentConnection(context.Background(), "agent", 1); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("repeated close error = %v", err)
	}
	closed := make(chan struct{})
	go func() {
		state.Close()
		close(closed)
	}()
	waitForStateCloseGate(t, state)
	select {
	case <-closed:
		t.Fatal("Close returned before accepted closer completed")
	default:
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	<-closed
	if calls.Load() != 1 {
		t.Fatalf("closer calls = %d", calls.Load())
	}
}

func TestStateSessionCloserIsConsumedOnce(t *testing.T) {
	state := actorTestState(t, 4)
	var calls atomic.Int32
	if err := state.call(context.Background(), func(data *stateData) error {
		data.sessions["session"] = &cloudv1.ClientSessionSummary{SessionId: "session", Generation: 1}
		data.sessionClosers["session"] = sessionCloser{generation: 1, close: func() { calls.Add(1) }}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.CloseSession(context.Background(), "session", 1); err != nil {
		t.Fatal(err)
	}
	if err := state.CloseSession(context.Background(), "session", 1); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("repeated session close error = %v", err)
	}
	state.Close()
	if calls.Load() != 1 {
		t.Fatalf("session closer calls = %d", calls.Load())
	}
}

func TestStateCloseWaitsForAttachOldCloser(t *testing.T) {
	state := actorTestState(t, 4)
	entered := make(chan struct{})
	release := make(chan struct{})
	defer closeIfOpen(release)
	var oldCalls, newCalls atomic.Int32
	if err := state.call(context.Background(), func(data *stateData) error {
		data.agentWriters["agent"] = agentWriter{generation: 1, close: func() {
			oldCalls.Add(1)
			close(entered)
			<-release
		}}
		data.agentNextGen["agent"] = 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claims := &cloudv1.DaemonBindingClaims{
		BindingId: "binding", DaemonId: "agent", AccountId: "account", EdgeId: "edge", DeviceId: "device", DevicePublicKey: make([]byte, 32),
		Capabilities: []cloudv1.DaemonCapability{cloudv1.DaemonCapability_DAEMON_CAPABILITY_SIGNALING}, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Minute)),
	}
	agent := &cloudv1.AgentPresence{
		DaemonId: "agent", AccountId: "account", BootId: "boot", ConnectionId: "connection", BindingId: "binding", BindingIssuedAt: claims.GetIssuedAt(),
	}
	attached := make(chan error, 1)
	go func() {
		_, err := state.AttachAuthenticatedAgent(context.Background(), agent, claims, func(*cloudv1.EdgeCommand) bool { return true }, func() { newCalls.Add(1) })
		attached <- err
	}()
	<-entered
	closed := make(chan struct{})
	go func() {
		state.Close()
		close(closed)
	}()
	waitForStateCloseGate(t, state)
	select {
	case <-closed:
		t.Fatal("Close returned before replaced writer closer completed")
	default:
	}
	close(release)
	if err := <-attached; err != nil {
		t.Fatal(err)
	}
	<-closed
	if oldCalls.Load() != 1 || newCalls.Load() != 1 {
		t.Fatalf("old closer=%d new closer=%d", oldCalls.Load(), newCalls.Load())
	}
}

func TestStateCloseClosesOwnershipMatrixExactlyOnce(t *testing.T) {
	for _, agents := range []int{0, 1, 3} {
		for _, sessions := range []int{0, 1, 3} {
			t.Run(fmt.Sprintf("agents=%d/sessions=%d", agents, sessions), func(t *testing.T) {
				state := actorTestState(t, 4)
				agentCounts := make([]atomic.Int32, agents)
				sessionCounts := make([]atomic.Int32, sessions)
				pending := make(chan *cloudv1.AgentEvent)
				subscriber := make(chan *cloudv1.RuntimeDelta)
				if err := state.call(context.Background(), func(data *stateData) error {
					for index := range agents {
						counter := &agentCounts[index]
						data.agentWriters[fmt.Sprint(index)] = agentWriter{close: func() { counter.Add(1) }}
					}
					for index := range sessions {
						counter := &sessionCounts[index]
						data.sessionClosers[fmt.Sprint(index)] = sessionCloser{close: func() { counter.Add(1) }}
					}
					data.pendingSignals["pending"] = pendingSignal{result: pending}
					data.subscribers[1] = subscriber
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				state.Close()
				state.Close()
				for index := range agentCounts {
					if agentCounts[index].Load() != 1 {
						t.Fatalf("agent closer %d calls = %d", index, agentCounts[index].Load())
					}
				}
				for index := range sessionCounts {
					if sessionCounts[index].Load() != 1 {
						t.Fatalf("session closer %d calls = %d", index, sessionCounts[index].Load())
					}
				}
				assertClosed(t, pending, "pending signal")
				assertClosed(t, subscriber, "subscriber")
			})
		}
	}
}

func TestStateCloseJoinsOwnedClosers(t *testing.T) {
	state := actorTestState(t, 1)
	entered := make(chan struct{})
	release := make(chan struct{})
	if err := state.call(context.Background(), func(data *stateData) error {
		data.agentWriters["agent"] = agentWriter{close: func() {
			close(entered)
			<-release
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		state.Close()
		close(closed)
	}()
	<-entered
	select {
	case <-closed:
		t.Fatal("Close returned before owned closer completed")
	default:
	}
	close(release)
	<-closed
}

func actorTestState(t *testing.T, mailboxSize int) *State {
	t.Helper()
	state, err := NewState(StateConfig{MailboxSize: mailboxSize, DeltaBuffer: 8})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(state.Close)
	return state
}

func waitMailboxLength(t *testing.T, state *State, length int) {
	t.Helper()
	waitCondition(t, fmt.Sprintf("mailbox length to reach %d (got %d)", length, len(state.mailbox)), func() bool {
		return len(state.mailbox) == length
	})
}

func waitCondition(t *testing.T, description string, ready func() bool) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for !ready() {
		select {
		case <-timer.C:
			t.Fatalf("timed out waiting for %s", description)
		default:
		}
		goruntime.Gosched()
	}
}

func waitForStateCloseGate(t *testing.T, state *State) {
	t.Helper()
	waitCondition(t, "State.Close to wait on the lifecycle gate", func() bool {
		if state.gate.TryRLock() {
			state.gate.RUnlock()
			return false
		}
		return true
	})
}

func closeIfOpen(channel chan struct{}) {
	select {
	case <-channel:
	default:
		close(channel)
	}
}

func assertClosed[T any](t *testing.T, channel <-chan T, name string) {
	t.Helper()
	if _, ok := <-channel; ok {
		t.Fatalf("%s remained open", name)
	}
}
