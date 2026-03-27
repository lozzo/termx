package tui

import (
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
	localvterm "github.com/lozzow/termx/vterm"
)

func TestWorkbenchOwnsWorkspaceTree(t *testing.T) {
	workspace := Workspace{
		Name:      "main",
		Tabs:      []*Tab{newTab("1")},
		ActiveTab: 0,
	}

	workbench := NewWorkbench(workspace)

	current := workbench.Current()
	if current == nil {
		t.Fatal("expected current workspace")
	}
	if current.Name != "main" {
		t.Fatalf("expected current workspace name main, got %q", current.Name)
	}

	order := workbench.Order()
	if len(order) != 1 {
		t.Fatalf("expected 1 workspace in order, got %d", len(order))
	}
	if order[0] != "main" {
		t.Fatalf("expected workspace order [main], got %v", order)
	}

	if got := workbench.ActiveWorkspaceIndex(); got != 0 {
		t.Fatalf("expected active workspace index 0, got %d", got)
	}

	workspace.Name = "mutated"
	workspace.ActiveTab = 1
	workspace.Tabs = []*Tab{newTab("2")}

	current = workbench.Current()
	if current.Name != "main" {
		t.Fatalf("expected workbench to keep original workspace name, got %q", current.Name)
	}
	if current.ActiveTab != 0 {
		t.Fatalf("expected workbench to keep active tab 0, got %d", current.ActiveTab)
	}
	if len(current.Tabs) != 1 || current.Tabs[0].Name != "1" {
		t.Fatalf("expected workbench to keep original tab tree, got %+v", current.Tabs)
	}
}

func TestNewWorkbenchClonesNestedTabState(t *testing.T) {
	root := &LayoutNode{
		Direction: SplitHorizontal,
		Ratio:     0.5,
		First:     NewLeaf("pane-1"),
		Second:    NewLeaf("pane-2"),
	}
	floating := &FloatingPane{PaneID: "pane-2", Rect: Rect{X: 3, Y: 4, W: 20, H: 8}, Z: 7}
	pane1 := &Pane{ID: "pane-1", Title: "Pane 1"}
	pane2 := &Pane{ID: "pane-2", Title: "Pane 2"}
	workspace := Workspace{
		Name: "main",
		Tabs: []*Tab{{
			Name:            "1",
			Root:            root,
			Panes:           map[string]*Pane{"pane-1": pane1, "pane-2": pane2},
			Floating:        []*FloatingPane{floating},
			FloatingVisible: true,
			ActivePaneID:    "pane-1",
		}},
		ActiveTab: 0,
	}

	workbench := NewWorkbench(workspace)

	root.First.PaneID = "mutated-root"
	floating.PaneID = "mutated-floating"
	floating.Rect.X = 99
	pane1.Title = "mutated pane title"
	pane2.ID = "mutated-pane-id"
	workspace.Tabs[0].Floating = append(workspace.Tabs[0].Floating, &FloatingPane{PaneID: "extra", Rect: Rect{X: 1, Y: 1, W: 1, H: 1}, Z: 8})

	current := workbench.Current()
	if current == nil {
		t.Fatal("expected current workspace")
	}
	if len(current.Tabs) != 1 || current.Tabs[0] == nil {
		t.Fatalf("expected single owned tab, got %+v", current.Tabs)
	}
	ownedTab := current.Tabs[0]

	if ownedTab.Root == root {
		t.Fatal("expected workbench to clone tab root")
	}
	if ownedTab.Root.First == root.First {
		t.Fatal("expected workbench to deep clone layout tree")
	}
	if ownedTab.Root.First.PaneID != "pane-1" {
		t.Fatalf("expected cloned root leaf pane-1, got %q", ownedTab.Root.First.PaneID)
	}

	if len(ownedTab.Floating) != 1 {
		t.Fatalf("expected one owned floating pane, got %d", len(ownedTab.Floating))
	}
	if ownedTab.Floating[0] == floating {
		t.Fatal("expected workbench to clone floating pane entries")
	}
	if ownedTab.Floating[0].PaneID != "pane-2" {
		t.Fatalf("expected cloned floating pane id pane-2, got %q", ownedTab.Floating[0].PaneID)
	}
	if ownedTab.Floating[0].Rect.X != 3 {
		t.Fatalf("expected cloned floating pane rect x 3, got %d", ownedTab.Floating[0].Rect.X)
	}

	if ownedTab.Panes["pane-1"] == pane1 {
		t.Fatal("expected workbench to clone pane values")
	}
	if ownedTab.Panes["pane-2"] == pane2 {
		t.Fatal("expected workbench to clone each pane value")
	}
	if ownedTab.Panes["pane-1"].Title != "Pane 1" {
		t.Fatalf("expected cloned pane title %q, got %q", "Pane 1", ownedTab.Panes["pane-1"].Title)
	}
	if ownedTab.Panes["pane-2"].ID != "pane-2" {
		t.Fatalf("expected cloned pane id %q, got %q", "pane-2", ownedTab.Panes["pane-2"].ID)
	}
}

