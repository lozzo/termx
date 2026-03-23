package reducer

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lozzow/termx/tui/app/intent"
	"github.com/lozzow/termx/tui/domain/connection"
	promptdomain "github.com/lozzow/termx/tui/domain/prompt"
	terminalpickerdomain "github.com/lozzow/termx/tui/domain/terminalpicker"
	terminalmanagerdomain "github.com/lozzow/termx/tui/domain/terminalmanager"
	"github.com/lozzow/termx/tui/domain/types"
	workspacedomain "github.com/lozzow/termx/tui/domain/workspace"
)

type Effect interface {
	effectName() string
}

type ConnectTerminalEffect struct {
	PaneID     types.PaneID
	TerminalID types.TerminalID
}

func (ConnectTerminalEffect) effectName() string { return "connect_terminal" }

type CreateTerminalEffect struct {
	PaneID  types.PaneID
	Command []string
	Name    string
}

func (CreateTerminalEffect) effectName() string { return "create_terminal" }

type StopTerminalEffect struct {
	TerminalID types.TerminalID
}

func (StopTerminalEffect) effectName() string { return "stop_terminal" }

type ConnectTerminalInNewTabEffect struct {
	WorkspaceID types.WorkspaceID
	TerminalID  types.TerminalID
}

func (ConnectTerminalInNewTabEffect) effectName() string { return "connect_terminal_in_new_tab" }

type ConnectTerminalInFloatingPaneEffect struct {
	WorkspaceID types.WorkspaceID
	TabID       types.TabID
	TerminalID  types.TerminalID
}

func (ConnectTerminalInFloatingPaneEffect) effectName() string {
	return "connect_terminal_in_floating_pane"
}

type UpdateTerminalMetadataEffect struct {
	TerminalID types.TerminalID
	Name       string
	Tags       map[string]string
}

func (UpdateTerminalMetadataEffect) effectName() string { return "update_terminal_metadata" }

type OpenPromptEffect struct {
	PromptKind string
	TerminalID types.TerminalID
}

func (OpenPromptEffect) effectName() string { return "open_prompt" }

const (
	PromptKindCreateWorkspace      = "create_workspace"
	PromptKindEditTerminalMetadata = "edit_terminal_metadata"
)

type Result struct {
	State   types.AppState
	Effects []Effect
}

type StateReducer interface {
	Reduce(state types.AppState, in intent.Intent) Result
}

type DefaultReducer struct{}

func New() StateReducer {
	return DefaultReducer{}
}

// Reduce 保持纯状态迁移，不直接触碰 runtime service。
// 这里先把最容易反复返工的连接、退出和 workspace 跳转链路锁定下来。
func (DefaultReducer) Reduce(state types.AppState, in intent.Intent) Result {
	next := cloneAppState(state)
	result := Result{State: next}

	switch intentValue := in.(type) {
	case intent.ConnectTerminalIntent:
		applyConnectTerminal(&result.State, intentValue)
		result.Effects = append(result.Effects, ConnectTerminalEffect{
			PaneID:     intentValue.PaneID,
			TerminalID: intentValue.TerminalID,
		})
	case intent.StopTerminalIntent:
		applyStopTerminal(&result.State, intentValue)
		result.Effects = append(result.Effects, StopTerminalEffect{
			TerminalID: intentValue.TerminalID,
		})
	case intent.TerminalProgramExitedIntent:
		applyProgramExited(&result.State, intentValue)
	case intent.WorkspaceTreeJumpIntent:
		applyWorkspaceTreeJump(&result.State, intentValue)
	case intent.ClosePaneIntent:
		applyClosePane(&result.State, intentValue)
	case intent.OpenTerminalPickerIntent:
		applyOpenTerminalPicker(&result.State)
	case intent.OpenWorkspacePickerIntent:
		applyOpenWorkspacePicker(&result.State)
	case intent.OpenTerminalManagerIntent:
		applyOpenTerminalManager(&result.State)
	case intent.OpenPromptIntent:
		applyOpenPrompt(&result.State, intentValue)
	case intent.CloseOverlayIntent:
		applyCloseOverlay(&result.State)
	case intent.WorkspacePickerMoveIntent:
		applyWorkspacePickerMove(&result.State, intentValue)
	case intent.WorkspacePickerAppendQueryIntent:
		applyWorkspacePickerAppendQuery(&result.State, intentValue)
	case intent.WorkspacePickerBackspaceIntent:
		applyWorkspacePickerBackspace(&result.State)
	case intent.WorkspacePickerExpandIntent:
		applyWorkspacePickerExpand(&result.State)
	case intent.WorkspacePickerCollapseIntent:
		applyWorkspacePickerCollapse(&result.State)
	case intent.WorkspacePickerSubmitIntent:
		result.Effects = append(result.Effects, applyWorkspacePickerSubmit(&result.State)...)
	case intent.TerminalPickerMoveIntent:
		applyTerminalPickerMove(&result.State, intentValue)
	case intent.TerminalPickerAppendQueryIntent:
		applyTerminalPickerAppendQuery(&result.State, intentValue)
	case intent.TerminalPickerBackspaceIntent:
		applyTerminalPickerBackspace(&result.State)
	case intent.TerminalPickerSubmitIntent:
		applyTerminalPickerSubmit(&result)
	case intent.TerminalManagerMoveIntent:
		applyTerminalManagerMove(&result.State, intentValue)
	case intent.TerminalManagerAppendQueryIntent:
		applyTerminalManagerAppendQuery(&result.State, intentValue)
	case intent.TerminalManagerBackspaceIntent:
		applyTerminalManagerBackspace(&result.State)
	case intent.TerminalManagerConnectHereIntent:
		applyTerminalManagerConnectHere(&result, intentValue)
	case intent.TerminalManagerConnectInNewTabIntent:
		applyTerminalManagerConnectInNewTab(&result)
	case intent.TerminalManagerConnectInFloatingPaneIntent:
		applyTerminalManagerConnectInFloatingPane(&result)
	case intent.TerminalManagerEditMetadataIntent:
		applyTerminalManagerEditMetadata(&result)
	case intent.TerminalManagerStopIntent:
		applyTerminalManagerStop(&result)
	case intent.TerminalManagerCreateTerminalIntent:
		applyTerminalManagerCreateTerminal(&result)
	case intent.SubmitPromptIntent:
		result.Effects = append(result.Effects, applySubmitPrompt(&result.State, intentValue)...)
	case intent.CancelPromptIntent:
		applyCancelPrompt(&result.State)
	case intent.PromptAppendInputIntent:
		applyPromptAppendInput(&result.State, intentValue)
	case intent.PromptBackspaceIntent:
		applyPromptBackspace(&result.State)
	case intent.PromptNextFieldIntent:
		applyPromptNextField(&result.State)
	case intent.PromptPreviousFieldIntent:
		applyPromptPreviousField(&result.State)
	case intent.ActivateModeIntent:
		applyActivateMode(&result.State, intentValue)
	case intent.ModeTimedOutIntent:
		applyModeTimedOut(&result.State, intentValue)
	}

	return result
}

