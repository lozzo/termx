package workspace

import (
	"testing"

	"github.com/lozzow/termx/tui/core/types"
)

func TestWorkspaceCreatesUnconnectedPaneByDefault(t *testing.T) {
	ws := New("main")
	tab := ws.ActiveTab()
	if tab == nil {
		t.Fatal("expected default active tab")
	}

	pane := tab.ActivePane()
	if pane.ID == "" {
		t.Fatal("expected default pane id")
	}
	if pane.SlotState != types.PaneSlotUnconnected {
		t.Fatalf("expected unconnected pane, got %q", pane.SlotState)
	}
	if pane.Kind != types.PaneKindTiled {
		t.Fatalf("expected tiled pane, got %q", pane.Kind)
	}
}

func TestTrackPaneUpdatesActivePane(t *testing.T) {
	ws := New("main")
	tab := ws.ActiveTab()
	tab.TrackPane(PaneState{
		ID:        types.PaneID("pane-2"),
		Kind:      types.PaneKindFloating,
		SlotState: types.PaneSlotLive,
	})
	tab.ActivePaneID = types.PaneID("pane-2")

	pane := tab.ActivePane()
	if pane.ID != types.PaneID("pane-2") {
		t.Fatalf("expected active pane-2, got %q", pane.ID)
	}
	if pane.Kind != types.PaneKindFloating {
		t.Fatalf("expected floating pane, got %q", pane.Kind)
	}
}
