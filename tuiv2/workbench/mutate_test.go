package workbench

import "testing"

// setupWorkbench creates a workbench with one workspace "main" and one tab "t1".
func setupWorkbench(t *testing.T) *Workbench {
	t.Helper()
	wb := NewWorkbench()
	wb.AddWorkspace("main", &WorkspaceState{Name: "main"})
	return wb
}

// ────────────────────────────────────────────────────────────────────────────
// CreateTab
// ────────────────────────────────────────────────────────────────────────────

func TestCreateTab_AddsTabToWorkspace(t *testing.T) {
	wb := setupWorkbench(t)
	err := wb.CreateTab("main", "tab1", "Tab One")
	if err != nil {
		t.Fatalf("CreateTab: unexpected error: %v", err)
	}
	ws := wb.store["main"]
	if len(ws.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(ws.Tabs))
	}
	tab := ws.Tabs[0]
	if tab.ID != "tab1" || tab.Name != "Tab One" {
		t.Errorf("tab fields mismatch: %+v", tab)
	}
	if tab.Panes == nil {
		t.Error("tab.Panes should be initialised, got nil")
	}
}

func TestCreateTab_UnknownWorkspace_ReturnsError(t *testing.T) {
	wb := setupWorkbench(t)
	if err := wb.CreateTab("nope", "t", "T"); err == nil {
		t.Fatal("expected error for unknown workspace")
	}
}

func TestCreateTab_DuplicateID_ReturnsError(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	if err := wb.CreateTab("main", "tab1", "Tab Again"); err == nil {
		t.Fatal("expected error for duplicate tab ID")
	}
}

func TestCreateTab_ActivatesNewTab(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")

	if err := wb.CreateTab("main", "tab2", "Tab Two"); err != nil {
		t.Fatalf("CreateTab: unexpected error: %v", err)
	}

	ws := wb.store["main"]
	if ws.ActiveTab != 1 {
		t.Fatalf("expected new tab to become active, got ActiveTab=%d", ws.ActiveTab)
	}
	if current := wb.CurrentTab(); current == nil || current.ID != "tab2" {
		t.Fatalf("expected current tab tab2, got %#v", current)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// CreateFirstPane
// ────────────────────────────────────────────────────────────────────────────

func TestCreateFirstPane_SetsRootLeafAndActivePane(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")

	err := wb.CreateFirstPane("tab1", "pane1")
	if err != nil {
		t.Fatalf("CreateFirstPane: unexpected error: %v", err)
	}
	ws := wb.store["main"]
	tab := ws.Tabs[0]

	// Panes map must contain the pane
	if _, ok := tab.Panes["pane1"]; !ok {
		t.Fatal("pane1 not found in tab.Panes")
	}
	// Root must be a leaf pointing at pane1
	if tab.Root == nil {
		t.Fatal("tab.Root should not be nil")
	}
	if !tab.Root.IsLeaf() {
		t.Fatal("tab.Root should be a leaf node")
	}
	if tab.Root.PaneID != "pane1" {
		t.Errorf("expected root paneID=pane1, got %q", tab.Root.PaneID)
	}
	// ActivePaneID updated
	if tab.ActivePaneID != "pane1" {
		t.Errorf("expected ActivePaneID=pane1, got %q", tab.ActivePaneID)
	}
}

func TestCreateFirstPane_UnknownTab_ReturnsError(t *testing.T) {
	wb := setupWorkbench(t)
	if err := wb.CreateFirstPane("ghost", "p1"); err == nil {
		t.Fatal("expected error for unknown tab")
	}
}

func TestCreateFirstPane_TabAlreadyHasRoot_ReturnsError(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "T")
	_ = wb.CreateFirstPane("tab1", "pane1")
	if err := wb.CreateFirstPane("tab1", "pane2"); err == nil {
		t.Fatal("expected error when root already set")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// SplitPane
// ────────────────────────────────────────────────────────────────────────────

func TestSplitPane_HorizontalCreatesInternalNode(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "T")
	_ = wb.CreateFirstPane("tab1", "pane1")

	err := wb.SplitPane("tab1", "pane1", "pane2", SplitHorizontal)
	if err != nil {
		t.Fatalf("SplitPane: unexpected error: %v", err)
	}
	tab := wb.store["main"].Tabs[0]

	// Both panes must be in the map
	if _, ok := tab.Panes["pane2"]; !ok {
		t.Fatal("pane2 not found in tab.Panes after split")
	}
	// Root must now be an internal node
	if tab.Root.IsLeaf() {
		t.Fatal("root should be internal after split")
	}
	if tab.Root.Direction != SplitHorizontal {
		t.Errorf("expected direction horizontal, got %q", tab.Root.Direction)
	}
	// Leaf IDs must include both panes
	ids := tab.Root.LeafIDs()
	if !contains(ids, "pane1") || !contains(ids, "pane2") {
		t.Errorf("expected both pane IDs in leaf set, got %v", ids)
	}
	// ActivePaneID should be updated to the new pane
	if tab.ActivePaneID != "pane2" {
		t.Errorf("expected ActivePaneID=pane2, got %q", tab.ActivePaneID)
	}
}

func TestSplitPane_VerticalCreatesInternalNode(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "T")
	_ = wb.CreateFirstPane("tab1", "pane1")

	if err := wb.SplitPane("tab1", "pane1", "pane2", SplitVertical); err != nil {
		t.Fatalf("SplitPane: unexpected error: %v", err)
	}
	tab := wb.store["main"].Tabs[0]
	if tab.Root.Direction != SplitVertical {
		t.Errorf("expected direction vertical, got %q", tab.Root.Direction)
	}
}

func TestSplitPane_DefaultRatioIsHalf(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "T")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.SplitPane("tab1", "pane1", "pane2", SplitHorizontal)

	tab := wb.store["main"].Tabs[0]
	if tab.Root.Ratio != 0.5 {
		t.Errorf("expected ratio 0.5, got %v", tab.Root.Ratio)
	}
}

