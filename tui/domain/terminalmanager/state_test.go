package terminalmanager

import (
	"testing"

	"github.com/lozzow/termx/tui/domain/types"
)

func TestManagerStateDefaultsSelectionToFocusedPaneTerminal(t *testing.T) {
	state := sampleDomainState()

	manager := NewState(state, types.FocusState{
		Layer:       types.FocusLayerTiled,
		WorkspaceID: types.WorkspaceID("ws-1"),
		TabID:       types.TabID("tab-1"),
		PaneID:      types.PaneID("pane-1"),
	})

	selected, ok := manager.SelectedTerminalID()
	if !ok {
		t.Fatalf("expected selected terminal")
	}
	if selected != types.TerminalID("term-2") {
		t.Fatalf("expected focused pane terminal to be selected, got %q", selected)
	}
}

func TestManagerStateMoveSelectionClampsWithinRows(t *testing.T) {
	state := sampleDomainState()

	manager := NewState(state, types.FocusState{})
	manager.MoveSelection(100)

	selected, ok := manager.SelectedTerminalID()
	if !ok {
		t.Fatalf("expected selected terminal at bottom")
	}
	if selected != types.TerminalID("term-3") {
		t.Fatalf("expected clamp to last terminal, got %q", selected)
	}

	manager.MoveSelection(-100)
	selected, ok = manager.SelectedTerminalID()
	if !ok {
		t.Fatalf("expected selected terminal at top")
	}
	if selected != types.TerminalID("term-1") {
		t.Fatalf("expected clamp to first terminal, got %q", selected)
	}
}

func TestManagerStateVisibleRowsSortedByName(t *testing.T) {
	state := sampleDomainState()

	manager := NewState(state, types.FocusState{})
	rows := manager.VisibleRows()
	if len(rows) != 3 {
		t.Fatalf("expected 3 manager rows, got %d", len(rows))
	}
	if rows[0].TerminalID != types.TerminalID("term-1") || rows[0].Label != "alpha" {
		t.Fatalf("expected first row to be alpha, got %+v", rows[0])
	}
	if rows[2].TerminalID != types.TerminalID("term-3") || rows[2].Label != "gamma" {
		t.Fatalf("expected last row to be gamma, got %+v", rows[2])
	}
}

func sampleDomainState() types.DomainState {
	return types.DomainState{
		ActiveWorkspaceID: types.WorkspaceID("ws-1"),
		WorkspaceOrder:    []types.WorkspaceID{types.WorkspaceID("ws-1")},
		Workspaces: map[types.WorkspaceID]types.WorkspaceState{
			types.WorkspaceID("ws-1"): {
				ID:          types.WorkspaceID("ws-1"),
				Name:        "ws-1",
				ActiveTabID: types.TabID("tab-1"),
				TabOrder:    []types.TabID{types.TabID("tab-1")},
				Tabs: map[types.TabID]types.TabState{
					types.TabID("tab-1"): {
						ID:           types.TabID("tab-1"),
						Name:         "tab-1",
						ActivePaneID: types.PaneID("pane-1"),
						Panes: map[types.PaneID]types.PaneState{
							types.PaneID("pane-1"): {
								ID:         types.PaneID("pane-1"),
								Kind:       types.PaneKindTiled,
								SlotState:  types.PaneSlotConnected,
								TerminalID: types.TerminalID("term-2"),
							},
						},
					},
				},
			},
		},
		Terminals: map[types.TerminalID]types.TerminalRef{
			types.TerminalID("term-1"): {ID: types.TerminalID("term-1"), Name: "alpha"},
			types.TerminalID("term-2"): {ID: types.TerminalID("term-2"), Name: "beta"},
			types.TerminalID("term-3"): {ID: types.TerminalID("term-3"), Name: "gamma"},
		},
	}
}
