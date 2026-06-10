package render

// ActionID 是 render 暴露给 app 的稳定交互动作编号。
// render 只声明可点击对象的语义，不执行业务逻辑。
type ActionID string

func (id ActionID) String() string {
	return string(id)
}

const (
	ActionPaneFocus               ActionID = "pane.focus"
	ActionPaneResize              ActionID = "pane.resize"
	ActionPaneSplitDown           ActionID = "pane.split-down"
	ActionPaneSplitRight          ActionID = "pane.split-right"
	ActionPaneZoom                ActionID = "pane.zoom"
	ActionPaneClose               ActionID = "pane.close"
	ActionPaneFooterSplit         ActionID = "pane.footer-split"
	ActionPaneFooterClose         ActionID = "pane.footer-close"
	ActionPaneFooterFocus         ActionID = "pane.footer-focus"
	ActionPaneFooterZoom          ActionID = "pane.footer-zoom"
	ActionResizeLeft              ActionID = "resize.left"
	ActionResizeRight             ActionID = "resize.right"
	ActionResizeUp                ActionID = "resize.up"
	ActionResizeDown              ActionID = "resize.down"
	ActionResizeBalance           ActionID = "resize.balance"
	ActionCopyOlder               ActionID = "copy.older"
	ActionTerminalTakeResizeOwner ActionID = "terminal.resize-owner.take"

	ActionTabCreate   ActionID = "tab.create"
	ActionTabSwitch   ActionID = "tab.switch"
	ActionTabClose    ActionID = "tab.close"
	ActionTabRename   ActionID = "tab.rename"
	ActionTabPrevious ActionID = "tab.previous"
	ActionTabNext     ActionID = "tab.next"

	ActionFooterPaneMode          ActionID = "footer.mode-pane"
	ActionFooterResizeMode        ActionID = "footer.mode-resize"
	ActionFooterTabMode           ActionID = "footer.mode-tab"
	ActionFooterWorkspaceMode     ActionID = "footer.mode-workspace"
	ActionFooterFloatingMode      ActionID = "footer.mode-floating"
	ActionFooterCopyMode          ActionID = "footer.mode-copy"
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
	ActionFloatingNew        ActionID = "floating.new"
	ActionFloatingClose      ActionID = "floating.close"
	ActionFloatingResize     ActionID = "floating.resize"
	ActionFloatingCenter     ActionID = "floating.center"
	ActionFloatingCollapse   ActionID = "floating.collapse"
	ActionFloatingMoveLeft   ActionID = "floating.move-left"
	ActionFloatingMoveRight  ActionID = "floating.move-right"
	ActionFloatingMoveUp     ActionID = "floating.move-up"
	ActionFloatingMoveDown   ActionID = "floating.move-down"
	ActionFloatingNarrow     ActionID = "floating.narrow"
	ActionFloatingWide       ActionID = "floating.wide"
	ActionFloatingShort      ActionID = "floating.short"
	ActionFloatingTall       ActionID = "floating.tall"
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
	ActionPromptOpen   ActionID = "prompt.open"

	ActionHelpClose ActionID = "help.close"
	ActionHelpOpen  ActionID = "help.open"
)

func actionID(id ActionID) string {
	return string(id)
}

func prefixedActionID(prefix string, action string) string {
	return prefix + "." + action
}

type ActionSurface string

const (
	ActionSurfaceFooter         ActionSurface = "footer"
	ActionSurfacePaneChrome     ActionSurface = "pane-chrome"
	ActionSurfaceFloatingChrome ActionSurface = "floating-chrome"
	ActionSurfaceContent        ActionSurface = "content"
	ActionSurfaceHelp           ActionSurface = "help"
	ActionSurfaceInput          ActionSurface = "input"
	ActionSurfaceLayout         ActionSurface = "layout"
)

type ActionDispatch string

const (
	ActionDispatchNone        ActionDispatch = ""
	ActionDispatchApp         ActionDispatch = "app"
	ActionDispatchPaneCommand ActionDispatch = "pane-command"
	ActionDispatchDrag        ActionDispatch = "drag"
)

type ActionSpec struct {
	ID          ActionID
	Surfaces    []ActionSurface
	Dispatch    ActionDispatch
	FooterKey   string
	FooterLabel string
	FooterStyle StyleToken
	ChromeGlyph string
	HelpLabel   string
	Danger      bool
}

func (spec ActionSpec) HasSurface(surface ActionSurface) bool {
	for _, candidate := range spec.Surfaces {
		if candidate == surface {
			return true
		}
	}
	return false
}

func actionSpec(id ActionID, dispatch ActionDispatch, surfaces ...ActionSurface) ActionSpec {
	return ActionSpec{ID: id, Dispatch: dispatch, Surfaces: surfaces}
}

