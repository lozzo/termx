package render

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestCoordinatorRenderFrameUpdatesEmptyPaneSelection(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "empty"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	state := WithTermSize(AdaptVisibleStateWithSize(wb, nil, 100, 18), 100, 20)
	state = WithEmptyPaneSelection(state, "pane-1", 0)

	coordinator := NewCoordinator(func() VisibleRenderState { return state })
	first := coordinator.RenderFrame()

	state = WithEmptyPaneSelection(state, "pane-1", 1)
	coordinator.Invalidate()
	second := coordinator.RenderFrame()

	if first == second {
		t.Fatal("expected frame to change when empty-pane selection changes")
	}
}
