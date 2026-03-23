package terminalpicker

import (
	"testing"

	"github.com/lozzow/termx/tui/domain/types"
)

func TestPickerStateDefaultsSelectionToFocusedPaneTerminal(t *testing.T) {
	state := sampleDomainState()

	picker := NewState(state, types.FocusState{
		WorkspaceID: types.WorkspaceID("ws-1"),
		TabID:       types.TabID("tab-1"),
		PaneID:      types.PaneID("pane-1"),
	})
	selected, ok := picker.SelectedTerminalID()
	if !ok || selected != types.TerminalID("term-1") {
		t.Fatalf("expected focused terminal selected, got %q ok=%v", selected, ok)
	}
}

func TestPickerStateAppendQuerySelectsFirstMatchedTerminal(t *testing.T) {
	state := sampleDomainState()
	picker := NewState(state, types.FocusState{})

	picker.AppendQuery("build")

	if picker.Query() != "build" {
		t.Fatalf("expected query to update, got %q", picker.Query())
	}
	selected, ok := picker.SelectedTerminalID()
	if !ok || selected != types.TerminalID("term-2") {
		t.Fatalf("expected build terminal selected, got %q ok=%v", selected, ok)
	}
}

func TestPickerStateVisibleRowsKeepCreateRowWhenFiltering(t *testing.T) {
	state := sampleDomainState()
	picker := NewState(state, types.FocusState{})

	picker.AppendQuery("missing")
	rows := picker.VisibleRows()
	if len(rows) != 1 || rows[0].Kind != RowKindCreate {
		t.Fatalf("expected create row to remain visible, got %+v", rows)
	}
}

func TestPickerStateBackspaceShrinksQuery(t *testing.T) {
	state := sampleDomainState()
	picker := NewState(state, types.FocusState{})

	picker.AppendQuery("api")
	picker.BackspaceQuery()
	if picker.Query() != "ap" {
		t.Fatalf("expected query to shrink, got %q", picker.Query())
	}
}

func sampleDomainState() types.DomainState {
	return types.DomainState{
		ActiveWorkspaceID: types.WorkspaceID("ws-1"),
		WorkspaceOrder:    []types.WorkspaceID{types.WorkspaceID("ws-1")},
		Workspaces: map[types.WorkspaceID]types.WorkspaceState{
			types.WorkspaceID("ws-1"): {
				ID:          types.WorkspaceID("ws-1"),
				Name:        "project-api",
				ActiveTabID: types.TabID("tab-1"),
				TabOrder:    []types.TabID{types.TabID("tab-1")},
				Tabs: map[types.TabID]types.TabState{
					types.TabID("tab-1"): {
						ID:           types.TabID("tab-1"),
						Name:         "dev",
						ActivePaneID: types.PaneID("pane-1"),
						Panes: map[types.PaneID]types.PaneState{
							types.PaneID("pane-1"): {
								ID:         types.PaneID("pane-1"),
								Kind:       types.PaneKindTiled,
								SlotState:  types.PaneSlotConnected,
								TerminalID: types.TerminalID("term-1"),
							},
						},
					},
				},
			},
		},
		Terminals: map[types.TerminalID]types.TerminalRef{
			types.TerminalID("term-1"): {
				ID:      types.TerminalID("term-1"),
				Name:    "api-dev",
				Command: []string{"npm", "run", "dev"},
				State:   types.TerminalRunStateRunning,
				Tags:    map[string]string{"team": "backend"},
			},
			types.TerminalID("term-2"): {
				ID:      types.TerminalID("term-2"),
				Name:    "build-log",
				Command: []string{"tail", "-f", "build.log"},
				State:   types.TerminalRunStateRunning,
				Tags:    map[string]string{"group": "build"},
			},
		},
	}
}
