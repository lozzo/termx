package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
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
			if shell.Overlay.Kind == state.OverlayPrompt && shell.Overlay.Prompt.SuggestionFocused {
				return reducePromptInput(root, inputMsg.Event)
			}
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
		if shell.Overlay.Open && shell.Overlay.Kind == state.OverlayFloatingOverview {
			return reduceFloatingOverviewInput(root, inputMsg.Event)
		}
		if shell.Overlay.Open && shell.Overlay.Kind == state.OverlayPrompt {
			return reducePromptInput(root, inputMsg.Event)
		}
		if shell.Overlay.Open && shell.Overlay.Kind == state.OverlayHelp {
			return reduceHelpInput(root, inputMsg.Event)
		}
		if handled, next, effects := reduceEmptyPaneCTAInput(root, inputMsg.Event); handled {
			return next, effects
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
		case input.IntentWorkbenchCommand:
			return reduceWorkbenchCommandIntent(root, intent)
		default:
			if root.Shell.EnsureDefaults().InteractionMode != state.InteractionModeNormal {
				return root, []Effect{handledEffect{}}
			}
			return root, nil
		}
	}
}

func reduceEmptyPaneCTAInput(root state.Root, event input.InputEvent) (bool, state.Root, []Effect) {
	if event.Kind != input.EventKindKey {
		return false, root, nil
	}
	shell := root.Shell.EnsureDefaults()
	if shell.InteractionMode != state.InteractionModeNormal || shell.ActiveFloatingID != "" {
		return false, root, nil
	}
	pane, ok := shell.Pane(state.PaneCommandTarget{PaneID: shell.ActivePaneID})
	if !ok || pane.Kind != state.PaneEmpty {
		return false, root, nil
	}
	switch event.Key {
	case input.KeyUp:
		root.Shell = shell.MoveEmptyPaneCTASelection(-1, render.EmptyPaneActionCount())
		return true, root.Advance(), []Effect{handledEffect{}}
	case input.KeyDown:
		root.Shell = shell.MoveEmptyPaneCTASelection(1, render.EmptyPaneActionCount())
		return true, root.Advance(), []Effect{handledEffect{}}
	case input.KeyEnter:
		actionID := render.EmptyPaneActionID(shell.EmptyPaneCTA.SelectedIndex)
		if actionID == "" {
			return true, root, []Effect{handledEffect{}}
		}
		return true, root, []Effect{
			handledEffect{},
			FuncEffect{Run: func(context.Context) Msg {
				return ShellContentActionMsg{ActionID: actionID.String(), PaneID: pane.ID}
			}},
		}
	default:
		return false, root, nil
	}
}

func reduceTerminalPickerInput(root state.Root, event input.InputEvent) (state.Root, []Effect) {
	if event.Kind != input.EventKindKey {
		return root, nil
	}
	items := state.TerminalPickerItems(root)
	if actionID, ok := terminalPickerKeyboardAction(event); ok {
		return reduceOverlayKeyboardAction(root, actionID)
	}
	switch event.Key {
	case input.KeyUp:
		root.Shell = root.Shell.MoveTerminalPickerSelection(-1, len(items))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyDown:
		root.Shell = root.Shell.MoveTerminalPickerSelection(1, len(items))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyEnter:
		return reduceTerminalPickerConfirm(root, items)
	case input.KeyBackspace, input.KeyDelete:
		root.Shell = root.Shell.SetTerminalPickerQuery(trimLastRune(root.Shell.EnsureDefaults().Overlay.Query))
		return root.Advance(), []Effect{handledEffect{}}
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
	if selected.CreateNew {
		return root, []Effect{
			handledEffect{},
			FuncEffect{Run: func(context.Context) Msg {
				return ShellOpenPromptMsg{Prompt: createTerminalPrompt(root.Shell.EnsureDefaults().ActivePaneID)}
			}},
		}
	}
	if selected.TerminalID == "" {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "picker.attach", Body: "no terminal"})
		return root.Advance(), []Effect{handledEffect{}}
	}
	if root.Shell.EnsureDefaults().ActiveFloatingID != "" {
		targetFloatingID := root.Shell.EnsureDefaults().ActiveFloatingID
		return root, []Effect{
			handledEffect{},
			FuncEffect{Run: func(context.Context) Msg {
				return TerminalPoolAttachRequestMsg{TerminalID: selected.TerminalID, TargetFloatingID: targetFloatingID}
			}},
		}
	}
	return root, []Effect{
		handledEffect{},
		FuncEffect{Run: func(context.Context) Msg {
			return TerminalPoolAttachRequestMsg{TerminalID: selected.TerminalID}
		}},
	}
}

