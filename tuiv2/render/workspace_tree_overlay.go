package render

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/uiinput"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func renderWorkspacePickerOverlay(picker *modal.WorkspacePickerState, termSize TermSize) string {
	return strings.Join(renderWorkspacePickerOverlayLinesWithThemeAndCursor(picker, nil, termSize, defaultUITheme(), true), "\n")
}

func renderWorkspacePickerOverlayLinesWithThemeAndCursor(picker *modal.WorkspacePickerState, runtimeState *VisibleRuntimeStateProxy, termSize TermSize, theme uiTheme, cursorVisible bool) []string {
	_ = cursorVisible
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
	lines = append(lines, renderOverlaySearchInput(theme, picker.QueryState(), layout.innerWidth))
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
			X: cardX + 1 + uiinput.PromptWidth(overlaySearchPrompt()),
			Y: cardY + 1,
			W: maxInt(1, innerWidth-uiinput.PromptWidth(overlaySearchPrompt())),
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
		forceWidthANSIOverlay(offsetCHAANSI(right, leftWidth+1), rightWidth)
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
	active := item.Active || item.Current
	lead := workbenchTreeLead(theme, selected, active)
	branch := workbenchTreeBranch(theme, item.Depth, selected)
	icon := workbenchTreeNodeStyle(theme, kind, selected, active).Render(workbenchTreeNodeGlyph(kind))
	switch {
	case item.CreateNew || kind == modal.WorkspacePickerItemCreate:
		label := strings.TrimSpace(item.Name)
		if label == "" {
			label = "New workspace"
		}
		body := lipgloss.JoinHorizontal(
			lipgloss.Left,
			lead,
			branch,
			icon,
			" ",
			workbenchTreeTitleStyle(theme, kind, selected, active).Render(label),
			workbenchTreeGap(theme, 2),
			workbenchTreeToken(theme, "new", theme.success, selected),
		)
		return workbenchTreeLineStyle(theme, selected, body)
	case kind == modal.WorkspacePickerItemWorkspace:
		title := strings.TrimSpace(item.Name)
		if title == "" {
			title = item.WorkspaceName
		}
		metaParts := make([]string, 0, 4)
		if item.Current {
			metaParts = append(metaParts, workbenchTreeToken(theme, "current", theme.warning, selected))
		}
		if item.TabCount > 0 {
			metaParts = append(metaParts, workbenchTreeToken(theme, fmt.Sprintf("%d tabs", item.TabCount), theme.info, selected))
		}
		if item.PaneCount > 0 {
			metaParts = append(metaParts, workbenchTreeToken(theme, fmt.Sprintf("%d panes", item.PaneCount), theme.chromeAccent, selected))
		}
		if item.FloatingCount > 0 {
			metaParts = append(metaParts, workbenchTreeToken(theme, fmt.Sprintf("%d float", item.FloatingCount), theme.warning, selected))
		}
		line := lipgloss.JoinHorizontal(lipgloss.Left, lead, branch, icon, " ", workbenchTreeTitleStyle(theme, kind, selected, item.Current).Render(title))
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
			metaParts = append(metaParts, workbenchTreeToken(theme, "floating", theme.warning, selected))
		}
		line := lipgloss.JoinHorizontal(lipgloss.Left, lead, branch, icon, " ", workbenchTreeTitleStyle(theme, kind, selected, item.Active).Render(title))
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
		metaParts = append(metaParts, workbenchTreeToken(theme, fmt.Sprintf("tab %d", item.TabIndex+1), theme.chromeAccent, selected))
		if item.Active {
			metaParts = append(metaParts, workbenchTreeToken(theme, "active", theme.chromeAccent, selected))
		}
		if item.PaneCount > 0 {
			metaParts = append(metaParts, workbenchTreeToken(theme, fmt.Sprintf("%d panes", item.PaneCount), theme.info, selected))
		}
		line := lipgloss.JoinHorizontal(lipgloss.Left, lead, branch, icon, " ", workbenchTreeTitleStyle(theme, kind, selected, item.Active).Render(title))
		if len(metaParts) > 0 {
			line = lipgloss.JoinHorizontal(lipgloss.Left, line, workbenchTreeGap(theme, 2), strings.Join(metaParts, workbenchTreeGap(theme, 1)))
		}
		return workbenchTreeLineStyle(theme, selected, line)
	}
}

func workbenchTreeKindAccent(theme uiTheme, kind modal.WorkspacePickerItemKind) string {
	color := theme.panelBorder2
	switch kind {
	case modal.WorkspacePickerItemWorkspace:
		color = theme.warning
	case modal.WorkspacePickerItemTab:
		color = theme.chromeAccent
	case modal.WorkspacePickerItemPane:
		color = theme.info
	case modal.WorkspacePickerItemCreate:
		color = theme.success
	}
	return color
}

