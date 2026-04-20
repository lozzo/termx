package render

import (
	"strings"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/uiinput"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func renderPromptOverlay(prompt *modal.PromptState, termSize TermSize) string {
	return strings.Join(renderPromptOverlayLinesWithThemeAndCursor(prompt, termSize, defaultUITheme(), true), "\n")
}

func renderPromptOverlayLinesWithThemeAndCursor(prompt *modal.PromptState, termSize TermSize, theme uiTheme, cursorVisible bool) []string {
	if prompt == nil {
		return nil
	}
	width, height := overlayViewport(termSize)
	lines, inputLines := promptOverlayContent(prompt)
	lineCount := len(lines)
	innerWidth := pickerInnerWidth(width)
	var floatingPopup promptSuggestionPopupLayout
	if prompt.IsForm() {
		activeField := prompt.ActiveField
		if activeField < 0 {
			activeField = 0
		}
		if activeField >= len(prompt.Fields) {
			activeField = len(prompt.Fields) - 1
		}
		if activeField >= 0 && activeField < len(inputLines) && activeField < len(prompt.Fields) {
			inputLine := inputLines[activeField]
			if inputLine >= 0 && inputLine < len(lines) {
				lines[inputLine] = renderOverlayPromptFormFieldWithCursor(theme, prompt, activeField, innerWidth, cursorVisible)
				popup := buildPromptSuggestionPopupLayout(theme, prompt.Fields[activeField], prompt.PromptSuggestionSelected, inputLine, innerWidth)
				if popup.visible {
					floatingPopup = popup
				}
			}
		}
	} else if len(inputLines) > 0 {
		inputLine := inputLines[0]
		if inputLine >= 0 && inputLine < len(lines) {
			lines[inputLine] = renderOverlayPromptFieldWithCursor(theme, prompt, innerWidth, cursorVisible)
		}
	}
	footerLine, _ := layoutOverlayFooterActionsWithTheme(theme, promptFooterActionSpecs(prompt), workbench.Rect{W: innerWidth, H: 1})
	footer := footerLine
	if strings.TrimSpace(footer) == "" {
		footer = prompt.Hint
	}
	hasFooter := strings.TrimSpace(footer) != ""
	out := renderPickerCardLinesWithTheme(theme, coalesce(prompt.Title, "Prompt"), "", lines, footer, width, height)
	if floatingPopup.visible {
		layout := buildPickerCardLayout(width, height, lineCount, hasFooter)
		popupTopY := promptSuggestionPopupTopY(layout, floatingPopup)
		for rowIndex, popupRow := range floatingPopup.rows {
			absY := popupTopY + rowIndex
			if absY < 0 || absY >= len(out) {
				continue
			}
			out[absY] = buildFloatingPopupRow(out[absY], layout, floatingPopup, popupRow, width)
		}
	}
	return out
}

