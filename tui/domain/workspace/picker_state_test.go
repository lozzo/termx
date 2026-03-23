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

func TestPickerStateDefaultsSelectionToActivePane(t *testing.T) {
	state := sampleDomainStateForPicker()

	picker := NewPickerState(state)
	row, ok := picker.SelectedRow()
	if !ok {
		t.Fatalf("expected selected row")
	}
	if row.Node.Kind != TreeNodeKindPane || row.Node.PaneID != types.PaneID("pane-api") {
		t.Fatalf("expected active pane row to be selected, got %+v", row.Node)
	}
}

func TestPickerStateMoveSelectionAndClamp(t *testing.T) {
	state := sampleDomainStateForPicker()

	picker := NewPickerState(state)
	picker.MoveSelection(-10)
	first, ok := picker.SelectedRow()
	if !ok {
		t.Fatalf("expected selected row after moving to top")
	}
	if first.Node.Kind != TreeNodeKindCreate {
		t.Fatalf("expected selection to clamp to create row, got %+v", first.Node)
	}

	picker.MoveSelection(100)
	last, ok := picker.SelectedRow()
	if !ok {
		t.Fatalf("expected selected row after moving to bottom")
	}
	if last.Node.Kind != TreeNodeKindTab || last.Node.Label != "logs" {
		t.Fatalf("expected selection to clamp to last visible row, got %+v", last.Node)
	}
}

func TestPickerStateExpandAndCollapseSelectedNode(t *testing.T) {
	state := sampleDomainStateForPicker()

	picker := NewPickerState(state)
	picker.MoveSelection(-10)
	if changed := picker.ExpandSelected(); changed {
		t.Fatalf("expected create row expand to be ignored")
	}

	picker.MoveSelection(1)
	row, ok := picker.SelectedRow()
	if !ok || row.Node.Kind != TreeNodeKindWorkspace {
		t.Fatalf("expected workspace row selected, got %+v", row.Node)
	}
	if !row.Expanded {
		t.Fatalf("expected active workspace default expanded")
	}

	if changed := picker.CollapseSelected(); !changed {
		t.Fatalf("expected workspace collapse to change state")
	}
	rows := picker.VisibleRows()
	if len(rows) != 2 {
		t.Fatalf("expected workspace children hidden after collapse, got %d rows", len(rows))
	}

	if changed := picker.ExpandSelected(); !changed {
		t.Fatalf("expected workspace expand to change state")
	}
	rows = picker.VisibleRows()
	if len(rows) <= 2 {
		t.Fatalf("expected workspace children restored after expand, got %d rows", len(rows))
	}
}

func TestPickerStateClearQueryRestoresDefaultAndManualExpansion(t *testing.T) {
	state := sampleDomainStateForPicker()

	picker := NewPickerState(state)
	picker.MoveSelection(-10)
	picker.MoveSelection(1)
	picker.ExpandSelected()

	picker.SetQuery("deploy-log")
	queriedRows := picker.VisibleRows()
	if len(queriedRows) < 5 {
		t.Fatalf("expected query rows to reveal matched path, got %d", len(queriedRows))
	}

	picker.SetQuery("")
	rows := picker.VisibleRows()
	var foundLogsTab bool
	for _, row := range rows {
		if row.Node.Kind == TreeNodeKindTab && row.Node.TabID == types.TabID("tab-logs") {
			foundLogsTab = true
		}
	}
	if !foundLogsTab {
		t.Fatalf("expected manual expansion to survive query clear, got rows %+v", rows)
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