func applyConnectTerminal(state *types.AppState, in intent.ConnectTerminalIntent) {
	disconnectPaneFromCurrentTerminal(state, in.PaneID, in.TerminalID)
	setPaneState(state, in.PaneID, func(pane *types.PaneState) {
		pane.TerminalID = in.TerminalID
		pane.SlotState = types.PaneSlotConnected
		pane.LastExitCode = nil
	})
	terminal := state.Domain.Terminals[in.TerminalID]
	if terminal.ID == "" {
		terminal.ID = in.TerminalID
	}
	state.Domain.Terminals[in.TerminalID] = terminal
	snapshot := state.Domain.Connections[in.TerminalID]
	snapshot.TerminalID = in.TerminalID
	conn := connection.FromSnapshot(snapshot)
	conn.Connect(in.PaneID)
	state.Domain.Connections[in.TerminalID] = conn.Snapshot()
}

func applyStopTerminal(state *types.AppState, in intent.StopTerminalIntent) {
	forEachPane(state, func(pane *types.PaneState) {
		if pane.TerminalID != in.TerminalID {
			return
		}
		pane.TerminalID = ""
		pane.SlotState = types.PaneSlotEmpty
	})
	terminal := state.Domain.Terminals[in.TerminalID]
	terminal.State = types.TerminalRunStateStopped
	state.Domain.Terminals[in.TerminalID] = terminal
	delete(state.Domain.Connections, in.TerminalID)
}

func applyProgramExited(state *types.AppState, in intent.TerminalProgramExitedIntent) {
	exitCode := in.ExitCode
	forEachPane(state, func(pane *types.PaneState) {
		if pane.TerminalID != in.TerminalID {
			return
		}
		pane.SlotState = types.PaneSlotExited
		pane.LastExitCode = &exitCode
	})
	terminal := state.Domain.Terminals[in.TerminalID]
	terminal.State = types.TerminalRunStateExited
	terminal.ExitCode = &exitCode
	state.Domain.Terminals[in.TerminalID] = terminal
}

func applyWorkspaceTreeJump(state *types.AppState, in intent.WorkspaceTreeJumpIntent) {
	workspace, ok := state.Domain.Workspaces[in.WorkspaceID]
	if !ok {
		return
	}
	focus, ok := workspacedomain.ResolveTreeJumpFocus(workspace, in.TabID, in.PaneID)
	if !ok {
		return
	}
	workspace.ActiveTabID = in.TabID
	tab := workspace.Tabs[in.TabID]
	tab.ActivePaneID = in.PaneID
	tab.ActiveLayer = focus.Layer
	workspace.Tabs[in.TabID] = tab
	state.Domain.Workspaces[in.WorkspaceID] = workspace
	state.Domain.ActiveWorkspaceID = in.WorkspaceID
	state.UI.Focus = focus
}

