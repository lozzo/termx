package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-tui-v3/shortcut"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type RenderVM struct {
	Shell ShellVM
	Theme Theme
}

type HitRegionKind string

const (
	HitRegionHistoryRow    HitRegionKind = "history-row"
	HitRegionStatus        HitRegionKind = "status"
	HitRegionOverlay       HitRegionKind = "overlay"
	HitRegionToast         HitRegionKind = "toast"
	HitRegionToastClose    HitRegionKind = "toast-close"
	HitRegionPaneChrome    HitRegionKind = "pane-chrome"
	HitRegionPaneAction    HitRegionKind = "pane-action"
	HitRegionPaneResize    HitRegionKind = "pane-resize"
	HitRegionPaneContent   HitRegionKind = "pane-content"
	HitRegionContentAction HitRegionKind = "content-action"
)

type Rect struct {
	X int
	Y int
	W int
	H int
}

type ResizeGroupItem struct {
	PaneID    string
	Cells     int
	DeltaSign int
}

type HitRegion struct {
	Kind               HitRegionKind
	Rect               Rect
	LineID             uint64
	Row                int
	PaneID             string
	Floating           bool
	ActionID           string
	Invocation         shortcut.ActionInvocation
	Direction          string
	SplitPath          string
	ResizeBeforePaneID string
	ResizeAfterPaneID  string
	ResizeBeforeCells  int
	ResizeAfterCells   int
	ResizeGroup        []ResizeGroupItem
}

type RenderVMBuilder struct{}

func NewRenderVMBuilder() RenderVMBuilder {
	return RenderVMBuilder{}
}

func buildHeaderVM(shell state.ShellStore, root state.Root) HeaderVM {
	notice := root.Surface.Err
	if root.Session.LastError != "" {
		notice = root.Session.LastError
	}
	return HeaderVM{
		Visible:           shell.HeaderVisible,
		Workspace:         shell.Workspace.Name,
		Tab:               tabStripSummary(shell),
		Tabs:              buildHeaderTabVMs(shell),
		WorkspaceTemplate: root.Config.Chrome.WorkspaceTemplate,
		TabTemplate:       root.Config.Chrome.TabTemplate,
		TabCreateIcon:     root.Config.Chrome.TabCreateIcon,
		TabCreateTemplate: root.Config.Chrome.TabCreateTemplate,
		ActivePane:        shell.ActivePaneID,
		TerminalSummary:   terminalSummary(root),
		FloatingSummary:   floatingSummary(shell),
		Notice:            notice,
		Title:             shell.Workspace.Name,
	}
}

func buildFooterVM(root state.Root, shell state.ShellStore, content ContentVM) FooterVM {
	shell = shell.ReadonlyDefaults()
	mode := footerMode(root, shell)
	hint := content.Status
	if hint == "" {
		hint = activeViewLiveStatus(root, shell)
	}
	footerConfig := root.Config.Footer
	modeConfig := footerConfig.Modes[mode]
	return FooterVM{
		Visible:                          shell.FooterVisible,
		Mode:                             mode,
		ModeIcon:                         modeConfig.Icon,
		ModeLabel:                        modeConfig.Label,
		ModeStyle:                        footerStyleTokenFromConfig(modeConfig.Style),
		Hint:                             hint,
		ActionTokens:                     footerActionCatalogForRoot(mode, root, shell),
		KeyTemplate:                      footerKeyTemplateForConfig(root.Config),
		KeyTemplateSet:                   true,
		ActionTemplate:                   footerTemplateOrDefault(footerConfig.Templates.Action, defaultFooterActionTemplate),
		ModeBadgeTemplate:                footerTemplateOrDefault(footerConfig.Templates.ModeBadge, defaultFooterModeBadgeTemplate),
		ActionSeparator:                  footerConfig.Templates.Separator,
		WorkspaceSummaryTemplate:         footerTemplateOrDefault(footerConfig.Templates.WorkspaceSummary, defaultFooterWorkspaceSummaryTemplate),
		FloatingSummaryTemplate:          footerTemplateOrDefault(footerConfig.Templates.FloatingSummary, defaultFooterFloatingSummaryTemplate),
		FloatingCollapsedSummaryTemplate: footerTemplateOrDefault(footerConfig.Templates.FloatingCollapsedSummary, defaultFooterFloatingCollapsedSummaryTemplate),
		TerminalsSummaryTemplate:         footerTemplateOrDefault(footerConfig.Templates.TerminalsSummary, defaultFooterTerminalsSummaryTemplate),
		TabsSummaryTemplate:              footerTemplateOrDefault(footerConfig.Templates.TabsSummary, defaultFooterTabsSummaryTemplate),
		PanesSummaryTemplate:             footerTemplateOrDefault(footerConfig.Templates.PanesSummary, defaultFooterPanesSummaryTemplate),
		KeylockOnTemplate:                footerConfig.Templates.KeylockOn,
		KeylockOffTemplate:               footerConfig.Templates.KeylockOff,
		ActiveTarget:                     activeTargetSummary(shell, root),
		GlobalSummary:                    globalSummary(root, shell),
		FloatingSummaryOpen:              len(shell.ActiveFloatings()) > 0,
	}
}

const (
	defaultFooterActionTemplate                   = "{{key}} {{icon}} {{label}}"
	defaultFooterKeyTemplate                      = "{{key}}"
	defaultFooterModeBadgeTemplate                = "{{mode_icon}} {{mode_label}}"
	defaultFooterWorkspaceSummaryTemplate         = "ws:{{workspace}}"
	defaultFooterFloatingSummaryTemplate          = "float:{{count}}"
	defaultFooterFloatingCollapsedSummaryTemplate = "collapsed:{{count}}"
	defaultFooterTerminalsSummaryTemplate         = "terminals:{{count}}"
	defaultFooterTabsSummaryTemplate              = "tabs:{{count}}"
	defaultFooterPanesSummaryTemplate             = "panes:{{count}}"
)

