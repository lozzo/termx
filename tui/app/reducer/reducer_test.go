package reducer

import (
	"testing"
	"time"

	"github.com/lozzow/termx/tui/app/intent"
	promptdomain "github.com/lozzow/termx/tui/domain/prompt"
	terminalmanagerdomain "github.com/lozzow/termx/tui/domain/terminalmanager"
	terminalpickerdomain "github.com/lozzow/termx/tui/domain/terminalpicker"
	"github.com/lozzow/termx/tui/domain/types"
	workspacedomain "github.com/lozzow/termx/tui/domain/workspace"
)

type terminalPickerOverlay interface {
	Query() string
	SelectedRow() (terminalpickerdomain.Row, bool)
}

type workspacePickerOverlay interface {
	Query() string
	SelectedRow() (workspacedomain.TreeRow, bool)
}

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

func TestReducerRestartProgramExitedTerminalEmitsCreateEffectWithOriginalCommand(t *testing.T) {
	reducer := New()
	state := newConnectedAppState()
	exitCode := 7
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	pane := tab.Panes[types.PaneID("pane-1")]
	pane.SlotState = types.PaneSlotExited
	pane.LastExitCode = &exitCode
	tab.Panes[types.PaneID("pane-1")] = pane
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "deploy-log",
		Command: []string{"npm", "run", "deploy"},
		State:   types.TerminalRunStateExited,
		Visible: true,
	}

	result := reducer.Reduce(state, intent.RestartProgramExitedTerminalIntent{PaneID: types.PaneID("pane-1")})

	if len(result.Effects) != 1 {
		t.Fatalf("expected one restart create effect, got %d", len(result.Effects))
	}
	effect, ok := result.Effects[0].(CreateTerminalEffect)
	if !ok {
		t.Fatalf("expected create effect for restart, got %T", result.Effects[0])
	}
	if effect.PaneID != types.PaneID("pane-1") || effect.Name != "deploy-log" {
		t.Fatalf("unexpected restart effect identity: %+v", effect)
	}
	if len(effect.Command) != 3 || effect.Command[0] != "npm" || effect.Command[2] != "deploy" {
		t.Fatalf("expected restart to reuse terminal command, got %+v", effect.Command)
	}
}

func TestReducerTerminalRemovedClearsPaneAndTerminalState(t *testing.T) {
	reducer := New()
	state := newConnectedAppState()

	result := reducer.Reduce(state, intent.TerminalRemovedIntent{
		TerminalID: types.TerminalID("term-1"),
	})

	pane := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.TerminalID != "" || pane.SlotState != types.PaneSlotEmpty {
		t.Fatalf("expected removed terminal pane to become empty, got %+v", pane)
	}
	if _, ok := result.State.Domain.Terminals[types.TerminalID("term-1")]; ok {
		t.Fatalf("expected removed terminal to disappear from domain, got %+v", result.State.Domain.Terminals[types.TerminalID("term-1")])
	}
	if _, ok := result.State.Domain.Connections[types.TerminalID("term-1")]; ok {
		t.Fatalf("expected removed terminal connection to disappear")
	}
}

func TestReducerTerminalRemovedRefreshesTerminalManagerProjection(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	result := reducer.Reduce(opened.State, intent.TerminalRemovedIntent{
		TerminalID: types.TerminalID("term-1"),
	})

	manager, ok := result.State.UI.Overlay.Data.(*terminalmanagerdomain.State)
	if !ok {
		t.Fatalf("expected terminal manager overlay data, got %T", result.State.UI.Overlay.Data)
	}
	row, ok := manager.SelectedRow()
	if !ok || row.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected selection to fall through to next terminal after removal, got %+v ok=%v", row, ok)
	}
	detail, ok := manager.SelectedDetail()
	if !ok || detail.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected detail projection to refresh after removal, got %+v ok=%v", detail, ok)
	}
}

func TestReducerRegisterTerminalAddsDetachedTerminalRef(t *testing.T) {
	reducer := New()
	state := newAppStateWithSinglePane()

	result := reducer.Reduce(state, intent.RegisterTerminalIntent{
		TerminalID: types.TerminalID("term-2"),
		Name:       "build-log",
		Command:    []string{"tail", "-f", "build.log"},
		State:      types.TerminalRunStateRunning,
	})

	terminal, ok := result.State.Domain.Terminals[types.TerminalID("term-2")]
	if !ok {
		t.Fatal("expected registered terminal to be stored in domain")
	}
	if terminal.ID != types.TerminalID("term-2") || terminal.Name != "build-log" {
		t.Fatalf("unexpected registered terminal identity: %+v", terminal)
	}
	if terminal.State != types.TerminalRunStateRunning {
		t.Fatalf("expected registered terminal to be running, got %+v", terminal)
	}
	if len(terminal.Command) != 3 || terminal.Command[0] != "tail" {
		t.Fatalf("expected registered command to be cloned, got %+v", terminal.Command)
	}
	if terminal.Visible {
		t.Fatalf("expected detached runtime terminal to remain non-visible, got %+v", terminal)
	}
	if len(result.Effects) != 0 {
		t.Fatalf("expected register terminal to be side-effect free, got %+v", result.Effects)
	}
}

func TestReducerSyncTerminalStateStoppedClearsPaneConnection(t *testing.T) {
	reducer := New()
	state := newConnectedAppState()

	result := reducer.Reduce(state, intent.SyncTerminalStateIntent{
		TerminalID: types.TerminalID("term-1"),
		State:      types.TerminalRunStateStopped,
	})

	pane := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.TerminalID != "" || pane.SlotState != types.PaneSlotEmpty {
		t.Fatalf("expected stopped terminal pane to become empty, got %+v", pane)
	}
	terminal := result.State.Domain.Terminals[types.TerminalID("term-1")]
	if terminal.State != types.TerminalRunStateStopped {
		t.Fatalf("expected terminal state stopped, got %+v", terminal)
	}
	if _, ok := result.State.Domain.Connections[types.TerminalID("term-1")]; ok {
		t.Fatalf("expected stopped terminal connection to disappear")
	}
}

func TestReducerSyncTerminalStateStoppedRefreshesTerminalManagerProjection(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	result := reducer.Reduce(opened.State, intent.SyncTerminalStateIntent{
		TerminalID: types.TerminalID("term-1"),
		State:      types.TerminalRunStateStopped,
	})

	manager, ok := result.State.UI.Overlay.Data.(*terminalmanagerdomain.State)
	if !ok {
		t.Fatalf("expected terminal manager overlay data, got %T", result.State.UI.Overlay.Data)
	}
	detail, ok := manager.SelectedDetail()
	if !ok {
		t.Fatalf("expected selected detail after state sync")
	}
	if detail.TerminalID != types.TerminalID("term-1") || detail.State != types.TerminalRunStateStopped || detail.ConnectedPaneCount != 0 || detail.VisibilityLabel != "hidden" {
		t.Fatalf("expected stopped terminal detail to refresh, got %+v", detail)
	}
	row, ok := manager.SelectedRow()
	if !ok || row.TerminalID != types.TerminalID("term-1") || row.State != types.TerminalRunStateStopped || row.ConnectedPaneCount != 0 || row.VisibilityLabel != "hidden" {
		t.Fatalf("expected stopped terminal row to refresh, got %+v ok=%v", row, ok)
	}
}

func TestReducerSyncTerminalStateRunningClearsExitCodeOnly(t *testing.T) {
	reducer := New()
	state := newConnectedAppState()
	exitCode := 7
	terminal := state.Domain.Terminals[types.TerminalID("term-1")]
	terminal.State = types.TerminalRunStateExited
	terminal.ExitCode = &exitCode
	state.Domain.Terminals[types.TerminalID("term-1")] = terminal

	result := reducer.Reduce(state, intent.SyncTerminalStateIntent{
		TerminalID: types.TerminalID("term-1"),
		State:      types.TerminalRunStateRunning,
	})

	pane := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.TerminalID != types.TerminalID("term-1") || pane.SlotState != types.PaneSlotConnected {
		t.Fatalf("expected running sync to keep pane connected, got %+v", pane)
	}
	terminal = result.State.Domain.Terminals[types.TerminalID("term-1")]
	if terminal.State != types.TerminalRunStateRunning || terminal.ExitCode != nil {
		t.Fatalf("expected terminal to return running without exit code, got %+v", terminal)
	}
}

