package workbench

import (
	"testing"

	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
	coreworkspace "github.com/lozzow/termx/tui/core/workspace"
)

func TestMarkTerminalExitedUpdatesAttachedPanes(t *testing.T) {
	state := sampleState()

	updated := state.MarkTerminalExited(types.TerminalID("term-1"))
	if updated != 2 {
		t.Fatalf("expected 2 panes updated, got %d", updated)
	}

	tab := state.Workspace.ActiveTab()
	for _, paneID := range []types.PaneID{"pane-1", "pane-2"} {
		if got := tab.Panes[paneID].SlotState; got != types.PaneSlotExited {
			t.Fatalf("expected pane %q to be exited, got %q", paneID, got)
		}
	}
	if got := state.Terminals[types.TerminalID("term-1")].State; got != coreterminal.StateExited {
		t.Fatalf("expected terminal exited state, got %q", got)
	}
}

func TestMarkPaneDisconnectedKeepsOtherSharedPaneLive(t *testing.T) {
	state := sampleState()

	if !state.MarkPaneDisconnected(types.PaneID("pane-1")) {
		t.Fatal("expected pane disconnect to succeed")
	}

	tab := state.Workspace.ActiveTab()
	if got := tab.Panes[types.PaneID("pane-1")].SlotState; got != types.PaneSlotUnconnected {
		t.Fatalf("expected pane-1 unconnected, got %q", got)
	}
	if got := tab.Panes[types.PaneID("pane-1")].TerminalID; got != "" {
		t.Fatalf("expected pane-1 terminal cleared, got %q", got)
	}
	if got := tab.Panes[types.PaneID("pane-2")].SlotState; got != types.PaneSlotLive {
		t.Fatalf("expected pane-2 to stay live, got %q", got)
	}
}

func sampleState() State {
	state := New("main")
	tab := state.Workspace.ActiveTab()
	tab.TrackPane(coreworkspace.PaneState{
		ID:         types.PaneID("pane-1"),
		Kind:       types.PaneKindTiled,
		SlotState:  types.PaneSlotLive,
		TerminalID: types.TerminalID("term-1"),
	})
	tab.TrackPane(coreworkspace.PaneState{
		ID:         types.PaneID("pane-2"),
		Kind:       types.PaneKindTiled,
		SlotState:  types.PaneSlotLive,
		TerminalID: types.TerminalID("term-1"),
	})
	tab.ActivePaneID = types.PaneID("pane-1")
	state.Terminals[types.TerminalID("term-1")] = coreterminal.Metadata{
		ID:              types.TerminalID("term-1"),
		Name:            "shell",
		State:           coreterminal.StateRunning,
		OwnerPaneID:     types.PaneID("pane-1"),
		AttachedPaneIDs: []types.PaneID{types.PaneID("pane-1"), types.PaneID("pane-2")},
	}
	return state
}