func reduceTerminalPoolPageInput(root state.Root, event input.InputEvent) (state.Root, []Effect) {
	if event.Kind != input.EventKindKey {
		return root, nil
	}
	items := state.TerminalPoolPageItems(root)
	if actionID, ok := terminalPoolKeyboardAction(event); ok {
		return reduceOverlayKeyboardAction(root, actionID)
	}
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
	if actionID, ok := workbenchTreeKeyboardAction(event); ok {
		return reduceOverlayKeyboardAction(root, actionID)
	}
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

func reduceFloatingOverviewInput(root state.Root, event input.InputEvent) (state.Root, []Effect) {
	if event.Kind != input.EventKindKey {
		return root, nil
	}
	items := state.FloatingOverviewItems(root)
	switch event.Key {
	case input.KeyUp:
		root.Shell = root.Shell.MoveFloatingOverviewSelection(-1, len(items))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyDown:
		root.Shell = root.Shell.MoveFloatingOverviewSelection(1, len(items))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyEnter:
		return reduceFloatingOverviewOpen(root, items)
	case input.KeyChar:
		if index, ok := floatingSummonIndex(event.Char); ok {
			return root, []Effect{handledEffect{}, FuncEffect{Run: func(context.Context) Msg {
				return ShellFloatingCommandMsg{Command: state.FloatingCommand{Action: state.FloatingCommandSummon, Index: index, Source: state.PaneCommandSourceKeyboard}}
			}}}
		}
		switch event.Char {
		case "s":
			return root, []Effect{handledEffect{}, FuncEffect{Run: func(context.Context) Msg {
				return ShellContentActionMsg{ActionID: render.ActionFloatingShowAll.String(), Row: -1}
			}}}
		case "c":
			return root, []Effect{handledEffect{}, FuncEffect{Run: func(context.Context) Msg {
				return ShellContentActionMsg{ActionID: render.ActionFloatingCollapseAll.String(), Row: -1}
			}}}
		case "x":
			return root, []Effect{handledEffect{}, FuncEffect{Run: func(context.Context) Msg {
				return ShellContentActionMsg{ActionID: render.ActionFloatingClose.String(), Row: -1}
			}}}
		}
		return root, []Effect{handledEffect{}}
	default:
		return root, []Effect{handledEffect{}}
	}
}

func reduceFloatingOverviewOpen(root state.Root, items []state.FloatingOverviewItem) (state.Root, []Effect) {
	selected, ok := selectedFloatingOverviewItem(items)
	if !ok {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "floating.open", Body: "no floating"})
		return root.Advance(), []Effect{handledEffect{}}
	}
	return root, []Effect{handledEffect{}, FuncEffect{Run: func(context.Context) Msg {
		return ShellFloatingCommandMsg{Command: state.FloatingCommand{Action: state.FloatingCommandSummon, TargetID: selected.FloatingID, Source: state.PaneCommandSourceKeyboard}}
	}}}
}

func reduceOverlayKeyboardAction(root state.Root, actionID render.ActionID) (state.Root, []Effect) {
	if _, ok := render.ActionSpecByID(actionID); !ok {
		return root, []Effect{handledEffect{}}
	}
	return root, []Effect{
		handledEffect{},
		FuncEffect{Run: func(context.Context) Msg {
			return ShellContentActionMsg{ActionID: actionID.String(), Row: -1}
		}},
	}
}

func terminalPickerKeyboardAction(event input.InputEvent) (render.ActionID, bool) {
	if event.Key == input.KeyTab && !event.Ctrl && !event.Alt && !event.Shift {
		return render.ActionPickerSplit, true
	}
	if event.Key != input.KeyChar || !event.Ctrl || event.Alt || event.Shift {
		return "", false
	}
	switch event.Char {
	case "\x05", "e":
		return render.ActionPickerEdit, true
	case "\x0b", "k":
		return render.ActionPickerKill, true
	case "\x18", "x":
		return render.ActionPickerDelete, true
	default:
		return "", false
	}
}

