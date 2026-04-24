package render

import (
	"strings"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/uiinput"
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

func pickerFooterRowY(layout pickerCardLayout) int {
	return layout.firstItemY + layout.listHeight + 1
}

func pickerQueryRowRect(layout pickerCardLayout) workbench.Rect {
	return overlayQueryInputRect(layout, uiinput.PromptWidth(overlaySearchPrompt()))
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
	prefixWidth := uiinput.PromptWidth(overlayPromptString(strings.TrimSpace(label)))
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
	prefixWidth := uiinput.PromptWidth(overlayPromptFormString(strings.TrimSpace(label), fieldIndex == prompt.ActiveField))
	editableX := layout.cardX + 1 + maxInt(0, prefixWidth)
	editableW := maxInt(1, layout.innerWidth-maxInt(0, prefixWidth))
	return workbench.Rect{
		X: editableX,
		Y: layout.firstItemY + inputLine,
		W: editableW,
		H: 1,
	}
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

func renderPickerCardLinesWithTheme(theme uiTheme, title, header string, items []string, footer string, width, height int) []string {
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
	return placeOverlayCardLines(theme, layout.width, layout.contentHeight, layout.cardX, layout.cardY, layout.cardWidth, cardLines)
}

func placeOverlayCardLines(theme uiTheme, width, height, cardX, cardY, cardWidth int, cardLines []string) []string {
	if height <= 0 || width <= 0 {
		return nil
	}
	bodyStyle := terminalPickerBodyStyle(theme)
	out := make([]string, height)
	blank := bodyStyle.Render(strings.Repeat(" ", width))
	for i := range out {
		out[i] = blank
	}
	leftPad := strings.Repeat(" ", maxInt(0, cardX))
	rightWidth := maxInt(0, width-cardX-cardWidth)
	rightPad := strings.Repeat(" ", rightWidth)
	for i, line := range cardLines {
		y := cardY + i
		if y < 0 || y >= height {
			continue
		}
		line = offsetCHAANSI(line, cardX)
		row := leftPad + line + rightPad
		out[y] = bodyStyle.Render(forceWidthANSIOverlay(row, width))
	}
	return out
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
	return border.Render("│") + forceWidthANSIOverlay(offsetCHAANSI(content, 1), innerWidth) + border.Render("│")
}

func pickerInnerWidth(termWidth int) int {
	modalWidth := minInt(maxInt(54, termWidth/2), 84)
	modalWidth = minInt(modalWidth, maxInt(30, termWidth-12))
	return maxInt(24, modalWidth-2)
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
	return renderOverlaySearchInput(theme, uiinput.FromLegacy(query, cursor, cursorSet, ""), innerWidth)
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

func renderOverlayPromptFieldWithCursor(theme uiTheme, prompt *modal.PromptState, innerWidth int, cursorVisible bool) string {
	_ = cursorVisible
	if prompt == nil {
		return ""
	}
	label := promptFieldLabel(prompt.Kind)
	return renderOverlayTextInput(theme, prompt.ValueState(), promptInputRenderConfig(theme, overlayPromptString(label), true, innerWidth))
}

func renderOverlayPromptFormFieldWithCursor(theme uiTheme, prompt *modal.PromptState, fieldIndex int, innerWidth int, cursorVisible bool) string {
	_ = cursorVisible
	if prompt == nil || fieldIndex < 0 || fieldIndex >= len(prompt.Fields) {
		return ""
	}
	field := prompt.Fields[fieldIndex]
	active := fieldIndex == prompt.ActiveField
	label := field.Label
	if field.Required {
		label += "*"
	}
	return renderOverlayTextInput(theme, field.ValueState(), promptFormInputRenderConfig(theme, overlayPromptFormString(label, active), active, innerWidth))
}

func renderOverlayPromptValue(style lipgloss.Style, value string, width int) string {
	return renderOverlaySpan(style, value, width)
}

func promptCursorIndex(prompt *modal.PromptState) int {
	if prompt == nil {
		return 0
	}
	return prompt.ValueState().Position()
}

func promptFieldCursorIndex(field modal.PromptField) int {
	return field.ValueState().Position()
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
	return rect.X + picker.QueryState().CursorCellOffset(uiinput.RenderConfig{Width: rect.W}), rect.Y, true
}

func workspacePickerOverlayCursorTarget(picker *modal.WorkspacePickerState, termSize TermSize) (int, int, bool) {
	if picker == nil {
		return 0, 0, false
	}
	width, height := overlayViewport(termSize)
	layout := buildWorkbenchTreeCardLayout(width, height, len(picker.VisibleItems()), picker.Selected)
	rect := layout.queryRect
	return rect.X + picker.QueryState().CursorCellOffset(uiinput.RenderConfig{Width: rect.W}), rect.Y, true
}

func overlaySearchPrompt() string {
	return "  search: "
}

func overlayPromptString(label string) string {
	return "  " + strings.TrimSpace(label) + ": "
}

const overlayPromptActiveIcon = " "

func overlayPromptFormString(label string, active bool) string {
	prefix := strings.Repeat(" ", xansi.StringWidth(overlayPromptActiveIcon))
	if active {
		prefix = overlayPromptActiveIcon
	}
	return prefix + strings.TrimSpace(label) + ": "
}

func renderOverlaySearchInput(theme uiTheme, state uiinput.State, innerWidth int) string {
	return renderOverlayTextInput(theme, state, promptInputRenderConfig(theme, overlaySearchPrompt(), true, innerWidth))
}

func renderOverlayTextInput(theme uiTheme, state uiinput.State, cfg uiinput.RenderConfig) string {
	return renderOverlaySpan(overlayCardFillStyle(theme), state.Render(cfg), maxInt(0, uiinput.PromptWidth(cfg.Prompt)+cfg.Width))
}

func promptInputRenderConfig(theme uiTheme, prompt string, active bool, innerWidth int) uiinput.RenderConfig {
	return promptInputRenderConfigWithBackground(theme, prompt, active, innerWidth, theme.fieldBG)
}

func promptFormInputRenderConfig(theme uiTheme, prompt string, active bool, innerWidth int) uiinput.RenderConfig {
	return promptInputRenderConfigWithBackground(theme, prompt, active, innerWidth, overlayCardBG(theme))
}

func promptInputRenderConfigWithBackground(theme uiTheme, prompt string, active bool, innerWidth int, background string) uiinput.RenderConfig {
	textFG := theme.fieldText
	promptFG := theme.panelMuted
	if active {
		promptFG = theme.fieldAccent
	}
	return uiinput.RenderConfig{
		Prompt: prompt,
		Width:  maxInt(1, uiinput.ValueWidth(innerWidth, prompt)),
		PromptStyle: uiinput.Style{
			Foreground: promptFG,
			Background: background,
		},
		TextStyle: uiinput.Style{
			Foreground: textFG,
			Background: background,
		},
		PlaceholderStyle: uiinput.Style{
			Foreground: theme.panelMuted,
			Background: background,
		},
		CompletionStyle: uiinput.Style{
			Foreground: theme.panelMuted,
			Background: background,
		},
	}
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