func footerTemplateOrDefault(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func footerStyleTokenFromConfig(value string) StyleToken {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return StyleToken(value)
}

func footerKeyTemplateForConfig(cfg state.TUIConfigStore) string {
	if cfg.Version == 0 && strings.TrimSpace(cfg.Footer.Templates.Key) == "" {
		return defaultFooterKeyTemplate
	}
	return cfg.Footer.Templates.Key
}

func activeViewLiveStatus(root state.Root, shell state.ShellStore) string {
	var binding state.TerminalViewBinding
	var ok bool
	if activeFloatingID := shell.ActiveFloatingID(); activeFloatingID != "" {
		binding, ok = root.TerminalViews.FloatingBinding(activeFloatingID)
	} else {
		binding, ok = root.TerminalViews.PaneBinding(shell.ActivePaneID)
	}
	if !ok || binding.TerminalID == "" {
		return ""
	}
	return liveStatus(surfaceForBinding(root, binding), sessionForBinding(root, binding))
}

func terminalSummary(root state.Root) string {
	count := terminalCount(root)
	if count == 0 {
		return "term:0"
	}
	return fmt.Sprintf("term:%d", count)
}

func terminalCount(root state.Root) int {
	ids := map[string]struct{}{}
	for terminalID := range root.Surface.Surfaces {
		if terminalID != "" {
			ids[terminalID] = struct{}{}
		}
	}
	for _, binding := range root.TerminalViews.Views {
		if binding.TerminalID != "" {
			ids[binding.TerminalID] = struct{}{}
		}
	}
	if root.Surface.TerminalID != "" {
		ids[root.Surface.TerminalID] = struct{}{}
	}
	if root.Session.TerminalID != "" {
		ids[root.Session.TerminalID] = struct{}{}
	}
	for _, terminalID := range root.CopyHistoryTerminalIDs() {
		ids[terminalID] = struct{}{}
	}
	return len(ids)
}

func floatingSummary(shell state.ShellStore) string {
	shell = shell.ReadonlyDefaults()
	floatings := shell.ActiveFloatings()
	collapsed := 0
	for _, floating := range floatings {
		if floating.Collapsed {
			collapsed++
		}
	}
	if collapsed > 0 {
		return fmt.Sprintf("float:%d collapsed:%d", len(floatings), collapsed)
	}
	return fmt.Sprintf("float:%d", len(floatings))
}

func footerMode(root state.Root, shell state.ShellStore) string {
	if shell.Overlay.Open {
		return string(shell.Overlay.Kind)
	}
	if shell.InteractionMode != state.InteractionModeNormal {
		return string(shell.InteractionMode)
	}
	if root.HasActiveCopyMode() {
		return "copy"
	}
	return "live"
}

func footerActionCatalogForRoot(mode string, root state.Root, shell state.ShellStore) []FooterActionVM {
	tokens := footerActionCatalogFromShortcuts(mode, root)
	if len(tokens) == 0 {
		return tokens
	}
	out := make([]FooterActionVM, 0, len(tokens))
	for _, token := range tokens {
		if footerActionAvailable(token.ActionID, mode, root, shell) {
			out = append(out, token)
		}
	}
	return out
}

// 中文说明：这里仅过滤可见 footer token；reducer 仍负责最终语义校验和错误反馈。
func footerActionAvailable(actionID string, mode string, root state.Root, shell state.ShellStore) bool {
	if actionID == "" {
		return true
	}
	switch ActionID(actionID) {
	case ActionPaneFooterClose:
		return activeTabPaneCount(shell) > 1
	case ActionPaneFooterDetach:
		binding, ok := root.TerminalViews.PaneBinding(shell.ActivePaneID)
		return ok && binding.TerminalID != ""
	case ActionPaneFooterFocus:
		return activeTabPaneCount(shell) > 1
	case ActionPaneFooterBalance, ActionResizeBalance, ActionResizeLeft, ActionResizeRight, ActionResizeUp, ActionResizeDown:
		return activeTabPaneCount(shell) > 1
	case ActionTabPrevious, ActionTabNext, ActionTabClose:
		return activeWorkspaceTabCount(shell) > 1
	case ActionFooterPreviousWorkspace, ActionFooterNextWorkspace, ActionFooterDeleteWorkspace:
		return len(shell.ReadonlyDefaults().Workspaces) > 1
	case ActionFooterCloseToast, ActionFooterClearToasts:
		return len(shell.ReadonlyDefaults().Toasts) > 0
	case ActionFloatingSummon, ActionFloatingToggleAll, ActionFloatingShowAll, ActionFloatingCollapseAll:
		return len(shell.ActiveFloatings()) > 0
	case ActionFloatingTakeOwner, ActionFloatingFit, ActionFloatingAutoFit, ActionFloatingCenter, ActionFloatingCollapse, ActionFloatingClose:
		return shell.ActiveFloatingID() != "" || mode == string(state.OverlayFloatingOverview) && len(shell.ActiveFloatings()) > 0
	case ActionPoolAttach, ActionPoolAttachTab, ActionPoolAttachFloat, ActionPoolRestart, ActionPoolEdit, ActionPoolKill, ActionPoolDelete:
		if mode == string(state.OverlayTerminalPool) {
			return len(state.TerminalPoolPageItems(root)) > 0
		}
		return len(root.TerminalPool.Items) > 0
	case ActionClipboardHistoryPaste, ActionClipboardHistoryEdit, ActionClipboardHistoryDelete:
		return len(state.ClipboardHistoryItems(root)) > 0
	default:
		return true
	}
}

func activeWorkspaceTabCount(shell state.ShellStore) int {
	shell = shell.ReadonlyDefaults()
	return len(shell.Workspace.Tabs)
}

func activeTabPaneCount(shell state.ShellStore) int {
	shell = shell.ReadonlyDefaults()
	activeTabID := shell.Workspace.ActiveTabID
	for _, tab := range shell.Workspace.Tabs {
		if activeTabID == "" || tab.ID == activeTabID {
			return len(tab.Panes)
		}
	}
	return 0
}

func footerActionFor(id ActionID) FooterActionVM {
	spec, ok := ActionSpecByID(id)
	if !ok {
		return FooterActionVM{ActionID: id.String()}
	}
	return footerActionFromSpec(spec)
}

func footerActionFromSpec(spec ActionSpec) FooterActionVM {
	return FooterActionVM{Key: spec.FooterKey, Label: spec.FooterLabel, ActionID: spec.ID.String(), Style: spec.FooterStyle}
}

func activeTargetSummary(shell state.ShellStore, root state.Root) string {
	pane, ok := shell.Pane(state.PaneCommandTarget{PaneID: shell.ActivePaneID})
	paneTitle := shell.ActivePaneID
	if ok && pane.Title != "" {
		paneTitle = pane.Title
	}
	terminalState := terminalStateSummary(root, pane)
	if terminalState == "" {
		return "pane:" + paneTitle
	}
	return "pane:" + paneTitle + " " + terminalState
}

func globalSummary(root state.Root, shell state.ShellStore) string {
	summary := fmt.Sprintf("ws:%s %s terminals:%d", shell.Workspace.Name, floatingSummary(shell), terminalCount(root))
	if shell.ShortcutPassthroughLocked {
		summary += " keylock:on"
	}
	return summary
}

func tabStripSummary(shell state.ShellStore) string {
	shell = shell.ReadonlyDefaults()
	if len(shell.Workspace.Tabs) == 0 {
		return ""
	}
	out := ""
	for index, tab := range shell.Workspace.Tabs {
		if index > 0 {
			out += " "
		}
		title := tab.Title
		if title == "" {
			title = tab.ID
		}
		if tab.ID == shell.Workspace.ActiveTabID {
			out += "[" + title + "]"
		} else {
			out += title
		}
	}
	return out
}

func buildHeaderTabVMs(shell state.ShellStore) []HeaderTabVM {
	shell = shell.ReadonlyDefaults()
	tabs := shell.Workspace.Tabs
	if len(tabs) == 0 {
		return nil
	}
	out := make([]HeaderTabVM, 0, len(tabs))
	for index, tab := range tabs {
		title := tab.Title
		if title == "" {
			title = tab.ID
		}
		if title == "" {
			title = fmt.Sprintf("tab-%d", index+1)
		}
		out = append(out, HeaderTabVM{
			ID:            tab.ID,
			Title:         title,
			Index:         index + 1,
			Active:        tab.ID == shell.Workspace.ActiveTabID || (shell.Workspace.ActiveTabID == "" && index == 0),
			CloseActionID: ActionTabClose.String(),
			CloseTargetID: tab.ID,
		})
	}
	return out
}

func terminalStateSummary(root state.Root, pane state.PaneState) string {
	binding, hasBinding := root.TerminalViews.PaneBinding(pane.ID)
	surface := state.TerminalSurfaceStore{}
	session := state.TerminalSessionStore{}
	if hasBinding && binding.TerminalID != "" {
		surface = surfaceForBinding(root, binding)
		session = sessionForBinding(root, binding)
	}
	switch {
	case session.LastError != "" || surface.Err != "":
		return "error"
	case copyModeBelongsToPane(root, pane.ID):
		return "copy"
	case session.State == state.TerminalLiveExited || surface.State == state.TerminalLiveExited:
		return "exited"
	case hasBinding && binding.Attached:
		return "attached"
	case surface.TerminalID != "":
		return "live"
	case hasBinding && binding.TerminalID != "":
		return "bound"
	default:
		return ""
	}
}

func (projector ShellProjector) buildLayoutVM(shell state.ShellStore, activeContent ContentVM, root state.Root) LayoutVM {
	if shell.ZoomedPaneID != "" {
		return LayoutVM{
			Viewport:    viewportRect(root.Viewport),
			Panels:      projector.buildZoomedPanelVMs(shell, activeContent, root),
			BodyContent: activeContent,
			Floating:    projector.buildFloatingVMs(shell, root),
			Split:       SplitVM{PaneID: shell.ZoomedPaneID},
		}
	}
	return LayoutVM{
		Viewport:    viewportRect(root.Viewport),
		Panels:      projector.buildPanelVMs(shell, activeContent, root),
		BodyContent: activeContent,
		Floating:    projector.buildFloatingVMs(shell, root),
		Split:       buildSplitVM(activeTab(shell).RootSplit),
	}
}

func viewportRect(viewport state.ViewportStore) Rect {
	if !viewport.Valid {
		return Rect{}
	}
	return Rect{W: viewport.Cols, H: viewport.Rows}
}

func (projector ShellProjector) buildPanelVMs(shell state.ShellStore, activeContent ContentVM, root state.Root) []PanelVM {
	shell = shell.ReadonlyDefaults()
	tab := activeTab(shell)
	floatingOwnsFocus := shell.ActiveFloatingID() != ""
	if len(tab.Panes) == 0 {
		return nil
	}
	panels := make([]PanelVM, len(tab.Panes))
	for i, pane := range tab.Panes {
		active := pane.ID == shell.ActivePaneID
		focused := active && !floatingOwnsFocus
		content := projector.contentForPane(root, pane, activeContent, focused)
		if focused && !copyModeBelongsToPane(root, pane.ID) {
			content = activeContent
		}
		content = contentWithPaneLayout(root, pane, content)
		panels[i] = PanelVM{
			ID:           pane.ID,
			Title:        activePaneTitle(pane),
			Presentation: renderPanelPresentation(shell.PanelPresentation),
			Active:       focused,
			Content:      content,
			Chrome:       buildPanelChromeVM(root, pane, focused, content),
		}
	}
	return panels
}

func (projector ShellProjector) buildZoomedPanelVMs(shell state.ShellStore, activeContent ContentVM, root state.Root) []PanelVM {
	shell = shell.ReadonlyDefaults()
	floatingOwnsFocus := shell.ActiveFloatingID() != ""
	for _, pane := range activeTab(shell).Panes {
		if pane.ID == shell.ZoomedPaneID {
			focused := pane.ID == shell.ActivePaneID && !floatingOwnsFocus
			content := projector.contentForPane(root, pane, activeContent, focused)
			return []PanelVM{{
				ID:           pane.ID,
				Title:        activePaneTitle(pane),
				Presentation: renderPanelPresentation(shell.PanelPresentation),
				Active:       focused,
				IsZoomMode:   true,
				Content:      content,
				Chrome:       buildPanelChromeVMWithZoom(root, pane, focused, content, true),
			}}
		}
	}
	return projector.buildPanelVMs(shell, activeContent, root)
}

func (projector ShellProjector) buildFloatingVMs(shell state.ShellStore, root state.Root) []FloatingVM {
	shell = shell.ReadonlyDefaults()
	floatings := shell.ActiveFloatings()
	if len(floatings) == 0 {
		return nil
	}
	out := make([]FloatingVM, 0, len(floatings))
	for _, floating := range floatings {
		content := projector.contentForFloating(root, shell, floating)
		content = contentWithFloatingLayout(root, floating, content)
		out = append(out, FloatingVM{
			ID:        floating.ID,
			PaneID:    floating.Pane.ID,
			Title:     floating.Title,
			Rect:      Rect{X: floating.Rect.X, Y: floating.Rect.Y, W: floating.Rect.W, H: floating.Rect.H},
			Z:         floating.Z,
			Active:    floating.Active,
			Collapsed: floating.Collapsed,
			Chrome:    floatingChromeVM(root, floating, content),
			Content:   content,
		})
	}
	return out
}

func contentWithPaneLayout(root state.Root, pane state.PaneState, content ContentVM) ContentVM {
	if content.Kind != ContentTerminalLive {
		return content
	}
	binding, ok := root.TerminalViews.PaneBinding(pane.ID)
	if !ok {
		return content
	}
	content.Layout = contentLayoutVMFromState(binding.Layout)
	return content
}

func contentWithFloatingLayout(root state.Root, floating state.FloatingPaneState, content ContentVM) ContentVM {
	if content.Kind != ContentTerminalLive {
		return content
	}
	binding, ok := root.TerminalViews.FloatingBinding(floating.ID)
	if !ok {
		return content
	}
	content.Layout = contentLayoutVMFromState(binding.Layout)
	return content
}

func contentLayoutVMFromState(layout state.TerminalViewLayout) ContentLayoutVM {
	layout = layout.Normalize()
	return ContentLayoutVM{Known: true, Mode: layout.Mode, PanX: layout.PanX, PanY: layout.PanY, AlignX: layout.AlignX, AlignY: layout.AlignY}
}

func floatingChromeVM(root state.Root, floating state.FloatingPaneState, content ContentVM) FloatingChromeVM {
	style := StyleMuted
	if floating.Active {
		style = StyleAccent
	}
	return FloatingChromeVM{
		FillOverlay:      true,
		ShowResizeHandle: false,
		Terminal:         terminalChromeVMForFloating(root, floating, content, style),
		Actions:          defaultFloatingChromeActionVMs(style),
	}
}

func buildPanelChromeVM(root state.Root, pane state.PaneState, active bool, content ContentVM) PanelChromeVM {
	return buildPanelChromeVMWithZoom(root, pane, active, content, false)
}

func buildPanelChromeVMWithZoom(root state.Root, pane state.PaneState, active bool, content ContentVM, zoomMode bool) PanelChromeVM {
	style := StyleMuted
	if active {
		style = StyleAccent
	}
	actions := defaultPaneChromeActionVMsForZoom(style, zoomMode)
	terminal := terminalChromeVM(root, pane, active, content, style)
	var meta []ChromeSlotVM
	if active && content.Kind == ContentCopyHistory && content.Status != "" {
		meta = append(meta, ChromeSlotVM{Text: compactCopyHistoryChromeStatus(content.Status), Style: StyleMuted})
	}
	return PanelChromeVM{
		Title:    ChromeSlotVM{Text: activePaneTitle(pane), Style: style},
		State:    paneChromeStateSlot(active, content),
		Meta:     meta,
		Terminal: terminal,
		Actions:  actions,
	}
}

func compactCopyHistoryChromeStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return ""
	}
	if DisplayWidth(status) <= 42 {
		return status
	}
	return TruncateCells(status, 42)
}