func TestReducerSyncTerminalStateExitedWithoutExitCodeKeepsExitedPane(t *testing.T) {
	reducer := New()
	state := newConnectedAppState()

	result := reducer.Reduce(state, intent.SyncTerminalStateIntent{
		TerminalID: types.TerminalID("term-1"),
		State:      types.TerminalRunStateExited,
	})

	pane := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.SlotState != types.PaneSlotExited || pane.LastExitCode != nil {
		t.Fatalf("expected exited pane without exit code to keep history but no code, got %+v", pane)
	}
	terminal := result.State.Domain.Terminals[types.TerminalID("term-1")]
	if terminal.State != types.TerminalRunStateExited || terminal.ExitCode != nil {
		t.Fatalf("expected terminal exited without exit code, got %+v", terminal)
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

func TestReducerWorkspaceTreeJumpAutoAcquireOwnerOnTabEnter(t *testing.T) {
	reducer := New()
	state := newWorkspaceJumpAutoAcquireState(true)

	result := reducer.Reduce(state, intent.WorkspaceTreeJumpIntent{
		WorkspaceID: types.WorkspaceID("ws-2"),
		TabID:       types.TabID("tab-2"),
		PaneID:      types.PaneID("pane-2"),
	})

	conn := result.State.Domain.Connections[types.TerminalID("term-1")]
	if conn.OwnerPaneID != types.PaneID("pane-2") {
		t.Fatalf("expected workspace jump auto-acquire to transfer owner, got %+v", conn)
	}
}

func TestReducerWorkspaceTreeJumpWithoutAutoAcquireKeepsExistingOwner(t *testing.T) {
	reducer := New()
	state := newWorkspaceJumpAutoAcquireState(false)

	result := reducer.Reduce(state, intent.WorkspaceTreeJumpIntent{
		WorkspaceID: types.WorkspaceID("ws-2"),
		TabID:       types.TabID("tab-2"),
		PaneID:      types.PaneID("pane-2"),
	})

	conn := result.State.Domain.Connections[types.TerminalID("term-1")]
	if conn.OwnerPaneID != types.PaneID("pane-1") {
		t.Fatalf("expected workspace jump without auto-acquire to keep owner, got %+v", conn)
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

func TestReducerOpenTerminalPickerMovesFocusToOverlayAndStoresReturnFocus(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	result := reducer.Reduce(state, intent.OpenTerminalPickerIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayTerminalPicker {
		t.Fatalf("expected terminal picker overlay, got %q", result.State.UI.Overlay.Kind)
	}
	if result.State.UI.Focus.Layer != types.FocusLayerOverlay {
		t.Fatalf("expected overlay focus layer, got %+v", result.State.UI.Focus)
	}
	if result.State.UI.Overlay.ReturnFocus.PaneID != types.PaneID("pane-1") {
		t.Fatalf("expected previous pane focus retained, got %+v", result.State.UI.Overlay.ReturnFocus)
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

func TestReducerTerminalPickerSearchMovesSelectionToMatchedTerminal(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalPickerIntent{})
	typed := reducer.Reduce(opened.State, intent.TerminalPickerAppendQueryIntent{Text: "ops"})

	picker, ok := typed.State.UI.Overlay.Data.(terminalPickerOverlay)
	if !ok {
		t.Fatalf("expected terminal picker overlay data, got %T", typed.State.UI.Overlay.Data)
	}
	if picker.Query() != "ops" {
		t.Fatalf("expected query to update, got %q", picker.Query())
	}
	row, ok := picker.SelectedRow()
	if !ok || row.TerminalID != types.TerminalID("term-3") {
		t.Fatalf("expected search to select ops terminal, got %+v ok=%v", row, ok)
	}
}

func TestReducerTerminalPickerSubmitConnectsSelectedTerminalAndClosesOverlay(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalPickerIntent{})
	moved := reducer.Reduce(opened.State, intent.TerminalPickerMoveIntent{Delta: 1})
	result := reducer.Reduce(moved.State, intent.TerminalPickerSubmitIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected terminal picker overlay to close, got %q", result.State.UI.Overlay.Kind)
	}
	pane := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected picker to connect selected terminal, got %+v", pane)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("expected one connect effect, got %d", len(result.Effects))
	}
	effect, ok := result.Effects[0].(ConnectTerminalEffect)
	if !ok || effect.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("unexpected picker connect effect: %+v %T", result.Effects[0], result.Effects[0])
	}
}

func TestReducerTerminalPickerSubmitCreateRowEmitsCreateEffect(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalPickerIntent{})
	moved := reducer.Reduce(opened.State, intent.TerminalPickerMoveIntent{Delta: -100})
	result := reducer.Reduce(moved.State, intent.TerminalPickerSubmitIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayTerminalPicker {
		t.Fatalf("expected picker overlay to stay open until create success, got %q", result.State.UI.Overlay.Kind)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("expected one create effect, got %d", len(result.Effects))
	}
	effect, ok := result.Effects[0].(CreateTerminalEffect)
	if !ok || effect.Name != "ws-1-tab-1-pane-1" {
		t.Fatalf("unexpected picker create effect: %+v %T", result.Effects[0], result.Effects[0])
	}
}

func TestReducerCreateTerminalSucceededClosesPickerRegistersTerminalAndConnectsPane(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalPickerIntent{})
	moved := reducer.Reduce(opened.State, intent.TerminalPickerMoveIntent{Delta: -100})
	result := reducer.Reduce(moved.State, intent.CreateTerminalSucceededIntent{
		PaneID:     types.PaneID("pane-1"),
		TerminalID: types.TerminalID("term-created"),
		Name:       "ws-1-tab-1-pane-1",
		Command:    []string{"sh", "-l"},
		State:      types.TerminalRunStateRunning,
	})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected picker overlay to close after create success, got %q", result.State.UI.Overlay.Kind)
	}
	pane := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.TerminalID != types.TerminalID("term-created") || pane.SlotState != types.PaneSlotConnected {
		t.Fatalf("expected picker create success to connect pane-1, got %+v", pane)
	}
	terminal := result.State.Domain.Terminals[types.TerminalID("term-created")]
	if terminal.ID != types.TerminalID("term-created") || terminal.Name != "ws-1-tab-1-pane-1" || terminal.State != types.TerminalRunStateRunning || !terminal.Visible {
		t.Fatalf("unexpected registered terminal after create success: %+v", terminal)
	}
	oldConn := result.State.Domain.Connections[types.TerminalID("term-1")]
	if len(oldConn.ConnectedPaneIDs) != 1 || oldConn.ConnectedPaneIDs[0] != types.PaneID("float-2") || oldConn.OwnerPaneID != types.PaneID("float-2") {
		t.Fatalf("expected old shared terminal to keep only float-2 after picker create connect, got %+v", oldConn)
	}
}

func TestReducerWorkspacePickerInputQueryMovesSelectionToMatchedPane(t *testing.T) {
	reducer := New()
	state := newAppStateWithTwoWorkspaces()

	opened := reducer.Reduce(state, intent.OpenWorkspacePickerIntent{})
	typed := reducer.Reduce(opened.State, intent.WorkspacePickerAppendQueryIntent{Text: "float-dev"})

	picker, ok := typed.State.UI.Overlay.Data.(workspacePickerOverlay)
	if !ok {
		t.Fatalf("expected workspace picker overlay data, got %T", typed.State.UI.Overlay.Data)
	}
	if picker.Query() != "float-dev" {
		t.Fatalf("expected query to update, got %q", picker.Query())
	}
	row, ok := picker.SelectedRow()
	if !ok {
		t.Fatalf("expected selected row after typing query")
	}
	if row.Node.Kind != workspacedomain.TreeNodeKindPane || row.Node.PaneID != types.PaneID("pane-float") {
		t.Fatalf("expected query to select matched pane, got %+v", row.Node)
	}
}

func TestReducerWorkspacePickerBackspaceShrinksQuery(t *testing.T) {
	reducer := New()
	state := newAppStateWithTwoWorkspaces()

	opened := reducer.Reduce(state, intent.OpenWorkspacePickerIntent{})
	typed := reducer.Reduce(opened.State, intent.WorkspacePickerAppendQueryIntent{Text: "float"})
	backspaced := reducer.Reduce(typed.State, intent.WorkspacePickerBackspaceIntent{})

	picker, ok := backspaced.State.UI.Overlay.Data.(workspacePickerOverlay)
	if !ok {
		t.Fatalf("expected workspace picker overlay data, got %T", backspaced.State.UI.Overlay.Data)
	}
	if picker.Query() != "floa" {
		t.Fatalf("expected query to shrink after backspace, got %q", picker.Query())
	}
}

func TestReducerWorkspacePickerSubmitCreateRowEmitsPromptEffect(t *testing.T) {
	reducer := New()
	state := newAppStateWithSinglePane()

	opened := reducer.Reduce(state, intent.OpenWorkspacePickerIntent{})
	movedToCreate := reducer.Reduce(opened.State, intent.WorkspacePickerMoveIntent{Delta: -100})
	result := reducer.Reduce(movedToCreate.State, intent.WorkspacePickerSubmitIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected overlay to close before prompt handoff, got %q", result.State.UI.Overlay.Kind)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("expected one prompt effect, got %d", len(result.Effects))
	}
	effect, ok := result.Effects[0].(OpenPromptEffect)
	if !ok {
		t.Fatalf("expected open prompt effect, got %T", result.Effects[0])
	}
	if effect.PromptKind != PromptKindCreateWorkspace {
		t.Fatalf("expected create-workspace prompt kind, got %q", effect.PromptKind)
	}
}

func TestReducerOpenPromptMovesFocusToPromptLayer(t *testing.T) {
	reducer := New()
	state := newAppStateWithSinglePane()

	result := reducer.Reduce(state, intent.OpenPromptIntent{
		PromptKind: PromptKindCreateWorkspace,
	})

	if result.State.UI.Overlay.Kind != types.OverlayPrompt {
		t.Fatalf("expected prompt overlay, got %q", result.State.UI.Overlay.Kind)
	}
	if result.State.UI.Focus.Layer != types.FocusLayerPrompt {
		t.Fatalf("expected prompt focus layer, got %+v", result.State.UI.Focus)
	}
	prompt, ok := result.State.UI.Overlay.Data.(*promptdomain.State)
	if !ok {
		t.Fatalf("expected prompt overlay data, got %T", result.State.UI.Overlay.Data)
	}
	if prompt.Kind != promptdomain.KindCreateWorkspace {
		t.Fatalf("expected create-workspace prompt state, got %+v", prompt)
	}
	if prompt.Draft != "" {
		t.Fatalf("expected create-workspace prompt to start empty, got %q", prompt.Draft)
	}
}

func TestReducerCancelPromptRestoresPreviousPaneFocus(t *testing.T) {
	reducer := New()
	state := newAppStateWithSinglePane()

	opened := reducer.Reduce(state, intent.OpenPromptIntent{
		PromptKind: PromptKindCreateWorkspace,
	})
	cancelled := reducer.Reduce(opened.State, intent.CancelPromptIntent{})

	if cancelled.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected prompt overlay to close, got %q", cancelled.State.UI.Overlay.Kind)
	}
	if cancelled.State.UI.Focus.Layer != types.FocusLayerTiled || cancelled.State.UI.Focus.PaneID != types.PaneID("pane-1") {
		t.Fatalf("expected focus to restore to pane, got %+v", cancelled.State.UI.Focus)
	}
}

func TestReducerSubmitCreateWorkspacePromptCreatesWorkspaceAndFocusesNewPane(t *testing.T) {
	reducer := New()
	state := newAppStateWithSinglePane()

	opened := reducer.Reduce(state, intent.OpenPromptIntent{
		PromptKind: PromptKindCreateWorkspace,
	})
	submitted := reducer.Reduce(opened.State, intent.SubmitPromptIntent{
		Value: "ops-center",
	})

	if submitted.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected prompt to close after submit, got %q", submitted.State.UI.Overlay.Kind)
	}
	if submitted.State.Domain.ActiveWorkspaceID == types.WorkspaceID("ws-1") {
		t.Fatalf("expected active workspace to switch to newly created one")
	}
	createdID := submitted.State.Domain.ActiveWorkspaceID
	workspace, ok := submitted.State.Domain.Workspaces[createdID]
	if !ok {
		t.Fatalf("expected new workspace in state, got %q", createdID)
	}
	if workspace.Name != "ops-center" {
		t.Fatalf("expected workspace name to match prompt, got %+v", workspace)
	}
	if len(workspace.TabOrder) != 1 {
		t.Fatalf("expected default tab to exist, got %+v", workspace.TabOrder)
	}
	tab := workspace.Tabs[workspace.ActiveTabID]
	pane := tab.Panes[tab.ActivePaneID]
	if pane.SlotState != types.PaneSlotEmpty || pane.TerminalID != "" {
		t.Fatalf("expected default pane to be unconnected, got %+v", pane)
	}
	if submitted.State.UI.Focus.WorkspaceID != createdID || submitted.State.UI.Focus.TabID != workspace.ActiveTabID || submitted.State.UI.Focus.PaneID != tab.ActivePaneID {
		t.Fatalf("expected focus to land on new workspace pane, got %+v", submitted.State.UI.Focus)
	}
}

func TestReducerSubmitMetadataPromptKeepsPromptOpenUntilSuccessAndEmitsEffect(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenPromptIntent{
		PromptKind: PromptKindEditTerminalMetadata,
		TerminalID: types.TerminalID("term-2"),
	})
	submitted := reducer.Reduce(opened.State, intent.SubmitPromptIntent{
		Value: "build-log-v2\nenv=prod,team=platform",
	})

	if submitted.State.UI.Overlay.Kind != types.OverlayPrompt {
		t.Fatalf("expected prompt to stay open until metadata service success, got %q", submitted.State.UI.Overlay.Kind)
	}
	terminal := submitted.State.Domain.Terminals[types.TerminalID("term-2")]
	if terminal.Name != "build-log" {
		t.Fatalf("expected terminal name to stay unchanged before success feedback, got %+v", terminal)
	}
	if terminal.Tags["group"] != "build" || len(terminal.Tags) != 1 {
		t.Fatalf("expected terminal tags to stay unchanged before success feedback, got %+v", terminal.Tags)
	}
	if len(submitted.Effects) != 1 {
		t.Fatalf("expected one metadata effect, got %d", len(submitted.Effects))
	}
	effect, ok := submitted.Effects[0].(UpdateTerminalMetadataEffect)
	if !ok {
		t.Fatalf("expected metadata effect, got %T", submitted.Effects[0])
	}
	if effect.TerminalID != types.TerminalID("term-2") || effect.Name != "build-log-v2" {
		t.Fatalf("unexpected metadata effect payload: %+v", effect)
	}
	if effect.Tags["env"] != "prod" || effect.Tags["team"] != "platform" {
		t.Fatalf("expected metadata effect tags, got %+v", effect.Tags)
	}
}