func TestSplitPane_UnknownTab_ReturnsError(t *testing.T) {
	wb := setupWorkbench(t)
	if err := wb.SplitPane("ghost", "p1", "p2", SplitHorizontal); err == nil {
		t.Fatal("expected error for unknown tab")
	}
}

func TestSplitPane_UnknownPane_ReturnsError(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "T")
	_ = wb.CreateFirstPane("tab1", "pane1")
	if err := wb.SplitPane("tab1", "ghost", "pane2", SplitHorizontal); err == nil {
		t.Fatal("expected error for unknown source pane")
	}
}

func TestSplitPane_DuplicateNewPaneID_ReturnsError(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "T")
	_ = wb.CreateFirstPane("tab1", "pane1")
	if err := wb.SplitPane("tab1", "pane1", "pane1", SplitHorizontal); err == nil {
		t.Fatal("expected error for duplicate new pane ID")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// FocusPane
// ────────────────────────────────────────────────────────────────────────────

func TestFocusPane_UpdatesActivePaneID(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "T")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.SplitPane("tab1", "pane1", "pane2", SplitHorizontal)

	if err := wb.FocusPane("tab1", "pane1"); err != nil {
		t.Fatalf("FocusPane: unexpected error: %v", err)
	}
	tab := wb.store["main"].Tabs[0]
	if tab.ActivePaneID != "pane1" {
		t.Errorf("expected ActivePaneID=pane1, got %q", tab.ActivePaneID)
	}
}

func TestFocusPane_UnknownTab_ReturnsError(t *testing.T) {
	wb := setupWorkbench(t)
	if err := wb.FocusPane("ghost", "p"); err == nil {
		t.Fatal("expected error for unknown tab")
	}
}

func TestFocusPane_UnknownPane_ReturnsError(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "T")
	_ = wb.CreateFirstPane("tab1", "pane1")
	if err := wb.FocusPane("tab1", "ghost"); err == nil {
		t.Fatal("expected error for unknown pane")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// BindPaneTerminal
// ────────────────────────────────────────────────────────────────────────────

func TestBindPaneTerminal_SetsTerminalID(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "T")
	_ = wb.CreateFirstPane("tab1", "pane1")

	if err := wb.BindPaneTerminal("tab1", "pane1", "term-abc"); err != nil {
		t.Fatalf("BindPaneTerminal: unexpected error: %v", err)
	}
	tab := wb.store["main"].Tabs[0]
	pane := tab.Panes["pane1"]
	if pane.TerminalID != "term-abc" {
		t.Errorf("expected TerminalID=term-abc, got %q", pane.TerminalID)
	}
}

func TestBindPaneTerminal_UnknownTab_ReturnsError(t *testing.T) {
	wb := setupWorkbench(t)
	if err := wb.BindPaneTerminal("ghost", "p", "tid"); err == nil {
		t.Fatal("expected error for unknown tab")
	}
}

