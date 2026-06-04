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
		if shell.Overlay.Open && shell.Overlay.Kind == state.OverlayTerminalPicker {
			return reduceTerminalPickerInput(root, inputMsg.Event)
		}
		if shell.Overlay.Open && shell.Overlay.Kind == state.OverlayTerminalPool {
			return reduceTerminalPoolPageInput(root, inputMsg.Event)
		}
		if shell.Overlay.Open && shell.Overlay.Kind == state.OverlayWorkbenchTree {
			return reduceWorkbenchTreeInput(root, inputMsg.Event)
		}
		intent := input.RouteWithMode(inputMsg.Event, root.CopyMode.Active, inputMode(root.Shell.EnsureDefaults().InteractionMode))
		switch intent.Kind {
		case input.IntentOpenTerminalPicker:
			root.Shell = root.Shell.OpenTerminalPicker()
			return root.Advance(), []Effect{
				handledEffect{},
				FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }},
			}
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

func reduceTerminalPickerInput(root state.Root, event input.InputEvent) (state.Root, []Effect) {
	if event.Kind != input.EventKindKey {
		return root, nil
	}
	items := state.TerminalPickerItems(root)
	switch event.Key {
	case input.KeyUp:
		root.Shell = root.Shell.MoveTerminalPickerSelection(-1, len(items))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyDown:
		root.Shell = root.Shell.MoveTerminalPickerSelection(1, len(items))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyEnter:
		return reduceTerminalPickerConfirm(root, items)
	case input.KeyChar:
		if isBackspaceEvent(event) {
			root.Shell = root.Shell.SetTerminalPickerQuery(trimLastRune(root.Shell.EnsureDefaults().Overlay.Query))
			return root.Advance(), []Effect{handledEffect{}}
		}
		if event.Ctrl || event.Char == "" {
			return root, []Effect{handledEffect{}}
		}
		root.Shell = root.Shell.SetTerminalPickerQuery(root.Shell.EnsureDefaults().Overlay.Query + event.Char)
		return root.Advance(), []Effect{handledEffect{}}
	default:
		return root, []Effect{handledEffect{}}
	}
}

func reduceTerminalPickerConfirm(root state.Root, items []state.TerminalPickerItem) (state.Root, []Effect) {
	if len(items) == 0 {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.attach", Body: "no terminal"})
		return root.Advance(), []Effect{handledEffect{}}
	}
	selected := items[0]
	for _, item := range items {
		if item.Selected {
			selected = item
			break
		}
	}
	if selected.PaneID == "" {
		if selected.TerminalID == "" {
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.attach", Body: "no terminal"})
			return root.Advance(), []Effect{handledEffect{}}
		}
		return root, []Effect{
			handledEffect{},
			FuncEffect{Run: func(context.Context) Msg {
				return TerminalPoolAttachRequestMsg{TerminalID: selected.TerminalID}
			}},
		}
	}
	root.Shell = root.Shell.FocusPane(state.PaneCommandTarget{PaneID: selected.PaneID})
	root.Shell = root.Shell.CloseOverlay()
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "picker.attach", Body: selected.PaneID})
	return root.Advance(), []Effect{handledEffect{}}
}

func reduceTerminalPoolPageInput(root state.Root, event input.InputEvent) (state.Root, []Effect) {
	if event.Kind != input.EventKindKey {
		return root, nil
	}
	items := state.TerminalPoolPageItems(root)
	switch event.Key {
	case input.KeyUp:
		root.Shell = root.Shell.MoveTerminalPoolSelection(-1, len(items))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyDown:
		root.Shell = root.Shell.MoveTerminalPoolSelection(1, len(items))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyEnter:
		return reduceTerminalPoolPageAttach(root, items)
	case input.KeyChar:
		if isBackspaceEvent(event) {
			root.Shell = root.Shell.SetTerminalPoolQuery(trimLastRune(root.Shell.EnsureDefaults().Overlay.Query))
			return root.Advance(), []Effect{handledEffect{}}
		}
		if event.Ctrl || event.Char == "" {
			return root, []Effect{handledEffect{}}
		}
		root.Shell = root.Shell.SetTerminalPoolQuery(root.Shell.EnsureDefaults().Overlay.Query + event.Char)
		return root.Advance(), []Effect{handledEffect{}}
	default:
		return root, []Effect{handledEffect{}}
	}
}

func reduceTerminalPoolPageAttach(root state.Root, items []state.TerminalPoolPageItem) (state.Root, []Effect) {
	if len(items) == 0 {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "pool.attach", Body: "no terminal"})
		return root.Advance(), []Effect{handledEffect{}}
	}
	selected := items[0]
	for _, item := range items {
		if item.Selected {
			selected = item
			break
		}
	}
	if selected.TerminalID == "" {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "pool.attach", Body: "no terminal"})
		return root.Advance(), []Effect{handledEffect{}}
	}
	return root, []Effect{
		handledEffect{},
		FuncEffect{Run: func(context.Context) Msg {
			return TerminalPoolAttachRequestMsg{TerminalID: selected.TerminalID}
		}},
	}
}