func TestReducerUpdateTerminalMetadataSucceededUpdatesTerminalAndClosesPrompt(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenPromptIntent{
		PromptKind: PromptKindEditTerminalMetadata,
		TerminalID: types.TerminalID("term-2"),
	})
	result := reducer.Reduce(opened.State, intent.UpdateTerminalMetadataSucceededIntent{
		TerminalID: types.TerminalID("term-2"),
		Name:       "build-log-v2",
		Tags: map[string]string{
			"group": "build",
			"env":   "prod",
		},
	})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected prompt to close after metadata success, got %q", result.State.UI.Overlay.Kind)
	}
	terminal := result.State.Domain.Terminals[types.TerminalID("term-2")]
	if terminal.Name != "build-log-v2" {
		t.Fatalf("expected terminal name to update after success, got %+v", terminal)
	}
	if terminal.Tags["group"] != "build" || terminal.Tags["env"] != "prod" {
		t.Fatalf("expected terminal tags to update after success, got %+v", terminal.Tags)
	}
}

func TestReducerSubmitMetadataPromptWithoutOwnerKeepsPromptOpenAndEmitsNotice(t *testing.T) {
	reducer := New()
	state := newFollowerManagerAppState()

	opened := reducer.Reduce(state, intent.OpenPromptIntent{
		PromptKind: PromptKindEditTerminalMetadata,
		TerminalID: types.TerminalID("term-1"),
	})
	submitted := reducer.Reduce(opened.State, intent.SubmitPromptIntent{
		Value: "api-dev-v2\nenv=prod",
	})

	if submitted.State.UI.Overlay.Kind != types.OverlayPrompt {
		t.Fatalf("expected prompt to stay open without owner, got %q", submitted.State.UI.Overlay.Kind)
	}
	terminal := submitted.State.Domain.Terminals[types.TerminalID("term-1")]
	if terminal.Name != "api-dev" {
		t.Fatalf("expected terminal metadata to remain unchanged without owner, got %+v", terminal)
	}
	if len(submitted.Effects) != 1 {
		t.Fatalf("expected one notice effect when owner missing, got %d", len(submitted.Effects))
	}
	effect, ok := submitted.Effects[0].(NoticeEffect)
	if !ok {
		t.Fatalf("expected notice effect, got %T", submitted.Effects[0])
	}
	if effect.Level != NoticeLevelError || effect.Text != "terminal metadata update requires owner; acquire owner first" {
		t.Fatalf("unexpected notice effect payload: %+v", effect)
	}
}

func TestReducerOpenMetadataPromptSeedsDraftFromCurrentTerminal(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	result := reducer.Reduce(state, intent.OpenPromptIntent{
		PromptKind: PromptKindEditTerminalMetadata,
		TerminalID: types.TerminalID("term-2"),
	})

	prompt, ok := result.State.UI.Overlay.Data.(*promptdomain.State)
	if !ok {
		t.Fatalf("expected prompt overlay data, got %T", result.State.UI.Overlay.Data)
	}
	if prompt.Draft != "build-log\ngroup=build" {
		t.Fatalf("expected metadata prompt draft to seed from terminal, got %q", prompt.Draft)
	}
	if len(prompt.Fields) != 2 || prompt.Fields[0].Key != "name" || prompt.Fields[0].Value != "build-log" {
		t.Fatalf("expected structured metadata fields, got %+v", prompt.Fields)
	}
	if prompt.Fields[1].Key != "tags" || prompt.Fields[1].Value != "group=build" {
		t.Fatalf("expected tags field to be seeded, got %+v", prompt.Fields)
	}
}

func TestReducerPromptInputMutatesDraftAndSubmitUsesDraftWhenValueEmpty(t *testing.T) {
	reducer := New()
	state := newAppStateWithSinglePane()

	opened := reducer.Reduce(state, intent.OpenPromptIntent{
		PromptKind: PromptKindCreateWorkspace,
	})
	typed := reducer.Reduce(opened.State, intent.PromptAppendInputIntent{Text: "ops-center"})
	backspaced := reducer.Reduce(typed.State, intent.PromptBackspaceIntent{})
	retyped := reducer.Reduce(backspaced.State, intent.PromptAppendInputIntent{Text: "r"})
	submitted := reducer.Reduce(retyped.State, intent.SubmitPromptIntent{})

	if submitted.State.Domain.ActiveWorkspaceID == types.WorkspaceID("ws-1") {
		t.Fatalf("expected prompt draft to be used for workspace creation")
	}
	workspace := submitted.State.Domain.Workspaces[submitted.State.Domain.ActiveWorkspaceID]
	if workspace.Name != "ops-center" {
		t.Fatalf("expected workspace name from prompt draft, got %+v", workspace)
	}
}

func TestReducerMetadataPromptStructuredInputSwitchesFieldAndSubmits(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenPromptIntent{
		PromptKind: PromptKindEditTerminalMetadata,
		TerminalID: types.TerminalID("term-2"),
	})
	nameEdited := reducer.Reduce(opened.State, intent.PromptAppendInputIntent{Text: "-v2"})
	switched := reducer.Reduce(nameEdited.State, intent.PromptNextFieldIntent{})
	tagsEdited := reducer.Reduce(switched.State, intent.PromptAppendInputIntent{Text: ",env=prod"})
	submitted := reducer.Reduce(tagsEdited.State, intent.SubmitPromptIntent{})
	completed := reducer.Reduce(submitted.State, intent.UpdateTerminalMetadataSucceededIntent{
		TerminalID: types.TerminalID("term-2"),
		Name:       "build-log-v2",
		Tags: map[string]string{
			"group": "build",
			"env":   "prod",
		},
	})

	terminal := completed.State.Domain.Terminals[types.TerminalID("term-2")]
	if terminal.Name != "build-log-v2" {
		t.Fatalf("expected structured name edit, got %+v", terminal)
	}
	if terminal.Tags["group"] != "build" || terminal.Tags["env"] != "prod" {
		t.Fatalf("expected structured tags edit, got %+v", terminal.Tags)
	}
}

func TestReducerMetadataPromptPreviousFieldReturnsFocusToNameField(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenPromptIntent{
		PromptKind: PromptKindEditTerminalMetadata,
		TerminalID: types.TerminalID("term-2"),
	})
	switched := reducer.Reduce(opened.State, intent.PromptNextFieldIntent{})
	returned := reducer.Reduce(switched.State, intent.PromptPreviousFieldIntent{})
	edited := reducer.Reduce(returned.State, intent.PromptAppendInputIntent{Text: "-v3"})
	submitted := reducer.Reduce(edited.State, intent.SubmitPromptIntent{})
	completed := reducer.Reduce(submitted.State, intent.UpdateTerminalMetadataSucceededIntent{
		TerminalID: types.TerminalID("term-2"),
		Name:       "build-log-v3",
		Tags: map[string]string{
			"group": "build",
		},
	})

	terminal := completed.State.Domain.Terminals[types.TerminalID("term-2")]
	if terminal.Name != "build-log-v3" {
		t.Fatalf("expected previous field to return focus to name, got %+v", terminal)
	}
	if terminal.Tags["group"] != "build" {
		t.Fatalf("expected tags to remain unchanged, got %+v", terminal.Tags)
	}
}

