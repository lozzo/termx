package render

import "github.com/lozzow/termx/termx-tui-v3/state"

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
	HitRegionHistoryRow  HitRegionKind = "history-row"
	HitRegionStatus      HitRegionKind = "status"
	HitRegionOverlay     HitRegionKind = "overlay"
	HitRegionToast       HitRegionKind = "toast"
	HitRegionToastClose  HitRegionKind = "toast-close"
	HitRegionPaneChrome  HitRegionKind = "pane-chrome"
	HitRegionPaneAction  HitRegionKind = "pane-action"
	HitRegionPaneResize  HitRegionKind = "pane-resize"
	HitRegionPaneContent HitRegionKind = "pane-content"
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
	return vm.withCompatibilityProjection()
}

func buildShellVM(root state.Root) ShellVM {
	shellState := root.Shell.EnsureDefaults()
	activeContent := buildActiveContentVM(root)
	return ShellVM{
		Header:  buildHeaderVM(shellState, root),
		Footer:  buildFooterVM(root, activeContent),
		Layout:  buildLayoutVM(shellState, activeContent, root.Viewport),
		Overlay: buildOverlayVM(shellState),
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
		Visible: shell.HeaderVisible,
		Title:   shell.Workspace.Name,
		Notice:  notice,
	}
}

func buildFooterVM(root state.Root, content ContentVM) FooterVM {
	mode := "live"
	if root.CopyMode.Active {
		mode = "copy"
	}
	hint := content.Status
	if hint == "" {
		hint = liveStatus(root.Surface, root.Session)
	}
	return FooterVM{
		Visible: root.Shell.EnsureDefaults().FooterVisible,
		Mode:    mode,
		Hint:    hint,
	}
}

func buildLayoutVM(shell state.ShellStore, activeContent ContentVM, viewport state.ViewportStore) LayoutVM {
	if shell.ZoomedPaneID != "" {
		return LayoutVM{
			Viewport: viewportRect(viewport),
			Panels:   buildZoomedPanelVMs(shell, activeContent),
			Split:    SplitVM{PaneID: shell.ZoomedPaneID},
		}
	}
	return LayoutVM{
		Viewport: viewportRect(viewport),
		Panels:   buildPanelVMs(shell, activeContent),
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

func buildActiveContentVM(root state.Root) ContentVM {
	if root.CopyMode.Active {
		return buildCopyHistoryContentVM(root.History, root.CopyMode)
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
			Status:  copyModeStatus(copyMode),
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
	lines := make([]Line, len(history.Rows))
	regions := make([]HitRegion, len(history.Rows))
	for i, row := range history.Rows {
		lines[i] = NewLine(row.Text)
		regions[i] = HitRegion{
			Kind:   HitRegionHistoryRow,
			Rect:   Rect{Y: i, W: history.Cols, H: 1},
			LineID: row.LineID,
			Row:    i,
		}
	}
	return ContentVM{
		Kind:       ContentCopyHistory,
		Lines:      lines,
		Status:     copyModeStatus(copyMode),
		Cursor:     copyModeCursor(copyMode),
		HitRegions: regions,
	}
}

func buildLiveContentVM(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) ContentVM {
	lines := lineVMsFromStrings(surface.Lines)
	if len(lines) == 0 {
		lines = []Line{NewLine("live surface pending")}
	}
	content := ContentVM{
		Kind:   ContentTerminalLive,
		Lines:  lines,
		Status: liveStatus(surface, session),
	}
	if session.LastError != "" {
		content.Error = session.LastError
	} else if surface.Err != "" {
		content.Error = surface.Err
	}
	return content
}

func copyModeStatus(copyMode state.CopyModeStore) string {
	if copyMode.Empty {
		return "copy: empty"
	}
	return "copy"
}

func liveStatus(surface state.TerminalSurfaceStore, session state.TerminalSessionStore) string {
	status := "live"
	if surface.TerminalID != "" {
		status = "live: " + surface.TerminalID
	}
	if session.LastError != "" {
		status = "error: " + session.LastError
	} else if surface.Err != "" {
		status = "error: " + surface.Err
	}
	return status
}

func copyModeCursor(copyMode state.CopyModeStore) Cursor {
	if !copyMode.Active {
		return Cursor{}
	}
	return Cursor{
		Visible: true,
		Row:     copyMode.Cursor.Row,
		Col:     copyMode.Cursor.Col,
		Shape:   CursorShapeBlock,
	}
}

func buildOverlayVM(shell state.ShellStore) OverlayVM {
	if !shell.Overlay.Open {
		return OverlayVM{}
	}
	switch shell.Overlay.Kind {
	case state.OverlayTerminalPicker:
		return OverlayVM{
			Kind:   OverlayTerminalPicker,
			Opaque: false,
			Content: ContentVM{
				Kind:    ContentTerminalPicker,
				Lines:   []Line{NewLine("terminal picker pending")},
				Status:  "terminal picker",
				Pending: true,
			},
		}
	case state.OverlayPrompt:
		return OverlayVM{Kind: OverlayPrompt, Opaque: true, Content: ContentVM{Kind: ContentPrompt, Pending: true}}
	case state.OverlayHelp:
		return OverlayVM{Kind: OverlayHelp, Opaque: true, Content: ContentVM{Kind: ContentHelp, Pending: true}}
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
	vm.Lines = stringsFromLines(content.Lines)
	vm.Status = content.Status
	vm.HitRegions = cloneHitRegions(content.HitRegions)
	vm.Shell.Cursor = content.Cursor
	return vm
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
		return ContentVM{Kind: ContentEmptyPane, Lines: []Line{NewLine(title + " empty")}, Empty: true}
	case state.PaneExited:
		return ContentVM{Kind: ContentExitedPane, Lines: []Line{NewLine(title + " exited")}, Status: "exited"}
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