func TestNewWorkbenchClonesPaneViewportOwnership(t *testing.T) {
	stopCalls := 0
	stopStream := func() { stopCalls++ }
	vt := localvterm.New(80, 24, 100, nil)
	snapshot := &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "a", Width: 1}}}},
		Scrollback: [][]protocol.Cell{{{Content: "b", Width: 1}}},
		Timestamp:  time.Unix(123, 0),
	}
	cellCache := [][]drawCell{{{Content: "x", Width: 1}}}
	viewportCache := [][]drawCell{{{Content: "y", Width: 1}}}
	viewport := &Viewport{
		TerminalID:      "term-1",
		Name:            "shell",
		Command:         []string{"bash"},
		Tags:            map[string]string{"role": "dev"},
		Mode:            ViewportModeFixed,
		Offset:          Point{X: 2, Y: 3},
		ResizeAcquired:  true,
		VTerm:           vt,
		Snapshot:        snapshot,
		stopStream:      stopStream,
		cellCache:       cellCache,
		cellVersion:     7,
		viewportCache:   viewportCache,
		viewportOffset:  Point{X: 4, Y: 5},
		viewportWidth:   40,
		viewportHeight:  12,
		viewportVersion: 9,
		renderDirty:     true,
		live:            true,
		syncLost:        true,
		droppedBytes:    13,
		recovering:      true,
		catchingUp:      true,
		dirtyTicks:      2,
		cleanTicks:      3,
		skipTick:        true,
		dirtyRowsKnown:  true,
		dirtyRowStart:   1,
		dirtyRowEnd:     2,
		dirtyColsKnown:  true,
		dirtyColStart:   3,
		dirtyColEnd:     4,
	}
	workspace := Workspace{
		Name: "main",
		Tabs: []*Tab{{
			Name:  "1",
			Panes: map[string]*Pane{"pane-1": {ID: "pane-1", Title: "Pane 1", Viewport: viewport}},
		}},
	}

	workbench := NewWorkbench(workspace)

	viewport.TerminalID = "mutated-term"
	viewport.Name = "mutated"
	viewport.Command[0] = "zsh"
	viewport.Tags["role"] = "ops"
	viewport.Offset.X = 99
	viewport.ResizeAcquired = false
	snapshot.TerminalID = "mutated-snapshot"
	snapshot.Screen.Cells[0][0].Content = "m"
	snapshot.Scrollback[0][0].Content = "n"
	cellCache[0][0].Content = "c"
	viewportCache[0][0].Content = "v"
	stopStream()

	ownedPane := workbench.Current().Tabs[0].Panes["pane-1"]
	if ownedPane.Viewport == viewport {
		t.Fatal("expected workbench to own a distinct viewport copy")
	}
	if ownedPane.TerminalID != "term-1" {
		t.Fatalf("expected cloned terminal id term-1, got %q", ownedPane.TerminalID)
	}
	if ownedPane.Name != "shell" {
		t.Fatalf("expected cloned viewport name shell, got %q", ownedPane.Name)
	}
	if len(ownedPane.Command) != 1 || ownedPane.Command[0] != "bash" {
		t.Fatalf("expected cloned command [bash], got %v", ownedPane.Command)
	}
	if got := ownedPane.Tags["role"]; got != "dev" {
		t.Fatalf("expected cloned tag role=dev, got %q", got)
	}
	if ownedPane.Offset.X != 2 {
		t.Fatalf("expected cloned offset x 2, got %d", ownedPane.Offset.X)
	}
	if !ownedPane.ResizeAcquired {
		t.Fatal("expected cloned resize ownership to remain true")
	}
	if ownedPane.VTerm != nil {
		t.Fatal("expected workbench-owned viewport to drop live vterm reference")
	}
	if ownedPane.Snapshot == snapshot {
		t.Fatal("expected workbench to clone snapshot data")
	}
	if ownedPane.Snapshot == nil {
		t.Fatal("expected cloned snapshot to be preserved")
	}
	if ownedPane.Snapshot.TerminalID != "term-1" {
		t.Fatalf("expected cloned snapshot terminal id term-1, got %q", ownedPane.Snapshot.TerminalID)
	}
	if ownedPane.Snapshot.Screen.Cells[0][0].Content != "a" {
		t.Fatalf("expected cloned snapshot screen cell a, got %q", ownedPane.Snapshot.Screen.Cells[0][0].Content)
	}
	if ownedPane.Snapshot.Scrollback[0][0].Content != "b" {
		t.Fatalf("expected cloned snapshot scrollback cell b, got %q", ownedPane.Snapshot.Scrollback[0][0].Content)
	}
	if ownedPane.stopStream != nil {
		t.Fatal("expected workbench-owned viewport to drop stopStream hook")
	}
	if ownedPane.cellCache != nil {
		t.Fatal("expected workbench-owned viewport to drop cell cache")
	}
	if ownedPane.viewportCache != nil {
		t.Fatal("expected workbench-owned viewport to drop viewport cache")
	}
	if ownedPane.cellVersion != 0 || ownedPane.viewportVersion != 0 {
		t.Fatalf("expected cache versions reset to zero, got cell=%d viewport=%d", ownedPane.cellVersion, ownedPane.viewportVersion)
	}
	if ownedPane.viewportWidth != 0 || ownedPane.viewportHeight != 0 {
		t.Fatalf("expected viewport cache dimensions reset, got %dx%d", ownedPane.viewportWidth, ownedPane.viewportHeight)
	}
	if ownedPane.viewportOffset != (Point{}) {
		t.Fatalf("expected viewport cache offset reset, got %+v", ownedPane.viewportOffset)
	}
	if ownedPane.renderDirty {
		t.Fatal("expected workbench-owned viewport to start with clean render state")
	}
	if ownedPane.live || ownedPane.syncLost || ownedPane.recovering || ownedPane.catchingUp || ownedPane.skipTick {
		t.Fatalf("expected runtime flags cleared, got live=%v syncLost=%v recovering=%v catchingUp=%v skipTick=%v", ownedPane.live, ownedPane.syncLost, ownedPane.recovering, ownedPane.catchingUp, ownedPane.skipTick)
	}
	if ownedPane.droppedBytes != 0 || ownedPane.dirtyTicks != 0 || ownedPane.cleanTicks != 0 {
		t.Fatalf("expected runtime counters cleared, got dropped=%d dirty=%d clean=%d", ownedPane.droppedBytes, ownedPane.dirtyTicks, ownedPane.cleanTicks)
	}
	if ownedPane.dirtyRowsKnown || ownedPane.dirtyColsKnown {
		t.Fatalf("expected dirty range tracking cleared, got rows=%v cols=%v", ownedPane.dirtyRowsKnown, ownedPane.dirtyColsKnown)
	}
	if ownedPane.dirtyRowStart != 0 || ownedPane.dirtyRowEnd != 0 || ownedPane.dirtyColStart != 0 || ownedPane.dirtyColEnd != 0 {
		t.Fatalf("expected dirty range bounds reset, got row=%d..%d col=%d..%d", ownedPane.dirtyRowStart, ownedPane.dirtyRowEnd, ownedPane.dirtyColStart, ownedPane.dirtyColEnd)
	}
	if stopCalls != 1 {
		t.Fatalf("expected source stopStream to remain callable exactly once, got %d", stopCalls)
	}
}