func applyClosePane(state *types.AppState, in intent.ClosePaneIntent) {
	for workspaceID, workspace := range state.Domain.Workspaces {
		changedWorkspace := false
		for tabID, tab := range workspace.Tabs {
			pane, ok := tab.Panes[in.PaneID]
			if !ok {
				continue
			}
			delete(tab.Panes, in.PaneID)
			if pane.TerminalID != "" {
				snapshot := state.Domain.Connections[pane.TerminalID]
				conn := connection.FromSnapshot(snapshot)
				conn.Disconnect(in.PaneID)
				next := conn.Snapshot()
				if len(next.ConnectedPaneIDs) == 0 {
					delete(state.Domain.Connections, pane.TerminalID)
				} else {
					state.Domain.Connections[pane.TerminalID] = next
				}
			}
			if tab.ActivePaneID == in.PaneID {
				tab.ActivePaneID = firstRemainingPaneID(tab.Panes)
			}
			tab.RootSplit = removePaneFromSplit(tab.RootSplit, in.PaneID)
			workspace.Tabs[tabID] = tab
			changedWorkspace = true
		}
		if changedWorkspace {
			state.Domain.Workspaces[workspaceID] = workspace
		}
	}
}

// applyOpenWorkspacePicker 只负责切到 overlay 焦点，并挂上可变的 picker 状态。
// 这样后续 shell 只要把键盘输入翻译成 intent，就不会再直接改 UI 内部结构。
func applyOpenWorkspacePicker(state *types.AppState) {
	returnFocus := state.UI.Focus
	returnFocus.OverlayTarget = ""
	state.UI.Overlay = types.OverlayState{
		Kind:        types.OverlayWorkspacePicker,
		Data:        workspacedomain.NewPickerState(state.Domain),
		ReturnFocus: returnFocus,
	}
	state.UI.Focus = types.FocusState{
		Layer:         types.FocusLayerOverlay,
		WorkspaceID:   returnFocus.WorkspaceID,
		TabID:         returnFocus.TabID,
		PaneID:        returnFocus.PaneID,
		OverlayTarget: types.OverlayWorkspacePicker,
	}
	state.UI.Mode = types.ModeState{
		Active: types.ModePicker,
	}
}

func applyOpenTerminalPicker(state *types.AppState) {
	returnFocus := state.UI.Focus
	returnFocus.OverlayTarget = ""
	state.UI.Overlay = types.OverlayState{
		Kind:        types.OverlayTerminalPicker,
		Data:        terminalpickerdomain.NewState(state.Domain, returnFocus),
		ReturnFocus: returnFocus,
	}
	state.UI.Focus = types.FocusState{
		Layer:         types.FocusLayerOverlay,
		WorkspaceID:   returnFocus.WorkspaceID,
		TabID:         returnFocus.TabID,
		PaneID:        returnFocus.PaneID,
		OverlayTarget: types.OverlayTerminalPicker,
	}
	state.UI.Mode = types.ModeState{Active: types.ModePicker}
}

func applyOpenTerminalManager(state *types.AppState) {
	returnFocus := state.UI.Focus
	returnFocus.OverlayTarget = ""
	state.UI.Overlay = types.OverlayState{
		Kind:        types.OverlayTerminalManager,
		Data:        terminalmanagerdomain.NewState(state.Domain, returnFocus),
		ReturnFocus: returnFocus,
	}
	state.UI.Focus = types.FocusState{
		Layer:         types.FocusLayerOverlay,
		WorkspaceID:   returnFocus.WorkspaceID,
		TabID:         returnFocus.TabID,
		PaneID:        returnFocus.PaneID,
		OverlayTarget: types.OverlayTerminalManager,
	}
	state.UI.Mode = types.ModeState{
		Active: types.ModePicker,
	}
}

func applyOpenPrompt(state *types.AppState, in intent.OpenPromptIntent) {
	returnFocus := state.UI.Focus
	returnFocus.OverlayTarget = ""
	state.UI.Overlay = types.OverlayState{
		Kind:        types.OverlayPrompt,
		Data:        buildPromptState(state, in),
		ReturnFocus: returnFocus,
	}
	state.UI.Focus = types.FocusState{
		Layer:         types.FocusLayerPrompt,
		WorkspaceID:   returnFocus.WorkspaceID,
		TabID:         returnFocus.TabID,
		PaneID:        returnFocus.PaneID,
		OverlayTarget: types.OverlayPrompt,
	}
}

func applyCloseOverlay(state *types.AppState) {
	if state.UI.Overlay.Kind == types.OverlayNone {
		return
	}
	state.UI.Focus = state.UI.Overlay.ReturnFocus
	state.UI.Focus.OverlayTarget = ""
	state.UI.Overlay = types.OverlayState{Kind: types.OverlayNone}
	if state.UI.Mode.Active == types.ModePicker {
		state.UI.Mode = types.ModeState{Active: types.ModeNone}
	}
}

func applyWorkspacePickerMove(state *types.AppState, in intent.WorkspacePickerMoveIntent) {
	picker, ok := workspacePicker(state)
	if !ok {
		return
	}
	picker.MoveSelection(in.Delta)
}

