package app

import (
	"context"

	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type ShellSetPanelPresentationMsg struct {
	Presentation state.PanelPresentation
}

func (ShellSetPanelPresentationMsg) isMsg() {}

type ShellTogglePanelPresentationMsg struct{}

func (ShellTogglePanelPresentationMsg) isMsg() {}

type ShellSetHeaderVisibleMsg struct {
	Visible bool
}

func (ShellSetHeaderVisibleMsg) isMsg() {}

type ShellToggleHeaderVisibleMsg struct{}

func (ShellToggleHeaderVisibleMsg) isMsg() {}

type ShellSetFooterVisibleMsg struct {
	Visible bool
}

func (ShellSetFooterVisibleMsg) isMsg() {}

type ShellToggleFooterVisibleMsg struct{}

func (ShellToggleFooterVisibleMsg) isMsg() {}

type ShellAddToastMsg struct {
	Toast state.ToastSpec
}

func (ShellAddToastMsg) isMsg() {}

type ShellTickToastsMsg struct {
	Ticks uint64
}

func (ShellTickToastsMsg) isMsg() {}

type ShellCloseCurrentToastMsg struct{}

func (ShellCloseCurrentToastMsg) isMsg() {}

type ShellClearToastsMsg struct{}

func (ShellClearToastsMsg) isMsg() {}

type ShellOpenTerminalPickerMsg struct{}

func (ShellOpenTerminalPickerMsg) isMsg() {}

type ShellOpenTerminalPoolMsg struct{}

func (ShellOpenTerminalPoolMsg) isMsg() {}

type ShellOpenWorkbenchTreeMsg struct{}

func (ShellOpenWorkbenchTreeMsg) isMsg() {}

type ShellOpenPromptMsg struct {
	Prompt state.PromptState
}

func (ShellOpenPromptMsg) isMsg() {}

type ShellOpenHelpMsg struct {
	Section string
}

func (ShellOpenHelpMsg) isMsg() {}

type ShellCloseOverlayMsg struct{}

func (ShellCloseOverlayMsg) isMsg() {}

type ShellPromptSetValueMsg struct {
	Value string
}

func (ShellPromptSetValueMsg) isMsg() {}

type ShellPromptSubmitMsg struct{}

func (ShellPromptSubmitMsg) isMsg() {}

type ShellPromptCancelMsg struct{}

func (ShellPromptCancelMsg) isMsg() {}

type ShellContentActionMsg struct {
	ActionID string
	PaneID   string
	Row      int
}

func (ShellContentActionMsg) isMsg() {}

type HostThemeMsg struct {
	Update state.HostThemeUpdate
}

func (HostThemeMsg) isMsg() {}

type ShellSplitActivePaneMsg struct {
	Pane      state.PaneState
	Direction state.SplitDirection
}

func (ShellSplitActivePaneMsg) isMsg() {}

type ShellPaneCommandMsg struct {
	Command state.PaneCommand
}

func (ShellPaneCommandMsg) isMsg() {}

type ShellFloatingCommandMsg struct {
	Command state.FloatingCommand
}

func (ShellFloatingCommandMsg) isMsg() {}

type ShellWorkbenchCommandMsg struct {
	Command state.WorkbenchCommand
}

func (ShellWorkbenchCommandMsg) isMsg() {}

type PaneCommandFeedbackEffect struct {
	Result  state.PaneCommandResult
	Command state.PaneCommand
}

func (PaneCommandFeedbackEffect) isEffect() {}

type PaneTerminalKillEffect struct {
	TerminalID string
	PaneID     string
	Command    state.PaneCommand
}

func (PaneTerminalKillEffect) isEffect() {}

func NewShellReducer() Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		switch msg := msg.(type) {
		case ShellSetPanelPresentationMsg:
			return reducePaneCommand(root, state.PaneCommand{
				Action:       state.PaneCommandSetPresentation,
				Presentation: msg.Presentation,
				Source:       state.PaneCommandSourceTest,
			})
		case ShellTogglePanelPresentationMsg:
			return reducePaneCommand(root, state.PaneCommand{
				Action: state.PaneCommandTogglePresentation,
				Source: state.PaneCommandSourceTest,
			})
		case ShellSetHeaderVisibleMsg:
			root.Shell = root.Shell.SetHeaderVisible(msg.Visible)
		case ShellToggleHeaderVisibleMsg:
			root.Shell = root.Shell.ToggleHeaderVisible()
		case ShellSetFooterVisibleMsg:
			root.Shell = root.Shell.SetFooterVisible(msg.Visible)
		case ShellToggleFooterVisibleMsg:
			root.Shell = root.Shell.ToggleFooterVisible()
		case ShellAddToastMsg:
			root.Shell = root.Shell.AddToast(msg.Toast)
		case ShellTickToastsMsg:
			root.Shell = root.Shell.TickToasts(msg.Ticks)
		case TickMsg:
			if msg.Token != "" && msg.Token != toastTickToken {
				return root, nil
			}
			if len(root.Shell.Toasts) == 0 {
				return root, nil
			}
			ticks := msg.Ticks
			if ticks == 0 {
				ticks = 1
			}
			root.Shell = root.Shell.TickToasts(ticks)
		case ShellCloseCurrentToastMsg:
			root.Shell = root.Shell.CloseCurrentToast()
		case ShellClearToastsMsg:
			root.Shell = root.Shell.ClearToasts()
		case ShellOpenTerminalPickerMsg:
			root.Shell = root.Shell.OpenTerminalPicker()
			return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
		case ShellOpenTerminalPoolMsg:
			root.Shell = root.Shell.OpenTerminalPool()
			return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
		case ShellOpenWorkbenchTreeMsg:
			root.Shell = root.Shell.OpenWorkbenchTree()
		case ShellOpenPromptMsg:
			root.Shell = root.Shell.OpenPrompt(msg.Prompt)
		case ShellOpenHelpMsg:
			root.Shell = root.Shell.OpenHelp(msg.Section)
		case ShellCloseOverlayMsg:
			root.Shell = root.Shell.CloseOverlay()
		case ShellPromptSetValueMsg:
			root.Shell = root.Shell.SetPromptValue(msg.Value)
		case ShellPromptSubmitMsg:
			return reducePromptSubmit(root)
		case ShellPromptCancelMsg:
			root.Shell = root.Shell.CancelPrompt().CloseOverlay()
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "prompt.cancel", Body: "canceled"})
		case ShellContentActionMsg:
			return reduceShellContentAction(root, msg)
		case HostThemeMsg:
			root.HostTheme = root.HostTheme.ApplyUpdate(msg.Update)
		case ShellSplitActivePaneMsg:
			return reducePaneCommand(root, state.PaneCommand{
				Action:         state.PaneCommandSplit,
				SplitDirection: msg.Direction,
				NewPane:        msg.Pane,
				Source:         state.PaneCommandSourceTest,
			})
		case ShellPaneCommandMsg:
			return reducePaneCommand(root, msg.Command)
		case ShellFloatingCommandMsg:
			return reduceFloatingCommand(root, msg.Command)
		case ShellWorkbenchCommandMsg:
			return reduceWorkbenchCommand(root, msg.Command)
		default:
			return root, nil
		}
		return root.Advance(), nil
	}
}

