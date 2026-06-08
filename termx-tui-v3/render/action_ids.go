package render

// ActionID 是 render 暴露给 app 的稳定交互动作编号。
// render 只声明可点击对象的语义，不执行业务逻辑。
type ActionID string

func (id ActionID) String() string {
	return string(id)
}

const (
	ActionPaneFocus       ActionID = "pane.focus"
	ActionPaneResize      ActionID = "pane.resize"
	ActionPaneSplitDown   ActionID = "pane.split-down"
	ActionPaneSplitRight  ActionID = "pane.split-right"
	ActionPaneClose       ActionID = "pane.close"
	ActionPaneFooterSplit ActionID = "pane.footer-split"
	ActionPaneFooterClose ActionID = "pane.footer-close"
	ActionPaneFooterFocus ActionID = "pane.footer-focus"
	ActionPaneFooterZoom  ActionID = "pane.footer-zoom"
	ActionResizeLeft      ActionID = "resize.left"
	ActionResizeRight     ActionID = "resize.right"
	ActionResizeUp        ActionID = "resize.up"
	ActionResizeDown      ActionID = "resize.down"
	ActionResizeBalance   ActionID = "resize.balance"

	ActionTabCreate   ActionID = "tab.create"
	ActionTabClose    ActionID = "tab.close"
	ActionTabRename   ActionID = "tab.rename"
	ActionTabPrevious ActionID = "tab.previous"
	ActionTabNext     ActionID = "tab.next"

	ActionFooterPaneMode          ActionID = "footer.mode-pane"
	ActionFooterResizeMode        ActionID = "footer.mode-resize"
	ActionFooterGlobalMode        ActionID = "footer.mode-global"
	ActionFooterPicker            ActionID = "footer.open-picker"
	ActionFooterToggleHeader      ActionID = "footer.toggle-header"
	ActionFooterToggleFooter      ActionID = "footer.toggle-footer"
	ActionFooterOpenPool          ActionID = "footer.open-pool"
	ActionFooterOpenTree          ActionID = "footer.open-tree"
	ActionFooterCloseToast        ActionID = "footer.close-toast"
	ActionFooterClearToasts       ActionID = "footer.clear-toasts"
	ActionFooterNewWorkspace      ActionID = "footer.new-workspace"
	ActionFooterRenameWorkspace   ActionID = "footer.rename-workspace"
	ActionFooterPreviousWorkspace ActionID = "footer.previous-workspace"
	ActionFooterNextWorkspace     ActionID = "footer.next-workspace"
	ActionFooterDeleteWorkspace   ActionID = "footer.delete-workspace"

	ActionFloatingRaise      ActionID = "floating.raise"
	ActionFloatingClose      ActionID = "floating.close"
	ActionFloatingResize     ActionID = "floating.resize"
	ActionFloatingMoveDrag   ActionID = "floating.move-drag"
	ActionFloatingResizeDrag ActionID = "floating.resize-drag"

	ActionEmptyAttach  ActionID = "empty.attach"
	ActionEmptyCreate  ActionID = "empty.create"
	ActionEmptyManager ActionID = "empty.manager"
	ActionEmptyClose   ActionID = "empty.close"

	ActionExitedRestart   ActionID = "exited.restart"
	ActionExitedReconnect ActionID = "exited.reconnect"
	ActionExitedClose     ActionID = "exited.close"

	ActionPickerAttach ActionID = "picker.attach"
	ActionPickerNew    ActionID = "picker.new"

	ActionPoolSelect ActionID = "pool.select"
	ActionPoolAttach ActionID = "pool.attach"
	ActionPoolEdit   ActionID = "pool.edit"
	ActionPoolKill   ActionID = "pool.kill"

	ActionWorkbenchSelect ActionID = "workbench.select"
	ActionWorkbenchOpen   ActionID = "workbench.open"
	ActionWorkbenchRename ActionID = "workbench.rename"
	ActionWorkbenchNew    ActionID = "workbench.new"
	ActionWorkbenchDelete ActionID = "workbench.delete"

	ActionPromptSubmit ActionID = "prompt.submit"
	ActionPromptCancel ActionID = "prompt.cancel"

	ActionHelpClose ActionID = "help.close"
)

func actionID(id ActionID) string {
	return string(id)
}

func prefixedActionID(prefix string, action string) string {
	return prefix + "." + action
}

// ActionIDCatalog 返回 render/app 共享的 action id 注册表。
// 该表用于 guard，防止 renderer 和 reducer 继续各自散落字符串契约。
func ActionIDCatalog() []ActionID {
	return []ActionID{
		ActionPaneFocus,
		ActionPaneResize,
		ActionPaneSplitDown,
		ActionPaneSplitRight,
		ActionPaneClose,
		ActionPaneFooterSplit,
		ActionPaneFooterClose,
		ActionPaneFooterFocus,
		ActionPaneFooterZoom,
		ActionResizeLeft,
		ActionResizeRight,
		ActionResizeUp,
		ActionResizeDown,
		ActionResizeBalance,
		ActionTabCreate,
		ActionTabClose,
		ActionTabRename,
		ActionTabPrevious,
		ActionTabNext,
		ActionFooterPaneMode,
		ActionFooterResizeMode,
		ActionFooterGlobalMode,
		ActionFooterPicker,
		ActionFooterToggleHeader,
		ActionFooterToggleFooter,
		ActionFooterOpenPool,
		ActionFooterOpenTree,
		ActionFooterCloseToast,
		ActionFooterClearToasts,
		ActionFooterNewWorkspace,
		ActionFooterRenameWorkspace,
		ActionFooterPreviousWorkspace,
		ActionFooterNextWorkspace,
		ActionFooterDeleteWorkspace,
		ActionFloatingRaise,
		ActionFloatingClose,
		ActionFloatingResize,
		ActionFloatingMoveDrag,
		ActionFloatingResizeDrag,
		ActionEmptyAttach,
		ActionEmptyCreate,
		ActionEmptyManager,
		ActionEmptyClose,
		ActionExitedRestart,
		ActionExitedReconnect,
		ActionExitedClose,
		ActionPickerAttach,
		ActionPickerNew,
		ActionPoolSelect,
		ActionPoolAttach,
		ActionPoolEdit,
		ActionPoolKill,
		ActionWorkbenchSelect,
		ActionWorkbenchOpen,
		ActionWorkbenchRename,
		ActionWorkbenchNew,
		ActionWorkbenchDelete,
		ActionPromptSubmit,
		ActionPromptCancel,
		ActionHelpClose,
	}
}