func TestNewWorkbenchClearsTabRenderCache(t *testing.T) {
	cache := &tabRenderCache{width: 80, height: 24, rects: map[string]Rect{"pane-1": {X: 1, Y: 2, W: 3, H: 4}}}
	workspace := Workspace{
		Name: "main",
		Tabs: []*Tab{{
			Name:        "1",
			Panes:       map[string]*Pane{"pane-1": {ID: "pane-1", Title: "Pane 1"}},
			renderCache: cache,
		}},
	}

	workbench := NewWorkbench(workspace)

	ownedTab := workbench.Current().Tabs[0]
	if ownedTab.renderCache != nil {
		t.Fatal("expected workbench-owned tab to drop caller render cache")
	}
}

func TestWorkbenchSnapshotCurrentWorkspaceUpdatesStore(t *testing.T) {
	workbench := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1")}, ActiveTab: 0})
	current := workbench.Current()
	if current == nil {
		t.Fatal("expected current workspace")
	}
	current.Tabs[0].Name = "renamed"

	workbench.SnapshotCurrent()

	order := workbench.Order()
	if len(order) != 1 || order[0] != "main" {
		t.Fatalf("expected order [main], got %v", order)
	}
	updated := workbench.Current()
	if updated == nil {
		t.Fatal("expected current workspace after snapshot")
	}
	if updated.Tabs[0].Name != "renamed" {
		t.Fatalf("expected snapshot to preserve current workspace changes, got tab %q", updated.Tabs[0].Name)
	}
}