func terminalPoolKeyboardAction(event input.InputEvent) (render.ActionID, bool) {
	if event.Key != input.KeyChar || !event.Ctrl || event.Alt || event.Shift {
		return "", false
	}
	switch event.Char {
	case "\x14", "t":
		return render.ActionPoolAttachTab, true
	case "\x0f", "o":
		return render.ActionPoolAttachFloat, true
	case "\x05", "e":
		return render.ActionPoolEdit, true
	case "\x0b", "k":
		return render.ActionPoolKill, true
	case "\x18", "x":
		return render.ActionPoolDelete, true
	default:
		return "", false
	}
}

func workbenchTreeKeyboardAction(event input.InputEvent) (render.ActionID, bool) {
	if event.Key != input.KeyChar || !event.Ctrl || event.Alt || event.Shift {
		return "", false
	}
	switch event.Char {
	case "\x0e", "n":
		return render.ActionWorkbenchNew, true
	case "\x12", "r":
		return render.ActionWorkbenchRename, true
	case "\x18", "x":
		return render.ActionWorkbenchDelete, true
	case "\x04", "d":
		return render.ActionWorkbenchDetach, true
	case "\x1a", "z":
		return render.ActionWorkbenchZoom, true
	default:
		return "", false
	}
}

func reducePromptInput(root state.Root, event input.InputEvent) (state.Root, []Effect) {
	if event.Kind != input.EventKindKey {
		return root, nil
	}
	shell := root.Shell.EnsureDefaults()
	if shell.Overlay.Prompt.SuggestionFocused {
		return reducePromptSuggestionInput(root, event)
	}
	switch event.Key {
	case input.KeyEnter:
		return root, []Effect{
			handledEffect{},
			FuncEffect{Run: func(context.Context) Msg { return ShellPromptSubmitMsg{} }},
		}
	case input.KeyTab:
		root.Shell = refreshPromptCompletions(root.Shell)
		if len(root.Shell.EnsureDefaults().Overlay.Prompt.ActiveSuggestionItems()) > 0 {
			root.Shell = root.Shell.SetPromptSuggestionFocused(true)
			return root.Advance(), []Effect{handledEffect{}}
		}
		root.Shell = refreshPromptCompletions(root.Shell.MovePromptField(1))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyDown:
		root.Shell = root.Shell.MovePromptField(1)
		root.Shell = refreshPromptCompletions(root.Shell)
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyUp, input.KeyShiftTab:
		root.Shell = root.Shell.MovePromptField(-1)
		root.Shell = refreshPromptCompletions(root.Shell)
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyBackspace:
		root.Shell = refreshPromptCompletions(root.Shell.DeletePromptBackward())
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyDelete:
		root.Shell = refreshPromptCompletions(root.Shell.DeletePromptForward())
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyLeft:
		root.Shell = refreshPromptCompletions(root.Shell.MovePromptCursor(-1))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyRight:
		root.Shell = refreshPromptCompletions(root.Shell.MovePromptCursor(1))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyHome:
		root.Shell = refreshPromptCompletions(root.Shell.SetPromptCursor(0))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyEnd:
		root.Shell = refreshPromptCompletions(root.Shell.SetPromptCursor(len([]rune(promptEditableValue(root.Shell.EnsureDefaults().Overlay.Prompt)))))
		return root.Advance(), []Effect{handledEffect{}}
	case input.KeyChar:
		if isBackspaceEvent(event) {
			root.Shell = refreshPromptCompletions(root.Shell.DeletePromptBackward())
			return root.Advance(), []Effect{handledEffect{}}
		}
		if event.Ctrl || event.Char == "" {
			return root, []Effect{handledEffect{}}
		}
		root.Shell = refreshPromptCompletions(root.Shell.InsertPromptText(event.Char))
		return root.Advance(), []Effect{handledEffect{}}
	default:
		return root, []Effect{handledEffect{}}
	}
}

