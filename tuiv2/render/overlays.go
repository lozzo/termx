package render

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type pickerCardLayout struct {
	width         int
	height        int
	contentHeight int
	innerWidth    int
	hasFooter     bool
	listHeight    int
	fixedRows     int
	cardX         int
	cardY         int
	cardWidth     int
	cardHeight    int
	firstItemY    int
}

const overlayFooterActionGap = 2

type overlayFooterActionSpec struct {
	Label  string
	Action input.SemanticAction
}

type overlayFooterActionLayout struct {
	Label  string
	Action input.SemanticAction
	Rect   workbench.Rect
}

func overlayViewport(termSize TermSize) (int, int) {
	return maxInt(termSize.Width, 80), maxInt(termSize.Height, 24)
}

func buildPickerCardLayout(width, height, itemCount int, hasFooter bool) pickerCardLayout {
	contentHeight := maxInt(1, height)
	innerWidth := pickerInnerWidth(width)
	fixedRows := 5
	if hasFooter {
		fixedRows++
	}
	maxListHeight := maxInt(1, minInt(10, contentHeight-fixedRows))
	listHeight := minInt(maxInt(4, itemCount), maxListHeight)
	cardWidth := innerWidth + 2
	cardHeight := listHeight + fixedRows
	cardX := maxInt(0, (width-cardWidth)/2)
	cardY := maxInt(0, (contentHeight-cardHeight)/2)
	return pickerCardLayout{
		width:         width,
		height:        height,
		contentHeight: contentHeight,
		innerWidth:    innerWidth,
		hasFooter:     hasFooter,
		listHeight:    listHeight,
		fixedRows:     fixedRows,
		cardX:         cardX,
		cardY:         cardY,
		cardWidth:     cardWidth,
		cardHeight:    cardHeight,
		firstItemY:    cardY + 3,
	}
}

func pickerFooterActionSpecs() []overlayFooterActionSpec {
	return modeFooterActionSpecs(
		[]input.ActionKind{
			input.ActionSubmitPrompt,
			input.ActionPickerAttachSplit,
			input.ActionEditTerminal,
			input.ActionKillTerminal,
			input.ActionCancelMode,
		},
		map[input.ActionKind]string{
			input.ActionSubmitPrompt:      "attach",
			input.ActionPickerAttachSplit: "split+attach",
			input.ActionEditTerminal:      "edit",
			input.ActionKillTerminal:      "kill",
			input.ActionCancelMode:        "close",
		},
	)
}

func workspacePickerFooterActionSpecs() []overlayFooterActionSpec {
	return modeFooterActionSpecs(
		[]input.ActionKind{
			input.ActionSubmitPrompt,
			input.ActionCreateWorkspace,
			input.ActionRenameWorkspace,
			input.ActionDeleteWorkspace,
			input.ActionCancelMode,
		},
		map[input.ActionKind]string{
			input.ActionSubmitPrompt:    "open",
			input.ActionCreateWorkspace: "new",
			input.ActionRenameWorkspace: "rename",
			input.ActionDeleteWorkspace: "delete",
			input.ActionCancelMode:      "close",
		},
	)
}

func terminalManagerFooterActionSpecs() []overlayFooterActionSpec {
	return modeFooterActionSpecs(
		[]input.ActionKind{
			input.ActionSubmitPrompt,
			input.ActionAttachTab,
			input.ActionAttachFloating,
			input.ActionEditTerminal,
			input.ActionKillTerminal,
			input.ActionCancelMode,
		},
		map[input.ActionKind]string{
			input.ActionSubmitPrompt:   "here",
			input.ActionAttachTab:      "tab",
			input.ActionAttachFloating: "float",
			input.ActionEditTerminal:   "edit",
			input.ActionKillTerminal:   "kill",
			input.ActionCancelMode:     "close",
		},
	)
}

