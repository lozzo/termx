package reducer

import (
	"testing"
	"time"

	"github.com/lozzow/termx/tui/app/intent"
	"github.com/lozzow/termx/tui/domain/types"
)

func TestReducerConnectTerminalMarksPaneConnectedAndEmitsEffect(t *testing.T) {
	reducer := New()
	state := newAppStateWithSinglePane()

	result := reducer.Reduce(state, intent.ConnectTerminalIntent{
		PaneID:     types.PaneID("pane-1"),
		TerminalID: types.TerminalID("term-1"),
		Source:     intent.ConnectSourcePicker,
	})

	pane := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.SlotState != types.PaneSlotConnected || pane.TerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected pane to become connected, got %+v", pane)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("expected one effect, got %d", len(result.Effects))
	}
}

func TestReducerStopTerminalKeepsUnconnectedPaneSlots(t *testing.T) {
	reducer := New()
	state := newConnectedAppState()

	result := reducer.Reduce(state, intent.StopTerminalIntent{TerminalID: types.TerminalID("term-1")})

	pane := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.SlotState != types.PaneSlotEmpty || pane.TerminalID != "" {
		t.Fatalf("expected pane to become unconnected, got %+v", pane)
	}
}

func TestReducerTerminalProgramExitedMarksPaneExited(t *testing.T) {
	reducer := New()
	state := newConnectedAppState()

	result := reducer.Reduce(state, intent.TerminalProgramExitedIntent{
		TerminalID: types.TerminalID("term-1"),
		ExitCode:   7,
	})

	pane := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.SlotState != types.PaneSlotExited {
		t.Fatalf("expected pane to become exited, got %+v", pane)
	}
	if pane.LastExitCode == nil || *pane.LastExitCode != 7 {
		t.Fatalf("expected exit code to be retained, got %+v", pane.LastExitCode)
	}
}

func TestReducerWorkspaceTreeJumpSwitchesWorkspaceTabAndFocus(t *testing.T) {
	reducer := New()
	state := newAppStateWithTwoWorkspaces()

	result := reducer.Reduce(state, intent.WorkspaceTreeJumpIntent{
		WorkspaceID: types.WorkspaceID("ws-2"),
		TabID:       types.TabID("tab-2"),
		PaneID:      types.PaneID("pane-float"),
	})

	if result.State.Domain.ActiveWorkspaceID != types.WorkspaceID("ws-2") {
		t.Fatalf("expected active workspace to switch, got %q", result.State.Domain.ActiveWorkspaceID)
	}
	if result.State.UI.Focus.TabID != types.TabID("tab-2") || result.State.UI.Focus.PaneID != types.PaneID("pane-float") {
		t.Fatalf("expected focus to jump to target pane, got %+v", result.State.UI.Focus)
	}
	if result.State.UI.Focus.Layer != types.FocusLayerFloating {
		t.Fatalf("expected floating target to focus floating layer, got %q", result.State.UI.Focus.Layer)
	}
}

func TestReducerClosePaneKeepsTerminalAliveAndMigratesOwner(t *testing.T) {
	reducer := New()
	state := newSharedTerminalAppState()

	result := reducer.Reduce(state, intent.ClosePaneIntent{
		PaneID: types.PaneID("pane-1"),
	})

	tab := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")]
	if _, ok := tab.Panes[types.PaneID("pane-1")]; ok {
		t.Fatalf("expected closed pane to be removed from tab")
	}
	if result.State.Domain.Terminals[types.TerminalID("term-1")].ID == "" {
		t.Fatalf("expected terminal to remain alive after close pane")
	}
	conn := result.State.Domain.Connections[types.TerminalID("term-1")]
	if conn.OwnerPaneID != types.PaneID("pane-2") {
		t.Fatalf("expected owner to migrate to pane-2, got %q", conn.OwnerPaneID)
	}
	if len(conn.ConnectedPaneIDs) != 1 || conn.ConnectedPaneIDs[0] != types.PaneID("pane-2") {
		t.Fatalf("expected only pane-2 to remain connected, got %+v", conn.ConnectedPaneIDs)
	}
}

func TestReducerOpenWorkspacePickerMovesFocusToOverlayAndStoresReturnFocus(t *testing.T) {
	reducer := New()
	state := newAppStateWithTwoWorkspaces()

	result := reducer.Reduce(state, intent.OpenWorkspacePickerIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayWorkspacePicker {
		t.Fatalf("expected workspace picker overlay, got %q", result.State.UI.Overlay.Kind)
	}
	if result.State.UI.Focus.Layer != types.FocusLayerOverlay {
		t.Fatalf("expected overlay focus layer, got %+v", result.State.UI.Focus)
	}
	if result.State.UI.Overlay.ReturnFocus.PaneID != types.PaneID("pane-1") {
		t.Fatalf("expected previous pane focus to be retained, got %+v", result.State.UI.Overlay.ReturnFocus)
	}
}

func TestReducerCloseOverlayRestoresPreviousPaneFocus(t *testing.T) {
	reducer := New()
	state := newAppStateWithTwoWorkspaces()

	opened := reducer.Reduce(state, intent.OpenWorkspacePickerIntent{})
	closed := reducer.Reduce(opened.State, intent.CloseOverlayIntent{})

	if closed.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected overlay to close, got %q", closed.State.UI.Overlay.Kind)
	}
	if closed.State.UI.Focus.Layer != types.FocusLayerTiled || closed.State.UI.Focus.PaneID != types.PaneID("pane-1") {
		t.Fatalf("expected focus to return to original pane, got %+v", closed.State.UI.Focus)
	}
}

