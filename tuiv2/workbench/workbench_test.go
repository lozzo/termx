package workbench

import "testing"

func TestWorkbenchAddRemoveAndListWorkspaces(t *testing.T) {
	wb := NewWorkbench()
	wb.AddWorkspace("main", &WorkspaceState{Name: "main"})
	wb.AddWorkspace("ops", &WorkspaceState{Name: "ops"})

	listed := wb.ListWorkspaces()
	if len(listed) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(listed))
	}
	if !wb.SwitchWorkspace("ops") {
		t.Fatal("expected switch to ops to succeed")
	}
	if current := wb.CurrentWorkspace(); current == nil || current.Name != "ops" {
		t.Fatalf("expected current workspace ops, got %#v", current)
	}

	wb.RemoveWorkspace("ops")
	if wb.SwitchWorkspace("ops") {
		t.Fatal("expected removed workspace switch to fail")
	}
}

func TestWorkbenchCurrentTabAndActivePaneClampStaleState(t *testing.T) {
	wb := NewWorkbench()
	wb.AddWorkspace("main", &WorkspaceState{
		Name:      "main",
		ActiveTab: 4,
		Tabs: []*TabState{{
			ID:           "tab-1",
			ActivePaneID: "ghost",
			Panes: map[string]*PaneState{
				"pane-1": {ID: "pane-1", Title: "shell"},
			},
			Root: NewLeaf("pane-1"),
		}},
	})

	if current := wb.CurrentTab(); current == nil || current.ID != "tab-1" {
		t.Fatalf("expected stale active tab to clamp to tab-1, got %#v", current)
	}
	if pane := wb.ActivePane(); pane == nil || pane.ID != "pane-1" {
		t.Fatalf("expected stale active pane to clamp to pane-1, got %#v", pane)
	}
}

func TestVisibleWithSizeProjectsFloatingPanes(t *testing.T) {
	wb := NewWorkbench()
	wb.AddWorkspace("main", &WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*TabState{{
			ID:           "tab-1",
			ActivePaneID: "pane-1",
			Panes: map[string]*PaneState{
				"pane-1": {ID: "pane-1", Title: "base", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "float", TerminalID: "term-2"},
			},
			Root:     NewLeaf("pane-1"),
			Floating: []*FloatingState{{PaneID: "pane-2", Rect: Rect{X: 5, Y: 4, W: 20, H: 6}, Z: 1}},
		}},
	})

	visible := wb.VisibleWithSize(Rect{W: 100, H: 40})
	if visible == nil {
		t.Fatal("expected visible workbench")
	}
	if len(visible.FloatingPanes) != 1 {
		t.Fatalf("expected 1 floating pane, got %#v", visible.FloatingPanes)
	}
	floating := visible.FloatingPanes[0]
	if floating.ID != "pane-2" || floating.Title != "float" {
		t.Fatalf("unexpected floating pane projection: %#v", floating)
	}
	if floating.Rect.W != 20 || floating.Rect.H != 6 {
		t.Fatalf("unexpected floating rect: %#v", floating.Rect)
	}
}
