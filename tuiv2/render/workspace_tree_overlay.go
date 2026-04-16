package render

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func renderWorkspacePickerOverlay(picker *modal.WorkspacePickerState, termSize TermSize) string {
	return strings.Join(renderWorkspacePickerOverlayLinesWithThemeAndCursor(picker, nil, termSize, defaultUITheme(), true), "\n")
}

func renderWorkspacePickerOverlayWithTheme(picker *modal.WorkspacePickerState, termSize TermSize, theme uiTheme) string {
	return strings.Join(renderWorkspacePickerOverlayLinesWithThemeAndCursor(picker, nil, termSize, theme, true), "\n")
}

func renderWorkspacePickerOverlayWithThemeAndCursor(picker *modal.WorkspacePickerState, runtimeState *VisibleRuntimeStateProxy, termSize TermSize, theme uiTheme, cursorVisible bool) string {
	return strings.Join(renderWorkspacePickerOverlayLinesWithThemeAndCursor(picker, runtimeState, termSize, theme, cursorVisible), "\n")
}

func renderWorkspacePickerOverlayLinesWithThemeAndCursor(picker *modal.WorkspacePickerState, runtimeState *VisibleRuntimeStateProxy, termSize TermSize, theme uiTheme, cursorVisible bool) []string {
	if picker == nil {
		return nil
	}
	width, height := overlayViewport(termSize)
	items := picker.VisibleItems()
	layout := buildWorkbenchTreeCardLayout(width, height, len(items), picker.Selected)
	leftRows, visibleStart := renderWorkbenchTreeRows(picker, layout.treeRows, layout.leftRect.W, theme)
	rightRows := renderWorkbenchTreePreviewRows(picker, picker.SelectedItem(), runtimeState, layout.rightRect.W, layout.rightRect.H, theme)

	bodyLines := make([]string, 0, layout.bodyRect.H)
	bodyLines = append(bodyLines, renderWorkbenchTreeBodyHeader(theme, layout.leftRect.W, layout.rightRect.W, picker.SelectedItem()))
	for row := 0; row < layout.treeRows; row++ {
		left := ""
		if row < len(leftRows) {
			left = leftRows[row]
		}
		right := ""
		if row < len(rightRows) {
			right = rightRows[row]
		}
		bodyLines = append(bodyLines, renderWorkbenchTreeBodyRow(theme, left, right, layout.leftRect.W, layout.rightRect.W))
	}
	_ = visibleStart

	lines := make([]string, 0, len(bodyLines)+1)
	lines = append(lines, renderOverlaySearchLineWithCursor(theme, picker.Query, picker.Cursor, picker.CursorSet, layout.innerWidth, cursorVisible))
	lines = append(lines, bodyLines...)

	cardLines := make([]string, 0, len(lines)+2)
	cardLines = append(cardLines, renderModalTopBorder(theme, coalesce(picker.Title, "Workbench Navigator"), layout.innerWidth))
	for _, line := range lines {
		cardLines = append(cardLines, renderModalFramedRow(theme, line, layout.innerWidth))
	}
	cardLines = append(cardLines, renderModalBottomBorder(theme, layout.innerWidth))
	return placeOverlayCardLines(theme, layout.width, layout.contentHeight, layout.cardX, layout.cardY, layout.cardWidth, cardLines)
}

type workbenchTreeCardLayout struct {
	width         int
	height        int
	contentHeight int
	cardX         int
	cardY         int
	cardWidth     int
	cardHeight    int
	innerWidth    int
	bodyRect      workbench.Rect
	leftRect      workbench.Rect
	rightRect     workbench.Rect
	queryRect     workbench.Rect
	treeRows      int
	windowStart   int
	actionRowRect workbench.Rect
}