func reducePromptSuggestionInput(root state.Root, event input.InputEvent) (state.Root, []Effect) {
	switch event.Key {
	case input.KeyUp:
		root.Shell = root.Shell.MovePromptSuggestionSelection(-1)
	case input.KeyDown:
		root.Shell = root.Shell.MovePromptSuggestionSelection(1)
	case input.KeyEnter:
		root.Shell = refreshPromptCompletions(root.Shell.AcceptPromptSuggestion())
	case input.KeyRight:
		root.Shell = refreshPromptCompletions(root.Shell.EnterPromptSuggestion())
		if len(root.Shell.EnsureDefaults().Overlay.Prompt.ActiveSuggestionItems()) > 0 {
			root.Shell = root.Shell.SetPromptSuggestionFocused(true)
		}
	case input.KeyLeft:
		root.Shell = refreshPromptCompletions(root.Shell.LeavePromptSuggestionPath())
		if len(root.Shell.EnsureDefaults().Overlay.Prompt.ActiveSuggestionItems()) > 0 {
			root.Shell = root.Shell.SetPromptSuggestionFocused(true)
		}
	case input.KeyTab:
		root.Shell = root.Shell.MovePromptSuggestionSelection(1)
	case input.KeyShiftTab:
		root.Shell = root.Shell.MovePromptSuggestionSelection(-1)
	case input.KeyEsc:
		root.Shell = refreshPromptCompletions(root.Shell.SetPromptSuggestionFocused(false))
	case input.KeyBackspace:
		root.Shell = refreshPromptCompletions(root.Shell.SetPromptSuggestionFocused(false).DeletePromptBackward())
	case input.KeyDelete:
		root.Shell = refreshPromptCompletions(root.Shell.SetPromptSuggestionFocused(false).DeletePromptForward())
	case input.KeyChar:
		if isBackspaceEvent(event) {
			root.Shell = refreshPromptCompletions(root.Shell.SetPromptSuggestionFocused(false).DeletePromptBackward())
			break
		}
		if !event.Ctrl && event.Char != "" {
			root.Shell = refreshPromptCompletions(root.Shell.SetPromptSuggestionFocused(false).InsertPromptText(event.Char))
		}
	default:
		root.Shell = root.Shell.SetPromptSuggestionFocused(false)
	}
	return root.Advance(), []Effect{handledEffect{}}
}

func promptEditableValue(prompt state.PromptState) string {
	if active := prompt.ActivePromptField(); active != nil {
		return active.Value
	}
	return prompt.Value
}

func reduceHelpInput(root state.Root, event input.InputEvent) (state.Root, []Effect) {
	if event.Kind != input.EventKindKey {
		return root, nil
	}
	switch event.Key {
	case input.KeyEnter:
		root.Shell = root.Shell.CloseOverlay()
		return root.Advance(), []Effect{handledEffect{}}
	default:
		return root, []Effect{handledEffect{}}
	}
}

func isBackspaceEvent(event input.InputEvent) bool {
	return event.Key == input.KeyBackspace || (event.Key == input.KeyChar && (event.Char == "\x7f" || event.Char == "\b"))
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
	if command.Action == state.PaneCommandSplit {
		return root, []Effect{
			handledEffect{},
			FuncEffect{Run: func(context.Context) Msg {
				return ShellWorkbenchCommandMsg{Command: state.WorkbenchCommand{
					Action: state.WorkbenchCommandPaneSplit,
					Pane:   command,
					Source: state.PaneCommandSourceKeyboard,
				}}
			}},
		}
	}
	return root, []Effect{
		handledEffect{},
		FuncEffect{Run: func(context.Context) Msg {
			return ShellPaneCommandMsg{Command: command}
		}},
	}
}

func reduceWorkbenchCommandIntent(root state.Root, intent input.Intent) (state.Root, []Effect) {
	if next, effects, ok := reduceViewWorkbenchShortcut(root, intent.Command); ok {
		return next, effects
	}
	command, prompt, ok := workbenchCommandFromIntent(root, intent)
	if !ok {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench command", Body: "invalid shortcut"})
		return root.Advance(), []Effect{handledEffect{}}
	}
	if prompt.Title != "" {
		return root, []Effect{
			handledEffect{},
			FuncEffect{Run: func(context.Context) Msg { return ShellOpenPromptMsg{Prompt: prompt} }},
		}
	}
	return root, []Effect{
		handledEffect{},
		FuncEffect{Run: func(context.Context) Msg { return ShellWorkbenchCommandMsg{Command: command} }},
	}
}

