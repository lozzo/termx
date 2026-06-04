package render

import (
	"fmt"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

type Mode string

const (
	ModeLive Mode = "live"
	ModeCopy Mode = "copy"
)

type RenderVM struct {
	Shell      ShellVM
	Mode       Mode
	Lines      []string
	Status     string
	HitRegions []HitRegion
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

type HitRegion struct {
	Kind     HitRegionKind
	Rect     Rect
	LineID   uint64
	Row      int
	PaneID   string
	ActionID string
}

type RenderVMBuilder struct{}

func NewRenderVMBuilder() RenderVMBuilder {
	return RenderVMBuilder{}
}

func (RenderVMBuilder) Build(root state.Root) RenderVM {
	shell := buildShellVM(root)
	vm := RenderVM{Shell: shell}
	if root.CopyMode.Active && canRenderCopyHistory(root.History, root.CopyMode) {
		vm.Lines = copyHistoryPlainRows(root.History)
	}
	return vm.withCompatibilityProjection()
}

func buildShellVM(root state.Root) ShellVM {
	shellState := root.Shell.EnsureDefaults()
	activeContent := buildActiveContentVM(root)
	return ShellVM{
		Header:  buildHeaderVM(shellState, root),
		Footer:  buildFooterVM(root, activeContent),
		Layout:  buildLayoutVM(shellState, activeContent, root.Viewport),
		Overlay: buildOverlayVM(root, shellState),
		Toasts:  buildToastVMs(shellState),
		Cursor:  activeContent.Cursor,
	}
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
		Actions:       footerActions(mode),
		ActiveTarget:  activeTargetSummary(shell, root),
		GlobalSummary: globalSummary(shell),
	}
}

func terminalSummary(root state.Root) string {
	ids := map[string]struct{}{}
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
	count := len(ids)
	if count == 0 {
		return "term:0"
	}
	return fmt.Sprintf("term:%d", count)
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

func footerActions(mode string) []string {
	switch mode {
	case "pane":
		return []string{"v split", "x close", "n focus", "z zoom", "esc"}
	case "resize":
		return []string{"←/h", "→/l", "↑/k", "↓/j", "b balance", "esc"}
	case "global":
		return []string{"h header", "f footer", "p pool", "w tree", "T toast", "t clear", "esc"}
	case "copy":
		return []string{"pgup older", "wheel", "esc"}
	case string(state.OverlayTerminalPicker):
		return []string{"select", "attach", "esc"}
	case string(state.OverlayTerminalPool):
		return []string{"search", "attach", "edit", "kill", "esc"}
	case string(state.OverlayWorkbenchTree):
		return []string{"search", "open", "focus", "esc"}
	case string(state.OverlayPrompt):
		return []string{"type", "enter submit", "esc cancel"}
	case string(state.OverlayHelp):
		return []string{"read", "enter close", "esc"}
	case "floating":
		return []string{"n new", "arrows move", "HJKL size", "x close", "esc"}
	case "tab":
		return []string{"n new", "h/l switch", "r rename", "x close", "esc"}
	case "workspace":
		return []string{"n new", "h/l switch", "r rename", "t tree", "esc"}
	default:
		return []string{"^P pane", "^R size", "^T tab", "^W ws", "^V copy", "^F pick", "^G"}
	}
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

func globalSummary(shell state.ShellStore) string {
	tab := activeTab(shell)
	paneCount := len(tab.Panes)
	if paneCount == 0 {
		paneCount = 1
	}
	return fmt.Sprintf("ws:%s tabs:%d panes:%d %s", shell.Workspace.Name, len(shell.Workspace.Tabs), paneCount, floatingSummary(shell))
}

func tabStripSummary(shell state.ShellStore) string {
	shell = shell.EnsureDefaults()
	if len(shell.Workspace.Tabs) == 0 {
		return "main"
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

func buildLayoutVM(shell state.ShellStore, activeContent ContentVM, viewport state.ViewportStore) LayoutVM {
	if shell.ZoomedPaneID != "" {
		return LayoutVM{
			Viewport: viewportRect(viewport),
			Panels:   buildZoomedPanelVMs(shell, activeContent),
			Floating: buildFloatingVMs(shell),
			Split:    SplitVM{PaneID: shell.ZoomedPaneID},
		}
	}
	return LayoutVM{
		Viewport: viewportRect(viewport),
		Panels:   buildPanelVMs(shell, activeContent),
		Floating: buildFloatingVMs(shell),
		Split:    buildSplitVM(activeTab(shell).RootSplit),
	}
}

func viewportRect(viewport state.ViewportStore) Rect {
	if !viewport.Valid {
		return Rect{}
	}
	return Rect{W: viewport.Cols, H: viewport.Rows}
}

func buildPanelVMs(shell state.ShellStore, activeContent ContentVM) []PanelVM {
	shell = shell.EnsureDefaults()
	tab := activeTab(shell)
	if len(tab.Panes) == 0 {
		return []PanelVM{{
			ID:           state.DefaultPaneID,
			Title:        "shell",
			Presentation: renderPanelPresentation(shell.PanelPresentation),
			Active:       true,
			Content:      activeContent,
		}}
	}
	panels := make([]PanelVM, len(tab.Panes))
	for i, pane := range tab.Panes {
		active := pane.ID == shell.ActivePaneID
		content := placeholderContentForPane(pane)
		if active {
			content = activeContent
		}
		panels[i] = PanelVM{
			ID:           pane.ID,
			Title:        activePaneTitle(pane),
			Presentation: renderPanelPresentation(shell.PanelPresentation),
			Active:       active,
			Content:      content,
		}
	}
	return panels
}

func buildZoomedPanelVMs(shell state.ShellStore, activeContent ContentVM) []PanelVM {
	shell = shell.EnsureDefaults()
	for _, pane := range activeTab(shell).Panes {
		if pane.ID == shell.ZoomedPaneID {
			return []PanelVM{{
				ID:           pane.ID,
				Title:        activePaneTitle(pane),
				Presentation: renderPanelPresentation(shell.PanelPresentation),
				Active:       true,
				Content:      activeContent,
			}}
		}
	}
	return buildPanelVMs(shell, activeContent)
}

func buildFloatingVMs(shell state.ShellStore) []FloatingVM {
	shell = shell.EnsureDefaults()
	if len(shell.Floatings) == 0 {
		return nil
	}
	out := make([]FloatingVM, 0, len(shell.Floatings))
	for _, floating := range shell.Floatings {
		content := placeholderContentForPane(floating.Pane)
		out = append(out, FloatingVM{
			ID:        floating.ID,
			Title:     floating.Title,
			Rect:      Rect{X: floating.Rect.X, Y: floating.Rect.Y, W: floating.Rect.W, H: floating.Rect.H},
			Z:         floating.Z,
			Active:    floating.Active,
			Collapsed: floating.Collapsed,
			Content:   content,
		})
	}
	return out
}

func buildActiveContentVM(root state.Root) ContentVM {
	if root.CopyMode.Active {
		return buildCopyHistoryContentVM(root.History, root.CopyMode)
	}
	shell := root.Shell.EnsureDefaults()
	if pane, ok := shell.Pane(state.PaneCommandTarget{PaneID: shell.ActivePaneID}); ok {
		switch pane.Kind {
		case state.PaneEmpty, state.PaneExited:
			return placeholderContentForPane(pane)
		}
	}
	return buildLiveContentVM(root.Surface, root.Session)
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
		HitRegions: copyHistoryHitRegions(history),
	}
}

func buildLiveContentVM(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) ContentVM {
	lines := terminalLiveLineVMsFromStrings(surface.Lines)
	content := ContentVM{
		Kind:   ContentTerminalLive,
		Lines:  lines,
		Status: liveStatus(surface, session),
		Cursor: liveContentCursor(surface, session, lines),
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
	}
	return content
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

func buildOverlayVM(root state.Root, shell state.ShellStore) OverlayVM {
	if !shell.Overlay.Open {
		return OverlayVM{}
	}
	switch shell.Overlay.Kind {
	case state.OverlayTerminalPicker:
		return OverlayVM{
			Kind:    OverlayTerminalPicker,
			Opaque:  false,
			Content: buildTerminalPickerContent(root, shell),
		}
	case state.OverlayTerminalPool:
		return OverlayVM{
			Kind:    OverlayTerminalPool,
			Opaque:  true,
			Content: buildTerminalPoolContent(root, shell),
		}
	case state.OverlayWorkbenchTree:
		return OverlayVM{
			Kind:    OverlayWorkbenchTree,
			Opaque:  true,
			Content: buildWorkbenchTreeContent(root, shell),
		}
	case state.OverlayPrompt:
		return OverlayVM{Kind: OverlayPrompt, Opaque: true, Content: buildPromptContent(shell)}
	case state.OverlayHelp:
		return OverlayVM{Kind: OverlayHelp, Opaque: true, Content: buildHelpContent(shell)}
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

func (vm RenderVM) withCompatibilityProjection() RenderVM {
	content := activeContent(vm.Shell)
	vm.Mode = ModeLive
	if content.Kind == ContentCopyHistory {
		vm.Mode = ModeCopy
	}
	if len(vm.Lines) == 0 {
		vm.Lines = stringsFromLines(content.Lines)
	}
	vm.Status = content.Status
	vm.HitRegions = cloneHitRegions(content.HitRegions)
	vm.Shell.Cursor = content.Cursor
	return vm
}

func copyHistoryPlainRows(history state.HistoryStore) []string {
	if len(history.Rows) == 0 {
		return nil
	}
	lines := make([]string, len(history.Rows))
	for i, row := range history.Rows {
		lines[i] = row.Text
	}
	return lines
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

func lineVMsFromStrings(lines []string) []Line {
	if len(lines) == 0 {
		return nil
	}
	out := make([]Line, len(lines))
	for i, line := range lines {
		out[i] = NewLine(line)
	}
	return out
}

func terminalLiveLineVMsFromStrings(lines []string) []Line {
	if len(lines) == 0 {
		return nil
	}
	out := make([]Line, len(lines))
	for i, line := range lines {
		out[i] = terminalLiveLineFromANSI(line)
	}
	return out
}

func stringsFromLines(lines []Line) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = line.String()
	}
	return out
}

type Renderer struct {
	Theme Theme
}

func NewRenderer(theme Theme) Renderer {
	if theme == (Theme{}) {
		theme = DefaultTheme()
	}
	return Renderer{Theme: theme}
}

func (renderer Renderer) RenderResult(vm RenderVM) RenderResult {
	return renderer.renderFramework(vm)
}

func (renderer Renderer) Render(vm RenderVM) Frame {
	return renderer.RenderResult(vm).Frame()
}

func cloneHitRegions(regions []HitRegion) []HitRegion {
	if len(regions) == 0 {
		return nil
	}
	cloned := make([]HitRegion, len(regions))
	copy(cloned, regions)
	return cloned
}