func TestReducerMetadataPromptSelectFieldRoutesInputToClickedField(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenPromptIntent{
		PromptKind: PromptKindEditTerminalMetadata,
		TerminalID: types.TerminalID("term-2"),
	})
	selected := reducer.Reduce(opened.State, intent.PromptSelectFieldIntent{Index: 1})
	edited := reducer.Reduce(selected.State, intent.PromptAppendInputIntent{Text: ",env=prod"})
	submitted := reducer.Reduce(edited.State, intent.SubmitPromptIntent{})
	completed := reducer.Reduce(submitted.State, intent.UpdateTerminalMetadataSucceededIntent{
		TerminalID: types.TerminalID("term-2"),
		Name:       "build-log",
		Tags: map[string]string{
			"group": "build",
			"env":   "prod",
		},
	})

	terminal := completed.State.Domain.Terminals[types.TerminalID("term-2")]
	if terminal.Name != "build-log" {
		t.Fatalf("expected name to stay on original field, got %+v", terminal)
	}
	if terminal.Tags["group"] != "build" || terminal.Tags["env"] != "prod" {
		t.Fatalf("expected clicked tags field to receive appended input, got %+v", terminal.Tags)
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

func TestReducerPaneFocusMoveSwitchesToAdjacentPaneAndClearsMode(t *testing.T) {
	reducer := New()
	state := newSplitPaneAppState()
	state.UI.Mode = types.ModeState{Active: types.ModePane}

	result := reducer.Reduce(state, intent.PaneFocusMoveIntent{Direction: types.DirectionRight})

	if result.State.UI.Focus.PaneID != types.PaneID("pane-2") || result.State.UI.Focus.Layer != types.FocusLayerTiled {
		t.Fatalf("expected focus to move to adjacent pane-2, got %+v", result.State.UI.Focus)
	}
	tab := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")]
	if tab.ActivePaneID != types.PaneID("pane-2") || tab.ActiveLayer != types.FocusLayerTiled {
		t.Fatalf("expected active pane to switch to pane-2, got %+v", tab)
	}
	if result.State.UI.Mode.Active != types.ModeNone {
		t.Fatalf("expected pane mode to clear after one move, got %+v", result.State.UI.Mode)
	}
}

func TestReducerSplitActivePaneCreatesWaitingPaneAndOpensLayoutResolve(t *testing.T) {
	reducer := New()
	state := newConnectedAppState()
	state.UI.Mode = types.ModeState{Active: types.ModeGlobal}

	result := reducer.Reduce(state, intent.SplitActivePaneIntent{})

	workspace := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := workspace.Tabs[types.TabID("tab-1")]
	pane := tab.Panes[types.PaneID("pane-2")]
	if pane.ID != types.PaneID("pane-2") || pane.Kind != types.PaneKindTiled || pane.SlotState != types.PaneSlotWaiting || pane.TerminalID != "" {
		t.Fatalf("expected split to create waiting tiled pane-2, got %+v", pane)
	}
	if tab.ActivePaneID != types.PaneID("pane-2") || tab.ActiveLayer != types.FocusLayerTiled {
		t.Fatalf("expected split to focus new pane-2, got %+v", tab)
	}
	if tab.RootSplit == nil || tab.RootSplit.Direction != types.SplitDirectionVertical || tab.RootSplit.First == nil || tab.RootSplit.First.PaneID != types.PaneID("pane-1") || tab.RootSplit.Second == nil || tab.RootSplit.Second.PaneID != types.PaneID("pane-2") {
		t.Fatalf("expected split root to wrap pane-1 with new pane-2, got %+v", tab.RootSplit)
	}
	if result.State.UI.Overlay.Kind != types.OverlayLayoutResolve {
		t.Fatalf("expected split to open layout resolve, got %q", result.State.UI.Overlay.Kind)
	}
	if result.State.UI.Focus.Layer != types.FocusLayerOverlay || result.State.UI.Focus.PaneID != types.PaneID("pane-2") {
		t.Fatalf("expected focus to move into layout resolve for pane-2, got %+v", result.State.UI.Focus)
	}
	if result.State.UI.Mode.Active != types.ModePicker {
		t.Fatalf("expected split handoff to picker mode, got %+v", result.State.UI.Mode)
	}
}

func TestReducerTabFocusMoveSwitchesToAdjacentTabAndClearsMode(t *testing.T) {
	reducer := New()
	state := newTwoTabAppState()
	state.UI.Mode = types.ModeState{Active: types.ModeTab}

	result := reducer.Reduce(state, intent.TabFocusMoveIntent{Delta: 1})

	workspace := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")]
	if workspace.ActiveTabID != types.TabID("tab-2") {
		t.Fatalf("expected active tab to move to tab-2, got %q", workspace.ActiveTabID)
	}
	if result.State.UI.Focus.TabID != types.TabID("tab-2") || result.State.UI.Focus.PaneID != types.PaneID("pane-2") || result.State.UI.Focus.Layer != types.FocusLayerTiled {
		t.Fatalf("expected focus to land on tab-2 active pane, got %+v", result.State.UI.Focus)
	}
	if result.State.UI.Mode.Active != types.ModeNone {
		t.Fatalf("expected tab mode to clear after one move, got %+v", result.State.UI.Mode)
	}
}

func TestReducerCreateTabCreatesWaitingTabAndOpensLayoutResolve(t *testing.T) {
	reducer := New()
	state := newConnectedAppState()
	state.UI.Mode = types.ModeState{Active: types.ModeTab}

	result := reducer.Reduce(state, intent.CreateTabIntent{})

	workspace := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")]
	if workspace.ActiveTabID != types.TabID("tab-2") {
		t.Fatalf("expected active tab to switch to new tab-2, got %q", workspace.ActiveTabID)
	}
	if len(workspace.TabOrder) != 2 || workspace.TabOrder[1] != types.TabID("tab-2") {
		t.Fatalf("expected tab order to append tab-2, got %+v", workspace.TabOrder)
	}
	tab := workspace.Tabs[types.TabID("tab-2")]
	if tab.ID != types.TabID("tab-2") || tab.Name != "tab-2" {
		t.Fatalf("expected new tab identity to be populated, got %+v", tab)
	}
	pane := tab.Panes[types.PaneID("ws-1-tab-2-pane-1")]
	if pane.ID != types.PaneID("ws-1-tab-2-pane-1") || pane.Kind != types.PaneKindTiled || pane.SlotState != types.PaneSlotWaiting || pane.TerminalID != "" {
		t.Fatalf("expected new tab to start with waiting pane, got %+v", pane)
	}
	if result.State.UI.Overlay.Kind != types.OverlayLayoutResolve {
		t.Fatalf("expected create tab to open layout resolve, got %q", result.State.UI.Overlay.Kind)
	}
	if result.State.UI.Focus.Layer != types.FocusLayerOverlay || result.State.UI.Focus.TabID != types.TabID("tab-2") || result.State.UI.Focus.PaneID != types.PaneID("ws-1-tab-2-pane-1") {
		t.Fatalf("expected focus to move into new tab layout resolve, got %+v", result.State.UI.Focus)
	}
	if result.State.UI.Mode.Active != types.ModePicker {
		t.Fatalf("expected create tab to hand off into picker mode, got %+v", result.State.UI.Mode)
	}
}

func TestReducerCreateTerminalInActivePaneEmitsCreateEffectForEmptyPane(t *testing.T) {
	reducer := New()
	state := newAppStateWithSinglePane()

	result := reducer.Reduce(state, intent.CreateTerminalInActivePaneIntent{})

	if len(result.Effects) != 1 {
		t.Fatalf("expected one create effect, got %d", len(result.Effects))
	}
	effect, ok := result.Effects[0].(CreateTerminalEffect)
	if !ok {
		t.Fatalf("expected create terminal effect, got %T", result.Effects[0])
	}
	if effect.PaneID != types.PaneID("pane-1") || effect.Name != "ws-1-tab-1-pane-1" || len(effect.Command) == 0 {
		t.Fatalf("unexpected active-pane create effect: %+v", effect)
	}
}

func TestReducerCreateTerminalInActivePaneNoopsForConnectedPane(t *testing.T) {
	reducer := New()
	state := newConnectedAppState()

	result := reducer.Reduce(state, intent.CreateTerminalInActivePaneIntent{})

	if len(result.Effects) != 0 {
		t.Fatalf("expected connected pane create action to noop, got %+v", result.Effects)
	}
}

func TestReducerFloatingFocusMoveSwitchesToAdjacentFloatingPaneAndClearsMode(t *testing.T) {
	reducer := New()
	state := newFloatingPaneStackAppState()
	state.UI.Mode = types.ModeState{Active: types.ModeFloating}

	result := reducer.Reduce(state, intent.FloatingFocusMoveIntent{Delta: 1})

	tab := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")]
	if tab.ActivePaneID != types.PaneID("float-2") || tab.ActiveLayer != types.FocusLayerFloating {
		t.Fatalf("expected floating move to switch to float-2, got %+v", tab)
	}
	if result.State.UI.Focus.Layer != types.FocusLayerFloating || result.State.UI.Focus.PaneID != types.PaneID("float-2") {
		t.Fatalf("expected focus to land on float-2, got %+v", result.State.UI.Focus)
	}
	if result.State.UI.Mode.Active != types.ModeNone {
		t.Fatalf("expected floating mode to clear after one move, got %+v", result.State.UI.Mode)
	}
}

func TestReducerCreateFloatingPaneCreatesWaitingFloatingPaneAndOpensLayoutResolve(t *testing.T) {
	reducer := New()
	state := newConnectedAppState()
	state.UI.Mode = types.ModeState{Active: types.ModeFloating}

	result := reducer.Reduce(state, intent.CreateFloatingPaneIntent{})

	tab := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")]
	if tab.ActivePaneID != types.PaneID("float-1") || tab.ActiveLayer != types.FocusLayerFloating {
		t.Fatalf("expected new floating pane to become active, got %+v", tab)
	}
	if len(tab.FloatingOrder) != 1 || tab.FloatingOrder[0] != types.PaneID("float-1") {
		t.Fatalf("expected floating order to append float-1, got %+v", tab.FloatingOrder)
	}
	pane := tab.Panes[types.PaneID("float-1")]
	if pane.ID != types.PaneID("float-1") || pane.Kind != types.PaneKindFloating || pane.SlotState != types.PaneSlotWaiting || pane.TerminalID != "" {
		t.Fatalf("expected create floating to produce waiting floating pane, got %+v", pane)
	}
	if result.State.UI.Overlay.Kind != types.OverlayLayoutResolve {
		t.Fatalf("expected create floating to open layout resolve, got %q", result.State.UI.Overlay.Kind)
	}
	if result.State.UI.Focus.Layer != types.FocusLayerOverlay || result.State.UI.Focus.PaneID != types.PaneID("float-1") {
		t.Fatalf("expected focus to move into floating layout resolve, got %+v", result.State.UI.Focus)
	}
	if result.State.UI.Mode.Active != types.ModePicker {
		t.Fatalf("expected create floating to hand off into picker mode, got %+v", result.State.UI.Mode)
	}
}

func TestReducerConnectTerminalReplacesOldConnectionSnapshot(t *testing.T) {
	reducer := New()
	state := newConnectedAppState()
	state.Domain.Terminals[types.TerminalID("term-2")] = types.TerminalRef{
		ID:    types.TerminalID("term-2"),
		Name:  "log-stream",
		State: types.TerminalRunStateRunning,
	}

	result := reducer.Reduce(state, intent.ConnectTerminalIntent{
		PaneID:     types.PaneID("pane-1"),
		TerminalID: types.TerminalID("term-2"),
		Source:     intent.ConnectSourceManagerHere,
	})

	pane := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected pane to connect to new terminal, got %+v", pane)
	}
	if _, ok := result.State.Domain.Connections[types.TerminalID("term-1")]; ok {
		t.Fatalf("expected old terminal connection snapshot removed")
	}
	conn := result.State.Domain.Connections[types.TerminalID("term-2")]
	if len(conn.ConnectedPaneIDs) != 1 || conn.ConnectedPaneIDs[0] != types.PaneID("pane-1") {
		t.Fatalf("expected new terminal connection snapshot to contain pane-1, got %+v", conn)
	}
}

func TestReducerOpenTerminalManagerMovesFocusToOverlay(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	result := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayTerminalManager {
		t.Fatalf("expected terminal manager overlay, got %q", result.State.UI.Overlay.Kind)
	}
	if result.State.UI.Focus.Layer != types.FocusLayerOverlay {
		t.Fatalf("expected overlay focus, got %+v", result.State.UI.Focus)
	}
	manager, ok := result.State.UI.Overlay.Data.(*terminalmanagerdomain.State)
	if !ok {
		t.Fatalf("expected terminal manager overlay data, got %T", result.State.UI.Overlay.Data)
	}
	selected, ok := manager.SelectedTerminalID()
	if !ok || selected != types.TerminalID("term-1") {
		t.Fatalf("expected focused pane terminal to be selected, got %q ok=%v", selected, ok)
	}
}

func TestReducerTerminalManagerConnectHereUsesSelectedTerminalAndClosesOverlay(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	moved := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: 1})
	result := reducer.Reduce(moved.State, intent.TerminalManagerConnectHereIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected manager overlay to close, got %q", result.State.UI.Overlay.Kind)
	}
	pane := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected current pane to connect selected terminal, got %+v", pane)
	}
	if !result.State.Domain.Terminals[types.TerminalID("term-2")].Visible {
		t.Fatalf("expected connected terminal to become visible, got %+v", result.State.Domain.Terminals[types.TerminalID("term-2")])
	}
	if len(result.Effects) != 1 {
		t.Fatalf("expected one connect effect, got %d", len(result.Effects))
	}
	effect, ok := result.Effects[0].(ConnectTerminalEffect)
	if !ok {
		t.Fatalf("expected connect terminal effect, got %T", result.Effects[0])
	}
	if effect.PaneID != types.PaneID("pane-1") || effect.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("unexpected effect payload: %+v", effect)
	}
}

