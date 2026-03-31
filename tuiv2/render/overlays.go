package render

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lozzow/termx/tuiv2/modal"
)

func renderPickerOverlay(picker *modal.PickerState, termSize TermSize) string {
	if picker == nil {
		return ""
	}
	width := maxInt(termSize.Width, 80)
	height := maxInt(termSize.Height, 24)
	innerWidth := pickerInnerWidth(width)
	items := picker.VisibleItems()
	itemLines := make([]string, 0, len(items))
	for index := range items {
		item := items[index]
		itemLines = append(itemLines, item.RenderLine(innerWidth, index == picker.Selected, pickerLineStyle, pickerSelectedLineStyle, pickerCreateRowStyle))
	}
	return renderPickerCard(coalesce(picker.Title, "Terminal Picker"), picker.Query, itemLines, coalesce(picker.Footer, "[Enter] attach  [Esc] close"), width, height)
}

func renderPromptOverlay(prompt *modal.PromptState, termSize TermSize) string {
	if prompt == nil {
		return ""
	}
	width := maxInt(termSize.Width, 80)
	height := maxInt(termSize.Height, 24)
	value := prompt.Value
	field := "value"
	switch prompt.Kind {
	case "create-terminal-name":
		field = "name"
	case "create-terminal-tags":
		field = "tags"
	}
	lines := []string{field + ": " + value + "_"}
	if prompt.Name != "" && prompt.Kind == "create-terminal-tags" {
		lines = append(lines, "")
		lines = append(lines, "name: "+prompt.Name)
	}
	footer := prompt.Hint
	if footer == "" {
		footer = "[Enter] continue  [Esc] cancel"
	}
	return renderPickerCard(coalesce(prompt.Title, "Prompt"), "", lines, footer, width, height)
}

func renderWorkspacePickerOverlay(picker *modal.WorkspacePickerState, termSize TermSize) string {
	if picker == nil {
		return ""
	}
	width := maxInt(termSize.Width, 80)
	height := maxInt(termSize.Height, 24)
	innerWidth := pickerInnerWidth(width)
	items := picker.VisibleItems()
	itemLines := make([]string, 0, len(items))
	for index := range items {
		item := items[index]
		itemLines = append(itemLines, item.RenderLine(innerWidth, index == picker.Selected, pickerLineStyle, pickerSelectedLineStyle, pickerCreateRowStyle))
	}
	return renderPickerCard(
		coalesce(picker.Title, "Workspaces"),
		picker.Query,
		itemLines,
		coalesce(picker.Footer, "[Enter] switch  [Esc] close"),
		width,
		height,
	)
}

func renderHelpOverlay(help *modal.HelpState, termSize TermSize) string {
	if help == nil {
		return ""
	}
	width := maxInt(termSize.Width, 80)
	height := maxInt(termSize.Height, 24)
	innerWidth := pickerInnerWidth(width)
	lines := make([]string, 0, len(help.Bindings))
	for _, binding := range help.Bindings {
		lines = append(lines, forceWidthANSIOverlay(binding.Key+"  "+binding.Action, innerWidth))
	}
	return renderPickerCard("Help", "", lines, "[Esc] close", width, height)
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
	contentHeight := maxInt(1, height-2)
	innerWidth := pickerInnerWidth(width)
	maxListHeight := maxInt(4, minInt(10, contentHeight-8))
	listHeight := minInt(maxInt(4, len(items)), maxListHeight)
	modalHeight := minInt(maxInt(8, listHeight+4), maxInt(8, contentHeight-2))
	listHeight = maxInt(1, modalHeight-4)

	lines := make([]string, 0, modalHeight)
	lines = append(lines, centeredPickerBorderLine("top", innerWidth, title))
	lines = append(lines, centeredPickerContentLine("", innerWidth))
	lines = append(lines, centeredPickerContentLine(terminalPickerQueryStyle.Render(forceWidthANSIOverlay("search: "+query+"_", innerWidth)), innerWidth))
	for i := 0; i < listHeight; i++ {
		content := ""
		if i < len(items) {
			content = items[i]
		}
		lines = append(lines, centeredPickerContentLine(content, innerWidth))
	}
	lines = append(lines, centeredPickerContentLine("", innerWidth))
	lines = append(lines, centeredPickerContentLine(pickerFooterStyle.Render(forceWidthANSIOverlay(footer, innerWidth)), innerWidth))
	lines = append(lines, centeredPickerBorderLine("bottom", innerWidth, ""))

	card := strings.Join(lines, "\n")
	body := lipgloss.Place(width, contentHeight, lipgloss.Center, lipgloss.Center, card, lipgloss.WithWhitespaceChars(" "), lipgloss.WithWhitespaceBackground(lipgloss.Color("#020617")))
	return terminalPickerBodyStyle.Render(forceHeight(body, contentHeight))
}

func pickerInnerWidth(termWidth int) int {
	modalWidth := minInt(maxInt(54, termWidth/2), 84)
	modalWidth = minInt(modalWidth, maxInt(30, termWidth-12))
	return maxInt(24, modalWidth-2)
}

func centeredPickerBorderLine(edge string, innerWidth int, title string) string {
	switch edge {
	case "top":
		title = lipgloss.NewStyle().MaxWidth(innerWidth).Render(" " + title + " ")
		return pickerBorderStyle.Render("┌") + terminalPickerTitleStyle.Render(title) + pickerBorderStyle.Render(strings.Repeat("─", maxInt(0, innerWidth-lipgloss.Width(title)))) + pickerBorderStyle.Render("┐")
	default:
		return pickerBorderStyle.Render("└" + strings.Repeat("─", innerWidth) + "┘")
	}
}

func centeredPickerContentLine(content string, innerWidth int) string {
	return pickerBorderStyle.Render("│") + forceWidthANSIOverlay(content, innerWidth) + pickerBorderStyle.Render("│")
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