func reduceShellContentAction(root state.Root, msg ShellContentActionMsg) (state.Root, []Effect) {
	switch msg.ActionID {
	case render.ActionTabClose.String():
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandTabClose, Source: state.PaneCommandSourceMouse})
	case render.ActionTabCreate.String():
		shell := root.Shell.EnsureDefaults()
		return reduceWorkbenchCommand(root, state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: nextTabName(shell), Source: state.PaneCommandSourceMouse})
	case render.ActionEmptyClose.String(), render.ActionExitedClose.String():
		return reducePaneCommand(root, state.PaneCommand{
			Action: state.PaneCommandClose,
			Target: state.PaneCommandTarget{PaneID: msg.PaneID},
			Source: state.PaneCommandSourceMouse,
		})
	case render.ActionPickerAttach.String():
		items := state.TerminalPickerItems(root)
		if msg.Row >= 0 {
			root.Shell = root.Shell.SetTerminalPickerSelectedIndex(msg.Row, len(items))
		}
		selected, ok := terminalPickerItemAt(items, msg.Row)
		if !ok && msg.PaneID != "" {
			selected = state.TerminalPickerItem{PaneID: msg.PaneID}
			ok = true
		}
		if ok && selected.PaneID != "" {
			root.Shell = root.Shell.FocusPane(state.PaneCommandTarget{PaneID: selected.PaneID})
			root.Shell = root.Shell.CloseOverlay()
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "picker.attach", Body: selected.PaneID})
			return root.Advance(), nil
		}
		if ok && selected.TerminalID != "" {
			return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
				return TerminalPoolAttachRequestMsg{TerminalID: selected.TerminalID}
			}}}
		}
	case render.ActionPickerNew.String():
		return root, []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolCreateRequestMsg{} }}}
	case render.ActionPoolSelect.String():
		items := state.TerminalPoolPageItems(root)
		root.Shell = root.Shell.SetTerminalPoolSelectedIndex(msg.Row, len(items))
		return root.Advance(), nil
	case render.ActionPoolAttach.String():
		if selected, ok := terminalPoolPageItemForAction(root, msg.Row); ok {
			return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
				return TerminalPoolAttachRequestMsg{TerminalID: selected.TerminalID}
			}}}
		}
	case render.ActionPoolKill.String():
		if selected, ok := terminalPoolPageItemForAction(root, msg.Row); ok {
			return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
				return TerminalPoolKillRequestMsg{TerminalID: selected.TerminalID}
			}}}
		}
	case render.ActionPoolEdit.String():
		if selected, ok := terminalPoolPageItemForAction(root, msg.Row); ok {
			tags := cloneStringMap(selected.Tags)
			if tags == nil {
				tags = map[string]string{}
			}
			tags["edited-by"] = "termx-tui-v3"
			return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
				return TerminalPoolEditRequestMsg{TerminalID: selected.TerminalID, Title: selected.Title, Tags: tags}
			}}}
		}
	case render.ActionWorkbenchSelect.String():
		items := state.WorkbenchTreeItems(root)
		root.Shell = root.Shell.SetWorkbenchTreeSelectedIndex(msg.Row, len(items))
		return root.Advance(), nil
	case render.ActionWorkbenchOpen.String():
		items := state.WorkbenchTreeItems(root)
		if msg.Row >= 0 {
			root.Shell = root.Shell.SetWorkbenchTreeSelectedIndex(msg.Row, len(items))
			items = state.WorkbenchTreeItems(root)
		}
		return reduceWorkbenchTreeOpen(root, items)
	case render.ActionPromptSubmit.String():
		return reducePromptSubmit(root)
	case render.ActionPromptCancel.String():
		root.Shell = root.Shell.CloseOverlay()
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "prompt.cancel", Body: "canceled"})
		return root.Advance(), nil
	case render.ActionHelpClose.String():
		root.Shell = root.Shell.CloseOverlay()
		return root.Advance(), nil
	case render.ActionFloatingRaise.String():
		return reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandFocusRaise, TargetID: msg.PaneID, Source: state.PaneCommandSourceMouse})
	case render.ActionFloatingClose.String():
		return reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandClose, TargetID: msg.PaneID, Source: state.PaneCommandSourceMouse})
	case render.ActionFloatingResize.String():
		return reduceFloatingCommand(root, state.FloatingCommand{Action: state.FloatingCommandResize, TargetID: msg.PaneID, DeltaW: 2, DeltaH: 1, Source: state.PaneCommandSourceMouse})
	case render.ActionExitedRestart.String():
		return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
			return TerminalPoolRestartRequestMsg{TerminalID: terminalIDForContentAction(root, msg.PaneID)}
		}}}
	case render.ActionExitedReconnect.String():
		return root, []Effect{FuncEffect{Run: func(context.Context) Msg {
			return TerminalPoolReconnectRequestMsg{TerminalID: terminalIDForContentAction(root, msg.PaneID)}
		}}}
	case render.ActionEmptyManager.String():
		root.Shell = root.Shell.OpenTerminalPool()
		return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
	case render.ActionEmptyAttach.String(), render.ActionEmptyCreate.String():
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: msg.ActionID, Body: "not implemented"})
		return root.Advance(), nil
	}
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "content action", Body: "unknown " + msg.ActionID})
	return root.Advance(), nil
}

