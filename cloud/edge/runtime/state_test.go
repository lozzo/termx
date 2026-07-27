package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	edgeruntime "github.com/anytty/anytty/cloud/edge/runtime"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestStateFeedIsAtomicAndRejectsStaleGeneration(t *testing.T) {
	state := newState(t)
	if err := state.UpsertAgent(context.Background(), stateAgent("daemon-1", 2)); err != nil {
		t.Fatalf("upsert initial agent: %v", err)
	}
	feed, err := state.OpenFeed(context.Background())
	if err != nil {
		t.Fatalf("open feed: %v", err)
	}
	defer feed.Close()
	if feed.Snapshot.GetRevision() != 1 || len(feed.Snapshot.GetAgents()) != 1 {
		t.Fatalf("initial feed snapshot = %+v", feed.Snapshot)
	}
	if err := state.UpsertAgent(context.Background(), stateAgent("daemon-1", 1)); !errors.Is(err, edgeruntime.ErrStaleGeneration) {
		t.Fatalf("stale generation error = %v", err)
	}
	if err := state.UpsertSession(context.Background(), stateSession("session-1", 1)); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	select {
	case delta := <-feed.Deltas:
		if delta.GetRevision() != 2 || delta.GetSessionUpserted().GetSessionId() != "session-1" {
			t.Fatalf("feed delta = %+v", delta)
		}
	case <-time.After(time.Second):
		t.Fatal("feed did not receive delta")
	}
}

func TestStateConcurrentWritersRemainLinearized(t *testing.T) {
	state := newState(t)
	const writers = 200
	var wait sync.WaitGroup
	errorsOut := make(chan error, writers)
	for index := 0; index < writers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			errorsOut <- state.UpsertAgent(context.Background(), stateAgent(fmt.Sprintf("daemon-%03d", index), 1))
		}(index)
	}
	wait.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if err != nil {
			t.Fatalf("concurrent upsert: %v", err)
		}
	}
	snapshot, err := state.Snapshot(context.Background())
	if err != nil || snapshot.GetRevision() != writers || len(snapshot.GetAgents()) != writers {
		t.Fatalf("linearized snapshot revision=%d agents=%d err=%v", snapshot.GetRevision(), len(snapshot.GetAgents()), err)
	}
}

func newState(t *testing.T) *edgeruntime.State {
	t.Helper()
	state, err := edgeruntime.NewState(edgeruntime.StateConfig{MailboxSize: 256, DeltaBuffer: 256})
	if err != nil {
		t.Fatalf("create state: %v", err)
	}
	t.Cleanup(state.Close)
	return state
}

func stateAgent(id string, generation uint64) *cloudv1.AgentPresence {
	return &cloudv1.AgentPresence{DaemonId: id, AccountId: "account-1", BootId: "boot", ConnectionId: "connection", Generation: generation, TicketId: "ticket", TicketIssuedAt: timestamppb.New(time.Unix(1, 0))}
}

func stateSession(id string, generation uint64) *cloudv1.ClientSessionSummary {
	return &cloudv1.ClientSessionSummary{SessionId: id, AccountId: "account-1", DaemonId: "daemon-1", ClientId: "client-1", Product: cloudv1.ClientProduct_CLIENT_PRODUCT_TUI, Generation: generation}
}
