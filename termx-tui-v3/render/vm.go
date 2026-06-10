package render

import (
	"fmt"
	"strings"

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
		hint = liveStatus(root.Surface, root.Session)
	}
	return FooterVM{
		Visible:       shell.FooterVisible,
		Mode:          mode,
		Hint:          hint,
		ActionTokens:  footerActionCatalog(mode),
		ActiveTarget:  activeTargetSummary(shell, root),
		GlobalSummary: globalSummary(root, shell),
	}
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
	shell := root.Shell.EnsureDefaults()
	for _, tab := range shell.Workspace.Tabs {
		for _, pane := range tab.Panes {
			if pane.TerminalID != "" {
				ids[pane.TerminalID] = struct{}{}
			}
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

func footerActionCatalog(mode string) []FooterActionVM {
	switch mode {
	case "pane":
		return footerActionSpecs(
			footerActionFor(ActionPaneFooterSplit),
			footerActionFor(ActionPaneFooterClose),
			footerActionFor(ActionPaneFooterFocus),
			footerActionFor(ActionPaneFooterZoom),
			footerActionSpec("esc", "", "", StyleStatusMuted),
		)
	case "resize":
		return footerActionSpecs(
			footerActionFor(ActionResizeLeft),
			footerActionFor(ActionResizeRight),
			footerActionFor(ActionResizeUp),
			footerActionFor(ActionResizeDown),
			footerActionFor(ActionResizeBalance),
			footerActionSpec("esc", "", "", StyleStatusMuted),
		)
	case "global":
		return footerActionSpecs(
			footerActionFor(ActionFooterToggleHeader),
			footerActionFor(ActionFooterToggleFooter),
			footerActionFor(ActionFooterOpenPool),
			footerActionFor(ActionFooterOpenTree),
			footerActionFor(ActionFooterCloseToast),
			footerActionFor(ActionFooterClearToasts),
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
			footerActionWithKey(ActionFooterOpenTree, "t", "tree"),
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
	switch {
	case root.Session.LastError != "" || root.Surface.Err != "":
		return "error"
	case root.CopyMode.Active:
		return "copy"
	case root.Session.State == state.TerminalLiveExited || root.Surface.State == state.TerminalLiveExited:
		return "exited"
	case root.Session.Attached:
		return "attached"
	case root.Surface.TerminalID != "":
		return "live"
	case pane.TerminalID != "":
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
		if active {
			content = activeContent
		}
		panels[i] = PanelVM{
			ID:           pane.ID,
			Title:        activePaneTitle(pane),
			Presentation: renderPanelPresentation(shell.PanelPresentation),
			Active:       active && !floatingOwnsFocus,
			Content:      content,
			Chrome:       buildPanelChromeVM(pane, active && !floatingOwnsFocus, content, root.TerminalViews),
		}
	}
	return panels
}

func (projector ShellProjector) buildZoomedPanelVMs(shell state.ShellStore, activeContent ContentVM, root state.Root) []PanelVM {
	shell = shell.EnsureDefaults()
	for _, pane := range activeTab(shell).Panes {
		if pane.ID == shell.ZoomedPaneID {
			return []PanelVM{{
				ID:           pane.ID,
				Title:        activePaneTitle(pane),
				Presentation: renderPanelPresentation(shell.PanelPresentation),
				Active:       true,
				Content:      activeContent,
				Chrome:       buildPanelChromeVM(pane, true, activeContent, root.TerminalViews),
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
		content := projector.Content.Project(ContentProjectorContext{
			Root:    root,
			Shell:   shell,
			Pane:    floating.Pane,
			Kind:    contentKindForPane(floating.Pane),
			Active:  floating.Active,
			Surface: surfaceForPane(root, floating.Pane),
			Session: sessionForPane(root, floating.Pane),
		})
		out = append(out, FloatingVM{
			ID:        floating.ID,
			Title:     floating.Title,
			Rect:      Rect{X: floating.Rect.X, Y: floating.Rect.Y, W: floating.Rect.W, H: floating.Rect.H},
			Z:         floating.Z,
			Active:    floating.Active,
			Collapsed: floating.Collapsed,
			Chrome:    floatingChromeVM(floating),
			Content:   content,
		})
	}
	return out
}

func floatingChromeVM(floating state.FloatingPaneState) FloatingChromeVM {
	return FloatingChromeVM{FillOverlay: true, ShowResizeHandle: true}
}

func buildPanelChromeVM(pane state.PaneState, active bool, content ContentVM, views state.TerminalViewStore) PanelChromeVM {
	style := StyleMuted
	if active {
		style = StyleAccent
	}
	actions := defaultPaneChromeActionVMs(style)
	meta := terminalResizeOwnerMeta(pane.ID, views)
	if binding, ok := views.PaneBinding(pane.ID); ok && binding.ResizeRole != state.TerminalResizeRoleOwner {
		actions = append([]ChromeActionVM{paneChromeActionVM(ActionTerminalTakeResizeOwner, style)}, actions...)
	}
	return PanelChromeVM{
		Title:   ChromeSlotVM{Text: activePaneTitle(pane), Style: style},
		State:   paneChromeStateSlot(active, content),
		Meta:    meta,
		Actions: actions,
	}
}

func terminalResizeOwnerMeta(paneID string, views state.TerminalViewStore) []ChromeSlotVM {
	binding, ok := views.PaneBinding(paneID)
	if !ok || binding.TerminalID == "" {
		return nil
	}
	text := "size:" + binding.ResizeRole
	style := StyleMuted
	if binding.ResizeRole == state.TerminalResizeRoleOwner && binding.CanResize {
		style = StyleSuccess
	}
	return []ChromeSlotVM{{Text: text, Style: style}}
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
	if root.CopyMode.Active {
		return projector.Content.Project(ContentProjectorContext{Root: root, Shell: root.Shell.EnsureDefaults(), Kind: ContentCopyHistory})
	}
	shell := root.Shell.EnsureDefaults()
	if pane, ok := shell.Pane(state.PaneCommandTarget{PaneID: shell.ActivePaneID}); ok {
		return projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Pane: pane, Kind: contentKindForPane(pane), Surface: surfaceForPane(root, pane), Session: sessionForPane(root, pane), Active: true})
	}
	if len(shell.Workspace.Tabs) == 0 {
		return buildEmptyWorkspaceContent(shell.Workspace)
	}
	if tab := activeTab(shell); len(tab.Panes) == 0 {
		return buildEmptyTabContent(tab)
	}
	return projector.Content.Project(ContentProjectorContext{Root: root, Shell: shell, Kind: ContentTerminalLive, Surface: root.Surface, Session: root.Session, Active: true})
}

func (projector ShellProjector) contentForPane(root state.Root, pane state.PaneState, activeContent ContentVM, active bool) ContentVM {
	if active {
		return activeContent
	}
	session := state.TerminalSessionStore{}
	if pane.Kind == state.PaneTerminalLive && pane.TerminalID != "" {
		session = sessionForPane(root, pane)
	}
	return projector.Content.Project(ContentProjectorContext{Root: root, Shell: root.Shell.EnsureDefaults(), Pane: pane, Kind: contentKindForPane(pane), Surface: surfaceForPane(root, pane), Session: session, Active: false})
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

func surfaceForPane(root state.Root, pane state.PaneState) state.TerminalSurfaceStore {
	terminalID := pane.TerminalID
	if terminalID == "" && pane.Active {
		terminalID = root.Session.TerminalID
	}
	if terminalID == "" && pane.ID == root.Shell.EnsureDefaults().ActivePaneID {
		terminalID = root.Session.TerminalID
	}
	if terminalID == "" {
		return root.Surface
	}
	return root.Surface.SurfaceForTerminal(terminalID)
}

func sessionForPane(root state.Root, pane state.PaneState) state.TerminalSessionStore {
	terminalID := pane.TerminalID
	if terminalID == "" && pane.Kind == state.PaneTerminalLive && pane.ID == root.Shell.EnsureDefaults().ActivePaneID {
		terminalID = root.Session.TerminalID
	}
	if terminalID == "" || terminalID == root.Session.TerminalID {
		return root.Session
	}
	session := state.TerminalSessionStore{TerminalID: terminalID}
	if channel, ok := root.Session.InputChannelFor(terminalID); ok {
		session.Channel = channel
		session.Attached = true
	}
	surface := root.Surface.SurfaceForTerminal(terminalID)
	session.Cols = surface.Cols
	session.Rows = surface.Rows
	session.State = surface.State
	return session
}

func canRenderCopyHistory(history state.HistoryStore, copyMode state.CopyModeStore) bool {
	return copyMode.Active &&
		copyMode.TerminalID != "" &&
		history.TerminalID != "" &&
		copyMode.BoundToken != "" &&
		copyMode.BoundCols != 0 &&
		history.Cols != 0 &&
		copyMode.BoundToken == history.Token &&
		copyMode.BoundCols == history.Cols &&
		copyMode.TerminalID == history.TerminalID &&
		len(history.Rows) > 0
}

func copyHistoryPendingReason(history state.HistoryStore, copyMode state.CopyModeStore) string {
	switch {
	case !copyMode.Active:
		return ""
	case copyMode.TerminalID == "":
		return "copy history pending: terminal binding missing"
	case copyMode.BoundToken == "":
		return "copy history pending: authoritative history window pending"
	case copyMode.BoundCols == 0:
		return "copy history pending: bound cols missing"
	case history.TerminalID == "":
		return "copy history pending: authoritative history window pending"
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

func buildCopyHistoryContentVM(history state.HistoryStore, copyMode state.CopyModeStore) ContentVM {
	if !canRenderCopyHistory(history, copyMode) {
		reason := copyHistoryPendingReason(history, copyMode)
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

func buildLiveContentVM(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) ContentVM {
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
			content.Lines = []Line{liveExitedLine(surface, session)}
			content.Empty = true
		} else if surface.Ready {
			content.Lines = []Line{NewLine("live surface empty")}
			content.Empty = true
		} else {
			content.Lines = []Line{NewLine("live surface pending")}
			content.Pending = true
		}
		content.Cursor = Cursor{}
	} else if session.State == state.TerminalLiveExited || surface.State == state.TerminalLiveExited {
		content.Lines = append(content.Lines, NewLine(""), liveExitedLine(surface, session), liveExitedRestartLine(), liveExitedPickerLine())
		content.Cursor = Cursor{}
	}
	return content
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

func liveExitedRestartLine() Line {
	return Line{Cells: []Cell{styledCell("► R restart current terminal ◄", StyleWarning)}}
}

func liveExitedPickerLine() Line {
	return Line{Cells: []Cell{styledCell("[ Ctrl-F choose another terminal ]", StyleMuted)}}
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
		return buildExitedPaneContent(pane)
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
			Text:       text,
			Width:      width,
			ANSIStyle:  terminalLiveANSIStyle(liveCell),
			LinkURL:    liveCell.LinkURL,
			LinkParams: liveCell.LinkParams,
			Safe:       true,
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