func buildWorkbenchTreeCardLayout(width, height, itemCount, selected int) workbenchTreeCardLayout {
	contentHeight := maxInt(1, height)
	cardWidth := minInt(maxInt(92, width-8), maxInt(68, width-2))
	cardHeight := minInt(maxInt(18, height-4), maxInt(12, height-1))
	innerWidth := maxInt(48, cardWidth-2)
	cardX := maxInt(0, (width-cardWidth)/2)
	cardY := maxInt(0, (contentHeight-cardHeight)/2)
	bodyY := cardY + 2
	bodyHeight := maxInt(4, cardHeight-3)
	leftWidth := maxInt(22, minInt((innerWidth*36)/100, innerWidth-30))
	rightWidth := maxInt(20, innerWidth-leftWidth-1)
	treeRows := maxInt(1, bodyHeight-1)
	windowStart, _ := workbenchTreeWindow(itemCount, selected, treeRows)
	return workbenchTreeCardLayout{
		width:         width,
		height:        height,
		contentHeight: contentHeight,
		cardX:         cardX,
		cardY:         cardY,
		cardWidth:     cardWidth,
		cardHeight:    cardHeight,
		innerWidth:    innerWidth,
		bodyRect:      workbench.Rect{X: cardX + 1, Y: bodyY, W: innerWidth, H: bodyHeight},
		leftRect:      workbench.Rect{X: cardX + 1, Y: bodyY + 1, W: leftWidth, H: treeRows},
		rightRect:     workbench.Rect{X: cardX + 1 + leftWidth + 1, Y: bodyY + 1, W: rightWidth, H: treeRows},
		queryRect: workbench.Rect{
			X: cardX + 1 + xansi.StringWidth("search: "),
			Y: cardY + 1,
			W: maxInt(1, innerWidth-xansi.StringWidth("search: ")),
			H: 1,
		},
		treeRows:      treeRows,
		windowStart:   windowStart,
		actionRowRect: workbench.Rect{X: cardX + 1 + leftWidth + 1, Y: bodyY + bodyHeight - 1, W: rightWidth, H: 1},
	}
}

func workbenchTreeWindow(total, selected, rows int) (int, int) {
	if total <= 0 || rows <= 0 {
		return 0, 0
	}
	if rows >= total {
		return 0, total
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= total {
		selected = total - 1
	}
	start := selected - rows/3
	if start < 0 {
		start = 0
	}
	end := start + rows
	if end > total {
		end = total
		start = maxInt(0, end-rows)
	}
	return start, end
}

func renderWorkbenchTreeBodyHeader(theme uiTheme, leftWidth, rightWidth int, selected *modal.WorkspacePickerItem) string {
	left := renderOverlaySpan(
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.panelMuted)),
		"TREE",
		leftWidth,
	)
	rightLabel := "DETAIL"
	if selected != nil {
		switch workbenchTreeItemKind(*selected) {
		case modal.WorkspacePickerItemWorkspace:
			rightLabel = "WORKSPACE"
		case modal.WorkspacePickerItemTab:
			rightLabel = "TAB"
		case modal.WorkspacePickerItemPane:
			rightLabel = "PANE"
		case modal.WorkspacePickerItemCreate:
			rightLabel = "CREATE"
		}
	}
	right := renderOverlaySpan(
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.panelMuted)),
		rightLabel,
		rightWidth,
	)
	return left + pickerBorderStyle(theme).Render("│") + right
}

func renderWorkbenchTreeBodyRow(theme uiTheme, left, right string, leftWidth, rightWidth int) string {
	return forceWidthANSIOverlay(left, leftWidth) +
		pickerBorderStyle(theme).Render("│") +
		forceWidthANSIOverlay(right, rightWidth)
}

func renderWorkbenchTreeRows(picker *modal.WorkspacePickerState, rows, width int, theme uiTheme) ([]string, int) {
	if picker == nil || rows <= 0 || width <= 0 {
		return nil, 0
	}
	items := picker.VisibleItems()
	start, end := workbenchTreeWindow(len(items), picker.Selected, rows)
	out := make([]string, 0, rows)
	for index := start; index < end; index++ {
		out = append(out, renderWorkbenchTreeItemLine(items[index], index == picker.Selected, width, theme))
	}
	return out, start
}

