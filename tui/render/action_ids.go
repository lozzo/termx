package render

import actiondomain "github.com/lozzow/termx/tui/action"

// ProjectionID 是 render-local 的视觉投影编号。
// 它只能定位 footer/chrome/content metadata，不能作为 canonical action identity 或 app handler key。
type ProjectionID string

func (id ProjectionID) String() string {
	return string(id)
}

const (
	ActionPaneFocus               ProjectionID = "pane.focus"
	ActionPaneResize              ProjectionID = "pane.resize"
	ActionPaneSplitDown           ProjectionID = "pane.split-down"
	ActionPaneSplitRight          ProjectionID = "pane.split-right"
	ActionPaneZoom                ProjectionID = "pane.zoom"
	ActionPaneClose               ProjectionID = "pane.close"
	ActionPaneFooterClose         ProjectionID = "pane.footer-close"
	ActionPaneFooterDetach        ProjectionID = "pane.footer-detach"
	ActionPaneFooterFocus         ProjectionID = "pane.footer-focus"
	ActionPaneFooterSplitRight    ProjectionID = "pane.footer-split-right"
	ActionPaneFooterSplitDown     ProjectionID = "pane.footer-split-down"
	ActionPaneFooterZoom          ProjectionID = "pane.footer-zoom"
	ActionPaneFooterBalance       ProjectionID = "pane.footer-balance"
	ActionPaneFooterCard          ProjectionID = "pane.footer-card"
	ActionPaneFooterSplitLine     ProjectionID = "pane.footer-split-line"
	ActionResizeLeft              ProjectionID = "resize.left"
	ActionResizeRight             ProjectionID = "resize.right"
	ActionResizeUp                ProjectionID = "resize.up"
	ActionResizeDown              ProjectionID = "resize.down"
	ActionResizeBalance           ProjectionID = "resize.balance"
	ActionResizeLayoutLock        ProjectionID = "resize.layout-lock"
	ActionResizeLayoutToggle      ProjectionID = "resize.layout-toggle"
	ActionResizeLayoutPan         ProjectionID = "resize.layout-pan"
	ActionResizeLayoutAlign       ProjectionID = "resize.layout-align"
	ActionResizeLayoutCenter      ProjectionID = "resize.layout-center"
	ActionResizeLayoutReset       ProjectionID = "resize.layout-reset"
	ActionCopyOlder               ProjectionID = "copy.older"
	ActionTerminalTakeResizeOwner ProjectionID = "terminal.resize-owner.take"

	ActionTabCreate   ProjectionID = "tab.create"
	ActionTabSwitch   ProjectionID = "tab.switch"
	ActionTabClose    ProjectionID = "tab.close"
	ActionTabRename   ProjectionID = "tab.rename"
	ActionTabPrevious ProjectionID = "tab.previous"
	ActionTabNext     ProjectionID = "tab.next"

	ActionFooterPaneMode          ProjectionID = "footer.mode-pane"
	ActionFooterResizeMode        ProjectionID = "footer.mode-resize"
	ActionFooterTabMode           ProjectionID = "footer.mode-tab"
	ActionFooterWorkspaceMode     ProjectionID = "footer.mode-workspace"
	ActionFooterFloatingMode      ProjectionID = "footer.mode-floating"
	ActionFooterCopyMode          ProjectionID = "footer.mode-copy"
	ActionFooterGlobalMode        ProjectionID = "footer.mode-global"
	ActionFooterPicker            ProjectionID = "footer.open-picker"
	ActionFooterToggleHeader      ProjectionID = "footer.toggle-header"
	ActionFooterToggleFooter      ProjectionID = "footer.toggle-footer"
	ActionFooterShortcutLock      ProjectionID = "footer.shortcut-lock"
	ActionFooterOpenPool          ProjectionID = "footer.open-pool"
	ActionFooterOpenTree          ProjectionID = "footer.open-tree"
	ActionFooterCloseToast        ProjectionID = "footer.close-toast"
	ActionFooterClearToasts       ProjectionID = "footer.clear-toasts"
	ActionFooterQuit              ProjectionID = "footer.quit"
	ActionFooterNewWorkspace      ProjectionID = "footer.new-workspace"
	ActionFooterRenameWorkspace   ProjectionID = "footer.rename-workspace"
	ActionFooterPreviousWorkspace ProjectionID = "footer.previous-workspace"
	ActionFooterNextWorkspace     ProjectionID = "footer.next-workspace"
	ActionFooterDeleteWorkspace   ProjectionID = "footer.delete-workspace"

	ActionFloatingRaise       ProjectionID = "floating.raise"
	ActionFloatingNew         ProjectionID = "floating.new"
	ActionFloatingOverview    ProjectionID = "floating.overview"
	ActionFloatingSummon      ProjectionID = "floating.summon"
	ActionFloatingClose       ProjectionID = "floating.close"
	ActionFloatingPick        ProjectionID = "floating.pick"
	ActionFloatingTakeOwner   ProjectionID = "floating.take-owner"
	ActionFloatingResize      ProjectionID = "floating.resize"
	ActionFloatingCenter      ProjectionID = "floating.center"
	ActionFloatingCollapse    ProjectionID = "floating.collapse"
	ActionFloatingToggleAll   ProjectionID = "floating.toggle-all"
	ActionFloatingShowAll     ProjectionID = "floating.show-all"
	ActionFloatingCollapseAll ProjectionID = "floating.collapse-all"
	ActionFloatingFit         ProjectionID = "floating.fit"
	ActionFloatingAutoFit     ProjectionID = "floating.auto-fit"
	ActionFloatingMoveLeft    ProjectionID = "floating.move-left"
	ActionFloatingMoveRight   ProjectionID = "floating.move-right"
	ActionFloatingMoveUp      ProjectionID = "floating.move-up"
	ActionFloatingMoveDown    ProjectionID = "floating.move-down"
	ActionFloatingNarrow      ProjectionID = "floating.narrow"
	ActionFloatingWide        ProjectionID = "floating.wide"
	ActionFloatingShort       ProjectionID = "floating.short"
	ActionFloatingTall        ProjectionID = "floating.tall"
	ActionFloatingMoveDrag    ProjectionID = "floating.move-drag"
	ActionFloatingResizeDrag  ProjectionID = "floating.resize-drag"

	ActionEmptyAttach  ProjectionID = "empty.attach"
	ActionEmptyCreate  ProjectionID = "empty.create"
	ActionEmptyManager ProjectionID = "empty.manager"
	ActionEmptyClose   ProjectionID = "empty.close"

	ActionExitedRestart   ProjectionID = "exited.restart"
	ActionExitedReconnect ProjectionID = "exited.reconnect"
	ActionExitedClose     ProjectionID = "exited.close"

	ActionDisconnectedReconnect  ProjectionID = "disconnected.reconnect"
	ActionDisconnectedDisconnect ProjectionID = "disconnected.disconnect"

	ActionPickerAttach ProjectionID = "picker.attach"
	ActionPickerNew    ProjectionID = "picker.new"
	ActionPickerSplit  ProjectionID = "picker.split"
	ActionPickerEdit   ProjectionID = "picker.edit"
	ActionPickerKill   ProjectionID = "picker.kill"
	ActionPickerDelete ProjectionID = "picker.delete"

	ActionPoolSelect      ProjectionID = "pool.select"
	ActionPoolAttach      ProjectionID = "pool.attach"
	ActionPoolAttachTab   ProjectionID = "pool.attach-tab"
	ActionPoolAttachFloat ProjectionID = "pool.attach-float"
	ActionPoolRestart     ProjectionID = "pool.restart"
	ActionPoolEdit        ProjectionID = "pool.edit"
	ActionPoolKill        ProjectionID = "pool.kill"
	ActionPoolDelete      ProjectionID = "pool.delete"

	ActionWorkbenchSelect ProjectionID = "workbench.select"
	ActionWorkbenchOpen   ProjectionID = "workbench.open"
	ActionWorkbenchRename ProjectionID = "workbench.rename"
	ActionWorkbenchNew    ProjectionID = "workbench.new"
	ActionWorkbenchDelete ProjectionID = "workbench.delete"
	ActionWorkbenchDetach ProjectionID = "workbench.detach"
	ActionWorkbenchZoom   ProjectionID = "workbench.zoom"

	ActionClipboardHistoryOpen        ProjectionID = "clipboard-history.open"
	ActionClipboardHistorySelect      ProjectionID = "clipboard-history.select"
	ActionClipboardHistoryPaste       ProjectionID = "clipboard-history.paste"
	ActionClipboardHistoryNew         ProjectionID = "clipboard-history.new"
	ActionClipboardHistoryEdit        ProjectionID = "clipboard-history.edit"
	ActionClipboardHistoryDelete      ProjectionID = "clipboard-history.delete"
	ActionClipboardHistoryDividerDrag ProjectionID = "clipboard-history.divider-drag"

	ActionPromptSubmit ProjectionID = "prompt.submit"
	ActionPromptCancel ProjectionID = "prompt.cancel"
	ActionPromptOpen   ProjectionID = "prompt.open"

	ActionHelpClose    ProjectionID = "help.close"
	ActionHelpOpen     ProjectionID = "help.open"
	ActionShortcutExit ProjectionID = "shortcut.exit"
)