func promptFooterActionSpecs(prompt *modal.PromptState) []overlayFooterActionSpec {
	paneID := ""
	if prompt != nil {
		paneID = prompt.PaneID
	}
	return []overlayFooterActionSpec{
		{Label: "submit", Action: input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: paneID}},
		{Label: "cancel", Action: input.SemanticAction{Kind: input.ActionCancelMode}},
	}
}

func floatingOverviewFooterActionSpecs() []overlayFooterActionSpec {
	return modeFooterActionSpecs(
		[]input.ActionKind{
			input.ActionSubmitPrompt,
			input.ActionExpandAllFloatingPanes,
			input.ActionCollapseAllFloatingPanes,
			input.ActionCloseFloatingPane,
			input.ActionCancelMode,
		},
		map[input.ActionKind]string{
			input.ActionSubmitPrompt:             "open",
			input.ActionExpandAllFloatingPanes:   "show-all",
			input.ActionCollapseAllFloatingPanes: "collapse-all",
			input.ActionCloseFloatingPane:        "close-pane",
			input.ActionCancelMode:               "close",
		},
	)
}

func modeFooterActionSpecs(order []input.ActionKind, fallback map[input.ActionKind]string) []overlayFooterActionSpec {
	specs := make([]overlayFooterActionSpec, 0, len(order))
	for _, kind := range order {
		label := strings.TrimSpace(fallback[kind])
		if strings.TrimSpace(label) == "" {
			continue
		}
		specs = append(specs, overlayFooterActionSpec{
			Label:  label,
			Action: input.SemanticAction{Kind: kind},
		})
	}
	return specs
}

func pickerFooterRowY(layout pickerCardLayout) int {
	return layout.firstItemY + layout.listHeight + 1
}

func pickerQueryRowRect(layout pickerCardLayout) workbench.Rect {
	return overlayQueryInputRect(layout, xansi.StringWidth("search: "))
}

func overlayQueryInputRect(layout pickerCardLayout, prefixWidth int) workbench.Rect {
	editableX := layout.cardX + 1 + maxInt(0, prefixWidth)
	editableW := maxInt(1, layout.innerWidth-maxInt(0, prefixWidth))
	return workbench.Rect{
		X: editableX,
		Y: layout.cardY + 2,
		W: editableW,
		H: 1,
	}
}

func promptInputRect(layout pickerCardLayout, prompt *modal.PromptState, inputLine int) workbench.Rect {
	return promptInputRectForLabel(layout, promptFieldLabel(prompt.Kind), inputLine)
}

func promptInputRectForLabel(layout pickerCardLayout, label string, inputLine int) workbench.Rect {
	prefixWidth := xansi.StringWidth(strings.TrimSpace(label) + ": ")
	editableX := layout.cardX + 1 + maxInt(0, prefixWidth)
	editableW := maxInt(1, layout.innerWidth-maxInt(0, prefixWidth))
	return workbench.Rect{
		X: editableX,
		Y: layout.firstItemY + inputLine,
		W: editableW,
		H: 1,
	}
}

func promptFormInputRect(layout pickerCardLayout, prompt *modal.PromptState, inputLine, fieldIndex int) workbench.Rect {
	if prompt == nil || fieldIndex < 0 || fieldIndex >= len(prompt.Fields) {
		return workbench.Rect{}
	}
	label := prompt.Fields[fieldIndex].Label
	if prompt.Fields[fieldIndex].Required {
		label += "*"
	}
	prefixWidth := xansi.StringWidth("  " + strings.TrimSpace(label) + ": ")
	editableX := layout.cardX + 1 + maxInt(0, prefixWidth)
	editableW := maxInt(1, layout.innerWidth-maxInt(0, prefixWidth))
	return workbench.Rect{
		X: editableX,
		Y: layout.firstItemY + inputLine,
		W: editableW,
		H: 1,
	}
}

