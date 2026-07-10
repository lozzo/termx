package app

import (
	"strconv"
	"strings"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/shortcut"
)

// shortcutContentActionID 把配置 action 投影到仍由 shell overlay reducer 持有的内容动作。
// 这是开发期收敛边界：点击与键盘共享 invocation，但 overlay 状态变更仍只有 shell reducer 能执行。
func shortcutContentActionID(invocation shortcut.ActionInvocation) (render.ActionID, bool) {
	actions := map[string]render.ActionID{
		"terminal_picker.attach":         render.ActionPickerAttach,
		"terminal_picker.split":          render.ActionPickerSplit,
		"terminal_picker.edit":           render.ActionPickerEdit,
		"terminal_picker.kill":           render.ActionPickerKill,
		"terminal_picker.delete":         render.ActionPickerDelete,
		"terminal_picker.close":          render.ActionHelpClose,
		"terminal_pool.attach":           render.ActionPoolAttach,
		"terminal_pool.attach_tab":       render.ActionPoolAttachTab,
		"terminal_pool.attach_float":     render.ActionPoolAttachFloat,
		"terminal_pool.restart":          render.ActionPoolRestart,
		"terminal_pool.edit":             render.ActionPoolEdit,
		"terminal_pool.kill":             render.ActionPoolKill,
		"terminal_pool.delete":           render.ActionPoolDelete,
		"terminal_pool.close":            render.ActionHelpClose,
		"workbench_tree.open":            render.ActionWorkbenchOpen,
		"workbench_tree.new":             render.ActionWorkbenchNew,
		"workbench_tree.rename":          render.ActionWorkbenchRename,
		"workbench_tree.delete":          render.ActionWorkbenchDelete,
		"workbench_tree.detach":          render.ActionWorkbenchDetach,
		"workbench_tree.zoom":            render.ActionWorkbenchZoom,
		"workbench_tree.close":           render.ActionHelpClose,
		"clipboard_history.paste":        render.ActionClipboardHistoryPaste,
		"clipboard_history.new":          render.ActionClipboardHistoryNew,
		"clipboard_history.edit":         render.ActionClipboardHistoryEdit,
		"clipboard_history.delete":       render.ActionClipboardHistoryDelete,
		"clipboard_history.close":        render.ActionHelpClose,
		"floating_overview.open":         render.ActionFloatingSummon,
		"floating_overview.show_all":     render.ActionFloatingShowAll,
		"floating_overview.collapse_all": render.ActionFloatingCollapseAll,
		"floating_overview.close":        render.ActionHelpClose,
		"prompt.submit":                  render.ActionPromptSubmit,
		"prompt.cancel":                  render.ActionPromptCancel,
		"help.close":                     render.ActionHelpClose,
	}
	action, ok := actions[invocation.ID]
	return action, ok
}

func shortcutIntentOwnedByCopy(intent input.Intent) bool {
	switch intent.Kind {
	case input.IntentEnterCopyMode, input.IntentRequestOlder, input.IntentRequestNewer, input.IntentExitCopyMode,
		input.IntentOpenClipboardHistory, input.IntentPasteLastCopy, input.IntentPasteClipboard,
		input.IntentCopyCommand:
		return true
	default:
		return false
	}
}

