package workspace

import (
	"testing"

	"github.com/lozzow/termx/tui/state/types"
)

func TestWorkspaceTracksUnconnectedPaneAndFloatingPane(t *testing.T) {
	ws := NewTemporary("main")
	tab := ws.ActiveTab()
	if tab == nil {
		t.Fatal("expected temporary workspace to create an active tab")
	}
	if tab.ActivePaneID == "" {
		t.Fatal("expected temporary workspace to create an active pane")
	}

	activePane, ok := tab.ActivePane()
	if !ok {
		t.Fatal("expected temporary workspace to expose active pane state")
	}
	if activePane.Kind != types.PaneKindTiled {
		t.Fatalf("expected default pane kind %q, got %q", types.PaneKindTiled, activePane.Kind)
	}
	if activePane.SlotState != types.PaneSlotUnconnected {
		t.Fatalf("expected default pane slot state %q, got %q", types.PaneSlotUnconnected, activePane.SlotState)
	}

	tab.TrackPane(PaneState{
		ID:        types.PaneID("float-1"),
		Kind:      types.PaneKindFloating,
		SlotState: types.PaneSlotUnconnected,
		Rect:      types.Rect{X: 4, Y: 2, W: 30, H: 10},
	})
	tab.TrackPane(PaneState{
		ID:        types.PaneID("pane-2"),
		Kind:      types.PaneKindTiled,
		SlotState: types.PaneSlotUnconnected,
	})

	floatPane, ok := tab.Pane(types.PaneID("float-1"))
	if !ok {
		t.Fatal("expected floating pane to be tracked")
	}
	if floatPane.Rect != (types.Rect{X: 4, Y: 2, W: 30, H: 10}) {
		t.Fatalf("expected floating rect to be preserved, got %+v", floatPane.Rect)
	}
	if len(tab.FloatingOrder) != 1 || tab.FloatingOrder[0] != types.PaneID("float-1") {
		t.Fatalf("expected floating order to contain float-1, got %+v", tab.FloatingOrder)
	}

	unconnectedPane, ok := tab.Pane(types.PaneID("pane-2"))
	if !ok {
		t.Fatal("expected unconnected pane to be tracked")
	}
	if unconnectedPane.SlotState != types.PaneSlotUnconnected {
		t.Fatalf("expected pane-2 slot state %q, got %q", types.PaneSlotUnconnected, unconnectedPane.SlotState)
	}
}
