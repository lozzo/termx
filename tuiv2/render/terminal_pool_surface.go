package render

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/terminalmeta"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/uiinput"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func renderTerminalPoolPageWithCursor(pool *modal.TerminalManagerState, runtimeState *VisibleRuntimeStateProxy, theme uiTheme, termSize TermSize, cursorOffsetY int, cursorVisible bool) renderedBody {
	if pool == nil {
		return renderedBody{}
	}
	width := maxInt(1, termSize.Width)
	height := maxInt(1, termSize.Height)
	layout := buildTerminalPoolPageLayout(pool, width, height)
	lookup := newRuntimeLookup(runtimeState)
	lines := renderTerminalPoolCardLines(pool, lookup, layout, theme)
	result := renderedBody{
		lines:  placeOverlayCardLines(theme, width, height, layout.cardX, layout.cardY, layout.cardWidth, lines),
		cursor: hideCursorANSI(),
		meta:   solidPresentMetadata(width, height, renderOwnerTerminalPool),
		blink:  true,
	}
	if cursorVisible {
		cursorX := layout.queryRect.X + pool.QueryState().CursorCellOffset(uiinput.RenderConfig{Width: layout.queryRect.W})
		result.cursor = hostCursorANSI(cursorX, layout.queryRect.Y+cursorOffsetY, "bar", false)
	}
	return result
}

func renderTerminalPoolCardLines(pool *modal.TerminalManagerState, lookup runtimeLookup, layout terminalPoolPageLayout, theme uiTheme) []string {
	bodyLines := make([]string, 0, layout.bodyRect.H+2)
	bodyLines = append(bodyLines, renderOverlaySearchInput(theme, pool.QueryState(), layout.innerWidth))
	bodyLines = append(bodyLines, renderTerminalPoolBodyHeader(theme, layout.listRect.W, layout.previewRect.W))
	leftRows := renderTerminalPoolListLines(pool, layout, theme)
	rightRows := renderTerminalPoolPreviewLines(pool, lookup, layout, theme)
	for row := 0; row < layout.listRect.H; row++ {
		left := ""
		if row < len(leftRows) {
			left = leftRows[row]
		}
		right := ""
		if row < len(rightRows) {
			right = rightRows[row]
		}
		bodyLines = append(bodyLines, renderTerminalPoolBodyRow(theme, left, right, layout.listRect.W, layout.previewRect.W))
	}
	footerLine, _ := layoutTerminalPoolFooterActionsWithTheme(theme, layout.innerWidth, 1)
	bodyLines = append(bodyLines, footerLine)

	cardLines := make([]string, 0, len(bodyLines)+2)
	cardLines = append(cardLines, renderModalTopBorder(theme, coalesce(strings.TrimSpace(pool.Title), "Terminal Pool"), layout.innerWidth))
	for _, line := range bodyLines {
		cardLines = append(cardLines, renderModalFramedRow(theme, line, layout.innerWidth))
	}
	cardLines = append(cardLines, renderModalBottomBorder(theme, layout.innerWidth))
	return cardLines
}

func renderTerminalPoolBodyHeader(theme uiTheme, leftWidth, rightWidth int) string {
	left := renderOverlaySpan(
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.panelMuted)),
		"TERMINALS",
		leftWidth,
	)
	rightLabel := "PREVIEW PANE"
	right := renderOverlaySpan(
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.panelMuted)),
		rightLabel,
		rightWidth,
	)
	return left + pickerBorderStyle(theme).Render("│") + right
}

func renderTerminalPoolBodyRow(theme uiTheme, left, right string, leftWidth, rightWidth int) string {
	return forceWidthANSIOverlay(left, leftWidth) +
		pickerBorderStyle(theme).Render("│") +
		forceWidthANSIOverlay(offsetCHAANSI(right, leftWidth+1), rightWidth)
}

func renderTerminalPoolListLines(pool *modal.TerminalManagerState, layout terminalPoolPageLayout, theme uiTheme) []string {
	if pool == nil || layout.listRect.W <= 0 || layout.listRect.H <= 0 {
		return nil
	}
	items := pool.VisibleItems()
	rows := terminalPoolListRows(items)
	lines := make([]string, 0, layout.listRect.H)
	for _, row := range rows {
		if len(lines) >= layout.listRect.H {
			break
		}
		if row.itemIndex < 0 {
			lines = append(lines, renderOverlaySpan(overlayCardFillStyle(theme), "  "+overlaySectionTitleStyle(theme).Render(row.groupText), layout.listRect.W))
			continue
		}
		index := row.itemIndex
		selected := index == pool.Selected
		style := terminalPoolItemStyle(theme, selected)
		lines = append(lines, style.Render(forceWidthANSIOverlay(terminalPoolItemText(items[index], selected), layout.listRect.W)))
	}
	for len(lines) < layout.listRect.H {
		lines = append(lines, renderOverlaySpan(overlayCardFillStyle(theme), "", layout.listRect.W))
	}
	return lines
}

