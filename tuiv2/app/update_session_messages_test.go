package app

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/workbenchdoc"
)

func TestHandleSessionMessageSnapshotAppliesStateAndError(t *testing.T) {
	model := setupModel(t, modelOpts{})
	snapshot := &protocol.SessionSnapshot{
		Session: protocol.SessionInfo{ID: "session-main", Revision: 7},
		View:    &protocol.ViewInfo{ViewID: "view-1"},
		Workbench: &workbenchdoc.Doc{
			CurrentWorkspace: "main",
			WorkspaceOrder:   []string{"main"},
			Workspaces:       map[string]*workbenchdoc.Workspace{"main": {Name: "main"}},
		},
	}

	cmd, handled := model.handleSessionMessage(sessionSnapshotMsg{Snapshot: snapshot, Err: errors.New("snapshot failed")})
	if !handled || cmd == nil {
		t.Fatalf("expected sessionSnapshotMsg handled with error cmd, got handled=%v cmd=%#v", handled, cmd)
	}
	if model.sessionID != "session-main" || model.sessionViewID != "view-1" || model.sessionRevision != 7 {
		t.Fatalf("expected snapshot applied before surfacing error, got sessionID=%q viewID=%q revision=%d", model.sessionID, model.sessionViewID, model.sessionRevision)
	}
	if msg := cmd(); msg == nil {
		t.Fatal("expected showError follow-up message")
	}
}

func TestHandleSessionMessageSessionUpdatePullsWhenViewChanges(t *testing.T) {
	client := &recordingBridgeClient{
		sessionSnapshot: &protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session-main", Revision: 9}},
	}
	model := setupModel(t, modelOpts{client: client})
	model.sessionID = "session-main"
	model.sessionViewID = "view-local"
	model.sessionRevision = 3

	cmd, handled := model.handleSessionMessage(sessionEventMsg{
		Event: protocol.Event{
			Type:      protocol.EventSessionUpdated,
			SessionID: "session-main",
			Session:   &protocol.SessionEventData{Revision: 4, ViewID: "view-remote"},
		},
	})
	if !handled || cmd == nil {
		t.Fatalf("expected session update to trigger pull cmd, got handled=%v cmd=%#v", handled, cmd)
	}
	msg := cmd()
	snapshotMsg, ok := msg.(sessionSnapshotMsg)
	if !ok {
		t.Fatalf("expected sessionSnapshotMsg from pull command, got %#v", msg)
	}
	if snapshotMsg.Snapshot == nil || snapshotMsg.Snapshot.Session.ID != "session-main" {
		t.Fatalf("expected pulled session snapshot, got %#v", snapshotMsg)
	}
}

func TestHandleSessionMessageSessionDeleteShowsError(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.sessionID = "session-main"

	cmd, handled := model.handleSessionMessage(sessionEventMsg{
		Event: protocol.Event{Type: protocol.EventSessionDeleted, SessionID: "session-main"},
	})
	if !handled || cmd == nil {
		t.Fatalf("expected session delete to return error cmd, got handled=%v cmd=%#v", handled, cmd)
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("expected showError follow-up")
	}
}

func TestHandleSessionMessageSessionViewUpdatedAppliesViewIDAndError(t *testing.T) {
	model := setupModel(t, modelOpts{})

	cmd, handled := model.handleSessionMessage(sessionViewUpdatedMsg{
		View: &protocol.ViewInfo{ViewID: "view-next"},
		Err:  errors.New("view update failed"),
	})
	if !handled || cmd == nil {
		t.Fatalf("expected sessionViewUpdatedMsg handled with error cmd, got handled=%v cmd=%#v", handled, cmd)
	}
	if model.sessionViewID != "view-next" {
		t.Fatalf("expected session view id updated, got %q", model.sessionViewID)
	}
	if msg := cmd(); msg == nil {
		t.Fatal("expected showError follow-up")
	}
}

func TestHandleSessionMessageUnknownMessageFallsThrough(t *testing.T) {
	model := setupModel(t, modelOpts{})
	cmd, handled := model.handleSessionMessage(tea.WindowSizeMsg{Width: 80, Height: 24})
	if handled || cmd != nil {
		t.Fatalf("expected unrelated msg to fall through, got handled=%v cmd=%#v", handled, cmd)
	}
}
