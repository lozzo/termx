package persist

import (
	"encoding/json"
	"testing"

	"github.com/lozzow/termx/tuiv2/workbench"
)

func buildTestWorkbench() *workbench.Workbench {
	wb := workbench.NewWorkbench()
	ws := &workbench.WorkspaceState{Name: "default"}
	wb.AddWorkspace("default", ws)
	_ = wb.CreateTab("default", "tab-1", "one")
	_ = wb.CreateFirstPane("tab-1", "pane-a")
	_ = wb.SplitPane("tab-1", "pane-a", "pane-b", workbench.SplitVertical)
	_ = wb.BindPaneTerminal("tab-1", "pane-a", "term-1")
	return wb
}

func TestSave_RoundTrip(t *testing.T) {
	wb := buildTestWorkbench()
	data, err := Save(wb)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := Load(data)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Version != 2 {
		t.Errorf("expected version 2, got %d", loaded.Version)
	}
	if len(loaded.Data) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(loaded.Data))
	}
	ws := loaded.Data[0]
	if ws.Name != "default" {
		t.Errorf("expected workspace name 'default', got %q", ws.Name)
	}
	if len(ws.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(ws.Tabs))
	}
	tab := ws.Tabs[0]
	if tab.Name != "one" {
		t.Errorf("expected tab name 'one', got %q", tab.Name)
	}
	if len(tab.Panes) != 2 {
		t.Errorf("expected 2 panes, got %d", len(tab.Panes))
	}
	// Verify pane-a has its terminal binding preserved.
	foundBound := false
	for _, p := range tab.Panes {
		if p.ID == "pane-a" && p.TerminalID == "term-1" {
			foundBound = true
		}
	}
	if !foundBound {
		t.Error("pane-a terminal binding not preserved in round-trip")
	}
	// Verify layout was saved.
	if tab.Layout == nil {
		t.Error("expected non-nil layout after round-trip")
	}
}

func TestSave_RoundTripPersistsFloatingEntriesSeparately(t *testing.T) {
	wb := workbench.NewWorkbench()
	ws := &workbench.WorkspaceState{Name: "main"}
	wb.AddWorkspace("main", ws)
	_ = wb.CreateTab("main", "tab-1", "one")
	_ = wb.CreateFirstPane("tab-1", "pane-a")
	_ = wb.CreateFloatingPane("tab-1", "pane-b", workbench.Rect{X: 7, Y: 3, W: 22, H: 9})
	_ = wb.CreateFloatingPane("tab-1", "pane-c", workbench.Rect{X: 2, Y: 1, W: 14, H: 5})
	_ = wb.BindPaneTerminal("tab-1", "pane-b", "term-b")
	_ = wb.BindPaneTerminal("tab-1", "pane-c", "term-c")
	if !wb.ReorderFloatingPane("tab-1", "pane-b", true) {
		t.Fatal("expected floating reorder to succeed")
	}

	data, err := Save(wb)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := Load(data)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded.Data) != 1 || len(loaded.Data[0].Tabs) != 1 {
		t.Fatalf("unexpected loaded data shape: %#v", loaded.Data)
	}
	savedTab := loaded.Data[0].Tabs[0]
	if len(savedTab.Panes) != 3 {
		t.Fatalf("expected pane store to contain 3 panes, got %#v", savedTab.Panes)
	}
	if len(savedTab.Floating) != 2 {
		t.Fatalf("expected 2 explicit floating entries, got %#v", savedTab.Floating)
	}
	if savedTab.Floating[0].PaneID != "pane-c" || savedTab.Floating[0].Rect.X != 2 || savedTab.Floating[0].Z != 0 {
		t.Fatalf("unexpected first floating entry: %#v", savedTab.Floating[0])
	}
	if savedTab.Floating[1].PaneID != "pane-b" || savedTab.Floating[1].Rect.W != 22 || savedTab.Floating[1].Z != 1 {
		t.Fatalf("unexpected second floating entry: %#v", savedTab.Floating[1])
	}
}

func TestSave_PreservesPaneRecordStoreEntriesOutsideLayoutAndFloating(t *testing.T) {
	wb := workbench.NewWorkbench()
	ws := &workbench.WorkspaceState{Name: "main"}
	wb.AddWorkspace("main", ws)
	_ = wb.CreateTab("main", "tab-1", "one")
	_ = wb.CreateFirstPane("tab-1", "pane-a")
	tab := wb.CurrentTab()
	tab.Panes["pane-orphan"] = &workbench.PaneState{ID: "pane-orphan", Title: "orphan", TerminalID: "term-orphan"}

	data, err := Save(wb)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := Load(data)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	savedTab := loaded.Data[0].Tabs[0]
	if len(savedTab.Panes) != 2 {
		t.Fatalf("expected pane store to include layout and orphan pane, got %#v", savedTab.Panes)
	}
	foundOrphan := false
	for _, pane := range savedTab.Panes {
		if pane.ID == "pane-orphan" && pane.TerminalID == "term-orphan" {
			foundOrphan = true
		}
	}
	if !foundOrphan {
		t.Fatalf("expected orphan pane to remain in persisted pane store, got %#v", savedTab.Panes)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	_, err := Load([]byte("{invalid json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoad_UnsupportedVersion(t *testing.T) {
	data, _ := json.Marshal(map[string]any{"version": 99})
	_, err := Load(data)
	if err == nil {
		t.Fatal("expected error for unsupported version, got nil")
	}
}

func TestLoad_EmptyData(t *testing.T) {
	_, err := Load(nil)
	if err != ErrEmptyStateData {
		t.Fatalf("expected ErrEmptyStateData, got %v", err)
	}
	_, err = Load([]byte{})
	if err != ErrEmptyStateData {
		t.Fatalf("expected ErrEmptyStateData for empty slice, got %v", err)
	}
}
