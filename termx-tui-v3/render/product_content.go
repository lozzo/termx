package render

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

const contentActionWidth = 12

const clipboardHistoryPreviewWidth = 200
const clipboardHistoryBodyRows = 10
const clipboardHistoryMaxBodyRows = 12

const emptyPaneActionCount = 4

const exitedPaneActionCount = 2

const defaultWorkbenchNavigatorTreeWidth = 36

type workbenchNavigatorLayout struct {
	ContentWidth   int
	BodyRows       int
	TreeWidth      int
	RightWidth     int
	ActionRow      int
	SnapshotX      int
	SnapshotY      int
	SnapshotWidth  int
	SnapshotHeight int
}

func EmptyPaneActionCount() int {
	return emptyPaneActionCount
}

func ExitedPaneActionCount() int {
	return exitedPaneActionCount
}

func EmptyPaneActionID(index int) ActionID {
	actions := emptyPaneActions()
	if index < 0 || index >= len(actions) {
		return ""
	}
	return actions[index].ID
}

func ExitedPaneActionID(index int) ActionID {
	actions := liveExitedActions()
	if index < 0 || index >= len(actions) {
		return ""
	}
	return actions[index].ID
}

// empty pane 内容只描述当前 pane 可执行的产品动作，不创建 terminal。
func buildEmptyPaneContent(pane state.PaneState) ContentVM {
	lines, regions, cursor := emptyPaneContentLayout(pane.ID, 0)
	return ContentVM{
		Kind:       ContentEmptyPane,
		Lines:      lines,
		Status:     "unconnected: Attach / Create / Manager / Close",
		Cursor:     cursor,
		Empty:      true,
		HitRegions: regions,
	}
}

func buildEmptyPaneContentWithSelection(pane state.PaneState, selectedIndex int) ContentVM {
	lines, regions, cursor := emptyPaneContentLayout(pane.ID, selectedIndex)
	return ContentVM{
		Kind:       ContentEmptyPane,
		Lines:      lines,
		Status:     "unconnected: Attach / Create / Manager / Close",
		Cursor:     cursor,
		Empty:      true,
		HitRegions: regions,
	}
}

func emptyPaneContentLayout(paneID string, selectedIndex int) ([]Line, []HitRegion, Cursor) {
	actions := emptyPaneActions()
	if selectedIndex < 0 || selectedIndex >= len(actions) {
		selectedIndex = 0
	}
	lines := []Line{centeredStyledLine("unconnected", StyleForeground)}
	regions := make([]HitRegion, 0, len(actions))
	for index, action := range actions {
		selected := index == selectedIndex
		text := emptyPaneActionLabel(action.Label, selected)
		style := action.Style
		if selected && style == StyleForeground {
			style = StyleStrongForeground
		}
		line := centeredStyledLine(text, style)
		lines = append(lines, line)
		regions = append(regions, HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: index + 1, W: DisplayWidth(text), H: 1}, PaneID: paneID, ActionID: action.ID.String()})
	}
	return lines, regions, Cursor{}
}

type emptyPaneActionSpec struct {
	ID    ActionID
	Label string
	Style StyleToken
}

func emptyPaneActions() []emptyPaneActionSpec {
	return []emptyPaneActionSpec{
		{ID: ActionEmptyAttach, Label: "Attach existing terminal", Style: StyleAccent},
		{ID: ActionEmptyCreate, Label: "Create new terminal", Style: StyleSuccess},
		{ID: ActionEmptyManager, Label: "Open terminal manager", Style: StyleForeground},
		{ID: ActionEmptyClose, Label: "Close pane", Style: StyleDangerStrong},
	}
}

func emptyPaneActionLabel(label string, selected bool) string {
	label = strings.TrimSpace(label)
	if selected {
		return "► " + label + " ◄"
	}
	return "[ " + label + " ]"
}

type liveExitedActionSpec struct {
	ID    ActionID
	Label string
	Style StyleToken
}

func liveExitedActions() []liveExitedActionSpec {
	return []liveExitedActionSpec{
		{ID: ActionExitedRestart, Label: "R restart current terminal", Style: StyleWarning},
		{ID: ActionExitedReconnect, Label: "Ctrl-F choose another terminal", Style: StyleMuted},
	}
}

func centeredStyledLine(text string, style StyleToken) Line {
	return Line{Cells: []Cell{styledCell(text, style)}}
}

// empty tab 是 workspace/tab truth，不伪造 pane；用户动作再创建或连接真实 pane。
func buildEmptyTabContent(tab state.TabState) ContentVM {
	title := strings.TrimSpace(tab.Title)
	if title == "" {
		title = tab.ID
	}
	if title == "" {
		title = "tab"
	}
	lines := []Line{
		{Cells: []Cell{styledCell("No panel in tab ", StyleMuted), styledCell(title, StyleAccent)}},
		NewLine(""),
		contentActionLine("attach", "Choose terminal"),
		contentActionLine("create", "New terminal"),
		contentActionLine("manager", "Terminal Pool"),
	}
	return ContentVM{
		Kind:       ContentEmptyPane,
		Lines:      lines,
		Status:     "empty tab: Choose terminal / New terminal / Terminal Pool",
		Cursor:     Cursor{Visible: true, Row: 0, Col: DisplayWidth("No panel in tab ") + DisplayWidth(title), Shape: CursorShapeBar},
		Empty:      true,
		HitRegions: contentActionRegions([]ActionID{ActionEmptyAttach, ActionEmptyCreate, ActionEmptyManager}, "", 2),
	}
}

// empty workspace 表示用户已关闭全部 tab；CTA 只创建真实 tab 或进入 terminal flow。
func buildEmptyWorkspaceContent(workspace state.WorkspaceState) ContentVM {
	title := strings.TrimSpace(workspace.Name)
	if title == "" {
		title = workspace.ID
	}
	if title == "" {
		title = "workspace"
	}
	lines := []Line{
		{Cells: []Cell{styledCell("No tabs in workspace ", StyleMuted), styledCell(title, StyleAccent)}},
		NewLine(""),
		contentActionLine("tab", "Create tab"),
		contentActionLine("create", "New terminal"),
		contentActionLine("manager", "Terminal Pool"),
	}
	return ContentVM{
		Kind:       ContentEmptyPane,
		Lines:      lines,
		Status:     "empty workspace: Create tab / New terminal / Terminal Pool",
		Cursor:     Cursor{Visible: true, Row: 0, Col: DisplayWidth("No tabs in workspace ") + DisplayWidth(title), Shape: CursorShapeBar},
		Empty:      true,
		HitRegions: contentActionRegions([]ActionID{ActionTabCreate, ActionEmptyCreate, ActionEmptyManager}, "", 2),
	}
}

// Terminal Picker 只消费 reducer-owned root；服务端 terminal list 必须先回投 TerminalPoolStore。
func buildTerminalPickerContent(root state.Root, shell state.ShellStore) ContentVM {
	shell = shell.EnsureDefaults()
	query := strings.TrimSpace(shell.Overlay.Query)
	lines := []Line{terminalPickerSearchLine(query)}
	if poolLine, ok := terminalPoolStateLine(root.TerminalPool); ok {
		lines = append(lines, poolLine)
	}
	rowOffset := len(lines)
	rows := state.TerminalPickerItems(root)
	for _, row := range rows {
		lines = append(lines, terminalPickerLine(row, query))
	}
	regions := terminalPickerHitRegions(rows, rowOffset)
	return ContentVM{
		Kind:       ContentTerminalPicker,
		Lines:      lines,
		Status:     terminalPickerStatus(terminalPickerSelectableCount(rows), query),
		Cursor:     Cursor{Visible: true, Row: 0, Col: terminalPickerSearchCursorCol(query), Shape: CursorShapeBar},
		HitRegions: regions,
	}
}

// Terminal Pool Page 是独立管理页面；renderer 只消费 reducer-owned page/list state。
func buildTerminalPoolContent(root state.Root, shell state.ShellStore) ContentVM {
	shell = shell.EnsureDefaults()
	query := strings.TrimSpace(shell.Overlay.Query)
	rows := state.TerminalPoolPageItems(root)
	lines := []Line{
		pageTitleLine("Terminal Pool", "global terminal manager"),
		searchRowLine(query, "shell"),
	}
	if statusLine, ok := terminalPoolPageStateLine(root.TerminalPool, len(rows)); ok {
		lines = append(lines, statusLine)
	}
	rowOffset := len(lines)
	for _, row := range rows {
		lines = append(lines, terminalPoolPageRowLine(row))
	}
	lines = append(lines, terminalPoolDetailLines(rows)...)
	actionOffset := len(lines)
	lines = append(lines,
		contentActionLine("attach", "Attach Here"),
		contentActionLine("edit", "Edit"),
		contentActionLine("kill", "Kill"),
	)
	regions := terminalPoolPageHitRegions(rows, rowOffset)
	regions = append(regions,
		HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset, W: contentActionWidth, H: 1}, Row: -1, ActionID: ActionPoolAttach.String()},
		HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset + 1, W: contentActionWidth, H: 1}, Row: -1, ActionID: ActionPoolEdit.String()},
		HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset + 2, W: contentActionWidth, H: 1}, Row: -1, ActionID: ActionPoolKill.String()},
	)
	return ContentVM{
		Kind:       ContentTerminalPool,
		Lines:      lines,
		Status:     terminalPoolPageStatus(root.TerminalPool, len(rows), query),
		Cursor:     Cursor{Visible: true, Row: 1, Col: searchCursorCol(query), Shape: CursorShapeBar},
		HitRegions: regions,
		Pending:    root.TerminalPool.Status == state.TerminalPoolLoading,
		Empty:      root.TerminalPool.Status == state.TerminalPoolReady && len(rows) == 0,
		Error:      root.TerminalPool.LastError,
	}
}