func layoutOverlayFooterActions(specs []overlayFooterActionSpec, rowRect workbench.Rect) (string, []overlayFooterActionLayout) {
	return layoutOverlayFooterActionsWithTheme(defaultUITheme(), specs, rowRect)
}

func layoutOverlayFooterActionsWithTheme(theme uiTheme, specs []overlayFooterActionSpec, rowRect workbench.Rect) (string, []overlayFooterActionLayout) {
	if rowRect.W <= 0 || rowRect.H <= 0 || len(specs) == 0 {
		return "", nil
	}
	var builder strings.Builder
	actions := make([]overlayFooterActionLayout, 0, len(specs))
	currentX := 0
	for _, spec := range specs {
		label := renderOverlayFooterActionLabel(theme, spec.Label)
		labelW := xansi.StringWidth(label)
		if labelW <= 0 {
			continue
		}
		need := labelW
		if len(actions) > 0 {
			need += overlayFooterActionGap
		}
		if currentX+need > rowRect.W {
			break
		}
		if len(actions) > 0 {
			builder.WriteString(renderOverlaySpan(overlayFooterPlainStyle(theme), "", overlayFooterActionGap))
			currentX += overlayFooterActionGap
		}
		actions = append(actions, overlayFooterActionLayout{
			Label:  label,
			Action: spec.Action,
			Rect: workbench.Rect{
				X: rowRect.X + currentX,
				Y: rowRect.Y,
				W: labelW,
				H: 1,
			},
		})
		builder.WriteString(label)
		currentX += labelW
	}
	return builder.String(), actions
}

func renderWorkspacePickerOverlay(picker *modal.WorkspacePickerState, termSize TermSize) string {
	return renderWorkspacePickerOverlayWithThemeAndCursor(picker, nil, termSize, defaultUITheme(), true)
}

func renderWorkspacePickerOverlayWithTheme(picker *modal.WorkspacePickerState, termSize TermSize, theme uiTheme) string {
	return renderWorkspacePickerOverlayWithThemeAndCursor(picker, nil, termSize, theme, true)
}