func applyWorkspacePickerAppendQuery(state *types.AppState, in intent.WorkspacePickerAppendQueryIntent) {
	picker, ok := workspacePicker(state)
	if !ok {
		return
	}
	picker.AppendQuery(in.Text)
}

func applyTerminalPickerMove(state *types.AppState, in intent.TerminalPickerMoveIntent) {
	picker, ok := terminalPicker(state)
	if !ok {
		return
	}
	picker.MoveSelection(in.Delta)
}

func applyTerminalPickerAppendQuery(state *types.AppState, in intent.TerminalPickerAppendQueryIntent) {
	picker, ok := terminalPicker(state)
	if !ok {
		return
	}
	picker.AppendQuery(in.Text)
}

func applyTerminalPickerBackspace(state *types.AppState) {
	picker, ok := terminalPicker(state)
	if !ok {
		return
	}
	picker.BackspaceQuery()
}

func applyWorkspacePickerBackspace(state *types.AppState) {
	picker, ok := workspacePicker(state)
	if !ok {
		return
	}
	picker.BackspaceQuery()
}

func applyWorkspacePickerExpand(state *types.AppState) {
	picker, ok := workspacePicker(state)
	if !ok {
		return
	}
	picker.ExpandSelected()
}

func applyWorkspacePickerCollapse(state *types.AppState) {
	picker, ok := workspacePicker(state)
	if !ok {
		return
	}
	picker.CollapseSelected()
}

func applyWorkspacePickerSubmitNode(state *types.AppState, node workspacedomain.TreeNode) {
	switch node.Kind {
	case workspacedomain.TreeNodeKindWorkspace:
		workspace, ok := state.Domain.Workspaces[node.WorkspaceID]
		if !ok {
			return
		}
		tabID := workspace.ActiveTabID
		tab, ok := workspace.Tabs[tabID]
		if !ok {
			return
		}
		applyWorkspaceTreeJump(state, intent.WorkspaceTreeJumpIntent{
			WorkspaceID: node.WorkspaceID,
			TabID:       tabID,
			PaneID:      tab.ActivePaneID,
		})
	case workspacedomain.TreeNodeKindTab:
		workspace, ok := state.Domain.Workspaces[node.WorkspaceID]
		if !ok {
			return
		}
		tab, ok := workspace.Tabs[node.TabID]
		if !ok {
			return
		}
		applyWorkspaceTreeJump(state, intent.WorkspaceTreeJumpIntent{
			WorkspaceID: node.WorkspaceID,
			TabID:       node.TabID,
			PaneID:      tab.ActivePaneID,
		})
	case workspacedomain.TreeNodeKindPane:
		applyWorkspaceTreeJump(state, intent.WorkspaceTreeJumpIntent{
			WorkspaceID: node.WorkspaceID,
			TabID:       node.TabID,
			PaneID:      node.PaneID,
		})
	default:
		return
	}

	state.UI.Overlay = types.OverlayState{Kind: types.OverlayNone}
	state.UI.Focus.OverlayTarget = ""
	if state.UI.Mode.Active == types.ModePicker {
		state.UI.Mode = types.ModeState{Active: types.ModeNone}
	}
}

func applyWorkspacePickerSubmit(resultState *types.AppState) []Effect {
	picker, ok := workspacePicker(resultState)
	if !ok {
		return nil
	}
	node, ok := picker.SelectedNode()
	if !ok {
		return nil
	}
	if node.Kind != workspacedomain.TreeNodeKindCreate {
		applyWorkspacePickerSubmitNode(resultState, node)
		return nil
	}
	applyCloseOverlay(resultState)
	return []Effect{OpenPromptEffect{PromptKind: PromptKindCreateWorkspace}}
}

func applyTerminalManagerMove(state *types.AppState, in intent.TerminalManagerMoveIntent) {
	manager, ok := terminalManager(state)
	if !ok {
		return
	}
	manager.MoveSelection(in.Delta)
}

func applyTerminalManagerAppendQuery(state *types.AppState, in intent.TerminalManagerAppendQueryIntent) {
	manager, ok := terminalManager(state)
	if !ok {
		return
	}
	manager.AppendQuery(in.Text)
}

func applyTerminalManagerBackspace(state *types.AppState) {
	manager, ok := terminalManager(state)
	if !ok {
		return
	}
	manager.BackspaceQuery()
}

func applyTerminalManagerConnectHere(result *Result, _ intent.TerminalManagerConnectHereIntent) {
	manager, ok := terminalManager(&result.State)
	if !ok {
		return
	}
	terminalID, ok := manager.SelectedTerminalID()
	if !ok {
		return
	}
	paneID := result.State.UI.Overlay.ReturnFocus.PaneID
	applyConnectTerminal(&result.State, intent.ConnectTerminalIntent{
		PaneID:     paneID,
		TerminalID: terminalID,
		Source:     intent.ConnectSourceManagerHere,
	})
	result.Effects = append(result.Effects, ConnectTerminalEffect{
		PaneID:     paneID,
		TerminalID: terminalID,
	})
	applyCloseOverlay(&result.State)
}