// Workbench Navigator 展示 reducer-owned workspace/tab/pane 树；右侧 snapshot 只消费当前 TUI 已持有的 live 投影。
func buildWorkbenchTreeContent(root state.Root, shell state.ShellStore) ContentVM {
	shell = shell.EnsureDefaults()
	query := strings.TrimSpace(shell.Overlay.Query)
	rows := state.WorkbenchTreeItems(root)
	layout := workbenchNavigatorLayoutForViewport(root.Viewport)
	lines := workbenchNavigatorLines(root, rows, query, layout)
	rowOffset := 2
	visibleRows := rows
	if len(visibleRows) > layout.BodyRows {
		visibleRows = visibleRows[:layout.BodyRows]
	}
	regions := workbenchTreeHitRegions(visibleRows, rowOffset, layout.TreeWidth)
	regions = append(regions, workbenchNavigatorActionHitRegions(layout)...)
	return ContentVM{
		Kind:       ContentWorkbenchTree,
		Lines:      lines,
		Meta:       workbenchNavigatorMeta(root, rows, layout),
		Status:     workbenchTreeStatus(len(rows), query),
		Cursor:     Cursor{Visible: true, Row: 0, Col: searchCursorCol(query), Shape: CursorShapeBar},
		HitRegions: regions,
		Empty:      len(rows) == 0,
	}
}

// Clipboard History overlay 只消费 reducer-owned clipboard 历史快照。
func buildClipboardHistoryContent(root state.Root, shell state.ShellStore) ContentVM {
	shell = shell.EnsureDefaults()
	query := strings.TrimSpace(shell.Overlay.Query)
	rows := state.ClipboardHistoryItems(root)
	nameWidth := clipboardHistoryNameWidth(shell)
	lines := []Line{clipboardHistorySearchLine(query, nameWidth), clipboardHistoryDividerSpaceLine(nameWidth)}
	rowOffset := len(lines)
	bodyRowLimit := clipboardHistoryBodyRowsForViewport(root.Viewport)
	selectedIndex := clipboardHistorySelectedIndex(rows)
	listStart := clipboardHistoryListStart(selectedIndex, len(rows), bodyRowLimit)
	selectedItem, selectedOK := clipboardHistorySelectedItem(rows)
	// 中文说明：右侧预览展示当前选中项正文，左侧列表只负责选择入口。
	previewSegments := clipboardHistoryPreviewSegments(selectedItem, selectedOK, query, clipboardHistoryPreviewWidth, bodyRowLimit)
	bodyRows := maxInt(bodyRowLimit, len(previewSegments))
	if len(rows) == 0 {
		previewSegments = []clipboardHistoryPreviewSegment{{Text: "No clipboard entries"}}
		bodyRows = bodyRowLimit
	}
	for row := 0; row < bodyRows; row++ {
		entryIndex := listStart + row
		var entry state.ClipboardHistoryItem
		hasEntry := entryIndex >= 0 && entryIndex < len(rows)
		if hasEntry {
			entry = rows[entryIndex]
		}
		preview := clipboardHistoryPreviewSegment{}
		if row < len(previewSegments) {
			preview = previewSegments[row]
		}
		lines = append(lines, clipboardHistoryBodyLine(entry, hasEntry, preview, nameWidth))
	}
	regions := clipboardHistoryHitRegions(rows, rowOffset, listStart, bodyRows, nameWidth)
	regions = append(regions, clipboardHistoryDividerHitRegion(rowOffset, bodyRows, nameWidth))
	return ContentVM{
		Kind:       ContentClipboardHistory,
		Lines:      lines,
		Meta:       ContentMetaVM{ClipboardNameWidth: nameWidth},
		Status:     clipboardHistoryStatus(len(rows), query),
		Cursor:     Cursor{Visible: true, Row: 0, Col: clipboardHistorySearchCursorCol(query), Shape: CursorShapeBar},
		HitRegions: regions,
		Empty:      len(rows) == 0,
	}
}

// Floating Overview 只投影 reducer-owned floating 列表；打开/召回通过 ActionID 回到 app reducer。
func buildFloatingOverviewContent(root state.Root, shell state.ShellStore) ContentVM {
	shell = shell.EnsureDefaults()
	rows := state.FloatingOverviewItems(root)
	lines := []Line{pageTitleLine("Floating Overview", "summon and raise")}
	rowOffset := len(lines)
	for index, row := range rows {
		lines = append(lines, floatingOverviewRowLine(index, row))
	}
	if len(rows) == 0 {
		lines = append(lines, Line{Cells: []Cell{styledCell("No floating panes", StyleMuted)}})
	}
	actionOffset := len(lines)
	lines = append(lines,
		contentActionLine("summon", "Open Selected"),
		contentActionLine("show-all", "Show All"),
		contentActionLine("collapse-all", "Collapse All"),
		contentActionLine("close", "Close Selected"),
	)
	regions := floatingOverviewHitRegions(rows, rowOffset)
	regions = append(regions,
		HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset, W: contentActionWidth, H: 1}, Row: -1, ActionID: ActionFloatingSummon.String()},
		HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset + 1, W: contentActionWidth, H: 1}, Row: -1, ActionID: ActionFloatingShowAll.String()},
		HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset + 2, W: contentActionWidth, H: 1}, Row: -1, ActionID: ActionFloatingCollapseAll.String()},
		HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset + 3, W: contentActionWidth, H: 1}, Row: -1, ActionID: ActionFloatingClose.String()},
	)
	return ContentVM{
		Kind:       ContentFloatingOverview,
		Lines:      lines,
		Status:     floatingOverviewStatus(len(rows)),
		Cursor:     Cursor{Visible: false},
		HitRegions: regions,
		Empty:      len(rows) == 0,
	}
}

type clipboardHistoryPreviewSegment struct {
	Text       string
	MatchIndex []int
}

func clipboardHistoryBodyLine(row state.ClipboardHistoryItem, hasRow bool, preview clipboardHistoryPreviewSegment, nameWidth int) Line {
	cells := clipboardHistoryNameCells(row, hasRow, nameWidth)
	cells = append(cells, styledCell("│", StyleForeground))
	cells = append(cells, clipboardHistoryColumnCells(preview.Text, preview.MatchIndex, StyleForeground, clipboardHistoryPreviewWidth)...)
	return Line{Cells: cells}
}

func clipboardHistoryNameCells(row state.ClipboardHistoryItem, hasRow bool, nameWidth int) []Cell {
	if !hasRow {
		return []Cell{styledCell(strings.Repeat(" ", nameWidth), StylePicker)}
	}
	prefix := "  "
	titleStyle := StylePicker
	if row.Selected {
		prefix = "› "
		titleStyle = StylePickerAccent
	}
	title := strings.TrimSpace(row.Title)
	if title == "" {
		title = clipboardHistoryTitleFromText(row.Text)
	}
	cells := []Cell{styledCell(prefix, titleStyle)}
	cells = append(cells, clipboardHistoryColumnCells(title, row.TitleMatchIndexes, titleStyle, nameWidth-DisplayWidth(prefix))...)
	return cells
}

func clipboardHistorySearchLine(query string, nameWidth int) Line {
	value := query
	style := StylePickerAccent
	if value == "" {
		value = "Search:"
		style = StylePickerMuted
	} else {
		value = "Search: " + value
	}
	return clipboardHistoryPlainLine(value, style, nameWidth)
}

func clipboardHistoryDividerSpaceLine(nameWidth int) Line {
	return clipboardHistoryPlainLine("", StylePicker, nameWidth)
}

func clipboardHistoryColumnCells(value string, matchIndexes []int, baseStyle StyleToken, width int) []Cell {
	if width <= 0 {
		return nil
	}
	value = TruncateCells(value, width)
	cells := clipboardHistoryHighlightedCells(value, matchIndexes, baseStyle)
	if pad := width - DisplayWidth(value); pad > 0 {
		cells = append(cells, styledCell(strings.Repeat(" ", pad), baseStyle))
	}
	return cells
}

func clipboardHistoryHighlightedCells(value string, matchIndexes []int, baseStyle StyleToken) []Cell {
	if value == "" {
		return nil
	}
	if len(matchIndexes) == 0 {
		return []Cell{styledCell(value, baseStyle)}
	}
	matchSet := make(map[int]struct{}, len(matchIndexes))
	for _, index := range matchIndexes {
		matchSet[index] = struct{}{}
	}
	runes := []rune(value)
	cells := make([]Cell, 0, len(runes))
	for index, r := range runes {
		style := baseStyle
		if _, ok := matchSet[index]; ok {
			style = StylePickerMatch
		}
		cells = append(cells, styledCell(string(r), style))
	}
	return cells
}

