package render

import (
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
	prefixWidth := xansi.StringWidth(promptFieldLabel(prompt.Kind) + ": ")
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
	if rowRect.W <= 0 || rowRect.H <= 0 || len(specs) == 0 {
		return "", nil
	}
	var builder strings.Builder
	actions := make([]overlayFooterActionLayout, 0, len(specs))
	currentX := 0
	for _, spec := range specs {
		label := renderOverlayFooterActionLabel(spec.Label)
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
			builder.WriteString(strings.Repeat(" ", overlayFooterActionGap))
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
	if picker == nil {
		return ""
	}
	width, height := overlayViewport(termSize)
	innerWidth := pickerInnerWidth(width)
	items := picker.VisibleItems()
	itemLines := make([]string, 0, len(items))
	for index := range items {
		item := items[index]
		itemLines = append(itemLines, item.RenderLineWithPrefix(innerWidth, index == picker.Selected, "  ", "> ", pickerLineStyle, pickerSelectedLineStyle, pickerCreateRowStyle))
	}
	footerLine, _ := layoutOverlayFooterActions(pickerFooterActionSpecs(), workbench.Rect{W: innerWidth, H: 1})
	return renderPickerCard(coalesce(picker.Title, "Terminal Picker"), picker.Query, itemLines, footerLine, width, height)
}

func renderPromptOverlay(prompt *modal.PromptState, termSize TermSize) string {
	if prompt == nil {
		return ""
	}
	width, height := overlayViewport(termSize)
	lines, inputLine := promptOverlayContent(prompt)
	if inputLine >= 0 && inputLine < len(lines) {
		lines[inputLine] = renderOverlayPromptField(prompt)
	}
	footerLine, _ := layoutOverlayFooterActions(promptFooterActionSpecs(prompt), workbench.Rect{W: pickerInnerWidth(width), H: 1})
	footer := footerLine
	if strings.TrimSpace(footer) == "" {
		footer = prompt.Hint
	}
	return renderPickerCard(coalesce(prompt.Title, "Prompt"), "", lines, footer, width, height)
}

func promptOverlayContent(prompt *modal.PromptState) ([]string, int) {
	if prompt == nil {
		return nil, -1
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
	return lines, inputLine
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
	if picker == nil {
		return ""
	}
	width, height := overlayViewport(termSize)
	innerWidth := pickerInnerWidth(width)
	items := picker.VisibleItems()
	itemLines := make([]string, 0, len(items))
	for index := range items {
		item := items[index]
		itemLines = append(itemLines, item.RenderLine(innerWidth, index == picker.Selected, pickerLineStyle, pickerSelectedLineStyle, pickerCreateRowStyle))
	}
	footerLine, _ := layoutOverlayFooterActions(workspacePickerFooterActionSpecs(), workbench.Rect{W: innerWidth, H: 1})
	return renderPickerCard(
		coalesce(picker.Title, "Workspaces"),
		picker.Query,
		itemLines,
		footerLine,
		width,
		height,
	)
}

func renderTerminalManagerOverlay(manager *modal.TerminalManagerState, termSize TermSize) string {
	if manager == nil {
		return ""
	}
	width, height := overlayViewport(termSize)
	innerWidth := pickerInnerWidth(width)
	items := manager.VisibleItems()
	itemLines := make([]string, 0, len(items))
	for index := range items {
		item := items[index]
		itemLines = append(itemLines, item.RenderLine(innerWidth, index == manager.Selected, pickerLineStyle, pickerSelectedLineStyle, pickerCreateRowStyle))
	}
	if detailLines := renderTerminalManagerDetails(manager.SelectedItem(), innerWidth); len(detailLines) > 0 {
		itemLines = append(itemLines, "")
		itemLines = append(itemLines, detailLines...)
	}
	footerLine, _ := layoutOverlayFooterActions(terminalManagerFooterActionSpecs(), workbench.Rect{W: innerWidth, H: 1})
	return renderPickerCard(
		coalesce(manager.Title, "Terminal Manager"),
		manager.Query,
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
	if help == nil {
		return ""
	}
	width, height := overlayViewport(termSize)
	innerWidth := pickerInnerWidth(width)
	lines := helpOverlayLines(help, innerWidth)
	return renderPickerCard("Help", "", lines, "", width, height)
}

func helpOverlayLines(help *modal.HelpState, innerWidth int) []string {
	if help == nil {
		return nil
	}
	lines := make([]string, 0)
	for _, section := range help.Sections {
		lines = append(lines, forceWidthANSIOverlay(overlaySectionTitleStyle.Render("▍ "+section.Title), innerWidth))
		for _, binding := range section.Bindings {
			line := overlayHelpKeyStyle.Render(binding.Key) + "  " + overlayHelpActionStyle.Render(binding.Action)
			lines = append(lines, forceWidthANSIOverlay(line, innerWidth))
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
	layout := buildPickerCardLayout(width, height, len(items), strings.TrimSpace(footer) != "")

	lines := make([]string, 0, layout.listHeight+layout.fixedRows)
	lines = append(lines, centeredPickerBorderLine("top", layout.innerWidth, title))
	lines = append(lines, centeredPickerContentLine("", layout.innerWidth))
	lines = append(lines, centeredPickerContentLine(renderOverlaySearchLine(query, layout.innerWidth), layout.innerWidth))
	for i := 0; i < layout.listHeight; i++ {
		content := ""
		if i < len(items) {
			content = items[i]
		}
		lines = append(lines, centeredPickerContentLine(content, layout.innerWidth))
	}
	lines = append(lines, centeredPickerContentLine("", layout.innerWidth))
	if layout.hasFooter {
		lines = append(lines, centeredPickerContentLine(renderOverlayFooterLine(footer, layout.innerWidth), layout.innerWidth))
	}
	lines = append(lines, centeredPickerBorderLine("bottom", layout.innerWidth, ""))

	card := strings.Join(lines, "\n")
	body := lipgloss.Place(
		layout.width,
		layout.contentHeight,
		lipgloss.Center,
		lipgloss.Center,
		card,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceStyle(
			lipgloss.NewStyle().Background(lipgloss.Color("#050816")),
		),
	)
	return terminalPickerBodyStyle.Render(forceHeight(body, layout.contentHeight))
}

func pickerInnerWidth(termWidth int) int {
	modalWidth := minInt(maxInt(54, termWidth/2), 84)
	modalWidth = minInt(modalWidth, maxInt(30, termWidth-12))
	return maxInt(24, modalWidth-2)
}

func centeredPickerBorderLine(edge string, innerWidth int, title string) string {
	switch edge {
	case "top":
		title = terminalPickerTitleStyle.Render(lipgloss.NewStyle().MaxWidth(maxInt(0, innerWidth)).Render(title))
		return pickerBorderStyle.Render("╭") + title + pickerBorderStyle.Render(strings.Repeat("─", maxInt(0, innerWidth-lipgloss.Width(title)))) + pickerBorderStyle.Render("╮")
	default:
		return pickerBorderStyle.Render("╰" + strings.Repeat("─", innerWidth) + "╯")
	}
}

func centeredPickerContentLine(content string, innerWidth int) string {
	row := forceWidthANSIOverlay(content, innerWidth)
	return pickerBorderStyle.Render("│") + overlayCardFillStyle.Render(row) + pickerBorderStyle.Render("│")
}

func renderOverlaySearchLine(query string, innerWidth int) string {
	value := query + "_"
	row := overlayFieldPrefixStyle.Render("search: ") + overlayFieldValueStyle.Render(value)
	remain := innerWidth - xansi.StringWidth("search: ") - xansi.StringWidth(value)
	if remain > 0 {
		row += overlayFieldValueStyle.Render(strings.Repeat(" ", remain))
	}
	return terminalPickerQueryStyle.Render(forceWidthANSIOverlay(row, innerWidth))
}

func renderOverlayFooterLine(footer string, innerWidth int) string {
	return pickerFooterStyle.Render(forceWidthANSIOverlay(footer, innerWidth))
}

func renderOverlayPromptField(prompt *modal.PromptState) string {
	if prompt == nil {
		return ""
	}
	value := promptValueWithCursor(prompt)
	row := overlayFieldPrefixStyle.Render(promptFieldLabel(prompt.Kind)+": ") + overlayFieldValueStyle.Render(value)
	return row
}

func renderOverlayFooterActionLabel(label string) string {
	key, text := splitOverlayFooterLabel(label)
	switch {
	case key != "" && text != "":
		return overlayFooterKeyStyle.Render(key) + overlayFooterTextStyle.Render(text)
	case key != "":
		return overlayFooterKeyStyle.Render(key)
	default:
		return overlayFooterPlainStyle.Render(label)
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