func actionID(id ProjectionID) string {
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

// ProjectionSpec 只描述 render surface 的视觉和布局元数据。
// Canonical identity/label 属于 tui/action，handler/dispatch 属于 app；本类型不能重新声明执行语义。
type ProjectionSpec struct {
	ID                ProjectionID
	CanonicalActionID actiondomain.ID
	Surfaces          []ActionSurface
	FooterKey         string
	FooterLabel       string
	FooterStyle       StyleToken
	ChromeGlyph       string
	HelpLabel         string
	Danger            bool
}

func (spec ProjectionSpec) HasSurface(surface ActionSurface) bool {
	for _, candidate := range spec.Surfaces {
		if candidate == surface {
			return true
		}
	}
	return false
}

func projectionSpec(id ProjectionID, surfaces ...ActionSurface) ProjectionSpec {
	return ProjectionSpec{ID: id, CanonicalActionID: canonicalActionForProjection(id), Surfaces: surfaces}
}

func (spec ProjectionSpec) withFooter(key string, label string, style StyleToken) ProjectionSpec {
	spec.FooterKey = key
	spec.FooterLabel = label
	spec.FooterStyle = style
	return spec
}

func (spec ProjectionSpec) withChromeGlyph(glyph string) ProjectionSpec {
	spec.ChromeGlyph = glyph
	return spec
}

func (spec ProjectionSpec) withHelp(label string) ProjectionSpec {
	spec.HelpLabel = label
	return spec
}

func (spec ProjectionSpec) withDanger() ProjectionSpec {
	spec.Danger = true
	return spec
}

// ProjectionCatalog 返回现有 render surface 的视觉/布局投影清单。
// ProjectionID 只在 render 内定位 metadata；可唯一执行的投影必须用 CanonicalActionID 引用 tui/action，
// 多 action 聚合提示则由具体 VM 携带 Invocation。footer/help 遗留字段将在 KS016 删除。
func ProjectionCatalog() []ProjectionSpec {
	return []ProjectionSpec{
		projectionSpec(ActionPaneFocus, ActionSurfacePaneChrome, ActionSurfaceLayout).withHelp("focus"),
		projectionSpec(ActionPaneResize, ActionSurfaceLayout).withHelp("resize"),
		projectionSpec(ActionPaneSplitDown, ActionSurfacePaneChrome, ActionSurfaceHelp).withChromeGlyph(paneChromeSplitHorizontalActionText()).withHelp("split down"),
		projectionSpec(ActionPaneSplitRight, ActionSurfacePaneChrome, ActionSurfaceHelp).withChromeGlyph(paneChromeSplitVerticalActionText()).withHelp("split right"),
		projectionSpec(ActionPaneZoom, ActionSurfacePaneChrome, ActionSurfaceFloatingChrome, ActionSurfaceHelp).withChromeGlyph(paneChromeZoomGlyph()).withHelp("zoom"),
		projectionSpec(ActionPaneClose, ActionSurfacePaneChrome, ActionSurfaceHelp).withChromeGlyph(paneChromeCloseActionText()).withHelp("close").withDanger(),
		projectionSpec(ActionPaneFooterClose, ActionSurfaceFooter).withFooter("w", "CLOSE", StyleStatusWarning).withHelp("close").withDanger(),
		projectionSpec(ActionPaneFooterDetach, ActionSurfaceFooter).withFooter("d", "DETACH", StyleStatusWarning).withHelp("detach"),
		projectionSpec(ActionPaneFooterFocus, ActionSurfaceFooter).withFooter("h/j/k/l", "FOCUS", StyleStatusAccent).withHelp("focus"),
		projectionSpec(ActionPaneFooterSplitRight, ActionSurfaceFooter).withFooter("%", "VSPLIT", StyleStatusAccent).withHelp("vertical split"),
		projectionSpec(ActionPaneFooterSplitDown, ActionSurfaceFooter).withFooter("\"", "HSPLIT", StyleStatusAccent).withHelp("horizontal split"),
		projectionSpec(ActionPaneFooterZoom, ActionSurfaceFooter).withFooter("z", "ZOOM", StyleStatusAccent).withHelp("zoom"),
		projectionSpec(ActionPaneFooterBalance, ActionSurfaceFooter).withFooter("b", "BALANCE", StyleStatusAccent).withHelp("balance"),
		projectionSpec(ActionPaneFooterCard, ActionSurfaceFooter).withFooter("c", "CARD", StyleStatusAccent).withHelp("card presentation"),
		projectionSpec(ActionPaneFooterSplitLine, ActionSurfaceFooter).withFooter("p", "LINE", StyleStatusAccent).withHelp("split line presentation"),
		projectionSpec(ActionResizeLeft, ActionSurfaceFooter).withFooter("←/h", "", StyleStatusWarning).withHelp("resize left"),
		projectionSpec(ActionResizeRight, ActionSurfaceFooter).withFooter("→/l", "", StyleStatusWarning).withHelp("resize right"),
		projectionSpec(ActionResizeUp, ActionSurfaceFooter).withFooter("↑/k", "", StyleStatusWarning).withHelp("resize up"),
		projectionSpec(ActionResizeDown, ActionSurfaceFooter).withFooter("↓/j", "", StyleStatusWarning).withHelp("resize down"),
		projectionSpec(ActionResizeBalance, ActionSurfaceFooter).withFooter("=", "BALANCE", StyleStatusAccent).withHelp("balance"),
		projectionSpec(ActionResizeLayoutLock, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("s", "LOCK", StyleStatusAccent).withHelp("toggle terminal size lock"),
		projectionSpec(ActionResizeLayoutToggle, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("space", "LAYOUT", StyleStatusAccent).withHelp("toggle terminal view layout"),
		projectionSpec(ActionResizeLayoutPan, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("S+arrows", "PAN", StyleStatusWarning).withHelp("pan terminal view content"),
		projectionSpec(ActionResizeLayoutAlign, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("0/$/^/B", "ALIGN", StyleStatusAccent).withHelp("align terminal view content"),
		projectionSpec(ActionResizeLayoutCenter, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("m/|/_", "CENTER", StyleStatusAccent).withHelp("center terminal view content"),
		projectionSpec(ActionResizeLayoutReset, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("r", "RESET", StyleStatusWarning).withHelp("reset terminal view layout"),
		projectionSpec(ActionCopyOlder, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("PgUp", "SCROLL", StyleStatusAccent).withHelp("older history"),
		projectionSpec(ActionTerminalTakeResizeOwner, ActionSurfacePaneChrome, ActionSurfaceHelp).withChromeGlyph(paneChromeTakeOwnerText()).withHelp("take resize owner"),
		projectionSpec(ActionTabCreate, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("c", "NEW", StyleStatusAccent).withHelp("create"),
		projectionSpec(ActionTabSwitch, ActionSurfaceLayout, ActionSurfaceHelp).withHelp("switch"),
		projectionSpec(ActionTabClose, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("x", "CLOSE", StyleStatusWarning).withHelp("close").withDanger(),
		projectionSpec(ActionTabRename, ActionSurfaceFooter).withFooter("r", "RENAME", StyleStatusAccent).withHelp("rename"),
		projectionSpec(ActionTabPrevious, ActionSurfaceFooter).withFooter("p", "PREV", StyleStatusAccent).withHelp("previous"),
		projectionSpec(ActionTabNext, ActionSurfaceFooter).withFooter("n", "NEXT", StyleStatusAccent).withHelp("next"),
		projectionSpec(ActionFooterPaneMode, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("^P", "PANE", StyleFooterKeyPane).withHelp("pane"),
		projectionSpec(ActionFooterResizeMode, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("^R", "RESIZE", StyleFooterKeyResize).withHelp("resize"),
		projectionSpec(ActionFooterTabMode, ActionSurfaceFooter).withFooter("^T", "TAB", StyleFooterKeyTab).withHelp("tab"),
		projectionSpec(ActionFooterWorkspaceMode, ActionSurfaceFooter).withFooter("^W", "WORKSPACE", StyleFooterKeyWorkspace).withHelp("workspace"),
		projectionSpec(ActionFooterFloatingMode, ActionSurfaceFooter).withFooter("^O", "FLOAT", StyleFooterKeyFloat).withHelp("floating"),
		projectionSpec(ActionFooterCopyMode, ActionSurfaceFooter).withFooter("^V", "COPY", StyleFooterKeyCopy).withHelp("copy"),
		projectionSpec(ActionFooterGlobalMode, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("^G", "GLOBAL", StyleFooterKeyGlobal).withHelp("global"),
		projectionSpec(ActionFooterPicker, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("^F", "PICKER", StyleFooterKeyPicker).withHelp("picker"),
		projectionSpec(ActionFooterToggleHeader, ActionSurfaceFooter).withFooter("h", "HEADER", StyleStatusAccent).withHelp("toggle header"),
		projectionSpec(ActionFooterToggleFooter, ActionSurfaceFooter).withFooter("f", "FOOTER", StyleStatusAccent).withHelp("toggle footer"),
		projectionSpec(ActionFooterShortcutLock, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("l", "KEYLOCK", StyleStatusWarning).withHelp("toggle shortcut passthrough"),
		projectionSpec(ActionFooterOpenPool, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("t", "TERMINALS", StyleStatusAccent).withHelp("terminal manager"),
		projectionSpec(ActionFooterOpenTree, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("w", "TREE", StyleStatusAccent).withHelp("tree"),
		projectionSpec(ActionFooterCloseToast, ActionSurfaceFooter).withFooter("T", "TOAST", StyleStatusWarning).withHelp("close toast"),
		projectionSpec(ActionFooterClearToasts, ActionSurfaceFooter).withFooter("c", "CLEAR", StyleStatusWarning).withHelp("clear toasts"),
		projectionSpec(ActionFooterQuit, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("q", "QUIT", StyleStatusWarning).withHelp("quit tui"),
		projectionSpec(ActionFooterNewWorkspace, ActionSurfaceFooter).withFooter("c", "NEW", StyleStatusAccent).withHelp("new workspace"),
		projectionSpec(ActionFooterRenameWorkspace, ActionSurfaceFooter).withFooter("r", "RENAME", StyleStatusAccent).withHelp("rename workspace"),
		projectionSpec(ActionFooterPreviousWorkspace, ActionSurfaceFooter).withFooter("p", "PREV", StyleStatusAccent).withHelp("previous workspace"),
		projectionSpec(ActionFooterNextWorkspace, ActionSurfaceFooter).withFooter("n", "NEXT", StyleStatusAccent).withHelp("next workspace"),
		projectionSpec(ActionFooterDeleteWorkspace, ActionSurfaceFooter).withFooter("x", "DELETE", StyleStatusWarning).withHelp("delete workspace").withDanger(),
		projectionSpec(ActionFloatingRaise, ActionSurfaceFloatingChrome, ActionSurfaceContent, ActionSurfaceHelp).withChromeGlyph(paneChromeZoomGlyph()).withHelp("raise"),
		projectionSpec(ActionFloatingNew, ActionSurfaceFooter, ActionSurfaceInput).withFooter("n", "NEW FLOAT", StyleStatusAccent).withHelp("new floating"),
		projectionSpec(ActionFloatingOverview, ActionSurfaceFooter, ActionSurfaceInput, ActionSurfaceHelp).withFooter("o", "OVERVIEW", StyleStatusAccent).withHelp("floating overview"),
		projectionSpec(ActionFloatingSummon, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceInput, ActionSurfaceHelp).withFooter("1-9", "SUMMON", StyleStatusAccent).withHelp("summon floating"),
		projectionSpec(ActionFloatingClose, ActionSurfaceFooter, ActionSurfaceFloatingChrome, ActionSurfaceHelp).withFooter("x", "CLOSE", StyleStatusWarning).withChromeGlyph(paneChromeCloseGlyph()).withHelp("close").withDanger(),
		projectionSpec(ActionFloatingPick, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("f", "PICK", StyleStatusAccent).withHelp("pick terminal"),
		projectionSpec(ActionFloatingTakeOwner, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("a", "OWNER", StyleStatusAccent).withHelp("take resize owner"),
		projectionSpec(ActionFloatingResize, ActionSurfaceFooter, ActionSurfaceHelp).withHelp("resize"),
		projectionSpec(ActionFloatingCenter, ActionSurfaceFooter, ActionSurfaceFloatingChrome, ActionSurfaceInput).withFooter("c", "CENTER", StyleStatusAccent).withChromeGlyph(paneChromeFloatingCenterGlyph()).withHelp("center"),
		projectionSpec(ActionFloatingCollapse, ActionSurfaceFooter, ActionSurfaceFloatingChrome, ActionSurfaceInput).withFooter("m", "HIDE", StyleStatusAccent).withChromeGlyph(paneChromeFloatingCollapseGlyph()).withHelp("hide"),
		projectionSpec(ActionFloatingToggleAll, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("v", "ALL", StyleStatusAccent).withHelp("toggle all floating panes"),
		projectionSpec(ActionFloatingFit, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("=", "FIT", StyleStatusAccent).withHelp("fit floating to live content"),
		projectionSpec(ActionFloatingAutoFit, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("s", "AUTO-FIT", StyleStatusAccent).withHelp("toggle floating auto-fit"),
		projectionSpec(ActionFloatingShowAll, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("s", "SHOW ALL", StyleStatusAccent).withHelp("show all floating panes"),
		projectionSpec(ActionFloatingCollapseAll, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("c", "COLLAPSE ALL", StyleStatusWarning).withHelp("collapse all floating panes"),
		projectionSpec(ActionFloatingMoveLeft, ActionSurfaceInput).withHelp("move left"),
		projectionSpec(ActionFloatingMoveRight, ActionSurfaceInput).withHelp("move right"),
		projectionSpec(ActionFloatingMoveUp, ActionSurfaceInput).withHelp("move up"),
		projectionSpec(ActionFloatingMoveDown, ActionSurfaceInput).withHelp("move down"),
		projectionSpec(ActionFloatingNarrow, ActionSurfaceInput).withHelp("narrow"),
		projectionSpec(ActionFloatingWide, ActionSurfaceInput).withHelp("wide"),
		projectionSpec(ActionFloatingShort, ActionSurfaceInput).withHelp("short"),
		projectionSpec(ActionFloatingTall, ActionSurfaceInput).withHelp("tall"),
		projectionSpec(ActionFloatingMoveDrag, ActionSurfaceFloatingChrome, ActionSurfaceLayout, ActionSurfaceHelp).withHelp("move drag"),
		projectionSpec(ActionFloatingResizeDrag, ActionSurfaceFloatingChrome, ActionSurfaceLayout).withHelp("resize drag"),
		projectionSpec(ActionEmptyAttach, ActionSurfaceContent).withHelp("attach"),
		projectionSpec(ActionEmptyCreate, ActionSurfaceContent).withHelp("create"),
		projectionSpec(ActionEmptyManager, ActionSurfaceContent).withHelp("manager"),
		projectionSpec(ActionEmptyClose, ActionSurfaceContent).withHelp("close").withDanger(),
		projectionSpec(ActionExitedRestart, ActionSurfaceContent).withHelp("restart"),
		projectionSpec(ActionExitedReconnect, ActionSurfaceContent).withHelp("reconnect"),
		projectionSpec(ActionExitedClose, ActionSurfaceContent).withHelp("close").withDanger(),
		projectionSpec(ActionDisconnectedReconnect, ActionSurfaceContent).withHelp("reconnect endpoint terminal"),
		projectionSpec(ActionDisconnectedDisconnect, ActionSurfaceContent).withHelp("disconnect pane").withDanger(),
		projectionSpec(ActionPickerAttach, ActionSurfaceFooter, ActionSurfaceContent).withFooter("attach", "", StyleStatusAccent).withHelp("attach"),
		projectionSpec(ActionPickerNew, ActionSurfaceContent).withHelp("new"),
		projectionSpec(ActionPickerSplit, ActionSurfaceInput, ActionSurfaceHelp).withHelp("attach in split"),
		projectionSpec(ActionPickerEdit, ActionSurfaceInput, ActionSurfaceHelp).withHelp("edit terminal metadata"),
		projectionSpec(ActionPickerKill, ActionSurfaceInput, ActionSurfaceHelp).withHelp("kill terminal").withDanger(),
		projectionSpec(ActionPickerDelete, ActionSurfaceInput, ActionSurfaceHelp).withHelp("delete terminal inventory entry").withDanger(),
		projectionSpec(ActionPoolSelect, ActionSurfaceContent, ActionSurfaceHelp).withHelp("select"),
		projectionSpec(ActionPoolAttach, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("enter", "ATTACH", StyleStatusAccent).withHelp("attach here"),
		projectionSpec(ActionPoolAttachTab, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceInput, ActionSurfaceHelp).withFooter("^T", "TAB", StyleStatusAccent).withHelp("attach as new tab"),
		projectionSpec(ActionPoolAttachFloat, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceInput, ActionSurfaceHelp).withFooter("^O", "FLOAT", StyleStatusAccent).withHelp("attach as floating"),
		projectionSpec(ActionPoolRestart, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceInput, ActionSurfaceHelp).withFooter("^R", "RESTART", StyleStatusAccent).withHelp("restart terminal"),
		projectionSpec(ActionPoolEdit, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("^E", "RENAME", StyleStatusAccent).withHelp("rename"),
		projectionSpec(ActionPoolKill, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("^K", "KILL", StyleStatusWarning).withHelp("kill").withDanger(),
		projectionSpec(ActionPoolDelete, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceInput, ActionSurfaceHelp).withFooter("^X", "REMOVE", StyleStatusWarning).withHelp("remove terminal inventory entry").withDanger(),
		projectionSpec(ActionWorkbenchSelect, ActionSurfaceFooter, ActionSurfaceContent).withFooter("focus", "", StyleStatusAccent).withHelp("focus"),
		projectionSpec(ActionWorkbenchOpen, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("open", "", StyleStatusAccent).withHelp("open"),
		projectionSpec(ActionWorkbenchRename, ActionSurfaceContent, ActionSurfaceHelp).withHelp("rename"),
		projectionSpec(ActionWorkbenchNew, ActionSurfaceContent, ActionSurfaceHelp).withHelp("new"),
		projectionSpec(ActionWorkbenchDelete, ActionSurfaceContent).withHelp("delete").withDanger(),
		projectionSpec(ActionWorkbenchDetach, ActionSurfaceInput, ActionSurfaceHelp).withHelp("detach pane"),
		projectionSpec(ActionWorkbenchZoom, ActionSurfaceInput, ActionSurfaceHelp).withHelp("zoom pane"),
		projectionSpec(ActionClipboardHistoryOpen, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("H", "CLIPBOARD", StyleStatusAccent).withHelp("open clipboard history"),
		projectionSpec(ActionClipboardHistorySelect, ActionSurfaceContent, ActionSurfaceHelp).withHelp("select clipboard entry"),
		projectionSpec(ActionClipboardHistoryPaste, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("enter", "paste", StyleStatusAccent).withHelp("paste clipboard entry"),
		projectionSpec(ActionClipboardHistoryNew, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("new", "", StyleStatusAccent).withHelp("new clipboard entry"),
		projectionSpec(ActionClipboardHistoryEdit, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("edit", "", StyleStatusAccent).withHelp("edit clipboard entry"),
		projectionSpec(ActionClipboardHistoryDelete, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("delete", "", StyleStatusWarning).withHelp("delete clipboard entry").withDanger(),
		projectionSpec(ActionClipboardHistoryDividerDrag, ActionSurfaceContent).withHelp("resize clipboard columns"),
		projectionSpec(ActionPromptSubmit, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("enter", "submit", StyleStatusAccent).withHelp("submit"),
		projectionSpec(ActionPromptCancel, ActionSurfaceFooter, ActionSurfaceContent, ActionSurfaceHelp).withFooter("esc", "cancel", StyleStatusWarning).withHelp("cancel"),
		projectionSpec(ActionPromptOpen, ActionSurfaceFooter, ActionSurfaceInput).withHelp("open prompt"),
		projectionSpec(ActionHelpClose, ActionSurfaceFooter, ActionSurfaceHelp, ActionSurfaceContent).withFooter("enter", "close", StyleStatusAccent).withHelp("close"),
		projectionSpec(ActionHelpOpen, ActionSurfaceFooter, ActionSurfaceInput).withFooter("?", "HELP", StyleStatusAccent).withHelp("open help"),
		projectionSpec(ActionShortcutExit, ActionSurfaceFooter, ActionSurfaceHelp).withFooter("esc", "BACK", StyleStatusWarning).withHelp("exit current shortcut scene"),
	}
}

// ProjectionByID 返回 render-local 投影；动态 glyph 只改变视觉文本，不改变 canonical action 引用。
func ProjectionByID(id ProjectionID) (ProjectionSpec, bool) {
	spec, ok := projectionByIDCatalog[id]
	if !ok {
		return ProjectionSpec{}, false
	}
	return projectionWithCurrentGlyph(spec), true
}

var projectionByIDCatalog = buildProjectionByIDCatalog()

func buildProjectionByIDCatalog() map[ProjectionID]ProjectionSpec {
	specs := ProjectionCatalog()
	out := make(map[ProjectionID]ProjectionSpec, len(specs))
	for _, spec := range specs {
		if spec.CanonicalActionID != "" {
			if _, ok := actiondomain.SpecByID(spec.CanonicalActionID); !ok {
				panic("render projection references unknown canonical action " + spec.CanonicalActionID.String())
			}
		}
		out[spec.ID] = spec
	}
	return out
}

func projectionWithCurrentGlyph(spec ProjectionSpec) ProjectionSpec {
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
	case ActionFloatingCenter:
		spec.ChromeGlyph = paneChromeFloatingCenterGlyph()
	case ActionFloatingCollapse:
		spec.ChromeGlyph = paneChromeFloatingCollapseGlyph()
	case ActionTerminalTakeResizeOwner:
		spec.ChromeGlyph = paneChromeTakeOwnerText()
	}
	return spec
}

// ProjectionByIDString 供尚未迁移的 HitRegion/VM 字符串字段查询投影；它不解析 canonical action alias。
func ProjectionByIDString(id string) (ProjectionSpec, bool) {
	return ProjectionByID(ProjectionID(id))
}

// ProjectionActionIDs 返回 render-local projection 清单，供 KS016 前的 surface 完备性审计使用。
// app 不得把该清单当作 handler registry；执行身份只能来自 ProjectionSpec.CanonicalActionID 或 Invocation。
func ProjectionActionIDs() []ProjectionID {
	specs := ProjectionCatalog()
	out := make([]ProjectionID, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec.ID)
	}
	return out
}
