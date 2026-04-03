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
	if got := wb.WorkspaceByName("main"); got == nil || got.Name != "main" {
		t.Fatalf("expected WorkspaceByName(main), got %#v", got)
	}
	if got := wb.WorkspaceByName("ops"); got != nil {
		t.Fatalf("expected removed workspace lookup to be nil, got %#v", got)
	}
}

func TestWorkbenchRenameWorkspacePreservesOrderAndCurrent(t *testing.T) {
	wb := NewWorkbench()
	wb.AddWorkspace("main", &WorkspaceState{Name: "main"})
	wb.AddWorkspace("ops", &WorkspaceState{Name: "ops"})
	if !wb.SwitchWorkspace("ops") {
		t.Fatal("expected switch to ops to succeed")
	}

	if err := wb.RenameWorkspace("ops", "dev"); err != nil {
		t.Fatalf("rename workspace failed: %v", err)
	}

	if current := wb.CurrentWorkspace(); current == nil || current.Name != "dev" {
		t.Fatalf("expected current workspace dev, got %#v", current)
	}
	listed := wb.ListWorkspaces()
	if len(listed) != 2 || listed[1] != "dev" {
		t.Fatalf("expected renamed workspace order to be preserved, got %#v", listed)
	}
	if got := wb.CurrentWorkspaceName(); got != "dev" {
		t.Fatalf("expected CurrentWorkspaceName to be dev, got %q", got)
	}
}

func TestWorkbenchSwitchWorkspaceByOffsetWraps(t *testing.T) {
	wb := NewWorkbench()
	wb.AddWorkspace("main", &WorkspaceState{Name: "main"})
	wb.AddWorkspace("ops", &WorkspaceState{Name: "ops"})
	wb.AddWorkspace("dev", &WorkspaceState{Name: "dev"})

	if err := wb.SwitchWorkspaceByOffset(-1); err != nil {
		t.Fatalf("switch prev failed: %v", err)
	}
	if current := wb.CurrentWorkspace(); current == nil || current.Name != "dev" {
		t.Fatalf("expected wrap to dev, got %#v", current)
	}

	if err := wb.SwitchWorkspaceByOffset(1); err != nil {
		t.Fatalf("switch next failed: %v", err)
	}
	if current := wb.CurrentWorkspace(); current == nil || current.Name != "main" {
		t.Fatalf("expected wrap back to main, got %#v", current)
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

func TestVisibleWithSizeProjectsFloatingPanesInStoredOrder(t *testing.T) {
	wb := NewWorkbench()
	wb.AddWorkspace("main", &WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*TabState{{
			ID:           "tab-1",
			ActivePaneID: "pane-1",
			Panes: map[string]*PaneState{
				"pane-1": {ID: "pane-1", Title: "base", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "float-a", TerminalID: "term-2"},
				"pane-3": {ID: "pane-3", Title: "float-b", TerminalID: "term-3"},
			},
			Root: NewLeaf("pane-1"),
			Floating: []*FloatingState{
				{PaneID: "pane-2", Rect: Rect{X: 1, Y: 2, W: 16, H: 5}, Z: 2},
				{PaneID: "pane-3", Rect: Rect{X: 9, Y: 4, W: 24, H: 7}, Z: 5},
			},
		}},
	})

	visible := wb.VisibleWithSize(Rect{W: 100, H: 40})
	if visible == nil {
		t.Fatal("expected visible workbench")
	}
	if len(visible.FloatingPanes) != 2 {
		t.Fatalf("expected 2 floating panes, got %#v", visible.FloatingPanes)
	}
	if visible.FloatingPanes[0].ID != "pane-2" || visible.FloatingPanes[1].ID != "pane-3" {
		t.Fatalf("expected visible floating order to follow stored floating order, got %#v", visible.FloatingPanes)
	}
	if visible.FloatingPanes[1].Rect.X != 9 || visible.FloatingPanes[1].Rect.H != 7 {
		t.Fatalf("unexpected projected floating rect: %#v", visible.FloatingPanes[1].Rect)
	}
}

func TestVisibleWithSizeDoesNotProjectAllFloatingTabsAsTiled(t *testing.T) {
	wb := NewWorkbench()
	wb.AddWorkspace("main", &WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*TabState{{
			ID:           "tab-1",
			ActivePaneID: "pane-2",
			Panes: map[string]*PaneState{
				"pane-2": {ID: "pane-2", Title: "float-a", TerminalID: "term-2"},
				"pane-3": {ID: "pane-3", Title: "float-b", TerminalID: "term-3"},
			},
			Floating: []*FloatingState{
				{PaneID: "pane-2", Rect: Rect{X: 2, Y: 3, W: 18, H: 6}, Z: 1},
				{PaneID: "pane-3", Rect: Rect{X: 10, Y: 7, W: 22, H: 9}, Z: 2},
			},
		}},
	})

	visible := wb.VisibleWithSize(Rect{W: 100, H: 40})
	if visible == nil {
		t.Fatal("expected visible workbench")
	}
	if len(visible.Tabs) != 1 {
		t.Fatalf("expected 1 visible tab, got %#v", visible.Tabs)
	}
	if len(visible.Tabs[0].Panes) != 0 {
		t.Fatalf("expected all-floating tab to have no tiled panes, got %#v", visible.Tabs[0].Panes)
	}
	if len(visible.FloatingPanes) != 2 {
		t.Fatalf("expected 2 floating panes, got %#v", visible.FloatingPanes)
	}
}