func clipboardHistoryPlainLine(value string, style StyleToken, nameWidth int) Line {
	width := clipboardHistoryRowWidth(nameWidth)
	value = TruncateCells(value, width)
	cells := []Cell{styledCell(value, style)}
	if pad := width - DisplayWidth(value); pad > 0 {
		cells = append(cells, styledCell(strings.Repeat(" ", pad), StylePicker))
	}
	return Line{Cells: cells}
}

func clipboardHistoryTitleFromText(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if text == "" {
		return "clipboard"
	}
	return text
}

func clipboardHistorySelectedIndex(rows []state.ClipboardHistoryItem) int {
	for index, row := range rows {
		if row.Selected {
			return index
		}
	}
	if len(rows) > 0 {
		return 0
	}
	return -1
}

func clipboardHistorySelectedItem(rows []state.ClipboardHistoryItem) (state.ClipboardHistoryItem, bool) {
	index := clipboardHistorySelectedIndex(rows)
	if index < 0 || index >= len(rows) {
		return state.ClipboardHistoryItem{}, false
	}
	return rows[index], true
}

func clipboardHistoryBodyRowsForViewport(viewport state.ViewportStore) int {
	if !viewport.Valid || viewport.Rows <= 0 {
		return clipboardHistoryBodyRows
	}
	bodyRows := viewport.Rows - clipboardHistoryVerticalMargin(viewport.Rows) - 4
	return clampInt(bodyRows, 4, clipboardHistoryMaxBodyRows)
}

func clipboardHistoryListStart(selected int, total int, visibleRows int) int {
	if selected < 0 || total <= visibleRows {
		return 0
	}
	start := selected - visibleRows/2
	maxStart := maxInt(0, total-visibleRows)
	return clampInt(start, 0, maxStart)
}

func clipboardHistoryPreviewSegments(item state.ClipboardHistoryItem, ok bool, query string, width int, limit int) []clipboardHistoryPreviewSegment {
	if !ok || width <= 0 || limit <= 0 {
		return nil
	}
	text := clipboardHistoryPreviewText(item)
	if text == "" {
		return nil
	}
	var matches []int
	if strings.TrimSpace(query) != "" {
		matches = state.TerminalPickerQueryMatchIndexes(text, query)
	}
	return clipboardHistoryPreviewTextSegments(text, matches, width, limit)
}