func reduceWorkbenchTreeInput(root state.Root, event input.InputEvent) (state.Root, []Effect) {
	if event.Kind != input.EventKindKey {
		return root, nil
	}
	items := state.WorkbenchTreeItems(root)
	switch event.Key {
	case input.KeyUp:
		root.Shell = root.Shell.MoveWorkbenchTreeSelection(-1, len(items))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyDown:
		root.Shell = root.Shell.MoveWorkbenchTreeSelection(1, len(items))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyEnter:
		next, effects := reduceWorkbenchTreeOpen(root, items)
		return next, append([]Effect{handledEffect{}}, effects...)
	case input.KeyChar:
		if isBackspaceEvent(event) {
			root.Shell = root.Shell.SetWorkbenchTreeQuery(trimLastRune(root.Shell.EnsureDefaults().Overlay.Query))
			return root.Advance(), []Effect{handledEffect{}}
		}
		if event.Ctrl || event.Char == "" {
			return root, []Effect{handledEffect{}}
		}
		root.Shell = root.Shell.SetWorkbenchTreeQuery(root.Shell.EnsureDefaults().Overlay.Query + event.Char)
		return root.Advance(), []Effect{handledEffect{}}
	default:
		return root, []Effect{handledEffect{}}
	}
}

func isBackspaceEvent(event input.InputEvent) bool {
	return event.Key == input.KeyChar && (event.Char == "\x7f" || event.Char == "\b")
}

func trimLastRune(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	return string(runes[:len(runes)-1])
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
	case input.ShellActionFloatingNew, input.ShellActionFloatingCtrl, input.ShellActionFloatingMove, input.ShellActionFloatingSize:
		command, ok := floatingCommandFromIntent(root, intent)
		if !ok {
			return root, []Effect{handledEffect{}}
		}
		msg = ShellFloatingCommandMsg{Command: command}
	case input.ShellActionOpenPool:
		msg = ShellOpenTerminalPoolMsg{}
	case input.ShellActionOpenTree:
		msg = ShellOpenWorkbenchTreeMsg{}
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

func floatingCommandFromIntent(root state.Root, intent input.Intent) (state.FloatingCommand, bool) {
	command := state.FloatingCommand{
		Source:  state.PaneCommandSourceKeyboard,
		BoundsW: root.Viewport.Cols,
		BoundsH: root.Viewport.Rows,
	}
	switch intent.Action {
	case input.ShellActionFloatingNew:
		command.Action = state.FloatingCommandCreate
		command.TargetID = nextFloatingID(root.Shell)
		command.Pane = state.PaneState{ID: nextFloatingPaneID(root.Shell), Title: "floating", Kind: state.PaneEmpty}
		command.Title = "floating"
		return command, true
	case input.ShellActionFloatingCtrl:
		switch intent.Reason {
		case "close":
			command.Action = state.FloatingCommandClose
		case "collapse":
			command.Action = state.FloatingCommandToggleCollapse
		case "center":
			command.Action = state.FloatingCommandCenter
		default:
			return state.FloatingCommand{}, false
		}
		return command, true
	case input.ShellActionFloatingMove:
		command.Action = state.FloatingCommandMove
		switch intent.Reason {
		case "left":
			command.DeltaX = -2
		case "right":
			command.DeltaX = 2
		case "up":
			command.DeltaY = -1
		case "down":
			command.DeltaY = 1
		default:
			return state.FloatingCommand{}, false
		}
		return command, true
	case input.ShellActionFloatingSize:
		command.Action = state.FloatingCommandResize
		switch intent.Reason {
		case "narrow":
			command.DeltaW = -2
		case "wide":
			command.DeltaW = 2
		case "short":
			command.DeltaH = -1
		case "tall":
			command.DeltaH = 1
		default:
			return state.FloatingCommand{}, false
		}
		return command, true
	default:
		return state.FloatingCommand{}, false
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

func nextFloatingPaneID(shell state.ShellStore) string {
	shell = shell.EnsureDefaults()
	for i := 1; ; i++ {
		id := fmt.Sprintf("floating-pane-%d", i)
		exists := false
		for _, floating := range shell.Floatings {
			if floating.Pane.ID == id {
				exists = true
				break
			}
		}
		if !exists {
			return id
		}
	}
}

func nextFloatingID(shell state.ShellStore) string {
	shell = shell.EnsureDefaults()
	for i := 1; ; i++ {
		id := fmt.Sprintf("floating-%d", i)
		exists := false
		for _, floating := range shell.Floatings {
			if floating.ID == id {
				exists = true
				break
			}
		}
		if !exists {
			return id
		}
	}
}
