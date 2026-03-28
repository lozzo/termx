package tui

import "testing"

func TestNewAppHoldsWorkbenchReference(t *testing.T) {
	workbench := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1")}})

	app := NewApp(workbench, nil, nil, nil)

	if app == nil {
		t.Fatal("expected app")
	}
	if app.Workbench() != workbench {
		t.Fatal("expected app to hold workbench reference")
	}
}

func TestAppActivateTabDelegatesToWorkbench(t *testing.T) {
	workbench := NewWorkbench(Workspace{
		Name:      "main",
		Tabs:      []*Tab{{Name: "1"}, {Name: "2"}},
		ActiveTab: 0,
	})
	app := NewApp(workbench, nil, nil, nil)

	if !app.ActivateTab(1) {
		t.Fatal("expected activate tab to succeed")
	}
	if workbench.CurrentWorkspace().ActiveTab != 1 {
		t.Fatalf("expected active tab 1, got %d", workbench.CurrentWorkspace().ActiveTab)
	}
}

func TestAppFocusPaneDelegatesToWorkbench(t *testing.T) {
	tab := &Tab{
		Name:         "1",
		Panes:        map[string]*Pane{"p1": {ID: "p1", Title: "Pane 1"}, "p2": {ID: "p2", Title: "Pane 2"}},
		ActivePaneID: "p1",
	}
	workbench := NewWorkbench(Workspace{
		Name:      "main",
		Tabs:      []*Tab{tab},
		ActiveTab: 0,
	})
	app := NewApp(workbench, nil, nil, nil)

	if !app.FocusPane("p2") {
		t.Fatal("expected focus pane to succeed")
	}
	if workbench.CurrentTab().ActivePaneID != "p2" {
		t.Fatalf("expected active pane p2, got %q", workbench.CurrentTab().ActivePaneID)
	}
}

func TestAppOpenTerminalPickerUsesWorkbenchSelection(t *testing.T) {
	workspace := Workspace{
		Name: "main",
		Tabs: []*Tab{
			{
				Name:         "1",
				Panes:        map[string]*Pane{"p1": {ID: "p1", Title: "Pane 1", Viewport: &Viewport{TerminalID: "term-1"}}},
				ActivePaneID: "p1",
			},
			{
				Name:         "2",
				Panes:        map[string]*Pane{"p2": {ID: "p2", Title: "Pane 2", Viewport: &Viewport{}}},
				ActivePaneID: "p2",
			},
		},
		ActiveTab: 0,
	}
	workbench := NewWorkbench(workspace)
	app := NewApp(workbench, nil, nil, nil)

	if !app.ActivateTab(1) {
		t.Fatal("expected activate tab to succeed")
	}

	action, allowCreate := app.TerminalPickerContext()
	if action.Kind != terminalPickerActionReplace {
		t.Fatalf("expected replace action, got %v", action.Kind)
	}
	if action.TabIndex != 1 {
		t.Fatalf("expected tab index 1, got %d", action.TabIndex)
	}
	if !allowCreate {
		t.Fatal("expected create to be allowed for empty active pane")
	}
}

func TestAppHandlesWorkspaceActivatedBySyncingWorkbench(t *testing.T) {
	workbench := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1")}, ActiveTab: 0})
	app := NewApp(workbench, nil, nil, nil)
	workspace := Workspace{Name: "dev", Tabs: []*Tab{newTab("2")}, ActiveTab: 0}

	notice, bootstrap := app.HandleWorkspaceActivated(workspace, 0)
	if notice != "" {
		t.Fatalf("expected empty notice passthrough, got %q", notice)
	}
	if !bootstrap {
		t.Fatal("expected activate helper to preserve bootstrap flag for caller-controlled flow")
	}
	if workbench.CurrentWorkspace().Name != "dev" {
		t.Fatalf("expected workbench current workspace dev, got %q", workbench.CurrentWorkspace().Name)
	}
}