func terminalChromeVM(root state.Root, pane state.PaneState, active bool, content ContentVM, style StyleToken) TerminalChromeVM {
	binding, ok := root.TerminalViews.PaneBinding(pane.ID)
	return terminalChromeVMFromBinding(root, pane, binding, ok, active, content, style)
}

func terminalChromeVMForFloating(root state.Root, floating state.FloatingPaneState, content ContentVM, style StyleToken) TerminalChromeVM {
	binding, ok := root.TerminalViews.FloatingBinding(floating.ID)
	pane := floating.Pane
	if strings.TrimSpace(pane.Title) == "" {
		pane.Title = floating.Title
	}
	return terminalChromeVMFromBinding(root, pane, binding, ok, floating.Active, content, style)
}

func terminalChromeVMFromBinding(root state.Root, pane state.PaneState, binding state.TerminalViewBinding, ok bool, active bool, content ContentVM, style StyleToken) TerminalChromeVM {
	if !ok || binding.TerminalID == "" {
		return TerminalChromeVM{}
	}
	role := binding.ResizeRole
	if role == "" {
		role = state.TerminalResizeRoleFollower
	}
	ownerText := paneChromeTakeOwnerText()
	ownerStyle := StyleMuted
	projectedOwner := binding.HasProjectedResizeOwner()
	if root.Shell.ReadonlyDefaults().OwnerConfirm.ViewID == binding.ViewID {
		ownerText = paneChromeOwnerPendingText()
		ownerStyle = StyleWarning
	} else if projectedOwner {
		ownerText = paneChromeOwnerText()
		ownerStyle = StyleSuccess
	} else if binding.HasResizeOwner() {
		// 中文说明：owner? 只属于鼠标首击后的 UI 确认态；attach/restore 阶段的本地 owner intent
		// 不能默认展示成中间确认态，否则用户会误以为自己触发了 take-owner。
		ownerText = paneChromeOwnerText()
		ownerStyle = StyleSuccess
	}
	title := terminalChromeTitle(root, pane, binding)
	layout := binding.Layout.Normalize()
	return TerminalChromeVM{
		Locked:       binding.SizeLocked,
		LayoutMode:   layout.Mode,
		PanX:         layout.PanX,
		PanY:         layout.PanY,
		AlignX:       layout.AlignX,
		AlignY:       layout.AlignY,
		Title:        ChromeSlotVM{Text: title, Style: style},
		State:        terminalChromeStateSlot(root, binding, active, content),
		AttachCount:  terminalChromeAttachCount(root, binding),
		Owner:        ChromeSlotVM{Text: ownerText, Style: ownerStyle},
		TakeOwner:    !binding.HasResizeOwner(),
		CanLockSize:  projectedOwner,
		ResizeRole:   role,
		CanResize:    binding.CanResize,
		TerminalID:   binding.TerminalID,
		TerminalView: binding.ViewID,
	}
}

func terminalChromeAttachCount(root state.Root, binding state.TerminalViewBinding) int {
	ref := binding.TerminalRef()
	if count := len(root.TerminalViews.BindingsForTerminalRef(ref)); count > 0 {
		return count
	}
	for _, item := range root.TerminalPool.Items {
		if item.TerminalRef().Equal(ref) && item.AttachmentCount > 0 {
			return item.AttachmentCount
		}
	}
	return 0
}

func defaultFloatingChromeActionVMs(style StyleToken) []ChromeActionVM {
	return []ChromeActionVM{
		paneChromeActionVM(ActionFloatingCenter, style),
		paneChromeActionVM(ActionFloatingCollapse, style),
		paneChromeActionVM(ActionPaneZoom, style),
		paneChromeActionVM(ActionFloatingClose, style),
	}
}

