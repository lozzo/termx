package sessionbind

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestManagerDetachClearsWorkbenchAndRuntime(t *testing.T) {
	wb := testWorkbench("term-1")
	rt := testRuntimeBinding("pane-1", "term-1", "running")
	term := rt.Registry().Get("term-1")

	manager := NewManager(wb, rt)
	target, err := manager.Detach("pane-1")
	if err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if target.PaneID != "pane-1" || target.TerminalID != "term-1" {
		t.Fatalf("unexpected detach target: %#v", target)
	}
	pane := wb.ActivePane()
	if pane == nil || pane.TerminalID != "" {
		t.Fatalf("expected workbench pane detached, got %#v", pane)
	}
	if got := rt.Binding("pane-1"); got != nil {
		t.Fatalf("expected runtime binding removed, got %#v", got)
	}
	if term.OwnerPaneID != "" || len(term.BoundPaneIDs) != 0 {
		t.Fatalf("expected terminal ownership cleared, got %#v", term)
	}
}

func TestManagerReconnectKeepsExitedBinding(t *testing.T) {
	wb := testWorkbench("term-1")
	rt := testRuntimeBinding("pane-1", "term-1", "exited")
	term := rt.Registry().Get("term-1")

	manager := NewManager(wb, rt)
	result, err := manager.Reconnect("pane-1")
	if err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	if !result.KeptExitedTerminal {
		t.Fatalf("expected exited binding to be preserved, got %#v", result)
	}
	pane := wb.ActivePane()
	if pane == nil || pane.TerminalID != "term-1" {
		t.Fatalf("expected exited pane to remain bound, got %#v", pane)
	}
	if got := rt.Binding("pane-1"); got == nil {
		t.Fatal("expected runtime binding retained for exited reconnect")
	}
	if term.OwnerPaneID != "pane-1" || len(term.BoundPaneIDs) != 1 || term.BoundPaneIDs[0] != "pane-1" {
		t.Fatalf("expected exited terminal ownership preserved, got %#v", term)
	}
}

func TestManagerBindDetachedTerminalUpdatesWorkbenchFocusAndRuntime(t *testing.T) {
	wb := testWorkbench("term-old")
	rt := testRuntimeBinding("pane-1", "term-old", "running")
	exitCode := 7

	manager := NewManager(wb, rt)
	result, err := manager.BindDetachedTerminal(BindDetachedTerminalRequest{
		TabID:      "tab-1",
		PaneID:     "pane-1",
		TerminalID: "term-new",
		Binding: runtime.DetachedTerminalBinding{
			Name:     "  dev shell  ",
			Command:  []string{"zsh", "-l"},
			Tags:     map[string]string{"role": "dev"},
			State:    "exited",
			ExitCode: &exitCode,
		},
	})
	if err != nil {
		t.Fatalf("BindDetachedTerminal: %v", err)
	}
	if result.Target.TabID != "tab-1" || result.Target.PaneID != "pane-1" || result.Target.TerminalID != "term-new" {
		t.Fatalf("unexpected bind target: %#v", result.Target)
	}
	if !result.LoadSnapshotAfter {
		t.Fatalf("expected exited detached terminal to request snapshot load, got %#v", result)
	}
	tab := wb.CurrentTab()
	if tab == nil || tab.ActivePaneID != "pane-1" {
		t.Fatalf("expected bound pane focused, got %#v", tab)
	}
	pane := wb.ActivePane()
	if pane == nil || pane.TerminalID != "term-new" || pane.Title != "dev shell" {
		t.Fatalf("expected workbench pane rebound with trimmed title, got %#v", pane)
	}
	if old := rt.Registry().Get("term-old"); old == nil || len(old.BoundPaneIDs) != 0 || old.OwnerPaneID != "" {
		t.Fatalf("expected old terminal detached from runtime cache, got %#v", old)
	}
	newTerm := rt.Registry().Get("term-new")
	if newTerm == nil {
		t.Fatal("expected new terminal in runtime registry")
	}
	if newTerm.Name != "dev shell" || newTerm.State != "exited" || newTerm.ExitCode == nil || *newTerm.ExitCode != exitCode {
		t.Fatalf("expected detached terminal metadata applied, got %#v", newTerm)
	}
	if len(newTerm.Command) != 2 || newTerm.Command[0] != "zsh" || newTerm.Tags["role"] != "dev" {
		t.Fatalf("expected detached terminal command/tags applied, got %#v", newTerm)
	}
}

func testWorkbench(terminalID string) *workbench.Workbench {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", TerminalID: terminalID},
			},
		}},
	})
	return wb
}

func testRuntimeBinding(paneID, terminalID, state string) *runtime.Runtime {
	rt := runtime.New(nil)
	binding := rt.BindPane(paneID)
	binding.Connected = true
	binding.Role = runtime.BindingRoleOwner
	term := rt.Registry().GetOrCreate(terminalID)
	term.State = state
	term.OwnerPaneID = paneID
	term.ControlPaneID = paneID
	term.BoundPaneIDs = []string{paneID}
	return rt
}