func TestReducerConnectTerminalSwitchingLastPaneHidesOldTerminalAndShowsNewTerminal(t *testing.T) {
	reducer := New()
	state := newConnectedAppState()
	terminal := state.Domain.Terminals[types.TerminalID("term-1")]
	terminal.Visible = true
	state.Domain.Terminals[types.TerminalID("term-1")] = terminal
	state.Domain.Terminals[types.TerminalID("term-2")] = types.TerminalRef{
		ID:    types.TerminalID("term-2"),
		Name:  "build-log",
		State: types.TerminalRunStateRunning,
	}

	result := reducer.Reduce(state, intent.ConnectTerminalIntent{
		PaneID:     types.PaneID("pane-1"),
		TerminalID: types.TerminalID("term-2"),
	})

	if result.State.Domain.Terminals[types.TerminalID("term-1")].Visible {
		t.Fatalf("expected old terminal to become hidden after losing last pane, got %+v", result.State.Domain.Terminals[types.TerminalID("term-1")])
	}
	if !result.State.Domain.Terminals[types.TerminalID("term-2")].Visible {
		t.Fatalf("expected new terminal to become visible after connect, got %+v", result.State.Domain.Terminals[types.TerminalID("term-2")])
	}
	if _, ok := result.State.Domain.Connections[types.TerminalID("term-1")]; ok {
		t.Fatalf("expected old terminal connection snapshot to be removed after losing last pane")
	}
}

func TestReducerTerminalManagerConnectInNewTabEmitsPlanningEffect(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	moved := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: 1})
	result := reducer.Reduce(moved.State, intent.TerminalManagerConnectInNewTabIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayTerminalManager {
		t.Fatalf("expected manager overlay to stay open until new-tab success, got %q", result.State.UI.Overlay.Kind)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("expected one new-tab effect, got %d", len(result.Effects))
	}
	effect, ok := result.Effects[0].(ConnectTerminalInNewTabEffect)
	if !ok {
		t.Fatalf("expected new-tab effect, got %T", result.Effects[0])
	}
	if effect.WorkspaceID != types.WorkspaceID("ws-1") || effect.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("unexpected effect payload: %+v", effect)
	}
}

func TestReducerTerminalManagerConnectInNewTabSucceededCreatesTabAndClosesOverlay(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	moved := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: 1})
	result := reducer.Reduce(moved.State, intent.ConnectTerminalInNewTabSucceededIntent{
		WorkspaceID: types.WorkspaceID("ws-1"),
		TerminalID:  types.TerminalID("term-2"),
	})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected manager overlay to close after new-tab success, got %q", result.State.UI.Overlay.Kind)
	}
	workspace := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")]
	if workspace.ActiveTabID != types.TabID("tab-2") {
		t.Fatalf("expected active tab to switch to new tab, got %q", workspace.ActiveTabID)
	}
	if len(workspace.TabOrder) != 2 || workspace.TabOrder[1] != types.TabID("tab-2") {
		t.Fatalf("expected tab order to append new tab, got %+v", workspace.TabOrder)
	}
	tab := workspace.Tabs[types.TabID("tab-2")]
	if tab.ID != types.TabID("tab-2") || tab.Name != "tab-2" {
		t.Fatalf("expected new tab identity to be populated, got %+v", tab)
	}
	if tab.ActiveLayer != types.FocusLayerTiled {
		t.Fatalf("expected new tab to start on tiled layer, got %q", tab.ActiveLayer)
	}
	if tab.ActivePaneID != types.PaneID("ws-1-tab-2-pane-1") {
		t.Fatalf("expected new connected pane to become active, got %q", tab.ActivePaneID)
	}
	pane := tab.Panes[types.PaneID("ws-1-tab-2-pane-1")]
	if pane.ID != types.PaneID("ws-1-tab-2-pane-1") || pane.Kind != types.PaneKindTiled || pane.SlotState != types.PaneSlotConnected || pane.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected connected pane in new tab, got %+v", pane)
	}
	if tab.RootSplit == nil || tab.RootSplit.PaneID != types.PaneID("ws-1-tab-2-pane-1") {
		t.Fatalf("expected new tab root split to point at created pane, got %+v", tab.RootSplit)
	}
	if result.State.UI.Focus.Layer != types.FocusLayerTiled || result.State.UI.Focus.TabID != types.TabID("tab-2") || result.State.UI.Focus.PaneID != types.PaneID("ws-1-tab-2-pane-1") {
		t.Fatalf("expected focus to move into new tab pane, got %+v", result.State.UI.Focus)
	}
	conn := result.State.Domain.Connections[types.TerminalID("term-2")]
	if len(conn.ConnectedPaneIDs) != 1 || conn.ConnectedPaneIDs[0] != types.PaneID("ws-1-tab-2-pane-1") {
		t.Fatalf("expected new-tab success to register new connection pane, got %+v", conn)
	}
	if conn.OwnerPaneID != types.PaneID("ws-1-tab-2-pane-1") {
		t.Fatalf("expected first pane in new tab to own terminal, got %+v", conn)
	}
	if !result.State.Domain.Terminals[types.TerminalID("term-2")].Visible {
		t.Fatalf("expected new-tab success to mark terminal visible, got %+v", result.State.Domain.Terminals[types.TerminalID("term-2")])
	}
}

func TestReducerTerminalManagerConnectInFloatingPaneEmitsPlanningEffect(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	moved := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: 1})
	result := reducer.Reduce(moved.State, intent.TerminalManagerConnectInFloatingPaneIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayTerminalManager {
		t.Fatalf("expected manager overlay to stay open until floating success, got %q", result.State.UI.Overlay.Kind)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("expected one floating effect, got %d", len(result.Effects))
	}
	effect, ok := result.Effects[0].(ConnectTerminalInFloatingPaneEffect)
	if !ok {
		t.Fatalf("expected floating effect, got %T", result.Effects[0])
	}
	if effect.WorkspaceID != types.WorkspaceID("ws-1") || effect.TabID != types.TabID("tab-1") || effect.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("unexpected effect payload: %+v", effect)
	}
}

func TestReducerTerminalManagerConnectInFloatingPaneSucceededCreatesFloatingPaneAndClosesOverlay(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	moved := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: 1})
	result := reducer.Reduce(moved.State, intent.ConnectTerminalInFloatingPaneSucceededIntent{
		WorkspaceID: types.WorkspaceID("ws-1"),
		TabID:       types.TabID("tab-1"),
		TerminalID:  types.TerminalID("term-2"),
	})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected manager overlay to close after floating success, got %q", result.State.UI.Overlay.Kind)
	}
	if result.State.Domain.ActiveWorkspaceID != types.WorkspaceID("ws-1") {
		t.Fatalf("expected floating success to keep workspace on ws-1, got %q", result.State.Domain.ActiveWorkspaceID)
	}
	tab := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")]
	if tab.ActiveLayer != types.FocusLayerFloating {
		t.Fatalf("expected tab to switch to floating layer, got %q", tab.ActiveLayer)
	}
	if tab.ActivePaneID != types.PaneID("float-1") {
		t.Fatalf("expected new floating pane to become active, got %q", tab.ActivePaneID)
	}
	if len(tab.FloatingOrder) != 1 || tab.FloatingOrder[0] != types.PaneID("float-1") {
		t.Fatalf("expected floating order to contain new pane, got %+v", tab.FloatingOrder)
	}
	pane := tab.Panes[types.PaneID("float-1")]
	if pane.ID != types.PaneID("float-1") || pane.Kind != types.PaneKindFloating || pane.SlotState != types.PaneSlotConnected || pane.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected connected floating pane for term-2, got %+v", pane)
	}
	if result.State.UI.Focus.Layer != types.FocusLayerFloating || result.State.UI.Focus.PaneID != types.PaneID("float-1") {
		t.Fatalf("expected focus to move to new floating pane, got %+v", result.State.UI.Focus)
	}
	conn := result.State.Domain.Connections[types.TerminalID("term-2")]
	if len(conn.ConnectedPaneIDs) != 1 || conn.ConnectedPaneIDs[0] != types.PaneID("float-1") {
		t.Fatalf("expected floating success to register new connection pane, got %+v", conn)
	}
	if conn.OwnerPaneID != types.PaneID("float-1") {
		t.Fatalf("expected first floating connection to own terminal, got %+v", conn)
	}
	if !result.State.Domain.Terminals[types.TerminalID("term-2")].Visible {
		t.Fatalf("expected floating success to mark terminal visible, got %+v", result.State.Domain.Terminals[types.TerminalID("term-2")])
	}
}

func TestReducerTerminalManagerJumpToConnectedPaneClosesOverlayAndMovesFocusToOwnerPane(t *testing.T) {
	reducer := New()
	state := newManagerJumpAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	moved := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: 1})
	result := reducer.Reduce(moved.State, intent.TerminalManagerJumpToConnectedPaneIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected manager overlay to close after jump, got %q", result.State.UI.Overlay.Kind)
	}
	if result.State.Domain.ActiveWorkspaceID != types.WorkspaceID("ws-2") {
		t.Fatalf("expected jump to switch workspace, got %q", result.State.Domain.ActiveWorkspaceID)
	}
	workspace := result.State.Domain.Workspaces[types.WorkspaceID("ws-2")]
	if workspace.ActiveTabID != types.TabID("tab-2") {
		t.Fatalf("expected jump to switch active tab, got %q", workspace.ActiveTabID)
	}
	tab := workspace.Tabs[types.TabID("tab-2")]
	if tab.ActivePaneID != types.PaneID("pane-remote") || tab.ActiveLayer != types.FocusLayerTiled {
		t.Fatalf("expected jump to focus owner pane-remote, got %+v", tab)
	}
	if result.State.UI.Focus.WorkspaceID != types.WorkspaceID("ws-2") || result.State.UI.Focus.TabID != types.TabID("tab-2") || result.State.UI.Focus.PaneID != types.PaneID("pane-remote") || result.State.UI.Focus.Layer != types.FocusLayerTiled {
		t.Fatalf("expected focus to move to connected pane target, got %+v", result.State.UI.Focus)
	}
}

func TestReducerTerminalManagerJumpToConnectedPaneWithoutLocationKeepsOverlayOpenAndEmitsNotice(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	moved := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: 1})
	result := reducer.Reduce(moved.State, intent.TerminalManagerJumpToConnectedPaneIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayTerminalManager {
		t.Fatalf("expected manager overlay to stay open without connected pane, got %q", result.State.UI.Overlay.Kind)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("expected one notice effect, got %d", len(result.Effects))
	}
	effect, ok := result.Effects[0].(NoticeEffect)
	if !ok {
		t.Fatalf("expected notice effect, got %T", result.Effects[0])
	}
	if effect.Level != NoticeLevelError || effect.Text != "selected terminal has no connected pane" {
		t.Fatalf("unexpected jump failure notice: %+v", effect)
	}
}

func TestReducerTerminalManagerJumpToLocationClosesOverlayAndMovesFocusToExactFloatingPane(t *testing.T) {
	reducer := New()
	state := newManagerLocationJumpAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	result := reducer.Reduce(opened.State, intent.TerminalManagerJumpToLocationIntent{
		WorkspaceID: types.WorkspaceID("ws-2"),
		TabID:       types.TabID("tab-2"),
		PaneID:      types.PaneID("float-ops"),
	})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected manager overlay to close after exact location jump, got %q", result.State.UI.Overlay.Kind)
	}
	if result.State.Domain.ActiveWorkspaceID != types.WorkspaceID("ws-2") {
		t.Fatalf("expected exact jump to switch workspace, got %q", result.State.Domain.ActiveWorkspaceID)
	}
	workspace := result.State.Domain.Workspaces[types.WorkspaceID("ws-2")]
	tab := workspace.Tabs[types.TabID("tab-2")]
	if tab.ActivePaneID != types.PaneID("float-ops") || tab.ActiveLayer != types.FocusLayerFloating {
		t.Fatalf("expected exact jump to focus float-ops, got %+v", tab)
	}
	if result.State.UI.Focus.WorkspaceID != types.WorkspaceID("ws-2") || result.State.UI.Focus.TabID != types.TabID("tab-2") || result.State.UI.Focus.PaneID != types.PaneID("float-ops") || result.State.UI.Focus.Layer != types.FocusLayerFloating {
		t.Fatalf("expected exact jump focus to land on floating pane, got %+v", result.State.UI.Focus)
	}
}

