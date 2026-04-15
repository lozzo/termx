package app

import (
	"testing"

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

func TestAllowVerticalScrollOptimizationAllowsStackedFullWidthPanes(t *testing.T) {
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

	if !model.allowVerticalScrollOptimization() {
		t.Fatal("expected stacked full-width panes to keep vertical scroll optimization enabled")
	}
}

func TestAllowVerticalScrollOptimizationRejectsSideBySidePanes(t *testing.T) {
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

	if model.allowVerticalScrollOptimization() {
		t.Fatal("expected side-by-side panes to keep vertical scroll optimization disabled")
	}
}
