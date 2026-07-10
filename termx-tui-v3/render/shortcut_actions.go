package render

import (
	"strconv"
	"strings"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/shortcut"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

// ShortcutActionRenderID 把 shortcut action id 映射为 render/app 共享的可点击 action id。
// 快捷键真值来自 input catalog；这里只提供展示和点击分发所需的 render 元数据桥接。
func ShortcutActionRenderID(actionID string) (ActionID, bool) {
	switch actionID {
	case "menu.panel":
		return ActionFooterPaneMode, true
	case "menu.resize":
		return ActionFooterResizeMode, true
	case "menu.tab":
		return ActionFooterTabMode, true
	case "menu.workspace":
		return ActionFooterWorkspaceMode, true
	case "menu.floating":
		return ActionFooterFloatingMode, true
	case "menu.copy", "copy.enter":
		return ActionFooterCopyMode, true
	case "menu.system":
		return ActionFooterGlobalMode, true
	case "terminal_picker.open", "picker.open", "menu.terminal_picker", "system.open_terminal_picker":
		return ActionFooterPicker, true
	case "menu.terminal_pool", "system.open_terminal_pool":
		return ActionFooterOpenPool, true
	case "menu.workbench_tree", "system.open_workbench_tree":
		return ActionFooterOpenTree, true
	case "menu.clipboard_history", "copy.open_clipboard_history":
		return ActionClipboardHistoryOpen, true
	case "menu.floating_overview", "floating.overview":
		return ActionFloatingOverview, true
	case "menu.prompt", "system.open_prompt":
		return ActionPromptOpen, true
	case "menu.help", "system.open_help":
		return ActionHelpOpen, true
	case "interaction.exit", "copy.exit":
		return ActionShortcutExit, true
	case "system.toggle_header":
		return ActionFooterToggleHeader, true
	case "system.toggle_footer":
		return ActionFooterToggleFooter, true
	case "system.toggle_shortcut_lock":
		return ActionFooterShortcutLock, true
	case "system.clear_toasts":
		return ActionFooterClearToasts, true
	case "system.close_toast":
		return ActionFooterCloseToast, true
	case "system.quit":
		return ActionFooterQuit, true
	case "panel.close", "pane.close":
		return ActionPaneFooterClose, true
	case "panel.detach", "pane.detach":
		return ActionPaneFooterDetach, true
	case "panel.split_right", "pane.split_right":
		return ActionPaneFooterSplitRight, true
	case "panel.split_down", "pane.split_down":
		return ActionPaneFooterSplitDown, true
	case "panel.toggle_zoom", "pane.toggle_zoom":
		return ActionPaneFooterZoom, true
	case "panel.balance", "pane.balance":
		return ActionPaneFooterBalance, true
	case "panel.presentation_card", "pane.presentation_card":
		return ActionPaneFooterCard, true
	case "panel.presentation_split_line", "pane.presentation_split_line":
		return ActionPaneFooterSplitLine, true
	case "panel.focus_next", "panel.focus_prev", "pane.focus_next", "pane.focus_prev":
		return ActionPaneFooterFocus, true
	case "resize.left", "resize.left_large":
		return ActionResizeLeft, true
	case "resize.right", "resize.right_large":
		return ActionResizeRight, true
	case "resize.up", "resize.up_large":
		return ActionResizeUp, true
	case "resize.down", "resize.down_large":
		return ActionResizeDown, true
	case "resize.layout_toggle":
		return ActionResizeLayoutToggle, true
	case "resize.pan_left", "resize.pan_right", "resize.pan_up", "resize.pan_down":
		return ActionResizeLayoutPan, true
	case "resize.align_left", "resize.align_right", "resize.align_top", "resize.align_bottom":
		return ActionResizeLayoutAlign, true
	case "resize.center", "resize.center_x", "resize.center_y":
		return ActionResizeLayoutCenter, true
	case "resize.layout_reset":
		return ActionResizeLayoutReset, true
	case "tab.create":
		return ActionTabCreate, true
	case "tab.next":
		return ActionTabNext, true
	case "tab.previous":
		return ActionTabPrevious, true
	case "tab.rename":
		return ActionTabRename, true
	case "tab.close", "tab.kill":
		return ActionTabClose, true
	case "workspace.create":
		return ActionFooterNewWorkspace, true
	case "workspace.next":
		return ActionFooterNextWorkspace, true
	case "workspace.previous":
		return ActionFooterPreviousWorkspace, true
	case "workspace.rename":
		return ActionFooterRenameWorkspace, true
	case "workspace.delete":
		return ActionFooterDeleteWorkspace, true
	case "floating.new":
		return ActionFloatingNew, true
	case "floating.summon":
		return ActionFloatingSummon, true
	case "floating.take_owner":
		return ActionFloatingTakeOwner, true
	case "floating.close":
		return ActionFloatingClose, true
	case "floating.center":
		return ActionFloatingCenter, true
	case "floating.collapse":
		return ActionFloatingCollapse, true
	case "floating.toggle_all":
		return ActionFloatingToggleAll, true
	case "floating.fit":
		return ActionFloatingFit, true
	case "floating.auto_fit":
		return ActionFloatingAutoFit, true
	case "floating.move_left":
		return ActionFloatingMoveLeft, true
	case "floating.move_right":
		return ActionFloatingMoveRight, true
	case "floating.move_up":
		return ActionFloatingMoveUp, true
	case "floating.move_down":
		return ActionFloatingMoveDown, true
	case "floating.narrow":
		return ActionFloatingNarrow, true
	case "floating.wide":
		return ActionFloatingWide, true
	case "floating.short":
		return ActionFloatingShort, true
	case "floating.tall":
		return ActionFloatingTall, true
	case "floating_overview.show_all":
		return ActionFloatingShowAll, true
	case "floating_overview.collapse_all":
		return ActionFloatingCollapseAll, true
	case "floating_overview.open":
		return ActionFloatingSummon, true
	case "copy.request_older":
		return ActionCopyOlder, true
	case "terminal_picker.attach":
		return ActionPickerAttach, true
	case "terminal_picker.split":
		return ActionPickerSplit, true
	case "terminal_picker.edit":
		return ActionPickerEdit, true
	case "terminal_picker.kill":
		return ActionPickerKill, true
	case "terminal_picker.delete":
		return ActionPickerDelete, true
	case "terminal_picker.close":
		return ActionHelpClose, true
	case "terminal_pool.attach":
		return ActionPoolAttach, true
	case "terminal_pool.attach_tab":
		return ActionPoolAttachTab, true
	case "terminal_pool.attach_float":
		return ActionPoolAttachFloat, true
	case "terminal_pool.restart":
		return ActionPoolRestart, true
	case "terminal_pool.edit":
		return ActionPoolEdit, true
	case "terminal_pool.kill":
		return ActionPoolKill, true
	case "terminal_pool.delete":
		return ActionPoolDelete, true
	case "terminal_pool.close":
		return ActionHelpClose, true
	case "workbench_tree.open":
		return ActionWorkbenchOpen, true
	case "workbench_tree.new":
		return ActionWorkbenchNew, true
	case "workbench_tree.rename":
		return ActionWorkbenchRename, true
	case "workbench_tree.delete":
		return ActionWorkbenchDelete, true
	case "workbench_tree.detach":
		return ActionWorkbenchDetach, true
	case "workbench_tree.zoom":
		return ActionWorkbenchZoom, true
	case "workbench_tree.close":
		return ActionHelpClose, true
	case "clipboard_history.paste":
		return ActionClipboardHistoryPaste, true
	case "clipboard_history.new":
		return ActionClipboardHistoryNew, true
	case "clipboard_history.edit":
		return ActionClipboardHistoryEdit, true
	case "clipboard_history.delete":
		return ActionClipboardHistoryDelete, true
	case "clipboard_history.close":
		return ActionHelpClose, true
	case "floating_overview.close":
		return ActionHelpClose, true
	case "prompt.submit":
		return ActionPromptSubmit, true
	case "prompt.cancel":
		return ActionPromptCancel, true
	case "help.close":
		return ActionHelpClose, true
	}
	if strings.HasPrefix(actionID, "tab.jump.") {
		return ActionTabSwitch, true
	}
	if strings.HasPrefix(actionID, "floating.summon.") {
		return ActionFloatingSummon, true
	}
	return "", false
}

func shortcutSceneForFooterMode(mode string) string {
	switch mode {
	case "live", "normal", "":
		return "global"
	case "global":
		return "system"
	case "pane":
		return "panel"
	case string(state.OverlayTerminalPicker):
		return "terminal_picker"
	case string(state.OverlayTerminalPool):
		return "terminal_pool"
	case string(state.OverlayWorkbenchTree):
		return "workbench_tree"
	case string(state.OverlayClipboardHistory):
		return "clipboard_history"
	case string(state.OverlayFloatingOverview):
		return "floating_overview"
	default:
		return mode
	}
}

// bindOverlayShortcutInvocations 把 overlay content 的可配置点击动作绑定到当前 catalog。
// invocation 是“执行什么”的真值，HitRegion 上的 row/pane 仍只是点击目标上下文；catalog 未配置的
// shortcut action 不保留旧 ActionID 点击入口，行选择和拖拽等非 shortcut 交互不受影响。
func bindOverlayShortcutInvocations(kind OverlayKind, regions []HitRegion, shortcuts state.TUIShortcutConfig) []HitRegion {
	scene := shortcutSceneForFooterMode(string(kind))
	entries := input.ShortcutEntriesForScene(shortcuts, scene)
	invocations := map[ActionID][]shortcut.ActionInvocation{}
	for _, entry := range entries {
		invocation, _, err := shortcut.ParseInvocation(entry.ActionID)
		if err != nil {
			continue
		}
		renderID, ok := shortcutActionRenderIDForEntry(entry)
		if !ok {
			continue
		}
		if scene == "floating_overview" && renderID == ActionFloatingSummon && invocation.ID != "floating_overview.open" {
			continue
		}
		invocations[renderID] = appendUniqueInvocation(invocations[renderID], invocation)
	}
	out := make([]HitRegion, 0, len(regions))
	for _, region := range regions {
		renderID := ActionID(region.ActionID)
		if !overlayShortcutRenderAction(renderID) {
			out = append(out, region)
			continue
		}
		candidates := invocations[renderID]
		if len(candidates) != 1 {
			continue
		}
		region.Invocation = candidates[0]
		out = append(out, region)
	}
	return out
}

func appendUniqueInvocation(items []shortcut.ActionInvocation, invocation shortcut.ActionInvocation) []shortcut.ActionInvocation {
	signature := invocation.Signature()
	for _, item := range items {
		if item.Signature() == signature {
			return items
		}
	}
	return append(items, invocation)
}

func overlayShortcutRenderAction(id ActionID) bool {
	switch id {
	case ActionPickerAttach, ActionPickerSplit, ActionPickerEdit, ActionPickerKill, ActionPickerDelete,
		ActionPoolAttach, ActionPoolAttachTab, ActionPoolAttachFloat, ActionPoolRestart, ActionPoolEdit, ActionPoolKill, ActionPoolDelete,
		ActionWorkbenchOpen, ActionWorkbenchNew, ActionWorkbenchRename, ActionWorkbenchDelete, ActionWorkbenchDetach, ActionWorkbenchZoom,
		ActionClipboardHistoryPaste, ActionClipboardHistoryNew, ActionClipboardHistoryEdit, ActionClipboardHistoryDelete,
		ActionFloatingSummon, ActionFloatingShowAll, ActionFloatingCollapseAll,
		ActionPromptSubmit, ActionPromptCancel, ActionHelpClose:
		return true
	default:
		return false
	}
}

func footerActionCatalogFromShortcuts(mode string, root state.Root) []FooterActionVM {
	return shortcutActionCatalogFromShortcuts(mode, root, false)
}

func helpActionCatalogFromShortcuts(mode string, root state.Root) []FooterActionVM {
	return shortcutActionCatalogFromShortcuts(mode, root, true)
}

func shortcutActionCatalogFromShortcuts(mode string, root state.Root, includeHidden bool) []FooterActionVM {
	scene := shortcutSceneForFooterMode(mode)
	entries := input.ShortcutEntriesForScene(root.Config.Shortcuts, scene)
	out := make([]FooterActionVM, 0, len(entries))
	for _, entry := range entries {
		if input.ShortcutKeyRequiresEnhancedKeyboard(entry.Key) && !root.HostCapabilities.KeyboardDisambiguation {
			continue
		}
		action, ok := shortcutActionFromShortcutEntry(entry, root.Config, includeHidden)
		if ok {
			out = append(out, action)
		}
	}
	out = compactShortcutFooterActions(scene, out)
	return out
}

func shortcutActionFromShortcutEntry(entry input.ShortcutEntry, cfg state.TUIConfigStore, forHelp bool) (FooterActionVM, bool) {
	invocation, spec, err := shortcut.ParseInvocation(entry.ActionID)
	if err != nil {
		return FooterActionVM{}, false
	}
	visible := spec.Display.Footer == shortcut.VisibilityVisible
	if entry.Show != nil {
		visible = *entry.Show
	}
	if forHelp {
		visible = spec.Display.Help == shortcut.VisibilityVisible
	}
	if !visible {
		return FooterActionVM{}, false
	}
	action := FooterActionVM{ActionID: invocation.ID}
	if renderID, ok := shortcutActionRenderIDForEntry(entry); ok {
		action = footerActionFor(renderID)
	} else if !forHelp && (entry.Show == nil || !*entry.Show) {
		// 默认 footer 仍按现有 render product catalog 收敛；只有用户显式 show:true 才扩展提示面。
		return FooterActionVM{}, false
	}
	action.Key = entry.KeyLabel
	if action.Key == "" {
		action.Key = input.ShortcutKeyDisplay(entry.Key)
	}
	if label := shortcutActionLabel(entry, cfg, action); label != "" {
		action.Label = label
	}
	action.Invocation = invocation
	action.Click = spec.Display.Click
	return action, true
}

func shortcutActionRenderIDForEntry(entry input.ShortcutEntry) (ActionID, bool) {
	scene := shortcutCatalogScene(entry.Scene)
	if scene == "floating" && entry.ActionID == "system.open_terminal_picker" {
		return ActionFloatingPick, true
	}
	if scene == "resize" && (entry.ActionID == "panel.take_owner" || entry.ActionID == "pane.take_owner") {
		return ActionTerminalTakeResizeOwner, true
	}
	if scene == "resize" && (entry.ActionID == "panel.size_lock" || entry.ActionID == "pane.size_lock") {
		return ActionResizeLayoutLock, true
	}
	if scene == "resize" && (entry.ActionID == "panel.balance" || entry.ActionID == "pane.balance") {
		return ActionResizeBalance, true
	}
	return ShortcutActionRenderID(entry.ActionID)
}

func shortcutCatalogScene(scene string) string {
	scene = strings.ReplaceAll(strings.TrimSpace(scene), "-", "_")
	if scene == "pane" {
		return "panel"
	}
	return scene
}

func shortcutActionLabel(entry input.ShortcutEntry, cfg state.TUIConfigStore, action FooterActionVM) string {
	if label := strings.TrimSpace(entry.Label); label != "" {
		return label
	}
	if label := configuredShortcutActionLabel(cfg.Shortcuts.Actions, entry.ActionID); label != "" {
		return label
	}
	_, spec, err := shortcut.ParseInvocation(entry.ActionID)
	if err == nil {
		return strings.TrimSpace(spec.DefaultLabel)
	}
	return ""
}

func configuredShortcutActionLabel(actions map[string]state.TUIShortcutActionConfig, actionID string) string {
	_, target, err := shortcut.ParseInvocation(actionID)
	if err != nil {
		return ""
	}
	for configuredID, action := range actions {
		_, configured, err := shortcut.ParseInvocation(configuredID)
		if err == nil && configured.ID == target.ID {
			return strings.TrimSpace(action.Label)
		}
	}
	return ""
}

func compactShortcutFooterActions(scene string, actions []FooterActionVM) []FooterActionVM {
	scene = shortcutCatalogScene(scene)
	if scene == "global" || scene == "system" {
		actions = compactFooterActionGroup(actions, ActionFooterOpenPool)
		actions = orderShortcutFooterActions(actions, []ActionID{
			ActionFooterToggleHeader,
			ActionFooterToggleFooter,
			ActionHelpOpen,
			ActionPromptOpen,
			ActionFooterOpenPool,
			ActionFooterOpenTree,
			ActionFooterShortcutLock,
			ActionFooterClearToasts,
			ActionFooterCloseToast,
			ActionFooterQuit,
		})
	}
	if scene == "panel" {
		actions = compactFooterActionGroup(actions, ActionPaneFooterClose)
		actions = compactFooterActionGroup(actions, ActionPaneFooterSplitRight)
		actions = compactFooterActionGroup(actions, ActionPaneFooterSplitDown)
		actions = compactFooterActionGroup(actions, ActionPaneFooterFocus)
	}
	if scene == "resize" {
		actions = compactFooterActionGroup(actions, ActionResizeLeft)
		actions = compactFooterActionGroup(actions, ActionResizeRight)
		actions = compactFooterActionGroup(actions, ActionResizeUp)
		actions = compactFooterActionGroup(actions, ActionResizeDown)
		actions = compactFooterActionGroup(actions, ActionResizeBalance)
		actions = compactFooterActionGroup(actions, ActionResizeLayoutPan)
		actions = compactFooterActionGroup(actions, ActionResizeLayoutAlign)
		actions = compactFooterActionGroup(actions, ActionResizeLayoutCenter)
	}
	if scene == "tab" {
		actions = compactFooterActionGroup(actions, ActionTabSwitch)
		actions = compactFooterActionGroup(actions, ActionTabNext)
		actions = compactFooterActionGroup(actions, ActionTabPrevious)
	}
	if scene == "workspace" {
		actions = compactFooterActionGroup(actions, ActionFooterNextWorkspace)
		actions = compactFooterActionGroup(actions, ActionFooterPreviousWorkspace)
		actions = compactFooterActionGroup(actions, ActionFooterOpenTree)
	}
	if scene == "floating" || scene == "floating_overview" {
		actions = compactFloatingSummonActions(actions)
		actions = compactFooterActionGroup(actions, ActionFloatingCollapse)
		actions = compactFooterActionGroup(actions, ActionFloatingMoveLeft)
		actions = compactFooterActionGroup(actions, ActionFloatingMoveRight)
		actions = compactFooterActionGroup(actions, ActionFloatingMoveUp)
		actions = compactFooterActionGroup(actions, ActionFloatingMoveDown)
	}
	return actions
}

func compactFloatingSummonActions(actions []FooterActionVM) []FooterActionVM {
	actionID := ActionFloatingSummon.String()
	indexes := []int{}
	keys := []string{}
	var base FooterActionVM
	for index, action := range actions {
		if action.ActionID != actionID {
			continue
		}
		if _, err := strconv.Atoi(action.Key); err != nil {
			continue
		}
		if len(indexes) == 0 {
			base = action
		}
		indexes = append(indexes, index)
		keys = append(keys, action.Key)
	}
	if len(indexes) <= 1 {
		return actions
	}
	if collapsed := collapsedKeyRange(keys); collapsed != "" {
		base.Key = collapsed
	} else {
		base.Key = strings.Join(keys, "/")
	}
	base.Click = shortcut.ClickHintOnly
	base.Invocation = shortcut.ActionInvocation{}
	return replaceFooterActionIndexes(actions, indexes, base)
}

func orderShortcutFooterActions(actions []FooterActionVM, order []ActionID) []FooterActionVM {
	if len(actions) <= 1 {
		return actions
	}
	remaining := append([]FooterActionVM(nil), actions...)
	out := make([]FooterActionVM, 0, len(actions))
	for _, id := range order {
		actionID := id.String()
		for index := 0; index < len(remaining); index++ {
			if remaining[index].ActionID != actionID {
				continue
			}
			out = append(out, remaining[index])
			remaining = append(remaining[:index], remaining[index+1:]...)
			index--
		}
	}
	out = append(out, remaining...)
	return out
}

func compactFooterActionGroup(actions []FooterActionVM, id ActionID) []FooterActionVM {
	actionID := id.String()
	indexes := []int{}
	keys := []string{}
	var base FooterActionVM
	for index, action := range actions {
		if action.ActionID != actionID {
			continue
		}
		if len(indexes) == 0 {
			base = action
		}
		indexes = append(indexes, index)
		keys = append(keys, action.Key)
	}
	if len(indexes) <= 1 {
		return actions
	}
	if collapsed := collapsedKeyRange(keys); collapsed != "" {
		base.Key = collapsed
	} else {
		base.Key = strings.Join(keys, "/")
	}
	for _, index := range indexes[1:] {
		if actions[index].Invocation.Signature() != base.Invocation.Signature() {
			base.Click = shortcut.ClickHintOnly
			base.Invocation = shortcut.ActionInvocation{}
			break
		}
	}
	return replaceFooterActionIndexes(actions, indexes, base)
}

func replaceFooterActionIndexes(actions []FooterActionVM, indexes []int, base FooterActionVM) []FooterActionVM {
	out := make([]FooterActionVM, 0, len(actions)-len(indexes)+1)
	inserted := false
	skip := map[int]struct{}{}
	for _, index := range indexes {
		skip[index] = struct{}{}
	}
	for index, action := range actions {
		if _, ok := skip[index]; ok {
			if !inserted {
				out = append(out, base)
				inserted = true
			}
			continue
		}
		out = append(out, action)
	}
	return out
}

func collapsedKeyRange(keys []string) string {
	if len(keys) < 3 {
		return ""
	}
	for index, key := range keys {
		if key != strconv.Itoa(index+1) {
			return ""
		}
	}
	return keys[0] + "-" + keys[len(keys)-1]
}