func terminalPoolItemText(item modal.PickerItem, selected bool) string {
	marker := "○"
	if selected {
		marker = "●"
	}
	parts := []string{marker}
	if item.TerminalID != "" {
		parts = append(parts, item.TerminalID)
	}
	if item.Name != "" && item.Name != item.TerminalID {
		name := item.Name
		if terminalmeta.SizeLocked(item.Tags) {
			name = terminalmeta.SizeLockLockedIcon + " " + name
		}
		parts = append(parts, name)
	}
	if item.State != "" {
		parts = append(parts, item.State)
	}
	if item.Location != "" {
		parts = append(parts, "@"+item.Location)
	}
	return " " + strings.Join(parts, "  ")
}

func terminalPoolItemStyle(theme uiTheme, selected bool) lipgloss.Style {
	style := pickerLineStyle(theme)
	if selected {
		style = pickerSelectedLineStyle(theme)
	}
	return style
}

func renderTerminalPoolPreviewLines(pool *modal.TerminalManagerState, lookup runtimeLookup, layout terminalPoolPageLayout, theme uiTheme) []string {
	rect := layout.previewRect
	if rect.W <= 0 || rect.H <= 0 {
		return nil
	}
	item := pool.SelectedItem()
	if item == nil {
		return terminalPoolEmptyPreviewPane(theme, rect.W, rect.H, "no terminal selected")
	}
	detailLines := renderTerminalPoolDetailLines(item, lookup, rect.W, maxInt(0, rect.H-3), theme)
	paneRect := terminalPoolPaneRect(rect, len(detailLines))
	paneLines := terminalPoolPaneFrameLines(item, lookup, theme, rect.W, paneRect.H)
	rows := make([]string, 0, rect.H)
	for _, line := range detailLines {
		if len(rows) >= rect.H {
			break
		}
		rows = append(rows, line)
	}
	for _, line := range paneLines {
		if len(rows) >= rect.H {
			break
		}
		rows = append(rows, line)
	}
	for len(rows) < rect.H {
		rows = append(rows, renderOverlaySpan(overlayCardFillStyle(theme), "", rect.W))
	}
	return rows[:rect.H]
}

func terminalPoolPaneFrameLines(item *modal.PickerItem, lookup runtimeLookup, theme uiTheme, width, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	terminal := lookup.terminal(item.TerminalID)
	if terminal == nil || (terminal.Snapshot == nil && terminal.Surface == nil) {
		return terminalPoolEmptyPreviewPane(theme, width, height, "PREVIEW  (no live preview)")
	}
	entry := terminalPoolPanePreviewRenderEntry(item, terminal, width, height, theme)
	if entry == nil {
		return terminalPoolEmptyPreviewPane(theme, width, height, "no live preview")
	}
	canvas := buildPreviewSprite(*entry, nil)
	if canvas == nil {
		return terminalPoolEmptyPreviewPane(theme, width, height, "no live preview")
	}
	lines := make([]string, height)
	for row := 0; row < height; row++ {
		lines[row] = canvas.serializeRowRange(row, 0, width-1)
	}
	return lines
}

func terminalPoolEmptyPreviewPane(theme uiTheme, width, height int, text string) []string {
	lines := make([]string, 0, height)
	for len(lines) < height {
		lines = append(lines, renderOverlaySpan(overlayCardFillStyle(theme), text, width))
		text = ""
	}
	return lines
}

func terminalPoolPaneFillStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().Background(lipgloss.Color(theme.panelBG))
}

func terminalPoolInteractivePaneLines(item *modal.PickerItem, lookup runtimeLookup, theme uiTheme, width, rows int) []string {
	if rows <= 0 {
		return nil
	}
	terminal := lookup.terminal(item.TerminalID)
	if terminal == nil {
		return []string{renderOverlaySpan(terminalPoolPaneFillStyle(theme), "(no live preview)", width)}
	}
	source := renderSource(terminal.Snapshot, terminal.Surface)
	if source == nil || source.ScreenRows() == 0 {
		return []string{renderOverlaySpan(terminalPoolPaneFillStyle(theme), "(no live preview)", width)}
	}
	canvas := newComposedCanvas(width, rows)
	drawTerminalSourceWithOffset(canvas, workbench.Rect{W: width, H: rows}, source, 0, theme)
	return canvas.cachedContentLines()
}