func clipboardHistoryPreviewText(item state.ClipboardHistoryItem) string {
	text := item.Text
	if strings.TrimSpace(text) == "" {
		text = item.Preview
	}
	if strings.TrimSpace(text) == "" {
		text = item.Title
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

func clipboardHistoryPreviewTextSegments(text string, matchIndexes []int, width int, limit int) []clipboardHistoryPreviewSegment {
	segments := make([]clipboardHistoryPreviewSegment, 0, limit)
	runeStart := 0
	for _, rawLine := range strings.Split(text, "\n") {
		line := SafeLine(rawLine)
		if line == "" {
			segments = append(segments, clipboardHistoryPreviewSegment{})
			runeStart++
			if len(segments) >= limit {
				return segments
			}
			continue
		}
		for DisplayWidth(line) > width {
			chunk := SliceCells(line, 0, width)
			segments = append(segments, clipboardHistoryPreviewSegment{
				Text:       chunk,
				MatchIndex: clipboardHistoryShiftMatchIndexes(matchIndexes, runeStart, len([]rune(chunk))),
			})
			runeStart += len([]rune(chunk))
			line = SliceCells(line, width, DisplayWidth(line))
			if len(segments) >= limit {
				return segments
			}
		}
		segments = append(segments, clipboardHistoryPreviewSegment{
			Text:       line,
			MatchIndex: clipboardHistoryShiftMatchIndexes(matchIndexes, runeStart, len([]rune(line))),
		})
		runeStart += len([]rune(line)) + 1
		if len(segments) >= limit {
			return segments
		}
	}
	return segments
}

func clipboardHistoryShiftMatchIndexes(matchIndexes []int, start int, length int) []int {
	if len(matchIndexes) == 0 || length <= 0 {
		return nil
	}
	out := make([]int, 0, len(matchIndexes))
	for _, index := range matchIndexes {
		if index >= start && index < start+length {
			out = append(out, index-start)
		}
	}
	return out
}

func clipboardHistoryNameWidth(shell state.ShellStore) int {
	return state.ClipboardHistoryNameWidth(shell.EnsureDefaults().Overlay)
}

func clipboardHistoryContentNameWidth(content ContentVM) int {
	return state.ClipboardHistoryNameWidth(state.OverlayState{ClipboardNameWidth: content.Meta.ClipboardNameWidth})
}

func clipboardHistoryRowWidth(nameWidth int) int {
	return nameWidth + 1 + clipboardHistoryPreviewWidth
}

func clipboardHistorySearchCursorCol(query string) int {
	if query == "" {
		return DisplayWidth("Search:")
	}
	return DisplayWidth("Search: ") + DisplayWidth(query)
}

func clipboardHistoryHitRegions(rows []state.ClipboardHistoryItem, rowOffset int, listStart int, visibleRows int, nameWidth int) []HitRegion {
	if visibleRows <= 0 || listStart >= len(rows) {
		return nil
	}
	end := minInt(len(rows), listStart+visibleRows)
	regions := make([]HitRegion, 0, maxInt(0, end-listStart))
	for index := listStart; index < end; index++ {
		regions = append(regions, HitRegion{
			Kind:     HitRegionContentAction,
			Rect:     Rect{Y: rowOffset + index - listStart, W: nameWidth, H: 1},
			Row:      index,
			ActionID: ActionClipboardHistorySelect.String(),
		})
	}
	return regions
}

func clipboardHistoryDividerHitRegion(rowOffset int, visibleRows int, nameWidth int) HitRegion {
	return HitRegion{
		Kind:     HitRegionContentAction,
		Rect:     Rect{X: nameWidth, Y: rowOffset - 1, W: 1, H: visibleRows + 1},
		ActionID: ActionClipboardHistoryDividerDrag.String(),
	}
}

func clipboardHistoryStatus(count int, query string) string {
	if count == 0 && strings.TrimSpace(query) != "" {
		return "clipboard history: 0 filtered"
	}
	return fmt.Sprintf("clipboard history: %d", count)
}

// Prompt 是 reducer-owned 表单 overlay；提交只回投 shell message，不直接执行业务 IO。
func buildPromptContent(shell state.ShellStore) ContentVM {
	shell = shell.EnsureDefaults()
	prompt := shell.Overlay.Prompt
	title := prompt.Title
	if title == "" {
		title = "Command Prompt"
	}
	if len(prompt.Fields) > 0 {
		return buildPromptFormContent(prompt, title)
	}
	placeholder := prompt.Placeholder
	if placeholder == "" {
		placeholder = "command"
	}
	value := prompt.Value
	displayValue := value
	if displayValue == "" {
		displayValue = "[" + placeholder + "]"
	}
	lines := []Line{
		pageTitleLine(title, ""),
		formFieldLine("Name", displayValue, value != ""),
	}
	if prompt.Destructive {
		lines = append(lines, Line{Cells: []Cell{styledCell(" ! confirm ", StyleWarning), NewCell("type " + prompt.ConfirmText + " before submit")}})
	}
	return ContentVM{
		Kind:   ContentPrompt,
		Lines:  lines,
		Status: "prompt",
		Cursor: Cursor{Visible: true, Row: 1, Col: DisplayWidth("Name ") + DisplayWidth(value), Shape: CursorShapeBar},
	}
}

func buildPromptFormContent(prompt state.PromptState, title string) ContentVM {
	lines := []Line{pageTitleLine(title, "")}
	activeField := prompt.ActiveField
	if activeField < 0 {
		activeField = 0
	}
	if activeField >= len(prompt.Fields) {
		activeField = len(prompt.Fields) - 1
	}
	cursorRow := 1 + activeField
	cursorCol := 0
	for index, field := range prompt.Fields {
		active := index == activeField
		lines = append(lines, promptFormFieldLine(field, active))
		if active {
			cursorCol = promptFormFieldValueCol(field) + promptFieldCursorDisplayWidth(field)
		}
	}
	return ContentVM{
		Kind:   ContentPrompt,
		Lines:  lines,
		Status: "prompt",
		Cursor: Cursor{Visible: true, Row: cursorRow, Col: cursorCol, Shape: CursorShapeBar},
	}
}

func promptFormFieldLine(field state.PromptFieldState, active bool) Line {
	label := field.Label
	if label == "" {
		label = field.Key
	}
	if field.Required {
		label += "*"
	}
	value := field.Value
	valueSet := value != ""
	if value == "" && field.Placeholder != "" {
		value = "[" + field.Placeholder + "]"
	}
	labelStyle := StyleStrongForeground
	if active {
		labelStyle = StyleAccent
	}
	valueStyle := StyleForeground
	if !valueSet {
		valueStyle = StyleStrongForeground
	}
	return Line{Cells: []Cell{
		styledCell(label+": ", labelStyle),
		styledCell(value, valueStyle),
	}}
}

func promptFormFieldValueCol(field state.PromptFieldState) int {
	label := field.Label
	if label == "" {
		label = field.Key
	}
	if field.Required {
		label += "*"
	}
	return DisplayWidth(label + ": ")
}

func promptFieldCursorDisplayWidth(field state.PromptFieldState) int {
	runes := []rune(field.Value)
	cursor := field.Cursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}
	return DisplayWidth(string(runes[:cursor]))
}

func buildHelpContent(shell state.ShellStore) ContentVM {
	shell = shell.EnsureDefaults()
	lines := []Line{pageTitleLine("Help", "core workflows")}
	for _, group := range helpActionGroups() {
		if line, ok := helpActionGroupLine(group); ok {
			lines = append(lines, line)
		}
	}
	lines = append(lines, contentActionLine("close", "Close Help"))
	return ContentVM{
		Kind:   ContentHelp,
		Lines:  lines,
		Status: "help: core workflows",
		Cursor: Cursor{Visible: false},
		HitRegions: []HitRegion{{
			Kind:     HitRegionContentAction,
			Rect:     Rect{Y: len(lines) - 1, W: contentActionWidth, H: 1},
			ActionID: ActionHelpClose.String(),
		}},
	}
}

// Help 只展示当前已接线的 action 或明确存在的键盘入口，避免继续把产品愿景文案画成可用功能。
type helpActionGroup struct {
	Label   string
	Items   []helpActionItem
	Details []string
}

type helpActionItem struct {
	Action ActionID
}

func helpActionGroups() []helpActionGroup {
	return []helpActionGroup{
		// Help 是产品导航页；toast 清理这类维护动作不放到主说明里。
		{Label: "Most used", Details: []string{"Ctrl-p pane", "Ctrl-r resize", "Ctrl-f picker", "Ctrl-g global"}},
		{Label: "Shell", Items: []helpActionItem{
			{Action: ActionFooterToggleHeader},
			{Action: ActionFooterToggleFooter},
			{Action: ActionFooterGlobalMode},
		}},
		{Label: "Pane", Items: []helpActionItem{
			{Action: ActionPaneFooterFocus},
			{Action: ActionPaneFooterSplitRight},
			{Action: ActionPaneFooterSplitDown},
			{Action: ActionPaneFooterDetach},
			{Action: ActionPaneFooterZoom},
			{Action: ActionPaneFooterClose},
		}},
		{Label: "Tab / Workspace", Items: []helpActionItem{
			{Action: ActionTabCreate},
			{Action: ActionTabPrevious},
			{Action: ActionTabNext},
			{Action: ActionTabClose},
			{Action: ActionFooterOpenTree},
			{Action: ActionFooterNewWorkspace},
			{Action: ActionFooterRenameWorkspace},
		}},
		{Label: "Floating", Items: []helpActionItem{
			{Action: ActionFloatingNew},
			{Action: ActionFloatingOverview},
			{Action: ActionFloatingSummon},
			{Action: ActionFloatingPick},
			{Action: ActionFloatingClose},
		}},
		{Label: "Terminal Pool", Items: []helpActionItem{
			{Action: ActionPoolAttach},
			{Action: ActionPoolAttachTab},
			{Action: ActionPoolAttachFloat},
			{Action: ActionPoolEdit},
			{Action: ActionPoolKill},
			{Action: ActionPoolDelete},
		}, Details: []string{"search"}},
		{Label: "Workbench Tree", Items: []helpActionItem{
			{Action: ActionWorkbenchOpen},
			{Action: ActionWorkbenchRename},
			{Action: ActionWorkbenchNew},
			{Action: ActionWorkbenchDetach},
			{Action: ActionWorkbenchZoom},
		}},
		{Label: "Prompt / Help", Items: []helpActionItem{
			{Action: ActionPromptOpen},
			{Action: ActionPromptSubmit},
			{Action: ActionPromptCancel},
			{Action: ActionHelpOpen},
		}, Details: []string{"confirm"}},
		{Label: "Display / Copy", Items: []helpActionItem{
			{Action: ActionCopyOlder},
			{Action: ActionClipboardHistoryPaste},
			{Action: ActionClipboardHistoryNew},
			{Action: ActionClipboardHistoryEdit},
			{Action: ActionClipboardHistoryDelete},
		}, Details: []string{"authoritative HistoryWindow"}},
	}
}

func helpActionGroupLine(group helpActionGroup) (Line, bool) {
	labels := make([]string, 0, len(group.Items)+len(group.Details))
	for _, item := range group.Items {
		if label, ok := helpActionLabel(item.Action); ok {
			labels = append(labels, label)
		}
	}
	labels = append(labels, group.Details...)
	if len(labels) == 0 {
		return Line{}, false
	}
	return helpTopicLine(group.Label, strings.Join(labels, " / ")), true
}

func helpActionLabel(action ActionID) (string, bool) {
	spec, ok := ActionSpecByID(action)
	if !ok || spec.HelpLabel == "" {
		return "", false
	}
	return spec.HelpLabel, true
}

func terminalPickerLine(row state.TerminalPickerItem, query string) Line {
	marker := "  "
	textStyle := StylePicker
	markerStyle := StylePicker
	if row.Selected {
		marker = "▸ "
		markerStyle = StylePickerAccent
	}
	if row.CreateNew {
		cells := []Cell{
			styledCell(marker, markerStyle),
			styledCell("+", StylePickerInfo),
			pickerSpace(" "),
		}
		cells = append(cells, terminalPickerColumnCells(row.Title, query, textStyle, 24)...)
		cells = append(cells, pickerSpace("  "))
		cells = append(cells, terminalPickerColumnCells("new", query, StylePickerInfo, 10)...)
		cells = append(cells, pickerSpace("  "))
		cells = append(cells, terminalPickerColumnCells("-", query, StylePickerMuted, 8)...)
		cells = append(cells, pickerSpace("  "))
		cells = append(cells, highlightPickerText("Create terminal", query, textStyle)...)
		return Line{Cells: cells}
	}
	stateText := terminalPickerStateLabel(row)
	sizeText := terminalPickerSizeLabel(row)
	if sizeText == "" {
		sizeText = "-"
	}
	cells := []Cell{
		styledCell(marker, markerStyle),
		styledCell("●", terminalPoolStateStyle(stateText)),
		pickerSpace(" "),
	}
	cells = append(cells, terminalPickerColumnCells(row.Title, query, textStyle, 24)...)
	cells = append(cells, pickerSpace("  "))
	cells = append(cells, terminalPickerColumnCells(stateText, query, terminalPoolStateStyle(stateText), 10)...)
	cells = append(cells, pickerSpace("  "))
	cells = append(cells, terminalPickerColumnCells(sizeText, query, textStyle, 8)...)
	cells = append(cells, pickerSpace("  "))
	cells = append(cells, styledCell("Attach here", StylePickerMuted))
	return Line{Cells: cells}
}

func terminalPickerColumnCells(value string, query string, baseStyle StyleToken, width int) []Cell {
	cells := highlightPickerText(value, query, baseStyle)
	pad := width - DisplayWidth(value)
	if pad > 0 {
		cells = append(cells, pickerSpace(strings.Repeat(" ", pad)))
	}
	return cells
}

func terminalPickerStateLabel(row state.TerminalPickerItem) string {
	if strings.TrimSpace(row.PoolState) != "" {
		return row.PoolState
	}
	switch row.Kind {
	case state.PaneTerminalLive:
		return "live"
	case state.PaneEmpty:
		return "empty"
	default:
		return string(row.Kind)
	}
}

func terminalPickerSizeLabel(row state.TerminalPickerItem) string {
	if row.Cols <= 0 || row.Rows <= 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d", row.Cols, row.Rows)
}

func terminalPickerSearchLine(query string) Line {
	if query == "" {
		return Line{Cells: []Cell{
			styledCell("search:", StylePickerMuted),
		}}
	}
	return Line{Cells: []Cell{
		styledCell("search: ", StylePickerMuted),
		styledCell(query, StylePickerAccent),
	}}
}

func highlightPickerText(value string, query string, baseStyle StyleToken) []Cell {
	if value == "" {
		return nil
	}
	matchIndexes := state.TerminalPickerQueryMatchIndexes(value, query)
	if matchIndexes == nil || len(matchIndexes) == 0 {
		return []Cell{styledCell(value, baseStyle)}
	}
	matchSet := make(map[int]struct{}, len(matchIndexes))
	for _, index := range matchIndexes {
		matchSet[index] = struct{}{}
	}
	runes := []rune(value)
	cells := make([]Cell, 0, len(runes))
	for index, r := range runes {
		style := baseStyle
		if _, ok := matchSet[index]; ok {
			style = StylePickerMatch
		}
		cells = append(cells, styledCell(string(r), style))
	}
	return cells
}

func pickerSpace(value string) Cell {
	return styledCell(value, StylePicker)
}

func terminalPickerSearchCursorCol(query string) int {
	if query == "" {
		return DisplayWidth("search:")
	}
	return DisplayWidth("search: ") + DisplayWidth(query)
}

func terminalPoolPageRowLine(row state.TerminalPoolPageItem) Line {
	marker := "  "
	style := StyleMuted
	if row.Selected {
		marker = "▌ "
		style = StyleAccent
	}
	stateText := row.State
	if stateText == "" {
		stateText = "unknown"
	}
	attached := "parked"
	if row.Attached {
		attached = "attached"
	}
	return Line{Cells: []Cell{
		styledCell(marker, style),
		styledCell(row.Title, style),
		NewCell(" "),
		tokenCell(stateText, terminalPoolStateStyle(stateText)),
		NewCell(" "),
		tokenCell(attached, StyleMuted),
		NewCell(" "),
		tokenCell(terminalPoolSizeLabel(row.Cols, row.Rows), StyleMuted),
		NewCell(" "),
		styledCell(row.TerminalID, StyleMuted),
	}}
}

func workbenchTreeRowLine(row state.WorkbenchTreeItem) Line {
	marker := "  "
	markerStyle := StyleMuted
	prefixStyle := StyleMuted
	if row.Selected {
		marker = "▸ "
		markerStyle = StyleAccent
		prefixStyle = StyleForeground
	}
	status := row.Summary
	if row.Active {
		status = strings.TrimSpace(status + " active")
	}
	title := workbenchTreeTitle(row)
	prefix := strings.Repeat("│ ", row.Depth)
	if row.Depth > 0 {
		prefix = strings.Repeat("│ ", row.Depth-1) + "├─"
	}
	cells := []Cell{
		styledCell(marker, markerStyle),
		styledCell(prefix, prefixStyle),
		tokenCell(workbenchTreeKindGlyph(row), workbenchTreeKindStyle(row)),
		NewCell(" "),
		styledCell(title, workbenchTreeTitleStyle(row)),
		NewCell(" "),
	}
	cells = append(cells, workbenchTreeStatusCells(row, status)...)
	return Line{Cells: cells}
}

func workbenchNavigatorLines(root state.Root, rows []state.WorkbenchTreeItem, query string, layout workbenchNavigatorLayout) []Line {
	selected, selectedOK := selectedWorkbenchTreeItem(rows)
	right := workbenchNavigatorRightLines(root, selected, layout)
	lines := []Line{
		workbenchNavigatorFullLine(searchRowLine(query, "main"), layout),
		workbenchNavigatorBodyLine(workbenchNavigatorHeaderLine("TREE"), workbenchNavigatorHeaderLine(workbenchNavigatorRightHeader(selected, selectedOK)), layout),
	}
	for row := 0; row < layout.BodyRows; row++ {
		left := Line{}
		if row < len(rows) {
			left = workbenchTreeRowLine(rows[row])
		}
		rightLine := Line{}
		if row < len(right) {
			rightLine = right[row]
		}
		lines = append(lines, workbenchNavigatorBodyLine(left, rightLine, layout))
	}
	lines = append(lines, workbenchNavigatorBodyLine(Line{}, workbenchNavigatorActionLine(), layout))
	return lines
}

func workbenchNavigatorFullLine(line Line, layout workbenchNavigatorLayout) Line {
	return fitContentLine(line, layout.ContentWidth, StyleForeground)
}

func workbenchNavigatorBodyLine(left Line, right Line, layout workbenchNavigatorLayout) Line {
	cells := fitContentLine(left, layout.TreeWidth, StyleForeground).Cells
	cells = append(cells, styledCell("│", StyleForeground))
	cells = append(cells, fitContentLine(right, layout.RightWidth, StyleForeground).Cells...)
	return Line{Cells: cells}
}

func workbenchNavigatorHeaderLine(label string) Line {
	return Line{Cells: []Cell{styledCell(label, StyleStrongForeground)}}
}

func workbenchNavigatorActionLine() Line {
	return Line{Cells: []Cell{
		styledCell("Open", StyleAccent),
		NewCell("  "),
		styledCell("Zoom", StyleAccent),
		NewCell("  "),
		styledCell("Detach", StyleAccent),
		NewCell("  "),
		styledCell("Close", StyleAccent),
	}}
}

func selectedWorkbenchTreeItem(rows []state.WorkbenchTreeItem) (state.WorkbenchTreeItem, bool) {
	if len(rows) == 0 {
		return state.WorkbenchTreeItem{}, false
	}
	selected := rows[0]
	for _, row := range rows {
		if row.Selected {
			return row, true
		}
	}
	return selected, true
}

func workbenchNavigatorRightHeader(selected state.WorkbenchTreeItem, ok bool) string {
	if !ok {
		return "PANE"
	}
	switch selected.Kind {
	case state.WorkbenchTreeKindWorkspace:
		return "WORKSPACE"
	case state.WorkbenchTreeKindTab:
		return "TAB"
	case state.WorkbenchTreeKindPane:
		return "PANE"
	case state.WorkbenchTreeKindFloating:
		return "FLOATING"
	default:
		return "PANE"
	}
}

func workbenchNavigatorRightLines(root state.Root, selected state.WorkbenchTreeItem, layout workbenchNavigatorLayout) []Line {
	if selected.Kind == "" {
		return []Line{NewLine("No workbench node selected")}
	}
	title := workbenchTreeTitle(selected)
	switch selected.Kind {
	case state.WorkbenchTreeKindPane:
		return workbenchNavigatorPaneLines(root, selected, layout)
	case state.WorkbenchTreeKindTab:
		lines := []Line{
			{Cells: []Cell{styledCell(title, StyleForeground)}},
			workbenchNavigatorTokenLine([]string{selected.WorkspaceName, "tab:" + selected.TabTitle, selected.Summary}),
			{Cells: []Cell{styledCell("PANES", StyleStrongForeground)}},
		}
		lines = append(lines, workbenchNavigatorPaneSnapshotLines(layout)...)
		return lines
	case state.WorkbenchTreeKindWorkspace:
		return []Line{
			{Cells: []Cell{styledCell(title, StyleForeground)}},
			workbenchNavigatorTokenLine([]string{"current", selected.Summary}),
			{Cells: []Cell{styledCell("SUMMARY", StyleStrongForeground)}},
			{Cells: []Cell{styledCell(workbenchTreePreview(selected), StyleMuted)}},
		}
	case state.WorkbenchTreeKindFloating:
		lines := []Line{
			{Cells: []Cell{styledCell(title, StyleForeground)}},
			workbenchNavigatorTokenLine([]string{selected.Summary}),
			{Cells: []Cell{styledCell("SNAPSHOT", StyleStrongForeground)}},
		}
		if selected.FloatingID == "" {
			lines = append(lines, Line{Cells: []Cell{styledCell(workbenchTreePreview(selected), StyleMuted)}})
			return lines
		}
		lines = append(lines, workbenchNavigatorPaneSnapshotLines(layout)...)
		return lines
	default:
		return []Line{{Cells: []Cell{styledCell(workbenchTreePreview(selected), StyleMuted)}}}
	}
}

func workbenchNavigatorPaneLines(root state.Root, selected state.WorkbenchTreeItem, layout workbenchNavigatorLayout) []Line {
	lines := []Line{
		{Cells: []Cell{styledCell(workbenchTreeTitle(selected), StyleForeground)}},
		workbenchNavigatorTokenLine([]string{selected.WorkspaceName, "tab:" + selected.TabTitle, workbenchPaneStateLabel(root, selected), workbenchPaneRoleLabel(root, selected)}),
		{Cells: []Cell{styledCell("SNAPSHOT", StyleStrongForeground)}},
	}
	lines = append(lines, workbenchNavigatorPaneSnapshotLines(layout)...)
	return lines
}

func workbenchNavigatorPaneSnapshotLines(layout workbenchNavigatorLayout) []Line {
	lines := make([]Line, 0, layout.SnapshotHeight)
	for row := 0; row < layout.SnapshotHeight; row++ {
		lines = append(lines, NewLine(strings.Repeat(" ", layout.SnapshotWidth)))
	}
	return lines
}

func workbenchNavigatorLayoutForViewport(viewport state.ViewportStore) workbenchNavigatorLayout {
	cols := 100
	rows := 30
	if viewport.Valid && viewport.Cols > 0 {
		cols = viewport.Cols
	}
	if viewport.Valid && viewport.Rows > 0 {
		rows = viewport.Rows
	}
	overlay := measureWorkbenchNavigatorOverlay(Rect{W: cols, H: rows})
	content := measureOverlayContentRect(OverlayVM{Content: ContentVM{Kind: ContentWorkbenchTree}}, overlay)
	contentWidth := maxInt(40, content.W)
	contentHeight := maxInt(10, content.H)
	treeWidth := clampInt(contentWidth*34/100, defaultWorkbenchNavigatorTreeWidth, 64)
	if contentWidth < 96 {
		treeWidth = clampInt(contentWidth*40/100, 28, 40)
	}
	rightWidth := maxInt(20, contentWidth-treeWidth-1)
	if rightWidth < 36 && contentWidth > 44 {
		treeWidth = maxInt(24, contentWidth-37)
		rightWidth = maxInt(20, contentWidth-treeWidth-1)
	}
	bodyRows := maxInt(6, contentHeight-3)
	actionRow := bodyRows + 2
	// 中文说明：snapshot 坐标以 overlay content rect 为原点，和最终 runtime 叠加坐标保持一致。
	snapshotWidth := maxInt(0, rightWidth-2)
	snapshotHeight := clampInt(bodyRows-3, 4, bodyRows)
	return workbenchNavigatorLayout{
		ContentWidth:   contentWidth,
		BodyRows:       bodyRows,
		TreeWidth:      treeWidth,
		RightWidth:     rightWidth,
		ActionRow:      actionRow,
		SnapshotX:      treeWidth + 2,
		SnapshotY:      5,
		SnapshotWidth:  snapshotWidth,
		SnapshotHeight: snapshotHeight,
	}
}

func workbenchNavigatorMeta(root state.Root, rows []state.WorkbenchTreeItem, layout workbenchNavigatorLayout) ContentMetaVM {
	meta := ContentMetaVM{
		WorkbenchTreeWidth: layout.TreeWidth,
		WorkbenchBodyRows:  layout.BodyRows,
		WorkbenchActionRow: layout.ActionRow,
	}
	selected, ok := selectedWorkbenchTreeItem(rows)
	if !ok {
		return meta
	}
	meta.WorkbenchSnapshots = workbenchNavigatorSnapshotVMs(root, selected, layout)
	if len(meta.WorkbenchSnapshots) > 0 {
		first := meta.WorkbenchSnapshots[0]
		meta.WorkbenchSnapshotPanel = &first.Panel
		meta.WorkbenchSnapshotRect = first.Rect
		meta.WorkbenchSnapshotContent = first.Content
	}
	return meta
}

type workbenchNavigatorPreviewPane struct {
	Pane         state.PaneState
	Floating     state.FloatingPaneState
	Active       bool
	FloatingMode bool
}

func workbenchNavigatorSnapshotVMs(root state.Root, selected state.WorkbenchTreeItem, layout workbenchNavigatorLayout) []WorkbenchSnapshotVM {
	panes := workbenchNavigatorPreviewPanes(root, selected)
	if len(panes) == 0 {
		return nil
	}
	rects := workbenchNavigatorSnapshotRects(layout, len(panes))
	out := make([]WorkbenchSnapshotVM, 0, minInt(len(panes), len(rects)))
	for index, preview := range panes {
		if index >= len(rects) {
			break
		}
		panel := workbenchNavigatorSnapshotPanelForPreview(root, preview)
		rect := rects[index]
		content := Rect{X: rect.X + 1, Y: rect.Y + 1, W: maxInt(0, rect.W-2), H: maxInt(0, rect.H-2)}
		out = append(out, WorkbenchSnapshotVM{Panel: panel, Rect: rect, Content: content})
	}
	return out
}

func workbenchNavigatorPreviewPanes(root state.Root, selected state.WorkbenchTreeItem) []workbenchNavigatorPreviewPane {
	shell := root.Shell.EnsureDefaults()
	switch selected.Kind {
	case state.WorkbenchTreeKindPane:
		pane, ok := shell.Pane(state.PaneCommandTarget{PaneID: selected.PaneID})
		if !ok {
			return nil
		}
		return []workbenchNavigatorPreviewPane{{Pane: pane, Active: selected.Active}}
	case state.WorkbenchTreeKindTab:
		tab, ok := workbenchNavigatorTab(shell.Workspace, selected.TabID)
		if !ok {
			return nil
		}
		out := make([]workbenchNavigatorPreviewPane, 0, len(tab.Panes))
		for _, pane := range tab.Panes {
			out = append(out, workbenchNavigatorPreviewPane{Pane: pane, Active: selected.Active && pane.ID == tab.ActivePaneID})
		}
		return out
	case state.WorkbenchTreeKindFloating:
		if selected.FloatingID == "" {
			return nil
		}
		floating, ok := workbenchNavigatorFloating(shell, selected.FloatingID)
		if !ok {
			return nil
		}
		return []workbenchNavigatorPreviewPane{{Pane: floating.Pane, Floating: floating, Active: floating.Active, FloatingMode: true}}
	default:
		return nil
	}
}

func workbenchNavigatorSnapshotRects(layout workbenchNavigatorLayout, count int) []Rect {
	if count <= 0 || layout.SnapshotWidth <= 0 || layout.SnapshotHeight <= 0 {
		return nil
	}
	if count == 1 {
		return []Rect{{X: layout.SnapshotX, Y: layout.SnapshotY, W: layout.SnapshotWidth, H: layout.SnapshotHeight}}
	}
	gap := 1
	usable := layout.SnapshotHeight - gap*(count-1)
	if usable < count*3 {
		gap = 0
		usable = layout.SnapshotHeight
	}
	base := maxInt(1, usable/count)
	remainder := usable % count
	rects := make([]Rect, 0, count)
	y := layout.SnapshotY
	for index := 0; index < count; index++ {
		height := base
		if index < remainder {
			height++
		}
		remaining := layout.SnapshotY + layout.SnapshotHeight - y
		if height > remaining {
			height = remaining
		}
		if height <= 0 {
			break
		}
		rects = append(rects, Rect{X: layout.SnapshotX, Y: y, W: layout.SnapshotWidth, H: height})
		y += height + gap
	}
	return rects
}

func workbenchNavigatorSnapshotPanelForPreview(root state.Root, preview workbenchNavigatorPreviewPane) PanelVM {
	content := workbenchNavigatorPreviewContent(root, preview)
	pane := preview.Pane
	title := activePaneTitle(pane)
	if preview.FloatingMode && strings.TrimSpace(preview.Floating.Title) != "" {
		title = preview.Floating.Title
	}
	chrome := buildPanelChromeVM(root, pane, preview.Active, content)
	if preview.FloatingMode {
		chrome = PanelChromeVM{
			Title:    ChromeSlotVM{Text: title, Style: workbenchPreviewChromeStyle(preview.Active)},
			State:    paneChromeStateSlot(preview.Active, content),
			Terminal: terminalChromeVMForFloating(root, preview.Floating, content, workbenchPreviewChromeStyle(preview.Active)),
			Actions:  defaultPaneChromeActionVMs(workbenchPreviewChromeStyle(preview.Active)),
		}
	}
	return PanelVM{
		ID:           pane.ID,
		Title:        title,
		Presentation: PanelPresentationCard,
		Active:       preview.Active,
		Content:      content,
		Chrome:       chrome,
	}
}

func workbenchNavigatorPreviewContent(root state.Root, preview workbenchNavigatorPreviewPane) ContentVM {
	pane := preview.Pane
	switch pane.Kind {
	case state.PaneEmpty:
		return buildEmptyPaneContent(pane)
	case state.PaneTerminalLive:
		var surface state.TerminalSurfaceStore
		var session state.TerminalSessionStore
		if preview.FloatingMode {
			surface = surfaceForFloating(root, preview.Floating.ID)
			session = sessionForFloating(root, preview.Floating.ID)
		} else {
			surface, session = terminalContentStoresForPane(root, pane)
		}
		content := buildLiveContentVM(surface, session)
		if preview.FloatingMode {
			return contentWithFloatingLayout(root, preview.Floating, content)
		}
		return contentWithPaneLayout(root, pane, content)
	default:
		return placeholderContentForPane(pane)
	}
}

func workbenchPreviewChromeStyle(active bool) StyleToken {
	if active {
		return StyleAccent
	}
	return StyleMuted
}

func workbenchNavigatorTab(workspace state.WorkspaceState, tabID string) (state.TabState, bool) {
	for _, tab := range workspace.Tabs {
		if tab.ID == tabID {
			return tab, true
		}
	}
	return state.TabState{}, false
}

func workbenchNavigatorFloating(shell state.ShellStore, floatingID string) (state.FloatingPaneState, bool) {
	for _, floating := range shell.Floatings {
		if floating.ID == floatingID {
			return floating, true
		}
	}
	return state.FloatingPaneState{}, false
}

func workbenchNavigatorTokenLine(tokens []string) Line {
	cells := []Cell{}
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if len(cells) > 0 {
			cells = append(cells, NewCell("  "))
		}
		cells = append(cells, styledCell(token, StyleMuted))
	}
	if len(cells) == 0 {
		return NewLine("")
	}
	return Line{Cells: cells}
}