func renderWorkbenchTreeItemLine(item modal.WorkspacePickerItem, selected bool, width int, theme uiTheme) string {
	kind := workbenchTreeItemKind(item)
	indent := workbenchTreeGap(theme, 2*maxInt(0, item.Depth))
	marker := workbenchTreeMarker(theme, kind, selected, item.Active || item.Current)
	switch {
	case item.CreateNew || kind == modal.WorkspacePickerItemCreate:
		label := strings.TrimSpace(item.Name)
		if label == "" {
			label = "New workspace"
		}
		body := lipgloss.JoinHorizontal(
			lipgloss.Left,
			marker,
			indent,
			workbenchTreeTitleStyle(theme, kind, selected, item.Active || item.Current).Render("+ "+label),
			workbenchTreeGap(theme, 2),
			workbenchTreeTagStyle(theme, "new", theme.success, selected).Render("new"),
		)
		return workbenchTreeLineStyle(theme, selected, body)
	case kind == modal.WorkspacePickerItemWorkspace:
		title := strings.TrimSpace(item.Name)
		if title == "" {
			title = item.WorkspaceName
		}
		metaParts := make([]string, 0, 4)
		if item.Current {
			metaParts = append(metaParts, workbenchTreeTagStyle(theme, "current", theme.warning, selected).Render("current"))
		}
		if item.TabCount > 0 {
			metaParts = append(metaParts, workbenchTreeTagStyle(theme, "tabs", theme.info, selected).Render(fmt.Sprintf("%d tab", item.TabCount)))
		}
		if item.PaneCount > 0 {
			metaParts = append(metaParts, workbenchTreeTagStyle(theme, "panes", theme.chromeAccent, selected).Render(fmt.Sprintf("%d pane", item.PaneCount)))
		}
		if item.FloatingCount > 0 {
			metaParts = append(metaParts, workbenchTreeTagStyle(theme, "float", theme.warning, selected).Render(fmt.Sprintf("%d float", item.FloatingCount)))
		}
		line := lipgloss.JoinHorizontal(lipgloss.Left, marker, indent, workbenchTreeTitleStyle(theme, kind, selected, item.Current).Render("▾ "+title))
		if len(metaParts) > 0 {
			line = lipgloss.JoinHorizontal(lipgloss.Left, line, workbenchTreeGap(theme, 2), strings.Join(metaParts, workbenchTreeGap(theme, 1)))
		}
		return workbenchTreeLineStyle(theme, selected, line)
	case kind == modal.WorkspacePickerItemPane:
		title := strings.TrimSpace(item.Name)
		if title == "" {
			title = item.PaneID
		}
		metaParts := make([]string, 0, 4)
		if strings.TrimSpace(item.State) != "" {
			metaParts = append(metaParts, workbenchTreeStateTag(theme, item.State, selected))
		}
		if strings.TrimSpace(item.Role) != "" {
			metaParts = append(metaParts, workbenchTreeRoleTag(theme, item.Role, selected))
		}
		if item.Floating {
			metaParts = append(metaParts, workbenchTreeTagStyle(theme, "floating", theme.warning, selected).Render("floating"))
		}
		line := lipgloss.JoinHorizontal(lipgloss.Left, marker, indent, workbenchTreeTitleStyle(theme, kind, selected, item.Active).Render("• "+title))
		if len(metaParts) > 0 {
			line = lipgloss.JoinHorizontal(lipgloss.Left, line, workbenchTreeGap(theme, 2), strings.Join(metaParts, workbenchTreeGap(theme, 1)))
		}
		return workbenchTreeLineStyle(theme, selected, line)
	default:
		title := strings.TrimSpace(item.Name)
		if title == "" {
			title = fmt.Sprintf("tab %d", item.TabIndex+1)
		}
		metaParts := make([]string, 0, 3)
		metaParts = append(metaParts, workbenchTreeTagStyle(theme, "index", theme.chromeAccent, selected).Render(fmt.Sprintf("%d", item.TabIndex+1)))
		if item.Active {
			metaParts = append(metaParts, workbenchTreeTagStyle(theme, "active", theme.chromeAccent, selected).Render("active"))
		}
		if item.PaneCount > 0 {
			metaParts = append(metaParts, workbenchTreeTagStyle(theme, "panes", theme.info, selected).Render(fmt.Sprintf("%d pane", item.PaneCount)))
		}
		line := lipgloss.JoinHorizontal(lipgloss.Left, marker, indent, workbenchTreeTitleStyle(theme, kind, selected, item.Active).Render("└ "+title))
		if len(metaParts) > 0 {
			line = lipgloss.JoinHorizontal(lipgloss.Left, line, workbenchTreeGap(theme, 2), strings.Join(metaParts, workbenchTreeGap(theme, 1)))
		}
		return workbenchTreeLineStyle(theme, selected, line)
	}
}