func applyTerminalPickerSubmit(result *Result) {
	picker, ok := terminalPicker(&result.State)
	if !ok {
		return
	}
	row, ok := picker.SelectedRow()
	if !ok {
		return
	}
	switch row.Kind {
	case terminalpickerdomain.RowKindCreate:
		result.Effects = append(result.Effects, CreateTerminalEffect{
			PaneID:  result.State.UI.Overlay.ReturnFocus.PaneID,
			Command: defaultCreateTerminalCommand(),
			Name:    defaultCreateTerminalName(result.State.UI.Overlay.ReturnFocus),
		})
		applyCloseOverlay(&result.State)
	case terminalpickerdomain.RowKindTerminal:
		paneID := result.State.UI.Overlay.ReturnFocus.PaneID
		applyConnectTerminal(&result.State, intent.ConnectTerminalIntent{
			PaneID:     paneID,
			TerminalID: row.TerminalID,
			Source:     intent.ConnectSourcePicker,
		})
		result.Effects = append(result.Effects, ConnectTerminalEffect{
			PaneID:     paneID,
			TerminalID: row.TerminalID,
		})
		applyCloseOverlay(&result.State)
	}
}

func applyTerminalManagerConnectInNewTab(result *Result) {
	manager, ok := terminalManager(&result.State)
	if !ok {
		return
	}
	terminalID, ok := manager.SelectedTerminalID()
	if !ok {
		return
	}
	workspaceID := result.State.UI.Overlay.ReturnFocus.WorkspaceID
	result.Effects = append(result.Effects, ConnectTerminalInNewTabEffect{
		WorkspaceID: workspaceID,
		TerminalID:  terminalID,
	})
	applyCloseOverlay(&result.State)
}

func applyTerminalManagerConnectInFloatingPane(result *Result) {
	manager, ok := terminalManager(&result.State)
	if !ok {
		return
	}
	terminalID, ok := manager.SelectedTerminalID()
	if !ok {
		return
	}
	result.Effects = append(result.Effects, ConnectTerminalInFloatingPaneEffect{
		WorkspaceID: result.State.UI.Overlay.ReturnFocus.WorkspaceID,
		TabID:       result.State.UI.Overlay.ReturnFocus.TabID,
		TerminalID:  terminalID,
	})
	applyCloseOverlay(&result.State)
}

func applyTerminalManagerEditMetadata(result *Result) {
	manager, ok := terminalManager(&result.State)
	if !ok {
		return
	}
	terminalID, ok := manager.SelectedTerminalID()
	if !ok {
		return
	}
	applyCloseOverlay(&result.State)
	result.Effects = append(result.Effects, OpenPromptEffect{
		PromptKind: PromptKindEditTerminalMetadata,
		TerminalID: terminalID,
	})
}

func applyTerminalManagerStop(result *Result) {
	manager, ok := terminalManager(&result.State)
	if !ok {
		return
	}
	terminalID, ok := manager.SelectedTerminalID()
	if !ok {
		return
	}
	applyStopTerminal(&result.State, intent.StopTerminalIntent{TerminalID: terminalID})
	result.Effects = append(result.Effects, StopTerminalEffect{TerminalID: terminalID})
	applyCloseOverlay(&result.State)
}

func applyTerminalManagerCreateTerminal(result *Result) {
	manager, ok := terminalManager(&result.State)
	if !ok {
		return
	}
	row, ok := manager.SelectedRow()
	if !ok || row.Kind != terminalmanagerdomain.RowKindCreate {
		return
	}
	result.Effects = append(result.Effects, CreateTerminalEffect{
		PaneID:  result.State.UI.Overlay.ReturnFocus.PaneID,
		Command: defaultCreateTerminalCommand(),
		Name:    defaultCreateTerminalName(result.State.UI.Overlay.ReturnFocus),
	})
	applyCloseOverlay(&result.State)
}

func applySubmitPrompt(state *types.AppState, in intent.SubmitPromptIntent) []Effect {
	promptState, ok := promptState(state)
	if !ok {
		return nil
	}
	value := promptValue(promptState, in.Value)
	if value == "" {
		return nil
	}
	switch promptState.Kind {
	case promptdomain.KindCreateWorkspace:
		applyCreateWorkspace(state, value)
		return nil
	case promptdomain.KindEditTerminalMetadata:
		return applyUpdateTerminalMetadataFromPrompt(state, promptState, value)
	default:
		return nil
	}
}

func applyCancelPrompt(state *types.AppState) {
	if state.UI.Overlay.Kind != types.OverlayPrompt {
		return
	}
	applyCloseOverlay(state)
}

func applyPromptAppendInput(state *types.AppState, in intent.PromptAppendInputIntent) {
	prompt, ok := promptState(state)
	if !ok {
		return
	}
	prompt.AppendInput(in.Text)
}

func applyPromptBackspace(state *types.AppState) {
	prompt, ok := promptState(state)
	if !ok {
		return
	}
	prompt.BackspaceInput()
}

func applyPromptNextField(state *types.AppState) {
	prompt, ok := promptState(state)
	if !ok {
		return
	}
	prompt.NextField()
}

