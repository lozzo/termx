package app

import (
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestTerminalViewportRectKeepsDistinctPaneEdges(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			ActivePaneID: "pane-2",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "left"},
				"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-2"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})

	model := New(shared.Config{}, wb, nil)
	model.width = 80
	model.height = 26

	visible := wb.VisibleWithSize(model.bodyRect())
	if visible == nil || visible.ActiveTab < 0 {
		t.Fatalf("unexpected visible workbench: %#v", visible)
	}
	right := visible.Tabs[visible.ActiveTab].Panes[1]

	viewport, ok := model.terminalViewportRect(right.ID, right.Rect)
	if !ok {
		t.Fatal("expected terminal viewport rect")
	}
	want, ok := workbench.FramedPaneContentRect(right.Rect, right.SharedLeft, right.SharedTop)
	if !ok {
		t.Fatal("expected framed pane content rect")
	}
	if viewport != want {
		t.Fatalf("expected framed viewport %#v, got %#v", want, viewport)
	}
}

func TestVerticalScrollOptimizationModeSinglePane(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "main"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	model := New(shared.Config{}, wb, nil)
	model.width = 120
	model.height = 36

	mode, reason := model.verticalScrollOptimizationMode()
	if mode != verticalScrollModeRowsAndRects || reason != "single_pane" {
		t.Fatalf("expected single pane mode rows_and_rects, got mode=%q reason=%q", mode.String(), reason)
	}
}

func TestVerticalScrollOptimizationModeStackedFullWidthPanes(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "top"},
				"pane-2": {ID: "pane-2", Title: "bottom"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitHorizontal,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})

	model := New(shared.Config{}, wb, nil)
	model.width = 120
	model.height = 36

	mode, reason := model.verticalScrollOptimizationMode()
	if mode != verticalScrollModeRowsAndRects || reason != "stacked_full_width" {
		t.Fatalf("expected stacked panes mode rows_and_rects, got mode=%q reason=%q", mode.String(), reason)
	}
}

func TestVerticalScrollOptimizationModeSideBySideUsesRectsOnly(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "left"},
				"pane-2": {ID: "pane-2", Title: "right"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})

	model := New(shared.Config{}, wb, nil)
	model.width = 120
	model.height = 36

	mode, reason := model.verticalScrollOptimizationMode()
	if mode != verticalScrollModeRectsOnly || reason != "tiled_partial_width" {
		t.Fatalf("expected side-by-side panes mode rects_only, got mode=%q reason=%q", mode.String(), reason)
	}
	if !model.allowVerticalScrollOptimization() {
		t.Fatal("expected side-by-side panes to keep vertical scroll optimization enabled via rect-scroll")
	}
}

func TestVerticalScrollOptimizationModeMixedTiledUsesRectsOnly(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "left"},
				"pane-2": {ID: "pane-2", Title: "top-right"},
				"pane-3": {ID: "pane-3", Title: "bottom-right"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second: &workbench.LayoutNode{
					Direction: workbench.SplitHorizontal,
					Ratio:     0.5,
					First:     workbench.NewLeaf("pane-2"),
					Second:    workbench.NewLeaf("pane-3"),
				},
			},
		}},
	})

	model := New(shared.Config{}, wb, nil)
	model.width = 120
	model.height = 36

	mode, reason := model.verticalScrollOptimizationMode()
	if mode != verticalScrollModeRectsOnly || reason != "tiled_partial_width" {
		t.Fatalf("expected mixed tiled panes mode rects_only, got mode=%q reason=%q", mode.String(), reason)
	}
}