func workbenchTreeMarker(theme uiTheme, kind modal.WorkspacePickerItemKind, selected bool, active bool) string {
	marker := " "
	color := theme.panelBorder2
	switch kind {
	case modal.WorkspacePickerItemWorkspace:
		marker = "▍"
		color = theme.warning
	case modal.WorkspacePickerItemTab:
		marker = "▍"
		color = theme.chromeAccent
	case modal.WorkspacePickerItemPane:
		marker = "▍"
		color = theme.info
	case modal.WorkspacePickerItemCreate:
		marker = "▍"
		color = theme.success
	}
	if active && !selected {
		color = ensureContrast(color, overlayCardBG(theme), 3.4)
	}
	return lipgloss.NewStyle().
		Bold(selected || active).
		Foreground(lipgloss.Color(ensureContrast(color, overlayCardBG(theme), 3.4))).
		Render(marker)
}

func workbenchTreeTitleStyle(theme uiTheme, kind modal.WorkspacePickerItemKind, selected bool, active bool) lipgloss.Style {
	fg := theme.panelText
	if active {
		switch kind {
		case modal.WorkspacePickerItemWorkspace:
			fg = theme.warning
		case modal.WorkspacePickerItemTab:
			fg = theme.chromeAccent
		case modal.WorkspacePickerItemPane:
			fg = theme.info
		}
	}
	if selected {
		fg = ensureContrast(mixHex(fg, theme.chromeAccent, 0.28), overlayCardBG(theme), 4.0)
	}
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(fg))
	if selected {
		style = style.Underline(true)
	}
	return style
}

func workbenchTreeTagStyle(theme uiTheme, _ string, accent string, selected bool) lipgloss.Style {
	fg := ensureContrast(accent, overlayCardBG(theme), 3.2)
	if selected {
		fg = ensureContrast(mixHex(theme.panelText, accent, 0.42), overlayCardBG(theme), 4.0)
	}
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(fg))
}

func workbenchTreeLineStyle(theme uiTheme, selected bool, content string) string {
	if !selected {
		return content
	}
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ensureContrast(mixHex(theme.panelText, theme.chromeAccent, 0.2), overlayCardBG(theme), 4.0))).
		Render(content)
}

func workbenchTreeGap(theme uiTheme, width int) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelMuted)).
		Render(strings.Repeat(" ", width))
}

func workbenchTreeStateTag(theme uiTheme, state string, selected bool) string {
	accent := theme.panelMuted
	switch strings.TrimSpace(strings.ToLower(state)) {
	case "running", "attached":
		accent = theme.success
	case "exited", "dead":
		accent = theme.danger
	case "unconnected":
		accent = theme.warning
	default:
		accent = theme.info
	}
	return workbenchTreeTagStyle(theme, "state", accent, selected).Render(state)
}

func workbenchTreeRoleTag(theme uiTheme, role string, selected bool) string {
	accent := theme.chromeAccent
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "owner":
		accent = theme.chromeAccent
	case "follower":
		accent = theme.warning
	}
	return workbenchTreeTagStyle(theme, "role", accent, selected).Render(role)
}

