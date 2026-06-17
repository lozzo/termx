package render

import (
	"fmt"
	"strings"
	"time"

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
		Visible:         shell.HeaderVisible,
		Workspace:       shell.Workspace.Name,
		Tab:             tabStripSummary(shell),
		Tabs:            buildHeaderTabVMs(shell),
		ActivePane:      shell.ActivePaneID,
		TerminalSummary: terminalSummary(root),
		FloatingSummary: floatingSummary(shell),
		Notice:          notice,
		Title:           shell.Workspace.Name,
	}
}

func buildFooterVM(root state.Root, content ContentVM) FooterVM {
	shell := root.Shell.EnsureDefaults()
	mode := footerMode(root, shell)
	hint := content.Status
	if hint == "" {
		hint = activeViewLiveStatus(root, shell)
	}
	return FooterVM{
		Visible:       shell.FooterVisible,
		Mode:          mode,
		Hint:          hint,
		ActionTokens:  footerActionCatalogForRoot(mode, root, shell),
		ActiveTarget:  activeTargetSummary(shell, root),
		GlobalSummary: globalSummary(root, shell),
	}
}

func activeViewLiveStatus(root state.Root, shell state.ShellStore) string {
	var binding state.TerminalViewBinding
	var ok bool
	if shell.ActiveFloatingID != "" {
		binding, ok = root.TerminalViews.FloatingBinding(shell.ActiveFloatingID)
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
	if root.History.TerminalID != "" {
		ids[root.History.TerminalID] = struct{}{}
	}
	if root.CopyMode.TerminalID != "" {
		ids[root.CopyMode.TerminalID] = struct{}{}
	}
	return len(ids)
}

func floatingSummary(shell state.ShellStore) string {
	shell = shell.EnsureDefaults()
	return fmt.Sprintf("float:%d", len(shell.Floatings))
}

func footerMode(root state.Root, shell state.ShellStore) string {
	if shell.Overlay.Open {
		return string(shell.Overlay.Kind)
	}
	if shell.InteractionMode != state.InteractionModeNormal {
		return string(shell.InteractionMode)
	}
	if root.CopyMode.Active {
		return "copy"
	}
	return "live"
}

func footerActionLabels(mode string) []string {
	tokens := footerActionCatalog(mode)
	labels := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if strings.TrimSpace(token.Key) == "" {
			continue
		}
		label := strings.TrimSpace(token.Key)
		if token.Label != "" {
			label += " " + strings.TrimSpace(token.Label)
		}
		labels = append(labels, label)
	}
	return labels
}

// footerActions 只服务手写 FooterVM 的兼容 fallback；默认 builder 直接输出 FooterActionVM。
func footerActions(mode string) []string {
	return footerActionLabels(mode)
}

func footerActionCatalogForRoot(mode string, root state.Root, shell state.ShellStore) []FooterActionVM {
	tokens := footerActionCatalog(mode)
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
		return len(shell.EnsureDefaults().Workspaces) > 1
	case ActionFooterCloseToast, ActionFooterClearToasts:
		return len(shell.EnsureDefaults().Toasts) > 0
	case ActionFloatingSummon, ActionFloatingToggleAll, ActionFloatingShowAll, ActionFloatingCollapseAll:
		return len(shell.EnsureDefaults().Floatings) > 0
	case ActionFloatingTakeOwner, ActionFloatingFit, ActionFloatingAutoFit, ActionFloatingCenter, ActionFloatingCollapse, ActionFloatingClose:
		return shell.EnsureDefaults().ActiveFloatingID != "" || mode == string(state.OverlayFloatingOverview) && len(shell.EnsureDefaults().Floatings) > 0
	case ActionPoolAttach, ActionPoolEdit, ActionPoolKill:
		return len(root.TerminalPool.Items) > 0
	case ActionClipboardHistoryPaste, ActionClipboardHistoryEdit, ActionClipboardHistoryDelete:
		return len(state.ClipboardHistoryItems(root)) > 0
	default:
		return true
	}
}