func applyPromptPreviousField(state *types.AppState) {
	prompt, ok := promptState(state)
	if !ok {
		return
	}
	prompt.PreviousField()
}

// applyCreateWorkspace 为新 workspace 建立最小可工作骨架：
// 一个默认 tab，一个未连接 terminal 的 pane，并把焦点直接落过去。
func applyCreateWorkspace(state *types.AppState, name string) {
	workspaceID := nextWorkspaceID(state, name)
	tabID := types.TabID(fmt.Sprintf("%s-tab-1", workspaceID))
	paneID := types.PaneID(fmt.Sprintf("%s-pane-1", workspaceID))
	tab := types.TabState{
		ID:           tabID,
		Name:         "main",
		ActivePaneID: paneID,
		ActiveLayer:  types.FocusLayerTiled,
		Panes: map[types.PaneID]types.PaneState{
			paneID: {
				ID:        paneID,
				Kind:      types.PaneKindTiled,
				SlotState: types.PaneSlotEmpty,
			},
		},
		RootSplit: &types.SplitNode{PaneID: paneID},
	}
	state.Domain.Workspaces[workspaceID] = types.WorkspaceState{
		ID:          workspaceID,
		Name:        name,
		ActiveTabID: tabID,
		TabOrder:    []types.TabID{tabID},
		Tabs: map[types.TabID]types.TabState{
			tabID: tab,
		},
	}
	state.Domain.WorkspaceOrder = append(state.Domain.WorkspaceOrder, workspaceID)
	state.Domain.ActiveWorkspaceID = workspaceID
	state.UI.Overlay = types.OverlayState{Kind: types.OverlayNone}
	state.UI.Focus = types.FocusState{
		Layer:       types.FocusLayerTiled,
		WorkspaceID: workspaceID,
		TabID:       tabID,
		PaneID:      paneID,
	}
}

func applyUpdateTerminalMetadataFromPrompt(state *types.AppState, promptState *promptdomain.State, value string) []Effect {
	terminal, ok := state.Domain.Terminals[promptState.TerminalID]
	if !ok {
		return nil
	}
	name, tags := parseTerminalMetadataPrompt(value, terminal)
	terminal.Name = name
	terminal.Tags = tags
	state.Domain.Terminals[promptState.TerminalID] = terminal
	applyCloseOverlay(state)
	return []Effect{UpdateTerminalMetadataEffect{
		TerminalID: promptState.TerminalID,
		Name:       name,
		Tags:       cloneStringMap(tags),
	}}
}

func applyActivateMode(state *types.AppState, in intent.ActivateModeIntent) {
	state.UI.Mode = types.ModeState{
		Active:     in.Mode,
		Sticky:     in.Sticky,
		DeadlineAt: cloneTimePtr(in.DeadlineAt),
	}
}

func applyModeTimedOut(state *types.AppState, in intent.ModeTimedOutIntent) {
	if state.UI.Mode.Active == types.ModeNone || state.UI.Mode.Sticky || state.UI.Mode.DeadlineAt == nil {
		return
	}
	if in.Now.Before(*state.UI.Mode.DeadlineAt) {
		return
	}
	state.UI.Mode = types.ModeState{Active: types.ModeNone}
}

func setPaneState(state *types.AppState, paneID types.PaneID, mutate func(*types.PaneState)) {
	for workspaceID, workspace := range state.Domain.Workspaces {
		changedWorkspace := false
		for tabID, tab := range workspace.Tabs {
			pane, ok := tab.Panes[paneID]
			if !ok {
				continue
			}
			mutate(&pane)
			tab.Panes[paneID] = pane
			workspace.Tabs[tabID] = tab
			changedWorkspace = true
		}
		if changedWorkspace {
			state.Domain.Workspaces[workspaceID] = workspace
		}
	}
}

func forEachPane(state *types.AppState, fn func(*types.PaneState)) {
	for workspaceID, workspace := range state.Domain.Workspaces {
		for tabID, tab := range workspace.Tabs {
			for paneID, pane := range tab.Panes {
				fn(&pane)
				tab.Panes[paneID] = pane
			}
			workspace.Tabs[tabID] = tab
		}
		state.Domain.Workspaces[workspaceID] = workspace
	}
}