func reducePromptSubmit(root state.Root) (state.Root, []Effect) {
	shell := root.Shell.EnsureDefaults()
	if shell.Overlay.Kind != state.OverlayPrompt || !shell.Overlay.Open {
		return root, nil
	}
	before := shell.Overlay.Prompt
	shell = shell.SubmitPrompt()
	after := shell.Overlay.Prompt
	root.Shell = shell
	if after.Submitted {
		root.Shell = root.Shell.CloseOverlay()
		body := after.LastResult
		if body == "" {
			body = "(empty)"
		}
		if command, ok := promptWorkbenchCommand(after); ok {
			return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg {
				return ShellWorkbenchCommandMsg{Command: command}
			}}}
		}
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "prompt.submit", Body: body})
		return root.Advance(), nil
	}
	if after.LastResult != "" && after.LastResult != before.LastResult {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "prompt.confirm", Body: after.LastResult})
	}
	return root.Advance(), nil
}

func promptWorkbenchCommand(prompt state.PromptState) (state.WorkbenchCommand, bool) {
	name := prompt.LastResult
	if name == "" {
		name = prompt.Value
	}
	switch prompt.Purpose {
	case "tab.rename":
		return state.WorkbenchCommand{Action: state.WorkbenchCommandTabRename, Name: name, Source: state.PaneCommandSourcePalette}, true
	case "workspace.rename":
		return state.WorkbenchCommand{Action: state.WorkbenchCommandWorkspaceRename, Name: name, Source: state.PaneCommandSourcePalette}, true
	default:
		return state.WorkbenchCommand{}, false
	}
}