func activeWorkspaceTabCount(shell state.ShellStore) int {
	shell = shell.EnsureDefaults()
	return len(shell.Workspace.Tabs)
}

func activeTabPaneCount(shell state.ShellStore) int {
	shell = shell.EnsureDefaults()
	activeTabID := shell.Workspace.ActiveTabID
	for _, tab := range shell.Workspace.Tabs {
		if activeTabID == "" || tab.ID == activeTabID {
			return len(tab.Panes)
		}
	}
	return 0
}

func footerActionCatalog(mode string) []FooterActionVM {
	switch mode {
	case "pane":
		return footerActionSpecs(
			footerActionFor(ActionPaneFooterFocus),
			footerActionFor(ActionPaneFooterSplitRight),
			footerActionFor(ActionPaneFooterSplitDown),
			footerActionFor(ActionPaneFooterDetach),
			footerActionFor(ActionPaneFooterZoom),
			footerActionFor(ActionPaneFooterBalance),
			footerActionFor(ActionPaneFooterCard),
			footerActionFor(ActionPaneFooterSplitLine),
			footerActionFor(ActionPaneFooterClose),
			footerActionSpec("esc", "", "", StyleStatusMuted),
		)
	case "resize":
		return footerActionSpecs(
			footerActionFor(ActionResizeLeft),
			footerActionFor(ActionResizeRight),
			footerActionFor(ActionResizeUp),
			footerActionFor(ActionResizeDown),
			footerActionFor(ActionResizeBalance),
			footerActionFor(ActionResizeLayoutLock),
			footerActionFor(ActionResizeLayoutToggle),
			footerActionFor(ActionResizeLayoutPan),
			footerActionFor(ActionResizeLayoutAlign),
			footerActionFor(ActionResizeLayoutCenter),
			footerActionFor(ActionResizeLayoutReset),
			footerActionSpec("esc", "", "", StyleStatusMuted),
		)
	case "global":
		return footerActionSpecs(
			footerActionFor(ActionFooterToggleHeader),
			footerActionFor(ActionFooterToggleFooter),
			footerActionFor(ActionHelpOpen),
			footerActionFor(ActionFooterOpenPool),
			footerActionFor(ActionFooterOpenTree),
			footerActionFor(ActionFooterQuit),
			footerActionSpec("esc", "", "", StyleStatusMuted),
		)
	case "copy":
		return footerActionSpecs(
			footerActionFor(ActionCopyOlder),
			footerActionSpec("wheel", "", "", StyleStatusAccent),
			footerActionSpec("esc", "", "", StyleStatusMuted),
		)
	case string(state.OverlayTerminalPicker):
		return footerActionSpecs(
			footerActionSpec("enter", "select", "", StyleStatusAccent),
			footerActionSpec("esc", "", "", StyleStatusMuted),
		)
	case string(state.OverlayTerminalPool):
		return footerActionSpecs(
			footerActionSpec("search", "", "", StyleStatusAccent),
			footerActionFor(ActionPoolAttach),
			footerActionFor(ActionPoolEdit),
			footerActionFor(ActionPoolKill),
			footerActionSpec("esc", "", "", StyleStatusMuted),
		)
	case string(state.OverlayWorkbenchTree):
		return footerActionSpecs(
			footerActionSpec("search", "", "", StyleStatusAccent),
			footerActionFor(ActionWorkbenchOpen),
			footerActionFor(ActionWorkbenchSelect),
			footerActionSpec("esc", "", "", StyleStatusMuted),
		)
	case string(state.OverlayClipboardHistory):
		return footerActionSpecs(
			footerActionSpec("search", "", "", StyleStatusAccent),
			footerActionSpec("↑/↓", "select", "", StyleStatusAccent),
			footerActionFor(ActionClipboardHistoryPaste),
			footerActionFor(ActionClipboardHistoryEdit),
			footerActionFor(ActionClipboardHistoryDelete),
			footerActionSpec("esc", "", "", StyleStatusMuted),
		)
	case string(state.OverlayFloatingOverview):
		return footerActionSpecs(
			footerActionSpec("↑/↓", "select", "", StyleStatusAccent),
			footerActionFor(ActionFloatingSummon),
			footerActionFor(ActionFloatingShowAll),
			footerActionFor(ActionFloatingCollapseAll),
			footerActionFor(ActionFloatingClose),
			footerActionSpec("enter", "OPEN", ActionFloatingSummon.String(), StyleStatusAccent),
			footerActionSpec("esc", "", "", StyleStatusMuted),
		)
	case string(state.OverlayPrompt):
		return footerActionSpecs(
			footerActionSpec("type", "", "", StyleStatusAccent),
			footerActionFor(ActionPromptSubmit),
		)
	case string(state.OverlayHelp):
		return footerActionSpecs(
			footerActionSpec("read", "", "", StyleStatusAccent),
			footerActionFor(ActionHelpClose),
			footerActionSpec("esc", "", "", StyleStatusMuted),
		)
	case "floating":
		return footerActionSpecs(
			footerActionFor(ActionFloatingNew),
			footerActionFor(ActionFloatingOverview),
			footerActionFor(ActionFloatingSummon),
			footerActionFor(ActionFloatingPick),
			footerActionFor(ActionFloatingTakeOwner),
			footerActionFor(ActionFloatingToggleAll),
			footerActionFor(ActionFloatingFit),
			footerActionFor(ActionFloatingAutoFit),
			footerActionFor(ActionFloatingCenter),
			footerActionFor(ActionFloatingCollapse),
			footerActionSpec("arrows", "move", "", StyleStatusAccent),
			footerActionSpec("HJKL", "size", "", StyleStatusWarning),
			footerActionFor(ActionFloatingClose),
			footerActionSpec("esc", "", "", StyleStatusMuted),
		)
	case "tab":
		return footerActionSpecs(
			footerActionFor(ActionTabCreate),
			footerActionSpec("1-9", "jump", "", StyleStatusAccent),
			footerActionFor(ActionTabPrevious),
			footerActionFor(ActionTabNext),
			footerActionFor(ActionTabRename),
			footerActionFor(ActionTabClose),
			footerActionSpec("esc", "", "", StyleStatusMuted),
		)
	case "workspace":
		return footerActionSpecs(
			footerActionFor(ActionFooterNewWorkspace),
			footerActionFor(ActionFooterPreviousWorkspace),
			footerActionFor(ActionFooterNextWorkspace),
			footerActionFor(ActionFooterRenameWorkspace),
			footerActionWithKey(ActionFooterOpenTree, "f", "PICK"),
			footerActionFor(ActionFooterDeleteWorkspace),
			footerActionSpec("esc", "", "", StyleStatusMuted),
		)
	default:
		return footerActionSpecs(
			footerActionFor(ActionFooterPaneMode),
			footerActionFor(ActionFooterResizeMode),
			footerActionFor(ActionFooterTabMode),
			footerActionFor(ActionFooterWorkspaceMode),
			footerActionFor(ActionFooterFloatingMode),
			footerActionFor(ActionFooterCopyMode),
			footerActionFor(ActionFooterPicker),
			footerActionFor(ActionFooterGlobalMode),
		)
	}
}

