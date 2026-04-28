package viewstate

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestCaptureApplyPreservesPureClientProjection(t *testing.T) {
	wb := seededWorkbench()
	offsets := map[string]int{
		"pane-main-1": 2,
		"pane-dev-3":  7,
	}

	_ = wb.SwitchWorkspace("dev")
	_ = wb.SwitchTab("dev", 1)
	_ = wb.FocusPane("tab-dev-2", "pane-dev-3")

	proj := Capture(wb, CaptureOptions{
		PaneViewportOffset: func(paneID string) (int, bool) {
			offset, ok := offsets[paneID]
			return offset, ok
		},
		EffectiveTabViewportOffset: func(tab *workbench.TabState) int {
			if tab == nil {
				return 0
			}
			return tab.ScrollOffset
		},
	})

	_ = wb.SwitchWorkspace("main")
	_ = wb.SwitchTab("main", 0)
	_ = wb.FocusPane("tab-main-1", "pane-main-1")
	wb.WorkspaceByName("main").Tabs[0].ZoomedPaneID = ""
	wb.WorkspaceByName("dev").Tabs[1].ZoomedPaneID = ""
	offsets["pane-main-1"] = 0
	offsets["pane-dev-3"] = 0

	Apply(wb, proj, ApplyOptions{
		SetPaneViewportOffset: func(paneID string, offset int) bool {
			offsets[paneID] = offset
			return true
		},
	})

	if got := wb.CurrentWorkspaceName(); got != "dev" {
		t.Fatalf("expected workspace dev, got %q", got)
	}
	if got := wb.CurrentTab(); got == nil || got.ID != "tab-dev-2" {
		t.Fatalf("expected current tab tab-dev-2, got %#v", got)
	}
	if got := wb.CurrentTab().ActivePaneID; got != "pane-dev-3" {
		t.Fatalf("expected focused pane pane-dev-3, got %q", got)
	}
	if got := wb.WorkspaceByName("main").Tabs[0].ZoomedPaneID; got != "pane-main-1" {
		t.Fatalf("expected main zoom pane-main-1, got %q", got)
	}
	if got := wb.WorkspaceByName("dev").Tabs[1].ZoomedPaneID; got != "pane-dev-3" {
		t.Fatalf("expected dev zoom pane-dev-3, got %q", got)
	}
	if got := offsets["pane-main-1"]; got != 2 {
		t.Fatalf("expected pane-main-1 viewport 2, got %d", got)
	}
	if got := offsets["pane-dev-3"]; got != 7 {
		t.Fatalf("expected pane-dev-3 viewport 7, got %d", got)
	}
}

func TestCaptureFallsBackToEffectiveTabScrollbackForFocusedPane(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "Tab 1",
			ActivePaneID: "pane-1",
			ZoomedPaneID: "pane-1",
			ScrollOffset: 9,
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	proj := Capture(wb, CaptureOptions{
		PaneViewportOffset: func(string) (int, bool) { return 0, false },
		EffectiveTabViewportOffset: func(tab *workbench.TabState) int {
			if tab == nil {
				return 0
			}
			return tab.ScrollOffset
		},
	})

	if got := proj.ViewportByPane["pane-1"]; got != 9 {
		t.Fatalf("expected focused pane viewport fallback 9, got %d", got)
	}
}

func seededWorkbench() *workbench.Workbench {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-main-1",
			Name:         "Main",
			ActivePaneID: "pane-main-1",
			ZoomedPaneID: "pane-main-1",
			Panes: map[string]*workbench.PaneState{
				"pane-main-1": {ID: "pane-main-1"},
			},
			Root: workbench.NewLeaf("pane-main-1"),
		}},
	})
	wb.AddWorkspace("dev", &workbench.WorkspaceState{
		Name:      "dev",
		ActiveTab: 1,
		Tabs: []*workbench.TabState{
			{
				ID:           "tab-dev-1",
				Name:         "Dev 1",
				ActivePaneID: "pane-dev-1",
				Panes: map[string]*workbench.PaneState{
					"pane-dev-1": {ID: "pane-dev-1"},
				},
				Root: workbench.NewLeaf("pane-dev-1"),
			},
			{
				ID:           "tab-dev-2",
				Name:         "Dev 2",
				ActivePaneID: "pane-dev-3",
				ZoomedPaneID: "pane-dev-3",
				ScrollOffset: 5,
				Panes: map[string]*workbench.PaneState{
					"pane-dev-2": {ID: "pane-dev-2"},
					"pane-dev-3": {ID: "pane-dev-3"},
				},
				Root: &workbench.LayoutNode{
					Direction: workbench.SplitVertical,
					Ratio:     0.5,
					First:     workbench.NewLeaf("pane-dev-2"),
					Second:    workbench.NewLeaf("pane-dev-3"),
				},
			},
		},
	})
	return wb
}