func floatingOverviewRowLine(index int, row state.FloatingOverviewItem) Line {
	marker := "  "
	style := StyleMuted
	if row.Selected {
		marker = "▌ "
		style = StyleAccent
	}
	active := "parked"
	if row.Active {
		active = "active"
	}
	collapsed := "open"
	if row.Collapsed {
		collapsed = "collapsed"
	}
	fitMode := "manual"
	if row.FitMode == state.FloatingFitAuto {
		fitMode = "auto-fit"
	}
	terminalID := row.TerminalID
	if terminalID == "" {
		terminalID = "unbound"
	}
	return Line{Cells: []Cell{
		styledCell(marker, style),
		tokenCell(fmt.Sprintf("%d", index+1), StyleStatusAccent),
		NewCell(" "),
		styledCell(row.Title, style),
		NewCell(" "),
		tokenCell(active, style),
		NewCell(" "),
		tokenCell(collapsed, StyleMuted),
		NewCell(" "),
		tokenCell(fitMode, StyleMuted),
		NewCell(" "),
		styledCell(terminalID, StyleMuted),
		NewCell(" "),
		styledCell(floatingOverviewRectLabel(row.Rect), StyleMuted),
	}}
}

func terminalPoolDetailLines(rows []state.TerminalPoolPageItem) []Line {
	if len(rows) == 0 {
		return []Line{
			{Cells: []Cell{styledCell("detail ", StyleMuted), NewCell("no terminal selected")}},
			{Cells: []Cell{styledCell("preview ", StyleMuted), NewCell("waiting for terminal list")}},
		}
	}
	selected := rows[0]
	for _, row := range rows {
		if row.Selected {
			selected = row
			break
		}
	}
	stateText := selected.State
	if stateText == "" {
		stateText = "unknown"
	}
	cwd := selected.CWD
	if cwd == "" {
		cwd = "-"
	}
	return []Line{
		detailHeaderLine("detail", selected.Title),
		detailTokenLine([]string{"id " + selected.TerminalID, "state " + stateText, terminalPoolSizeLabel(selected.Cols, selected.Rows)}),
		detailHeaderLine("cwd", cwd),
		detailHeaderLine("metadata", terminalPoolTagsLabel(selected.Tags)),
		detailHeaderLine("preview", terminalPoolPreviewLabel(selected)),
	}
}

