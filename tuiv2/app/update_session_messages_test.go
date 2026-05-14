package app

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/sessiondoc"
	"github.com/lozzow/termx/tuiv2/sessionstate"
	"github.com/lozzow/termx/tuiv2/sessionstore"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestHandleSessionMessageSnapshotAppliesStateAndError(t *testing.T) {
	model := setupModel(t, modelOpts{})
	snapshot := &sessionstore.Snapshot{
		Session: sessionstore.SessionInfo{ID: "session-main", Revision: 7},
		View:    &sessionstore.ViewInfo{ViewID: "view-1"},
		Workbench: &sessiondoc.Doc{
			CurrentWorkspace: "main",
			WorkspaceOrder:   []string{"main"},
			Workspaces:       map[string]*sessiondoc.Workspace{"main": {Name: "main"}},
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
		sessionSnapshot: &sessionstore.Snapshot{Session: sessionstore.SessionInfo{ID: "session-main", Revision: 9}},
	}
	model := setupModel(t, modelOpts{client: client})
	model.sessionID = "session-main"
	model.sessionViewID = "view-local"
	model.sessionRevision = 3

	cmd, handled := model.handleSessionMessage(sessionEventMsg{
		Event: sessionstore.EventData{SessionID: "session-main", Revision: 4, ViewID: "view-remote"},
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
		Event: sessionstore.EventData{SessionID: "session-main", Deleted: true},
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
		View: &sessionstore.ViewInfo{ViewID: "view-next"},
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

func TestHandleSessionMessageSessionViewConflictIsSuppressed(t *testing.T) {
	model := setupModel(t, modelOpts{})

	cmd, handled := model.handleSessionMessage(sessionViewUpdatedMsg{
		Err: sessionstore.ErrConflict,
	})
	if !handled {
		t.Fatal("expected sessionViewUpdatedMsg handled")
	}
	if cmd != nil {
		t.Fatalf("expected recoverable view conflict to be suppressed, got %#v", cmd)
	}
	if model.err != nil {
		t.Fatalf("expected no user-visible error, got %v", model.err)
	}
}

func TestHandleSessionMessageUnknownMessageFallsThrough(t *testing.T) {
	model := setupModel(t, modelOpts{})
	cmd, handled := model.handleSessionMessage(tea.WindowSizeMsg{Width: 80, Height: 24})
	if handled || cmd != nil {
		t.Fatalf("expected unrelated msg to fall through, got handled=%v cmd=%#v", handled, cmd)
	}
}

func TestHandleSessionMessageSnapshotDowngradesFailedRuntimeBindings(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 7, Mode: "collaborator"},
		snapshotErr:  errors.New("snapshot failed"),
	}
	model := setupModel(t, modelOpts{client: client})
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", TerminalID: "term-new"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})
	snapshot := &sessionstore.Snapshot{
		Session:   sessionstore.SessionInfo{ID: "session-main", Revision: 7},
		View:      &sessionstore.ViewInfo{ViewID: "view-1", ActiveWorkspaceName: "main", ActiveTabID: "tab-1", FocusedPaneID: "pane-1"},
		Workbench: sessionstate.ExportWorkbench(wb),
	}

	cmd, handled := model.handleSessionMessage(sessionSnapshotMsg{Snapshot: snapshot})
	if !handled || cmd == nil {
		t.Fatalf("expected failed runtime snapshot apply to surface an error, got handled=%v cmd=%#v", handled, cmd)
	}
	tab := model.workbench.CurrentTab()
	if tab == nil || tab.Panes["pane-1"] == nil || tab.Panes["pane-1"].TerminalID != "" {
		t.Fatalf("expected failed runtime binding to be downgraded in workbench, got %#v", tab)
	}
	if msg := cmd(); msg == nil {
		t.Fatal("expected showError follow-up for failed runtime binding apply")
	}
}
