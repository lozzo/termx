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

func reducePaneCommand(root state.Root, command state.PaneCommand) (state.Root, []Effect) {
	command = command.WithDefaults(root.Shell)
	nextShell, result := root.Shell.ApplyPaneCommand(command)
	if result.Status == state.PaneCommandOK {
		root.Shell = nextShell
		effects := paneCommandEffects(root, command, result)
		return root.Advance(), effects
	}
	return root, []Effect{PaneCommandFeedbackEffect{Result: result, Command: command}}
}

func paneCommandEffects(root state.Root, command state.PaneCommand, result state.PaneCommandResult) []Effect {
	effects := []Effect{PaneCommandFeedbackEffect{Result: result, Command: command}}
	if command.Action != state.PaneCommandCloseAndKill {
		return effects
	}
	pane, ok := root.Shell.Pane(command.Target)
	if ok && pane.TerminalID != "" {
		effects = append(effects, PaneTerminalKillEffect{TerminalID: pane.TerminalID, PaneID: pane.ID, Command: command})
	}
	return effects
}
