package app

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type ShellSetInteractionModeMsg struct {
	Mode state.InteractionMode
}

func (ShellSetInteractionModeMsg) isMsg() {}

type ShellExitInteractionModeMsg struct{}

func (ShellExitInteractionModeMsg) isMsg() {}

func NewUIInputReducer() Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		inputMsg, ok := msg.(InputMsg)
		if !ok {
			return root, nil
		}
		shell := root.Shell.EnsureDefaults()
		if inputMsg.Event.Kind == input.EventKindKey && inputMsg.Event.Key == input.KeyEsc && shell.Overlay.Open {
			root.Shell = shell.CloseOverlay()
			return root.Advance(), []Effect{handledEffect{}}
		}
		intent := input.RouteWithMode(inputMsg.Event, root.CopyMode.Active, inputMode(root.Shell.EnsureDefaults().InteractionMode))
		switch intent.Kind {
		case input.IntentOpenTerminalPicker:
			root.Shell = root.Shell.OpenTerminalPicker()
			return root.Advance(), []Effect{handledEffect{}}
		case input.IntentSetInteractionMode:
			root.Shell = root.Shell.SetInteractionMode(stateInteractionMode(intent.Mode))
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: string(root.Shell.InteractionMode) + " mode"})
			return root.Advance(), []Effect{handledEffect{}}
		case input.IntentExitInteraction:
			root.Shell = root.Shell.ExitInteractionMode()
			return root.Advance(), []Effect{handledEffect{}}
		case input.IntentShellAction:
			return reduceShellActionIntent(root, intent)
		case input.IntentPaneCommand:
			return reducePaneCommandIntent(root, intent)
		default:
			if root.Shell.EnsureDefaults().InteractionMode != state.InteractionModeNormal {
				return root, []Effect{handledEffect{}}
			}
			return root, nil
		}
	}
}

func reducePaneCommandIntent(root state.Root, intent input.Intent) (state.Root, []Effect) {
	command, ok := PaneCommandFromIntent(intent)
	if !ok {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "pane command", Body: "invalid shortcut"})
		return root.Advance(), []Effect{handledEffect{}}
	}
	if command.Action == state.PaneCommandSplit && command.NewPane.ID == "" {
		command.NewPane = state.PaneState{ID: nextKeyboardPaneID(root.Shell), Title: "pane", Kind: state.PaneEmpty}
	}
	return root, []Effect{
		handledEffect{},
		FuncEffect{Run: func(context.Context) Msg {
			return ShellPaneCommandMsg{Command: command}
		}},
	}
}

func reduceShellActionIntent(root state.Root, intent input.Intent) (state.Root, []Effect) {
	var msg Msg
	switch intent.Action {
	case input.ShellActionToggleHeader:
		msg = ShellToggleHeaderVisibleMsg{}
	case input.ShellActionToggleFooter:
		msg = ShellToggleFooterVisibleMsg{}
	case input.ShellActionClearToasts:
		msg = ShellClearToastsMsg{}
	case input.ShellActionCloseToast:
		msg = ShellCloseCurrentToastMsg{}
	case input.ShellActionFloatingStub:
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "floating", Body: "not implemented"})
		return root.Advance(), []Effect{handledEffect{}}
	default:
		return root, []Effect{handledEffect{}}
	}
	return root, []Effect{
		handledEffect{},
		FuncEffect{Run: func(context.Context) Msg {
			return msg
		}},
	}
}

func inputMode(mode state.InteractionMode) input.InteractionMode {
	switch mode {
	case state.InteractionModePane:
		return input.InteractionModePane
	case state.InteractionModeResize:
		return input.InteractionModeResize
	case state.InteractionModeGlobal:
		return input.InteractionModeGlobal
	case state.InteractionModeFloating:
		return input.InteractionModeFloating
	default:
		return input.InteractionModeNormal
	}
}

func stateInteractionMode(mode input.InteractionMode) state.InteractionMode {
	switch mode {
	case input.InteractionModePane:
		return state.InteractionModePane
	case input.InteractionModeResize:
		return state.InteractionModeResize
	case input.InteractionModeGlobal:
		return state.InteractionModeGlobal
	case input.InteractionModeFloating:
		return state.InteractionModeFloating
	default:
		return state.InteractionModeNormal
	}
}

func nextKeyboardPaneID(shell state.ShellStore) string {
	shell = shell.EnsureDefaults()
	for i := 2; ; i++ {
		id := fmt.Sprintf("pane-%d", i)
		if !shell.HasPane(state.PaneCommandTarget{PaneID: id}) {
			return id
		}
	}
}
