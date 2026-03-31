package bootstrap_test

import (
	"encoding/json"
	"testing"

	"github.com/lozzow/termx/tuiv2/bootstrap"
	"github.com/lozzow/termx/tuiv2/persist"
	"github.com/lozzow/termx/tuiv2/workbench"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newWB() *workbench.Workbench { return workbench.NewWorkbench() }

// ---------------------------------------------------------------------------
// Startup — no prior state
// ---------------------------------------------------------------------------

func TestStartupNoState_ShouldOpenPicker(t *testing.T) {
	wb := newWB()
	result, err := bootstrap.Startup(bootstrap.Config{}, wb, nil)
	if err != nil {
		t.Fatalf("Startup returned unexpected error: %v", err)
	}
	if !result.ShouldOpenPicker {
		t.Fatal("expected ShouldOpenPicker=true when no prior state exists")
	}
}

func TestStartupNoState_DefaultWorkspaceCreated(t *testing.T) {
	wb := newWB()
	_, err := bootstrap.Startup(bootstrap.Config{}, wb, nil)
	if err != nil {
		t.Fatalf("Startup returned unexpected error: %v", err)
	}

	workspaces := wb.ListWorkspaces()
	if len(workspaces) == 0 {
		t.Fatal("expected at least one workspace after Startup")
	}

	ws := wb.CurrentWorkspace()
	if ws == nil {
		t.Fatal("expected a current workspace after Startup")
	}
	if len(ws.Tabs) == 0 {
		t.Fatal("expected at least one tab in the default workspace")
	}
}

// ---------------------------------------------------------------------------
// Restore — valid V2 data
// ---------------------------------------------------------------------------

func TestRestoreV2_PopulatesWorkbench(t *testing.T) {
	state := persist.WorkspaceStateFileV2{
		Version: 2,
		Data: []persist.WorkspaceEntryV2{
			{
				Name:      "dev",
				ActiveTab: 0,
				Tabs: []persist.TabEntryV2{
					{
						Name: "code",
						Panes: []persist.PaneEntryV2{
							{ID: "p1", Title: "editor", TerminalID: "t1"},
							{ID: "p2", Title: "shell", TerminalID: "t2"},
						},
						ActivePaneID: "p1",
					},
				},
			},
			{
				Name:      "ops",
				ActiveTab: 0,
				Tabs: []persist.TabEntryV2{
					{
						Name: "logs",
						Panes: []persist.PaneEntryV2{
							{ID: "p3", Title: "log", TerminalID: "t3"},
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("failed to marshal test state: %v", err)
	}

	wb := newWB()
	if err := bootstrap.Restore(data, wb, nil); err != nil {
		t.Fatalf("Restore returned unexpected error: %v", err)
	}

	workspaces := wb.ListWorkspaces()
	if len(workspaces) != 2 {
		t.Fatalf("expected 2 workspaces, got %d: %v", len(workspaces), workspaces)
	}

	if !wb.SwitchWorkspace("dev") {
		t.Fatal("expected 'dev' workspace to exist")
	}
	devWS := wb.CurrentWorkspace()
	if len(devWS.Tabs) != 1 {
		t.Fatalf("expected 1 tab in 'dev', got %d", len(devWS.Tabs))
	}
	tab := devWS.Tabs[0]
	if tab.Name != "code" {
		t.Fatalf("expected tab name 'code', got %q", tab.Name)
	}
	if len(tab.Panes) != 2 {
		t.Fatalf("expected 2 panes in tab 'code', got %d", len(tab.Panes))
	}
	if tab.ActivePaneID != "p1" {
		t.Fatalf("expected ActivePaneID='p1', got %q", tab.ActivePaneID)
	}

	pane, ok := tab.Panes["p1"]
	if !ok {
		t.Fatal("expected pane p1 to exist")
	}
	if pane.TerminalID != "t1" {
		t.Fatalf("expected pane p1 TerminalID='t1', got %q", pane.TerminalID)
	}

	if !wb.SwitchWorkspace("ops") {
		t.Fatal("expected 'ops' workspace to exist")
	}
	opsWS := wb.CurrentWorkspace()
	if len(opsWS.Tabs) != 1 {
		t.Fatalf("expected 1 tab in 'ops', got %d", len(opsWS.Tabs))
	}
}

// ---------------------------------------------------------------------------
// Restore — empty / nil data
// ---------------------------------------------------------------------------

func TestRestoreEmptyData_ReturnsError(t *testing.T) {
	wb := newWB()
	err := bootstrap.Restore(nil, wb, nil)
	if err == nil {
		t.Fatal("expected error when restoring nil data, got nil")
	}
}

func TestRestoreEmptyBytes_ReturnsError(t *testing.T) {
	wb := newWB()
	err := bootstrap.Restore([]byte{}, wb, nil)
	if err == nil {
		t.Fatal("expected error when restoring empty byte slice, got nil")
	}
}

func TestRestoreInvalidJSON_ReturnsError(t *testing.T) {
	wb := newWB()
	err := bootstrap.Restore([]byte("not json"), wb, nil)
	if err == nil {
		t.Fatal("expected error when restoring invalid JSON, got nil")
	}
}

func TestRestoreEmptyData_WorkbenchUnchanged(t *testing.T) {
	wb := newWB()
	// Pre-populate so we can assert nothing was touched.
	wb.AddWorkspace("preexisting", &workbench.WorkspaceState{Name: "preexisting"})

	_ = bootstrap.Restore(nil, wb, nil)

	workspaces := wb.ListWorkspaces()
	if len(workspaces) != 1 || workspaces[0] != "preexisting" {
		t.Fatalf("expected workbench unchanged, got %v", workspaces)
	}
}

func TestPersistSaveLoadRoundTripPreservesCurrentWorkspaceAndLayout(t *testing.T) {
	wb := newWB()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{
			{
				ID:           "tab-main",
				Name:         "1",
				Panes:        map[string]*workbench.PaneState{"main-pane": {ID: "main-pane", Title: "shell", TerminalID: "term-main"}},
				Root:         workbench.NewLeaf("main-pane"),
				ActivePaneID: "main-pane",
			},
		},
	})
	wb.AddWorkspace("dev", &workbench.WorkspaceState{
		Name:      "dev",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{
			{
				ID:   "tab-dev",
				Name: "code",
				Panes: map[string]*workbench.PaneState{
					"pane-left":  {ID: "pane-left", Title: "editor", TerminalID: "term-left"},
					"pane-right": {ID: "pane-right", Title: "shell", TerminalID: "term-right"},
				},
				Root: &workbench.LayoutNode{
					Direction: workbench.SplitVertical,
					Ratio:     0.6,
					First:     workbench.NewLeaf("pane-left"),
					Second:    workbench.NewLeaf("pane-right"),
				},
				ActivePaneID: "pane-right",
				ZoomedPaneID: "pane-left",
				LayoutPreset: 2,
			},
		},
	})
	if !wb.SwitchWorkspace("dev") {
		t.Fatal("expected to switch to dev workspace")
	}

	data, err := persist.Save(wb)
	if err != nil {
		t.Fatalf("Save returned unexpected error: %v", err)
	}

	file, err := persist.Load(data)
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if file.Version != 2 {
		t.Fatalf("expected version 2, got %d", file.Version)
	}
	if len(file.Data) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(file.Data))
	}
	if file.Data[0].Name != "dev" {
		t.Fatalf("expected current workspace to be serialized first, got %q", file.Data[0].Name)
	}
	if file.Data[0].Tabs[0].Layout == nil {
		t.Fatal("expected layout tree to be serialized")
	}
	if file.Data[0].Tabs[0].Layout.Direction != string(workbench.SplitVertical) {
		t.Fatalf("expected layout direction %q, got %q", workbench.SplitVertical, file.Data[0].Tabs[0].Layout.Direction)
	}

	restored := newWB()
	if err := bootstrap.RestoreFile(file, restored, nil); err != nil {
		t.Fatalf("RestoreFile returned unexpected error: %v", err)
	}

	if current := restored.CurrentWorkspace(); current == nil || current.Name != "dev" {
		t.Fatalf("expected restored current workspace to be dev, got %#v", current)
	}
	tab := restored.CurrentTab()
	if tab == nil {
		t.Fatal("expected restored current tab")
	}
	if tab.ActivePaneID != "pane-right" {
		t.Fatalf("expected active pane to round-trip, got %q", tab.ActivePaneID)
	}
	if tab.ZoomedPaneID != "pane-left" {
		t.Fatalf("expected zoomed pane to round-trip, got %q", tab.ZoomedPaneID)
	}
	if tab.Root == nil || tab.Root.Direction != workbench.SplitVertical || tab.Root.First == nil || tab.Root.Second == nil {
		t.Fatalf("expected restored split layout, got %#v", tab.Root)
	}
}

func TestPersistLoadImportsLegacyV1State(t *testing.T) {
	legacy := map[string]any{
		"version":          1,
		"active_workspace": 1,
		"workspaces": []any{
			map[string]any{
				"name":       "main",
				"active_tab": 0,
				"tabs": []any{
					map[string]any{
						"name":           "1",
						"panes":          []any{map[string]any{"id": "p-main", "title": "main"}},
						"root":           map[string]any{"pane_id": "p-main"},
						"active_pane_id": "p-main",
					},
				},
			},
			map[string]any{
				"name":       "dev",
				"active_tab": 0,
				"tabs": []any{
					map[string]any{
						"name":           "code",
						"active_pane_id": "p2",
						"root": map[string]any{
							"direction": "horizontal",
							"ratio":     0.5,
							"first":     map[string]any{"pane_id": "p1"},
							"second":    map[string]any{"pane_id": "p2"},
						},
						"panes": []any{
							map[string]any{
								"id":          "p1",
								"title":       "shell",
								"terminal_id": "term-1",
								"name":        "bash",
								"command":     []any{"bash", "-lc", "htop"},
								"tags":        map[string]any{"role": "shell"},
							},
							map[string]any{
								"id":          "p2",
								"title":       "editor",
								"terminal_id": "term-2",
								"name":        "nvim",
							},
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("failed to marshal legacy test state: %v", err)
	}

	file, err := persist.Load(data)
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if file.Version != 2 {
		t.Fatalf("expected imported version 2, got %d", file.Version)
	}
	if len(file.Data) != 2 {
		t.Fatalf("expected 2 imported workspaces, got %d", len(file.Data))
	}
	if file.Data[0].Name != "dev" {
		t.Fatalf("expected active workspace to be moved first, got %q", file.Data[0].Name)
	}
	tab := file.Data[0].Tabs[0]
	if tab.Layout == nil || tab.Layout.Direction != "horizontal" {
		t.Fatalf("expected legacy root to become v2 layout, got %#v", tab.Layout)
	}
	if len(file.Metadata) != 2 {
		t.Fatalf("expected terminal metadata for imported panes, got %d entries", len(file.Metadata))
	}
	if file.Metadata[0].TerminalID != "term-1" {
		t.Fatalf("expected first imported metadata terminal to be term-1, got %q", file.Metadata[0].TerminalID)
	}
	if len(file.Metadata[0].Command) != 3 || file.Metadata[0].Command[2] != "htop" {
		t.Fatalf("expected imported command metadata, got %#v", file.Metadata[0].Command)
	}
	if file.Metadata[0].Tags["role"] != "shell" {
		t.Fatalf("expected imported tags, got %#v", file.Metadata[0].Tags)
	}
}

func TestRestoreOrStartupFallsBackToStartupWhenDataIsEmpty(t *testing.T) {
	wb := newWB()
	result, err := bootstrap.RestoreOrStartup(nil, bootstrap.Config{DefaultWorkspaceName: "scratch"}, wb, nil)
	if err != nil {
		t.Fatalf("RestoreOrStartup returned unexpected error: %v", err)
	}
	if !result.ShouldOpenPicker {
		t.Fatal("expected empty restore path to fall back to startup picker")
	}
	if current := wb.CurrentWorkspace(); current == nil || current.Name != "scratch" {
		t.Fatalf("expected startup workspace scratch, got %#v", current)
	}
}

func TestRestoreOrStartupRestoresPersistedStateWithoutOpeningPicker(t *testing.T) {
	source := newWB()
	source.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{
			{
				ID:           "tab-1",
				Name:         "1",
				Panes:        map[string]*workbench.PaneState{"p1": {ID: "p1", TerminalID: "term-1"}},
				Root:         workbench.NewLeaf("p1"),
				ActivePaneID: "p1",
			},
		},
	})

	data, err := persist.Save(source)
	if err != nil {
		t.Fatalf("Save returned unexpected error: %v", err)
	}

	wb := newWB()
	result, err := bootstrap.RestoreOrStartup(data, bootstrap.Config{}, wb, nil)
	if err != nil {
		t.Fatalf("RestoreOrStartup returned unexpected error: %v", err)
	}
	if result.ShouldOpenPicker {
		t.Fatal("expected restored state to skip startup picker")
	}
	if current := wb.CurrentWorkspace(); current == nil || current.Name != "main" {
		t.Fatalf("expected restored workspace main, got %#v", current)
	}
}