func workbenchTreeDetailLines(rows []state.WorkbenchTreeItem) []Line {
	if len(rows) == 0 {
		return []Line{
			{Cells: []Cell{styledCell("detail ", StyleMuted), NewCell("no workbench node selected")}},
			{Cells: []Cell{styledCell("preview ", StyleMuted), NewCell("type to search workspace, tab, pane or floating")}},
		}
	}
	selected := rows[0]
	for _, row := range rows {
		if row.Selected {
			selected = row
			break
		}
	}
	return []Line{
		detailHeaderLine("detail", workbenchTreeTitle(selected)),
		detailTokenLine([]string{"kind " + selected.Kind, "path " + workbenchTreePath(selected)}),
		detailHeaderLine("target", workbenchTreeTarget(selected)),
		detailHeaderLine("preview", workbenchTreePreview(selected)),
	}
}

func terminalPoolPageHitRegions(rows []state.TerminalPoolPageItem, rowOffset int) []HitRegion {
	regions := make([]HitRegion, 0, len(rows)+3)
	for index := range rows {
		regions = append(regions, HitRegion{
			Kind:     HitRegionContentAction,
			Rect:     Rect{Y: rowOffset + index, W: 72, H: 1},
			Row:      index,
			ActionID: ActionPoolSelect.String(),
		})
	}
	return regions
}