func reduceViewWorkbenchShortcut(root state.Root, command string) (state.Root, []Effect, bool) {
	shell := root.Shell.EnsureDefaults()
	if layoutCommand, ok := terminalViewLayoutCommandFromString(command); ok {
		return applyActiveTerminalViewLayoutCommand(root, layoutCommand), []Effect{handledEffect{}}, true
	}
	switch command {
	case "pane take-owner":
		next, effects := requestPaneResizeOwner(root, shell.ActivePaneID)
		return next, append([]Effect{handledEffect{}}, effects...), true
	case "floating take-owner":
		if shell.ActiveFloatingID == "" {
			root.Shell = shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.owner", Body: "no active floating"})
			return root.Advance(), []Effect{handledEffect{}}, true
		}
		next, effects := requestFloatingResizeOwner(root, shell.ActiveFloatingID)
		return next, append([]Effect{handledEffect{}}, effects...), true
	case "pane reconnect":
		root.Shell = shell.OpenTerminalPicker()
		return root.Advance(), []Effect{handledEffect{}, FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}, true
	case "pane restart":
		terminalID := terminalIDForContentAction(root, shell.ActivePaneID)
		if terminalID == "" {
			root.Shell = shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.restart", Body: "no active terminal"})
			return root.Advance(), []Effect{handledEffect{}}, true
		}
		return root, []Effect{handledEffect{}, FuncEffect{Run: func(context.Context) Msg { return TerminalPoolRestartRequestMsg{TerminalID: terminalID} }}}, true
	default:
		return root, nil, false
	}
}

func applyActiveTerminalViewLayoutCommand(root state.Root, command state.TerminalViewLayoutCommand) state.Root {
	shell := root.Shell.EnsureDefaults()
	var binding state.TerminalViewBinding
	var ok bool
	if shell.ActiveFloatingID != "" {
		root.TerminalViews, binding, ok = root.TerminalViews.ApplyFloatingLayoutCommand(shell.ActiveFloatingID, command)
	} else {
		root.TerminalViews, binding, ok = root.TerminalViews.ApplyPaneLayoutCommand(shell.ActivePaneID, command)
	}
	if !ok {
		root.Shell = shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.layout", Body: "no active view"})
		return root.Advance()
	}
	root.Shell = shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "terminal.layout", Body: terminalViewLayoutToast(binding.Layout)})
	return root.Advance()
}

func terminalViewLayoutCommandFromString(command string) (state.TerminalViewLayoutCommand, bool) {
	switch command {
	case "terminal layout lock":
		return state.TerminalViewLayoutCommand{Action: "toggle-lock"}, true
	case "terminal layout toggle":
		return state.TerminalViewLayoutCommand{Action: "toggle-layout"}, true
	case "terminal layout pan-left":
		return state.TerminalViewLayoutCommand{Action: "pan", DeltaX: -2}, true
	case "terminal layout pan-right":
		return state.TerminalViewLayoutCommand{Action: "pan", DeltaX: 2}, true
	case "terminal layout pan-up":
		return state.TerminalViewLayoutCommand{Action: "pan", DeltaY: -1}, true
	case "terminal layout pan-down":
		return state.TerminalViewLayoutCommand{Action: "pan", DeltaY: 1}, true
	case "terminal layout align-left":
		return state.TerminalViewLayoutCommand{Action: "align", AlignX: state.TerminalViewAlignStart}, true
	case "terminal layout align-right":
		return state.TerminalViewLayoutCommand{Action: "align", AlignX: state.TerminalViewAlignEnd}, true
	case "terminal layout align-top":
		return state.TerminalViewLayoutCommand{Action: "align", AlignY: state.TerminalViewAlignStart}, true
	case "terminal layout align-bottom":
		return state.TerminalViewLayoutCommand{Action: "align", AlignY: state.TerminalViewAlignEnd}, true
	case "terminal layout center":
		return state.TerminalViewLayoutCommand{Action: "center"}, true
	case "terminal layout center-x":
		return state.TerminalViewLayoutCommand{Action: "align", AlignX: state.TerminalViewAlignCenter}, true
	case "terminal layout center-y":
		return state.TerminalViewLayoutCommand{Action: "align", AlignY: state.TerminalViewAlignCenter}, true
	case "terminal layout reset":
		return state.TerminalViewLayoutCommand{Action: "reset"}, true
	default:
		return state.TerminalViewLayoutCommand{}, false
	}
}