func TestReducerTerminalManagerAcquireOwnerTransfersOwnershipToReturnFocusPane(t *testing.T) {
	reducer := New()
	state := newFollowerManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	result := reducer.Reduce(opened.State, intent.TerminalManagerAcquireOwnerIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayTerminalManager {
		t.Fatalf("expected manager overlay to stay open, got %q", result.State.UI.Overlay.Kind)
	}
	conn := result.State.Domain.Connections[types.TerminalID("term-1")]
	if conn.OwnerPaneID != types.PaneID("pane-2") {
		t.Fatalf("expected owner to transfer to pane-2, got %+v", conn)
	}
	manager, ok := result.State.UI.Overlay.Data.(*terminalmanagerdomain.State)
	if !ok {
		t.Fatalf("expected terminal manager overlay data, got %T", result.State.UI.Overlay.Data)
	}
	row, ok := manager.SelectedRow()
	if !ok || row.TerminalID != types.TerminalID("term-1") || row.OwnerSlotLabel != "pane:pane-2" {
		t.Fatalf("expected selected row owner projection to refresh, got %+v ok=%v", row, ok)
	}
	detail, ok := manager.SelectedDetail()
	if !ok || detail.OwnerSlotLabel != "pane:pane-2" {
		t.Fatalf("expected detail owner projection to refresh, got %+v ok=%v", detail, ok)
	}
}

func TestReducerTerminalManagerAcquireOwnerIgnoresUnconnectedRequestorPane(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	moved := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: 1})
	result := reducer.Reduce(moved.State, intent.TerminalManagerAcquireOwnerIntent{})

	conn := result.State.Domain.Connections[types.TerminalID("term-2")]
	if conn.OwnerPaneID != "" {
		t.Fatalf("expected parked terminal without requestor connection to keep empty owner, got %+v", conn)
	}
	manager, ok := result.State.UI.Overlay.Data.(*terminalmanagerdomain.State)
	if !ok {
		t.Fatalf("expected terminal manager overlay data, got %T", result.State.UI.Overlay.Data)
	}
	detail, ok := manager.SelectedDetail()
	if !ok || detail.OwnerSlotLabel != "" {
		t.Fatalf("expected detail owner to stay empty, got %+v ok=%v", detail, ok)
	}
}

func TestReducerTerminalManagerCreateNewTerminalEmitsCreateEffect(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	movedToCreate := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: -100})
	result := reducer.Reduce(movedToCreate.State, intent.TerminalManagerCreateTerminalIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayTerminalManager {
		t.Fatalf("expected manager overlay to stay open until create success, got %q", result.State.UI.Overlay.Kind)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("expected one create effect, got %d", len(result.Effects))
	}
	effect, ok := result.Effects[0].(CreateTerminalEffect)
	if !ok {
		t.Fatalf("expected create terminal effect, got %T", result.Effects[0])
	}
	if effect.PaneID != types.PaneID("pane-1") {
		t.Fatalf("unexpected create effect payload: %+v", effect)
	}
	if len(effect.Command) == 0 {
		t.Fatalf("expected default shell command in create effect, got %+v", effect)
	}
	if effect.Name != "ws-1-tab-1-pane-1" {
		t.Fatalf("expected stable default terminal name, got %+v", effect)
	}
}

func TestReducerTerminalManagerCreateTerminalSucceededClosesOverlayRegistersTerminalAndConnectsPane(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	movedToCreate := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: -100})
	result := reducer.Reduce(movedToCreate.State, intent.CreateTerminalSucceededIntent{
		PaneID:     types.PaneID("pane-1"),
		TerminalID: types.TerminalID("term-created"),
		Name:       "ws-1-tab-1-pane-1",
		Command:    []string{"sh", "-l"},
		State:      types.TerminalRunStateRunning,
	})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected manager overlay to close after create success, got %q", result.State.UI.Overlay.Kind)
	}
	pane := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.TerminalID != types.TerminalID("term-created") || pane.SlotState != types.PaneSlotConnected {
		t.Fatalf("expected manager create success to connect pane-1, got %+v", pane)
	}
	terminal := result.State.Domain.Terminals[types.TerminalID("term-created")]
	if terminal.ID != types.TerminalID("term-created") || terminal.Name != "ws-1-tab-1-pane-1" || len(terminal.Command) != 2 || !terminal.Visible {
		t.Fatalf("unexpected registered terminal after manager create success: %+v", terminal)
	}
	oldConn := result.State.Domain.Connections[types.TerminalID("term-1")]
	if len(oldConn.ConnectedPaneIDs) != 1 || oldConn.ConnectedPaneIDs[0] != types.PaneID("float-2") || oldConn.OwnerPaneID != types.PaneID("float-2") {
		t.Fatalf("expected old shared terminal to keep only float-2 after manager create connect, got %+v", oldConn)
	}
}

func TestReducerTerminalManagerSearchMovesSelectionToMatchedTerminal(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	typed := reducer.Reduce(opened.State, intent.TerminalManagerAppendQueryIntent{Text: "ops"})

	manager, ok := typed.State.UI.Overlay.Data.(*terminalmanagerdomain.State)
	if !ok {
		t.Fatalf("expected terminal manager overlay data, got %T", typed.State.UI.Overlay.Data)
	}
	if manager.Query() != "ops" {
		t.Fatalf("expected query to update, got %q", manager.Query())
	}
	selected, ok := manager.SelectedTerminalID()
	if !ok || selected != types.TerminalID("term-3") {
		t.Fatalf("expected search to select matched terminal, got %q ok=%v", selected, ok)
	}
	detail, ok := manager.SelectedDetail()
	if !ok || detail.TerminalID != types.TerminalID("term-3") {
		t.Fatalf("expected selected detail to follow searched terminal, got %+v ok=%v", detail, ok)
	}
}

func TestReducerTerminalManagerDetailsExposePaneLocations(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	manager, ok := opened.State.UI.Overlay.Data.(*terminalmanagerdomain.State)
	if !ok {
		t.Fatalf("expected terminal manager overlay data, got %T", opened.State.UI.Overlay.Data)
	}
	detail, ok := manager.SelectedDetail()
	if !ok {
		t.Fatalf("expected selected detail")
	}
	if len(detail.Locations) != 2 {
		t.Fatalf("expected two projected locations, got %+v", detail.Locations)
	}
	if detail.Locations[0].SlotLabel != "pane:pane-1" {
		t.Fatalf("expected tiled location first, got %+v", detail.Locations[0])
	}
	if detail.Locations[1].SlotLabel != "float:float-2" {
		t.Fatalf("expected floating location second, got %+v", detail.Locations[1])
	}
	if detail.VisibilityLabel != "visible" || detail.OwnerSlotLabel != "pane:pane-1" {
		t.Fatalf("expected detail visibility/owner projection, got %+v", detail)
	}
	if len(detail.Tags) != 0 {
		t.Fatalf("expected connected api-dev terminal to have empty tags in fixture, got %+v", detail.Tags)
	}
}

func TestReducerTerminalManagerEditMetadataClosesOverlayAndEmitsPrompt(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	moved := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: 1})
	result := reducer.Reduce(moved.State, intent.TerminalManagerEditMetadataIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected manager overlay to close, got %q", result.State.UI.Overlay.Kind)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("expected one prompt effect, got %d", len(result.Effects))
	}
	effect, ok := result.Effects[0].(OpenPromptEffect)
	if !ok {
		t.Fatalf("expected open prompt effect, got %T", result.Effects[0])
	}
	if effect.PromptKind != PromptKindEditTerminalMetadata || effect.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("unexpected edit prompt effect: %+v", effect)
	}
}

func TestReducerTerminalManagerEditMetadataWithoutOwnerKeepsManagerOpenAndEmitsNotice(t *testing.T) {
	reducer := New()
	state := newFollowerManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	result := reducer.Reduce(opened.State, intent.TerminalManagerEditMetadataIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayTerminalManager {
		t.Fatalf("expected manager overlay to stay open without owner, got %q", result.State.UI.Overlay.Kind)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("expected one notice effect, got %d", len(result.Effects))
	}
	effect, ok := result.Effects[0].(NoticeEffect)
	if !ok {
		t.Fatalf("expected notice effect, got %T", result.Effects[0])
	}
	if effect.Level != NoticeLevelError || effect.Text != "terminal metadata update requires owner; acquire owner first" {
		t.Fatalf("unexpected notice effect payload: %+v", effect)
	}
}

func TestReducerTerminalManagerStopSelectedTerminalKeepsManagerOpenUntilSuccessAndEmitsEffect(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	moved := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: 1})
	result := reducer.Reduce(moved.State, intent.TerminalManagerStopIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayTerminalManager {
		t.Fatalf("expected manager overlay to stay open until stop success, got %q", result.State.UI.Overlay.Kind)
	}
	terminal := result.State.Domain.Terminals[types.TerminalID("term-2")]
	if terminal.State != types.TerminalRunStateRunning {
		t.Fatalf("expected selected terminal to stay running before success feedback, got %+v", terminal)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("expected one stop effect, got %d", len(result.Effects))
	}
	effect, ok := result.Effects[0].(StopTerminalEffect)
	if !ok {
		t.Fatalf("expected stop effect, got %T", result.Effects[0])
	}
	if effect.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("unexpected stop effect payload: %+v", effect)
	}
}

func TestReducerStopTerminalSucceededUpdatesStateAndClosesManager(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	moved := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: 1})
	result := reducer.Reduce(moved.State, intent.StopTerminalSucceededIntent{
		TerminalID: types.TerminalID("term-2"),
	})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected manager overlay to close after stop success, got %q", result.State.UI.Overlay.Kind)
	}
	terminal := result.State.Domain.Terminals[types.TerminalID("term-2")]
	if terminal.State != types.TerminalRunStateStopped {
		t.Fatalf("expected selected terminal to become stopped after success, got %+v", terminal)
	}
}