func TestVerticalScrollOptimizationModeRejectsFloatingVisible(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:              "tab-1",
			ActivePaneID:    "pane-1",
			FloatingVisible: true,
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "base"},
				"pane-2": {ID: "pane-2", Title: "float"},
			},
			Root: workbench.NewLeaf("pane-1"),
			Floating: []*workbench.FloatingState{{
				PaneID: "pane-2",
				Rect:   workbench.Rect{X: 8, Y: 4, W: 30, H: 10},
				Z:      1,
			}},
		}},
	})

	model := New(shared.Config{}, wb, nil)
	model.width = 120
	model.height = 36

	mode, reason := model.verticalScrollOptimizationMode()
	if mode != verticalScrollModeNone || reason != "floating_visible" {
		t.Fatalf("expected floating layouts to disable vertical scroll optimization, got mode=%q reason=%q", mode.String(), reason)
	}
}

func TestConservativeAltScreenDiffRequiredSinglePane(t *testing.T) {
	model := setupModel(t, modelOpts{width: 120, height: 36})
	terminal := model.runtime.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal")
	}
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
		Cursor:     protocol.CursorState{Visible: true},
		Modes:      protocol.TerminalModes{AlternateScreen: true},
	}

	if model.conservativeAltScreenDiffRequired() {
		t.Fatal("expected single alt-screen pane to keep incremental diff paths")
	}
}

func TestConservativeAltScreenDiffRequiredSplitPane(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "nvim", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "notes", TerminalID: "term-2"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})
	rt := runtime.New(nil)
	rt.Registry().GetOrCreate("term-1").Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 58, Rows: 30},
		Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
		Cursor:     protocol.CursorState{Visible: true},
		Modes:      protocol.TerminalModes{AlternateScreen: true},
	}
	rt.Registry().GetOrCreate("term-2").Snapshot = &protocol.Snapshot{
		TerminalID: "term-2",
		Size:       protocol.Size{Cols: 58, Rows: 30},
		Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "y", Width: 1}}}},
		Cursor:     protocol.CursorState{Visible: true},
		Modes:      protocol.TerminalModes{AutoWrap: true},
	}
	model := New(shared.Config{}, wb, rt)
	model.width = 120
	model.height = 36

	if !model.conservativeAltScreenDiffRequired() {
		t.Fatal("expected split alt-screen layout to require conservative diff")
	}
}

func TestConservativeAltScreenDiffRequiredFloatingPane(t *testing.T) {
	model := setupModel(t, modelOpts{width: 120, height: 36})
	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := model.workbench.BindPaneTerminal(tab.ID, "pane-1", "term-shared"); err != nil {
		t.Fatalf("bind base pane: %v", err)
	}
	if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 18, Y: 12, W: 34, H: 10}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}
	if err := model.workbench.BindPaneTerminal(tab.ID, "float-1", "term-shared"); err != nil {
		t.Fatalf("bind floating pane: %v", err)
	}
	terminal := model.runtime.Registry().GetOrCreate("term-shared")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-shared",
		Size:       protocol.Size{Cols: 18, Rows: 10},
		Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "z", Width: 1}}}},
		Cursor:     protocol.CursorState{Visible: true},
		Modes:      protocol.TerminalModes{AlternateScreen: true},
	}

	if !model.conservativeAltScreenDiffRequired() {
		t.Fatal("expected floating alt-screen layout to require conservative diff")
	}
}

func TestVerticalScrollOptimizationModeRejectsContentOverlap(t *testing.T) {
	visible := &workbench.VisibleWorkbench{
		ActiveTab: 0,
		Tabs: []workbench.VisibleTab{{
			ID: "tab-1",
			Panes: []workbench.VisiblePane{
				{ID: "pane-1", Rect: workbench.Rect{X: 0, Y: 0, W: 60, H: 20}},
				{ID: "pane-2", Rect: workbench.Rect{X: 30, Y: 0, W: 60, H: 20}},
			},
		}},
	}

	mode, reason := verticalScrollOptimizationModeForVisible(
		workbench.Rect{W: 120, H: 36},
		render.VisibleSurfaceWorkbench,
		render.VisibleOverlayNone,
		visible,
	)
	if mode != verticalScrollModeNone || reason != "content_overlap" {
		t.Fatalf("expected overlapping content rects to disable vertical scroll optimization, got mode=%q reason=%q", mode.String(), reason)
	}
}