func terminalViewLayoutToast(layout state.TerminalViewLayout) string {
	layout = layout.Normalize()
	lock := "unlocked"
	if layout.SizeLocked {
		lock = "locked"
	}
	return fmt.Sprintf("%s %s pan:%d,%d align:%s/%s", lock, layout.Mode, layout.PanX, layout.PanY, layout.AlignX, layout.AlignY)
}

func reduceShellActionIntent(root state.Root, intent input.Intent) (state.Root, []Effect) {
	actionID, ok := actionIDForShellAction(intent.Action, intent.Reason)
	if !ok {
		return root, []Effect{handledEffect{}}
	}
	if _, ok := render.ActionSpecByID(actionID); !ok {
		return root, []Effect{handledEffect{}}
	}
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
	case input.ShellActionFloatingNew, input.ShellActionFloatingCtrl, input.ShellActionFloatingGroup, input.ShellActionFloatingMove, input.ShellActionFloatingSize:
		command, ok := floatingCommandFromIntent(root, intent)
		if !ok {
			return root, []Effect{handledEffect{}}
		}
		msg = ShellFloatingCommandMsg{Command: command}
	case input.ShellActionFloatingOverview:
		msg = ShellOpenFloatingOverviewMsg{}
	case input.ShellActionFloatingSummon:
		index, ok := floatingSummonIndex(intent.Reason)
		if !ok {
			return root, []Effect{handledEffect{}}
		}
		msg = ShellFloatingCommandMsg{Command: state.FloatingCommand{Action: state.FloatingCommandSummon, Index: index, Source: state.PaneCommandSourceKeyboard}}
	case input.ShellActionOpenPool:
		msg = ShellOpenTerminalPoolMsg{}
	case input.ShellActionOpenTree:
		msg = ShellOpenWorkbenchTreeMsg{}
	case input.ShellActionOpenPicker:
		msg = ShellOpenTerminalPickerMsg{}
	case input.ShellActionOpenPrompt:
		msg = ShellOpenPromptMsg{Prompt: state.PromptState{
			Title:       "Command Prompt",
			Context:     "Type a command. Execution is intentionally a reducer-owned placeholder in this phase.",
			Placeholder: "command",
		}}
	case input.ShellActionOpenHelp:
		msg = ShellOpenHelpMsg{Section: "most-used"}
	case input.ShellActionQuit:
		msg = QuitMsg{}
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

func floatingSummonIndex(value string) (int, bool) {
	if len(value) != 1 || value[0] < '1' || value[0] > '9' {
		return 0, false
	}
	return int(value[0] - '1'), true
}

func selectedFloatingOverviewItem(items []state.FloatingOverviewItem) (state.FloatingOverviewItem, bool) {
	if len(items) == 0 {
		return state.FloatingOverviewItem{}, false
	}
	selected := items[0]
	for _, item := range items {
		if item.Selected {
			selected = item
			break
		}
	}
	return selected, selected.FloatingID != ""
}

func workbenchCommandFromIntent(root state.Root, intent input.Intent) (state.WorkbenchCommand, state.PromptState, bool) {
	shell := root.Shell.EnsureDefaults()
	command := state.WorkbenchCommand{Source: state.PaneCommandSourceKeyboard}
	if strings.HasPrefix(intent.Command, "tab jump ") {
		command.Action = state.WorkbenchCommandTabSwitch
		command.TargetID = tabJumpTargetID(shell, intent.Command)
		return command, state.PromptState{}, true
	}
	switch intent.Command {
	case "tab create":
		command.Action = state.WorkbenchCommandTabCreate
		command.Name = nextTabName(shell)
		return command, state.PromptState{}, true
	case "tab next":
		command.Action = state.WorkbenchCommandTabNext
		return command, state.PromptState{}, true
	case "tab previous":
		command.Action = state.WorkbenchCommandTabPrevious
		return command, state.PromptState{}, true
	case "tab rename":
		return command, tabRenamePrompt(shell), true
	case "tab close":
		command.Action = state.WorkbenchCommandTabClose
		return command, state.PromptState{}, true
	case "tab kill confirm=accepted":
		command.Action = state.WorkbenchCommandTabKill
		command.Confirm = state.PaneConfirmAccepted
		return command, state.PromptState{}, true
	case "workspace create":
		command.Action = state.WorkbenchCommandWorkspaceCreate
		command.Name = nextWorkspaceName(shell)
		return command, state.PromptState{}, true
	case "workspace next":
		command.Action = state.WorkbenchCommandWorkspaceNext
		return command, state.PromptState{}, true
	case "workspace previous":
		command.Action = state.WorkbenchCommandWorkspacePrevious
		return command, state.PromptState{}, true
	case "workspace rename":
		return command, workspaceRenamePrompt(shell), true
	case "workspace delete confirm=accepted":
		command.Action = state.WorkbenchCommandWorkspaceDelete
		command.Confirm = state.PaneConfirmAccepted
		return command, state.PromptState{}, true
	case "pane close":
		command.Action = state.WorkbenchCommandPaneClose
		command.Target = state.PaneCommandTarget{PaneID: shell.ActivePaneID}
		return command, state.PromptState{}, true
	case "pane detach":
		command.Action = state.WorkbenchCommandPaneDetach
		command.Target = state.PaneCommandTarget{PaneID: shell.ActivePaneID}
		return command, state.PromptState{}, true
	case "pane kill confirm=accepted":
		command.Action = state.WorkbenchCommandPaneKill
		command.Target = state.PaneCommandTarget{PaneID: shell.ActivePaneID}
		command.Confirm = state.PaneConfirmAccepted
		return command, state.PromptState{}, true
	default:
		return state.WorkbenchCommand{}, state.PromptState{}, false
	}
}

func tabJumpTargetID(shell state.ShellStore, command string) string {
	indexText := strings.TrimSpace(strings.TrimPrefix(command, "tab jump "))
	index, err := strconv.Atoi(indexText)
	if err != nil || index < 1 {
		return "__invalid_tab_jump__"
	}
	tabs := shell.EnsureDefaults().Workspace.Tabs
	if index > len(tabs) {
		return "__invalid_tab_jump__"
	}
	return tabs[index-1].ID
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
	case input.ShellActionFloatingGroup:
		switch intent.Reason {
		case "toggle-all":
			command.Action = state.FloatingCommandToggleAll
		case "fit":
			command.Action = state.FloatingCommandFit
		case "toggle-auto-fit":
			command.Action = state.FloatingCommandToggleAutoFit
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
	case state.InteractionModeTab:
		return input.InteractionModeTab
	case state.InteractionModeWorkspace:
		return input.InteractionModeWorkspace
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
	case input.InteractionModeTab:
		return state.InteractionModeTab
	case input.InteractionModeWorkspace:
		return state.InteractionModeWorkspace
	default:
		return state.InteractionModeNormal
	}
}

func activeTabTitle(shell state.ShellStore) string {
	shell = shell.EnsureDefaults()
	for _, tab := range shell.Workspace.Tabs {
		if tab.ID == shell.Workspace.ActiveTabID {
			return tab.Title
		}
	}
	return "main"
}

func tabRenamePrompt(shell state.ShellStore) state.PromptState {
	return state.PromptState{
		Title:       "Rename Tab",
		Context:     "Rename current tab. Submit applies through workbench command.",
		Purpose:     "tab.rename",
		Value:       activeTabTitle(shell),
		Placeholder: "tab name",
	}
}

func nextTabName(shell state.ShellStore) string {
	shell = shell.EnsureDefaults()
	return fmt.Sprintf("tab %d", len(shell.Workspace.Tabs)+1)
}

func nextWorkspaceName(shell state.ShellStore) string {
	shell = shell.EnsureDefaults()
	return fmt.Sprintf("workspace %d", len(shell.Workspaces)+1)
}

func workspaceRenamePrompt(shell state.ShellStore) state.PromptState {
	shell = shell.EnsureDefaults()
	return state.PromptState{
		Title:       "Rename Workspace",
		Context:     "Rename current workspace. Submit applies through workbench command.",
		Purpose:     "workspace.rename",
		Value:       shell.Workspace.Name,
		Placeholder: "workspace name",
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
