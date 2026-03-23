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
	row, ok := manager.SelectedRow()
	if !ok {
		t.Fatalf("expected selected terminal at top")
	}
	if row.Kind != RowKindCreate {
		t.Fatalf("expected clamp to create row, got %+v", row)
	}
}

func TestManagerStateVisibleRowsSortedByName(t *testing.T) {
	state := sampleDomainState()

	manager := NewState(state, types.FocusState{})
	rows := manager.VisibleRows()
	if len(rows) != 8 {
		t.Fatalf("expected grouped manager rows, got %d", len(rows))
	}
	if rows[0].Kind != RowKindHeader || rows[0].Label != "NEW" {
		t.Fatalf("expected NEW header first, got %+v", rows[0])
	}
	if rows[1].Kind != RowKindCreate || rows[1].Label != "+ new terminal" {
		t.Fatalf("expected create row after NEW header, got %+v", rows[1])
	}
	if rows[2].Kind != RowKindHeader || rows[2].Label != "VISIBLE" {
		t.Fatalf("expected VISIBLE header, got %+v", rows[2])
	}
	if rows[3].TerminalID != types.TerminalID("term-2") || rows[3].Label != "beta" {
		t.Fatalf("expected visible terminal row to be beta, got %+v", rows[3])
	}
	if rows[7].TerminalID != types.TerminalID("term-3") || rows[7].Label != "gamma" {
		t.Fatalf("expected exited terminal row last, got %+v", rows[7])
	}
}

func TestManagerStateAppendQueryFiltersRowsAndSelectsFirstMatch(t *testing.T) {
	state := sampleDomainState()

	manager := NewState(state, types.FocusState{})
	manager.AppendQuery("gam")

	if manager.Query() != "gam" {
		t.Fatalf("expected query to append, got %q", manager.Query())
	}
	rows := manager.VisibleRows()
	if len(rows) != 4 || rows[3].TerminalID != types.TerminalID("term-3") {
		t.Fatalf("expected only gamma to remain visible, got %+v", rows)
	}
	selected, ok := manager.SelectedTerminalID()
	if !ok || selected != types.TerminalID("term-3") {
		t.Fatalf("expected selected terminal to follow first match, got %q ok=%v", selected, ok)
	}
}

func TestManagerStateQueryMatchesTagsAndBackspaceShrinksQuery(t *testing.T) {
	state := sampleDomainState()

	manager := NewState(state, types.FocusState{})
	manager.AppendQuery("ops")
	rows := manager.VisibleRows()
	if len(rows) != 4 || rows[3].TerminalID != types.TerminalID("term-3") {
		t.Fatalf("expected tags to participate in search, got %+v", rows)
	}

	manager.BackspaceQuery()
	if manager.Query() != "op" {
		t.Fatalf("expected query to shrink after backspace, got %q", manager.Query())
	}
}

func TestManagerStateMoveSelectionCanReachCreateRow(t *testing.T) {
	state := sampleDomainState()

	manager := NewState(state, types.FocusState{})
	manager.MoveSelection(-100)

	row, ok := manager.SelectedRow()
	if !ok {
		t.Fatalf("expected selected row")
	}
	if row.Kind != RowKindCreate {
		t.Fatalf("expected selection to clamp to create row, got %+v", row)
	}
	if _, ok := manager.SelectedTerminalID(); ok {
		t.Fatalf("expected create row to have no selected terminal")
	}
}

func TestManagerStateSelectedDetailTracksCurrentTerminal(t *testing.T) {
	state := sampleDomainState()

	manager := NewState(state, types.FocusState{})
	detail, ok := manager.SelectedDetail()
	if !ok {
		t.Fatalf("expected selected terminal detail")
	}
	if detail.TerminalID != types.TerminalID("term-2") || detail.Name != "beta" {
		t.Fatalf("unexpected selected detail: %+v", detail)
	}
	if detail.ConnectedPaneCount != 1 {
		t.Fatalf("expected connected pane count, got %+v", detail)
	}
	if detail.Command != "npm run build" {
		t.Fatalf("expected command projection, got %+v", detail)
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
			types.TerminalID("term-1"): {ID: types.TerminalID("term-1"), Name: "alpha", Tags: map[string]string{"group": "api"}, Command: []string{"tail", "-f", "api.log"}},
			types.TerminalID("term-2"): {ID: types.TerminalID("term-2"), Name: "beta", Tags: map[string]string{"group": "build"}, Command: []string{"npm", "run", "build"}, Visible: true},
			types.TerminalID("term-3"): {ID: types.TerminalID("term-3"), Name: "gamma", Tags: map[string]string{"team": "ops"}, State: types.TerminalRunStateExited},
		},
		Connections: map[types.TerminalID]types.ConnectionState{
			types.TerminalID("term-2"): {
				TerminalID:       types.TerminalID("term-2"),
				ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1")},
				OwnerPaneID:      types.PaneID("pane-1"),
			},
		},
	}
}