func workbenchTreeNodeGlyph(kind modal.WorkspacePickerItemKind) string {
	switch kind {
	case modal.WorkspacePickerItemWorkspace:
		return "󰙅"
	case modal.WorkspacePickerItemTab:
		return "󰓩"
	case modal.WorkspacePickerItemPane:
		return ""
	case modal.WorkspacePickerItemCreate:
		return "󰐕"
	default:
		return "•"
	}
}

func workbenchTreeNodeStyle(theme uiTheme, kind modal.WorkspacePickerItemKind, selected bool, active bool) lipgloss.Style {
	fg := ensureContrast(workbenchTreeKindAccent(theme, kind), overlayCardBG(theme), 3.4)
	if selected {
		fg = ensureContrast(mixHex(fg, theme.panelText, 0.28), overlayCardBG(theme), 4.0)
	}
	style := lipgloss.NewStyle().
		Bold(selected || active).
		Foreground(lipgloss.Color(fg))
	if selected {
		style = style.Underline(true)
	}
	return style
}

func workbenchTreeLead(theme uiTheme, selected bool, active bool) string {
	content := "  "
	fg := theme.panelBorder2
	switch {
	case selected:
		content = "▸ "
		fg = theme.chromeAccent
	case active:
		content = "• "
		fg = theme.panelMuted
	}
	return lipgloss.NewStyle().
		Bold(selected || active).
		Foreground(lipgloss.Color(ensureContrast(fg, overlayCardBG(theme), 3.0))).
		Render(content)
}

func workbenchTreeBranch(theme uiTheme, depth int, selected bool) string {
	if depth <= 0 {
		return ""
	}
	guideFG := theme.panelBorder2
	if selected {
		guideFG = ensureContrast(mixHex(theme.panelBorder2, theme.chromeAccent, 0.35), overlayCardBG(theme), 2.4)
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(ensureContrast(guideFG, overlayCardBG(theme), 2.2)))
	var builder strings.Builder
	for i := 0; i < depth-1; i++ {
		builder.WriteString(style.Render("│ "))
	}
	builder.WriteString(style.Render("├─"))
	return builder.String()
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

func workbenchTreeToken(theme uiTheme, label string, accent string, selected bool) string {
	fg := ensureContrast(accent, overlayCardBG(theme), 3.2)
	border := ensureContrast(theme.panelBorder2, overlayCardBG(theme), 2.2)
	if selected {
		fg = ensureContrast(mixHex(theme.panelText, accent, 0.42), overlayCardBG(theme), 4.0)
		border = ensureContrast(mixHex(theme.panelBorder2, theme.chromeAccent, 0.35), overlayCardBG(theme), 2.4)
	}
	borderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(border))
	textStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(fg))
	return borderStyle.Render("[") + textStyle.Render(label) + borderStyle.Render("]")
}

func workbenchTreeLineStyle(theme uiTheme, selected bool, content string) string {
	_ = theme
	_ = selected
	return content
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
	icon := "󰈸"
	switch strings.TrimSpace(strings.ToLower(state)) {
	case "running", "attached":
		accent = theme.success
		icon = ""
	case "exited", "dead":
		accent = theme.danger
		icon = ""
	case "unconnected":
		accent = theme.warning
		icon = "󰔟"
	default:
		accent = theme.info
	}
	return workbenchTreeToken(theme, icon+" "+state, accent, selected)
}

func workbenchTreeRoleTag(theme uiTheme, role string, selected bool) string {
	accent := theme.chromeAccent
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "owner":
		accent = theme.chromeAccent
	case "follower":
		accent = theme.warning
	}
	return workbenchTreeToken(theme, role, accent, selected)
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
		return workbenchTreePanePreviewFrameLines(*item, terminal, runtimeState, width, maxLines, theme)
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
	usable := maxInt(len(panes), maxLines)
	baseBlock := usable / maxInt(1, len(panes))
	remainder := usable % maxInt(1, len(panes))
	for paneIndex, pane := range panes {
		if len(lines) >= maxLines {
			break
		}
		blockLines := baseBlock
		if paneIndex < remainder {
			blockLines++
		}
		blockLines = maxInt(3, blockLines)
		terminal := lookup.terminal(pane.TerminalID)
		frame := workbenchTreePanePreviewFrameLines(pane, terminal, runtimeState, width, minInt(blockLines, maxLines-len(lines)), theme)
		for _, frameLine := range frame {
			if len(lines) >= maxLines {
				break
			}
			lines = append(lines, frameLine)
		}
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return lines
}

