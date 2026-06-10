package render

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

const contentActionWidth = 12

// empty pane 内容只描述当前 pane 可执行的产品动作，不创建 terminal。
func buildEmptyPaneContent(pane state.PaneState) ContentVM {
	title := activePaneTitle(pane)
	lines := []Line{
		{Cells: []Cell{styledCell("No terminal attached ", StyleMuted), styledCell(title, StyleAccent)}},
		NewLine(""),
		contentActionLine("attach", "Attach existing"),
		contentActionLine("create", "New terminal"),
		contentActionLine("manager", "Terminal Pool"),
		contentActionLine("close", "Close"),
	}
	return ContentVM{
		Kind:       ContentEmptyPane,
		Lines:      lines,
		Status:     "empty: Attach existing / New terminal / Terminal Pool / Close",
		Cursor:     Cursor{Visible: true, Row: 0, Col: DisplayWidth("No terminal attached ") + DisplayWidth(title), Shape: CursorShapeBar},
		Empty:      true,
		HitRegions: contentActionRegions([]ActionID{ActionEmptyAttach, ActionEmptyCreate, ActionEmptyManager, ActionEmptyClose}, pane.ID, 2),
	}
}

// exited pane 内容保留 last state 与 recovery CTA；真实 restart 仍由后续 service 切片接入。
func buildExitedPaneContent(pane state.PaneState) ContentVM {
	title := activePaneTitle(pane)
	terminalID := pane.TerminalID
	if terminalID == "" {
		terminalID = "detached"
	}
	lines := []Line{
		{Cells: []Cell{styledCell("Terminal exited ", StyleWarning), styledCell(title, StyleAccent)}},
		NewLine("last state: " + terminalID),
		contentActionLine("restart", "Restart"),
		contentActionLine("reconnect", "Reconnect"),
		contentActionLine("close", "Close"),
	}
	return ContentVM{
		Kind:       ContentExitedPane,
		Lines:      lines,
		Status:     "exited: Restart / Reconnect / Close",
		Cursor:     Cursor{Visible: true, Row: 0, Col: DisplayWidth("Terminal exited ") + DisplayWidth(title), Shape: CursorShapeBar},
		HitRegions: contentActionRegions([]ActionID{ActionExitedRestart, ActionExitedReconnect, ActionExitedClose}, pane.ID, 2),
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

// Workbench Tree 只展示 reducer-owned workbench 结构，不读取 Terminal Pool 或远端状态。
func buildWorkbenchTreeContent(root state.Root, shell state.ShellStore) ContentVM {
	shell = shell.EnsureDefaults()
	query := strings.TrimSpace(shell.Overlay.Query)
	rows := state.WorkbenchTreeItems(root)
	lines := []Line{
		pageTitleLine("Workbench Tree", "TUI storage projection"),
		searchRowLine(query, "main"),
	}
	rowOffset := len(lines)
	for _, row := range rows {
		lines = append(lines, workbenchTreeRowLine(row))
	}
	lines = append(lines, workbenchTreeDetailLines(rows)...)
	actionOffset := len(lines)
	lines = append(lines,
		contentActionLine("open", "Open"),
		contentActionLine("rename", "Rename"),
		contentActionLine("new", "New"),
		contentActionLine("delete", "Delete"),
	)
	regions := workbenchTreeHitRegions(rows, rowOffset)
	regions = append(regions,
		HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset, W: contentActionWidth, H: 1}, Row: -1, ActionID: ActionWorkbenchOpen.String()},
		HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset + 1, W: contentActionWidth, H: 1}, Row: -1, ActionID: ActionWorkbenchRename.String()},
		HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset + 2, W: contentActionWidth, H: 1}, Row: -1, ActionID: ActionWorkbenchNew.String()},
		HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset + 3, W: contentActionWidth, H: 1}, Row: -1, ActionID: ActionWorkbenchDelete.String()},
	)
	return ContentVM{
		Kind:       ContentWorkbenchTree,
		Lines:      lines,
		Status:     workbenchTreeStatus(len(rows), query),
		Cursor:     Cursor{Visible: true, Row: 1, Col: searchCursorCol(query), Shape: CursorShapeBar},
		HitRegions: regions,
		Empty:      len(rows) == 0,
	}
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
	lines := []Line{pageTitleLine("Help", "available actions")}
	for _, group := range helpActionGroups() {
		if line, ok := helpActionGroupLine(group); ok {
			lines = append(lines, line)
		}
	}
	lines = append(lines, contentActionLine("close", "Close Help"))
	return ContentVM{
		Kind:   ContentHelp,
		Lines:  lines,
		Status: "help: available actions",
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
		{Label: "Most used", Details: []string{"Ctrl-p pane", "Ctrl-r resize", "Ctrl-f picker", "Ctrl-g global"}},
		{Label: "Pane", Items: []helpActionItem{
			{Action: ActionPaneSplitDown},
			{Action: ActionPaneSplitRight},
			{Action: ActionPaneClose},
		}, Details: []string{"focus/zoom/balance via pane mode keys"}},
		{Label: "Tab", Items: []helpActionItem{
			{Action: ActionTabCreate},
			{Action: ActionTabClose},
		}, Details: []string{"switch/rename via tab mode keys"}},
		{Label: "Footer", Items: []helpActionItem{
			{Action: ActionFooterPaneMode},
			{Action: ActionFooterResizeMode},
			{Action: ActionFooterPicker},
			{Action: ActionFooterGlobalMode},
			{Action: ActionFooterOpenPool},
			{Action: ActionFooterOpenTree},
		}},
		{Label: "Floating", Items: []helpActionItem{
			{Action: ActionFloatingRaise},
			{Action: ActionFloatingResize},
			{Action: ActionFloatingMoveDrag},
			{Action: ActionFloatingClose},
		}},
		{Label: "Terminal Pool", Items: []helpActionItem{
			{Action: ActionPoolSelect},
			{Action: ActionPoolAttach},
			{Action: ActionPoolKill},
		}, Details: []string{"search"}},
		{Label: "Workbench Tree", Items: []helpActionItem{
			{Action: ActionWorkbenchOpen},
			{Action: ActionWorkbenchRename},
			{Action: ActionWorkbenchNew},
		}},
		{Label: "Prompt", Items: []helpActionItem{
			{Action: ActionPromptSubmit},
			{Action: ActionPromptCancel},
		}, Details: []string{"confirm"}},
		{Label: "Copy", Details: []string{"authoritative HistoryWindow only"}},
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
	case state.PaneExited:
		return "exited"
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
	style := StyleMuted
	if row.Selected {
		marker = "▌ "
		style = StyleAccent
	}
	active := ""
	if row.Active {
		active = " active"
	}
	return Line{Cells: []Cell{
		styledCell(marker, style),
		NewCell(strings.Repeat("  ", row.Depth)),
		tokenCell(workbenchTreeKindLabel(row), style),
		NewCell(" "),
		styledCell(workbenchTreeTitle(row), style),
		NewCell(" "),
		styledCell(row.Summary+active, StyleMuted),
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

func workbenchTreeHitRegions(rows []state.WorkbenchTreeItem, rowOffset int) []HitRegion {
	regions := make([]HitRegion, 0, len(rows)+1)
	for index, row := range rows {
		regions = append(regions, HitRegion{
			Kind:     HitRegionContentAction,
			Rect:     Rect{Y: rowOffset + index, W: 72, H: 1},
			Row:      index,
			PaneID:   row.PaneID,
			ActionID: ActionWorkbenchSelect.String(),
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
		return row.Summary
	default:
		return "-"
	}
}

func workbenchTreePreview(row state.WorkbenchTreeItem) string {
	if row.Kind == state.WorkbenchTreeKindFloating {
		return "floating overview placeholder; drag/resize belongs to later floating slice"
	}
	if row.Summary != "" {
		return row.Summary
	}
	return "ready"
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
