package workbenchsvc

import (
	"testing"

	"github.com/lozzow/termx/termx-core/workbenchdoc"
	"github.com/lozzow/termx/termx-core/workbenchops"
)

func TestCreateAttachApplyAndUpdateView(t *testing.T) {
	svc := New()
	session, err := svc.CreateSession(CreateSessionOptions{ID: "main"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if session.Revision != 1 {
		t.Fatalf("expected revision 1, got %d", session.Revision)
	}
	attached, err := svc.AttachSession("main", AttachSessionOptions{
		ClientID:   "client-a",
		WindowCols: 160,
		WindowRows: 40,
	})
	if err != nil {
		t.Fatalf("attach session: %v", err)
	}
	if attached.View == nil || attached.View.ViewID == "" {
		t.Fatal("expected attached view")
	}
	result, err := svc.Apply("main", ApplyRequest{
		ViewID:       attached.View.ViewID,
		BaseRevision: attached.Session.Revision,
		Ops: []workbenchops.Op{
			{Kind: workbenchops.OpSplitPane, TabID: "1", PaneID: "1", NewPaneID: "2", Direction: "vertical"},
			{Kind: workbenchops.OpBindTerminal, TabID: "1", PaneID: "2", TerminalID: "term-2"},
		},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Session.Revision != 2 {
		t.Fatalf("expected revision 2, got %d", result.Session.Revision)
	}
	tab := result.Session.Doc.Workspaces["main"].Tabs[0]
	if got := tab.Panes["2"].TerminalID; got != "term-2" {
		t.Fatalf("expected terminal term-2, got %q", got)
	}
	view, err := svc.UpdateView("main", attached.View.ViewID, UpdateViewRequest{
		ActiveTabID:   "1",
		FocusedPaneID: "2",
	})
	if err != nil {
		t.Fatalf("update view: %v", err)
	}
	if view.FocusedPaneID != "2" {
		t.Fatalf("expected focused pane 2, got %q", view.FocusedPaneID)
	}
}

func TestApplyRejectsRevisionConflict(t *testing.T) {
	svc := New()
	if _, err := svc.CreateSession(CreateSessionOptions{ID: "main"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	_, err := svc.Apply("main", ApplyRequest{
		BaseRevision: 9,
		Ops:          []workbenchops.Op{{Kind: workbenchops.OpCreateWorkspace, NewName: "ops"}},
	})
	if err == nil {
		t.Fatal("expected revision conflict")
	}
}

func TestReplaceRejectsRevisionConflictAndUpdatesDocument(t *testing.T) {
	svc := New()
	session, err := svc.CreateSession(CreateSessionOptions{ID: "main"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := svc.Replace("main", ReplaceRequest{
		BaseRevision: session.Revision,
		Doc: &workbenchdoc.Doc{
			CurrentWorkspace: "main",
			WorkspaceOrder:   []string{"main"},
			Workspaces: map[string]*workbenchdoc.Workspace{
				"main": {
					Name: "main",
					Tabs: []*workbenchdoc.Tab{{
						ID:           "1",
						Name:         "renamed",
						Root:         workbenchdoc.NewLeaf("1"),
						Panes:        map[string]*workbenchdoc.Pane{"1": {ID: "1"}},
						ActivePaneID: "1",
					}},
					ActiveTab: 0,
				},
			},
		},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if _, err := svc.Replace("main", ReplaceRequest{
		BaseRevision: session.Revision,
		Doc:          workbenchdoc.New(),
	}); err == nil {
		t.Fatal("expected replace revision conflict")
	}
	snapshot, err := svc.GetSession("main")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got := snapshot.Session.Doc.Workspaces["main"].Tabs[0].Name; got != "renamed" {
		t.Fatalf("expected replaced tab name, got %q", got)
	}
}

func TestDetachRemovesView(t *testing.T) {
	svc := New()
	if _, err := svc.CreateSession(CreateSessionOptions{ID: "main"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	attached, err := svc.AttachSession("main", AttachSessionOptions{ClientID: "client-a"})
	if err != nil {
		t.Fatalf("attach session: %v", err)
	}
	if err := svc.DetachSession("main", attached.View.ViewID); err != nil {
		t.Fatalf("detach session: %v", err)
	}
	if _, err := svc.UpdateView("main", attached.View.ViewID, UpdateViewRequest{FocusedPaneID: "1"}); err == nil {
		t.Fatal("expected update view to fail after detach")
	}
}

func TestAcquireAndReleaseLease(t *testing.T) {
	svc := New()
	if _, err := svc.CreateSession(CreateSessionOptions{ID: "main"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	attached, err := svc.AttachSession("main", AttachSessionOptions{ClientID: "client-a"})
	if err != nil {
		t.Fatalf("attach session: %v", err)
	}
	lease, err := svc.AcquireLease("main", attached.View.ViewID, AcquireLeaseRequest{
		PaneID:     "1",
		TerminalID: "term-1",
	})
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	if lease.ViewID != attached.View.ViewID || lease.PaneID != "1" || lease.TerminalID != "term-1" {
		t.Fatalf("unexpected lease: %#v", lease)
	}
	snapshot, err := svc.GetSession("main")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if len(snapshot.Leases) != 1 || snapshot.Leases[0].TerminalID != "term-1" {
		t.Fatalf("expected one lease for term-1, got %#v", snapshot.Leases)
	}
	if err := svc.ReleaseLease("main", attached.View.ViewID, ReleaseLeaseRequest{TerminalID: "term-1"}); err != nil {
		t.Fatalf("release lease: %v", err)
	}
	snapshot, err = svc.GetSession("main")
	if err != nil {
		t.Fatalf("get session after release: %v", err)
	}
	if len(snapshot.Leases) != 0 {
		t.Fatalf("expected released lease to disappear, got %#v", snapshot.Leases)
	}
}