func terminalPoolPanePreviewRenderEntry(item *modal.PickerItem, terminal *runtime.VisibleTerminal, width, height int, theme uiTheme) *paneRenderEntry {
	if item == nil || width <= 0 || height <= 0 {
		return nil
	}
	paneID := strings.TrimSpace(item.Location)
	if paneID == "" {
		paneID = strings.TrimSpace(item.TerminalID)
	}
	if paneID == "" {
		paneID = "terminal-manager-preview"
	}
	title := strings.TrimSpace(item.Name)
	if title == "" {
		title = strings.TrimSpace(item.TerminalID)
	}
	if title == "" {
		title = "terminal"
	}
	terminalID := strings.TrimSpace(item.TerminalID)
	rect := workbench.Rect{W: width, H: height}
	border := terminalPoolPaneBorderInfo(item, terminal)
	snapshot := (*protocol.Snapshot)(nil)
	surface := runtime.TerminalSurface(nil)
	surfaceVersion := uint64(0)
	metrics := renderTerminalMetrics{}
	overflow := paneOverflowHints{}
	terminalKnown := terminal != nil
	terminalName := ""
	terminalState := ""
	terminalID = terminal.TerminalID
	terminalName = terminal.Name
	terminalState = terminal.State
	snapshot = terminal.Snapshot
	surface = terminal.Surface
	surfaceVersion = terminal.SurfaceVersion
	extent := terminalExtentProfileCached(snapshot, surface, surfaceVersion)
	metrics = extent.Metrics
	overflow = paneOverflowHintsForRenderWithMetrics(rect, rect, extent.Overflow)
	chrome := UIChromeConfig{PaneChrome: PaneChromeConfig{Top: []ChromeSlotID{SlotPaneTitle, SlotPaneState, SlotPaneShare, SlotPaneSize, SlotPaneRole}}}
	contentKey := paneContentKey{
		TerminalID:     terminalID,
		Snapshot:       snapshot,
		SurfaceVersion: surfaceVersion,
		Name:           terminalName,
		State:          terminalState,
		ThemeBG:        theme.panelBG,
		TerminalKnown:  terminalKnown,
	}
	return &paneRenderEntry{
		PaneID:     paneID,
		OwnerID:    paneOwnerID("terminal-manager-preview:" + paneID),
		Rect:       rect,
		Title:      title,
		Border:     border,
		Theme:      theme,
		Chrome:     chrome,
		Overflow:   overflow,
		ContentKey: contentKey,
		FrameKey: paneFrameKey{
			Rect:            rect,
			Title:           title,
			Border:          border,
			ThemeBG:         theme.panelBG,
			Overflow:        overflow,
			Active:          false,
			ChromeSignature: paneChromeLayoutSignature(rect, title, border, false, chrome),
		},
		TerminalID:           terminalID,
		Snapshot:             snapshot,
		Surface:              surface,
		SurfaceVersion:       surfaceVersion,
		Metrics:              metrics,
		Active:               false,
		ScrollOffset:         0,
		EmptyActionSelected:  -1,
		ExitedActionSelected: -1,
	}
}

func terminalPoolPaneBorderInfo(item *modal.PickerItem, terminal *runtime.VisibleTerminal) paneBorderInfo {
	if terminal != nil {
		info := paneBorderInfo{
			StateLabel: paneBorderStateLabel(terminal.State, terminal.ExitCode),
			StateTone:  paneBorderStateTone(terminal.State),
		}
		if len(terminal.BoundPaneIDs) > 1 {
			info.ShareLabel = "⇄" + strconv.Itoa(len(terminal.BoundPaneIDs))
		}
		if sizeLabel := workbenchTreeTerminalSizeLabel(terminal); sizeLabel != "" {
			info.SizeLabel = sizeLabel
		}
		return info
	}
	return paneBorderInfo{
		StateLabel: paneBorderStateLabel(item.TerminalState, item.ExitCode),
		StateTone:  paneBorderStateTone(item.TerminalState),
	}
}

func terminalPoolPreviewCursor(item *modal.PickerItem, lookup runtimeLookup, paneRect workbench.Rect) (int, int, bool) {
	if item == nil || paneRect.W <= 2 || paneRect.H <= 2 {
		return 0, 0, false
	}
	terminal := lookup.terminal(item.TerminalID)
	if terminal == nil {
		return 0, 0, false
	}
	source := renderSource(terminal.Snapshot, terminal.Surface)
	contentX := paneRect.X + 1
	contentY := paneRect.Y + 1
	if source == nil || !source.Cursor().Visible {
		return contentX, contentY, true
	}
	cursor := source.Cursor()
	x := contentX + cursor.Col
	y := contentY + cursor.Row
	if x < contentX || x >= paneRect.X+paneRect.W-1 || y < contentY || y >= paneRect.Y+paneRect.H-1 {
		return contentX, contentY, true
	}
	return x, y, true
}

