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

func TestRenameTab_UpdatesName(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Old")

	if err := wb.RenameTab("tab1", "New"); err != nil {
		t.Fatalf("RenameTab: unexpected error: %v", err)
	}
	if current := wb.CurrentTab(); current == nil || current.Name != "New" {
		t.Fatalf("expected renamed current tab, got %#v", current)
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

func TestDividerAtReturnsNestedSplitBounds(t *testing.T) {
	root := &LayoutNode{
		Direction: SplitVertical,
		Ratio:     0.5,
		First: &LayoutNode{
			Direction: SplitHorizontal,
			Ratio:     0.5,
			First:     NewLeaf("pane1"),
			Second:    NewLeaf("pane2"),
		},
		Second: NewLeaf("pane3"),
	}

	hit, ok := root.DividerAt(Rect{W: 100, H: 40}, 20, 19)
	if !ok {
		t.Fatal("expected nested horizontal divider hit")
	}
	if hit.Node != root.First {
		t.Fatalf("expected nested split node, got %#v", hit.Node)
	}
	if hit.Root != (Rect{X: 0, Y: 0, W: 50, H: 40}) {
		t.Fatalf("expected nested split bounds, got %#v", hit.Root)
	}
	if hit.Rect != (Rect{X: 0, Y: 19, W: 50, H: 2}) {
		t.Fatalf("expected nested divider rect, got %#v", hit.Rect)
	}
}

func TestResizeSplitUpdatesSiblingRects(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.SplitPane("tab1", "pane1", "pane2", SplitVertical)

	tab := wb.store["main"].Tabs[0]
	hit, ok := tab.Root.DividerAt(Rect{W: 100, H: 40}, 49, 10)
	if !ok {
		t.Fatal("expected root divider hit")
	}
	if !wb.ResizeSplit("tab1", hit.Node, hit.Root, 39, 10, 0, 0) {
		t.Fatal("expected resize split to report change")
	}

	rects := tab.Root.Rects(Rect{W: 100, H: 40})
	if rects["pane1"].W != 40 || rects["pane2"].W != 60 {
		t.Fatalf("expected 40/60 widths after drag, got pane1=%#v pane2=%#v", rects["pane1"], rects["pane2"])
	}
}

func TestResizeSplitHonorsDividerOffsetWithoutJitter(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.SplitPane("tab1", "pane1", "pane2", SplitVertical)

	tab := wb.store["main"].Tabs[0]
	hit, ok := tab.Root.DividerAt(Rect{W: 100, H: 40}, 50, 10)
	if !ok {
		t.Fatal("expected root divider hit")
	}
	offsetX := 50 - hit.Rect.X
	if wb.ResizeSplit("tab1", hit.Node, hit.Root, 50, 10, offsetX, 0) {
		t.Fatal("expected no-op resize when divider does not move")
	}
	if tab.Root.Ratio != 0.5 {
		t.Fatalf("expected ratio to stay at 0.5, got %f", tab.Root.Ratio)
	}
}

func TestReorderFloatingPaneUpdatesZOrderMetadata(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateFloatingPane("tab1", "pane2", Rect{X: 1, Y: 1, W: 20, H: 8})
	_ = wb.CreateFloatingPane("tab1", "pane3", Rect{X: 4, Y: 3, W: 16, H: 6})

	if !wb.ReorderFloatingPane("tab1", "pane2", true) {
		t.Fatal("expected reorder to succeed")
	}

	tab := wb.store["main"].Tabs[0]
	if len(tab.Floating) != 2 {
		t.Fatalf("expected 2 floating panes, got %#v", tab.Floating)
	}
	if tab.Floating[0].PaneID != "pane3" || tab.Floating[0].Z != 0 {
		t.Fatalf("expected pane3 to stay behind with z=0, got %#v", tab.Floating[0])
	}
	if tab.Floating[1].PaneID != "pane2" || tab.Floating[1].Z != 1 {
		t.Fatalf("expected pane2 to move to top with z=1, got %#v", tab.Floating[1])
	}
}

func TestCreateFloatingPaneCascadesOverlappingPlacement(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateFloatingPane("tab1", "pane2", Rect{})
	_ = wb.CreateFloatingPane("tab1", "pane3", Rect{})

	tab := wb.store["main"].Tabs[0]
	if len(tab.Floating) != 2 {
		t.Fatalf("expected 2 floating panes, got %#v", tab.Floating)
	}
	first := tab.Floating[0].Rect
	second := tab.Floating[1].Rect
	if first == second {
		t.Fatalf("expected cascaded placement to avoid exact overlap, got %#v", second)
	}
	if overlap := rectOverlapArea(first, second); overlap >= first.W*first.H {
		t.Fatalf("expected less than full overlap after cascade, overlap=%d first=%#v second=%#v", overlap, first, second)
	}
}

func TestNextFloatingRectFindsNonOverlappingCandidate(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateFloatingPane("tab1", "pane2", Rect{X: 10, Y: 5, W: 20, H: 8})

	next, err := wb.NextFloatingRect("tab1", Rect{X: 10, Y: 5, W: 20, H: 8}, Rect{})
	if err != nil {
		t.Fatalf("NextFloatingRect: unexpected error: %v", err)
	}
	if next == (Rect{X: 10, Y: 5, W: 20, H: 8}) {
		t.Fatalf("expected next floating rect to move away from existing one, got %#v", next)
	}
	if overlap := rectOverlapArea(next, Rect{X: 10, Y: 5, W: 20, H: 8}); overlap != 0 {
		t.Fatalf("expected non-overlapping candidate, overlap=%d rect=%#v", overlap, next)
	}
}

func TestSetFloatingPaneDisplayRestoresRectWhenExpanded(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateFloatingPane("tab1", "pane2", Rect{X: 10, Y: 5, W: 20, H: 8})

	if !wb.MoveFloatingPane("tab1", "pane2", 12, 6) {
		t.Fatal("expected initial move to succeed")
	}
	tab := wb.store["main"].Tabs[0]
	if tab.Floating[0].Rect != (Rect{X: 12, Y: 6, W: 20, H: 8}) {
		t.Fatalf("unexpected rect after move: %#v", tab.Floating[0].Rect)
	}

	if !wb.SetFloatingPaneDisplay("tab1", "pane2", FloatingDisplayCollapsed) {
		t.Fatal("expected collapse to change state")
	}
	if tab.Floating[0].Display != FloatingDisplayCollapsed {
		t.Fatalf("expected collapsed display state, got %#v", tab.Floating[0].Display)
	}
	if tab.Floating[0].RestoreRect != (Rect{X: 12, Y: 6, W: 20, H: 8}) {
		t.Fatalf("expected restore rect to capture expanded rect, got %#v", tab.Floating[0].RestoreRect)
	}

	// Moving while collapsed should not rewrite restore geometry.
	if !wb.MoveFloatingPane("tab1", "pane2", 40, 20) {
		t.Fatal("expected move while collapsed to succeed")
	}
	if tab.Floating[0].RestoreRect != (Rect{X: 12, Y: 6, W: 20, H: 8}) {
		t.Fatalf("expected restore rect to remain stable while collapsed, got %#v", tab.Floating[0].RestoreRect)
	}

	if !wb.SetFloatingPaneDisplay("tab1", "pane2", FloatingDisplayExpanded) {
		t.Fatal("expected expand to change state")
	}
	if tab.Floating[0].Display != FloatingDisplayExpanded {
		t.Fatalf("expected expanded display state, got %#v", tab.Floating[0].Display)
	}
	if tab.Floating[0].Rect != (Rect{X: 12, Y: 6, W: 20, H: 8}) {
		t.Fatalf("expected expand to restore previous rect, got %#v", tab.Floating[0].Rect)
	}
}

func TestSetFloatingPaneFitModeAndAutoFitSize(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateFloatingPane("tab1", "pane2", Rect{X: 8, Y: 4, W: 24, H: 10})

	if !wb.SetFloatingPaneFitMode("tab1", "pane2", FloatingFitAuto) {
		t.Fatal("expected fit mode update to auto")
	}
	if !wb.SetFloatingPaneAutoFitSize("tab1", "pane2", 120, 40) {
		t.Fatal("expected auto-fit size metadata update")
	}

	tab := wb.store["main"].Tabs[0]
	if tab.Floating[0].FitMode != FloatingFitAuto {
		t.Fatalf("expected auto fit mode, got %#v", tab.Floating[0].FitMode)
	}
	if tab.Floating[0].AutoFitCols != 120 || tab.Floating[0].AutoFitRows != 40 {
		t.Fatalf("expected auto-fit metadata 120x40, got cols=%d rows=%d", tab.Floating[0].AutoFitCols, tab.Floating[0].AutoFitRows)
	}

	if !wb.SetFloatingPaneFitMode("tab1", "pane2", FloatingFitManual) {
		t.Fatal("expected fit mode update to manual")
	}
	if tab.Floating[0].AutoFitCols != 0 || tab.Floating[0].AutoFitRows != 0 {
		t.Fatalf("expected manual fit mode to clear auto-fit metadata, got cols=%d rows=%d", tab.Floating[0].AutoFitCols, tab.Floating[0].AutoFitRows)
	}
}

func TestSetFloatingPaneDisplayRestorePreservesPositionWhenUnobstructed(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateFloatingPane("tab1", "pane2", Rect{X: 12, Y: 6, W: 20, H: 8})

	if !wb.SetFloatingPaneDisplay("tab1", "pane2", FloatingDisplayCollapsed) {
		t.Fatal("expected collapse to succeed")
	}
	if !wb.SetFloatingPaneDisplay("tab1", "pane2", FloatingDisplayExpanded) {
		t.Fatal("expected re-expand to succeed")
	}

	tab := wb.store["main"].Tabs[0]
	if got := tab.Floating[0].Rect; got != (Rect{X: 12, Y: 6, W: 20, H: 8}) {
		t.Fatalf("expected re-expand to preserve original rect, got %#v", got)
	}
}

func TestSetFloatingPaneDisplayRestoreOffsetsWhenPositionConflicts(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateFloatingPane("tab1", "pane2", Rect{X: 12, Y: 6, W: 20, H: 8})
	_ = wb.CreateFloatingPane("tab1", "pane3", Rect{X: 40, Y: 6, W: 20, H: 8})

	if !wb.MoveFloatingPane("tab1", "pane3", 11, 6) {
		t.Fatal("expected move to overlapping position to succeed")
	}
	if !wb.SetFloatingPaneDisplay("tab1", "pane2", FloatingDisplayCollapsed) {
		t.Fatal("expected collapse to succeed")
	}
	if !wb.SetFloatingPaneDisplay("tab1", "pane2", FloatingDisplayExpanded) {
		t.Fatal("expected re-expand to succeed")
	}

	tab := wb.store["main"].Tabs[0]
	got := tab.Floating[0].Rect
	if got == (Rect{X: 12, Y: 6, W: 20, H: 8}) {
		t.Fatalf("expected conflicting restore to offset from original rect, got %#v", got)
	}
	if got.X <= 12 {
		t.Fatalf("expected conflicting restore to cascade far enough to show title, got %#v", got)
	}
	if tab.Floating[0].RestoreRect != (Rect{X: 12, Y: 6, W: 20, H: 8}) {
		t.Fatalf("expected restore rect to preserve user's original position, got %#v", tab.Floating[0].RestoreRect)
	}
}

func TestSetFloatingPaneDisplayRestoreKeepsPositionWhenOnlyBodiesOverlap(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateFloatingPane("tab1", "pane2", Rect{X: 12, Y: 6, W: 20, H: 8})
	_ = wb.CreateFloatingPane("tab1", "pane3", Rect{X: 16, Y: 10, W: 20, H: 8})

	if !wb.SetFloatingPaneDisplay("tab1", "pane2", FloatingDisplayCollapsed) {
		t.Fatal("expected collapse to succeed")
	}
	if !wb.SetFloatingPaneDisplay("tab1", "pane2", FloatingDisplayExpanded) {
		t.Fatal("expected re-expand to succeed")
	}

	tab := wb.store["main"].Tabs[0]
	if got := tab.Floating[0].Rect; got != (Rect{X: 12, Y: 6, W: 20, H: 8}) {
		t.Fatalf("expected body-only overlap to preserve original rect, got %#v", got)
	}
}

func TestExpandAllFloatingPanesPreservesRestoreRectUntilConflict(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateFloatingPane("tab1", "pane2", Rect{X: 12, Y: 6, W: 20, H: 8})
	_ = wb.CreateFloatingPane("tab1", "pane3", Rect{X: 40, Y: 6, W: 20, H: 8})

	if !wb.MoveFloatingPane("tab1", "pane3", 11, 6) {
		t.Fatal("expected move to overlapping position to succeed")
	}
	if !wb.CollapseAllFloatingPanes("tab1") {
		t.Fatal("expected collapse-all to succeed")
	}
	if !wb.ExpandAllFloatingPanes("tab1") {
		t.Fatal("expected expand-all to succeed")
	}

	tab := wb.store["main"].Tabs[0]
	if got := tab.Floating[0].Rect; got == (Rect{X: 12, Y: 6, W: 20, H: 8}) {
		t.Fatalf("expected lower floating pane to offset because its title is fully occluded, got %#v", got)
	}
	if tab.Floating[0].RestoreRect != (Rect{X: 12, Y: 6, W: 20, H: 8}) {
		t.Fatalf("expected lower floating pane restore rect to remain original, got %#v", tab.Floating[0].RestoreRect)
	}
	if got := tab.Floating[1].Rect; got != (Rect{X: 11, Y: 6, W: 20, H: 8}) {
		t.Fatalf("expected top floating pane to preserve original overlapping position because its title stays visible, got %#v", got)
	}
	if tab.Floating[1].RestoreRect != (Rect{X: 11, Y: 6, W: 20, H: 8}) {
		t.Fatalf("expected top floating pane restore rect to remain original, got %#v", tab.Floating[1].RestoreRect)
	}
}

func TestMoveFloatingPaneByClampsAtOrigin(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateFloatingPane("tab1", "pane2", Rect{X: 3, Y: 2, W: 20, H: 8})

	if !wb.MoveFloatingPaneBy("tab1", "pane2", -10, -10) {
		t.Fatal("expected move by to succeed")
	}

	tab := wb.store["main"].Tabs[0]
	if tab.Floating[0].Rect.X != 0 || tab.Floating[0].Rect.Y != 0 {
		t.Fatalf("expected floating pane to clamp to origin, got %#v", tab.Floating[0])
	}
}

func TestResizeFloatingPaneByClampsMinimumSize(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateFloatingPane("tab1", "pane2", Rect{X: 3, Y: 2, W: 20, H: 8})

	if !wb.ResizeFloatingPaneBy("tab1", "pane2", -50, -50) {
		t.Fatal("expected resize by to succeed")
	}

	tab := wb.store["main"].Tabs[0]
	if tab.Floating[0].Rect.W != 10 || tab.Floating[0].Rect.H != 4 {
		t.Fatalf("expected floating pane to clamp minimum size, got %#v", tab.Floating[0])
	}
}

func TestCenterFloatingPaneCentersWithinBounds(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateFloatingPane("tab1", "pane2", Rect{X: 0, Y: 0, W: 20, H: 8})

	if !wb.CenterFloatingPane("tab1", "pane2", Rect{W: 100, H: 40}) {
		t.Fatal("expected center to succeed")
	}

	tab := wb.store["main"].Tabs[0]
	if tab.Floating[0].Rect.X != 40 || tab.Floating[0].Rect.Y != 16 {
		t.Fatalf("expected centered floating pane, got %#v", tab.Floating[0])
	}
}

func TestReflowFloatingPanesScalesRectsWithViewport(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateFloatingPane("tab1", "pane2", Rect{X: 50, Y: 10, W: 30, H: 12})

	if !wb.ReflowFloatingPanes(Rect{W: 100, H: 40}, Rect{W: 50, H: 20}) {
		t.Fatal("expected reflow to report changes")
	}

	tab := wb.store["main"].Tabs[0]
	got := tab.Floating[0].Rect
	if got != (Rect{X: 25, Y: 5, W: 15, H: 6}) {
		t.Fatalf("expected scaled floating rect, got %#v", got)
	}
}

func TestReflowFloatingPanesClampsWithinSmallerViewport(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateFloatingPane("tab1", "pane2", Rect{X: 70, Y: 30, W: 24, H: 10})

	if !wb.ReflowFloatingPanes(Rect{W: 100, H: 40}, Rect{W: 30, H: 10}) {
		t.Fatal("expected reflow to report changes")
	}

	tab := wb.store["main"].Tabs[0]
	got := tab.Floating[0].Rect
	if got.X < 0 || got.Y < 0 || got.X+got.W > 30 || got.Y+got.H > 10 {
		t.Fatalf("expected floating rect clamped inside new viewport, got %#v", got)
	}
}

func TestClampFloatingPanesToBoundsShrinksAndRepositions(t *testing.T) {
	wb := setupWorkbench(t)
	_ = wb.CreateTab("main", "tab1", "Tab One")
	_ = wb.CreateFirstPane("tab1", "pane1")
	_ = wb.CreateFloatingPane("tab1", "pane2", Rect{X: 60, Y: 20, W: 50, H: 20})

	if !wb.ClampFloatingPanesToBounds(Rect{W: 40, H: 12}) {
		t.Fatal("expected clamp to report changes")
	}

	tab := wb.store["main"].Tabs[0]
	got := tab.Floating[0].Rect
	if got != (Rect{X: 1, Y: 1, W: 39, H: 11}) {
		t.Fatalf("expected floating rect clamped into viewport, got %#v", got)
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

func TestVisibleWithSize_ZoomShowsOnlyZoomedPane(t *testing.T) {
	wb := NewWorkbench()
	wb.AddWorkspace("main", &WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*TabState{{
			ID:           "tab1",
			Name:         "Tab One",
			ActivePaneID: "pane1",
			ZoomedPaneID: "pane1",
			Panes: map[string]*PaneState{
				"pane1":  {ID: "pane1", Title: "Pane One", TerminalID: "term-1"},
				"pane2":  {ID: "pane2", Title: "Pane Two", TerminalID: "term-2"},
				"float1": {ID: "float1", Title: "Float One", TerminalID: "term-3"},
			},
			Root: &LayoutNode{
				Direction: SplitVertical,
				Ratio:     0.5,
				First:     NewLeaf("pane1"),
				Second:    NewLeaf("pane2"),
			},
			FloatingVisible: true,
			Floating: []*FloatingState{{
				PaneID:  "float1",
				Rect:    Rect{X: 10, Y: 5, W: 20, H: 8},
				Display: FloatingDisplayExpanded,
			}},
		}},
	})

	visible := wb.VisibleWithSize(Rect{W: 80, H: 24})
	if len(visible.Tabs) != 1 {
		t.Fatalf("expected 1 visible tab, got %d", len(visible.Tabs))
	}
	if got := visible.Tabs[0].ZoomedPaneID; got != "pane1" {
		t.Fatalf("expected visible zoomed pane pane1, got %q", got)
	}
	if len(visible.Tabs[0].Panes) != 1 {
		t.Fatalf("expected only zoomed pane to remain visible, got %#v", visible.Tabs[0].Panes)
	}
	if got := visible.Tabs[0].Panes[0]; got.ID != "pane1" || got.Rect != (Rect{W: 80, H: 24}) {
		t.Fatalf("expected zoomed pane to occupy full body rect, got %#v", got)
	}
	if len(visible.FloatingPanes) != 0 {
		t.Fatalf("expected floating panes hidden during zoom, got %#v", visible.FloatingPanes)
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