func renderWorkbenchTreePreviewRows(picker *modal.WorkspacePickerState, item *modal.WorkspacePickerItem, runtimeState *VisibleRuntimeStateProxy, width, height int, theme uiTheme) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	rows := make([]string, 0, height)
	if item == nil {
		for len(rows) < height {
			rows = append(rows, "")
		}
		return rows
	}
	title := strings.TrimSpace(item.Name)
	if title == "" {
		title = "selection"
	}
	subtitle := renderWorkbenchTreeSubtitle(item)
	rows = append(rows, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.panelText)).Render(forceWidthANSIOverlay(title, width)))
	rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color(theme.panelMuted)).Render(forceWidthANSIOverlay(subtitle, width)))

	sectionTitle := "DETAIL"
	switch workbenchTreeItemKind(*item) {
	case modal.WorkspacePickerItemWorkspace:
		sectionTitle = "SUMMARY"
	case modal.WorkspacePickerItemTab:
		sectionTitle = "PANES"
	case modal.WorkspacePickerItemPane:
		sectionTitle = "SNAPSHOT"
	case modal.WorkspacePickerItemCreate:
		sectionTitle = "CREATE"
	}
	rows = append(rows, renderOverlaySpan(lipgloss.NewStyle().Foreground(lipgloss.Color(theme.warning)).Bold(true), sectionTitle, width))

	previewLines := workbenchTreePreviewLines(picker, item, newRuntimeLookup(runtimeState), runtimeState, width, maxInt(1, height-4), theme)
	rows = append(rows, previewLines...)
	for len(rows) < height-1 {
		rows = append(rows, "")
	}
	actionLine, _ := layoutOverlayFooterActionsWithTheme(theme, workbenchTreeActionSpecs(item), workbench.Rect{W: width, H: 1})
	rows = append(rows, actionLine)
	if len(rows) > height {
		rows = rows[:height]
	}
	return rows
}

func renderWorkbenchTreeSubtitle(item *modal.WorkspacePickerItem) string {
	if item == nil {
		return ""
	}
	switch workbenchTreeItemKind(*item) {
	case modal.WorkspacePickerItemWorkspace:
		parts := []string{workbenchTreeWorkspaceName(*item)}
		if item.ActiveTabName != "" {
			parts = append(parts, "tab:"+item.ActiveTabName)
		}
		if item.ActivePaneName != "" {
			parts = append(parts, "pane:"+item.ActivePaneName)
		}
		return strings.Join(parts, "  ")
	case modal.WorkspacePickerItemTab:
		parts := []string{workbenchTreeWorkspaceName(*item), fmt.Sprintf("tab:%d", item.TabIndex+1)}
		if item.ActivePaneName != "" {
			parts = append(parts, "pane:"+item.ActivePaneName)
		}
		return strings.Join(parts, "  ")
	case modal.WorkspacePickerItemPane:
		parts := []string{workbenchTreeWorkspaceName(*item)}
		if strings.TrimSpace(item.TabName) != "" {
			parts = append(parts, "tab:"+item.TabName)
		}
		if strings.TrimSpace(item.State) != "" {
			parts = append(parts, item.State)
		}
		if strings.TrimSpace(item.Role) != "" {
			parts = append(parts, item.Role)
		}
		return strings.Join(parts, "  ")
	default:
		return "create workspace"
	}
}

func workbenchTreeActionSpecs(item *modal.WorkspacePickerItem) []overlayFooterActionSpec {
	if item == nil {
		return nil
	}
	switch workbenchTreeItemKind(*item) {
	case modal.WorkspacePickerItemWorkspace:
		return []overlayFooterActionSpec{
			{Label: "Open", Action: input.SemanticAction{Kind: input.ActionSubmitPrompt}},
			{Label: "Rename", Action: input.SemanticAction{Kind: input.ActionRenameWorkspace}},
			{Label: "Delete", Action: input.SemanticAction{Kind: input.ActionDeleteWorkspace}},
		}
	case modal.WorkspacePickerItemTab:
		return []overlayFooterActionSpec{
			{Label: "Open", Action: input.SemanticAction{Kind: input.ActionSubmitPrompt}},
			{Label: "Rename Tab", Action: input.SemanticAction{Kind: input.ActionRenameTab}},
			{Label: "Close Tab", Action: input.SemanticAction{Kind: input.ActionCloseTab}},
		}
	case modal.WorkspacePickerItemPane:
		return []overlayFooterActionSpec{
			{Label: "Open", Action: input.SemanticAction{Kind: input.ActionSubmitPrompt}},
			{Label: "Zoom", Action: input.SemanticAction{Kind: input.ActionZoomPane}},
			{Label: "Detach", Action: input.SemanticAction{Kind: input.ActionDetachPane}},
			{Label: "Close", Action: input.SemanticAction{Kind: input.ActionDeleteWorkspace}},
		}
	case modal.WorkspacePickerItemCreate:
		return []overlayFooterActionSpec{
			{Label: "New Workspace", Action: input.SemanticAction{Kind: input.ActionCreateWorkspace}},
		}
	default:
		return nil
	}
}