func terminalChromeTitle(root state.Root, pane state.PaneState, binding state.TerminalViewBinding) string {
	ref := binding.TerminalRef()
	title := terminalChromeDefaultTitle(root, pane, binding)
	if rendered, ok := renderTerminalChromeTitleTemplate(root.Config.Chrome.PaneTitleTemplate, terminalChromeTitleTemplateContext{
		Terminal:      title,
		TerminalTitle: title,
		TerminalID:    binding.TerminalID,
		Endpoint:      terminalChromeEndpointLabel(root, ref),
		EndpointLabel: terminalChromeEndpointLabel(root, ref),
		EndpointID:    string(ref.EndpointID),
		Pane:          strings.TrimSpace(pane.Title),
		PaneTitle:     strings.TrimSpace(pane.Title),
	}); ok && strings.TrimSpace(rendered) != "" {
		return strings.TrimSpace(rendered)
	}
	return title
}

func terminalChromeDefaultTitle(root state.Root, pane state.PaneState, binding state.TerminalViewBinding) string {
	ref := binding.TerminalRef()
	for _, item := range root.TerminalPool.Items {
		if !item.TerminalRef().Equal(ref) {
			continue
		}
		if title := strings.TrimSpace(item.Title); title != "" {
			return title
		}
		break
	}
	if title := strings.TrimSpace(pane.Title); title != "" {
		return title
	}
	return binding.TerminalID
}

func terminalChromeEndpointLabel(root state.Root, ref state.TerminalRef) string {
	if endpoint, ok := root.Endpoints.DisplayEndpoint(ref.EndpointID); ok {
		return endpoint.DisplayLabel()
	}
	ref = ref.Normalize()
	return string(ref.EndpointID)
}

func terminalChromeStateSlot(root state.Root, binding state.TerminalViewBinding, active bool, content ContentVM) ChromeSlotVM {
	style := StyleMuted
	if active {
		style = StyleSuccess
	}
	if content.Pending {
		style = StyleWarning
	}
	if content.Error != "" || content.Empty {
		style = StyleDanger
	}
	surface := surfaceForBinding(root, binding)
	if surface.State == state.TerminalLiveExited || surface.State == state.TerminalLiveError {
		style = StyleDanger
	}
	return ChromeSlotVM{Text: paneChromeRunningGlyph(), Style: style}
}

func paneChromeStateSlot(active bool, content ContentVM) ChromeSlotVM {
	switch {
	case content.Error != "":
		return ChromeSlotVM{Text: "error", Style: StyleDanger}
	case content.Pending:
		return ChromeSlotVM{Text: "pending", Style: StyleWarning}
	case content.Empty:
		return ChromeSlotVM{Text: "empty", Style: StyleMuted}
	case active:
		return ChromeSlotVM{Text: "active", Style: StyleSuccess}
	default:
		return ChromeSlotVM{Text: "idle", Style: StyleMuted}
	}
}

func defaultPaneChromeActionVMs(style StyleToken) []ChromeActionVM {
	return defaultPaneChromeActionVMsForZoom(style, false)
}

func defaultPaneChromeActionVMsForZoom(style StyleToken, zoomMode bool) []ChromeActionVM {
	if zoomMode {
		return []ChromeActionVM{
			paneChromeZoomActionVM(style, true),
			paneChromeActionVMWithZoomMode(ActionPaneClose, style, true),
		}
	}
	return []ChromeActionVM{
		paneChromeZoomActionVM(style, false),
		paneChromeActionVM(ActionPaneSplitRight, style),
		paneChromeActionVM(ActionPaneSplitDown, style),
		paneChromeActionVM(ActionPaneClose, style),
	}
}

func paneChromeActionVM(id ActionID, style StyleToken) ChromeActionVM {
	return paneChromeActionVMWithZoomMode(id, style, false)
}

func paneChromeActionVMWithZoomMode(id ActionID, style StyleToken, zoomMode bool) ChromeActionVM {
	spec, ok := ActionSpecByID(id)
	if !ok {
		return ChromeActionVM{ActionID: id.String(), Style: style, IsZoomMode: zoomMode}
	}
	return ChromeActionVM{Text: spec.ChromeGlyph, ActionID: spec.ID.String(), Label: spec.HelpLabel, Style: style, IsZoomMode: zoomMode}
}

func paneChromeZoomActionVM(style StyleToken, zoomMode bool) ChromeActionVM {
	action := paneChromeActionVMWithZoomMode(ActionPaneZoom, style, zoomMode)
	if zoomMode {
		action.Text = paneChromeUnzoomGlyph()
		action.Label = "unzoom"
	}
	return action
}

func splitActionLabel(action string) (string, string) {
	parts := strings.Fields(strings.TrimSpace(action))
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.Join(parts[1:], " ")
}

func (projector ShellProjector) buildActiveContentVM(root state.Root, shell state.ShellStore) ContentVM {
	shell = shell.ReadonlyDefaults()
	if floatingID := shell.ActiveFloatingID(); floatingID != "" && copyModeBelongsToFloating(root, floatingID) {
		return projector.copyHistoryContentForView(root, shell, copyHistoryViewIDForFloating(root, floatingID), state.PaneState{}, true)
	}
	if pane, ok := shell.Pane(state.PaneCommandTarget{PaneID: shell.ActivePaneID}); ok {
		if copyModeBelongsToPane(root, pane.ID) {
			return projector.copyHistoryContentForPane(root, shell, pane, true)
		}
		if pane.Kind == state.PaneEmpty && !paneHasTerminalBinding(root, pane.ID) {
			return buildEmptyPaneContentWithSelection(pane, shell.EmptyPaneCTA.SelectedIndex)
		}
		if root.Session.LastError != "" && root.Session.TerminalID == "" {
			return projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Pane: pane, Kind: contentKindForPaneRoot(root, pane), Session: root.Session, Active: true})
		}
		surface, session := terminalContentStoresForPane(root, pane)
		binding, _ := root.TerminalViews.PaneBinding(pane.ID)
		return projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Pane: pane, Binding: binding, Kind: contentKindForPaneRoot(root, pane), Surface: surface, Session: session, Active: true})
	}
	if len(shell.Workspace.Tabs) == 0 {
		return buildEmptyWorkspaceContent(shell.Workspace)
	}
	if tab := activeTab(shell); len(tab.Panes) == 0 {
		return buildEmptyTabContent(tab)
	}
	return projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentPlaceholder, Active: true})
}

func (projector ShellProjector) contentForPane(root state.Root, pane state.PaneState, activeContent ContentVM, active bool) ContentVM {
	if copyModeBelongsToPane(root, pane.ID) {
		return projector.copyHistoryContentForPane(root, root.Shell.ReadonlyDefaults(), pane, active)
	}
	if active {
		return activeContent
	}
	surface, session := terminalContentStoresForPane(root, pane)
	binding, _ := root.TerminalViews.PaneBinding(pane.ID)
	return projector.Content.Project(ContentProjectorContext{Root: root, Shell: root.Shell.ReadonlyDefaults(), Pane: pane, Binding: binding, Kind: contentKindForPaneRoot(root, pane), Surface: surface, Session: session, Active: false})
}

func (projector ShellProjector) copyHistoryContent(root state.Root, shell state.ShellStore, pane state.PaneState, active bool) ContentVM {
	return projector.copyHistoryContentForPane(root, shell, pane, active)
}

func (projector ShellProjector) copyHistoryContentForPane(root state.Root, shell state.ShellStore, pane state.PaneState, active bool) ContentVM {
	return projector.copyHistoryContentForView(root, shell, copyHistoryViewIDForPane(root, pane.ID), pane, active)
}

func (projector ShellProjector) copyHistoryContentForView(root state.Root, shell state.ShellStore, viewID string, pane state.PaneState, active bool) ContentVM {
	history, copyMode := root.CopyHistorySessionForView(viewID)
	if historyStoreBelongsToView(root.History, viewID) || copyModeStoreBelongsToView(root.CopyMode, viewID) {
		history = root.History
		copyMode = root.CopyMode
	} else if root.CopyMode.Active && root.CopyMode.ViewID == "" && root.CopyMode.PaneID == "" {
		history = root.History
		copyMode = root.CopyMode
	}
	return projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Pane: pane, Kind: ContentCopyHistory, Active: active, History: history, CopyMode: copyMode})
}