func promptOverlayContent(prompt *modal.PromptState) ([]string, []int) {
	if prompt == nil {
		return nil, nil
	}
	if prompt.IsForm() {
		lines := make([]string, 0, len(prompt.Fields)+3)
		if hint := strings.TrimSpace(prompt.Hint); hint != "" {
			lines = append(lines, hint)
			lines = append(lines, "")
		}
		inputLines := make([]int, 0, len(prompt.Fields))
		for _, field := range prompt.Fields {
			inputLines = append(inputLines, len(lines))
			lines = append(lines, field.Label+": "+field.ValueState().Value())
		}
		return lines, inputLines
	}
	value := promptValueWithCursor(prompt)
	field := promptFieldLabel(prompt.Kind)
	lines := make([]string, 0, 8)
	if step := promptStepLabel(prompt.Kind); step != "" {
		lines = append(lines, "step: "+step)
	}
	if prompt.TerminalID != "" {
		lines = append(lines, "terminal: "+prompt.TerminalID)
	}
	if len(prompt.Command) > 0 {
		lines = append(lines, "command: "+summarizeCommand(prompt.Command))
	}
	if strings.HasPrefix(prompt.Kind, "edit-terminal") {
		lines = append(lines, "editing metadata does not change pane bindings")
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	inputLine := len(lines)
	lines = append(lines, field+": "+value)
	if prompt.Name != "" && promptShowsNameSummary(prompt.Kind) {
		lines = append(lines, "")
		lines = append(lines, "name: "+prompt.Name)
	}
	return lines, []int{inputLine}
}

func promptValueWithCursor(prompt *modal.PromptState) string {
	return promptValueWithCursorVisible(prompt, true)
}

func promptValueWithCursorVisible(prompt *modal.PromptState, cursorVisible bool) string {
	_ = cursorVisible
	if prompt == nil {
		return ""
	}
	return prompt.ValueState().Value()
}

func promptFieldLabel(kind string) string {
	switch kind {
	case "create-terminal-name", "edit-terminal-name":
		return "name"
	case "create-terminal-tags", "edit-terminal-tags":
		return "tags"
	default:
		return "value"
	}
}

func promptStepLabel(kind string) string {
	switch kind {
	case "create-terminal-name", "edit-terminal-name":
		return "1/2 name"
	case "create-terminal-tags", "edit-terminal-tags":
		return "2/2 tags"
	default:
		return ""
	}
}

func promptShowsNameSummary(kind string) bool {
	return kind == "create-terminal-tags" || kind == "edit-terminal-tags"
}

func summarizeCommand(command []string) string {
	if len(command) == 0 {
		return ""
	}
	summary := strings.Join(command, " ")
	if lipgloss.Width(summary) <= 48 {
		return summary
	}
	return lipgloss.NewStyle().MaxWidth(48).Render(summary)
}

func promptOverlayCursorTarget(prompt *modal.PromptState, termSize TermSize) (int, int, bool) {
	if prompt == nil {
		return 0, 0, false
	}
	width, height := overlayViewport(termSize)
	lines, inputLines := promptOverlayContent(prompt)
	footerSpecs := promptFooterActionSpecs(prompt)
	footer := ""
	if len(footerSpecs) == 0 && prompt != nil {
		footer = prompt.Hint
	}
	layout := buildPickerCardLayout(width, height, len(lines), len(footerSpecs) > 0 || strings.TrimSpace(footer) != "")
	if prompt.IsForm() {
		activeField := prompt.ActiveField
		if activeField < 0 {
			activeField = 0
		}
		if activeField >= len(prompt.Fields) || activeField >= len(inputLines) {
			return 0, 0, false
		}
		rect := promptFormInputRect(layout, prompt, inputLines[activeField], activeField)
		return rect.X + prompt.Fields[activeField].ValueState().CursorCellOffset(uiinput.RenderConfig{Width: rect.W}), rect.Y, true
	}
	if len(inputLines) == 0 {
		return 0, 0, false
	}
	rect := promptInputRect(layout, prompt, inputLines[0])
	return rect.X + prompt.ValueState().CursorCellOffset(uiinput.RenderConfig{Width: rect.W}), rect.Y, true
}

func promptFieldSuggestionVisible(field modal.PromptField) bool {
	return strings.TrimSpace(field.SuggestionTitle) != "" || len(field.SuggestionItems) > 0 || strings.TrimSpace(field.SuggestionEmpty) != ""
}

type promptSuggestionPopupLayout struct {
	visible    bool
	startRow   int
	itemStart  int
	leftWidth  int
	popupWidth int
	rightWidth int
	rows       []string
	itemCount  int
}

func promptSuggestionPopupTopY(layout pickerCardLayout, popup promptSuggestionPopupLayout) int {
	return layout.firstItemY + popup.startRow
}

func buildPromptSuggestionPopupLayout(theme uiTheme, field modal.PromptField, selected, inputLine, innerWidth int) promptSuggestionPopupLayout {
	if !promptFieldSuggestionVisible(field) || innerWidth <= 0 {
		return promptSuggestionPopupLayout{}
	}
	label := strings.TrimSpace(field.Label)
	if field.Required {
		label += "*"
	}
	popupX := uiinput.PromptWidth(overlayPromptString(label))
	popupWidth := promptSuggestionPopupWidth(innerWidth, popupX)
	rows := renderPromptFieldSuggestionPopup(theme, field, selected, popupWidth)
	if len(rows) == 0 {
		return promptSuggestionPopupLayout{}
	}
	return promptSuggestionPopupLayout{
		visible:    true,
		startRow:   inputLine + 1,
		itemStart:  1,
		leftWidth:  popupX,
		popupWidth: popupWidth,
		rightWidth: maxInt(0, innerWidth-popupX-popupWidth),
		rows:       rows,
		itemCount:  len(field.SuggestionItems),
	}
}

func renderPromptFieldSuggestionPopup(theme uiTheme, field modal.PromptField, selected int, popupWidth int) []string {
	if !promptFieldSuggestionVisible(field) || popupWidth <= 0 {
		return nil
	}
	if popupWidth <= 8 {
		return nil
	}
	itemWidth := maxInt(0, popupWidth-2) // inner width, inside the │ borders

	itemCount := len(field.SuggestionItems)
	rows := make([]string, 0, 2+maxInt(1, itemCount))

	// Top border: ╭─{path}{─*fill}╮
	pathText := truncateANSI(field.SuggestionTitle, maxInt(0, itemWidth-1))
	pathDisplayW := xansi.StringWidth(pathText)
	topFillLen := maxInt(0, itemWidth-1-pathDisplayW)
	topBorder :=
		promptSuggestionBorderStyle(theme).Render("╭─") +
			promptSuggestionPathStyle(theme).Render(pathText) +
			promptSuggestionBorderStyle(theme).Render(strings.Repeat("─", topFillLen)+"╮")
	rows = append(rows, topBorder)

	if itemCount == 0 {
		emptyRow :=
			promptSuggestionBorderStyle(theme).Render("│") +
				renderOverlaySpan(promptSuggestionFillStyle(theme), truncateANSI(field.SuggestionEmpty, itemWidth), itemWidth) +
				promptSuggestionBorderStyle(theme).Render("│")
		rows = append(rows, emptyRow)
	} else {
		if selected < 0 {
			selected = 0
		}
		if selected >= itemCount {
			selected = itemCount - 1
		}
		for index, item := range field.SuggestionItems {
			name := suggestionItemBasename(item)
			line := "  " + name
			if index == selected {
				line = "▸ " + name
			}
			fill := promptSuggestionFillStyle(theme)
			style := promptSuggestionTextStyle(theme, false)
			if index == selected {
				fill = promptSuggestionSelectedFillStyle(theme)
				style = promptSuggestionSelectedTextStyle(theme)
			}
			itemRow :=
				promptSuggestionBorderStyle(theme).Render("│") +
					renderOverlaySpan(fill, style.Render(truncateANSI(line, itemWidth)), itemWidth) +
					promptSuggestionBorderStyle(theme).Render("│")
			rows = append(rows, itemRow)
		}
	}

	// Bottom border: ╰{─*itemWidth}╯
	bottomBorder := promptSuggestionBorderStyle(theme).Render("╰" + strings.Repeat("─", itemWidth) + "╯")
	rows = append(rows, bottomBorder)

	return rows
}

func suggestionItemBasename(item string) string {
	trimmed := strings.TrimRight(item, "/")
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
		return item[idx+1:]
	}
	return item
}

