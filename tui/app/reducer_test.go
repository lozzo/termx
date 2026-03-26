package app

import (
	"testing"

	corepool "github.com/lozzow/termx/tui/core/pool"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
	coreworkspace "github.com/lozzow/termx/tui/core/workspace"
	featureoverlay "github.com/lozzow/termx/tui/features/overlay"
	featureterminalpool "github.com/lozzow/termx/tui/features/terminalpool"
	featureworkbench "github.com/lozzow/termx/tui/features/workbench"
)

func TestReducerCanSwitchBetweenWorkbenchAndTerminalPool(t *testing.T) {
	model := NewModel("main")

	model, effects := Reduce(model, IntentOpenTerminalPool)
	if len(effects) != 1 {
		t.Fatalf("expected one effect for terminal pool load, got %d", len(effects))
	}
	if _, ok := effects[0].(EffectLoadTerminalPool); !ok {
		t.Fatalf("expected EffectLoadTerminalPool, got %T", effects[0])
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

func TestReducerAppliesLoadedTerminalPoolGroups(t *testing.T) {
	model := NewModel("main")
	model.Workbench.BindActivePane(coreterminal.Metadata{
		ID:              types.TerminalID("term-visible"),
		Name:            "visible-shell",
		State:           coreterminal.StateRunning,
		AttachedPaneIDs: []types.PaneID{types.PaneID("pane-1")},
	})

	model, _ = Reduce(model, MessageTerminalPoolLoaded{
		Terminals: []coreterminal.Metadata{
			{ID: types.TerminalID("term-visible"), Name: "visible-shell", State: coreterminal.StateRunning},
			{ID: types.TerminalID("term-parked"), Name: "parked-shell", State: coreterminal.StateRunning},
			{ID: types.TerminalID("term-exited"), Name: "exited-shell", State: coreterminal.StateExited},
		},
	})

	if len(model.Pool.Visible) != 1 || model.Pool.Visible[0].ID != types.TerminalID("term-visible") {
		t.Fatalf("expected visible pool item, got %#v", model.Pool.Visible)
	}
	if len(model.Pool.Parked) != 1 || model.Pool.Parked[0].ID != types.TerminalID("term-parked") {
		t.Fatalf("expected parked pool item, got %#v", model.Pool.Parked)
	}
	if len(model.Pool.Exited) != 1 || model.Pool.Exited[0].ID != types.TerminalID("term-exited") {
		t.Fatalf("expected exited pool item, got %#v", model.Pool.Exited)
	}
}

func TestPoolFeatureStateMatchesCoreGroupingContract(t *testing.T) {
	groups := corepool.Groups{
		Visible: []coreterminal.Metadata{{ID: types.TerminalID("term-visible"), Name: "visible-shell", State: coreterminal.StateRunning}},
	}
	state := featureterminalpool.State{}
	state.ApplyGroups(groups)
	if state.SelectedTerminalID != types.TerminalID("term-visible") {
		t.Fatalf("expected selected visible terminal, got %q", state.SelectedTerminalID)
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
