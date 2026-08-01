package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDaemonStateBlocksAndRestoresBusinessAfterAgentAck(t *testing.T) {
	state, err := NewState(StateConfig{MailboxSize: 32, DeltaBuffer: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	active := daemonStateRecord("daemon", cloudv1.DaemonState_DAEMON_STATE_ACTIVE, 1)
	if err := state.ApplyDaemonStateSnapshot(context.Background(), &cloudv1.DaemonStateSnapshot{Daemons: []*cloudv1.DaemonStateRecord{active}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claims := &cloudv1.DaemonBindingClaims{
		BindingId: "binding", DaemonId: "daemon", AccountId: "account", EdgeId: "edge", DeviceId: "device", DevicePublicKey: make([]byte, 32),
		IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Hour)),
	}
	presence := &cloudv1.AgentPresence{DaemonId: "daemon", AccountId: "account", BootId: "boot", ConnectionId: "connection", BindingId: "binding", BindingIssuedAt: claims.GetIssuedAt()}
	commands := make(chan *cloudv1.EdgeCommand, 4)
	generation, readyState, err := state.AttachAuthenticatedAgent(context.Background(), presence, claims, func(command *cloudv1.EdgeCommand) bool {
		commands <- command
		return true
	}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	assertRuntimeCounts(t, state, 0, 0)
	if err := state.ApplyDaemonLifecycleResult(context.Background(), "daemon", generation, &cloudv1.DaemonLifecycleResult{DaemonState: readyState, AgentGeneration: generation, Applied: true}); err != nil {
		t.Fatal(err)
	}
	var sessionClosed atomic.Bool
	if err := state.AttachSession(context.Background(), &cloudv1.ClientSessionSummary{SessionId: "session", AccountId: "account", DaemonId: "daemon", ClientId: "client", Product: cloudv1.ClientProduct_CLIENT_PRODUCT_TUI, Generation: 1}, func() { sessionClosed.Store(true) }); err != nil {
		t.Fatal(err)
	}

	blocked := daemonStateRecord("daemon", cloudv1.DaemonState_DAEMON_STATE_BLOCKED, 2)
	if err := state.ApplyDaemonStateDelta(context.Background(), &cloudv1.DaemonStateDelta{Daemon: blocked}); err != nil {
		t.Fatal(err)
	}
	if !sessionClosed.Load() {
		t.Fatal("blocked state did not close the current client session")
	}
	assertRuntimeCounts(t, state, 0, 0)
	if command := <-commands; command.GetLifecycle().GetDaemonState().GetState() != cloudv1.DaemonState_DAEMON_STATE_BLOCKED {
		t.Fatalf("blocked lifecycle command = %v", command)
	}

	restored := daemonStateRecord("daemon", cloudv1.DaemonState_DAEMON_STATE_ACTIVE, 3)
	if err := state.ApplyDaemonStateDelta(context.Background(), &cloudv1.DaemonStateDelta{Daemon: restored}); err != nil {
		t.Fatal(err)
	}
	assertRuntimeCounts(t, state, 0, 0)
	if command := <-commands; command.GetLifecycle().GetDaemonState().GetState() != cloudv1.DaemonState_DAEMON_STATE_ACTIVE {
		t.Fatalf("restore lifecycle command = %v", command)
	}
	if err := state.ApplyDaemonLifecycleResult(context.Background(), "daemon", generation, &cloudv1.DaemonLifecycleResult{DaemonState: restored, AgentGeneration: generation, Applied: true}); err != nil {
		t.Fatal(err)
	}
	assertRuntimeCounts(t, state, 1, 0)

	sessionClosed.Store(false)
	if err := state.AttachSession(context.Background(), &cloudv1.ClientSessionSummary{SessionId: "session-after-restore", AccountId: "account", DaemonId: "daemon", ClientId: "client", Product: cloudv1.ClientProduct_CLIENT_PRODUCT_TUI, Generation: 2}, func() { sessionClosed.Store(true) }); err != nil {
		t.Fatal(err)
	}
	grant, material := relayTestGrant(t, now, 1000)
	if err := state.RegisterRelayGrant(context.Background(), grant, material); err != nil {
		t.Fatal(err)
	}
	if err := state.DetachAgent(context.Background(), "daemon", generation); err != nil {
		t.Fatal(err)
	}
	if !sessionClosed.Load() {
		t.Fatal("Agent detach did not close the current client session")
	}
	assertRuntimeCounts(t, state, 0, 0)
	if err := state.call(context.Background(), func(data *stateData) error {
		group := data.relayGroups[grant.GetReservationId()]
		if group == nil || !group.closing || data.relayAuth[material.GetUsername()] != "" {
			t.Fatalf("detach Relay drain group=%+v auth=%q", group, data.relayAuth[material.GetUsername()])
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentReplacementDrainsOwnedSessionAndRelay(t *testing.T) {
	now := time.Now().UTC()
	state, err := NewState(StateConfig{MailboxSize: 32, DeltaBuffer: 32, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	active := daemonStateRecord("daemon", cloudv1.DaemonState_DAEMON_STATE_ACTIVE, 1)
	if err := state.ApplyDaemonStateSnapshot(context.Background(), &cloudv1.DaemonStateSnapshot{Daemons: []*cloudv1.DaemonStateRecord{active}}); err != nil {
		t.Fatal(err)
	}
	claims := &cloudv1.DaemonBindingClaims{
		BindingId: "binding", DaemonId: "daemon", AccountId: "account", EdgeId: "edge", DeviceId: "device", DevicePublicKey: make([]byte, 32),
		IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Hour)),
	}
	presence := &cloudv1.AgentPresence{DaemonId: "daemon", AccountId: "account", BootId: "boot", ConnectionId: "connection", BindingId: "binding", BindingIssuedAt: claims.GetIssuedAt()}
	var oldWriterClosed, oldSessionClosed atomic.Bool
	firstGeneration, readyState, err := state.AttachAuthenticatedAgent(context.Background(), presence, claims, func(*cloudv1.EdgeCommand) bool { return true }, func() { oldWriterClosed.Store(true) })
	if err != nil {
		t.Fatal(err)
	}
	if err := state.ApplyDaemonLifecycleResult(context.Background(), "daemon", firstGeneration, &cloudv1.DaemonLifecycleResult{DaemonState: readyState, AgentGeneration: firstGeneration, Applied: true}); err != nil {
		t.Fatal(err)
	}
	if err := state.AttachSession(context.Background(), &cloudv1.ClientSessionSummary{SessionId: "session", AccountId: "account", DaemonId: "daemon", ClientId: "client", Product: cloudv1.ClientProduct_CLIENT_PRODUCT_TUI, Generation: 1}, func() { oldSessionClosed.Store(true) }); err != nil {
		t.Fatal(err)
	}
	grant, material := relayTestGrant(t, now, 1000)
	if err := state.RegisterRelayGrant(context.Background(), grant, material); err != nil {
		t.Fatal(err)
	}

	replacement := &cloudv1.AgentPresence{DaemonId: "daemon", AccountId: "account", BootId: "boot-2", ConnectionId: "connection-2", BindingId: "binding", BindingIssuedAt: claims.GetIssuedAt()}
	secondGeneration, secondState, err := state.AttachAuthenticatedAgent(context.Background(), replacement, claims, func(*cloudv1.EdgeCommand) bool { return true }, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if !oldWriterClosed.Load() || !oldSessionClosed.Load() {
		t.Fatalf("replacement drain writer=%v session=%v", oldWriterClosed.Load(), oldSessionClosed.Load())
	}
	assertRuntimeCounts(t, state, 0, 0)
	if err := state.call(context.Background(), func(data *stateData) error {
		group := data.relayGroups[grant.GetReservationId()]
		if group == nil || !group.closing || data.relayAuth[material.GetUsername()] != "" {
			t.Fatalf("replacement Relay drain group=%+v auth=%q", group, data.relayAuth[material.GetUsername()])
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := state.ApplyDaemonLifecycleResult(context.Background(), "daemon", secondGeneration, &cloudv1.DaemonLifecycleResult{DaemonState: secondState, AgentGeneration: secondGeneration, Applied: true}); err != nil {
		t.Fatal(err)
	}
	var replacementSessionClosed atomic.Bool
	if err := state.AttachSession(context.Background(), &cloudv1.ClientSessionSummary{SessionId: "replacement-session", AccountId: "account", DaemonId: "daemon", ClientId: "client", Product: cloudv1.ClientProduct_CLIENT_PRODUCT_TUI, Generation: 2}, func() { replacementSessionClosed.Store(true) }); err != nil {
		t.Fatal(err)
	}
	if err := state.DetachAgent(context.Background(), "daemon", firstGeneration); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("stale detach error = %v", err)
	}
	if replacementSessionClosed.Load() {
		t.Fatal("stale detach drained the replacement generation session")
	}
}

func assertRuntimeCounts(t *testing.T, state *State, agents, sessions int) {
	t.Helper()
	snapshot, err := state.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.GetAgents()) != agents || len(snapshot.GetSessions()) != sessions {
		t.Fatalf("runtime counts agents=%d sessions=%d", len(snapshot.GetAgents()), len(snapshot.GetSessions()))
	}
}

func daemonStateRecord(daemonID string, state cloudv1.DaemonState, revision uint64) *cloudv1.DaemonStateRecord {
	return &cloudv1.DaemonStateRecord{DaemonId: daemonID, State: state, StateRevision: revision}
}