func workbenchTreePreviewLines(picker *modal.WorkspacePickerState, item *modal.WorkspacePickerItem, lookup runtimeLookup, runtimeState *VisibleRuntimeStateProxy, width, maxLines int, theme uiTheme) []string {
	if item == nil || maxLines <= 0 {
		return nil
	}
	switch workbenchTreeItemKind(*item) {
	case modal.WorkspacePickerItemWorkspace:
		return workbenchTreeWorkspaceSummaryLines(picker, item, width, maxLines)
	case modal.WorkspacePickerItemTab:
		return workbenchTreeTabPreviewLines(picker, item, lookup, runtimeState, width, maxLines, theme)
	}
	if terminal := lookup.terminal(item.TerminalID); terminal != nil {
		return terminalPreviewLinesANSI(terminal.Snapshot, terminal.Surface, runtimeState, width, maxLines)
	}
	out := []string{forceWidthANSIOverlay("(no live preview)", width)}
	if workbenchTreeItemKind(*item) == modal.WorkspacePickerItemPane {
		if item.State != "" {
			out = append(out, forceWidthANSIOverlay("state: "+item.State, width))
		}
		if item.Role != "" {
			out = append(out, forceWidthANSIOverlay("role: "+item.Role, width))
		}
	}
	if len(out) > maxLines {
		out = out[:maxLines]
	}
	return out
}

func workbenchTreeWorkspaceSummaryLines(picker *modal.WorkspacePickerState, item *modal.WorkspacePickerItem, width, maxLines int) []string {
	if item == nil || width <= 0 || maxLines <= 0 {
		return nil
	}
	lines := []string{
		forceWidthANSIOverlay(fmt.Sprintf("workspace: %s", workbenchTreeWorkspaceName(*item)), width),
		forceWidthANSIOverlay(fmt.Sprintf("tabs: %d", item.TabCount), width),
		forceWidthANSIOverlay(fmt.Sprintf("panes: %d", item.PaneCount), width),
		forceWidthANSIOverlay(fmt.Sprintf("floating: %d", item.FloatingCount), width),
	}
	if item.ActiveTabName != "" {
		lines = append(lines, forceWidthANSIOverlay("active tab: "+item.ActiveTabName, width))
	}
	if item.ActivePaneName != "" {
		lines = append(lines, forceWidthANSIOverlay("active pane: "+item.ActivePaneName, width))
	}
	tabNames := workbenchTreeTabTitlesForWorkspace(picker, workbenchTreeWorkspaceName(*item), 4)
	if len(tabNames) > 0 {
		lines = append(lines, forceWidthANSIOverlay("", width))
		lines = append(lines, forceWidthANSIOverlay("tabs in workspace:", width))
		for _, name := range tabNames {
			lines = append(lines, forceWidthANSIOverlay("  • "+name, width))
		}
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return lines
}

func workbenchTreeTabPreviewLines(picker *modal.WorkspacePickerState, item *modal.WorkspacePickerItem, lookup runtimeLookup, runtimeState *VisibleRuntimeStateProxy, width, maxLines int, theme uiTheme) []string {
	if item == nil || width <= 0 || maxLines <= 0 {
		return nil
	}
	panes := workbenchTreePaneItemsForTab(picker, item.WorkspaceName, item.TabID)
	if len(panes) == 0 {
		return []string{forceWidthANSIOverlay("(no pane previews)", width)}
	}
	lines := make([]string, 0, maxLines)
	separatorCount := maxInt(0, len(panes)-1)
	usable := maxInt(len(panes), maxLines-separatorCount)
	baseBlock := usable / maxInt(1, len(panes))
	remainder := usable % maxInt(1, len(panes))
	for paneIndex, pane := range panes {
		if len(lines) >= maxLines {
			break
		}
		if paneIndex > 0 {
			lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color(theme.panelBorder)).Render(strings.Repeat("─", maxInt(1, width))))
			if len(lines) >= maxLines {
				break
			}
		}
		blockLines := baseBlock
		if paneIndex < remainder {
			blockLines++
		}
		blockLines = maxInt(2, blockLines)
		title := strings.TrimSpace(pane.Name)
		if title == "" {
			title = pane.PaneID
		}
		blockUsed := 0
		header := workbenchTreeTitleStyle(theme, modal.WorkspacePickerItemPane, false, pane.Active).Render(title)
		meta := make([]string, 0, 3)
		if pane.State != "" {
			meta = append(meta, pane.State)
		}
		if pane.Role != "" {
			meta = append(meta, pane.Role)
		}
		if pane.Floating {
			meta = append(meta, "floating")
		}
		line := header
		if len(meta) > 0 {
			line += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(theme.panelMuted)).Render(strings.Join(meta, "  "))
		}
		lines = append(lines, forceWidthANSIOverlay(line, width))
		blockUsed++
		if terminal := lookup.terminal(pane.TerminalID); terminal != nil {
			preview := terminalPreviewBlockLinesANSI(terminal.Snapshot, terminal.Surface, runtimeState, maxInt(1, width-2), minInt(maxInt(1, blockLines-1), maxLines-len(lines)), theme)
			for _, previewLine := range preview {
				if len(lines) >= maxLines {
					break
				}
				lines = append(lines, forceWidthANSIOverlay("  "+previewLine, width))
				blockUsed++
			}
		} else if len(lines) < maxLines {
			lines = append(lines, forceWidthANSIOverlay("  (no live preview)", width))
			blockUsed++
		}
		for blockUsed < blockLines && len(lines) < maxLines {
			lines = append(lines, "")
			blockUsed++
		}
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return lines
}

