package projection

import (
	"slices"
	"testing"

	"github.com/lozzow/termx/tui/domain/types"
)

func TestProjectWorkbenchReturnsActivePaneAndOrderedFloating(t *testing.T) {
	state := newProjectionAppState()
	view := ProjectWorkbench(state, nil, 120, 40)

	if view.ActivePaneID != types.PaneID("pane-1") {
		t.Fatalf("expected active pane pane-1, got %q", view.ActivePaneID)
	}
	if len(view.Floating) != 2 {
		t.Fatalf("expected 2 floating panes, got %d", len(view.Floating))
	}
	if view.Floating[1].PaneID != types.PaneID("float-2") {
		t.Fatalf("expected top floating pane float-2, got %q", view.Floating[1].PaneID)
	}
}

func TestProjectWorkbenchReturnsDeterministicTiledOrder(t *testing.T) {
	state := newProjectionAppStateWithSplit()
	expected := []types.PaneID{
		types.PaneID("pane-2"),
		types.PaneID("pane-1"),
		types.PaneID("pane-3"),
	}

	for i := 0; i < 32; i++ {
		view := ProjectWorkbench(state, nil, 120, 40)
		got := projectedPaneIDs(view.Tiled)
		if !slices.Equal(got, expected) {
			t.Fatalf("expected deterministic tiled order %v, got %v", expected, got)
		}
	}
}

func newProjectionAppState() types.AppState {
	return types.AppState{
		Domain: types.DomainState{
			ActiveWorkspaceID: types.WorkspaceID("ws-1"),
			Workspaces: map[types.WorkspaceID]types.WorkspaceState{
				types.WorkspaceID("ws-1"): {
					ID:          types.WorkspaceID("ws-1"),
					Name:        "main",
					ActiveTabID: types.TabID("tab-1"),
					TabOrder:    []types.TabID{types.TabID("tab-1")},
					Tabs: map[types.TabID]types.TabState{
						types.TabID("tab-1"): {
							ID:           types.TabID("tab-1"),
							Name:         "shell",
							ActivePaneID: types.PaneID("pane-1"),
							ActiveLayer:  types.FocusLayerTiled,
							FloatingOrder: []types.PaneID{
								types.PaneID("float-1"),
								types.PaneID("float-2"),
							},
							Panes: map[types.PaneID]types.PaneState{
								types.PaneID("pane-1"): {
									ID:   types.PaneID("pane-1"),
									Kind: types.PaneKindTiled,
									Rect: types.Rect{X: 0, Y: 0, W: 80, H: 24},
								},
								types.PaneID("float-1"): {
									ID:   types.PaneID("float-1"),
									Kind: types.PaneKindFloating,
									Rect: types.Rect{X: 4, Y: 3, W: 30, H: 12},
								},
								types.PaneID("float-2"): {
									ID:   types.PaneID("float-2"),
									Kind: types.PaneKindFloating,
									Rect: types.Rect{X: 8, Y: 6, W: 32, H: 14},
								},
							},
						},
					},
				},
			},
		},
		UI: types.UIState{
			Focus: types.FocusState{
				Layer:       types.FocusLayerTiled,
				WorkspaceID: types.WorkspaceID("ws-1"),
				TabID:       types.TabID("tab-1"),
				PaneID:      types.PaneID("pane-1"),
			},
			Overlay: types.OverlayState{Kind: types.OverlayNone},
		},
	}
}

func newProjectionAppStateWithSplit() types.AppState {
	state := newProjectionAppState()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	tab.RootSplit = &types.SplitNode{
		Direction: types.SplitDirectionHorizontal,
		First: &types.SplitNode{
			PaneID: types.PaneID("pane-2"),
		},
		Second: &types.SplitNode{
			Direction: types.SplitDirectionVertical,
			First: &types.SplitNode{
				PaneID: types.PaneID("pane-1"),
			},
			Second: &types.SplitNode{
				PaneID: types.PaneID("pane-3"),
			},
		},
	}
	tab.Panes[types.PaneID("pane-2")] = types.PaneState{
		ID:   types.PaneID("pane-2"),
		Kind: types.PaneKindTiled,
		Rect: types.Rect{X: 80, Y: 0, W: 40, H: 20},
	}
	tab.Panes[types.PaneID("pane-3")] = types.PaneState{
		ID:   types.PaneID("pane-3"),
		Kind: types.PaneKindTiled,
		Rect: types.Rect{X: 80, Y: 20, W: 40, H: 20},
	}
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	return state
}

func projectedPaneIDs(panes []PaneProjection) []types.PaneID {
	ids := make([]types.PaneID, 0, len(panes))
	for _, pane := range panes {
		ids = append(ids, pane.PaneID)
	}
	return ids
}
