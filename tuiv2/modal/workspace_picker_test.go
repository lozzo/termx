package modal

import "testing"

func TestWorkspacePickerStateFieldsAndFiltering(t *testing.T) {
	state := WorkspacePickerState{
		Title:  "Choose Workspace",
		Footer: "[Enter] switch or create  [Esc] close",
		Items: []WorkspacePickerItem{
			{Name: "ws-3", Description: "Create a new workspace", CreateNew: true},
			{Name: "main", Description: "1 tab(s), 1 pane(s)"},
			{Name: "dev", Description: "2 tab(s), 3 pane(s) [active]"},
		},
		Selected:    1,
		Query:       "dev",
		RenderWidth: 72,
	}

	state.ApplyFilter()

	if state.Title != "Choose Workspace" {
		t.Fatalf("expected title to round-trip, got %q", state.Title)
	}
	if state.Footer == "" {
		t.Fatal("expected footer to be stored")
	}
	if state.RenderWidth != 72 {
		t.Fatalf("expected render width 72, got %d", state.RenderWidth)
	}
	if len(state.Filtered) != 1 {
		t.Fatalf("expected one filtered item, got %d", len(state.Filtered))
	}
	selected := state.SelectedItem()
	if selected == nil {
		t.Fatal("expected selected item after filtering")
	}
	if selected.Name != "dev" {
		t.Fatalf("expected selected filtered workspace dev, got %q", selected.Name)
	}
}

func TestWorkspacePickerStateMoveClampsToVisibleItems(t *testing.T) {
	state := WorkspacePickerState{
		Items: []WorkspacePickerItem{
			{Name: "main"},
			{Name: "dev"},
		},
	}
	state.ApplyFilter()

	state.Move(10)
	if state.Selected != 2 {
		t.Fatalf("expected selection to clamp at last item, got %d", state.Selected)
	}

	state.Move(-10)
	if state.Selected != 0 {
		t.Fatalf("expected selection to clamp at first item, got %d", state.Selected)
	}
}

func TestWorkspacePickerStateZeroValue(t *testing.T) {
	var state WorkspacePickerState
	if state.VisibleItems() != nil {
		t.Fatalf("expected nil visible items for zero value, got %#v", state.VisibleItems())
	}
	if state.SelectedItem() != nil {
		t.Fatal("expected nil selected item for zero value")
	}
}

func TestWorkspacePickerStateAddsCreateRowForUniqueQuery(t *testing.T) {
	state := WorkspacePickerState{
		Items: []WorkspacePickerItem{
			{Name: "main"},
			{Name: "dev"},
		},
		Query: "feat/auth",
	}
	state.ApplyFilter()
	items := state.VisibleItems()
	if len(items) != 1 || !items[0].CreateNew || items[0].CreateName != "feat/auth" {
		t.Fatalf("expected query-backed create row, got %#v", items)
	}
}

func TestWorkspacePickerStatePaneMatchRetainsAncestorPath(t *testing.T) {
	state := WorkspacePickerState{
		Items: []WorkspacePickerItem{
			{Kind: WorkspacePickerItemWorkspace, Name: "main", WorkspaceName: "main"},
			{Kind: WorkspacePickerItemTab, Name: "backend", WorkspaceName: "main", TabID: "tab-1", TabIndex: 0, Depth: 1},
			{Kind: WorkspacePickerItemPane, Name: "server-logs", WorkspaceName: "main", TabID: "tab-1", PaneID: "pane-1", Depth: 2},
			{Kind: WorkspacePickerItemWorkspace, Name: "dev", WorkspaceName: "dev"},
		},
		Query: "server",
	}
	state.ApplyFilter()
	items := state.VisibleItems()
	if len(items) != 4 {
		t.Fatalf("expected ancestor path plus create row, got %#v", items)
	}
	if items[0].Kind != WorkspacePickerItemWorkspace || items[0].Name != "main" {
		t.Fatalf("expected workspace ancestor first, got %#v", items[0])
	}
	if items[1].Kind != WorkspacePickerItemTab || items[1].Name != "backend" {
		t.Fatalf("expected tab ancestor second, got %#v", items[1])
	}
	if items[2].Kind != WorkspacePickerItemPane || items[2].Name != "server-logs" {
		t.Fatalf("expected matching pane third, got %#v", items[2])
	}
	if !items[3].CreateNew {
		t.Fatalf("expected trailing create row, got %#v", items[3])
	}
}