func (projector ShellProjector) contentForFloating(root state.Root, shell state.ShellStore, floating state.FloatingPaneState) ContentVM {
	if copyModeBelongsToFloating(root, floating.ID) {
		return projector.copyHistoryContentForView(root, shell, copyHistoryViewIDForFloating(root, floating.ID), floating.Pane, floating.Active)
	}
	if floating.Active && floating.Pane.Kind == state.PaneEmpty {
		return buildEmptyPaneContentWithSelection(floating.Pane, shell.EmptyPaneCTA.SelectedIndex)
	}
	return projector.Content.Project(ContentProjectorContext{
		Root:    root,
		Shell:   shell,
		Pane:    floating.Pane,
		Binding: terminalBindingForFloating(root, floating.ID),
		Kind:    contentKindForFloating(root, floating),
		Active:  floating.Active,
		Surface: surfaceForFloating(root, floating.ID),
		Session: sessionForFloating(root, floating.ID),
	})
}

func terminalBindingForFloating(root state.Root, floatingID string) state.TerminalViewBinding {
	binding, _ := root.TerminalViews.FloatingBinding(floatingID)
	return binding
}

func copyModeBelongsToPane(root state.Root, paneID string) bool {
	viewID := copyHistoryViewIDForPane(root, paneID)
	copyMode := copyModeForView(root, viewID)
	if !copyMode.HistoryRenderable() || paneID == "" {
		return false
	}
	if copyMode.PaneID == "" {
		return paneID == root.Shell.ReadonlyDefaults().ActivePaneID
	}
	return copyMode.PaneID == paneID
}

func copyModeBelongsToFloating(root state.Root, floatingID string) bool {
	viewID := copyHistoryViewIDForFloating(root, floatingID)
	copyMode := copyModeForView(root, viewID)
	return copyMode.HistoryRenderable() && floatingID != "" && copyMode.ViewID == viewID
}

func copyModeForView(root state.Root, viewID string) state.CopyModeStore {
	if copyModeStoreBelongsToView(root.CopyMode, viewID) {
		return root.CopyMode
	}
	if root.CopyMode.HistoryRenderable() && root.CopyMode.ViewID == "" && root.CopyMode.PaneID == "" {
		return root.CopyMode
	}
	_, copyMode := root.CopyHistorySessionForView(viewID)
	return copyMode
}

func copyModeStoreBelongsToView(copyMode state.CopyModeStore, viewID string) bool {
	if viewID == "" {
		return false
	}
	if copyMode.ViewID == viewID {
		return true
	}
	return copyMode.ViewID == "" && copyMode.PaneID != "" && state.TerminalPaneViewID(copyMode.PaneID) == viewID
}

func historyStoreBelongsToView(history state.HistoryStore, viewID string) bool {
	if viewID == "" {
		return false
	}
	if history.ViewID == viewID {
		return true
	}
	return history.ViewID == "" && history.PaneID != "" && state.TerminalPaneViewID(history.PaneID) == viewID
}

func copyHistoryViewIDForPane(root state.Root, paneID string) string {
	// 中文说明：真实 CLI / restore 里的 TerminalView ID 不一定等于 pane:<paneID>。
	// copy/history 会话必须按绑定的 ViewID 查找，否则最新/oldest 已写回但 pane 仍渲染旧会话。
	return root.TerminalViews.PaneViewID(paneID)
}

func copyHistoryViewIDForFloating(root state.Root, floatingID string) string {
	// 中文说明：floating 也可能拥有非派生 ViewID；必须和 pane 一样以 TerminalViewStore 为准。
	return root.TerminalViews.FloatingViewID(floatingID)
}

func copyModeIsFloating(root state.Root, copyMode state.CopyModeStore) bool {
	if copyMode.ViewID != "" {
		if binding, ok := root.TerminalViews.Views[copyMode.ViewID]; ok {
			return binding.FloatingID != ""
		}
	}
	return strings.HasPrefix(copyMode.ViewID, "floating:")
}

func contentKindForPane(pane state.PaneState) ContentKind {
	switch pane.Kind {
	case state.PaneEmpty:
		return ContentEmptyPane
	case state.PaneTerminalLive:
		return ContentTerminalLive
	default:
		return ContentPlaceholder
	}
}

func contentKindForPaneRoot(root state.Root, pane state.PaneState) ContentKind {
	if pane.Kind == state.PaneEmpty && paneHasTerminalBinding(root, pane.ID) {
		return ContentTerminalLive
	}
	return contentKindForPane(pane)
}

func contentKindForFloating(root state.Root, floating state.FloatingPaneState) ContentKind {
	if floating.Pane.Kind == state.PaneEmpty {
		if binding, ok := root.TerminalViews.FloatingBinding(floating.ID); ok && binding.TerminalID != "" {
			return ContentTerminalLive
		}
	}
	return contentKindForPane(floating.Pane)
}

func paneHasTerminalBinding(root state.Root, paneID string) bool {
	binding, ok := root.TerminalViews.PaneBinding(paneID)
	return ok && binding.TerminalID != ""
}

func terminalContentStoresForPane(root state.Root, pane state.PaneState) (state.TerminalSurfaceStore, state.TerminalSessionStore) {
	binding, ok := root.TerminalViews.PaneBinding(pane.ID)
	if !ok || binding.TerminalID == "" {
		return state.TerminalSurfaceStore{}, state.TerminalSessionStore{}
	}
	return surfaceForBinding(root, binding), sessionForBinding(root, binding)
}

func surfaceForFloating(root state.Root, floatingID string) state.TerminalSurfaceStore {
	binding, ok := root.TerminalViews.FloatingBinding(floatingID)
	if !ok || binding.TerminalID == "" {
		return state.TerminalSurfaceStore{}
	}
	return surfaceForBinding(root, binding)
}

func sessionForFloating(root state.Root, floatingID string) state.TerminalSessionStore {
	binding, ok := root.TerminalViews.FloatingBinding(floatingID)
	if !ok || binding.TerminalID == "" {
		return state.TerminalSessionStore{}
	}
	return sessionForBinding(root, binding)
}

func surfaceForBinding(root state.Root, binding state.TerminalViewBinding) state.TerminalSurfaceStore {
	if binding.TerminalID == "" {
		return state.TerminalSurfaceStore{}
	}
	return root.Surface.SurfaceForTerminalRef(binding.TerminalRef())
}

func sessionForBinding(root state.Root, binding state.TerminalViewBinding) state.TerminalSessionStore {
	if binding.TerminalID == "" {
		return state.TerminalSessionStore{}
	}
	ref := binding.TerminalRef()
	surface := root.Surface.SurfaceForTerminalRef(ref)
	cols, rows := binding.DesiredCols, binding.DesiredRows
	if cols <= 0 {
		cols = surface.Cols
	}
	if rows <= 0 {
		rows = surface.Rows
	}
	session := state.TerminalSessionStore{
		EndpointID:   ref.EndpointID,
		TerminalID:   binding.TerminalID,
		Channel:      binding.Channel,
		Attached:     binding.Attached,
		Cols:         cols,
		Rows:         rows,
		ResizePolicy: binding.ResizeRole,
		SurfaceID:    binding.SurfaceID,
		ViewID:       binding.ViewID,
		DesiredCols:  binding.DesiredCols,
		DesiredRows:  binding.DesiredRows,
		LastError:    binding.LastError,
		State:        surface.State,
		ExitCode:     surface.ExitCode,
		ExitReason:   surface.ExitReason,
		ExitedAt:     surface.ExitedAt,
		Command:      append([]string(nil), surface.Command...),
	}
	session = mergeSessionExitedLifecycle(session, root.Session)
	return session
}

func mergeSessionExitedLifecycle(session state.TerminalSessionStore, lifecycle state.TerminalSessionStore) state.TerminalSessionStore {
	if session.TerminalID == "" || lifecycle.TerminalID != session.TerminalID || lifecycle.State != state.TerminalLiveExited {
		return session
	}
	// TerminalSessionStore 是 input/live reducer 回投的 lifecycle truth；同一 terminal 的退出态必须
	// 覆盖仍 attached 的旧 surface 投影，否则渲染、hit region 和键盘 CTA 会看到不同状态。
	session.Attached = false
	session.State = state.TerminalLiveExited
	session.ExitCode = lifecycle.ExitCode
	session.ExitReason = lifecycle.ExitReason
	session.ExitedAt = lifecycle.ExitedAt
	session.Command = append([]string(nil), lifecycle.Command...)
	session.LastError = ""
	return session
}

func canRenderCopyHistory(root state.Root, history state.HistoryStore, copyMode state.CopyModeStore) bool {
	phase := copyMode.PhaseKind()
	tokenMatches := false
	switch phase {
	case state.CopyModeFrozenHistory:
		tokenMatches = copyMode.BoundToken != "" && copyMode.BoundToken == history.Token
	default:
		return false
	}
	return copyMode.HistoryRenderable() &&
		copyMode.TerminalID != "" &&
		history.TerminalID != "" &&
		copyMode.BoundCols != 0 &&
		history.Cols != 0 &&
		copyModeBindingStillValid(root, copyMode) &&
		tokenMatches &&
		copyMode.BoundCols == history.Cols &&
		copyMode.TerminalID == history.TerminalID &&
		len(history.Rows) > 0
}