func renderWorkspacePickerOverlayWithThemeAndCursor(picker *modal.WorkspacePickerState, runtimeState *VisibleRuntimeStateProxy, termSize TermSize, theme uiTheme, cursorVisible bool) string {
	if picker == nil {
		return ""
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
	card := strings.Join(cardLines, "\n")
	body := lipgloss.Place(
		layout.width,
		layout.contentHeight,
		lipgloss.Center,
		lipgloss.Center,
		card,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceStyle(backgroundStyle(theme.hostBG)),
	)
	return terminalPickerBodyStyle(theme).Render(forceHeight(body, layout.contentHeight))
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

	previewLines := workbenchTreePreviewLines(picker, item, runtimeState, width, maxInt(1, height-4), theme)
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

func workbenchTreePreviewLines(picker *modal.WorkspacePickerState, item *modal.WorkspacePickerItem, runtimeState *VisibleRuntimeStateProxy, width, maxLines int, theme uiTheme) []string {
	if item == nil || maxLines <= 0 {
		return nil
	}
	switch workbenchTreeItemKind(*item) {
	case modal.WorkspacePickerItemWorkspace:
		return workbenchTreeWorkspaceSummaryLines(picker, item, width, maxLines)
	case modal.WorkspacePickerItemTab:
		return workbenchTreeTabPreviewLines(picker, item, runtimeState, width, maxLines, theme)
	}
	lookup := newRuntimeLookup(runtimeState)
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

func workbenchTreeTabPreviewLines(picker *modal.WorkspacePickerState, item *modal.WorkspacePickerItem, runtimeState *VisibleRuntimeStateProxy, width, maxLines int, theme uiTheme) []string {
	if item == nil || width <= 0 || maxLines <= 0 {
		return nil
	}
	panes := workbenchTreePaneItemsForTab(picker, item.WorkspaceName, item.TabID)
	if len(panes) == 0 {
		return []string{forceWidthANSIOverlay("(no pane previews)", width)}
	}
	lookup := newRuntimeLookup(runtimeState)
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

func terminalPreviewBlockLinesANSI(snapshot *protocol.Snapshot, surface runtime.TerminalSurface, runtimeState *VisibleRuntimeStateProxy, width, height int, theme uiTheme) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	source := renderSource(snapshot, surface)
	if source == nil {
		lines := []string{forceWidthANSIOverlay("(no live preview)", width)}
		for len(lines) < height {
			lines = append(lines, "")
		}
		return lines
	}
	canvas := newComposedCanvas(width, height)
	if runtimeState != nil {
		canvas.hostEmojiVS16Mode = runtimeState.HostEmojiVS16Mode
	}
	drawTerminalSourceWithOffset(canvas, workbench.Rect{X: 0, Y: 0, W: width, H: height}, source, 0, theme)
	lines := canvas.embeddedContentLines()
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
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

func terminalPreviewLinesANSI(snapshot *protocol.Snapshot, surface runtime.TerminalSurface, runtimeState *VisibleRuntimeStateProxy, width, maxLines int) []string {
	source := renderSource(snapshot, surface)
	if source == nil || width <= 0 || maxLines <= 0 || source.ScreenRows() == 0 {
		return []string{forceWidthANSIOverlay("(no live preview)", width)}
	}
	lines := make([]string, 0, minInt(source.ScreenRows(), maxLines))
	base := source.ScrollbackRows()
	emojiMode := shared.AmbiguousEmojiVariationSelectorRaw
	if runtimeState != nil {
		emojiMode = runtimeState.HostEmojiVS16Mode
	}
	for rowIndex := 0; rowIndex < source.ScreenRows() && len(lines) < maxLines; rowIndex++ {
		lines = append(lines, protocolPreviewRowANSI(source.Row(base+rowIndex), width, emojiMode))
	}
	if len(lines) == 0 {
		return []string{forceWidthANSIOverlay("(no live preview)", width)}
	}
	return lines
}

func terminalPreviewSummaryLinesANSI(snapshot *protocol.Snapshot, surface runtime.TerminalSurface, runtimeState *VisibleRuntimeStateProxy, width, maxLines int) []string {
	source := renderSource(snapshot, surface)
	if source == nil || width <= 0 || maxLines <= 0 || source.ScreenRows() == 0 {
		return []string{forceWidthANSIOverlay("(no live preview)", width)}
	}
	emojiMode := shared.AmbiguousEmojiVariationSelectorRaw
	if runtimeState != nil {
		emojiMode = runtimeState.HostEmojiVS16Mode
	}
	base := source.ScrollbackRows()
	rows := make([]string, 0, source.ScreenRows())
	for rowIndex := 0; rowIndex < source.ScreenRows(); rowIndex++ {
		line := protocolPreviewRowANSITight(source.Row(base+rowIndex), width, emojiMode)
		if strings.TrimSpace(xansi.Strip(line)) == "" {
			continue
		}
		rows = append(rows, line)
	}
	if len(rows) == 0 {
		return []string{forceWidthANSIOverlay("(no live preview)", width)}
	}
	if len(rows) > maxLines {
		rows = rows[len(rows)-maxLines:]
	}
	return rows
}

func protocolPreviewRowANSI(row []protocol.Cell, width int, emojiMode shared.AmbiguousEmojiVariationSelectorMode) string {
	if width <= 0 {
		return ""
	}
	var builder strings.Builder
	current := drawStyle{}
	cols := 0
	for index := 0; index < len(row) && cols < width; index++ {
		cell := drawCellFromProtocolCell(row[index])
		if cell.Continuation {
			continue
		}
		content := cell.Content
		if content == "" {
			content = " "
		}
		if current != cell.Style {
			builder.WriteString(styleDiffANSI(current, cell.Style))
			current = cell.Style
		}
		nextCol := 0
		if cols+cell.Width < width {
			nextCol = cols + cell.Width + 1
		}
		builder.WriteString(serializeCellContentForDisplay(content, cell.Width, emojiMode, nextCol))
		cols += cell.Width
	}
	if current != (drawStyle{}) {
		builder.WriteString(styleANSI(drawStyle{}))
	}
	return forceWidthANSIOverlay(builder.String(), width)
}

func protocolPreviewRowANSITight(row []protocol.Cell, width int, emojiMode shared.AmbiguousEmojiVariationSelectorMode) string {
	if width <= 0 {
		return ""
	}
	trimmed := trimProtocolRowTrailingBlankCells(row)
	if len(trimmed) == 0 {
		return ""
	}
	var builder strings.Builder
	current := drawStyle{}
	cols := 0
	for index := 0; index < len(trimmed) && cols < width; index++ {
		cell := drawCellFromProtocolCell(trimmed[index])
		if cell.Continuation {
			continue
		}
		content := cell.Content
		if content == "" {
			content = " "
		}
		if current != cell.Style {
			builder.WriteString(styleDiffANSI(current, cell.Style))
			current = cell.Style
		}
		nextCol := 0
		if cols+cell.Width < width {
			nextCol = cols + cell.Width + 1
		}
		builder.WriteString(serializeCellContentForDisplay(content, cell.Width, emojiMode, nextCol))
		cols += cell.Width
	}
	if current != (drawStyle{}) {
		builder.WriteString(styleANSI(drawStyle{}))
	}
	return builder.String()
}

func trimProtocolRowTrailingBlankCells(row []protocol.Cell) []protocol.Cell {
	end := len(row)
	for end > 0 {
		cell := row[end-1]
		if cell.Content == "" && cell.Width == 0 {
			end--
			continue
		}
		if strings.TrimSpace(cell.Content) == "" && cell.Style == (protocol.CellStyle{}) {
			end--
			continue
		}
		break
	}
	return row[:end]
}

func compositeOverlay(body string, overlay string, _ TermSize) string {
	if overlay == "" {
		return body
	}
	// The overlay is rendered with lipgloss.Place using a solid background that
	// fills the entire terminal area, so there is nothing to see through. Return
	// the overlay directly — rune-level blending of ANSI-escaped strings is
	// unsound because escape sequences inflate rune count beyond display width.
	return overlay
}

func renderPickerCard(title, query string, items []string, footer string, width, height int) string {
	return renderPickerCardWithTheme(defaultUITheme(), title, query, items, footer, width, height)
}

func renderPickerCardWithTheme(theme uiTheme, title, header string, items []string, footer string, width, height int) string {
	layout := buildPickerCardLayout(width, height, len(items), strings.TrimSpace(footer) != "")

	lines := make([]string, 0, layout.cardHeight-2)
	lines = append(lines, renderCardContentRow(theme, "", layout.innerWidth))
	lines = append(lines, renderCardHeaderRow(theme, header, layout.innerWidth))
	for i := 0; i < layout.listHeight; i++ {
		content := ""
		if i < len(items) {
			content = items[i]
		}
		lines = append(lines, renderCardContentRow(theme, content, layout.innerWidth))
	}
	lines = append(lines, renderCardContentRow(theme, "", layout.innerWidth))
	if layout.hasFooter {
		lines = append(lines, renderCardContentRow(theme, renderOverlayFooterLine(theme, footer, layout.innerWidth), layout.innerWidth))
	}

	cardLines := make([]string, 0, len(lines)+2)
	cardLines = append(cardLines, renderModalTopBorder(theme, title, layout.innerWidth))
	for _, line := range lines {
		cardLines = append(cardLines, renderModalFramedRow(theme, line, layout.innerWidth))
	}
	cardLines = append(cardLines, renderModalBottomBorder(theme, layout.innerWidth))
	card := strings.Join(cardLines, "\n")
	body := lipgloss.Place(
		layout.width,
		layout.contentHeight,
		lipgloss.Center,
		lipgloss.Center,
		card,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceStyle(backgroundStyle(theme.hostBG)),
	)
	return terminalPickerBodyStyle(theme).Render(forceHeight(body, layout.contentHeight))
}

func renderModalTopBorder(theme uiTheme, title string, innerWidth int) string {
	border := pickerBorderStyle(theme)
	titleText := strings.TrimSpace(title)
	if titleText == "" {
		titleText = "modal"
	}
	maxTitleWidth := maxInt(1, innerWidth-1)
	titleText = xansi.Truncate(titleText, maxTitleWidth, "")
	titleWidth := xansi.StringWidth(titleText)
	rightWidth := maxInt(0, innerWidth-1-titleWidth)
	return border.Render("╭─") +
		modalBorderTitleStyle(theme).Render(titleText) +
		border.Render(strings.Repeat("─", rightWidth)+"╮")
}

func renderModalBottomBorder(theme uiTheme, innerWidth int) string {
	return pickerBorderStyle(theme).Render("╰" + strings.Repeat("─", maxInt(0, innerWidth)) + "╯")
}

func renderModalFramedRow(theme uiTheme, content string, innerWidth int) string {
	border := pickerBorderStyle(theme)
	return border.Render("│") + forceWidthANSIOverlay(content, innerWidth) + border.Render("│")
}

func pickerInnerWidth(termWidth int) int {
	modalWidth := minInt(maxInt(54, termWidth/2), 84)
	modalWidth = minInt(modalWidth, maxInt(30, termWidth-12))
	return maxInt(24, modalWidth-2)
}

func renderCardTitleRow(theme uiTheme, title string, innerWidth int) string {
	return terminalPickerTitleStyle(theme).
		Width(innerWidth).
		MaxWidth(innerWidth).
		Render(forceWidthANSIOverlay(title, innerWidth))
}

func renderCardHeaderRow(theme uiTheme, header string, innerWidth int) string {
	if strings.TrimSpace(header) == "" {
		return renderOverlaySpan(overlayCardFillStyle(theme), "", innerWidth)
	}
	return renderOverlaySpan(
		lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.panelMuted)).
			Background(lipgloss.Color(theme.panelStrong)),
		header,
		innerWidth,
	)
}

func renderCardContentRow(theme uiTheme, content string, innerWidth int) string {
	return renderOverlaySpan(overlayCardFillStyle(theme), content, innerWidth)
}

func renderOverlaySearchLine(theme uiTheme, query string, cursor int, cursorSet bool, innerWidth int) string {
	return renderOverlaySearchLineWithCursor(theme, query, cursor, cursorSet, innerWidth, true)
}

func renderOverlaySearchLineWithCursor(theme uiTheme, query string, cursor int, cursorSet bool, innerWidth int, cursorVisible bool) string {
	_ = cursorVisible
	value := queryValueWithCursorVisible(query, cursor, cursorSet, cursorVisible)
	label := "search: "
	prefix := "  " + label
	valueWidth := maxInt(0, innerWidth-xansi.StringWidth(prefix))
	row := promptFieldMarkerStyle(theme, false).Render("  ") +
		promptFieldLabelStyle(theme, true).Render(label) +
		renderOverlayPromptValue(promptFieldValueStyle(theme, true), value, valueWidth)
	return renderOverlaySpan(overlayCardFillStyle(theme), row, innerWidth)
}

func queryValueWithCursor(query string, cursor int, cursorSet bool) string {
	return queryValueWithCursorVisible(query, cursor, cursorSet, true)
}

func queryValueWithCursorVisible(query string, cursor int, cursorSet bool, cursorVisible bool) string {
	_ = cursor
	_ = cursorSet
	_ = cursorVisible
	return query
}

func renderOverlayFooterLine(theme uiTheme, footer string, innerWidth int) string {
	return renderOverlaySpan(pickerFooterStyle(theme), footer, innerWidth)
}

func renderOverlayPromptField(theme uiTheme, prompt *modal.PromptState, innerWidth int) string {
	return renderOverlayPromptFieldWithCursor(theme, prompt, innerWidth, true)
}

func renderOverlayPromptFieldWithCursor(theme uiTheme, prompt *modal.PromptState, innerWidth int, cursorVisible bool) string {
	_ = cursorVisible
	if prompt == nil {
		return ""
	}
	value := promptValueWithCursorVisible(prompt, cursorVisible)
	label := promptFieldLabel(prompt.Kind) + ": "
	prefix := "  " + label
	valueWidth := maxInt(0, innerWidth-xansi.StringWidth(prefix))
	return promptFieldMarkerStyle(theme, false).Render("  ") +
		promptFieldLabelStyle(theme, true).Render(label) +
		renderOverlayPromptValue(promptFieldValueStyle(theme, true), value, valueWidth)
}

func renderOverlayPromptFormField(theme uiTheme, prompt *modal.PromptState, fieldIndex int, innerWidth int) string {
	return renderOverlayPromptFormFieldWithCursor(theme, prompt, fieldIndex, innerWidth, true)
}

func renderOverlayPromptFormFieldWithCursor(theme uiTheme, prompt *modal.PromptState, fieldIndex int, innerWidth int, cursorVisible bool) string {
	_ = cursorVisible
	if prompt == nil || fieldIndex < 0 || fieldIndex >= len(prompt.Fields) {
		return ""
	}
	field := prompt.Fields[fieldIndex]
	active := fieldIndex == prompt.ActiveField
	valueStyle := promptFieldValueStyle(theme, active)
	value := field.Value
	if !active && value == "" && strings.TrimSpace(field.Placeholder) != "" {
		value = field.Placeholder
		valueStyle = valueStyle.Foreground(lipgloss.Color(theme.panelMuted))
	}
	label := field.Label
	if field.Required {
		label += "*"
	}
	label += ": "
	prefix := "  " + label
	valueWidth := maxInt(0, innerWidth-xansi.StringWidth(prefix))
	return promptFieldMarkerStyle(theme, false).Render("  ") +
		promptFieldLabelStyle(theme, active).Render(label) +
		renderOverlayPromptValue(valueStyle, value, valueWidth)
}

func renderOverlayPromptValue(style lipgloss.Style, value string, width int) string {
	return renderOverlaySpan(style, value, width)
}

func promptCursorIndex(prompt *modal.PromptState) int {
	if prompt == nil {
		return 0
	}
	runes := []rune(prompt.Value)
	cursor := prompt.Cursor
	if cursor < 0 || cursor > len(runes) {
		return len(runes)
	}
	return cursor
}

func promptFieldCursorIndex(field modal.PromptField) int {
	runes := []rune(field.Value)
	cursor := field.Cursor
	if cursor < 0 {
		return 0
	}
	if cursor > len(runes) {
		return len(runes)
	}
	return cursor
}

func queryCursorIndex(query string, cursor int, cursorSet bool) int {
	runes := []rune(query)
	if !cursorSet {
		return len(runes)
	}
	if cursor < 0 {
		return 0
	}
	if cursor > len(runes) {
		return len(runes)
	}
	return cursor
}

func valueCursorCellOffset(value string, cursor int, maxWidth int) int {
	runes := []rune(value)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}
	offset := xansi.StringWidth(string(runes[:cursor]))
	if maxWidth <= 0 {
		return 0
	}
	if offset >= maxWidth {
		return maxWidth - 1
	}
	return offset
}

func pickerOverlayCursorTarget(picker *modal.PickerState, termSize TermSize) (int, int, bool) {
	if picker == nil {
		return 0, 0, false
	}
	width, height := overlayViewport(termSize)
	layout := buildPickerCardLayout(width, height, len(picker.VisibleItems()), false)
	rect := pickerQueryRowRect(layout)
	return rect.X + valueCursorCellOffset(picker.Query, queryCursorIndex(picker.Query, picker.Cursor, picker.CursorSet), rect.W), rect.Y, true
}

func workspacePickerOverlayCursorTarget(picker *modal.WorkspacePickerState, termSize TermSize) (int, int, bool) {
	if picker == nil {
		return 0, 0, false
	}
	width, height := overlayViewport(termSize)
	layout := buildWorkbenchTreeCardLayout(width, height, len(picker.VisibleItems()), picker.Selected)
	rect := layout.queryRect
	return rect.X + valueCursorCellOffset(picker.Query, queryCursorIndex(picker.Query, picker.Cursor, picker.CursorSet), rect.W), rect.Y, true
}

func terminalManagerOverlayCursorTarget(manager *modal.TerminalManagerState, termSize TermSize) (int, int, bool) {
	if manager == nil {
		return 0, 0, false
	}
	width, height := overlayViewport(termSize)
	layout := buildPickerCardLayout(width, height, len(manager.VisibleItems()), len(terminalManagerFooterActionSpecs()) > 0)
	rect := pickerQueryRowRect(layout)
	return rect.X + valueCursorCellOffset(manager.Query, queryCursorIndex(manager.Query, manager.Cursor, manager.CursorSet), rect.W), rect.Y, true
}

func renderOverlaySpan(style lipgloss.Style, value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) > width {
		value = lipgloss.NewStyle().MaxWidth(width).Render(value)
	}
	return style.Width(width).Render(value)
}