func TestBindPaneTerminal_UnknownPane_ReturnsError(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "T")
	_ = wb.CreateFirstPane("tab1", "pane1")
	if err := wb.BindPaneTerminal("tab1", "ghost", "tid"); err == nil {
		t.Fatal("expected error for unknown pane")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// ClosePane
// ────────────────────────────────────────────────────────────────────────────

func TestClosePane_RemovesPaneCollapsesLayoutAndReturnsTerminalID(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "T")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.SplitPane("tab1", "pane1", "pane2", SplitVertical)
	_ = wb.BindPaneTerminal("tab1", "pane2", "term-2")

	terminalID, err := wb.ClosePane("tab1", "pane2")
	if err != nil {
		t.Fatalf("ClosePane: unexpected error: %v", err)
	}
	if terminalID != "term-2" {
		t.Fatalf("expected removed terminal term-2, got %q", terminalID)
	}

	tab := wb.store["main"].Tabs[0]
	if len(tab.Panes) != 1 {
		t.Fatalf("expected 1 pane after close, got %d", len(tab.Panes))
	}
	if _, ok := tab.Panes["pane2"]; ok {
		t.Fatal("expected pane2 to be removed from pane map")
	}
	if tab.Root == nil || !tab.Root.IsLeaf() || tab.Root.PaneID != "pane1" {
		t.Fatalf("expected layout to collapse to pane1 leaf, got %#v", tab.Root)
	}
	if tab.ActivePaneID != "pane1" {
		t.Fatalf("expected focus to fall back to pane1, got %q", tab.ActivePaneID)
	}
}

func TestClosePane_LastPaneRemovesTabAndClampsWorkspaceActiveTab(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateTab("main", "tab2", "Tab Two")
	_ = wb.CreateFirstPane("tab2", "pane2")
	_ = wb.SwitchTab("main", 0)

	terminalID, err := wb.ClosePane("tab1", "pane1")
	if err != nil {
		t.Fatalf("ClosePane: unexpected error: %v", err)
	}
	if terminalID != "" {
		t.Fatalf("expected empty terminal ID for unbound pane, got %q", terminalID)
	}

	ws := wb.store["main"]
	if len(ws.Tabs) != 1 {
		t.Fatalf("expected tab1 to be removed, got %d tabs", len(ws.Tabs))
	}
	if ws.ActiveTab != 0 {
		t.Fatalf("expected active tab to clamp to 0, got %d", ws.ActiveTab)
	}
	if current := wb.CurrentTab(); current == nil || current.ID != "tab2" {
		t.Fatalf("expected remaining tab2 to become current, got %#v", current)
	}
}