func footerActionFor(id ActionID) FooterActionVM {
	spec, ok := ActionSpecByID(id)
	if !ok {
		return FooterActionVM{ActionID: id.String()}
	}
	return footerActionFromSpec(spec)
}

func footerActionWithKey(id ActionID, key string, label string) FooterActionVM {
	action := footerActionFor(id)
	action.Key = key
	action.Label = label
	return action
}

func footerActionFromSpec(spec ActionSpec) FooterActionVM {
	return FooterActionVM{Key: spec.FooterKey, Label: spec.FooterLabel, ActionID: spec.ID.String(), Style: spec.FooterStyle}
}

func footerActionSpec(key string, label string, actionID string, style StyleToken) FooterActionVM {
	return FooterActionVM{Key: key, Label: label, ActionID: actionID, Style: style}
}

func footerActionSpecs(actions ...FooterActionVM) []FooterActionVM {
	out := make([]FooterActionVM, 0, len(actions))
	for _, action := range actions {
		if strings.TrimSpace(action.Key) == "" {
			continue
		}
		out = append(out, action)
	}
	return out
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
	return fmt.Sprintf("ws:%s %s terminals:%d", shell.Workspace.Name, floatingSummary(shell), terminalCount(root))
}

func tabStripSummary(shell state.ShellStore) string {
	shell = shell.EnsureDefaults()
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
	shell = shell.EnsureDefaults()
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
		surface = root.Surface.SurfaceForTerminal(binding.TerminalID)
		session = sessionForBinding(root, binding)
	}
	switch {
	case session.LastError != "" || surface.Err != "":
		return "error"
	case copyModeBelongsToPane(root.CopyMode, pane.ID, root.Shell.EnsureDefaults().ActivePaneID):
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
	shell = shell.EnsureDefaults()
	tab := activeTab(shell)
	floatingOwnsFocus := shell.ActiveFloatingID != ""
	if len(tab.Panes) == 0 {
		return nil
	}
	panels := make([]PanelVM, len(tab.Panes))
	for i, pane := range tab.Panes {
		active := pane.ID == shell.ActivePaneID
		content := projector.contentForPane(root, pane, activeContent, active)
		if active && !copyModeBelongsToPane(root.CopyMode, pane.ID, shell.ActivePaneID) {
			content = activeContent
		}
		content = contentWithPaneLayout(root, pane, content)
		panels[i] = PanelVM{
			ID:           pane.ID,
			Title:        activePaneTitle(pane),
			Presentation: renderPanelPresentation(shell.PanelPresentation),
			Active:       active && !floatingOwnsFocus,
			Content:      content,
			Chrome:       buildPanelChromeVM(root, pane, active && !floatingOwnsFocus, content),
		}
	}
	return panels
}