func promptSuggestionPopupWidth(innerWidth, popupX int) int {
	available := maxInt(0, innerWidth-popupX)
	preferred := minInt(52, available)
	if preferred < 16 {
		return available
	}
	return preferred
}

func promptSuggestionBorderStyle(theme uiTheme) lipgloss.Style {
	bg := promptSuggestionBG(theme)
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ensureContrast(mixHex(theme.panelStrong, theme.chromeAccent, 0.4), bg, 2.8))).
		Background(lipgloss.Color(bg))
}

func promptSuggestionFillStyle(theme uiTheme) lipgloss.Style {
	bg := promptSuggestionBG(theme)
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelText)).
		Background(lipgloss.Color(bg))
}

func promptSuggestionSelectedFillStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.selectedText)).
		Background(lipgloss.Color(theme.selectedBG))
}

func promptSuggestionPathStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelMuted)).
		Background(lipgloss.Color(promptSuggestionBG(theme)))
}

func promptSuggestionSelectedTextStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(theme.selectedText)).
		Background(lipgloss.Color(theme.selectedBG))
}

func promptSuggestionTextStyle(theme uiTheme, active bool) lipgloss.Style {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelText)).
		Background(lipgloss.Color(promptSuggestionBG(theme)))
	if active {
		style = style.Bold(true).Foreground(lipgloss.Color(ensureContrast(theme.chromeAccent, promptSuggestionBG(theme), 3.8)))
	}
	return style
}

func promptSuggestionEmptyStyle(theme uiTheme, value string) lipgloss.Style {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelMuted)).
		Background(lipgloss.Color(promptSuggestionBG(theme)))
	if strings.Contains(strings.ToLower(value), "not found") {
		style = style.Foreground(lipgloss.Color(theme.danger)).Bold(true)
	}
	return style
}

func promptSuggestionBG(theme uiTheme) string {
	return mixHex(theme.fieldBG, theme.panelStrong, 0.58)
}

func buildFloatingPopupRow(baseRow string, layout pickerCardLayout, popup promptSuggestionPopupLayout, popupRow string, totalWidth int) string {
	popupX := layout.cardX + 1 + popup.leftWidth
	prefix := xansi.Cut(baseRow, 0, popupX)
	suffixStart := minInt(totalWidth, popupX+popup.popupWidth)
	suffix := xansi.Cut(baseRow, suffixStart, totalWidth)
	row := prefix + forceWidthANSIOverlay(popupRow, popup.popupWidth) + suffix
	return forceWidthANSIOverlay(row, totalWidth)
}

func truncateANSI(value string, width int) string {
	if width <= 0 {
		return ""
	}
	return xansi.Truncate(value, width, "")
}