func renderTerminalPoolDetailsWithLookup(item *modal.PickerItem, lookup runtimeLookup, innerWidth int) []string {
	if item == nil {
		return nil
	}
	lines := []string(nil)
	if terminal := lookup.terminal(item.TerminalID); terminal != nil {
		if strings.TrimSpace(terminal.OwnerPaneID) != "" {
			lines = append(lines, forceWidthANSIOverlay("owner pane: "+terminal.OwnerPaneID, innerWidth))
		}
		lines = append(lines, forceWidthANSIOverlay("bound panes: "+strconv.Itoa(len(terminal.BoundPaneIDs)), innerWidth))
	}
	if strings.TrimSpace(item.Command) != "" {
		lines = append(lines, forceWidthANSIOverlay("command: "+item.Command, innerWidth))
	}
	if strings.TrimSpace(item.Location) != "" {
		lines = append(lines, forceWidthANSIOverlay("location: "+item.Location, innerWidth))
	}
	if strings.TrimSpace(item.Description) != "" {
		lines = append(lines, forceWidthANSIOverlay("status: "+item.Description, innerWidth))
	}
	return lines
}

func renderTerminalPoolDetailLines(item *modal.PickerItem, lookup runtimeLookup, width, maxRows int, theme uiTheme) []string {
	if item == nil || width <= 0 || maxRows <= 0 {
		return nil
	}
	plain := renderTerminalPoolDetailsWithLookupPlain(item, lookup)
	if len(plain) == 0 {
		return nil
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.panelMuted)).Background(lipgloss.Color(overlayCardBG(theme)))
	header := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.panelMuted)).Bold(true).Background(lipgloss.Color(overlayCardBG(theme))).Render(forceWidthANSIOverlay("DETAIL", width))
	out := []string{header}
	for _, line := range plain {
		for _, wrapped := range wrapTerminalPoolDetailLine(line, width) {
			if len(out) >= maxRows {
				return out
			}
			out = append(out, style.Render(forceWidthANSIOverlay(wrapped, width)))
		}
	}
	return out
}

func renderTerminalPoolDetailsWithLookupPlain(item *modal.PickerItem, lookup runtimeLookup) []string {
	if item == nil {
		return nil
	}
	lines := []string(nil)
	if terminal := lookup.terminal(item.TerminalID); terminal != nil {
		if strings.TrimSpace(terminal.OwnerPaneID) != "" {
			lines = append(lines, "owner pane: "+terminal.OwnerPaneID)
		}
		lines = append(lines, "bound panes: "+strconv.Itoa(len(terminal.BoundPaneIDs)))
	}
	if strings.TrimSpace(item.Command) != "" {
		lines = append(lines, "command: "+item.Command)
	}
	if strings.TrimSpace(item.Location) != "" {
		lines = append(lines, "location: "+item.Location)
	}
	if strings.TrimSpace(item.Description) != "" {
		lines = append(lines, "status: "+item.Description)
	}
	return lines
}

func wrapTerminalPoolDetailLine(line string, width int) []string {
	line = strings.TrimSpace(line)
	if line == "" || width <= 0 {
		return []string{""}
	}
	out := []string(nil)
	for lipgloss.Width(line) > width {
		cut := terminalPoolDetailWrapCut(line, width)
		if cut <= 0 || cut >= len(line) {
			break
		}
		out = append(out, strings.TrimSpace(line[:cut]))
		line = strings.TrimSpace(line[cut:])
	}
	out = append(out, line)
	return out
}

func terminalPoolDetailWrapCut(line string, width int) int {
	lastSpace := -1
	currentWidth := 0
	for index, r := range line {
		if r == ' ' || r == '\t' {
			lastSpace = index
		}
		currentWidth += lipgloss.Width(string(r))
		if currentWidth > width {
			if lastSpace > 0 {
				return lastSpace
			}
			return index
		}
	}
	return len(line)
}

func terminalPoolPreviewLines(snapshot *protocol.Snapshot, surface runtime.TerminalSurface, innerWidth int, maxLines int) []string {
	if maxLines <= 0 {
		maxLines = 4
	}
	lines := terminalPreviewLinesANSI(snapshot, surface, nil, innerWidth, maxLines)
	if len(lines) == 0 {
		lines = []string{forceWidthANSIOverlay("(no live preview)", innerWidth)}
	}
	return lines
}

func xansiTruncateDisplay(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(value)
}