func copyHistoryPendingReason(root state.Root, history state.HistoryStore, copyMode state.CopyModeStore) string {
	switch {
	case !copyMode.HistoryRenderable():
		return ""
	case copyMode.TerminalID == "":
		return "copy history pending: terminal binding missing"
	case !copyModeBindingStillValid(root, copyMode):
		return "copy history pending: copy binding missing"
	case copyMode.PhaseKind() == state.CopyModeFrozenHistory && copyMode.BoundToken == "":
		return "copy history pending: window pending"
	case copyMode.BoundCols == 0:
		return "copy history pending: bound cols missing"
	case history.TerminalID == "":
		return "copy history pending: window pending"
	case copyMode.TerminalID != history.TerminalID:
		return "copy history error: terminal mismatch"
	case copyMode.PhaseKind() == state.CopyModeFrozenHistory && copyMode.BoundToken != history.Token:
		return "copy history pending: stale history token"
	case history.Cols == 0:
		return "copy history pending: window pending"
	case copyMode.BoundCols != history.Cols:
		return "copy history pending: history cols changed"
	case len(history.Rows) == 0:
		return "copy history empty"
	default:
		return ""
	}
}

func buildCopyHistoryContentVM(root state.Root, history state.HistoryStore, copyMode state.CopyModeStore) ContentVM {
	if !canRenderCopyHistory(root, history, copyMode) {
		reason := copyHistoryPendingReason(root, history, copyMode)
		content := ContentVM{
			Kind:    ContentCopyHistory,
			Lines:   []Line{NewLine(reason)},
			Status:  copyHistoryStatus(history, copyMode),
			Pending: true,
		}
		if reason == "copy history empty" {
			content.Pending = false
			content.Empty = true
		}
		if reason == "copy history error: terminal mismatch" {
			content.Pending = false
			content.Error = reason
		}
		return content
	}
	content := ContentVM{
		Kind:   ContentCopyHistory,
		Lines:  copyHistoryLines(history, copyMode),
		Status: copyHistoryStatus(history, copyMode),
	}
	if copyMode.CanSelect() {
		content.Cursor = copyHistoryCursor(history, copyMode)
		content.HitRegions = copyHistoryHitRegions(history, copyMode)
	}
	return content
}

func copyModeBindingStillValid(root state.Root, copyMode state.CopyModeStore) bool {
	if !copyMode.HistoryRenderable() {
		return false
	}
	if copyMode.ViewID != "" {
		if binding, ok := root.TerminalViews.Views[copyMode.ViewID]; ok {
			return binding.TerminalID == copyMode.TerminalID
		}
		return false
	}
	if copyMode.PaneID != "" {
		if _, ok := root.Shell.ReadonlyDefaults().Pane(state.PaneCommandTarget{PaneID: copyMode.PaneID}); ok {
			return true
		}
		return false
	}
	return true
}

func buildLiveContentVM(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) ContentVM {
	return buildLiveContentVMWithSelection(surface, session, state.TerminalViewBinding{}, state.EndpointItem{}, 0)
}

func buildLiveContentVMWithSelection(surface state.TerminalSurfaceStore, session state.TerminalSessionStore, binding state.TerminalViewBinding, endpoint state.EndpointItem, selectedIndex int) ContentVM {
	lines := terminalLiveLineVMs(surface)
	content := ContentVM{
		Kind:   ContentTerminalLive,
		Lines:  lines,
		Status: liveStatus(surface, session),
		Cursor: liveContentCursor(surface, session, lines),
	}
	if len(lines) > 0 {
		content.Extent = liveContentExtent(surface, session)
	}
	if binding.AttachPending {
		return liveConnectingContent(surface, session, binding, endpoint, content.Lines)
	}
	if session.LastError != "" {
		content.Error = session.LastError
	} else if surface.Err != "" {
		content.Error = surface.Err
	}
	if liveContentIsDisconnected(surface, session) {
		return liveDisconnectedContent(surface, session, binding, endpoint, content.Lines, selectedIndex)
	}
	if len(content.Lines) == 0 {
		if session.State == state.TerminalLiveExited || surface.State == state.TerminalLiveExited {
			content = liveExitedContent(surface, session, nil, selectedIndex)
		} else if surface.Ready {
			content.Lines = []Line{NewLine("live surface empty")}
			content.Empty = true
			content.Cursor = liveEmptySurfaceCursor(surface, session)
		} else {
			content.Lines = []Line{NewLine("live surface pending")}
			content.Pending = true
			content.Cursor = Cursor{}
		}
	} else if session.State == state.TerminalLiveExited || surface.State == state.TerminalLiveExited {
		// 退出态是 terminal 内容流的尾部；viewport 负责看尾部，避免覆盖最后一屏历史。
		content = liveExitedContent(surface, session, content.Lines, selectedIndex)
	}
	return content
}

func liveContentIsDisconnected(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) bool {
	if session.State == state.TerminalLiveExited || surface.State == state.TerminalLiveExited {
		return false
	}
	kind := state.ClassifyEndpointErrorText(liveDisconnectedReason(surface, session))
	if kind == state.EndpointErrorUnknown || kind == state.EndpointErrorUnavailable {
		return false
	}
	if strings.TrimSpace(session.LastError) != "" && !session.Attached {
		return true
	}
	return strings.TrimSpace(surface.Err) != "" && surface.State == state.TerminalLiveError
}

func liveConnectingContent(surface state.TerminalSurfaceStore, session state.TerminalSessionStore, binding state.TerminalViewBinding, endpoint state.EndpointItem, previous []Line) ContentVM {
	ref := binding.TerminalRef()
	if ref.Empty() {
		ref = session.TerminalRef()
	}
	if ref.Empty() {
		ref = surface.TerminalRef()
	}
	lines := append([]Line(nil), previous...)
	if len(lines) > 0 {
		lines = append(lines, NewLine(""))
	}
	lines = append(lines,
		Line{Cells: []Cell{styledCell("● connecting", StyleWarning)}},
		Line{Cells: []Cell{styledCell("endpoint  ", StyleMuted), styledCell(endpointDisplayLabel(endpoint, ref.EndpointID), StyleAccent)}},
		Line{Cells: []Cell{styledCell("transport ", StyleMuted), styledCell(endpointTransportLabel(endpoint), StyleForeground)}},
		Line{Cells: []Cell{styledCell("terminal  ", StyleMuted), styledCell(ref.TerminalID, StyleForeground)}},
		Line{Cells: []Cell{styledCell("Establishing a new endpoint session…", StyleMuted)}},
	)
	return ContentVM{Kind: ContentTerminalLive, Lines: lines, Status: "connecting endpoint", Pending: true, Cursor: Cursor{}}
}

func liveDisconnectedContent(surface state.TerminalSurfaceStore, session state.TerminalSessionStore, binding state.TerminalViewBinding, endpoint state.EndpointItem, previous []Line, selectedIndex int) ContentVM {
	if selectedIndex < 0 || selectedIndex >= len(liveDisconnectedActions()) {
		selectedIndex = 0
	}
	ref := session.TerminalRef()
	if ref.Empty() {
		ref = surface.TerminalRef()
	}
	reason := liveDisconnectedReason(surface, session)
	kind := state.ClassifyEndpointErrorText(reason)
	reason = endpointErrorMessageWithoutKind(kind, reason)
	lines := make([]Line, 0, len(previous)+6)
	if len(previous) > 0 {
		lines = append(lines, previous...)
		lines = append(lines, NewLine(""))
	}
	lines = append(lines,
		Line{Cells: []Cell{styledCell("● endpoint disconnected", StyleDanger)}},
		Line{Cells: []Cell{styledCell("endpoint  ", StyleMuted), styledCell(endpointDisplayLabel(endpoint, ref.EndpointID), StyleAccent)}},
		Line{Cells: []Cell{styledCell("transport ", StyleMuted), styledCell(endpointTransportLabel(endpoint), StyleForeground)}},
		Line{Cells: []Cell{styledCell("terminal  ", StyleMuted), styledCell(ref.TerminalID, StyleForeground)}},
	)
	if reason != "" {
		lines = append(lines, Line{Cells: []Cell{styledCell("reason ", StyleMuted), styledCell(endpointErrorLabel(kind, reason), StyleWarning)}})
	}
	actionOffset := len(lines)
	actionLines, regions := liveDisconnectedActionLines(selectedIndex)
	lines = append(lines, actionLines...)
	for index := range regions {
		regions[index].Rect.Y += actionOffset
	}
	errorLabel := endpointErrorLabel(kind, reason)
	if errorLabel == "" {
		errorLabel = "endpoint disconnected"
	}
	return ContentVM{
		Kind:       ContentTerminalLive,
		Lines:      lines,
		Status:     "disconnected: Reconnect / Disconnect",
		Error:      errorLabel,
		Cursor:     Cursor{},
		HitRegions: regions,
	}
}