func workbenchTreeTabTitlesForWorkspace(picker *modal.WorkspacePickerState, workspaceName string, limit int) []string {
	if picker == nil || limit <= 0 {
		return nil
	}
	out := make([]string, 0, limit)
	for _, item := range picker.Items {
		if workbenchTreeItemKind(item) != modal.WorkspacePickerItemTab {
			continue
		}
		if strings.TrimSpace(item.WorkspaceName) != strings.TrimSpace(workspaceName) {
			continue
		}
		out = append(out, strings.TrimSpace(item.Name))
		if len(out) >= limit {
			break
		}
	}
	return out
}

func workbenchTreePaneItemsForTab(picker *modal.WorkspacePickerState, workspaceName, tabID string) []modal.WorkspacePickerItem {
	if picker == nil {
		return nil
	}
	out := make([]modal.WorkspacePickerItem, 0, 4)
	for _, item := range picker.Items {
		if workbenchTreeItemKind(item) != modal.WorkspacePickerItemPane {
			continue
		}
		if strings.TrimSpace(item.WorkspaceName) != strings.TrimSpace(workspaceName) || strings.TrimSpace(item.TabID) != strings.TrimSpace(tabID) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func workbenchTreeItemKind(item modal.WorkspacePickerItem) modal.WorkspacePickerItemKind {
	switch {
	case item.CreateNew:
		return modal.WorkspacePickerItemCreate
	case item.Kind != "":
		return item.Kind
	case strings.TrimSpace(item.PaneID) != "":
		return modal.WorkspacePickerItemPane
	case strings.TrimSpace(item.TabID) != "":
		return modal.WorkspacePickerItemTab
	default:
		return modal.WorkspacePickerItemWorkspace
	}
}

func workbenchTreeWorkspaceName(item modal.WorkspacePickerItem) string {
	if name := strings.TrimSpace(item.WorkspaceName); name != "" {
		return name
	}
	if workbenchTreeItemKind(item) == modal.WorkspacePickerItemWorkspace {
		return strings.TrimSpace(item.Name)
	}
	return ""
}