func renderOverlayFooterActionLabel(theme uiTheme, label string) string {
	key, text := splitOverlayFooterLabel(label)
	switch {
	case key != "" && text != "":
		return overlayFooterKeyStyle(theme).Render(key) + overlayFooterTextStyle(theme).Render(text)
	case key != "":
		return overlayFooterKeyStyle(theme).Render(key)
	default:
		return overlayFooterPlainStyle(theme).Render(label)
	}
}

func splitOverlayFooterLabel(label string) (string, string) {
	label = strings.TrimSpace(label)
	if !strings.HasPrefix(label, "[") {
		return "", label
	}
	end := strings.Index(label, "]")
	if end <= 0 {
		return "", label
	}
	key := label[:end+1]
	text := strings.TrimSpace(label[end+1:])
	if text != "" {
		text = " " + text
	}
	return key, text
}

func forceWidthANSIOverlay(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) >= width {
		return lipgloss.NewStyle().MaxWidth(width).Render(s)
	}
	return s + strings.Repeat(" ", width-lipgloss.Width(s))
}

func centerText(s string, width int) string {
	if width <= 0 {
		return ""
	}
	textWidth := lipgloss.Width(s)
	if textWidth >= width {
		return forceWidthANSIOverlay(s, width)
	}
	left := (width - textWidth) / 2
	right := width - textWidth - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

func forceHeight(s string, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) >= height {
		return strings.Join(lines[:height], "\n")
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func padOverlayRight(text string, width int) string {
	if lipgloss.Width(text) >= width {
		return text
	}
	return text + strings.Repeat(" ", width-lipgloss.Width(text))
}

func padLines(lines []string, width int, height int) []string {
	out := make([]string, 0, height)
	for _, line := range lines {
		out = append(out, padOverlayRight(line, width))
	}
	for len(out) < height {
		out = append(out, strings.Repeat(" ", width))
	}
	if len(out) > height {
		out = out[:height]
	}
	return out
}

func maxLineWidth(s string) int {
	width := 0
	for _, line := range strings.Split(s, "\n") {
		if current := lipgloss.Width(line); current > width {
			width = current
		}
	}
	return width
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func coalesce(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