func TestReducerTerminalManagerStopWithoutOwnerKeepsManagerOpenAndEmitsNotice(t *testing.T) {
	reducer := New()
	state := newFollowerManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	result := reducer.Reduce(opened.State, intent.TerminalManagerStopIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayTerminalManager {
		t.Fatalf("expected manager overlay to stay open without owner, got %q", result.State.UI.Overlay.Kind)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("expected one notice effect, got %d", len(result.Effects))
	}
	effect, ok := result.Effects[0].(NoticeEffect)
	if !ok {
		t.Fatalf("expected notice effect, got %T", result.Effects[0])
	}
	if effect.Level != NoticeLevelError || effect.Text != "stop terminal requires owner; acquire owner first" {
		t.Fatalf("unexpected stop notice effect payload: %+v", effect)
	}
	conn := result.State.Domain.Connections[types.TerminalID("term-1")]
	if conn.OwnerPaneID != types.PaneID("pane-1") {
		t.Fatalf("expected owner to stay unchanged without permission, got %+v", conn)
	}
}

func TestE2EReducerScenarioTerminalManagerConnectsSelectedTerminalHere(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	moved := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: 1})
	result := reducer.Reduce(moved.State, intent.TerminalManagerConnectHereIntent{})

	if result.State.UI.Focus.Layer != types.FocusLayerTiled || result.State.UI.Focus.PaneID != types.PaneID("pane-1") {
		t.Fatalf("expected focus to return to current pane, got %+v", result.State.UI.Focus)
	}
	pane := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.TerminalID != types.TerminalID("term-2") || pane.SlotState != types.PaneSlotConnected {
		t.Fatalf("expected pane to show selected terminal after manager flow, got %+v", pane)
	}
}

func TestE2EReducerScenarioTerminalPickerSearchesAndConnectsSelectedTerminal(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalPickerIntent{})
	typed := reducer.Reduce(opened.State, intent.TerminalPickerAppendQueryIntent{Text: "ops"})
	result := reducer.Reduce(typed.State, intent.TerminalPickerSubmitIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected overlay to close after picker submit, got %q", result.State.UI.Overlay.Kind)
	}
	pane := result.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.TerminalID != types.TerminalID("term-3") || pane.SlotState != types.PaneSlotConnected {
		t.Fatalf("expected picker flow to connect searched terminal, got %+v", pane)
	}
}

func TestE2EReducerScenarioTerminalManagerSearchesAndStopsSelectedTerminal(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	typed := reducer.Reduce(opened.State, intent.TerminalManagerAppendQueryIntent{Text: "ops"})
	triggered := reducer.Reduce(typed.State, intent.TerminalManagerStopIntent{})
	result := reducer.Reduce(triggered.State, intent.StopTerminalSucceededIntent{
		TerminalID: types.TerminalID("term-3"),
	})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected overlay to close after stop, got %q", result.State.UI.Overlay.Kind)
	}
	if result.State.Domain.Terminals[types.TerminalID("term-3")].State != types.TerminalRunStateStopped {
		t.Fatalf("expected searched terminal to stop, got %+v", result.State.Domain.Terminals[types.TerminalID("term-3")])
	}
}

func TestE2EReducerScenarioTerminalManagerCreatesNewTerminalFromCreateRow(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	createRow := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: -100})
	triggered := reducer.Reduce(createRow.State, intent.TerminalManagerCreateTerminalIntent{})
	result := reducer.Reduce(triggered.State, intent.CreateTerminalSucceededIntent{
		PaneID:     types.PaneID("pane-1"),
		TerminalID: types.TerminalID("term-created"),
		Name:       "ws-1-tab-1-pane-1",
		Command:    []string{"sh", "-l"},
		State:      types.TerminalRunStateRunning,
	})

	if result.State.UI.Focus.Layer != types.FocusLayerTiled || result.State.UI.Focus.PaneID != types.PaneID("pane-1") {
		t.Fatalf("expected focus to return to current pane, got %+v", result.State.UI.Focus)
	}
	if len(triggered.Effects) != 1 {
		t.Fatalf("expected one create terminal effect, got %d", len(triggered.Effects))
	}
	effect, ok := triggered.Effects[0].(CreateTerminalEffect)
	if !ok {
		t.Fatalf("expected create terminal effect, got %T", triggered.Effects[0])
	}
	if effect.Name != "ws-1-tab-1-pane-1" || len(effect.Command) == 0 {
		t.Fatalf("expected create effect defaults to be populated, got %+v", effect)
	}
	if result.State.Domain.Terminals[types.TerminalID("term-created")].Name != "ws-1-tab-1-pane-1" {
		t.Fatalf("expected create success to register terminal, got %+v", result.State.Domain.Terminals[types.TerminalID("term-created")])
	}
}

func TestE2EReducerScenarioTerminalManagerEnterOnCreateRowCreatesNewTerminal(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	opened := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	createRow := reducer.Reduce(opened.State, intent.TerminalManagerMoveIntent{Delta: -100})
	triggered := reducer.Reduce(createRow.State, intent.TerminalManagerConnectHereIntent{})
	result := reducer.Reduce(triggered.State, intent.CreateTerminalSucceededIntent{
		PaneID:     types.PaneID("pane-1"),
		TerminalID: types.TerminalID("term-created"),
		Name:       "ws-1-tab-1-pane-1",
		Command:    []string{"sh", "-l"},
		State:      types.TerminalRunStateRunning,
	})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected overlay to close after create row submit, got %q", result.State.UI.Overlay.Kind)
	}
	if result.State.UI.Focus.Layer != types.FocusLayerTiled || result.State.UI.Focus.PaneID != types.PaneID("pane-1") {
		t.Fatalf("expected focus to return to current pane, got %+v", result.State.UI.Focus)
	}
	if len(triggered.Effects) != 1 {
		t.Fatalf("expected one create terminal effect, got %d", len(triggered.Effects))
	}
	effect, ok := triggered.Effects[0].(CreateTerminalEffect)
	if !ok {
		t.Fatalf("expected create terminal effect, got %T", triggered.Effects[0])
	}
	if effect.Name != "ws-1-tab-1-pane-1" || len(effect.Command) == 0 {
		t.Fatalf("expected create effect defaults to be populated, got %+v", effect)
	}
	if result.State.Domain.Terminals[types.TerminalID("term-created")].Name != "ws-1-tab-1-pane-1" {
		t.Fatalf("expected create success to register terminal, got %+v", result.State.Domain.Terminals[types.TerminalID("term-created")])
	}
}

func TestE2EReducerScenarioWorkspacePickerSearchesAndJumpsToPane(t *testing.T) {
	reducer := New()
	state := newAppStateWithTwoWorkspaces()

	opened := reducer.Reduce(state, intent.OpenWorkspacePickerIntent{})
	typed := reducer.Reduce(opened.State, intent.WorkspacePickerAppendQueryIntent{Text: "float-dev"})
	result := reducer.Reduce(typed.State, intent.WorkspacePickerSubmitIntent{})

	if result.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected overlay to close after jump, got %q", result.State.UI.Overlay.Kind)
	}
	if result.State.Domain.ActiveWorkspaceID != types.WorkspaceID("ws-2") {
		t.Fatalf("expected jump to workspace ws-2, got %q", result.State.Domain.ActiveWorkspaceID)
	}
	if result.State.UI.Focus.PaneID != types.PaneID("pane-float") || result.State.UI.Focus.Layer != types.FocusLayerFloating {
		t.Fatalf("expected focus to land on matched pane, got %+v", result.State.UI.Focus)
	}
}

func TestE2EReducerScenarioWorkspacePickerCreateWorkspaceFlow(t *testing.T) {
	reducer := New()
	state := newAppStateWithSinglePane()

	openedPicker := reducer.Reduce(state, intent.OpenWorkspacePickerIntent{})
	createRow := reducer.Reduce(openedPicker.State, intent.WorkspacePickerMoveIntent{Delta: -100})
	handoff := reducer.Reduce(createRow.State, intent.WorkspacePickerSubmitIntent{})
	if len(handoff.Effects) != 1 {
		t.Fatalf("expected create row to emit prompt handoff effect, got %d", len(handoff.Effects))
	}
	promptOpened := reducer.Reduce(handoff.State, intent.OpenPromptIntent{
		PromptKind: PromptKindCreateWorkspace,
	})
	typed := reducer.Reduce(promptOpened.State, intent.PromptAppendInputIntent{Text: "ops-center"})
	submitted := reducer.Reduce(typed.State, intent.SubmitPromptIntent{})

	if submitted.State.Domain.ActiveWorkspaceID == types.WorkspaceID("ws-1") {
		t.Fatalf("expected created workspace to become active")
	}
	if submitted.State.UI.Focus.Layer != types.FocusLayerTiled {
		t.Fatalf("expected focus to land in new workspace pane, got %+v", submitted.State.UI.Focus)
	}
}

func TestE2EReducerScenarioTerminalManagerEditMetadataFlow(t *testing.T) {
	reducer := New()
	state := newManagerAppState()

	openedManager := reducer.Reduce(state, intent.OpenTerminalManagerIntent{})
	moved := reducer.Reduce(openedManager.State, intent.TerminalManagerMoveIntent{Delta: 1})
	handoff := reducer.Reduce(moved.State, intent.TerminalManagerEditMetadataIntent{})
	if len(handoff.Effects) != 1 {
		t.Fatalf("expected metadata prompt handoff effect, got %d", len(handoff.Effects))
	}
	promptOpened := reducer.Reduce(handoff.State, intent.OpenPromptIntent{
		PromptKind: PromptKindEditTerminalMetadata,
		TerminalID: types.TerminalID("term-2"),
	})
	triggered := reducer.Reduce(promptOpened.State, intent.SubmitPromptIntent{
		Value: "build-log-v2\nenv=prod",
	})
	submitted := reducer.Reduce(triggered.State, intent.UpdateTerminalMetadataSucceededIntent{
		TerminalID: types.TerminalID("term-2"),
		Name:       "build-log-v2",
		Tags: map[string]string{
			"group": "build",
			"env":   "prod",
		},
	})

	if submitted.State.Domain.Terminals[types.TerminalID("term-2")].Name != "build-log-v2" {
		t.Fatalf("expected terminal metadata to update, got %+v", submitted.State.Domain.Terminals[types.TerminalID("term-2")])
	}
	if submitted.State.UI.Focus.Layer != types.FocusLayerTiled || submitted.State.UI.Focus.PaneID != types.PaneID("pane-1") {
		t.Fatalf("expected focus to return to originating pane, got %+v", submitted.State.UI.Focus)
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

func newManagerJumpAppState() types.AppState {
	state := newConnectedAppState()
	state.Domain.WorkspaceOrder = append(state.Domain.WorkspaceOrder, types.WorkspaceID("ws-2"))
	state.Domain.Workspaces[types.WorkspaceID("ws-2")] = types.WorkspaceState{
		ID:          types.WorkspaceID("ws-2"),
		Name:        "ops",
		ActiveTabID: types.TabID("tab-2"),
		TabOrder:    []types.TabID{types.TabID("tab-2")},
		Tabs: map[types.TabID]types.TabState{
			types.TabID("tab-2"): {
				ID:           types.TabID("tab-2"),
				Name:         "logs",
				ActivePaneID: types.PaneID("pane-remote"),
				ActiveLayer:  types.FocusLayerTiled,
				Panes: map[types.PaneID]types.PaneState{
					types.PaneID("pane-remote"): {
						ID:         types.PaneID("pane-remote"),
						Kind:       types.PaneKindTiled,
						SlotState:  types.PaneSlotConnected,
						TerminalID: types.TerminalID("term-2"),
					},
				},
				RootSplit: &types.SplitNode{PaneID: types.PaneID("pane-remote")},
			},
		},
	}
	state.Domain.Terminals[types.TerminalID("term-2")] = types.TerminalRef{
		ID:      types.TerminalID("term-2"),
		Name:    "build-log",
		State:   types.TerminalRunStateRunning,
		Command: []string{"tail", "-f", "build.log"},
		Tags:    map[string]string{"group": "build"},
		Visible: true,
	}
	state.Domain.Connections[types.TerminalID("term-2")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-2"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-remote")},
		OwnerPaneID:      types.PaneID("pane-remote"),
	}
	return state
}