func endpointDisplayLabel(endpoint state.EndpointItem, endpointID state.EndpointID) string {
	if endpoint.ID != "" {
		return endpoint.DisplayLabel() + " (" + string(endpoint.ID) + ")"
	}
	return string(state.NormalizeEndpointID(endpointID))
}

func endpointTransportLabel(endpoint state.EndpointItem) string {
	if endpoint.Transport == "" {
		return "unknown"
	}
	return string(endpoint.Transport)
}

func liveDisconnectedReason(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) string {
	if strings.TrimSpace(session.LastError) != "" {
		return strings.TrimSpace(session.LastError)
	}
	return strings.TrimSpace(surface.Err)
}

func liveDisconnectedRefLabel(ref state.TerminalRef) string {
	ref = ref.Normalize()
	if ref.TerminalID == "" {
		return "unknown"
	}
	if ref.EndpointID == "" || ref.EndpointID == state.DefaultEndpointID {
		return ref.TerminalID
	}
	return string(ref.EndpointID) + "/" + ref.TerminalID
}

func endpointErrorMessageWithoutKind(kind state.EndpointErrorKind, message string) string {
	kind = state.NormalizeEndpointErrorKind(kind)
	message = strings.TrimSpace(message)
	if kind == state.EndpointErrorUnknown || message == "" {
		return message
	}
	prefix := string(kind) + ":"
	if strings.HasPrefix(message, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(message, prefix))
	}
	return message
}

func liveExitedContent(surface state.TerminalSurfaceStore, session state.TerminalSessionStore, previous []Line, selectedIndex int) ContentVM {
	exitLines, regions := liveExitedContentLines(surface, session, selectedIndex)
	if liveLinesContainExitMarker(previous, surface, session) {
		exitLines, regions = liveExitedActionLines(selectedIndex)
	}
	lines := make([]Line, 0, len(previous)+len(exitLines)+1)
	if len(previous) > 0 {
		lines = append(lines, previous...)
		if len(exitLines) > 0 {
			lines = append(lines, NewLine(""))
		}
	}
	actionOffset := len(lines)
	lines = append(lines, exitLines...)
	if actionOffset > 0 {
		for index := range regions {
			regions[index].Rect.Y += actionOffset
		}
	}
	return ContentVM{
		Kind:       ContentExitedPane,
		Lines:      lines,
		Status:     liveStatus(surface, session),
		Empty:      len(previous) == 0,
		Cursor:     Cursor{},
		HitRegions: regions,
	}
}

func liveExitedContentLines(surface state.TerminalSurfaceStore, session state.TerminalSessionStore, selectedIndex int) ([]Line, []HitRegion) {
	lines := []Line{liveExitedLine(surface, session)}
	if exitedAt := liveExitedAt(surface, session); !exitedAt.IsZero() {
		lines = append(lines, NewLine("exited at: "+exitedAt.UTC().Format(time.RFC3339)))
	}
	if command := liveExitCommand(surface, session); command != "" {
		lines = append(lines, NewLine("command: "+command))
	}
	actions := liveExitedActions()
	if selectedIndex < 0 || selectedIndex >= len(actions) {
		selectedIndex = 0
	}
	regions := make([]HitRegion, 0, len(actions))
	for index, action := range actions {
		selected := index == selectedIndex
		text := emptyPaneActionLabel(action.Label, selected)
		line := centeredStyledLine(text, action.Style)
		lines = append(lines, line)
		regions = append(regions, HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: len(lines) - 1, W: DisplayWidth(text), H: 1}, ActionID: action.ID.String()})
	}
	return lines, regions
}

func liveExitedActionLines(selectedIndex int) ([]Line, []HitRegion) {
	actions := liveExitedActions()
	if selectedIndex < 0 || selectedIndex >= len(actions) {
		selectedIndex = 0
	}
	lines := make([]Line, 0, len(actions))
	regions := make([]HitRegion, 0, len(actions))
	for index, action := range actions {
		selected := index == selectedIndex
		text := emptyPaneActionLabel(action.Label, selected)
		line := centeredStyledLine(text, action.Style)
		lines = append(lines, line)
		regions = append(regions, HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: len(lines) - 1, W: DisplayWidth(text), H: 1}, ActionID: action.ID.String()})
	}
	return lines, regions
}

func liveDisconnectedActionLines(selectedIndex int) ([]Line, []HitRegion) {
	actions := liveDisconnectedActions()
	if selectedIndex < 0 || selectedIndex >= len(actions) {
		selectedIndex = 0
	}
	lines := make([]Line, 0, len(actions))
	regions := make([]HitRegion, 0, len(actions))
	for index, action := range actions {
		selected := index == selectedIndex
		text := emptyPaneActionLabel(action.Label, selected)
		line := centeredStyledLine(text, action.Style)
		lines = append(lines, line)
		regions = append(regions, HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: len(lines) - 1, W: DisplayWidth(text), H: 1}, ActionID: action.ID.String()})
	}
	return lines, regions
}

func liveLinesContainExitMarker(lines []Line, surface state.TerminalSurfaceStore, session state.TerminalSessionStore) bool {
	if len(lines) == 0 {
		return false
	}
	prefix := "terminal exited"
	if terminalID := liveTerminalID(surface, session); terminalID != "" {
		prefix += ": " + terminalID
	}
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line.PlainString()), prefix) {
			return true
		}
	}
	return false
}

func liveContentExtent(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) ContentExtent {
	cols, rows := liveStatusSize(surface, session)
	if cols <= 0 || rows <= 0 {
		return ContentExtent{}
	}
	return ContentExtent{Known: true, Cols: cols, Rows: rows}
}

func liveStatus(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) string {
	status := "live"
	if surface.TerminalID != "" {
		status = "live: " + surface.TerminalID
	} else if session.TerminalID != "" {
		status = "live: " + session.TerminalID
	}
	if session.Attached || surface.State == state.TerminalLiveAttached {
		cols, rows := liveStatusSize(surface, session)
		if cols > 0 && rows > 0 {
			status += fmt.Sprintf(" attached %dx%d", cols, rows)
		} else {
			status += " attached"
		}
	}
	if session.State == state.TerminalLiveExited || surface.State == state.TerminalLiveExited {
		status = "exited: " + liveTerminalID(surface, session)
		if code, ok := liveExitCode(surface, session); ok {
			status += fmt.Sprintf(" code:%d", code)
		}
		if reason := liveExitReason(surface, session); reason != "" {
			status += " " + reason
		}
	}
	if session.LastError != "" {
		status = "error: " + session.LastError
	} else if surface.Err != "" {
		status = "error: " + surface.Err
	}
	return status
}

func liveExitedLine(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) Line {
	text := "terminal exited"
	if terminalID := liveTerminalID(surface, session); terminalID != "" {
		text += ": " + terminalID
	}
	if code, ok := liveExitCode(surface, session); ok {
		text += fmt.Sprintf(" code:%d", code)
	}
	if reason := liveExitReason(surface, session); reason != "" {
		text += " " + reason
	}
	return Line{Cells: []Cell{styledCell(text, StyleWarning)}}
}

func liveTerminalID(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) string {
	if surface.TerminalID != "" {
		return surface.TerminalID
	}
	return session.TerminalID
}

func liveExitCode(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) (int, bool) {
	switch {
	case session.State == state.TerminalLiveExited:
		return session.ExitCode, true
	case surface.State == state.TerminalLiveExited:
		return surface.ExitCode, true
	default:
		return 0, false
	}
}

func liveExitReason(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) string {
	if session.ExitReason != "" {
		return session.ExitReason
	}
	return surface.ExitReason
}

func liveExitedAt(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) time.Time {
	if !session.ExitedAt.IsZero() {
		return session.ExitedAt
	}
	return surface.ExitedAt
}

func liveExitCommand(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) string {
	if len(session.Command) > 0 {
		return strings.Join(session.Command, " ")
	}
	return strings.Join(surface.Command, " ")
}

func liveStatusSize(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) (int, int) {
	if surface.Cols > 0 && surface.Rows > 0 {
		return surface.Cols, surface.Rows
	}
	return session.Cols, session.Rows
}