func (spec ActionSpec) withFooter(key string, label string, style StyleToken) ActionSpec {
	spec.FooterKey = key
	spec.FooterLabel = label
	spec.FooterStyle = style
	return spec
}

func (spec ActionSpec) withChromeGlyph(glyph string) ActionSpec {
	spec.ChromeGlyph = glyph
	return spec
}

func (spec ActionSpec) withHelp(label string) ActionSpec {
	spec.HelpLabel = label
	return spec
}

func (spec ActionSpec) withDanger() ActionSpec {
	spec.Danger = true
	return spec
}

// ActionSpecCatalog 是所有可见和可点击 action 的唯一声明来源。
// 它只描述语义、默认显示和分发类别；业务修改仍由 app/state 完成。
func ActionSpecCatalog() []ActionSpec {
	return []ActionSpec{
		actionSpec(ActionPaneFocus, ActionDispatchPaneCommand, ActionSurfacePaneChrome, ActionSurfaceLayout).withHelp("focus"),
		actionSpec(ActionPaneResize, ActionDispatchDrag, ActionSurfaceLayout).withHelp("resize"),
		actionSpec(ActionPaneSplitDown, ActionDispatchPaneCommand, ActionSurfacePaneChrome, ActionSurfaceHelp).withChromeGlyph(paneChromeSplitHorizontalActionText()).withHelp("split down"),
		actionSpec(ActionPaneSplitRight, ActionDispatchPaneCommand, ActionSurfacePaneChrome, ActionSurfaceHelp).withChromeGlyph(paneChromeSplitVerticalActionText()).withHelp("split right"),
		actionSpec(ActionPaneZoom, ActionDispatchPaneCommand, ActionSurfacePaneChrome, ActionSurfaceHelp).withChromeGlyph(paneChromeZoomGlyph()).withHelp("zoom"),
		actionSpec(ActionPaneClose, ActionDispatchPaneCommand, ActionSurfacePaneChrome, ActionSurfaceHelp).withChromeGlyph(paneChromeCloseActionText()).withHelp("close").withDanger(),
		actionSpec(ActionPaneFooterSplit, ActionDispatchApp, ActionSurfaceFooter).withFooter("v", "split", StyleStatusAccent).withHelp("split"),
		actionSpec(ActionPaneFooterClose, ActionDispatchApp, ActionSurfaceFooter).withFooter("x", "close", StyleStatusWarning).withHelp("close").withDanger(),
		actionSpec(ActionPaneFooterFocus, ActionDispatchApp, ActionSurfaceFooter).withFooter("n", "focus", StyleStatusAccent).withHelp("focus"),
		actionSpec(ActionPaneFooterZoom, ActionDispatchApp, ActionSurfaceFooter).withFooter("z", "zoom", StyleStatusAccent).withHelp("zoom"),
		actionSpec(ActionResizeLeft, ActionDispatchApp, ActionSurfaceFooter).withFooter("←/h", "", StyleStatusWarning).withHelp("resize left"),
		actionSpec(ActionResizeRight, ActionDispatchApp, ActionSurfaceFooter).withFooter("→/l", "", StyleStatusWarning).withHelp("resize right"),
		actionSpec(ActionResizeUp, ActionDispatchApp, ActionSurfaceFooter).withFooter("↑/k", "", StyleStatusWarning).withHelp("resize up"),
		actionSpec(ActionResizeDown, ActionDispatchApp, ActionSurfaceFooter).withFooter("↓/j", "", StyleStatusWarning).withHelp("resize down"),
		actionSpec(ActionResizeBalance, ActionDispatchApp, ActionSurfaceFooter).withFooter("b", "balance", StyleStatusAccent).withHelp("balance"),
		actionSpec(ActionCopyOlder, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("pgup", "older", StyleStatusAccent).withHelp("older history"),
		actionSpec(ActionTerminalTakeResizeOwner, ActionDispatchApp, ActionSurfacePaneChrome, ActionSurfaceHelp).withChromeGlyph("◇ follow").withHelp("take resize owner"),
		actionSpec(ActionTabCreate, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("n", "new", StyleStatusAccent).withHelp("create"),
		actionSpec(ActionTabSwitch, ActionDispatchApp, ActionSurfaceLayout, ActionSurfaceHelp).withHelp("switch"),
		actionSpec(ActionTabClose, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("x", "close", StyleStatusWarning).withHelp("close").withDanger(),
		actionSpec(ActionTabRename, ActionDispatchApp, ActionSurfaceFooter).withFooter("r", "rename", StyleStatusAccent).withHelp("rename"),
		actionSpec(ActionTabPrevious, ActionDispatchApp, ActionSurfaceFooter).withFooter("h", "prev", StyleStatusAccent).withHelp("previous"),
		actionSpec(ActionTabNext, ActionDispatchApp, ActionSurfaceFooter).withFooter("l", "next", StyleStatusAccent).withHelp("next"),
		actionSpec(ActionFooterPaneMode, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("^P", "pane", StyleFooterKeyPane).withHelp("pane"),
		actionSpec(ActionFooterResizeMode, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("^R", "resize", StyleFooterKeyResize).withHelp("resize"),
		actionSpec(ActionFooterTabMode, ActionDispatchApp, ActionSurfaceFooter).withFooter("^T", "tab", StyleFooterKeyTab).withHelp("tab"),
		actionSpec(ActionFooterWorkspaceMode, ActionDispatchApp, ActionSurfaceFooter).withFooter("^W", "workspace", StyleFooterKeyWorkspace).withHelp("workspace"),
		actionSpec(ActionFooterFloatingMode, ActionDispatchApp, ActionSurfaceFooter).withFooter("^O", "float", StyleFooterKeyFloat).withHelp("floating"),
		actionSpec(ActionFooterCopyMode, ActionDispatchApp, ActionSurfaceFooter).withFooter("^V", "copy", StyleFooterKeyCopy).withHelp("copy"),
		actionSpec(ActionFooterGlobalMode, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("^G", "global", StyleFooterKeyGlobal).withHelp("global"),
		actionSpec(ActionFooterPicker, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("^F", "picker", StyleFooterKeyPicker).withHelp("picker"),
		actionSpec(ActionFooterToggleHeader, ActionDispatchApp, ActionSurfaceFooter).withFooter("h", "header", StyleStatusAccent).withHelp("toggle header"),
		actionSpec(ActionFooterToggleFooter, ActionDispatchApp, ActionSurfaceFooter).withFooter("f", "footer", StyleStatusAccent).withHelp("toggle footer"),
		actionSpec(ActionFooterOpenPool, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("p", "pool", StyleStatusAccent).withHelp("pool"),
		actionSpec(ActionFooterOpenTree, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("w", "tree", StyleStatusAccent).withHelp("tree"),
		actionSpec(ActionFooterCloseToast, ActionDispatchApp, ActionSurfaceFooter).withFooter("T", "toast", StyleStatusWarning).withHelp("close toast"),
		actionSpec(ActionFooterClearToasts, ActionDispatchApp, ActionSurfaceFooter).withFooter("t", "clear", StyleStatusWarning).withHelp("clear toasts"),
		actionSpec(ActionFooterNewWorkspace, ActionDispatchApp, ActionSurfaceFooter).withFooter("n", "new", StyleStatusAccent).withHelp("new workspace"),
		actionSpec(ActionFooterRenameWorkspace, ActionDispatchApp, ActionSurfaceFooter).withFooter("r", "rename", StyleStatusAccent).withHelp("rename workspace"),
		actionSpec(ActionFooterPreviousWorkspace, ActionDispatchApp, ActionSurfaceFooter).withFooter("h", "prev", StyleStatusAccent).withHelp("previous workspace"),
		actionSpec(ActionFooterNextWorkspace, ActionDispatchApp, ActionSurfaceFooter).withFooter("l", "next", StyleStatusAccent).withHelp("next workspace"),
		actionSpec(ActionFooterDeleteWorkspace, ActionDispatchApp, ActionSurfaceFooter).withFooter("x", "delete", StyleStatusWarning).withHelp("delete workspace").withDanger(),
		actionSpec(ActionFloatingRaise, ActionDispatchApp, ActionSurfaceFloatingChrome, ActionSurfaceContent, ActionSurfaceHelp).withChromeGlyph(paneChromeZoomGlyph()).withHelp("raise"),
		actionSpec(ActionFloatingNew, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceInput).withFooter("n", "new", StyleStatusAccent).withHelp("new floating"),
		actionSpec(ActionFloatingClose, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceFloatingChrome, ActionSurfaceHelp).withFooter("x", "close", StyleStatusWarning).withChromeGlyph(paneChromeCloseGlyph()).withHelp("close").withDanger(),
		actionSpec(ActionFloatingResize, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withHelp("resize"),
		actionSpec(ActionFloatingCenter, ActionDispatchApp, ActionSurfaceInput).withHelp("center"),
		actionSpec(ActionFloatingCollapse, ActionDispatchApp, ActionSurfaceInput).withHelp("collapse"),
		actionSpec(ActionFloatingMoveLeft, ActionDispatchApp, ActionSurfaceInput).withHelp("move left"),
		actionSpec(ActionFloatingMoveRight, ActionDispatchApp, ActionSurfaceInput).withHelp("move right"),
		actionSpec(ActionFloatingMoveUp, ActionDispatchApp, ActionSurfaceInput).withHelp("move up"),
		actionSpec(ActionFloatingMoveDown, ActionDispatchApp, ActionSurfaceInput).withHelp("move down"),
		actionSpec(ActionFloatingNarrow, ActionDispatchApp, ActionSurfaceInput).withHelp("narrow"),
		actionSpec(ActionFloatingWide, ActionDispatchApp, ActionSurfaceInput).withHelp("wide"),
		actionSpec(ActionFloatingShort, ActionDispatchApp, ActionSurfaceInput).withHelp("short"),
		actionSpec(ActionFloatingTall, ActionDispatchApp, ActionSurfaceInput).withHelp("tall"),
		actionSpec(ActionFloatingMoveDrag, ActionDispatchDrag, ActionSurfaceFloatingChrome, ActionSurfaceLayout, ActionSurfaceHelp).withHelp("move drag"),
		actionSpec(ActionFloatingResizeDrag, ActionDispatchDrag, ActionSurfaceFloatingChrome, ActionSurfaceLayout).withHelp("resize drag"),
		actionSpec(ActionEmptyAttach, ActionDispatchApp, ActionSurfaceContent).withHelp("attach"),
		actionSpec(ActionEmptyCreate, ActionDispatchApp, ActionSurfaceContent).withHelp("create"),
		actionSpec(ActionEmptyManager, ActionDispatchApp, ActionSurfaceContent).withHelp("manager"),
		actionSpec(ActionEmptyClose, ActionDispatchApp, ActionSurfaceContent).withHelp("close").withDanger(),
		actionSpec(ActionExitedRestart, ActionDispatchApp, ActionSurfaceContent).withHelp("restart"),
		actionSpec(ActionExitedReconnect, ActionDispatchApp, ActionSurfaceContent).withHelp("reconnect"),
		actionSpec(ActionExitedClose, ActionDispatchApp, ActionSurfaceContent).withHelp("close").withDanger(),
		actionSpec(ActionPickerAttach, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent).withFooter("attach", "", StyleStatusAccent).withHelp("attach"),
		actionSpec(ActionPickerNew, ActionDispatchApp, ActionSurfaceContent).withHelp("new"),
		actionSpec(ActionPoolSelect, ActionDispatchApp, ActionSurfaceContent, ActionSurfaceHelp).withHelp("select"),
		actionSpec(ActionPoolAttach, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("attach", "", StyleStatusAccent).withHelp("attach"),
		actionSpec(ActionPoolEdit, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent).withFooter("edit", "", StyleStatusAccent).withHelp("edit"),
		actionSpec(ActionPoolKill, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("kill", "", StyleStatusWarning).withHelp("kill").withDanger(),
		actionSpec(ActionWorkbenchSelect, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent).withFooter("focus", "", StyleStatusAccent).withHelp("focus"),
		actionSpec(ActionWorkbenchOpen, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("open", "", StyleStatusAccent).withHelp("open"),
		actionSpec(ActionWorkbenchRename, ActionDispatchApp, ActionSurfaceContent, ActionSurfaceHelp).withHelp("rename"),
		actionSpec(ActionWorkbenchNew, ActionDispatchApp, ActionSurfaceContent, ActionSurfaceHelp).withHelp("new"),
		actionSpec(ActionWorkbenchDelete, ActionDispatchApp, ActionSurfaceContent).withHelp("delete").withDanger(),
		actionSpec(ActionPromptSubmit, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("enter", "submit", StyleStatusAccent).withHelp("submit"),
		actionSpec(ActionPromptCancel, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("esc", "cancel", StyleStatusWarning).withHelp("cancel"),
		actionSpec(ActionPromptOpen, ActionDispatchApp, ActionSurfaceInput).withHelp("open prompt"),
		actionSpec(ActionHelpClose, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp, ActionSurfaceContent).withFooter("enter", "close", StyleStatusAccent).withHelp("close"),
		actionSpec(ActionHelpOpen, ActionDispatchApp, ActionSurfaceInput).withHelp("open help"),
	}
}

func ActionSpecByID(id ActionID) (ActionSpec, bool) {
	for _, spec := range ActionSpecCatalog() {
		if spec.ID == id {
			return spec, true
		}
	}
	return ActionSpec{}, false
}

func ActionSpecByIDString(id string) (ActionSpec, bool) {
	return ActionSpecByID(ActionID(id))
}

// ActionIDCatalog 返回 render/app 共享的 action id 注册表。
// 该表从 ActionSpecCatalog 派生，防止 renderer 和 reducer 继续各自散落字符串契约。
func ActionIDCatalog() []ActionID {
	specs := ActionSpecCatalog()
	out := make([]ActionID, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec.ID)
	}
	return out
}