func workbenchTreeHitRegions(rows []state.WorkbenchTreeItem, rowOffset int, treeWidth int) []HitRegion {
	regions := make([]HitRegion, 0, len(rows)+1)
	for index, row := range rows {
		regions = append(regions, HitRegion{
			Kind:     HitRegionContentAction,
			Rect:     Rect{Y: rowOffset + index, W: treeWidth, H: 1},
			Row:      index,
			PaneID:   row.PaneID,
			ActionID: ActionWorkbenchSelect.String(),
		})
	}
	return regions
}

func workbenchNavigatorActionHitRegions(layout workbenchNavigatorLayout) []HitRegion {
	x := layout.TreeWidth + 1
	y := layout.ActionRow
	actions := []struct {
		action ActionID
		label  string
	}{
		{ActionWorkbenchOpen, "Open"},
		{ActionWorkbenchZoom, "Zoom"},
		{ActionWorkbenchDetach, "Detach"},
		{ActionWorkbenchDelete, "Close"},
	}
	regions := make([]HitRegion, 0, len(actions))
	for _, item := range actions {
		width := DisplayWidth(item.label)
		regions = append(regions, HitRegion{
			Kind:     HitRegionContentAction,
			Rect:     Rect{X: x, Y: y, W: width, H: 1},
			Row:      -1,
			ActionID: item.action.String(),
		})
		x += width + 2
	}
	return regions
}

func floatingOverviewHitRegions(rows []state.FloatingOverviewItem, rowOffset int) []HitRegion {
	regions := make([]HitRegion, 0, len(rows)+1)
	for index, row := range rows {
		regions = append(regions, HitRegion{
			Kind:     HitRegionContentAction,
			Rect:     Rect{Y: rowOffset + index, W: 72, H: 1},
			Row:      index,
			PaneID:   row.FloatingID,
			ActionID: ActionFloatingSummon.String(),
		})
	}
	return regions
}

func terminalPickerHitRegions(rows []state.TerminalPickerItem, rowOffset int) []HitRegion {
	regions := make([]HitRegion, 0, len(rows)+1)
	for index, row := range rows {
		actionID := ActionPickerAttach.String()
		if row.CreateNew {
			actionID = ActionPickerNew.String()
		}
		regions = append(regions, HitRegion{
			Kind:     HitRegionContentAction,
			Rect:     Rect{Y: rowOffset + index, W: 72, H: 1},
			PaneID:   row.PaneID,
			Row:      index,
			ActionID: actionID,
		})
	}
	return regions
}

func workbenchTreeKindLabel(row state.WorkbenchTreeItem) string {
	switch row.Kind {
	case state.WorkbenchTreeKindWorkspace:
		return "workspace"
	case state.WorkbenchTreeKindTab:
		return "tab"
	case state.WorkbenchTreeKindPane:
		return "pane"
	case state.WorkbenchTreeKindFloating:
		return "floating"
	default:
		return row.Kind
	}
}

func workbenchTreeKindGlyph(row state.WorkbenchTreeItem) string {
	switch row.Kind {
	case state.WorkbenchTreeKindWorkspace:
		return "󰙅"
	case state.WorkbenchTreeKindTab:
		return "󰓩"
	case state.WorkbenchTreeKindPane:
		return ""
	case state.WorkbenchTreeKindFloating:
		return "󰐕"
	default:
		return workbenchTreeKindLabel(row)
	}
}

func workbenchTreeKindStyle(row state.WorkbenchTreeItem) StyleToken {
	if row.Selected {
		return StyleAccent
	}
	switch row.Kind {
	case state.WorkbenchTreeKindWorkspace:
		return StyleStatusAccent
	case state.WorkbenchTreeKindTab:
		if row.Active {
			return StyleAccent
		}
		return StyleStrongForeground
	case state.WorkbenchTreeKindPane:
		switch row.PaneKind {
		case state.PaneEmpty:
			return StyleWarning
		case state.PaneTerminalLive:
			return StyleSuccess
		default:
			return StyleForeground
		}
	case state.WorkbenchTreeKindFloating:
		if row.Active {
			return StyleAccent
		}
		return StyleWarning
	default:
		return StyleForeground
	}
}

func workbenchTreeTitleStyle(row state.WorkbenchTreeItem) StyleToken {
	if row.Selected {
		return StyleAccent
	}
	if row.Active {
		return StyleStrongForeground
	}
	if row.Kind == state.WorkbenchTreeKindFloating {
		return StyleForeground
	}
	return StyleForeground
}

func workbenchTreeStatusCells(row state.WorkbenchTreeItem, summary string) []Cell {
	tags := []string{}
	if summary != "" {
		tags = append(tags, strings.Fields(summary)...)
	}
	if row.Kind == state.WorkbenchTreeKindPane {
		if row.TerminalID != "" && !workbenchTreeHasTagPrefix(tags, "term:") {
			tags = append(tags, "term:"+row.TerminalID)
		}
	}
	if len(tags) == 0 {
		return nil
	}
	cells := []Cell{NewCell("[")}
	for index, tag := range tags {
		if index > 0 {
			cells = append(cells, NewCell("] ["))
		}
		cells = append(cells, styledCell(tag, workbenchTreeStatusStyle(row, tag)))
	}
	cells = append(cells, NewCell("]"))
	return cells
}

func workbenchTreeHasTagPrefix(tags []string, prefix string) bool {
	for _, tag := range tags {
		if strings.HasPrefix(strings.ToLower(tag), prefix) {
			return true
		}
	}
	return false
}

func workbenchTreeStatusStyle(row state.WorkbenchTreeItem, tag string) StyleToken {
	token := strings.ToLower(strings.Trim(tag, "[]"))
	switch {
	case token == "active":
		return StyleAccent
	case token == "running" || token == "attached" || token == "open" || token == string(state.PaneTerminalLive):
		return StyleSuccess
	case token == "exited" || token == "collapsed" || token == "pending":
		return StyleWarning
	case token == "error":
		return StyleDanger
	case token == "owner" || token == string(state.TerminalResizeRoleOwner):
		return StyleSuccess
	case token == "follower" || token == string(state.TerminalResizeRoleFollower) || token == "manual" || token == "auto-fit":
		return StyleStatusAccent
	case strings.HasPrefix(token, "term:") || strings.HasPrefix(token, "tabs:") || strings.HasPrefix(token, "panes:") || strings.HasPrefix(token, "active:") || strings.HasPrefix(token, "float:"):
		if row.Selected {
			return StyleForeground
		}
		return StyleMuted
	default:
		if row.Selected {
			return StyleForeground
		}
		return StyleMuted
	}
}

func workbenchTreeTitle(row state.WorkbenchTreeItem) string {
	switch row.Kind {
	case state.WorkbenchTreeKindWorkspace:
		if row.WorkspaceName != "" {
			return row.WorkspaceName
		}
		return row.WorkspaceID
	case state.WorkbenchTreeKindTab:
		if row.TabTitle != "" {
			return row.TabTitle
		}
		return row.TabID
	case state.WorkbenchTreeKindPane:
		if row.PaneTitle != "" {
			return row.PaneTitle
		}
		return row.PaneID
	case state.WorkbenchTreeKindFloating:
		if row.FloatingTitle != "" {
			return row.FloatingTitle
		}
		if row.PaneTitle != "" {
			return row.PaneTitle
		}
		if row.FloatingID != "" {
			return row.FloatingID
		}
		return "floating panes"
	default:
		return "node"
	}
}

