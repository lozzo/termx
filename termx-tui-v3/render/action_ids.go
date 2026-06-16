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
	ActionPaneFooterClose         ActionID = "pane.footer-close"
	ActionPaneFooterDetach        ActionID = "pane.footer-detach"
	ActionPaneFooterFocus         ActionID = "pane.footer-focus"
	ActionPaneFooterSplitRight    ActionID = "pane.footer-split-right"
	ActionPaneFooterSplitDown     ActionID = "pane.footer-split-down"
	ActionPaneFooterZoom          ActionID = "pane.footer-zoom"
	ActionPaneFooterBalance       ActionID = "pane.footer-balance"
	ActionPaneFooterCard          ActionID = "pane.footer-card"
	ActionPaneFooterSplitLine     ActionID = "pane.footer-split-line"
	ActionResizeLeft              ActionID = "resize.left"
	ActionResizeRight             ActionID = "resize.right"
	ActionResizeUp                ActionID = "resize.up"
	ActionResizeDown              ActionID = "resize.down"
	ActionResizeBalance           ActionID = "resize.balance"
	ActionResizeLayoutLock        ActionID = "resize.layout-lock"
	ActionResizeLayoutToggle      ActionID = "resize.layout-toggle"
	ActionResizeLayoutPan         ActionID = "resize.layout-pan"
	ActionResizeLayoutAlign       ActionID = "resize.layout-align"
	ActionResizeLayoutCenter      ActionID = "resize.layout-center"
	ActionResizeLayoutReset       ActionID = "resize.layout-reset"
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
	ActionFooterQuit              ActionID = "footer.quit"
	ActionFooterNewWorkspace      ActionID = "footer.new-workspace"
	ActionFooterRenameWorkspace   ActionID = "footer.rename-workspace"
	ActionFooterPreviousWorkspace ActionID = "footer.previous-workspace"
	ActionFooterNextWorkspace     ActionID = "footer.next-workspace"
	ActionFooterDeleteWorkspace   ActionID = "footer.delete-workspace"

	ActionFloatingRaise       ActionID = "floating.raise"
	ActionFloatingNew         ActionID = "floating.new"
	ActionFloatingOverview    ActionID = "floating.overview"
	ActionFloatingSummon      ActionID = "floating.summon"
	ActionFloatingClose       ActionID = "floating.close"
	ActionFloatingPick        ActionID = "floating.pick"
	ActionFloatingTakeOwner   ActionID = "floating.take-owner"
	ActionFloatingResize      ActionID = "floating.resize"
	ActionFloatingCenter      ActionID = "floating.center"
	ActionFloatingCollapse    ActionID = "floating.collapse"
	ActionFloatingToggleAll   ActionID = "floating.toggle-all"
	ActionFloatingShowAll     ActionID = "floating.show-all"
	ActionFloatingCollapseAll ActionID = "floating.collapse-all"
	ActionFloatingFit         ActionID = "floating.fit"
	ActionFloatingAutoFit     ActionID = "floating.auto-fit"
	ActionFloatingMoveLeft    ActionID = "floating.move-left"
	ActionFloatingMoveRight   ActionID = "floating.move-right"
	ActionFloatingMoveUp      ActionID = "floating.move-up"
	ActionFloatingMoveDown    ActionID = "floating.move-down"
	ActionFloatingNarrow      ActionID = "floating.narrow"
	ActionFloatingWide        ActionID = "floating.wide"
	ActionFloatingShort       ActionID = "floating.short"
	ActionFloatingTall        ActionID = "floating.tall"
	ActionFloatingMoveDrag    ActionID = "floating.move-drag"
	ActionFloatingResizeDrag  ActionID = "floating.resize-drag"

	ActionEmptyAttach  ActionID = "empty.attach"
	ActionEmptyCreate  ActionID = "empty.create"
	ActionEmptyManager ActionID = "empty.manager"
	ActionEmptyClose   ActionID = "empty.close"

	ActionExitedRestart   ActionID = "exited.restart"
	ActionExitedReconnect ActionID = "exited.reconnect"
	ActionExitedClose     ActionID = "exited.close"

	ActionPickerAttach ActionID = "picker.attach"
	ActionPickerNew    ActionID = "picker.new"
	ActionPickerSplit  ActionID = "picker.split"
	ActionPickerEdit   ActionID = "picker.edit"
	ActionPickerKill   ActionID = "picker.kill"
	ActionPickerDelete ActionID = "picker.delete"

	ActionPoolSelect      ActionID = "pool.select"
	ActionPoolAttach      ActionID = "pool.attach"
	ActionPoolAttachTab   ActionID = "pool.attach-tab"
	ActionPoolAttachFloat ActionID = "pool.attach-float"
	ActionPoolEdit        ActionID = "pool.edit"
	ActionPoolKill        ActionID = "pool.kill"
	ActionPoolDelete      ActionID = "pool.delete"

	ActionWorkbenchSelect ActionID = "workbench.select"
	ActionWorkbenchOpen   ActionID = "workbench.open"
	ActionWorkbenchRename ActionID = "workbench.rename"
	ActionWorkbenchNew    ActionID = "workbench.new"
	ActionWorkbenchDelete ActionID = "workbench.delete"
	ActionWorkbenchDetach ActionID = "workbench.detach"
	ActionWorkbenchZoom   ActionID = "workbench.zoom"

	ActionClipboardHistorySelect ActionID = "clipboard-history.select"
	ActionClipboardHistoryPaste  ActionID = "clipboard-history.paste"
	ActionClipboardHistoryEdit   ActionID = "clipboard-history.edit"
	ActionClipboardHistoryDelete ActionID = "clipboard-history.delete"

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
		actionSpec(ActionPaneZoom, ActionDispatchPaneCommand, ActionSurfacePaneChrome, ActionSurfaceFloatingChrome, ActionSurfaceHelp).withChromeGlyph(paneChromeZoomGlyph()).withHelp("zoom"),
		actionSpec(ActionPaneClose, ActionDispatchPaneCommand, ActionSurfacePaneChrome, ActionSurfaceHelp).withChromeGlyph(paneChromeCloseActionText()).withHelp("close").withDanger(),
		actionSpec(ActionPaneFooterClose, ActionDispatchApp, ActionSurfaceFooter).withFooter("w", "CLOSE", StyleStatusWarning).withHelp("close").withDanger(),
		actionSpec(ActionPaneFooterDetach, ActionDispatchApp, ActionSurfaceFooter).withFooter("d", "DETACH", StyleStatusWarning).withHelp("detach"),
		actionSpec(ActionPaneFooterFocus, ActionDispatchApp, ActionSurfaceFooter).withFooter("h/j/k/l", "FOCUS", StyleStatusAccent).withHelp("focus"),
		actionSpec(ActionPaneFooterSplitRight, ActionDispatchApp, ActionSurfaceFooter).withFooter("%", "VSPLIT", StyleStatusAccent).withHelp("vertical split"),
		actionSpec(ActionPaneFooterSplitDown, ActionDispatchApp, ActionSurfaceFooter).withFooter("\"", "HSPLIT", StyleStatusAccent).withHelp("horizontal split"),
		actionSpec(ActionPaneFooterZoom, ActionDispatchApp, ActionSurfaceFooter).withFooter("z", "ZOOM", StyleStatusAccent).withHelp("zoom"),
		actionSpec(ActionPaneFooterBalance, ActionDispatchApp, ActionSurfaceFooter).withFooter("b", "BALANCE", StyleStatusAccent).withHelp("balance"),
		actionSpec(ActionPaneFooterCard, ActionDispatchApp, ActionSurfaceFooter).withFooter("c", "CARD", StyleStatusAccent).withHelp("card presentation"),
		actionSpec(ActionPaneFooterSplitLine, ActionDispatchApp, ActionSurfaceFooter).withFooter("p", "LINE", StyleStatusAccent).withHelp("split line presentation"),
		actionSpec(ActionResizeLeft, ActionDispatchApp, ActionSurfaceFooter).withFooter("←/h", "", StyleStatusWarning).withHelp("resize left"),
		actionSpec(ActionResizeRight, ActionDispatchApp, ActionSurfaceFooter).withFooter("→/l", "", StyleStatusWarning).withHelp("resize right"),
		actionSpec(ActionResizeUp, ActionDispatchApp, ActionSurfaceFooter).withFooter("↑/k", "", StyleStatusWarning).withHelp("resize up"),
		actionSpec(ActionResizeDown, ActionDispatchApp, ActionSurfaceFooter).withFooter("↓/j", "", StyleStatusWarning).withHelp("resize down"),
		actionSpec(ActionResizeBalance, ActionDispatchApp, ActionSurfaceFooter).withFooter("=", "BALANCE", StyleStatusAccent).withHelp("balance"),
		actionSpec(ActionResizeLayoutLock, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("s", "LOCK", StyleStatusAccent).withHelp("lock terminal view size"),
		actionSpec(ActionResizeLayoutToggle, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("space", "LAYOUT", StyleStatusAccent).withHelp("toggle terminal view layout"),
		actionSpec(ActionResizeLayoutPan, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("S+arrows", "PAN", StyleStatusWarning).withHelp("pan terminal view content"),
		actionSpec(ActionResizeLayoutAlign, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("0/$/^/B", "ALIGN", StyleStatusAccent).withHelp("align terminal view content"),
		actionSpec(ActionResizeLayoutCenter, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("m/|/_", "CENTER", StyleStatusAccent).withHelp("center terminal view content"),
		actionSpec(ActionResizeLayoutReset, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("r", "RESET", StyleStatusWarning).withHelp("reset terminal view layout"),
		actionSpec(ActionCopyOlder, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("PgUp", "SCROLL", StyleStatusAccent).withHelp("older history"),
		actionSpec(ActionTerminalTakeResizeOwner, ActionDispatchApp, ActionSurfacePaneChrome, ActionSurfaceHelp).withChromeGlyph("◇ follow").withHelp("take resize owner"),
		actionSpec(ActionTabCreate, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("c", "NEW", StyleStatusAccent).withHelp("create"),
		actionSpec(ActionTabSwitch, ActionDispatchApp, ActionSurfaceLayout, ActionSurfaceHelp).withHelp("switch"),
		actionSpec(ActionTabClose, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("x", "KILL", StyleStatusWarning).withHelp("kill").withDanger(),
		actionSpec(ActionTabRename, ActionDispatchApp, ActionSurfaceFooter).withFooter("r", "RENAME", StyleStatusAccent).withHelp("rename"),
		actionSpec(ActionTabPrevious, ActionDispatchApp, ActionSurfaceFooter).withFooter("p", "PREV", StyleStatusAccent).withHelp("previous"),
		actionSpec(ActionTabNext, ActionDispatchApp, ActionSurfaceFooter).withFooter("n", "NEXT", StyleStatusAccent).withHelp("next"),
		actionSpec(ActionFooterPaneMode, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("^P", "PANE", StyleFooterKeyPane).withHelp("pane"),
		actionSpec(ActionFooterResizeMode, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("^R", "RESIZE", StyleFooterKeyResize).withHelp("resize"),
		actionSpec(ActionFooterTabMode, ActionDispatchApp, ActionSurfaceFooter).withFooter("^T", "TAB", StyleFooterKeyTab).withHelp("tab"),
		actionSpec(ActionFooterWorkspaceMode, ActionDispatchApp, ActionSurfaceFooter).withFooter("^W", "WORKSPACE", StyleFooterKeyWorkspace).withHelp("workspace"),
		actionSpec(ActionFooterFloatingMode, ActionDispatchApp, ActionSurfaceFooter).withFooter("^O", "FLOAT", StyleFooterKeyFloat).withHelp("floating"),
		actionSpec(ActionFooterCopyMode, ActionDispatchApp, ActionSurfaceFooter).withFooter("^V", "COPY", StyleFooterKeyCopy).withHelp("copy"),
		actionSpec(ActionFooterGlobalMode, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("^G", "GLOBAL", StyleFooterKeyGlobal).withHelp("global"),
		actionSpec(ActionFooterPicker, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("^F", "PICKER", StyleFooterKeyPicker).withHelp("picker"),
		actionSpec(ActionFooterToggleHeader, ActionDispatchApp, ActionSurfaceFooter).withFooter("h", "HEADER", StyleStatusAccent).withHelp("toggle header"),
		actionSpec(ActionFooterToggleFooter, ActionDispatchApp, ActionSurfaceFooter).withFooter("f", "FOOTER", StyleStatusAccent).withHelp("toggle footer"),
		actionSpec(ActionFooterOpenPool, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("t", "TERMINALS", StyleStatusAccent).withHelp("terminal pool"),
		actionSpec(ActionFooterOpenTree, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("w", "TREE", StyleStatusAccent).withHelp("tree"),
		actionSpec(ActionFooterCloseToast, ActionDispatchApp, ActionSurfaceFooter).withFooter("T", "TOAST", StyleStatusWarning).withHelp("close toast"),
		actionSpec(ActionFooterClearToasts, ActionDispatchApp, ActionSurfaceFooter).withFooter("c", "CLEAR", StyleStatusWarning).withHelp("clear toasts"),
		actionSpec(ActionFooterQuit, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("q", "QUIT", StyleStatusWarning).withHelp("quit tui"),
		actionSpec(ActionFooterNewWorkspace, ActionDispatchApp, ActionSurfaceFooter).withFooter("c", "NEW", StyleStatusAccent).withHelp("new workspace"),
		actionSpec(ActionFooterRenameWorkspace, ActionDispatchApp, ActionSurfaceFooter).withFooter("r", "RENAME", StyleStatusAccent).withHelp("rename workspace"),
		actionSpec(ActionFooterPreviousWorkspace, ActionDispatchApp, ActionSurfaceFooter).withFooter("p", "PREV", StyleStatusAccent).withHelp("previous workspace"),
		actionSpec(ActionFooterNextWorkspace, ActionDispatchApp, ActionSurfaceFooter).withFooter("n", "NEXT", StyleStatusAccent).withHelp("next workspace"),
		actionSpec(ActionFooterDeleteWorkspace, ActionDispatchApp, ActionSurfaceFooter).withFooter("x", "DELETE", StyleStatusWarning).withHelp("delete workspace").withDanger(),
		actionSpec(ActionFloatingRaise, ActionDispatchApp, ActionSurfaceFloatingChrome, ActionSurfaceContent, ActionSurfaceHelp).withChromeGlyph(paneChromeZoomGlyph()).withHelp("raise"),
		actionSpec(ActionFloatingNew, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceInput).withFooter("n", "NEW FLOAT", StyleStatusAccent).withHelp("new floating"),
		actionSpec(ActionFloatingOverview, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceInput, ActionSurfaceHelp).withFooter("o", "OVERVIEW", StyleStatusAccent).withHelp("floating overview"),
		actionSpec(ActionFloatingSummon, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceInput, ActionSurfaceHelp).withFooter("1-9", "SUMMON", StyleStatusAccent).withHelp("summon floating"),
		actionSpec(ActionFloatingClose, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceFloatingChrome, ActionSurfaceHelp).withFooter("x", "CLOSE", StyleStatusWarning).withChromeGlyph(paneChromeCloseGlyph()).withHelp("close").withDanger(),
		actionSpec(ActionFloatingPick, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("f", "PICK", StyleStatusAccent).withHelp("pick terminal"),
		actionSpec(ActionFloatingTakeOwner, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("a", "OWNER", StyleStatusAccent).withHelp("take resize owner"),
		actionSpec(ActionFloatingResize, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withHelp("resize"),
		actionSpec(ActionFloatingCenter, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceFloatingChrome, ActionSurfaceInput).withFooter("c", "CENTER", StyleStatusAccent).withChromeGlyph("").withHelp("center"),
		actionSpec(ActionFloatingCollapse, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceFloatingChrome, ActionSurfaceInput).withFooter("m", "COLLAPSE", StyleStatusAccent).withChromeGlyph("").withHelp("collapse"),
		actionSpec(ActionFloatingToggleAll, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("v", "ALL", StyleStatusAccent).withHelp("toggle all floating panes"),
		actionSpec(ActionFloatingFit, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("=", "FIT", StyleStatusAccent).withHelp("fit floating to live content"),
		actionSpec(ActionFloatingAutoFit, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("s", "AUTO-FIT", StyleStatusAccent).withHelp("toggle floating auto-fit"),
		actionSpec(ActionFloatingShowAll, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("s", "SHOW ALL", StyleStatusAccent).withHelp("show all floating panes"),
		actionSpec(ActionFloatingCollapseAll, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("c", "COLLAPSE ALL", StyleStatusWarning).withHelp("collapse all floating panes"),
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
		actionSpec(ActionPickerSplit, ActionDispatchApp, ActionSurfaceInput, ActionSurfaceHelp).withHelp("attach in split"),
		actionSpec(ActionPickerEdit, ActionDispatchApp, ActionSurfaceInput, ActionSurfaceHelp).withHelp("edit terminal metadata"),
		actionSpec(ActionPickerKill, ActionDispatchApp, ActionSurfaceInput, ActionSurfaceHelp).withHelp("kill terminal").withDanger(),
		actionSpec(ActionPickerDelete, ActionDispatchApp, ActionSurfaceInput, ActionSurfaceHelp).withHelp("delete terminal inventory entry").withDanger(),
		actionSpec(ActionPoolSelect, ActionDispatchApp, ActionSurfaceContent, ActionSurfaceHelp).withHelp("select"),
		actionSpec(ActionPoolAttach, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("attach", "", StyleStatusAccent).withHelp("attach"),
		actionSpec(ActionPoolAttachTab, ActionDispatchApp, ActionSurfaceInput, ActionSurfaceHelp).withHelp("attach as tab"),
		actionSpec(ActionPoolAttachFloat, ActionDispatchApp, ActionSurfaceInput, ActionSurfaceHelp).withHelp("attach as floating"),
		actionSpec(ActionPoolEdit, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent).withFooter("edit", "", StyleStatusAccent).withHelp("edit"),
		actionSpec(ActionPoolKill, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("kill", "", StyleStatusWarning).withHelp("kill").withDanger(),
		actionSpec(ActionPoolDelete, ActionDispatchApp, ActionSurfaceInput, ActionSurfaceHelp).withHelp("delete terminal inventory entry").withDanger(),
		actionSpec(ActionWorkbenchSelect, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent).withFooter("focus", "", StyleStatusAccent).withHelp("focus"),
		actionSpec(ActionWorkbenchOpen, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("open", "", StyleStatusAccent).withHelp("open"),
		actionSpec(ActionWorkbenchRename, ActionDispatchApp, ActionSurfaceContent, ActionSurfaceHelp).withHelp("rename"),
		actionSpec(ActionWorkbenchNew, ActionDispatchApp, ActionSurfaceContent, ActionSurfaceHelp).withHelp("new"),
		actionSpec(ActionWorkbenchDelete, ActionDispatchApp, ActionSurfaceContent).withHelp("delete").withDanger(),
		actionSpec(ActionWorkbenchDetach, ActionDispatchApp, ActionSurfaceInput, ActionSurfaceHelp).withHelp("detach pane"),
		actionSpec(ActionWorkbenchZoom, ActionDispatchApp, ActionSurfaceInput, ActionSurfaceHelp).withHelp("zoom pane"),
		actionSpec(ActionClipboardHistorySelect, ActionDispatchApp, ActionSurfaceContent, ActionSurfaceHelp).withHelp("select clipboard entry"),
		actionSpec(ActionClipboardHistoryPaste, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("enter", "paste", StyleStatusAccent).withHelp("paste clipboard entry"),
		actionSpec(ActionClipboardHistoryEdit, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("edit", "", StyleStatusAccent).withHelp("edit clipboard entry"),
		actionSpec(ActionClipboardHistoryDelete, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("delete", "", StyleStatusWarning).withHelp("delete clipboard entry").withDanger(),
		actionSpec(ActionPromptSubmit, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("enter", "submit", StyleStatusAccent).withHelp("submit"),
		actionSpec(ActionPromptCancel, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("esc", "cancel", StyleStatusWarning).withHelp("cancel"),
		actionSpec(ActionPromptOpen, ActionDispatchApp, ActionSurfaceInput).withHelp("open prompt"),
		actionSpec(ActionHelpClose, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceHelp, ActionSurfaceContent).withFooter("enter", "close", StyleStatusAccent).withHelp("close"),
		actionSpec(ActionHelpOpen, ActionDispatchApp, ActionSurfaceFooter, ActionSurfaceInput).withFooter("?", "HELP", StyleStatusAccent).withHelp("open help"),
	}
}

func ActionSpecByID(id ActionID) (ActionSpec, bool) {
	spec, ok := actionSpecByIDCatalog[id]
	if !ok {
		return ActionSpec{}, false
	}
	return actionSpecWithCurrentGlyph(spec), true
}

var actionSpecByIDCatalog = buildActionSpecByIDCatalog()

func buildActionSpecByIDCatalog() map[ActionID]ActionSpec {
	specs := ActionSpecCatalog()
	out := make(map[ActionID]ActionSpec, len(specs))
	for _, spec := range specs {
		out[spec.ID] = spec
	}
	return out
}

func actionSpecWithCurrentGlyph(spec ActionSpec) ActionSpec {
	switch spec.ID {
	case ActionPaneSplitDown:
		spec.ChromeGlyph = paneChromeSplitHorizontalActionText()
	case ActionPaneSplitRight:
		spec.ChromeGlyph = paneChromeSplitVerticalActionText()
	case ActionPaneZoom, ActionFloatingRaise:
		spec.ChromeGlyph = paneChromeZoomGlyph()
	case ActionPaneClose:
		spec.ChromeGlyph = paneChromeCloseActionText()
	case ActionFloatingClose:
		spec.ChromeGlyph = paneChromeCloseGlyph()
	}
	return spec
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