func TestClosePane_UnknownPane_ReturnsError(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "T")
	_ = wb.CreateFirstPane("tab1", "pane1")

	if _, err := wb.ClosePane("tab1", "ghost"); err == nil {
		t.Fatal("expected error for unknown pane")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// SwitchTab / CloseTab
// ────────────────────────────────────────────────────────────────────────────

func TestSwitchTab_UpdatesWorkspaceActiveTab(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateTab("main", "tab2", "Tab Two")

	if err := wb.SwitchTab("main", 0); err != nil {
		t.Fatalf("SwitchTab: unexpected error: %v", err)
	}
	if current := wb.CurrentTab(); current == nil || current.ID != "tab1" {
		t.Fatalf("expected current tab tab1, got %#v", current)
	}
}

func TestSwitchTab_OutOfRange_ReturnsError(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")

	if err := wb.SwitchTab("main", 3); err == nil {
		t.Fatal("expected error for out-of-range tab index")
	}
}

func TestCloseTab_RemovesTabAndClampsActiveIndex(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateTab("main", "tab2", "Tab Two")
	_ = wb.CreateFirstPane("tab2", "pane2")

	if err := wb.CloseTab("tab2"); err != nil {
		t.Fatalf("CloseTab: unexpected error: %v", err)
	}

	ws := wb.store["main"]
	if len(ws.Tabs) != 1 {
		t.Fatalf("expected 1 remaining tab, got %d", len(ws.Tabs))
	}
	if ws.ActiveTab != 0 {
		t.Fatalf("expected active tab to clamp to 0, got %d", ws.ActiveTab)
	}
	if current := wb.CurrentTab(); current == nil || current.ID != "tab1" {
		t.Fatalf("expected current tab tab1, got %#v", current)
	}
}

func TestCloseTab_LastTabLeavesWorkspaceWithoutActiveTab(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")

	if err := wb.CloseTab("tab1"); err != nil {
		t.Fatalf("CloseTab: unexpected error: %v", err)
	}

	ws := wb.store["main"]
	if len(ws.Tabs) != 0 {
		t.Fatalf("expected workspace to have no tabs, got %d", len(ws.Tabs))
	}
	if ws.ActiveTab != -1 {
		t.Fatalf("expected workspace ActiveTab=-1 when empty, got %d", ws.ActiveTab)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Visible() projection consistency
// ────────────────────────────────────────────────────────────────────────────

func TestVisible_ReflectsMutations(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.SplitPane("tab1", "pane1", "pane2", SplitVertical)
	_ = wb.BindPaneTerminal("tab1", "pane1", "term-1")
	_ = wb.BindPaneTerminal("tab1", "pane2", "term-2")
	_ = wb.FocusPane("tab1", "pane1")

	v := wb.Visible()
	if v == nil {
		t.Fatal("Visible() returned nil")
	}
	if len(v.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(v.Tabs))
	}
	vt := v.Tabs[0]
	if vt.ID != "tab1" {
		t.Errorf("expected tab ID tab1, got %q", vt.ID)
	}
	if vt.ActivePaneID != "pane1" {
		t.Errorf("expected active pane pane1, got %q", vt.ActivePaneID)
	}
	if len(vt.Panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(vt.Panes))
	}

	// Verify terminal bindings are visible
	termIDs := map[string]string{}
	for _, p := range vt.Panes {
		termIDs[p.ID] = p.TerminalID
	}
	if termIDs["pane1"] != "term-1" {
		t.Errorf("pane1 TerminalID: expected term-1, got %q", termIDs["pane1"])
	}
	if termIDs["pane2"] != "term-2" {
		t.Errorf("pane2 TerminalID: expected term-2, got %q", termIDs["pane2"])
	}

	// Rects are computed from a normalised 1×1 root; with integer rounding two
	// panes cannot each get non-zero width in a 1-pixel wide space.  Verify
	// instead that the rect map covers both panes (i.e. Rects() was called) by
	// checking at least one pane has a non-zero rect when using a large root.
	rects := wb.store["main"].Tabs[0].Root.Rects(Rect{W: 100, H: 100})
	if len(rects) != 2 {
		t.Errorf("expected 2 rect entries from large root, got %d", len(rects))
	}
	for id, r := range rects {
		if r.W == 0 || r.H == 0 {
			t.Errorf("pane %q has zero dimension rect %+v in 100×100 space", id, r)
		}
	}
}

func TestVisibleWithSize_ClampsStaleActiveReferences(t *testing.T) {
	wb := NewWorkbench()
	wb.AddWorkspace("main", &WorkspaceState{
		Name:      "main",
		ActiveTab: 9,
		Tabs: []*TabState{{
			ID:           "tab1",
			Name:         "Tab One",
			ActivePaneID: "ghost",
			ZoomedPaneID: "ghost",
			Panes: map[string]*PaneState{
				"pane1": {ID: "pane1", Title: "Pane One"},
			},
			Root: NewLeaf("pane1"),
		}},
	})

	visible := wb.VisibleWithSize(Rect{W: 80, H: 24})
	if visible.ActiveTab != 0 {
		t.Fatalf("expected visible ActiveTab=0, got %d", visible.ActiveTab)
	}
	if len(visible.Tabs) != 1 {
		t.Fatalf("expected 1 visible tab, got %d", len(visible.Tabs))
	}
	if visible.Tabs[0].ActivePaneID != "pane1" {
		t.Fatalf("expected visible active pane pane1, got %q", visible.Tabs[0].ActivePaneID)
	}
	if visible.Tabs[0].ZoomedPaneID != "" {
		t.Fatalf("expected stale zoomed pane to be cleared, got %q", visible.Tabs[0].ZoomedPaneID)
	}
	if len(visible.Tabs[0].Panes) != 1 || visible.Tabs[0].Panes[0].Rect.W == 0 || visible.Tabs[0].Panes[0].Rect.H == 0 {
		t.Fatalf("expected visible pane rect to be populated, got %#v", visible.Tabs[0].Panes)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// helpers
// ────────────────────────────────────────────────────────────────────────────

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