func cloneAppState(state types.AppState) types.AppState {
	next := state
	next.Domain.WorkspaceOrder = append([]types.WorkspaceID(nil), state.Domain.WorkspaceOrder...)
	next.Domain.Workspaces = make(map[types.WorkspaceID]types.WorkspaceState, len(state.Domain.Workspaces))
	for workspaceID, workspace := range state.Domain.Workspaces {
		nextWorkspace := workspace
		nextWorkspace.TabOrder = append([]types.TabID(nil), workspace.TabOrder...)
		nextWorkspace.Tabs = make(map[types.TabID]types.TabState, len(workspace.Tabs))
		for tabID, tab := range workspace.Tabs {
			nextTab := tab
			nextTab.FloatingOrder = append([]types.PaneID(nil), tab.FloatingOrder...)
			nextTab.Panes = make(map[types.PaneID]types.PaneState, len(tab.Panes))
			for paneID, pane := range tab.Panes {
				nextTab.Panes[paneID] = pane
			}
			nextWorkspace.Tabs[tabID] = nextTab
		}
		next.Domain.Workspaces[workspaceID] = nextWorkspace
	}
	next.Domain.Terminals = make(map[types.TerminalID]types.TerminalRef, len(state.Domain.Terminals))
	for terminalID, terminal := range state.Domain.Terminals {
		clone := terminal
		clone.Command = append([]string(nil), terminal.Command...)
		if terminal.Tags != nil {
			clone.Tags = make(map[string]string, len(terminal.Tags))
			for k, v := range terminal.Tags {
				clone.Tags[k] = v
			}
		}
		next.Domain.Terminals[terminalID] = clone
	}
	next.Domain.Connections = make(map[types.TerminalID]types.ConnectionState, len(state.Domain.Connections))
	for terminalID, conn := range state.Domain.Connections {
		next.Domain.Connections[terminalID] = types.ConnectionState{
			TerminalID:        conn.TerminalID,
			ConnectedPaneIDs:  append([]types.PaneID(nil), conn.ConnectedPaneIDs...),
			OwnerPaneID:       conn.OwnerPaneID,
			AutoAcquirePolicy: conn.AutoAcquirePolicy,
		}
	}
	next.UI.Mode = types.ModeState{
		Active:     state.UI.Mode.Active,
		Sticky:     state.UI.Mode.Sticky,
		DeadlineAt: cloneTimePtr(state.UI.Mode.DeadlineAt),
	}
	next.UI.Overlay = types.OverlayState{
		Kind:        state.UI.Overlay.Kind,
		ReturnFocus: state.UI.Overlay.ReturnFocus,
	}
	if state.UI.Overlay.Data != nil {
		next.UI.Overlay.Data = state.UI.Overlay.Data.CloneOverlayData()
	}
	return next
}

func firstRemainingPaneID(panes map[types.PaneID]types.PaneState) types.PaneID {
	for paneID := range panes {
		return paneID
	}
	return ""
}

func removePaneFromSplit(node *types.SplitNode, paneID types.PaneID) *types.SplitNode {
	if node == nil {
		return nil
	}
	if node.First == nil && node.Second == nil {
		if node.PaneID == paneID {
			return nil
		}
		return node
	}
	node.First = removePaneFromSplit(node.First, paneID)
	node.Second = removePaneFromSplit(node.Second, paneID)
	switch {
	case node.First == nil:
		return node.Second
	case node.Second == nil:
		return node.First
	default:
		return node
	}
}

func workspacePicker(state *types.AppState) (*workspacedomain.PickerState, bool) {
	if state.UI.Overlay.Kind != types.OverlayWorkspacePicker || state.UI.Overlay.Data == nil {
		return nil, false
	}
	picker, ok := state.UI.Overlay.Data.(*workspacedomain.PickerState)
	return picker, ok
}

func terminalPicker(state *types.AppState) (*terminalpickerdomain.State, bool) {
	if state.UI.Overlay.Kind != types.OverlayTerminalPicker || state.UI.Overlay.Data == nil {
		return nil, false
	}
	picker, ok := state.UI.Overlay.Data.(*terminalpickerdomain.State)
	return picker, ok
}