func terminalPickerItemAt(items []state.TerminalPickerItem, row int) (state.TerminalPickerItem, bool) {
	if row >= 0 && row < len(items) {
		return items[row], true
	}
	for _, item := range items {
		if item.Selected {
			return item, true
		}
	}
	return state.TerminalPickerItem{}, false
}

func terminalPoolPageItemForAction(root state.Root, row int) (state.TerminalPoolPageItem, bool) {
	items := state.TerminalPoolPageItems(root)
	if row >= 0 {
		root.Shell = root.Shell.SetTerminalPoolSelectedIndex(row, len(items))
		items = state.TerminalPoolPageItems(root)
	}
	if row >= 0 && row < len(items) {
		return items[row], true
	}
	for _, item := range items {
		if item.Selected {
			return item, true
		}
	}
	return state.TerminalPoolPageItem{}, false
}

func reduceWorkbenchTreeOpen(root state.Root, items []state.WorkbenchTreeItem) (state.Root, []Effect) {
	selected, ok := workbenchTreeSelectedItem(items)
	if !ok {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.open", Body: "no node"})
		return root.Advance(), nil
	}
	switch selected.Kind {
	case state.WorkbenchTreeKindPane:
		root.Shell = root.Shell.FocusPane(state.PaneCommandTarget{WorkspaceID: selected.WorkspaceID, TabID: selected.TabID, PaneID: selected.PaneID})
		root.Shell = root.Shell.CloseOverlay()
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "workbench.open", Body: selected.PaneID})
	case state.WorkbenchTreeKindTab:
		targetPaneID := selected.PaneID
		if targetPaneID == "" {
			targetPaneID = firstPaneIDForTab(root.Shell, selected.TabID)
		}
		if targetPaneID != "" {
			root.Shell = root.Shell.FocusPane(state.PaneCommandTarget{WorkspaceID: selected.WorkspaceID, TabID: selected.TabID, PaneID: targetPaneID})
			root.Shell = root.Shell.CloseOverlay()
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "workbench.open", Body: selected.TabID})
		} else {
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.open", Body: "tab has no pane"})
		}
	case state.WorkbenchTreeKindWorkspace:
		root.Shell = root.Shell.CloseOverlay()
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "workbench.open", Body: selected.WorkspaceName})
	case state.WorkbenchTreeKindFloating:
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "floating", Body: "not implemented"})
	default:
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.open", Body: "unknown node"})
	}
	return root.Advance(), nil
}

func workbenchTreeSelectedItem(items []state.WorkbenchTreeItem) (state.WorkbenchTreeItem, bool) {
	if len(items) == 0 {
		return state.WorkbenchTreeItem{}, false
	}
	for _, item := range items {
		if item.Selected {
			return item, true
		}
	}
	return items[0], true
}

func firstPaneIDForTab(shell state.ShellStore, tabID string) string {
	shell = shell.EnsureDefaults()
	for _, tab := range shell.Workspace.Tabs {
		if tab.ID == tabID && len(tab.Panes) > 0 {
			return tab.Panes[0].ID
		}
	}
	return ""
}

func terminalIDForContentAction(root state.Root, paneID string) string {
	if pane, ok := root.Shell.Pane(state.PaneCommandTarget{PaneID: paneID}); ok {
		return pane.TerminalID
	}
	return ""
}

func reducePaneCommand(root state.Root, command state.PaneCommand) (state.Root, []Effect) {
	command = command.WithDefaults(root.Shell)
	targetPane, hasTargetPane := root.Shell.Pane(command.Target)
	nextShell, result := root.Shell.ApplyPaneCommand(command)
	if result.Status == state.PaneCommandOK {
		root.Shell = deactivateFloatingAfterPaneCommand(nextShell, command)
		root.Shell = addPaneCommandToast(root.Shell, command, result)
		effects := paneCommandEffects(command, result, targetPane, hasTargetPane)
		return root.Advance(), effects
	}
	root.Shell = addPaneCommandToast(root.Shell, command, result)
	return root.Advance(), []Effect{PaneCommandFeedbackEffect{Result: result, Command: command}}
}

