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
	if len(model.Pool.Visible) != 1 || model.Pool.Visible[0].ID != types.TerminalID("term-1") {
		t.Fatalf("expected shared terminal to stay visible after one pane disconnect, got %#v", model.Pool.Visible)
	}

	model = sampleLiveWorkbenchModel()
	model, _ = Reduce(model, MessageTerminalExited{TerminalID: types.TerminalID("term-1")})
	if got := model.Workbench.Workspace.ActiveTab().Panes[types.PaneID("pane-1")].SlotState; got != types.PaneSlotExited {
		t.Fatalf("expected exited after kill, got %q", got)
	}
	if len(model.Pool.Exited) != 1 || model.Pool.Exited[0].ID != types.TerminalID("term-1") {
		t.Fatalf("expected terminal in exited group after kill, got %#v", model.Pool.Exited)
	}

	model = sampleLiveWorkbenchModel()
	model, _ = Reduce(model, MessageTerminalRemoved{TerminalID: types.TerminalID("term-1")})
	if got := model.Workbench.Workspace.ActiveTab().Panes[types.PaneID("pane-1")].SlotState; got != types.PaneSlotUnconnected {
		t.Fatalf("expected unconnected after remove, got %q", got)
	}
	if got := model.Workbench.Workspace.ActiveTab().Panes[types.PaneID("pane-1")].TerminalID; got != "" {
		t.Fatalf("expected pane terminal cleared after remove, got %q", got)
	}
	if len(model.Pool.Visible)+len(model.Pool.Parked)+len(model.Pool.Exited) != 0 {
		t.Fatalf("expected removed terminal to disappear from pool, got %#v %#v %#v", model.Pool.Visible, model.Pool.Parked, model.Pool.Exited)
	}
}

func TestReducerOpensConnectOverlayForUnconnectedPane(t *testing.T) {
	model := sampleLiveWorkbenchModel()
	model, effects := Reduce(model, IntentOpenConnectOverlay)
	if len(effects) != 0 {
		t.Fatalf("expected no effects for overlay open, got %d", len(effects))
	}
	if model.Overlay.Active.Kind != featureoverlay.KindConnectPicker {
		t.Fatalf("expected connect overlay, got %q", model.Overlay.Active.Kind)
	}
	if model.Overlay.Active.Selected != types.TerminalID("term-1") || len(model.Overlay.Active.Items) != 1 {
		t.Fatalf("expected connect picker items from pool, got %#v", model.Overlay.Active)
	}
}

func TestReducerOpensHelpOverlay(t *testing.T) {
	model := NewModel("main")
	model, effects := Reduce(model, IntentOpenHelpOverlay)
	if len(effects) != 0 {
		t.Fatalf("expected no effects for help overlay open, got %d", len(effects))
	}
	if model.Overlay.Active.Kind != featureoverlay.KindHelp {
		t.Fatalf("expected help overlay, got %q", model.Overlay.Active.Kind)
	}
}

func TestReducerConnectIntentReturnsConnectAndReloadEffects(t *testing.T) {
	model := NewModel("main")
	_, effects := Reduce(model, IntentConnectTerminal{TerminalID: types.TerminalID("term-2")})
	if len(effects) != 2 {
		t.Fatalf("expected connect and reload effects, got %d", len(effects))
	}
	if got, ok := effects[0].(EffectConnectTerminal); !ok || got.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected first effect to connect term-2, got %#v", effects[0])
	}
	if _, ok := effects[1].(EffectLoadTerminalPool); !ok {
		t.Fatalf("expected second effect to reload pool, got %#v", effects[1])
	}
}