func (projector ShellProjector) buildZoomedPanelVMs(shell state.ShellStore, activeContent ContentVM, root state.Root) []PanelVM {
	shell = shell.EnsureDefaults()
	for _, pane := range activeTab(shell).Panes {
		if pane.ID == shell.ZoomedPaneID {
			content := projector.contentForPane(root, pane, activeContent, pane.ID == shell.ActivePaneID)
			return []PanelVM{{
				ID:           pane.ID,
				Title:        activePaneTitle(pane),
				Presentation: renderPanelPresentation(shell.PanelPresentation),
				Active:       true,
				Content:      content,
				Chrome:       buildPanelChromeVM(root, pane, true, content),
			}}
		}
	}
	return projector.buildPanelVMs(shell, activeContent, root)
}

func (projector ShellProjector) buildFloatingVMs(shell state.ShellStore, root state.Root) []FloatingVM {
	shell = shell.EnsureDefaults()
	if len(shell.Floatings) == 0 {
		return nil
	}
	out := make([]FloatingVM, 0, len(shell.Floatings))
	for _, floating := range shell.Floatings {
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
		ShowResizeHandle: true,
		Terminal:         terminalChromeVMForFloating(root, floating, content, style),
		Actions:          defaultFloatingChromeActionVMs(style),
	}
}

