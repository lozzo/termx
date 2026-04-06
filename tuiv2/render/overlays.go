package render

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
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
		input.ModePicker,
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
	workspaceSpecs := modeFooterActionSpecs(
		input.ModeWorkspace,
		[]input.ActionKind{
			input.ActionCreateWorkspace,
			input.ActionRenameWorkspace,
			input.ActionDeleteWorkspace,
			input.ActionPrevWorkspace,
			input.ActionNextWorkspace,
		},
		map[input.ActionKind]string{
			input.ActionCreateWorkspace: "create",
			input.ActionRenameWorkspace: "rename",
			input.ActionDeleteWorkspace: "delete",
			input.ActionPrevWorkspace:   "prev",
			input.ActionNextWorkspace:   "next",
		},
	)
	pickerSpecs := modeFooterActionSpecs(
		input.ModeWorkspacePicker,
		[]input.ActionKind{
			input.ActionSubmitPrompt,
			input.ActionCancelMode,
		},
		map[input.ActionKind]string{
			input.ActionSubmitPrompt: "open",
			input.ActionCancelMode:   "close",
		},
	)
	if len(pickerSpecs) == 0 {
		return workspaceSpecs
	}
	if len(pickerSpecs) == 1 {
		return append([]overlayFooterActionSpec(nil), append(pickerSpecs, workspaceSpecs...)...)
	}
	return append(pickerSpecs[:1], append(workspaceSpecs, pickerSpecs[1:]...)...)
}

func terminalManagerFooterActionSpecs() []overlayFooterActionSpec {
	return modeFooterActionSpecs(
		input.ModeTerminalManager,
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
		{Label: "[Enter] submit", Action: input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: paneID}},
		{Label: "[Esc] cancel", Action: input.SemanticAction{Kind: input.ActionCancelMode}},
	}
}