func cloneTimePtr(in *time.Time) *time.Time {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func terminalManager(state *types.AppState) (*terminalmanagerdomain.State, bool) {
	if state.UI.Overlay.Kind != types.OverlayTerminalManager || state.UI.Overlay.Data == nil {
		return nil, false
	}
	manager, ok := state.UI.Overlay.Data.(*terminalmanagerdomain.State)
	return manager, ok
}

// disconnectPaneFromCurrentTerminal 保证 pane 改连新 terminal 时，旧 terminal 的连接快照会同步清理。
// 否则 owner/follower 关系会在旧 terminal 上留下脏引用，后续控制权判断会失真。
func disconnectPaneFromCurrentTerminal(state *types.AppState, paneID types.PaneID, nextTerminalID types.TerminalID) {
	currentTerminalID := findPaneTerminalID(state, paneID)
	if currentTerminalID == "" || currentTerminalID == nextTerminalID {
		return
	}
	snapshot := state.Domain.Connections[currentTerminalID]
	conn := connection.FromSnapshot(snapshot)
	conn.Disconnect(paneID)
	next := conn.Snapshot()
	if len(next.ConnectedPaneIDs) == 0 {
		delete(state.Domain.Connections, currentTerminalID)
		return
	}
	state.Domain.Connections[currentTerminalID] = next
}

func findPaneTerminalID(state *types.AppState, paneID types.PaneID) types.TerminalID {
	for _, workspace := range state.Domain.Workspaces {
		for _, tab := range workspace.Tabs {
			if pane, ok := tab.Panes[paneID]; ok {
				return pane.TerminalID
			}
		}
	}
	return ""
}

func promptState(state *types.AppState) (*promptdomain.State, bool) {
	if state.UI.Overlay.Kind != types.OverlayPrompt || state.UI.Overlay.Data == nil {
		return nil, false
	}
	prompt, ok := state.UI.Overlay.Data.(*promptdomain.State)
	return prompt, ok
}

func promptKindFromString(kind string) promptdomain.Kind {
	switch kind {
	case PromptKindEditTerminalMetadata:
		return promptdomain.KindEditTerminalMetadata
	default:
		return promptdomain.KindCreateWorkspace
	}
}

func promptTitle(kind string) string {
	switch kind {
	case PromptKindEditTerminalMetadata:
		return "edit terminal metadata"
	default:
		return "create workspace"
	}
}

func buildPromptState(state *types.AppState, in intent.OpenPromptIntent) *promptdomain.State {
	out := &promptdomain.State{
		Kind:       promptKindFromString(in.PromptKind),
		Title:      promptTitle(in.PromptKind),
		TerminalID: in.TerminalID,
		Draft:      promptDraft(state, in),
	}
	if out.Kind == promptdomain.KindEditTerminalMetadata {
		out.Fields = promptFields(state, in)
	}
	return out
}

func promptDraft(state *types.AppState, in intent.OpenPromptIntent) string {
	switch in.PromptKind {
	case PromptKindEditTerminalMetadata:
		terminal, ok := state.Domain.Terminals[in.TerminalID]
		if !ok {
			return ""
		}
		return formatTerminalMetadataDraft(terminal)
	default:
		return ""
	}
}

func promptFields(state *types.AppState, in intent.OpenPromptIntent) []promptdomain.Field {
	switch in.PromptKind {
	case PromptKindEditTerminalMetadata:
		terminal, ok := state.Domain.Terminals[in.TerminalID]
		if !ok {
			return nil
		}
		return []promptdomain.Field{
			{Key: "name", Label: "Name", Value: terminal.Name},
			{Key: "tags", Label: "Tags", Value: formatTerminalTags(terminal.Tags)},
		}
	default:
		return nil
	}
}

func nextWorkspaceID(state *types.AppState, name string) types.WorkspaceID {
	base := sanitizeID(name)
	if base == "" {
		base = "workspace"
	}
	candidate := types.WorkspaceID("ws-" + base)
	if _, exists := state.Domain.Workspaces[candidate]; !exists {
		return candidate
	}
	for index := 2; ; index++ {
		candidate = types.WorkspaceID(fmt.Sprintf("ws-%s-%d", base, index))
		if _, exists := state.Domain.Workspaces[candidate]; !exists {
			return candidate
		}
	}
}

func sanitizeID(in string) string {
	in = strings.TrimSpace(strings.ToLower(in))
	var out []rune
	lastDash := false
	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_':
			if len(out) == 0 || lastDash {
				continue
			}
			out = append(out, '-')
			lastDash = true
		}
	}
	if len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}

func parseTerminalMetadataPrompt(value string, current types.TerminalRef) (string, map[string]string) {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	name := strings.TrimSpace(lines[0])
	if name == "" {
		name = current.Name
	}
	tags := make(map[string]string)
	if len(lines) < 2 {
		if current.Tags != nil {
			return name, cloneStringMap(current.Tags)
		}
		return name, tags
	}
	for _, item := range strings.Split(lines[1], ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		key := strings.TrimSpace(parts[0])
		if key == "" {
			continue
		}
		val := ""
		if len(parts) == 2 {
			val = strings.TrimSpace(parts[1])
		}
		tags[key] = val
	}
	return name, tags
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func formatTerminalMetadataDraft(terminal types.TerminalRef) string {
	name := terminal.Name
	if name == "" {
		name = string(terminal.ID)
	}
	tagKeys := make([]string, 0, len(terminal.Tags))
	for key := range terminal.Tags {
		tagKeys = append(tagKeys, key)
	}
	slices.Sort(tagKeys)
	pairs := make([]string, 0, len(tagKeys))
	for _, key := range tagKeys {
		pairs = append(pairs, key+"="+terminal.Tags[key])
	}
	if len(pairs) == 0 {
		return name
	}
	return name + "\n" + strings.Join(pairs, ",")
}

func formatTerminalTags(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+tags[key])
	}
	return strings.Join(pairs, ",")
}

func promptValue(promptState *promptdomain.State, explicit string) string {
	value := strings.TrimSpace(explicit)
	if value != "" {
		return value
	}
	if len(promptState.Fields) > 0 {
		switch promptState.Kind {
		case promptdomain.KindEditTerminalMetadata:
			return strings.TrimSpace(promptState.Fields[0].Value) + "\n" + strings.TrimSpace(promptState.Fields[1].Value)
		}
	}
	return strings.TrimSpace(promptState.Draft)
}

func defaultCreateTerminalCommand() []string {
	return []string{"sh", "-l"}
}

func defaultCreateTerminalName(focus types.FocusState) string {
	return strings.Join([]string{
		string(focus.WorkspaceID),
		string(focus.TabID),
		string(focus.PaneID),
	}, "-")
}