func buildPanelChromeVM(root state.Root, pane state.PaneState, active bool, content ContentVM) PanelChromeVM {
	style := StyleMuted
	if active {
		style = StyleAccent
	}
	actions := defaultPaneChromeActionVMs(style)
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
	ownerText := "◇ follow"
	ownerStyle := StyleMuted
	if root.Shell.EnsureDefaults().OwnerConfirm.ViewID == binding.ViewID {
		ownerText = "◆ owner?"
		ownerStyle = StyleWarning
	} else if binding.HasResizeOwner() {
		ownerText = "◆ owner"
		ownerStyle = StyleSuccess
	}
	title := terminalChromeTitle(root, pane, binding.TerminalID)
	layout := binding.Layout.Normalize()
	return TerminalChromeVM{
		Locked:       binding.SizeLocked,
		LayoutMode:   layout.Mode,
		PanX:         layout.PanX,
		PanY:         layout.PanY,
		AlignX:       layout.AlignX,
		AlignY:       layout.AlignY,
		Title:        ChromeSlotVM{Text: title, Style: style},
		State:        terminalChromeStateSlot(root, binding.TerminalID, active, content),
		AttachCount:  len(root.TerminalViews.BindingsForTerminal(binding.TerminalID)),
		Owner:        ChromeSlotVM{Text: ownerText, Style: ownerStyle},
		TakeOwner:    !binding.HasResizeOwner(),
		CanLockSize:  binding.HasResizeOwner(),
		ResizeRole:   role,
		CanResize:    binding.CanResize,
		TerminalID:   binding.TerminalID,
		TerminalView: binding.ViewID,
	}
}

func defaultFloatingChromeActionVMs(style StyleToken) []ChromeActionVM {
	return []ChromeActionVM{
		paneChromeActionVM(ActionFloatingCenter, style),
		paneChromeActionVM(ActionFloatingCollapse, style),
		paneChromeActionVM(ActionPaneZoom, style),
		paneChromeActionVM(ActionFloatingClose, style),
	}
}

func terminalChromeTitle(root state.Root, pane state.PaneState, terminalID string) string {
	for _, item := range root.TerminalPool.Items {
		if item.TerminalID != terminalID {
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
	return terminalID
}

func terminalChromeStateSlot(root state.Root, terminalID string, active bool, content ContentVM) ChromeSlotVM {
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
	surface := root.Surface.SurfaceForTerminal(terminalID)
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
	return []ChromeActionVM{
		paneChromeActionVM(ActionPaneZoom, style),
		paneChromeActionVM(ActionPaneSplitRight, style),
		paneChromeActionVM(ActionPaneSplitDown, style),
		paneChromeActionVM(ActionPaneClose, style),
	}
}

func paneChromeActionVM(id ActionID, style StyleToken) ChromeActionVM {
	spec, ok := ActionSpecByID(id)
	if !ok {
		return ChromeActionVM{ActionID: id.String(), Style: style}
	}
	return ChromeActionVM{Text: paneChromeBracketToken(spec.ChromeGlyph), ActionID: spec.ID.String(), Style: style}
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

func (projector ShellProjector) buildActiveContentVM(root state.Root) ContentVM {
	shell := root.Shell.EnsureDefaults()
	if copyModeFloatingActive(root.CopyMode, shell) {
		return projector.copyHistoryContent(root, shell, state.PaneState{}, true)
	}
	if pane, ok := shell.Pane(state.PaneCommandTarget{PaneID: shell.ActivePaneID}); ok {
		if copyModeBelongsToPane(root.CopyMode, pane.ID, shell.ActivePaneID) {
			return projector.copyHistoryContent(root, shell, pane, true)
		}
		if pane.Kind == state.PaneEmpty {
			return buildEmptyPaneContentWithSelection(pane, shell.EmptyPaneCTA.SelectedIndex)
		}
		if root.Session.LastError != "" && root.Session.TerminalID == "" {
			return projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Pane: pane, Kind: contentKindForPane(pane), Session: root.Session, Active: true})
		}
		surface, session := terminalContentStoresForPane(root, pane)
		return projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Pane: pane, Kind: contentKindForPane(pane), Surface: surface, Session: session, Active: true})
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
	if copyModeBelongsToPane(root.CopyMode, pane.ID, root.Shell.EnsureDefaults().ActivePaneID) {
		return projector.copyHistoryContent(root, root.Shell.EnsureDefaults(), pane, active)
	}
	if active {
		return activeContent
	}
	surface, session := terminalContentStoresForPane(root, pane)
	return projector.Content.Project(ContentProjectorContext{Root: root, Shell: root.Shell.EnsureDefaults(), Pane: pane, Kind: contentKindForPane(pane), Surface: surface, Session: session, Active: false})
}