func workbenchTreePath(row state.WorkbenchTreeItem) string {
	parts := []string{}
	if row.WorkspaceName != "" {
		parts = append(parts, "ws:"+row.WorkspaceName)
	}
	if row.TabTitle != "" {
		parts = append(parts, "tab:"+row.TabTitle)
	}
	if row.FloatingTitle != "" {
		parts = append(parts, "floating:"+row.FloatingTitle)
	}
	if row.PaneTitle != "" {
		parts = append(parts, "pane:"+row.PaneTitle)
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " / ")
}

func workbenchTreeTarget(row state.WorkbenchTreeItem) string {
	switch row.Kind {
	case state.WorkbenchTreeKindPane:
		terminalID := row.TerminalID
		if terminalID == "" {
			terminalID = "none"
		}
		return "pane:" + row.PaneID + " term:" + terminalID
	case state.WorkbenchTreeKindTab:
		return "tab:" + row.TabID + " active-pane:" + row.PaneID
	case state.WorkbenchTreeKindWorkspace:
		return "workspace:" + row.WorkspaceID
	case state.WorkbenchTreeKindFloating:
		if row.FloatingID != "" {
			return "floating:" + row.FloatingID
		}
		return row.Summary
	default:
		return "-"
	}
}

func workbenchTreePreview(row state.WorkbenchTreeItem) string {
	if row.Kind == state.WorkbenchTreeKindFloating {
		if row.FloatingID != "" {
			return row.Summary
		}
		return "no floating panes"
	}
	if row.Summary != "" {
		return row.Summary
	}
	return "ready"
}

func workbenchPaneStateLabel(root state.Root, selected state.WorkbenchTreeItem) string {
	if selected.PaneKind == state.PaneEmpty {
		return "empty"
	}
	binding, hasBinding := root.TerminalViews.PaneBinding(selected.PaneID)
	surface := state.TerminalSurfaceStore{}
	session := state.TerminalSessionStore{}
	if hasBinding && binding.TerminalID != "" {
		surface = root.Surface.SurfaceForTerminal(binding.TerminalID)
		session = sessionForBinding(root, binding)
	}
	switch {
	case session.LastError != "" || surface.Err != "":
		return "error"
	case session.State == state.TerminalLiveExited || surface.State == state.TerminalLiveExited:
		return "exited"
	case hasBinding && binding.Attached:
		return "running"
	case hasBinding && binding.TerminalID != "":
		return "bound"
	case selected.TerminalID != "":
		return "bound"
	default:
		return string(selected.PaneKind)
	}
}

func workbenchPaneRoleLabel(root state.Root, selected state.WorkbenchTreeItem) string {
	binding, ok := root.TerminalViews.PaneBinding(selected.PaneID)
	if !ok || binding.ResizeRole == "" {
		return ""
	}
	return binding.ResizeRole
}

func fitContentLine(line Line, width int, fill StyleToken) Line {
	if width <= 0 {
		return Line{}
	}
	if line.Width() > width {
		return contentViewportFitLine(line, width)
	}
	cells := append([]Cell(nil), line.Cells...)
	if pad := width - line.Width(); pad > 0 {
		cells = append(cells, styledCell(strings.Repeat(" ", pad), fill))
	}
	return Line{Cells: cells}
}

func terminalPoolPageStateLine(pool state.TerminalPoolStore, rowCount int) (Line, bool) {
	switch pool.Status {
	case state.TerminalPoolLoading:
		return Line{Cells: []Cell{styledCell("list ", StyleMuted), NewCell("loading terminals")}}, true
	case state.TerminalPoolError:
		return Line{Cells: []Cell{styledCell("list error ", StyleWarning), NewCell(pool.LastError)}}, true
	case state.TerminalPoolReady:
		if rowCount == 0 {
			return Line{Cells: []Cell{styledCell("list ", StyleMuted), NewCell("empty")}}, true
		}
	}
	return Line{}, false
}

func selectedTerminalPickerItem(rows []state.TerminalPickerItem) state.TerminalPickerItem {
	selected := rows[0]
	for _, row := range rows {
		if row.Selected {
			return row
		}
	}
	return selected
}

func terminalPoolPageStatus(pool state.TerminalPoolStore, count int, query string) string {
	prefix := "terminal pool"
	switch pool.Status {
	case state.TerminalPoolLoading:
		prefix += ": loading"
	case state.TerminalPoolError:
		prefix += ": error"
	default:
		prefix += fmt.Sprintf(": %d items", count)
	}
	if query != "" {
		prefix += " query:" + query
	}
	return prefix
}

func workbenchTreeStatus(count int, query string) string {
	if query == "" {
		return fmt.Sprintf("workbench tree: %d items", count)
	}
	return fmt.Sprintf("workbench tree: %d items query:%s", count, query)
}

func floatingOverviewStatus(count int) string {
	return fmt.Sprintf("floating overview: %d items", count)
}

func floatingOverviewRectLabel(rect state.FloatingRect) string {
	return fmt.Sprintf("%dx%d@%d,%d", rect.W, rect.H, rect.X, rect.Y)
}

func terminalPoolSizeLabel(cols int, rows int) string {
	if cols <= 0 || rows <= 0 {
		return "size:-"
	}
	return fmt.Sprintf("%dx%d", cols, rows)
}

func terminalPoolTagsLabel(tags map[string]string) string {
	if len(tags) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(tags))
	for key, value := range tags {
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, ",")
}

func terminalPoolPreviewLabel(row state.TerminalPoolPageItem) string {
	stateText := row.State
	if stateText == "" {
		stateText = "unknown"
	}
	return "last known " + row.TerminalID + " " + stateText
}

func terminalPoolStateLine(pool state.TerminalPoolStore) (Line, bool) {
	switch pool.Status {
	case state.TerminalPoolLoading:
		return Line{Cells: []Cell{styledCell("pool ", StyleMuted), NewCell("loading terminals")}}, true
	case state.TerminalPoolError:
		return Line{Cells: []Cell{styledCell("pool error ", StyleWarning), NewCell(pool.LastError)}}, true
	}
	return Line{}, false
}

func terminalPickerSelectableCount(rows []state.TerminalPickerItem) int {
	count := 0
	for _, row := range rows {
		if !row.CreateNew {
			count++
		}
	}
	return count
}

func terminalPickerStatus(count int, query string) string {
	if query == "" {
		return fmt.Sprintf("terminal picker: %d items", count)
	}
	return fmt.Sprintf("terminal picker: %d items query:%s", count, query)
}

func searchLabel(query string) string {
	if query == "" {
		return "[type to filter]"
	}
	return query
}

func contentActionLine(action string, label string) Line {
	return Line{Cells: []Cell{
		styledCell(" ["+action+"] ", StyleAccent),
		NewCell(" " + label),
	}}
}

func pageTitleLine(title string, subtitle string) Line {
	return Line{Cells: []Cell{
		styledCell("◆ "+title, StyleAccent),
		NewCell(" "),
		styledCell(subtitle, StyleMuted),
	}}
}

func searchRowLine(query string, placeholder string) Line {
	value := searchLabel(query)
	if query == "" && placeholder != "" {
		value = "[" + placeholder + "]"
	}
	style := StyleAccent
	if query == "" {
		style = StyleMuted
	}
	return Line{Cells: []Cell{
		styledCell("⌕ search ", StyleMuted),
		styledCell(value, style),
	}}
}

func searchCursorCol(query string) int {
	return DisplayWidth("⌕ search ") + DisplayWidth(query)
}

func detailHeaderLine(label string, value string) Line {
	if strings.TrimSpace(value) == "" {
		value = "-"
	}
	return Line{Cells: []Cell{
		styledCell(strings.ToUpper(label)+" ", StyleMuted),
		NewCell(value),
	}}
}

func detailTokenLine(tokens []string) Line {
	cells := make([]Cell, 0, len(tokens)*2)
	for index, token := range tokens {
		if index > 0 {
			cells = append(cells, NewCell(" "))
		}
		cells = append(cells, tokenCell(token, StyleMuted))
	}
	return Line{Cells: cells}
}

func formFieldLine(label string, value string, filled bool) Line {
	style := StyleMuted
	if filled {
		style = StyleAccent
	}
	return Line{Cells: []Cell{
		styledCell(strings.ToUpper(label)+" ", StyleMuted),
		styledCell(value, style),
	}}
}

func helpTopicLine(label string, value string) Line {
	return Line{Cells: []Cell{
		tokenCell(label, StyleAccent),
		NewCell(" "),
		NewCell(value),
	}}
}

func tokenCell(text string, style StyleToken) Cell {
	return styledCell(" "+text+" ", style)
}

func terminalPoolStateStyle(stateText string) StyleToken {
	switch strings.ToLower(stateText) {
	case "ready", "running", "attached", "live":
		return StyleSuccess
	case "failed", "error", "exited":
		return StyleWarning
	default:
		return StyleMuted
	}
}

func contentActionRegions(actions []ActionID, paneID string, rowOffset int) []HitRegion {
	regions := make([]HitRegion, len(actions))
	for index, action := range actions {
		regions[index] = HitRegion{
			Kind:     HitRegionContentAction,
			Rect:     Rect{Y: index + rowOffset, W: contentActionWidth, H: 1},
			PaneID:   paneID,
			ActionID: action.String(),
		}
	}
	return regions
}