func TestReducerWorkspacePickerSubmitPaneJumpsAndClosesOverlay(t *testing.T) {
	reducer := New()
	state := newAppStateWithTwoWorkspaces()

	opened := reducer.Reduce(state, intent.OpenWorkspacePickerIntent{})
	movedToWorkspace := reducer.Reduce(opened.State, intent.WorkspacePickerMoveIntent{Delta: 2})
	expanded := reducer.Reduce(movedToWorkspace.State, intent.WorkspacePickerExpandIntent{})
	movedToPane := reducer.Reduce(expanded.State, intent.WorkspacePickerMoveIntent{Delta: 2})
	submitted := reducer.Reduce(movedToPane.State, intent.WorkspacePickerSubmitIntent{})

	if submitted.State.Domain.ActiveWorkspaceID != types.WorkspaceID("ws-2") {
		t.Fatalf("expected active workspace to switch to ws-2, got %q", submitted.State.Domain.ActiveWorkspaceID)
	}
	if submitted.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected overlay to close after submit, got %q", submitted.State.UI.Overlay.Kind)
	}
	if submitted.State.UI.Focus.PaneID != types.PaneID("pane-float") || submitted.State.UI.Focus.Layer != types.FocusLayerFloating {
		t.Fatalf("expected focus to jump to target pane, got %+v", submitted.State.UI.Focus)
	}
}

func TestReducerModeTimedOutClearsActiveMode(t *testing.T) {
	reducer := New()
	state := newAppStateWithSinglePane()
	deadline := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)

	activated := reducer.Reduce(state, intent.ActivateModeIntent{
		Mode:       types.ModeResize,
		Sticky:     false,
		DeadlineAt: &deadline,
	})
	result := reducer.Reduce(activated.State, intent.ModeTimedOutIntent{
		Now: deadline.Add(time.Second),
	})

	if result.State.UI.Mode.Active != types.ModeNone {
		t.Fatalf("expected mode to clear after timeout, got %+v", result.State.UI.Mode)
	}
	if result.State.UI.Mode.DeadlineAt != nil {
		t.Fatalf("expected mode deadline cleared, got %+v", result.State.UI.Mode.DeadlineAt)
	}
}

func newAppStateWithSinglePane() types.AppState {
	return types.AppState{
		Domain: types.DomainState{
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
							ActiveLayer:  types.FocusLayerTiled,
							Panes: map[types.PaneID]types.PaneState{
								types.PaneID("pane-1"): {
									ID:        types.PaneID("pane-1"),
									Kind:      types.PaneKindTiled,
									SlotState: types.PaneSlotEmpty,
								},
							},
						},
					},
				},
			},
			Terminals:   map[types.TerminalID]types.TerminalRef{},
			Connections: map[types.TerminalID]types.ConnectionState{},
		},
		UI: types.UIState{
			Focus: types.FocusState{
				Layer:       types.FocusLayerTiled,
				WorkspaceID: types.WorkspaceID("ws-1"),
				TabID:       types.TabID("tab-1"),
				PaneID:      types.PaneID("pane-1"),
			},
		},
	}
}

func newConnectedAppState() types.AppState {
	state := newAppStateWithSinglePane()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	pane := tab.Panes[types.PaneID("pane-1")]
	pane.SlotState = types.PaneSlotConnected
	pane.TerminalID = types.TerminalID("term-1")
	tab.Panes[types.PaneID("pane-1")] = pane
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:    types.TerminalID("term-1"),
		Name:  "api-dev",
		State: types.TerminalRunStateRunning,
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	return state
}

func newAppStateWithTwoWorkspaces() types.AppState {
	state := newAppStateWithSinglePane()
	state.Domain.WorkspaceOrder = append(state.Domain.WorkspaceOrder, types.WorkspaceID("ws-2"))
	state.Domain.Workspaces[types.WorkspaceID("ws-2")] = types.WorkspaceState{
		ID:          types.WorkspaceID("ws-2"),
		Name:        "ws-2",
		ActiveTabID: types.TabID("tab-2"),
		TabOrder:    []types.TabID{types.TabID("tab-2")},
		Tabs: map[types.TabID]types.TabState{
			types.TabID("tab-2"): {
				ID:           types.TabID("tab-2"),
				Name:         "tab-2",
				ActivePaneID: types.PaneID("pane-float"),
				ActiveLayer:  types.FocusLayerFloating,
				FloatingOrder: []types.PaneID{
					types.PaneID("pane-float"),
				},
				Panes: map[types.PaneID]types.PaneState{
					types.PaneID("pane-float"): {
						ID:        types.PaneID("pane-float"),
						Kind:      types.PaneKindFloating,
						SlotState: types.PaneSlotEmpty,
					},
				},
			},
		},
	}
	return state
}

func newSharedTerminalAppState() types.AppState {
	state := newAppStateWithSinglePane()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	tab.Panes[types.PaneID("pane-2")] = types.PaneState{
		ID:         types.PaneID("pane-2"),
		Kind:       types.PaneKindTiled,
		SlotState:  types.PaneSlotConnected,
		TerminalID: types.TerminalID("term-1"),
	}
	tab.ActivePaneID = types.PaneID("pane-1")
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws

	pane := tab.Panes[types.PaneID("pane-1")]
	pane.SlotState = types.PaneSlotConnected
	pane.TerminalID = types.TerminalID("term-1")
	tab.Panes[types.PaneID("pane-1")] = pane
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws

	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:    types.TerminalID("term-1"),
		Name:  "shared-term",
		State: types.TerminalRunStateRunning,
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1"), types.PaneID("pane-2")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	return state
}