func (projector ShellProjector) copyHistoryContent(root state.Root, shell state.ShellStore, pane state.PaneState, active bool) ContentVM {
	return projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Pane: pane, Kind: ContentCopyHistory, Active: active})
}

func (projector ShellProjector) contentForFloating(root state.Root, shell state.ShellStore, floating state.FloatingPaneState) ContentVM {
	if copyModeBelongsToFloating(root.CopyMode, floating.ID) {
		return projector.copyHistoryContent(root, shell, floating.Pane, floating.Active)
	}
	if floating.Active && floating.Pane.Kind == state.PaneEmpty {
		return buildEmptyPaneContentWithSelection(floating.Pane, shell.EmptyPaneCTA.SelectedIndex)
	}
	return projector.Content.Project(ContentProjectorContext{
		Root:    root,
		Shell:   shell,
		Pane:    floating.Pane,
		Kind:    contentKindForPane(floating.Pane),
		Active:  floating.Active,
		Surface: surfaceForFloating(root, floating.ID),
		Session: sessionForFloating(root, floating.ID),
	})
}

func copyModeBelongsToPane(copyMode state.CopyModeStore, paneID string, activePaneID string) bool {
	if !copyMode.Active || paneID == "" {
		return false
	}
	if copyMode.PaneID == "" {
		return paneID == activePaneID
	}
	return copyMode.PaneID == paneID
}

func copyModeBelongsToFloating(copyMode state.CopyModeStore, floatingID string) bool {
	return copyMode.Active && floatingID != "" && copyMode.ViewID == state.TerminalFloatingViewID(floatingID)
}

func copyModeFloatingActive(copyMode state.CopyModeStore, shell state.ShellStore) bool {
	if !copyMode.Active || copyMode.ViewID == "" || !strings.HasPrefix(copyMode.ViewID, "floating:") {
		return false
	}
	if shell.ActiveFloatingID == "" {
		return true
	}
	return copyMode.ViewID == state.TerminalFloatingViewID(shell.ActiveFloatingID)
}

func contentKindForPane(pane state.PaneState) ContentKind {
	switch pane.Kind {
	case state.PaneEmpty:
		return ContentEmptyPane
	case state.PaneExited:
		return ContentExitedPane
	case state.PaneCopyHistory:
		return ContentCopyHistory
	case state.PaneTerminalLive:
		return ContentTerminalLive
	default:
		return ContentPlaceholder
	}
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
	return root.Surface.SurfaceForTerminal(binding.TerminalID)
}

