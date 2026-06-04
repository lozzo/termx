package app

import "github.com/lozzow/termx/termx-tui-v3/state"

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

type ShellCloseOverlayMsg struct{}

func (ShellCloseOverlayMsg) isMsg() {}

type ShellContentActionMsg struct {
	ActionID string
	PaneID   string
	Row      int
}

func (ShellContentActionMsg) isMsg() {}

type ShellSplitActivePaneMsg struct {
	Pane      state.PaneState
	Direction state.SplitDirection
}

func (ShellSplitActivePaneMsg) isMsg() {}

type ShellPaneCommandMsg struct {
	Command state.PaneCommand
}

func (ShellPaneCommandMsg) isMsg() {}

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
		case ShellCloseCurrentToastMsg:
			root.Shell = root.Shell.CloseCurrentToast()
		case ShellClearToastsMsg:
			root.Shell = root.Shell.ClearToasts()
		case ShellOpenTerminalPickerMsg:
			root.Shell = root.Shell.OpenTerminalPicker()
		case ShellCloseOverlayMsg:
			root.Shell = root.Shell.CloseOverlay()
		case ShellContentActionMsg:
			return reduceShellContentAction(root, msg)
		case ShellSplitActivePaneMsg:
			return reducePaneCommand(root, state.PaneCommand{
				Action:         state.PaneCommandSplit,
				SplitDirection: msg.Direction,
				NewPane:        msg.Pane,
				Source:         state.PaneCommandSourceTest,
			})
		case ShellPaneCommandMsg:
			return reducePaneCommand(root, msg.Command)
		default:
			return root, nil
		}
		return root.Advance(), nil
	}
}

func reduceShellContentAction(root state.Root, msg ShellContentActionMsg) (state.Root, []Effect) {
	switch msg.ActionID {
	case "empty.close", "exited.close":
		return reducePaneCommand(root, state.PaneCommand{
			Action: state.PaneCommandClose,
			Target: state.PaneCommandTarget{PaneID: msg.PaneID},
			Source: state.PaneCommandSourceMouse,
		})
	case "picker.attach":
		if msg.PaneID != "" {
			items := state.TerminalPickerItems(root)
			if msg.Row >= 0 {
				root.Shell = root.Shell.SetTerminalPickerSelectedIndex(msg.Row, len(items))
			}
			root.Shell = root.Shell.FocusPane(state.PaneCommandTarget{PaneID: msg.PaneID})
			root.Shell = root.Shell.CloseOverlay()
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "picker.attach", Body: msg.PaneID})
			return root.Advance(), nil
		}
	case "empty.attach", "empty.create", "empty.manager", "exited.restart", "exited.reconnect", "picker.new":
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: msg.ActionID, Body: "not implemented"})
		return root.Advance(), nil
	}
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "content action", Body: "unknown " + msg.ActionID})
	return root.Advance(), nil
}

func reducePaneCommand(root state.Root, command state.PaneCommand) (state.Root, []Effect) {
	command = command.WithDefaults(root.Shell)
	targetPane, hasTargetPane := root.Shell.Pane(command.Target)
	nextShell, result := root.Shell.ApplyPaneCommand(command)
	if result.Status == state.PaneCommandOK {
		root.Shell = nextShell
		root.Shell = addPaneCommandToast(root.Shell, command, result)
		effects := paneCommandEffects(command, result, targetPane, hasTargetPane)
		return root.Advance(), effects
	}
	root.Shell = addPaneCommandToast(root.Shell, command, result)
	return root.Advance(), []Effect{PaneCommandFeedbackEffect{Result: result, Command: command}}
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
		return shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: string(command.Action)})
	case state.PaneCommandNeedsConfirmation:
		return shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: string(command.Action), Body: result.Reason, Pending: true})
	case state.PaneCommandInvalid:
		return shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: string(command.Action), Body: result.Reason})
	default:
		return shell
	}
}
