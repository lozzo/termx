package workspace

import (
	"testing"

	"github.com/lozzow/termx/tui/domain/types"
)

func TestPickerStateDefaultRowsExpandActiveWorkspaceAndTab(t *testing.T) {
	state := sampleDomainStateForPicker()

	picker := NewPickerState(state)
	rows := picker.VisibleRows()

	if len(rows) < 4 {
		t.Fatalf("expected active workspace path rows to be visible, got %d rows", len(rows))
	}
	if rows[1].Node.Kind != TreeNodeKindWorkspace || rows[1].Node.Label != "prod-main" {
		t.Fatalf("expected first workspace row to be prod-main, got %+v", rows[1].Node)
	}
	if rows[2].Node.Kind != TreeNodeKindTab || rows[2].Node.Label != "dev" {
		t.Fatalf("expected active tab row to be visible, got %+v", rows[2].Node)
	}
	if rows[3].Node.Kind != TreeNodeKindPane || rows[3].Node.Label != "api-dev" {
		t.Fatalf("expected active pane row to be visible, got %+v", rows[3].Node)
	}
}

func TestPickerStateQueryMatchPaneExpandsAncestors(t *testing.T) {
	state := sampleDomainStateForPicker()

	picker := NewPickerState(state)
	picker.SetQuery("deploy-log")
	rows := picker.VisibleRows()

	var foundWorkspace, foundTab, foundPane bool
	for _, row := range rows {
		switch {
		case row.Node.Kind == TreeNodeKindWorkspace && row.Node.Label == "prod-main":
			foundWorkspace = true
		case row.Node.Kind == TreeNodeKindTab && row.Node.Label == "logs":
			foundTab = true
		case row.Node.Kind == TreeNodeKindPane && row.Node.Label == "deploy-log":
			foundPane = true
		}
	}
	if !foundWorkspace || !foundTab || !foundPane {
		t.Fatalf("expected query to reveal ancestor path, got rows %+v", rows)
	}
}

func sampleDomainStateForPicker() types.DomainState {
	return types.DomainState{
		ActiveWorkspaceID: types.WorkspaceID("ws-1"),
		WorkspaceOrder:    []types.WorkspaceID{types.WorkspaceID("ws-1")},
		Workspaces: map[types.WorkspaceID]types.WorkspaceState{
			types.WorkspaceID("ws-1"): {
				ID:          types.WorkspaceID("ws-1"),
				Name:        "prod-main",
				ActiveTabID: types.TabID("tab-dev"),
				TabOrder:    []types.TabID{types.TabID("tab-dev"), types.TabID("tab-logs")},
				Tabs: map[types.TabID]types.TabState{
					types.TabID("tab-dev"): {
						ID:           types.TabID("tab-dev"),
						Name:         "dev",
						ActivePaneID: types.PaneID("pane-api"),
						Panes: map[types.PaneID]types.PaneState{
							types.PaneID("pane-api"): {
								ID:         types.PaneID("pane-api"),
								Kind:       types.PaneKindTiled,
								SlotState:  types.PaneSlotConnected,
								TerminalID: types.TerminalID("term-api"),
							},
						},
					},
					types.TabID("tab-logs"): {
						ID:           types.TabID("tab-logs"),
						Name:         "logs",
						ActivePaneID: types.PaneID("pane-log"),
						Panes: map[types.PaneID]types.PaneState{
							types.PaneID("pane-log"): {
								ID:         types.PaneID("pane-log"),
								Kind:       types.PaneKindTiled,
								SlotState:  types.PaneSlotConnected,
								TerminalID: types.TerminalID("term-log"),
							},
						},
					},
				},
			},
		},
		Terminals: map[types.TerminalID]types.TerminalRef{
			types.TerminalID("term-api"): {ID: types.TerminalID("term-api"), Name: "api-dev"},
			types.TerminalID("term-log"): {ID: types.TerminalID("term-log"), Name: "deploy-log"},
		},
	}
}