func TestWorkbenchSetOrderPrunesStaleStoreEntriesAfterRenameAndDelete(t *testing.T) {
	workbench := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1")}, ActiveTab: 0})

	if err := workbench.SwitchTo("workspace-2"); err != nil {
		t.Fatalf("expected switch to workspace-2, got %v", err)
	}
	current := workbench.Current()
	if current == nil {
		t.Fatal("expected current workspace")
	}
	current.Name = "dev"
	workbench.SnapshotCurrent()

	store := workbench.CloneStore()
	if _, exists := store["workspace-2"]; exists {
		t.Fatalf("expected rename snapshot to prune workspace-2 entry, store=%v", store)
	}
	if _, exists := store["dev"]; !exists {
		t.Fatalf("expected rename snapshot to store dev entry, store=%v", store)
	}

	workbench.SetOrder([]string{"main"})

	store = workbench.CloneStore()
	if _, exists := store["dev"]; exists {
		t.Fatalf("expected SetOrder to prune dev entry removed from order, store=%v", store)
	}
	if order := workbench.Order(); len(order) != 1 || order[0] != "main" {
		t.Fatalf("expected order [main], got %v", order)
	}
	if current := workbench.Current(); current == nil || current.Name != "main" {
		t.Fatalf("expected current workspace main after pruning, got %#v", current)
	}
}

func TestWorkbenchSwitchWorkspaceChangesCurrent(t *testing.T) {
	workbench := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1")}, ActiveTab: 0})
	workbench.SetOrder([]string{"main", "shared"})
	current := workbench.Current()
	if current == nil {
		t.Fatal("expected current workspace")
	}
	current.Tabs[0].Name = "main-tab"
	workbench.SnapshotCurrent()

	if err := workbench.SwitchTo("shared"); err != nil {
		t.Fatalf("expected switch to shared workspace, got %v", err)
	}
	if got := workbench.ActiveWorkspaceIndex(); got != 1 {
		t.Fatalf("expected active workspace index 1, got %d", got)
	}
	shared := workbench.Current()
	if shared == nil || shared.Name != "shared" {
		t.Fatalf("expected current workspace shared, got %#v", shared)
	}
	if len(shared.Tabs) != 1 || shared.Tabs[0].Name != "1" {
		t.Fatalf("expected switched workspace to initialize default tab, got %#v", shared.Tabs)
	}

	shared.Tabs[0].Name = "shared-tab"
	workbench.SnapshotCurrent()

	if err := workbench.SwitchTo("main"); err != nil {
		t.Fatalf("expected switch back to main workspace, got %v", err)
	}
	main := workbench.Current()
	if main == nil || main.Name != "main" {
		t.Fatalf("expected current workspace main, got %#v", main)
	}
	if len(main.Tabs) != 1 || main.Tabs[0].Name != "main-tab" {
		t.Fatalf("expected switched-back workspace to restore stored snapshot, got %#v", main.Tabs)
	}
}

func TestWorkbenchCurrentTabReturnsActiveTab(t *testing.T) {
	workbench := NewWorkbench(Workspace{
		Name: "main",
		Tabs: []*Tab{
			{Name: "1"},
			{Name: "2"},
		},
		ActiveTab: 1,
	})

	tab := workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if tab.Name != "2" {
		t.Fatalf("expected active tab 2, got %q", tab.Name)
	}
}

