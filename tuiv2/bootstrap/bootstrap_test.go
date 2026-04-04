package bootstrap_test

import (
	"encoding/json"
	"testing"

	"github.com/lozzow/termx/tuiv2/bootstrap"
	"github.com/lozzow/termx/tuiv2/persist"
	"github.com/lozzow/termx/tuiv2/runtime"
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
	if tab.ActivePaneID == "" {
		t.Fatal("expected restored active pane ID")
	}

	pane, ok := tab.Panes[tab.ActivePaneID]
	if !ok {
		t.Fatalf("expected active pane %q to exist", tab.ActivePaneID)
	}
	if pane.TerminalID != "t1" {
		t.Fatalf("expected active pane TerminalID='t1', got %q", pane.TerminalID)
	}

	if !wb.SwitchWorkspace("ops") {
		t.Fatal("expected 'ops' workspace to exist")
	}
	opsWS := wb.CurrentWorkspace()
	if len(opsWS.Tabs) != 1 {
		t.Fatalf("expected 1 tab in 'ops', got %d", len(opsWS.Tabs))
	}
}

func TestRestoreV2_RestoresFloatingEntries(t *testing.T) {
	state := persist.WorkspaceStateFileV2{
		Version: 2,
		Data: []persist.WorkspaceEntryV2{{
			Name:      "dev",
			ActiveTab: 0,
			Tabs: []persist.TabEntryV2{{
				Name:         "code",
				ActivePaneID: "p1",
				Panes: []persist.PaneEntryV2{
					{ID: "p1", Title: "editor", TerminalID: "t1"},
					{ID: "p2", Title: "float-a", TerminalID: "t2"},
					{ID: "p3", Title: "float-b", TerminalID: "t3"},
				},
				Layout: &persist.LayoutNodeEntry{PaneID: "p1"},
				Floating: []persist.FloatingEntryV2{
					{PaneID: "p2", Rect: persist.RectEntryV2{X: 4, Y: 2, W: 30, H: 8}, Z: 3},
					{PaneID: "p3", Rect: persist.RectEntryV2{X: 8, Y: 6, W: 18, H: 7}, Z: 7},
				},
			}},
		}},
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("failed to marshal test state: %v", err)
	}

	wb := newWB()
	if err := bootstrap.Restore(data, wb, nil); err != nil {
		t.Fatalf("Restore returned unexpected error: %v", err)
	}

	tab := wb.CurrentTab()
	if tab == nil {
		t.Fatal("expected restored current tab")
	}
	if len(tab.Floating) != 2 {
		t.Fatalf("expected 2 restored floating entries, got %#v", tab.Floating)
	}
	if tab.Root == nil || tab.Root.PaneID == "" {
		t.Fatalf("expected tiled root to stay separate from floating panes, got %#v", tab.Root)
	}
	if tab.Root.PaneID == tab.Floating[0].PaneID || tab.Root.PaneID == tab.Floating[1].PaneID {
		t.Fatalf("expected tiled root to stay separate from floating panes, got root=%q floating=%#v", tab.Root.PaneID, tab.Floating)
	}
	if tab.Panes[tab.Root.PaneID] == nil || tab.Panes[tab.Root.PaneID].TerminalID != "t1" {
		t.Fatalf("expected tiled root pane to retain terminal t1, got %#v", tab.Panes[tab.Root.PaneID])
	}
	if tab.Panes[tab.Floating[0].PaneID] == nil || tab.Panes[tab.Floating[0].PaneID].TerminalID != "t2" || tab.Floating[0].Rect.W != 30 || tab.Floating[0].Z != 3 {
		t.Fatalf("unexpected first floating entry: %#v", tab.Floating[0])
	}
	if tab.Panes[tab.Floating[1].PaneID] == nil || tab.Panes[tab.Floating[1].PaneID].TerminalID != "t3" || tab.Floating[1].Rect.Y != 6 || tab.Floating[1].Z != 7 {
		t.Fatalf("unexpected second floating entry: %#v", tab.Floating[1])
	}
}

func TestRestoreV2_IgnoresPersistedTerminalMetadata(t *testing.T) {
	data := []byte(`{
		"version": 2,
		"terminal_metadata": [{
			"terminal_id": "t1",
			"name": "shell",
			"command": ["bash", "-lc", "htop"],
			"tags": {"role": "dev"}
		}],
		"workspaces": [{
			"name": "dev",
			"active_tab": 0,
			"tabs": [{
				"name": "code",
				"active_pane_id": "p1",
				"panes": [{"id": "p1", "title": "editor", "terminal_id": "t1"}]
			}]
		}]
	}`)

	wb := newWB()
	rt := runtime.New(nil)
	if err := bootstrap.Restore(data, wb, rt); err != nil {
		t.Fatalf("Restore returned unexpected error: %v", err)
	}

	terminal := rt.Registry().Get("t1")
	if terminal != nil {
		t.Fatalf("expected restore to ignore terminal metadata cache, got %#v", terminal)
	}
}

func TestRestoreV2_AllFloatingTabStaysOutOfTiledProjection(t *testing.T) {
	state := persist.WorkspaceStateFileV2{
		Version: 2,
		Data: []persist.WorkspaceEntryV2{{
			Name:      "dev",
			ActiveTab: 0,
			Tabs: []persist.TabEntryV2{{
				Name:         "floaters",
				ActivePaneID: "p2",
				Panes: []persist.PaneEntryV2{
					{ID: "p2", Title: "float-a", TerminalID: "t2"},
					{ID: "p3", Title: "float-b", TerminalID: "t3"},
				},
				Floating: []persist.FloatingEntryV2{
					{PaneID: "p2", Rect: persist.RectEntryV2{X: 4, Y: 2, W: 30, H: 8}, Z: 3},
					{PaneID: "p3", Rect: persist.RectEntryV2{X: 8, Y: 6, W: 18, H: 7}, Z: 7},
				},
			}},
		}},
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("failed to marshal test state: %v", err)
	}

	wb := newWB()
	if err := bootstrap.Restore(data, wb, nil); err != nil {
		t.Fatalf("Restore returned unexpected error: %v", err)
	}

	tab := wb.CurrentTab()
	if tab == nil {
		t.Fatal("expected restored current tab")
	}
	if tab.Root != nil {
		t.Fatalf("expected all-floating tab to restore without tiled root, got %#v", tab.Root)
	}

	visible := wb.VisibleWithSize(workbench.Rect{W: 100, H: 40})
	if visible == nil {
		t.Fatal("expected visible workbench")
	}
	if len(visible.Tabs) != 1 {
		t.Fatalf("expected 1 visible tab, got %#v", visible.Tabs)
	}
	if len(visible.Tabs[0].Panes) != 0 {
		t.Fatalf("expected no tiled panes for all-floating tab, got %#v", visible.Tabs[0].Panes)
	}
	if len(visible.FloatingPanes) != 2 {
		t.Fatalf("expected 2 floating panes, got %#v", visible.FloatingPanes)
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
	if pane := tab.Panes[tab.ActivePaneID]; pane == nil || pane.TerminalID != "term-right" {
		t.Fatalf("expected active pane to retain terminal term-right, got %#v", pane)
	}
	if pane := tab.Panes[tab.ZoomedPaneID]; pane == nil || pane.TerminalID != "term-left" {
		t.Fatalf("expected zoomed pane to retain terminal term-left, got %#v", pane)
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