func workbenchTreePanePreviewFrameLines(pane modal.WorkspacePickerItem, terminal *runtime.VisibleTerminal, runtimeState *VisibleRuntimeStateProxy, width, height int, theme uiTheme) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	entry := workbenchTreePanePreviewRenderEntry(pane, terminal, width, height, theme)
	if entry == nil {
		return nil
	}
	canvas := buildPreviewSprite(*entry, runtimeState)
	if canvas == nil {
		return nil
	}
	lines := make([]string, height)
	for row := 0; row < height; row++ {
		lines[row] = canvas.serializeRowRange(row, 0, width-1)
	}
	return lines
}

func workbenchTreePanePreviewRenderEntry(pane modal.WorkspacePickerItem, terminal *runtime.VisibleTerminal, width, height int, theme uiTheme) *paneRenderEntry {
	if width <= 0 || height <= 0 {
		return nil
	}
	paneID := strings.TrimSpace(pane.PaneID)
	if paneID == "" {
		paneID = strings.TrimSpace(pane.Name)
	}
	if paneID == "" {
		paneID = "preview"
	}
	title := strings.TrimSpace(pane.Name)
	if title == "" {
		title = paneID
	}
	terminalID := strings.TrimSpace(pane.TerminalID)
	rect := workbench.Rect{W: width, H: height}
	border := workbenchTreePanePreviewBorderInfo(pane, terminal)
	snapshot := (*protocol.Snapshot)(nil)
	surface := runtime.TerminalSurface(nil)
	surfaceVersion := uint64(0)
	metrics := renderTerminalMetrics{}
	overflow := paneOverflowHints{}
	terminalKnown := terminal != nil
	terminalName := ""
	terminalState := ""
	if terminal != nil {
		terminalID = terminal.TerminalID
		terminalName = terminal.Name
		terminalState = terminal.State
		snapshot = terminal.Snapshot
		surface = terminal.Surface
		surfaceVersion = terminal.SurfaceVersion
		extent := terminalExtentProfileCached(snapshot, surface, surfaceVersion)
		metrics = extent.Metrics
		overflow = paneOverflowHintsForRenderWithMetrics(rect, rect, extent.Overflow)
	} else if terminalID == "" {
		terminalID = paneID
	}
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
		OwnerID:    paneOwnerID("workbench-navigator-preview:" + paneID),
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
			Active:          pane.Active,
			Floating:        pane.Floating,
			ChromeSignature: paneChromeLayoutSignature(rect, title, border, pane.Floating, chrome),
		},
		TerminalID:           terminalID,
		Snapshot:             snapshot,
		Surface:              surface,
		SurfaceVersion:       surfaceVersion,
		Metrics:              metrics,
		Active:               pane.Active,
		Floating:             pane.Floating,
		ScrollOffset:         0,
		EmptyActionSelected:  -1,
		ExitedActionSelected: -1,
	}
}

func workbenchTreePanePreviewBorderInfo(pane modal.WorkspacePickerItem, terminal *runtime.VisibleTerminal) paneBorderInfo {
	if terminal != nil {
		info := paneBorderInfo{
			StateLabel: paneBorderStateLabel(terminal.State, terminal.ExitCode),
			StateTone:  paneBorderStateTone(terminal.State),
		}
		switch strings.TrimSpace(strings.ToLower(pane.Role)) {
		case "owner":
			info.RoleLabel = "◆ owner"
		case "follower":
			info.RoleLabel = "◇ follow"
		}
		if len(terminal.BoundPaneIDs) > 1 {
			info.ShareLabel = fmt.Sprintf("⇄%d", len(terminal.BoundPaneIDs))
		}
		if sizeLabel := workbenchTreeTerminalSizeLabel(terminal); sizeLabel != "" {
			info.SizeLabel = sizeLabel
		}
		return info
	}
	return paneBorderInfo{
		StateLabel: paneBorderStateLabel(pane.State, nil),
		StateTone:  paneBorderStateTone(pane.State),
		RoleLabel:  workbenchTreePanePreviewRoleLabel(pane.Role),
	}
}

func workbenchTreeTerminalSizeLabel(terminal *runtime.VisibleTerminal) string {
	if terminal == nil {
		return ""
	}
	source := renderSource(terminal.Snapshot, terminal.Surface)
	if source == nil {
		return ""
	}
	size := source.Size()
	if size.Cols == 0 || size.Rows == 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d", size.Cols, size.Rows)
}

func workbenchTreePanePreviewRoleLabel(role string) string {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "owner":
		return "◆ owner"
	case "follower":
		return "◇ follow"
	default:
		return ""
	}
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