func sessionForBinding(root state.Root, binding state.TerminalViewBinding) state.TerminalSessionStore {
	if binding.TerminalID == "" {
		return state.TerminalSessionStore{}
	}
	surface := root.Surface.SurfaceForTerminal(binding.TerminalID)
	cols, rows := binding.DesiredCols, binding.DesiredRows
	if cols <= 0 {
		cols = surface.Cols
	}
	if rows <= 0 {
		rows = surface.Rows
	}
	session := state.TerminalSessionStore{
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
	return session
}

func canRenderCopyHistory(root state.Root, history state.HistoryStore, copyMode state.CopyModeStore) bool {
	return copyMode.Active &&
		copyMode.TerminalID != "" &&
		history.TerminalID != "" &&
		copyMode.BoundToken != "" &&
		copyMode.BoundCols != 0 &&
		history.Cols != 0 &&
		copyModeBindingStillValid(root, copyMode) &&
		copyMode.BoundToken == history.Token &&
		copyMode.BoundCols == history.Cols &&
		copyMode.TerminalID == history.TerminalID &&
		len(history.Rows) > 0
}

func copyHistoryPendingReason(root state.Root, history state.HistoryStore, copyMode state.CopyModeStore) string {
	switch {
	case !copyMode.Active:
		return ""
	case copyMode.TerminalID == "":
		return "copy history pending: terminal binding missing"
	case !copyModeBindingStillValid(root, copyMode):
		return "copy history pending: copy binding missing"
	case copyMode.BoundToken == "":
		return "copy history pending: window pending"
	case copyMode.BoundCols == 0:
		return "copy history pending: bound cols missing"
	case history.TerminalID == "":
		return "copy history pending: window pending"
	case copyMode.TerminalID != history.TerminalID:
		return "copy history error: terminal mismatch"
	case copyMode.BoundToken != history.Token:
		return "copy history pending: stale history token"
	case history.Cols == 0:
		return "copy history pending: history cols missing"
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
	return ContentVM{
		Kind:       ContentCopyHistory,
		Lines:      copyHistoryLines(history, copyMode),
		Status:     copyHistoryStatus(history, copyMode),
		Cursor:     copyHistoryCursor(history, copyMode),
		HitRegions: copyHistoryHitRegions(history, copyMode),
	}
}

func copyModeBindingStillValid(root state.Root, copyMode state.CopyModeStore) bool {
	if !copyMode.Active {
		return false
	}
	if copyMode.ViewID != "" {
		if binding, ok := root.TerminalViews.Views[copyMode.ViewID]; ok {
			return binding.TerminalID == copyMode.TerminalID
		}
		return false
	}
	if copyMode.PaneID != "" {
		if _, ok := root.Shell.EnsureDefaults().Pane(state.PaneCommandTarget{PaneID: copyMode.PaneID}); ok {
			return true
		}
		return false
	}
	return true
}

func buildLiveContentVM(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) ContentVM {
	return buildLiveContentVMWithSelection(surface, session, 0)
}

func buildLiveContentVMWithSelection(surface state.TerminalSurfaceStore, session state.TerminalSessionStore, selectedIndex int) ContentVM {
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
	if session.LastError != "" {
		content.Error = session.LastError
	} else if surface.Err != "" {
		content.Error = surface.Err
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
	if !session.Attached || len(lines) == 0 {
		return Cursor{}
	}
	row := len(lines) - 1
	if surface.Rows > 0 && row >= surface.Rows {
		row = surface.Rows - 1
	}
	col := lines[len(lines)-1].Width()
	cols := surface.Cols
	if cols <= 0 {
		cols = session.Cols
	}
	if cols > 0 && col >= cols {
		col = cols - 1
	}
	if col < 0 {
		col = 0
	}
	return Cursor{Visible: true, Row: row, Col: col, Shape: CursorShapeBlock}
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
	switch shell.Overlay.Kind {
	case state.OverlayTerminalPicker:
		return OverlayVM{
			Kind:    OverlayTerminalPicker,
			Opaque:  false,
			Content: projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentTerminalPicker}),
		}
	case state.OverlayTerminalPool:
		return OverlayVM{
			Kind:    OverlayTerminalPool,
			Opaque:  true,
			Content: projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentTerminalPool}),
		}
	case state.OverlayWorkbenchTree:
		return OverlayVM{
			Kind:    OverlayWorkbenchTree,
			Opaque:  true,
			Content: projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentWorkbenchTree}),
		}
	case state.OverlayClipboardHistory:
		return OverlayVM{
			Kind:    OverlayClipboardHistory,
			Opaque:  true,
			Content: projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentClipboardHistory}),
		}
	case state.OverlayFloatingOverview:
		return OverlayVM{
			Kind:    OverlayFloatingOverview,
			Opaque:  true,
			Content: projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentFloatingOverview}),
		}
	case state.OverlayPrompt:
		return OverlayVM{
			Kind:    OverlayPrompt,
			Opaque:  true,
			Content: projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentPrompt}),
			Popup:   buildPromptSuggestionPopupVM(shell.Overlay.Prompt),
		}
	case state.OverlayHelp:
		return OverlayVM{Kind: OverlayHelp, Opaque: true, Content: projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentHelp})}
	default:
		return OverlayVM{}
	}
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
	shell = shell.EnsureDefaults()
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
	case state.PaneExited:
		return buildExitedPaneContent(state.Root{}, pane)
	case state.PaneCopyHistory:
		return ContentVM{Kind: ContentCopyHistory, Lines: []Line{NewLine(title + " copy pending")}, Pending: true}
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
