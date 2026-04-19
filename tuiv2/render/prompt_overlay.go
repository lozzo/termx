package render

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/lozzow/termx/tuiv2/modal"
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
				lines[inputLine] = renderOverlayPromptFormFieldWithCursor(theme, prompt, activeField, pickerInnerWidth(width), cursorVisible)
			}
		}
	} else if len(inputLines) > 0 {
		inputLine := inputLines[0]
		if inputLine >= 0 && inputLine < len(lines) {
			lines[inputLine] = renderOverlayPromptFieldWithCursor(theme, prompt, pickerInnerWidth(width), cursorVisible)
		}
	}
	footerLine, _ := layoutOverlayFooterActionsWithTheme(theme, promptFooterActionSpecs(prompt), workbench.Rect{W: pickerInnerWidth(width), H: 1})
	footer := footerLine
	if strings.TrimSpace(footer) == "" {
		footer = prompt.Hint
	}
	return renderPickerCardLinesWithTheme(theme, coalesce(prompt.Title, "Prompt"), "", lines, footer, width, height)
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
			lines = append(lines, field.Label+": "+field.Value)
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
	return prompt.Value
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
		return rect.X + valueCursorCellOffset(prompt.Fields[activeField].Value, promptFieldCursorIndex(prompt.Fields[activeField]), rect.W), rect.Y, true
	}
	if len(inputLines) == 0 {
		return 0, 0, false
	}
	rect := promptInputRect(layout, prompt, inputLines[0])
	return rect.X + valueCursorCellOffset(prompt.Value, promptCursorIndex(prompt), rect.W), rect.Y, true
}