// shortcutIntentForInvocation 是 app action dispatcher 的兼容执行边界。
// shortcut domain 保留 action 身份和参数；这里把 invocation 投影为现有 reducer intent，
// 后续 reducer 可以逐步直接消费 invocation，但 input 不再拥有 command/effect 语义。
func shortcutIntentForInvocation(invocation shortcut.ActionInvocation, event input.InputEvent) (input.Intent, bool) {
	intent := input.Intent{Event: event, Invocation: invocation}
	id := invocation.ID
	if strings.HasPrefix(id, "menu.") {
		return shortcutMenuIntent(strings.TrimPrefix(id, "menu."), intent)
	}
	switch id {
	case "terminal_picker.open":
		intent.Kind = input.IntentOpenTerminalPicker
	case "copy.enter":
		intent.Kind = input.IntentEnterCopyMode
	case "interaction.exit":
		intent.Kind = input.IntentExitInteraction
	case "copy.exit":
		intent.Kind = input.IntentExitCopyMode
	case "copy.request_older":
		intent.Kind = input.IntentRequestOlder
	case "copy.request_newer":
		intent.Kind = input.IntentRequestNewer
	case "copy.open_clipboard_history":
		intent.Kind = input.IntentOpenClipboardHistory
	case "copy.paste_latest":
		intent.Kind = input.IntentPasteLastCopy
	case "copy.paste_system":
		intent.Kind = input.IntentPasteClipboard
	case "copy.line_start", "copy.line_end", "copy.cursor_left", "copy.cursor_right", "copy.cursor_down", "copy.cursor_up", "copy.accept", "copy.oldest", "copy.newest", "copy.half_page_older", "copy.half_page_newer", "copy.mark", "copy.copy_selection", "copy.search_start":
		intent.Kind, intent.Command = input.IntentCopyCommand, id
	default:
		if command, ok := shortcutPaneCommand(id); ok {
			intent.Kind, intent.Command = input.IntentPaneCommand, command
			return intent, true
		}
		if command, ok := shortcutWorkbenchCommand(invocation); ok {
			intent.Kind, intent.Command = input.IntentWorkbenchCommand, command
			return intent, true
		}
		if action, reason, ok := shortcutShellAction(invocation); ok {
			intent.Kind, intent.Action, intent.Reason = input.IntentShellAction, action, reason
			return intent, true
		}
		return input.Intent{}, false
	}
	return intent, true
}

func shortcutMenuIntent(scene string, intent input.Intent) (input.Intent, bool) {
	switch scene {
	case "copy":
		intent.Kind = input.IntentEnterCopyMode
	case "terminal_picker":
		intent.Kind = input.IntentOpenTerminalPicker
	case "terminal_pool":
		intent.Kind, intent.Action = input.IntentShellAction, input.ShellActionOpenPool
	case "workbench_tree":
		intent.Kind, intent.Action = input.IntentShellAction, input.ShellActionOpenTree
	case "clipboard_history":
		intent.Kind, intent.Action = input.IntentShellAction, input.ShellActionOpenClipboardHistory
	case "floating_overview":
		intent.Kind, intent.Action = input.IntentShellAction, input.ShellActionFloatingOverview
	case "prompt":
		intent.Kind, intent.Action = input.IntentShellAction, input.ShellActionOpenPrompt
	case "help":
		intent.Kind, intent.Action = input.IntentShellAction, input.ShellActionOpenHelp
	default:
		modes := map[string]input.InteractionMode{"panel": input.InteractionModePane, "resize": input.InteractionModeResize, "system": input.InteractionModeGlobal, "floating": input.InteractionModeFloating, "tab": input.InteractionModeTab, "workspace": input.InteractionModeWorkspace}
		mode, ok := modes[scene]
		if !ok {
			return input.Intent{}, false
		}
		intent.Kind, intent.Mode = input.IntentSetInteractionMode, mode
	}
	return intent, true
}

func shortcutPaneCommand(id string) (string, bool) {
	commands := map[string]string{
		"panel.split_right": "pane split-right", "panel.split_down": "pane split-down", "panel.toggle_zoom": "pane toggle-zoom", "panel.balance": "pane balance", "panel.presentation_card": "pane presentation card", "panel.presentation_split_line": "pane presentation split-line", "panel.focus_next": "pane focus-next", "panel.focus_prev": "pane focus-prev",
		"resize.left": "pane resize left delta=2", "resize.right": "pane resize right delta=2", "resize.up": "pane resize up delta=2", "resize.down": "pane resize down delta=2", "resize.left_large": "pane resize left delta=6", "resize.right_large": "pane resize right delta=6", "resize.up_large": "pane resize up delta=6", "resize.down_large": "pane resize down delta=6",
	}
	command, ok := commands[id]
	return command, ok
}

