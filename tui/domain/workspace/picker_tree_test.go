package workspace

import (
	"testing"

	"github.com/lozzow/termx/tui/domain/types"
)

func TestBuildPickerTreeUsesTerminalNameAndFallbackLabels(t *testing.T) {
	state := types.DomainState{
		ActiveWorkspaceID: types.WorkspaceID("ws-1"),
		Workspaces: map[types.WorkspaceID]types.WorkspaceState{
			types.WorkspaceID("ws-1"): {
				ID:          types.WorkspaceID("ws-1"),
				Name:        "prod-main",
				ActiveTabID: types.TabID("tab-1"),
				TabOrder:    []types.TabID{types.TabID("tab-1")},
				Tabs: map[types.TabID]types.TabState{
					types.TabID("tab-1"): {
						ID:           types.TabID("tab-1"),
						Name:         "dev",
						ActivePaneID: types.PaneID("pane-1"),
						Panes: map[types.PaneID]types.PaneState{
							types.PaneID("pane-1"): {ID: types.PaneID("pane-1"), SlotState: types.PaneSlotConnected, TerminalID: types.TerminalID("term-1")},
							types.PaneID("pane-2"): {ID: types.PaneID("pane-2"), SlotState: types.PaneSlotEmpty},
							types.PaneID("pane-3"): {ID: types.PaneID("pane-3"), SlotState: types.PaneSlotExited},
						},
					},
				},
			},
		},
		WorkspaceOrder: []types.WorkspaceID{types.WorkspaceID("ws-1")},
		Terminals: map[types.TerminalID]types.TerminalRef{
			types.TerminalID("term-1"): {ID: types.TerminalID("term-1"), Name: "api-dev"},
		},
	}

	tree := BuildPickerTree(state)
	if len(tree) != 2 {
		t.Fatalf("expected create row plus workspace root, got %d rows", len(tree))
	}

	workspaceNode := tree[1]
	tabNode := workspaceNode.Children[0]
	if got := tabNode.Children[0].Label; got != "api-dev" {
		t.Fatalf("expected connected pane label to use terminal name, got %q", got)
	}
	if got := tabNode.Children[1].Label; got != "unconnected pane" {
		t.Fatalf("expected empty pane fallback label, got %q", got)
	}
	if got := tabNode.Children[2].Label; got != "program exited pane" {
		t.Fatalf("expected exited pane fallback label, got %q", got)
	}
}

func TestResolveTreeJumpFocusesPaneLayer(t *testing.T) {
	ws := types.WorkspaceState{
		ID:          types.WorkspaceID("ws-1"),
		Name:        "prod-main",
		ActiveTabID: types.TabID("tab-1"),
		TabOrder:    []types.TabID{types.TabID("tab-1")},
		Tabs: map[types.TabID]types.TabState{
			types.TabID("tab-1"): {
				ID: types.TabID("tab-1"),
				Panes: map[types.PaneID]types.PaneState{
					types.PaneID("pane-float"): {ID: types.PaneID("pane-float"), Kind: types.PaneKindFloating},
				},
			},
		},
	}

	focus, ok := ResolveTreeJumpFocus(ws, types.TabID("tab-1"), types.PaneID("pane-float"))
	if !ok {
		t.Fatalf("expected jump resolution to succeed")
	}
	if focus.Layer != types.FocusLayerFloating {
		t.Fatalf("expected floating pane jump to focus floating layer, got %q", focus.Layer)
	}
}