func TestWorkbenchActivePaneReturnsWorkspaceActivePane(t *testing.T) {
	workbench := NewWorkbench(Workspace{
		Name: "main",
		Tabs: []*Tab{{
			Name:         "1",
			Panes:        map[string]*Pane{"pane-1": {ID: "pane-1", Title: "Pane 1"}, "pane-2": {ID: "pane-2", Title: "Pane 2"}},
			ActivePaneID: "pane-2",
		}},
		ActiveTab: 0,
	})

	pane := workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected active pane")
	}
	if pane.ID != "pane-2" {
		t.Fatalf("expected active pane pane-2, got %q", pane.ID)
	}
}

func TestWorkbenchVisibleStateExposesCurrentTabAndActivePane(t *testing.T) {
	workbench := NewWorkbench(Workspace{
		Name: "main",
		Tabs: []*Tab{
			{Name: "1", Panes: map[string]*Pane{"pane-1": {ID: "pane-1", Title: "Pane 1"}}, ActivePaneID: "pane-1"},
			{Name: "2", Panes: map[string]*Pane{"pane-2": {ID: "pane-2", Title: "Pane 2"}}, ActivePaneID: "pane-2"},
		},
		ActiveTab: 1,
	})

	visible := workbench.VisibleState()
	if visible.Workspace == nil {
		t.Fatal("expected visible workspace")
	}
	if visible.Tab == nil {
		t.Fatal("expected visible tab")
	}
	if visible.ActivePane == nil {
		t.Fatal("expected visible active pane")
	}
	if visible.Workspace.Name != "main" {
		t.Fatalf("expected visible workspace main, got %q", visible.Workspace.Name)
	}
	if visible.Tab.Name != "2" {
		t.Fatalf("expected visible tab 2, got %q", visible.Tab.Name)
	}
	if visible.ActivePane.ID != "pane-2" {
		t.Fatalf("expected visible active pane pane-2, got %q", visible.ActivePane.ID)
	}
	if visible.Tab != visible.Workspace.Tabs[visible.Workspace.ActiveTab] {
		t.Fatal("expected visible tab to match active workspace tab")
	}
	if visible.ActivePane != visible.Tab.Panes[visible.Tab.ActivePaneID] {
		t.Fatal("expected visible active pane to match tab active pane")
	}
}

func TestWorkbenchActivateTabDelegatesToWorkspace(t *testing.T) {
	workbench := NewWorkbench(Workspace{
		Name: "main",
		Tabs: []*Tab{{Name: "1"}, {Name: "2"}},
		ActiveTab: 0,
	})

	if !workbench.ActivateTab(1) {
		t.Fatal("expected activate tab to succeed")
	}
	if workbench.CurrentWorkspace().ActiveTab != 1 {
		t.Fatalf("expected active tab 1, got %d", workbench.CurrentWorkspace().ActiveTab)
	}
}

func TestWorkbenchFocusPaneDelegatesToWorkspace(t *testing.T) {
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

	if !workbench.FocusPane("p2") {
		t.Fatal("expected focus pane to succeed")
	}
	if workbench.CurrentTab().ActivePaneID != "p2" {
		t.Fatalf("expected active pane p2, got %q", workbench.CurrentTab().ActivePaneID)
	}
}

func TestWorkbenchRemovePaneDelegatesToWorkspace(t *testing.T) {
	tab := &Tab{
		Name:         "1",
		Panes:        map[string]*Pane{"pane-1": {ID: "pane-1", Title: "Pane 1", Viewport: &Viewport{}}},
		ActivePaneID: "pane-1",
		Root:         NewLeaf("pane-1"),
	}
	workbench := NewWorkbench(Workspace{
		Name:      "main",
		Tabs:      []*Tab{tab},
		ActiveTab: 0,
	})

	tabRemoved, workspaceEmpty, terminalID := workbench.RemovePane("pane-1")
	if !tabRemoved {
		t.Fatal("expected pane removal to remove the tab")
	}
	if !workspaceEmpty {
		t.Fatal("expected pane removal to empty the workspace")
	}
	if terminalID != "" {
		t.Fatalf("expected empty terminal id for unbound pane, got %q", terminalID)
	}
	if current := workbench.CurrentWorkspace(); current == nil || len(current.Tabs) != 0 {
		t.Fatalf("expected workbench current workspace to reflect pane removal, got %#v", current)
	}
}
