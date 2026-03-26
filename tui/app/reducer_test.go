package app

import (
	"testing"

	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
	coreworkspace "github.com/lozzow/termx/tui/core/workspace"
	featureoverlay "github.com/lozzow/termx/tui/features/overlay"
	featureworkbench "github.com/lozzow/termx/tui/features/workbench"
)

func TestReducerCanSwitchBetweenWorkbenchAndTerminalPool(t *testing.T) {
	model := NewModel("main")

	model, effects := Reduce(model, IntentOpenTerminalPool)
	if len(effects) != 0 {
		t.Fatalf("expected no effects for screen switch, got %d", len(effects))
	}
	if model.Screen != ScreenTerminalPool {
		t.Fatalf("expected terminal pool screen, got %q", model.Screen)
	}

	model, effects = Reduce(model, IntentCloseScreen)
	if len(effects) != 0 {
		t.Fatalf("expected no effects when closing screen, got %d", len(effects))
	}
	if model.Screen != ScreenWorkbench {
		t.Fatalf("expected workbench screen after close, got %q", model.Screen)
	}
}

func TestReducerDistinguishesDisconnectKillAndRemove(t *testing.T) {
	model := sampleLiveWorkbenchModel()

	model, effects := Reduce(model, MessageTerminalDisconnected{PaneID: types.PaneID("pane-1")})
	if len(effects) != 0 {
		t.Fatalf("expected no effects for disconnected message, got %d", len(effects))
	}
	if got := model.Workbench.Workspace.ActiveTab().Panes[types.PaneID("pane-1")].SlotState; got != types.PaneSlotUnconnected {
		t.Fatalf("expected unconnected after disconnect, got %q", got)
	}

	model = sampleLiveWorkbenchModel()
	model, _ = Reduce(model, MessageTerminalExited{TerminalID: types.TerminalID("term-1")})
	if got := model.Workbench.Workspace.ActiveTab().Panes[types.PaneID("pane-1")].SlotState; got != types.PaneSlotExited {
		t.Fatalf("expected exited after kill, got %q", got)
	}

	model = sampleLiveWorkbenchModel()
	model, _ = Reduce(model, MessageTerminalRemoved{TerminalID: types.TerminalID("term-1")})
	if got := model.Workbench.Workspace.ActiveTab().Panes[types.PaneID("pane-1")].SlotState; got != types.PaneSlotUnconnected {
		t.Fatalf("expected unconnected after remove, got %q", got)
	}
	if got := model.Workbench.Workspace.ActiveTab().Panes[types.PaneID("pane-1")].TerminalID; got != "" {
		t.Fatalf("expected pane terminal cleared after remove, got %q", got)
	}
}

func TestReducerOpensConnectOverlayForUnconnectedPane(t *testing.T) {
	model := NewModel("main")
	model, effects := Reduce(model, IntentOpenConnectOverlay)
	if len(effects) != 0 {
		t.Fatalf("expected no effects for overlay open, got %d", len(effects))
	}
	if model.Overlay.Active.Kind != featureoverlay.KindConnectPicker {
		t.Fatalf("expected connect overlay, got %q", model.Overlay.Active.Kind)
	}
}

func sampleLiveWorkbenchModel() Model {
	ws := coreworkspace.New("main")
	tab := ws.ActiveTab()
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
	tab.Layout = nil

	return Model{
		WorkspaceName: "main",
		Screen:        ScreenWorkbench,
		Workbench: featureworkbench.State{
			Workspace: ws,
			Terminals: map[types.TerminalID]coreterminal.Metadata{
				types.TerminalID("term-1"): {
					ID:              types.TerminalID("term-1"),
					Name:            "shell",
					State:           coreterminal.StateRunning,
					OwnerPaneID:     types.PaneID("pane-1"),
					AttachedPaneIDs: []types.PaneID{types.PaneID("pane-1"), types.PaneID("pane-2")},
				},
			},
		},
	}
}