func liveContentCursor(surface state.TerminalSurfaceStore, session state.TerminalSessionStore, lines []Line) Cursor {
	if session.LastError != "" || surface.Err != "" {
		return Cursor{}
	}
	if surface.Cursor.Visible {
		return Cursor{
			Visible: true,
			Row:     maxInt(0, surface.Cursor.Row),
			Col:     maxInt(0, surface.Cursor.Col),
			Shape:   liveCursorShape(surface.Cursor.Shape),
		}
	}
	// 中文说明：live terminal 的光标位置只能来自 core/protocol 的 surface cursor。
	// restart 会保留旧 live tail 但清掉旧进程 cursor；不能按文本尾部臆造新光标。
	return Cursor{}
}

func liveEmptySurfaceCursor(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) Cursor {
	if session.LastError != "" || surface.Err != "" || !session.Attached {
		return Cursor{}
	}
	if surface.Cursor.Visible {
		return Cursor{Visible: true, Row: maxInt(0, surface.Cursor.Row), Col: maxInt(0, surface.Cursor.Col), Shape: liveCursorShape(surface.Cursor.Shape)}
	}
	return Cursor{Visible: true, Row: 0, Col: 0, Shape: CursorShapeBlock}
}

func liveCursorShape(shape string) CursorShape {
	switch shape {
	case string(CursorShapeBar):
		return CursorShapeBar
	default:
		return CursorShapeBlock
	}
}

func (projector ShellProjector) buildOverlayVM(root state.Root, shell state.ShellStore) OverlayVM {
	if !shell.Overlay.Open {
		return OverlayVM{}
	}
	var overlay OverlayVM
	switch shell.Overlay.Kind {
	case state.OverlayTerminalPicker:
		overlay = OverlayVM{
			Kind:    OverlayTerminalPicker,
			Opaque:  false,
			Content: projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentTerminalPicker}),
		}
	case state.OverlayTerminalPool:
		overlay = OverlayVM{
			Kind:    OverlayTerminalPool,
			Opaque:  true,
			Content: projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentTerminalPool}),
		}
	case state.OverlayWorkbenchTree:
		overlay = OverlayVM{
			Kind:    OverlayWorkbenchTree,
			Opaque:  true,
			Content: projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentWorkbenchTree}),
		}
	case state.OverlayClipboardHistory:
		overlay = OverlayVM{
			Kind:    OverlayClipboardHistory,
			Opaque:  true,
			Content: projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentClipboardHistory}),
		}
	case state.OverlayFloatingOverview:
		overlay = OverlayVM{
			Kind:    OverlayFloatingOverview,
			Opaque:  true,
			Content: projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentFloatingOverview}),
		}
	case state.OverlayPrompt:
		overlay = OverlayVM{
			Kind:    OverlayPrompt,
			Opaque:  true,
			Content: projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentPrompt}),
			Popup:   buildPromptSuggestionPopupVM(shell.Overlay.Prompt),
		}
	case state.OverlayHelp:
		overlay = OverlayVM{Kind: OverlayHelp, Opaque: true, Content: projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentHelp})}
	default:
		return OverlayVM{}
	}
	overlay.Content.HitRegions = bindOverlayShortcutInvocations(overlay.Kind, overlay.Content.HitRegions, root.Config.Shortcuts)
	return overlay
}

func buildToastVMs(shell state.ShellStore) []ToastVM {
	if len(shell.Toasts) == 0 {
		return nil
	}
	toasts := make([]ToastVM, len(shell.Toasts))
	for i, toast := range shell.Toasts {
		toasts[i] = ToastVM{
			ID:       toast.ID,
			Severity: renderToastSeverity(toast.Severity),
			Title:    toast.Title,
			Body:     toast.Body,
			Pending:  toast.Pending,
		}
	}
	return toasts
}

func activeContent(shell ShellVM) ContentVM {
	for _, panel := range shell.Layout.Panels {
		if panel.Active {
			return panel.Content
		}
	}
	if len(shell.Layout.Panels) > 0 {
		return shell.Layout.Panels[0].Content
	}
	return ContentVM{}
}

func activeTab(shell state.ShellStore) state.TabState {
	shell = shell.ReadonlyDefaults()
	for _, tab := range shell.Workspace.Tabs {
		if tab.ID == shell.Workspace.ActiveTabID {
			return tab
		}
	}
	if len(shell.Workspace.Tabs) > 0 {
		return shell.Workspace.Tabs[0]
	}
	return state.TabState{}
}

func buildSplitVM(node state.SplitNode) SplitVM {
	out := SplitVM{
		PaneID:      node.PaneID,
		Direction:   renderSplitDirection(node.Direction),
		Ratio:       node.Ratio,
		BiasCells:   node.BiasCells,
		FixedPaneID: node.FixedPaneID,
		FixedCols:   node.FixedCols,
		FixedRows:   node.FixedRows,
	}
	if len(node.Children) > 0 {
		out.Children = make([]SplitVM, len(node.Children))
		for i, child := range node.Children {
			out.Children[i] = buildSplitVM(child)
		}
	}
	return out
}

func renderSplitDirection(direction state.SplitDirection) SplitDirection {
	switch direction {
	case state.SplitDirectionVertical:
		return SplitVertical
	case state.SplitDirectionHorizontal:
		return SplitHorizontal
	default:
		return ""
	}
}

func placeholderContentForPane(pane state.PaneState) ContentVM {
	title := activePaneTitle(pane)
	switch pane.Kind {
	case state.PaneEmpty:
		return buildEmptyPaneContent(pane)
	default:
		return ContentVM{Kind: ContentPlaceholder, Lines: []Line{NewLine(title + " inactive")}, Pending: true}
	}
}

func activePaneTitle(pane state.PaneState) string {
	if pane.Title != "" {
		return pane.Title
	}
	if pane.TerminalID != "" {
		return pane.TerminalID
	}
	if pane.ID != "" {
		return pane.ID
	}
	return "pane"
}

func renderPanelPresentation(presentation state.PanelPresentation) PanelPresentation {
	switch presentation {
	case state.PanelPresentationSplitLine:
		return PanelPresentationSplitLine
	default:
		return PanelPresentationCard
	}
}

func renderToastSeverity(severity state.ToastSeverity) ToastSeverity {
	switch severity {
	case state.ToastSuccess:
		return ToastSuccess
	case state.ToastWarning:
		return ToastWarning
	case state.ToastError:
		return ToastError
	default:
		return ToastInfo
	}
}

func terminalLiveLineVMs(surface state.TerminalSurfaceStore) []Line {
	if len(surface.Screen) > 0 {
		return terminalLiveLineVMsFromCells(surface.Screen)
	}
	if len(surface.Lines) == 0 {
		return nil
	}
	out := make([]Line, len(surface.Lines))
	for i, line := range surface.Lines {
		out[i] = terminalLiveLineFromANSI(line)
	}
	return out
}

func terminalLiveLineVMsFromCells(rows [][]state.LiveCell) []Line {
	if len(rows) == 0 {
		return nil
	}
	out := make([]Line, len(rows))
	for rowIndex, row := range rows {
		out[rowIndex] = terminalLiveLineFromCells(row)
	}
	return out
}

func terminalLiveLineFromCells(row []state.LiveCell) Line {
	if len(row) == 0 {
		return Line{}
	}
	cells := make([]Cell, 0, len(row))
	for _, liveCell := range row {
		text := SafeLine(liveCell.Text)
		width := liveCell.Width
		if width <= 0 {
			width = DisplayWidth(text)
		}
		if width <= 0 {
			continue
		}
		cells = append(cells, Cell{
			Text:            text,
			Width:           width,
			ANSIStyle:       terminalLiveANSIStyle(liveCell),
			LinkURL:         liveCell.LinkURL,
			LinkParams:      liveCell.LinkParams,
			TerminalContent: true,
			Safe:            true,
		})
	}
	return Line{Cells: cells}
}

func terminalLiveANSIStyle(cell state.LiveCell) ANSICellStyle {
	return ANSICellStyle{
		FG:            cell.FG,
		BG:            cell.BG,
		Bold:          cell.Bold,
		Italic:        cell.Italic,
		Underline:     cell.Underline,
		Blink:         cell.Blink,
		Reverse:       cell.Reverse,
		Strikethrough: cell.Strikethrough,
	}
}

func cloneHitRegions(regions []HitRegion) []HitRegion {
	if len(regions) == 0 {
		return nil
	}
	cloned := make([]HitRegion, len(regions))
	copy(cloned, regions)
	return cloned
}