func floatingOverviewFooterActionSpecs() []overlayFooterActionSpec {
	return modeFooterActionSpecs(
		input.ModeFloatingOverview,
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

func modeFooterActionSpecs(mode input.ModeKind, order []input.ActionKind, fallback map[input.ActionKind]string) []overlayFooterActionSpec {
	specs := make([]overlayFooterActionSpec, 0, len(order))
	for _, kind := range order {
		label := modeActionFooterLabel(mode, kind, fallback[kind])
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

func modeActionFooterLabel(mode input.ModeKind, action input.ActionKind, fallbackText string) string {
	doc, ok := bindingDocForModeAction(mode, action)
	if !ok {
		if strings.TrimSpace(fallbackText) == "" {
			return ""
		}
		return fallbackText
	}
	key := strings.TrimSpace(doc.KeyLabel)
	if key == "" {
		key = keyLabelFromBinding(doc.Binding)
	}
	text := strings.TrimSpace(doc.FooterText)
	if text == "" {
		text = strings.TrimSpace(fallbackText)
	}
	if key == "" {
		return text
	}
	if text == "" {
		return "[" + key + "]"
	}
	return "[" + key + "] " + text
}

func bindingDocForModeAction(mode input.ModeKind, action input.ActionKind) (input.BindingDoc, bool) {
	for _, doc := range input.DefaultBindingCatalog() {
		if doc.Mode == mode && doc.Binding.Action == action && strings.TrimSpace(doc.KeyLabel) != "" {
			return doc, true
		}
	}
	for _, doc := range input.DefaultBindingCatalog() {
		if doc.Mode == mode && doc.Binding.Action == action {
			return doc, true
		}
	}
	return input.BindingDoc{}, false
}

func keyLabelFromBinding(binding input.Binding) string {
	if binding.Type == tea.KeyRunes {
		if binding.Rune != 0 {
			return string(binding.Rune)
		}
		if binding.RuneMin != 0 || binding.RuneMax != 0 {
			return string(binding.RuneMin) + "-" + string(binding.RuneMax)
		}
	}
	return ""
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

func renderPickerOverlay(picker *modal.PickerState, termSize TermSize) string {
	return renderPickerOverlayWithTheme(picker, termSize, defaultUITheme())
}

func renderPickerOverlayWithTheme(picker *modal.PickerState, termSize TermSize, theme uiTheme) string {
	if picker == nil {
		return ""
	}
	width, height := overlayViewport(termSize)
	innerWidth := pickerInnerWidth(width)
	items := picker.VisibleItems()
	itemLines := make([]string, 0, len(items))
	for index := range items {
		item := items[index]
		itemLines = append(itemLines, item.RenderLineWithPrefix(innerWidth, index == picker.Selected, "  ", "> ", pickerLineStyle(theme), pickerSelectedLineStyle(theme), pickerCreateRowStyle(theme)))
	}
	header := renderOverlaySearchLine(theme, picker.Query, picker.Cursor, picker.CursorSet, innerWidth)
	return renderPickerCardWithTheme(theme, coalesce(picker.Title, "Terminal Picker"), header, itemLines, "", width, height)
}

func renderPromptOverlay(prompt *modal.PromptState, termSize TermSize) string {
	return renderPromptOverlayWithTheme(prompt, termSize, defaultUITheme())
}

func renderPromptOverlayWithTheme(prompt *modal.PromptState, termSize TermSize, theme uiTheme) string {
	if prompt == nil {
		return ""
	}
	width, height := overlayViewport(termSize)
	lines, inputLines := promptOverlayContent(prompt)
	if prompt.IsForm() {
		for fieldIndex, inputLine := range inputLines {
			if inputLine >= 0 && inputLine < len(lines) {
				lines[inputLine] = renderOverlayPromptFormField(theme, prompt, fieldIndex, pickerInnerWidth(width))
			}
		}
	} else if len(inputLines) > 0 {
		inputLine := inputLines[0]
		if inputLine >= 0 && inputLine < len(lines) {
			lines[inputLine] = renderOverlayPromptField(theme, prompt, pickerInnerWidth(width))
		}
	}
	footerLine, _ := layoutOverlayFooterActionsWithTheme(theme, promptFooterActionSpecs(prompt), workbench.Rect{W: pickerInnerWidth(width), H: 1})
	footer := footerLine
	if strings.TrimSpace(footer) == "" {
		footer = prompt.Hint
	}
	return renderPickerCardWithTheme(theme, coalesce(prompt.Title, "Prompt"), "", lines, footer, width, height)
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
	if prompt == nil {
		return ""
	}
	runes := []rune(prompt.Value)
	cursor := prompt.Cursor
	if cursor < 0 || cursor > len(runes) {
		cursor = len(runes)
	}
	return string(runes[:cursor]) + "_" + string(runes[cursor:])
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

func renderWorkspacePickerOverlay(picker *modal.WorkspacePickerState, termSize TermSize) string {
	return renderWorkspacePickerOverlayWithTheme(picker, termSize, defaultUITheme())
}

func renderWorkspacePickerOverlayWithTheme(picker *modal.WorkspacePickerState, termSize TermSize, theme uiTheme) string {
	if picker == nil {
		return ""
	}
	width, height := overlayViewport(termSize)
	innerWidth := pickerInnerWidth(width)
	items := picker.VisibleItems()
	itemLines := make([]string, 0, len(items))
	for index := range items {
		item := items[index]
		itemLines = append(itemLines, item.RenderLineWithPrefix(innerWidth, index == picker.Selected, "  ", "> ", pickerLineStyle(theme), pickerSelectedLineStyle(theme), pickerCreateRowStyle(theme)))
	}
	return renderPickerCardWithTheme(
		theme,
		coalesce(picker.Title, "Workspaces"),
		renderOverlaySearchLine(theme, picker.Query, picker.Cursor, picker.CursorSet, innerWidth),
		itemLines,
		"",
		width,
		height,
	)
}

func renderTerminalManagerOverlay(manager *modal.TerminalManagerState, termSize TermSize) string {
	return renderTerminalManagerOverlayWithTheme(manager, termSize, defaultUITheme())
}

func renderTerminalManagerOverlayWithTheme(manager *modal.TerminalManagerState, termSize TermSize, theme uiTheme) string {
	if manager == nil {
		return ""
	}
	width, height := overlayViewport(termSize)
	innerWidth := pickerInnerWidth(width)
	items := manager.VisibleItems()
	itemLines := make([]string, 0, len(items))
	for index := range items {
		item := items[index]
		itemLines = append(itemLines, item.RenderLine(innerWidth, index == manager.Selected, pickerLineStyle(theme), pickerSelectedLineStyle(theme), pickerCreateRowStyle(theme)))
	}
	if detailLines := renderTerminalManagerDetails(manager.SelectedItem(), innerWidth); len(detailLines) > 0 {
		itemLines = append(itemLines, "")
		itemLines = append(itemLines, detailLines...)
	}
	footerLine, _ := layoutOverlayFooterActionsWithTheme(theme, terminalManagerFooterActionSpecs(), workbench.Rect{W: innerWidth, H: 1})
	return renderPickerCardWithTheme(
		theme,
		coalesce(manager.Title, "Terminal Manager"),
		renderOverlaySearchLine(theme, manager.Query, manager.Cursor, manager.CursorSet, innerWidth),
		itemLines,
		footerLine,
		width,
		height,
	)
}

func renderTerminalManagerDetails(item *modal.PickerItem, innerWidth int) []string {
	if item == nil {
		return nil
	}
	lines := []string{
		forceWidthANSIOverlay("selected: "+coalesce(item.Name, item.TerminalID), innerWidth),
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

func renderHelpOverlay(help *modal.HelpState, termSize TermSize) string {
	return renderHelpOverlayWithTheme(help, termSize, defaultUITheme())
}

func renderHelpOverlayWithTheme(help *modal.HelpState, termSize TermSize, theme uiTheme) string {
	if help == nil {
		return ""
	}
	width, height := overlayViewport(termSize)
	innerWidth := pickerInnerWidth(width)
	lines := helpOverlayLines(theme, help, innerWidth)
	return renderPickerCardWithTheme(theme, "Help", "", lines, "", width, height)
}

func renderFloatingOverviewOverlayWithTheme(overview *modal.FloatingOverviewState, termSize TermSize, theme uiTheme) string {
	if overview == nil {
		return ""
	}
	width, height := overlayViewport(termSize)
	innerWidth := pickerInnerWidth(width)
	itemLines := make([]string, 0, len(overview.Items))
	for index := range overview.Items {
		itemLines = append(itemLines, renderFloatingOverviewItemLine(overview.Items[index], index == overview.Selected, innerWidth, theme))
	}
	footerLine, _ := layoutOverlayFooterActionsWithTheme(theme, floatingOverviewFooterActionSpecs(), workbench.Rect{W: innerWidth, H: 1})
	return renderPickerCardWithTheme(
		theme,
		"Floating Windows",
		"Restore, collapse, close, or summon floating panes",
		itemLines,
		footerLine,
		width,
		height,
	)
}

func renderFloatingOverviewItemLine(item modal.FloatingOverviewItem, selected bool, width int, theme uiTheme) string {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = item.PaneID
	}
	display := string(item.Display)
	if display == "" {
		display = string(workbench.FloatingDisplayExpanded)
	}
	fit := "manual"
	if item.FitMode == workbench.FloatingFitAuto {
		fit = "auto"
	}
	slot := " "
	if item.ShortcutSlot > 0 {
		slot = fmt.Sprintf("%d", item.ShortcutSlot)
	}
	body := fmt.Sprintf("[%s] %s  %s  %s  %dx%d", slot, title, display, fit, item.Rect.W, item.Rect.H)
	style := pickerLineStyle(theme)
	if selected {
		style = pickerSelectedLineStyle(theme)
	} else if item.Display != workbench.FloatingDisplayExpanded {
		style = pickerLineStyle(theme).Foreground(lipgloss.Color(theme.panelMuted))
	}
	return style.Render(forceWidthANSIOverlay(body, width))
}

func helpOverlayLines(theme uiTheme, help *modal.HelpState, innerWidth int) []string {
	if help == nil {
		return nil
	}
	lines := make([]string, 0)
	for _, section := range help.Sections {
		lines = append(lines, renderOverlaySpan(overlaySectionTitleStyle(theme), "▍ "+section.Title, innerWidth))
		for _, binding := range section.Bindings {
			line := overlayHelpKeyStyle(theme).Render(binding.Key) +
				renderOverlaySpan(overlayHelpActionStyle(theme), "", 2) +
				overlayHelpActionStyle(theme).Render(binding.Action)
			lines = append(lines, renderOverlaySpan(overlayHelpActionStyle(theme), line, innerWidth))
		}
		lines = append(lines, "")
	}
	return lines
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
	return renderOverlaySpan(overlayCardFillStyle(theme), header, innerWidth)
}

func renderCardContentRow(theme uiTheme, content string, innerWidth int) string {
	return renderOverlaySpan(overlayCardFillStyle(theme), content, innerWidth)
}

func renderOverlaySearchLine(theme uiTheme, query string, cursor int, cursorSet bool, innerWidth int) string {
	value := queryValueWithCursor(query, cursor, cursorSet)
	label := "search: "
	prefix := "  " + label
	valueWidth := maxInt(0, innerWidth-xansi.StringWidth(prefix))
	row := promptFieldMarkerStyle(theme, false).Render("  ") +
		promptFieldLabelStyle(theme, true).Render(label) +
		renderOverlayPromptValue(promptFieldValueStyle(theme, true), value, valueWidth)
	return renderOverlaySpan(overlayCardFillStyle(theme), row, innerWidth)
}

func queryValueWithCursor(query string, cursor int, cursorSet bool) string {
	runes := []rune(query)
	if !cursorSet {
		cursor = len(runes)
	} else {
		if cursor < 0 {
			cursor = 0
		}
		if cursor > len(runes) {
			cursor = len(runes)
		}
	}
	return string(runes[:cursor]) + "_" + string(runes[cursor:])
}

func renderOverlayFooterLine(theme uiTheme, footer string, innerWidth int) string {
	return renderOverlaySpan(pickerFooterStyle(theme), footer, innerWidth)
}

func renderOverlayPromptField(theme uiTheme, prompt *modal.PromptState, innerWidth int) string {
	if prompt == nil {
		return ""
	}
	value := promptValueWithCursor(prompt)
	label := promptFieldLabel(prompt.Kind) + ": "
	prefix := "  " + label
	valueWidth := maxInt(0, innerWidth-xansi.StringWidth(prefix))
	return promptFieldMarkerStyle(theme, false).Render("  ") +
		promptFieldLabelStyle(theme, true).Render(label) +
		renderOverlayPromptValue(promptFieldValueStyle(theme, true), value, valueWidth)
}

func renderOverlayPromptFormField(theme uiTheme, prompt *modal.PromptState, fieldIndex int, innerWidth int) string {
	if prompt == nil || fieldIndex < 0 || fieldIndex >= len(prompt.Fields) {
		return ""
	}
	field := prompt.Fields[fieldIndex]
	active := fieldIndex == prompt.ActiveField
	valueStyle := promptFieldValueStyle(theme, active)
	value := field.Value
	if active {
		runes := []rune(field.Value)
		cursor := field.Cursor
		if cursor < 0 {
			cursor = 0
		}
		if cursor > len(runes) {
			cursor = len(runes)
		}
		value = string(runes[:cursor]) + "_" + string(runes[cursor:])
	} else if value == "" && strings.TrimSpace(field.Placeholder) != "" {
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