func TestReducerConnectOverlaySelectionAndConfirm(t *testing.T) {
	model := sampleLiveWorkbenchModel()
	model.Pool.ApplyGroups(corepool.Groups{
		Visible: []coreterminal.Metadata{{ID: types.TerminalID("term-1"), Name: "shell", State: coreterminal.StateRunning}},
		Parked:  []coreterminal.Metadata{{ID: types.TerminalID("term-2"), Name: "logs", State: coreterminal.StateRunning}},
	})

	model, _ = Reduce(model, IntentOpenConnectOverlay)
	model, effects := Reduce(model, IntentOverlaySelectNext)
	if len(effects) != 0 {
		t.Fatalf("expected no effects for overlay selection, got %d", len(effects))
	}
	if model.Overlay.Active.Selected != types.TerminalID("term-2") {
		t.Fatalf("expected connect overlay moved to term-2, got %q", model.Overlay.Active.Selected)
	}

	_, effects = Reduce(model, IntentOverlayConfirmConnect)
	if len(effects) != 2 {
		t.Fatalf("expected connect confirm effects, got %d", len(effects))
	}
	if got, ok := effects[0].(EffectConnectTerminal); !ok || got.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected connect selected term-2, got %#v", effects[0])
	}
}

func TestReducerPoolSelectionAndSelectedTerminalActions(t *testing.T) {
	model := sampleLiveWorkbenchModel()
	model.Pool.ApplyGroups(corepool.Groups{
		Visible: []coreterminal.Metadata{{ID: types.TerminalID("term-1"), Name: "shell", State: coreterminal.StateRunning}},
		Parked:  []coreterminal.Metadata{{ID: types.TerminalID("term-2"), Name: "logs", State: coreterminal.StateRunning}},
	})

	model, effects := Reduce(model, IntentPoolSelectNext)
	if len(effects) != 0 {
		t.Fatalf("expected no effects for pool selection, got %d", len(effects))
	}
	if model.Pool.SelectedTerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected pool selection moved to term-2, got %q", model.Pool.SelectedTerminalID)
	}

	_, effects = Reduce(model, IntentKillSelectedTerminal{})
	if len(effects) != 2 {
		t.Fatalf("expected kill plus reload effects, got %d", len(effects))
	}
	if got, ok := effects[0].(EffectKillTerminal); !ok || got.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected kill selected term-2, got %#v", effects[0])
	}

	_, effects = Reduce(model, IntentRemoveSelectedTerminal{})
	if len(effects) != 2 {
		t.Fatalf("expected remove plus reload effects, got %d", len(effects))
	}
	if got, ok := effects[0].(EffectRemoveTerminal); !ok || got.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected remove selected term-2, got %#v", effects[0])
	}
}

func TestReducerMessageTerminalConnectedBindsPaneAndClearsOverlay(t *testing.T) {
	model := sampleLiveWorkbenchModel()
	model.Overlay = model.Overlay.OpenConnectPicker(model.Pool.Visible, types.TerminalID("term-1"))
	connected := coreterminal.Metadata{ID: types.TerminalID("term-9"), Name: "restored-shell", State: coreterminal.StateRunning}

	model, effects := Reduce(model, MessageTerminalConnected{Terminal: connected})
	if len(effects) != 0 {
		t.Fatalf("expected no follow-up effects, got %d", len(effects))
	}
	if got := model.Workbench.ActivePane().TerminalID; got != types.TerminalID("term-9") {
		t.Fatalf("expected active pane bound to term-9, got %q", got)
	}
	if model.Overlay.Active.Kind != "" {
		t.Fatalf("expected overlay cleared after connect, got %q", model.Overlay.Active.Kind)
	}
	if len(model.Pool.Visible) != 2 {
		t.Fatalf("expected connected terminal visible in pool, got %#v", model.Pool.Visible)
	}
	found := false
	for _, item := range model.Pool.Visible {
		if item.ID == types.TerminalID("term-9") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected connected terminal term-9 in pool, got %#v", model.Pool.Visible)
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

	model := Model{
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
			Sessions: map[types.TerminalID]featureworkbench.SessionState{},
		},
	}
	model.Pool.ApplyGroups(corepool.BuildGroups(indexTerminalMetadataFromWorkbench(model.Workbench.Terminals), model.Workbench.VisibleTerminalIDs(), model.Pool.Query))
	return model
}