func shortcutWorkbenchCommand(invocation shortcut.ActionInvocation) (string, bool) {
	id := invocation.ID
	commands := map[string]string{
		"panel.close": "pane close", "panel.detach": "pane detach", "panel.reconnect": "pane reconnect", "panel.restart": "pane restart", "panel.take_owner": "pane take-owner", "panel.size_lock": "terminal size lock", "panel.kill": "pane kill confirm=accepted", "panel.kill_and_close": "pane kill confirm=accepted",
		"resize.layout_toggle": "terminal layout toggle", "resize.pan_left": "terminal layout pan-left", "resize.pan_right": "terminal layout pan-right", "resize.pan_up": "terminal layout pan-up", "resize.pan_down": "terminal layout pan-down", "resize.align_left": "terminal layout align-left", "resize.align_right": "terminal layout align-right", "resize.align_top": "terminal layout align-top", "resize.align_bottom": "terminal layout align-bottom", "resize.center": "terminal layout center", "resize.center_x": "terminal layout center-x", "resize.center_y": "terminal layout center-y", "resize.layout_reset": "terminal layout reset",
		"floating.take_owner": "floating take-owner", "tab.create": "tab create", "tab.next": "tab next", "tab.previous": "tab previous", "tab.rename": "tab rename", "tab.close": "tab close", "tab.kill": "tab kill confirm=accepted", "workspace.create": "workspace create", "workspace.next": "workspace next", "workspace.previous": "workspace previous", "workspace.rename": "workspace rename", "workspace.delete": "workspace delete confirm=accepted",
	}
	if id == "tab.jump" {
		index, ok := invocation.Param("index")
		return "tab jump " + strconv.Itoa(index), ok
	}
	command, ok := commands[id]
	return command, ok
}

func shortcutShellAction(invocation shortcut.ActionInvocation) (input.ShellAction, string, bool) {
	id := invocation.ID
	plain := map[string]input.ShellAction{
		"system.toggle_header": input.ShellActionToggleHeader, "system.toggle_footer": input.ShellActionToggleFooter, "system.clear_toasts": input.ShellActionClearToasts, "system.close_toast": input.ShellActionCloseToast, "system.open_terminal_pool": input.ShellActionOpenPool, "system.open_terminal_picker": input.ShellActionOpenPicker, "system.open_workbench_tree": input.ShellActionOpenTree, "system.toggle_shortcut_lock": input.ShellActionToggleShortcutLock, "system.open_prompt": input.ShellActionOpenPrompt, "system.open_help": input.ShellActionOpenHelp, "system.quit": input.ShellActionQuit,
		"floating.new": input.ShellActionFloatingNew, "floating.overview": input.ShellActionFloatingOverview,
	}
	if action, ok := plain[id]; ok {
		return action, "", true
	}
	reasons := map[string]struct {
		action input.ShellAction
		reason string
	}{
		"floating.close": {input.ShellActionFloatingCtrl, "close"}, "floating.collapse": {input.ShellActionFloatingCtrl, "collapse"}, "floating.center": {input.ShellActionFloatingCtrl, "center"}, "floating.toggle_all": {input.ShellActionFloatingGroup, "toggle-all"}, "floating.fit": {input.ShellActionFloatingGroup, "fit"}, "floating.auto_fit": {input.ShellActionFloatingGroup, "toggle-auto-fit"}, "floating.move_left": {input.ShellActionFloatingMove, "left"}, "floating.move_right": {input.ShellActionFloatingMove, "right"}, "floating.move_up": {input.ShellActionFloatingMove, "up"}, "floating.move_down": {input.ShellActionFloatingMove, "down"}, "floating.narrow": {input.ShellActionFloatingSize, "narrow"}, "floating.wide": {input.ShellActionFloatingSize, "wide"}, "floating.short": {input.ShellActionFloatingSize, "short"}, "floating.tall": {input.ShellActionFloatingSize, "tall"},
	}
	if mapped, ok := reasons[id]; ok {
		return mapped.action, mapped.reason, true
	}
	if id == "floating.summon" {
		index, ok := invocation.Param("index")
		return input.ShellActionFloatingSummon, strconv.Itoa(index), ok
	}
	return "", "", false
}