func newManagerLocationJumpAppState() types.AppState {
	state := newManagerAppState()
	state.Domain.WorkspaceOrder = append(state.Domain.WorkspaceOrder, types.WorkspaceID("ws-2"))
	state.Domain.Workspaces[types.WorkspaceID("ws-2")] = types.WorkspaceState{
		ID:          types.WorkspaceID("ws-2"),
		Name:        "ops",
		ActiveTabID: types.TabID("tab-2"),
		TabOrder:    []types.TabID{types.TabID("tab-2")},
		Tabs: map[types.TabID]types.TabState{
			types.TabID("tab-2"): {
				ID:           types.TabID("tab-2"),
				Name:         "logs",
				ActivePaneID: types.PaneID("float-ops"),
				ActiveLayer:  types.FocusLayerFloating,
				FloatingOrder: []types.PaneID{
					types.PaneID("float-ops"),
				},
				Panes: map[types.PaneID]types.PaneState{
					types.PaneID("float-ops"): {
						ID:         types.PaneID("float-ops"),
						Kind:       types.PaneKindFloating,
						SlotState:  types.PaneSlotConnected,
						TerminalID: types.TerminalID("term-1"),
					},
				},
			},
		},
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1"), types.PaneID("float-2"), types.PaneID("float-ops")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	return state
}

func newFloatingPaneStackAppState() types.AppState {
	state := newConnectedAppState()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	delete(tab.Panes, types.PaneID("pane-1"))
	tab.Panes[types.PaneID("float-1")] = types.PaneState{
		ID:         types.PaneID("float-1"),
		Kind:       types.PaneKindFloating,
		SlotState:  types.PaneSlotConnected,
		TerminalID: types.TerminalID("term-1"),
	}
	tab.Panes[types.PaneID("float-2")] = types.PaneState{
		ID:         types.PaneID("float-2"),
		Kind:       types.PaneKindFloating,
		SlotState:  types.PaneSlotConnected,
		TerminalID: types.TerminalID("term-2"),
	}
	tab.FloatingOrder = []types.PaneID{types.PaneID("float-1"), types.PaneID("float-2")}
	tab.ActivePaneID = types.PaneID("float-1")
	tab.ActiveLayer = types.FocusLayerFloating
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.UI.Focus = types.FocusState{
		Layer:       types.FocusLayerFloating,
		WorkspaceID: types.WorkspaceID("ws-1"),
		TabID:       types.TabID("tab-1"),
		PaneID:      types.PaneID("float-1"),
	}
	term1 := state.Domain.Terminals[types.TerminalID("term-1")]
	term1.Visible = true
	state.Domain.Terminals[types.TerminalID("term-1")] = term1
	state.Domain.Terminals[types.TerminalID("term-2")] = types.TerminalRef{
		ID:      types.TerminalID("term-2"),
		Name:    "build-log",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("float-1")},
		OwnerPaneID:      types.PaneID("float-1"),
	}
	state.Domain.Connections[types.TerminalID("term-2")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-2"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("float-2")},
		OwnerPaneID:      types.PaneID("float-2"),
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
						ID:         types.PaneID("pane-float"),
						Kind:       types.PaneKindFloating,
						SlotState:  types.PaneSlotConnected,
						TerminalID: types.TerminalID("term-float"),
					},
				},
			},
		},
	}
	state.Domain.Terminals[types.TerminalID("term-float")] = types.TerminalRef{
		ID:    types.TerminalID("term-float"),
		Name:  "float-dev",
		State: types.TerminalRunStateRunning,
	}
	state.Domain.Connections[types.TerminalID("term-float")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-float"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-float")},
		OwnerPaneID:      types.PaneID("pane-float"),
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

func newSplitPaneAppState() types.AppState {
	state := newAppStateWithSinglePane()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	tab.Panes[types.PaneID("pane-1")] = types.PaneState{
		ID:         types.PaneID("pane-1"),
		Kind:       types.PaneKindTiled,
		SlotState:  types.PaneSlotConnected,
		TerminalID: types.TerminalID("term-1"),
	}
	tab.Panes[types.PaneID("pane-2")] = types.PaneState{
		ID:         types.PaneID("pane-2"),
		Kind:       types.PaneKindTiled,
		SlotState:  types.PaneSlotConnected,
		TerminalID: types.TerminalID("term-2"),
	}
	tab.RootSplit = &types.SplitNode{
		Direction: types.SplitDirectionVertical,
		Ratio:     0.5,
		First:     &types.SplitNode{PaneID: types.PaneID("pane-1")},
		Second:    &types.SplitNode{PaneID: types.PaneID("pane-2")},
	}
	tab.ActivePaneID = types.PaneID("pane-1")
	tab.ActiveLayer = types.FocusLayerTiled
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "api-dev",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	state.Domain.Terminals[types.TerminalID("term-2")] = types.TerminalRef{
		ID:      types.TerminalID("term-2"),
		Name:    "build-log",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	state.Domain.Connections[types.TerminalID("term-2")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-2"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-2")},
		OwnerPaneID:      types.PaneID("pane-2"),
	}
	return state
}

func newTwoTabAppState() types.AppState {
	state := newAppStateWithSinglePane()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	ws.TabOrder = []types.TabID{types.TabID("tab-1"), types.TabID("tab-2")}
	ws.ActiveTabID = types.TabID("tab-1")
	ws.Tabs[types.TabID("tab-1")] = types.TabState{
		ID:           types.TabID("tab-1"),
		Name:         "shell",
		ActivePaneID: types.PaneID("pane-1"),
		ActiveLayer:  types.FocusLayerTiled,
		Panes: map[types.PaneID]types.PaneState{
			types.PaneID("pane-1"): {
				ID:         types.PaneID("pane-1"),
				Kind:       types.PaneKindTiled,
				SlotState:  types.PaneSlotConnected,
				TerminalID: types.TerminalID("term-1"),
			},
		},
		RootSplit: &types.SplitNode{PaneID: types.PaneID("pane-1")},
	}
	ws.Tabs[types.TabID("tab-2")] = types.TabState{
		ID:           types.TabID("tab-2"),
		Name:         "logs",
		ActivePaneID: types.PaneID("pane-2"),
		ActiveLayer:  types.FocusLayerTiled,
		Panes: map[types.PaneID]types.PaneState{
			types.PaneID("pane-2"): {
				ID:         types.PaneID("pane-2"),
				Kind:       types.PaneKindTiled,
				SlotState:  types.PaneSlotConnected,
				TerminalID: types.TerminalID("term-2"),
			},
		},
		RootSplit: &types.SplitNode{PaneID: types.PaneID("pane-2")},
	}
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "api-dev",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	state.Domain.Terminals[types.TerminalID("term-2")] = types.TerminalRef{
		ID:      types.TerminalID("term-2"),
		Name:    "build-log",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	state.Domain.Connections[types.TerminalID("term-2")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-2"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-2")},
		OwnerPaneID:      types.PaneID("pane-2"),
	}
	state.UI.Focus = types.FocusState{
		Layer:       types.FocusLayerTiled,
		WorkspaceID: types.WorkspaceID("ws-1"),
		TabID:       types.TabID("tab-1"),
		PaneID:      types.PaneID("pane-1"),
	}
	return state
}

func newWorkspaceJumpAutoAcquireState(autoAcquire bool) types.AppState {
	state := newSharedTerminalAppState()
	state.Domain.WorkspaceOrder = append(state.Domain.WorkspaceOrder, types.WorkspaceID("ws-2"))
	state.Domain.Workspaces[types.WorkspaceID("ws-2")] = types.WorkspaceState{
		ID:          types.WorkspaceID("ws-2"),
		Name:        "ops",
		ActiveTabID: types.TabID("tab-2"),
		TabOrder:    []types.TabID{types.TabID("tab-2")},
		Tabs: map[types.TabID]types.TabState{
			types.TabID("tab-2"): {
				ID:               types.TabID("tab-2"),
				Name:             "logs",
				ActivePaneID:     types.PaneID("pane-2"),
				ActiveLayer:      types.FocusLayerTiled,
				AutoAcquireOwner: autoAcquire,
				Panes: map[types.PaneID]types.PaneState{
					types.PaneID("pane-2"): {
						ID:         types.PaneID("pane-2"),
						Kind:       types.PaneKindTiled,
						SlotState:  types.PaneSlotConnected,
						TerminalID: types.TerminalID("term-1"),
					},
				},
			},
		},
	}
	conn := state.Domain.Connections[types.TerminalID("term-1")]
	conn.ConnectedPaneIDs = []types.PaneID{types.PaneID("pane-1"), types.PaneID("pane-2")}
	conn.OwnerPaneID = types.PaneID("pane-1")
	state.Domain.Connections[types.TerminalID("term-1")] = conn
	return state
}

func newManagerAppState() types.AppState {
	state := newConnectedAppState()
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
				ActivePaneID: types.PaneID("float-2"),
				ActiveLayer:  types.FocusLayerFloating,
				FloatingOrder: []types.PaneID{
					types.PaneID("float-2"),
				},
				Panes: map[types.PaneID]types.PaneState{
					types.PaneID("float-2"): {
						ID:         types.PaneID("float-2"),
						Kind:       types.PaneKindFloating,
						SlotState:  types.PaneSlotConnected,
						TerminalID: types.TerminalID("term-1"),
					},
				},
			},
		},
	}
	state.Domain.Terminals[types.TerminalID("term-2")] = types.TerminalRef{
		ID:      types.TerminalID("term-2"),
		Name:    "build-log",
		State:   types.TerminalRunStateRunning,
		Command: []string{"tail", "-f", "build.log"},
		Tags:    map[string]string{"group": "build"},
	}
	state.Domain.Terminals[types.TerminalID("term-3")] = types.TerminalRef{
		ID:      types.TerminalID("term-3"),
		Name:    "ops-watch",
		State:   types.TerminalRunStateRunning,
		Command: []string{"journalctl", "-f"},
		Tags:    map[string]string{"team": "ops"},
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1"), types.PaneID("float-2")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	return state
}

func newFollowerManagerAppState() types.AppState {
	state := newManagerAppState()
	workspace := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := workspace.Tabs[types.TabID("tab-1")]
	tab.Panes[types.PaneID("pane-2")] = types.PaneState{
		ID:         types.PaneID("pane-2"),
		Kind:       types.PaneKindTiled,
		SlotState:  types.PaneSlotConnected,
		TerminalID: types.TerminalID("term-1"),
	}
	tab.ActivePaneID = types.PaneID("pane-2")
	workspace.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = workspace
	state.UI.Focus = types.FocusState{
		Layer:       types.FocusLayerTiled,
		WorkspaceID: types.WorkspaceID("ws-1"),
		TabID:       types.TabID("tab-1"),
		PaneID:      types.PaneID("pane-2"),
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1"), types.PaneID("pane-2"), types.PaneID("float-2")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	return state
}