func reduceFloatingCommand(root state.Root, command state.FloatingCommand) (state.Root, []Effect) {
	command = withFloatingCommandDefaults(root, command)
	nextShell, result := root.Shell.ApplyFloatingCommand(command)
	root.Shell = addFloatingCommandToast(nextShell, command, result)
	return root.Advance(), nil
}

func reduceWorkbenchCommand(root state.Root, command state.WorkbenchCommand) (state.Root, []Effect) {
	nextShell, result := root.Shell.ApplyWorkbenchCommand(command)
	root.Shell = addWorkbenchCommandToast(nextShell, result)
	return root.Advance(), nil
}

func withFloatingCommandDefaults(root state.Root, command state.FloatingCommand) state.FloatingCommand {
	if command.BoundsW <= 0 {
		command.BoundsW = root.Viewport.Cols
	}
	if command.BoundsH <= 0 {
		command.BoundsH = root.Viewport.Rows
	}
	if command.BoundsW <= 0 {
		command.BoundsW = 80
	}
	if command.BoundsH <= 0 {
		command.BoundsH = 24
	}
	if command.Action == state.FloatingCommandCreate && command.Pane.ID == "" {
		command.Pane = state.PaneState{ID: "floating-pane", Title: "floating", Kind: state.PaneEmpty}
	}
	return command
}

func addFloatingCommandToast(shell state.ShellStore, command state.FloatingCommand, result state.FloatingCommandResult) state.ShellStore {
	if result.Status == state.FloatingCommandOK {
		if shouldSuppressFloatingCommandSuccessToast(command) {
			return shell
		}
		return shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: string(result.Action), Body: result.ID})
	}
	return shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: string(result.Action), Body: result.Reason})
}

func shouldSuppressFloatingCommandSuccessToast(command state.FloatingCommand) bool {
	switch command.Action {
	case state.FloatingCommandFocusRaise, state.FloatingCommandDeactivate, state.FloatingCommandMove, state.FloatingCommandResize:
		return true
	default:
		return false
	}
}

func deactivateFloatingAfterPaneCommand(shell state.ShellStore, command state.PaneCommand) state.ShellStore {
	switch command.Action {
	case state.PaneCommandFocus, state.PaneCommandSplit, state.PaneCommandClose, state.PaneCommandZoom, state.PaneCommandUnzoom, state.PaneCommandToggleZoom:
		next, _ := shell.ApplyFloatingCommand(state.FloatingCommand{Action: state.FloatingCommandDeactivate, Source: command.Source})
		return next
	default:
		return shell
	}
}

func addWorkbenchCommandToast(shell state.ShellStore, result state.WorkbenchCommandResult) state.ShellStore {
	if result.Status == state.WorkbenchCommandOK {
		return shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: string(result.Action), Body: result.ID})
	}
	return shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: string(result.Action), Body: result.Reason})
}

func paneCommandEffects(command state.PaneCommand, result state.PaneCommandResult, targetPane state.PaneState, hasTargetPane bool) []Effect {
	effects := []Effect{PaneCommandFeedbackEffect{Result: result, Command: command}}
	if command.Action != state.PaneCommandCloseAndKill {
		return effects
	}
	if hasTargetPane && targetPane.TerminalID != "" {
		effects = append(effects, PaneTerminalKillEffect{TerminalID: targetPane.TerminalID, PaneID: targetPane.ID, Command: command})
	}
	return effects
}

func addPaneCommandToast(shell state.ShellStore, command state.PaneCommand, result state.PaneCommandResult) state.ShellStore {
	switch result.Status {
	case state.PaneCommandOK:
		if shouldSuppressPaneCommandSuccessToast(command) {
			return shell
		}
		return shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: string(command.Action)})
	case state.PaneCommandNeedsConfirmation:
		return shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: string(command.Action), Body: result.Reason, Pending: true})
	case state.PaneCommandInvalid:
		return shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: string(command.Action), Body: result.Reason})
	default:
		return shell
	}
}

func shouldSuppressPaneCommandSuccessToast(command state.PaneCommand) bool {
	switch command.Action {
	case state.PaneCommandFocus, state.PaneCommandFocusNext, state.PaneCommandFocusPrevious, state.PaneCommandResize:
		return true
	default:
		return false
	}
}
