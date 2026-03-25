package runtime

import (
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/state/types"
)

func TestSessionStoreTracksSnapshotAndStreamBinding(t *testing.T) {
	store := NewSessionStore()
	snapshot := &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 80, Rows: 24},
	}

	store.Bind(types.TerminalID("term-1"), 7, snapshot)
	session, ok := store.Session(types.TerminalID("term-1"))
	if !ok {
		t.Fatal("expected session to exist")
	}
	if session.Channel != 7 {
		t.Fatalf("expected channel 7, got %d", session.Channel)
	}
	if session.Snapshot == nil || session.Snapshot.TerminalID != "term-1" {
		t.Fatalf("expected snapshot for term-1, got %#v", session.Snapshot)
	}
}
