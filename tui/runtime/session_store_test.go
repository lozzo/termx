package runtime

import (
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/core/types"
)

func TestSessionStoreTracksAttachedSession(t *testing.T) {
	store := NewSessionStore()
	snapshot := &protocol.Snapshot{
		TerminalID: "term-1",
		Timestamp:  time.Unix(10, 0),
	}

	store.Upsert(Session{
		TerminalID: types.TerminalID("term-1"),
		Channel:    7,
		Snapshot:   snapshot,
	})

	session, ok := store.Session(types.TerminalID("term-1"))
	if !ok {
		t.Fatal("expected session to be stored")
	}
	if session.Channel != 7 {
		t.Fatalf("expected channel 7, got %d", session.Channel)
	}
	if session.Snapshot == nil || session.Snapshot.TerminalID != "term-1" {
		t.Fatalf("expected session snapshot for term-1, got %#v", session.Snapshot)
	}
}

func TestSessionStoreApplySnapshotReplacesPreviousSnapshot(t *testing.T) {
	store := NewSessionStore()
	store.Upsert(Session{TerminalID: types.TerminalID("term-1"), Channel: 7})

	store.ApplySnapshot(types.TerminalID("term-1"), &protocol.Snapshot{TerminalID: "term-1", Timestamp: time.Unix(20, 0)})

	session, ok := store.Session(types.TerminalID("term-1"))
	if !ok || session.Snapshot == nil || !session.Snapshot.Timestamp.Equal(time.Unix(20, 0)) {
		t.Fatalf("expected updated snapshot timestamp, got %#v", session)
	}
}
