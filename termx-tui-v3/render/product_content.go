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
		{Cells: []Cell{styledCell("empty pane ", StyleMuted), styledCell(title, StyleAccent)}},
		NewLine("no terminal is attached to this pane"),
		contentActionLine("attach", "attach terminal"),
		contentActionLine("create", "new terminal"),
		contentActionLine("manager", "open terminal manager"),
		contentActionLine("close", "close pane"),
	}
	return ContentVM{
		Kind:       ContentEmptyPane,
		Lines:      lines,
		Status:     "empty: attach/create/manager/close",
		Cursor:     Cursor{Visible: true, Row: 0, Col: DisplayWidth("empty pane ") + DisplayWidth(title), Shape: CursorShapeBar},
		Empty:      true,
		HitRegions: contentActionRegions("empty", []string{"attach", "create", "manager", "close"}, pane.ID),
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
		{Cells: []Cell{styledCell("exited pane ", StyleWarning), styledCell(title, StyleAccent)}},
		NewLine("last state: " + terminalID),
		contentActionLine("restart", "restart terminal"),
		contentActionLine("reconnect", "reattach terminal"),
		contentActionLine("close", "close pane"),
	}
	return ContentVM{
		Kind:       ContentExitedPane,
		Lines:      lines,
		Status:     "exited: restart/reconnect/close",
		Cursor:     Cursor{Visible: true, Row: 0, Col: DisplayWidth("exited pane ") + DisplayWidth(title), Shape: CursorShapeBar},
		HitRegions: contentActionRegions("exited", []string{"restart", "reconnect", "close"}, pane.ID),
	}
}

// Terminal Picker 只消费 reducer-owned root；服务端 terminal list 必须先回投 TerminalPoolStore。
func buildTerminalPickerContent(root state.Root, shell state.ShellStore) ContentVM {
	shell = shell.EnsureDefaults()
	query := strings.TrimSpace(shell.Overlay.Query)
	lines := []Line{
		searchRowLine(query, "filter terminals"),
	}
	if poolLine, ok := terminalPoolStateLine(root.TerminalPool); ok {
		lines = append(lines, poolLine)
	}
	rowOffset := len(lines)
	rows := state.TerminalPickerItems(root)
	for _, row := range rows {
		lines = append(lines, terminalPickerLine(row))
	}
	lines = append(lines, detailHeaderLine("preview", terminalPickerPreviewText(rows)))
	lines = append(lines, contentActionLine("new", "new terminal"))
	regions := terminalPickerHitRegions(rows, rowOffset)
	regions = append(regions, HitRegion{
		Kind:     HitRegionContentAction,
		Rect:     Rect{Y: len(lines) - 1, W: contentActionWidth, H: 1},
		ActionID: "picker.new",
	})
	return ContentVM{
		Kind:       ContentTerminalPicker,
		Lines:      lines,
		Status:     terminalPickerStatus(len(rows), query),
		Cursor:     Cursor{Visible: true, Row: 0, Col: searchCursorCol(query), Shape: CursorShapeBar},
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
		searchRowLine(query, "terminal id / title / cwd / tag"),
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
		contentActionLine("edit", "Edit Metadata"),
		contentActionLine("kill", "Kill Terminal"),
	)
	regions := terminalPoolPageHitRegions(rows, rowOffset)
	regions = append(regions,
		HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset, W: contentActionWidth, H: 1}, Row: -1, ActionID: "pool.attach"},
		HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset + 1, W: contentActionWidth, H: 1}, Row: -1, ActionID: "pool.edit"},
		HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset + 2, W: contentActionWidth, H: 1}, Row: -1, ActionID: "pool.kill"},
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
		pageTitleLine("Workbench Tree", "structure navigator"),
		searchRowLine(query, "workspace / tab / pane"),
	}
	rowOffset := len(lines)
	for _, row := range rows {
		lines = append(lines, workbenchTreeRowLine(row))
	}
	lines = append(lines, workbenchTreeDetailLines(rows)...)
	actionOffset := len(lines)
	lines = append(lines, contentActionLine("open", "Open / Focus"))
	regions := workbenchTreeHitRegions(rows, rowOffset)
	regions = append(regions, HitRegion{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset, W: contentActionWidth, H: 1}, Row: -1, ActionID: "workbench.open"})
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
		pageTitleLine(title, "esc to cancel"),
		detailHeaderLine("context", prompt.Context),
		formFieldLine("input", displayValue, value != ""),
	}
	if prompt.Destructive {
		lines = append(lines, Line{Cells: []Cell{styledCell(" ! confirm ", StyleWarning), NewCell("type " + prompt.ConfirmText + " before submit")}})
	}
	if prompt.LastResult != "" && !prompt.Submitted {
		lines = append(lines, detailHeaderLine("status", prompt.LastResult))
	}
	actionOffset := len(lines)
	lines = append(lines,
		contentActionLine("submit", "Submit"),
		contentActionLine("cancel", "Cancel"),
	)
	return ContentVM{
		Kind:   ContentPrompt,
		Lines:  lines,
		Status: "prompt: submit/cancel",
		Cursor: Cursor{Visible: true, Row: 2, Col: DisplayWidth("INPUT ") + DisplayWidth(value), Shape: CursorShapeBar},
		HitRegions: []HitRegion{
			{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset, W: contentActionWidth, H: 1}, ActionID: "prompt.submit"},
			{Kind: HitRegionContentAction, Rect: Rect{Y: actionOffset + 1, W: contentActionWidth, H: 1}, ActionID: "prompt.cancel"},
		},
	}
}

func buildHelpContent(shell state.ShellStore) ContentVM {
	shell = shell.EnsureDefaults()
	lines := []Line{
		pageTitleLine("Help", "concepts and actions"),
		helpTopicLine("Most used", "Ctrl-p pane  Ctrl-r resize  Ctrl-g global  Ctrl-o floating"),
		helpTopicLine("Pane", "split / close / focus / zoom / balance / card-split"),
		helpTopicLine("Tab", "create / switch / rename / close"),
		helpTopicLine("Workspace", "switch / create / rename / tree navigation"),
		helpTopicLine("Floating", "new / move / resize / center / collapse / close"),
		helpTopicLine("Terminal Pool", "search / attach / edit metadata / kill feedback"),
		helpTopicLine("Display/Copy", "authoritative HistoryWindow only; no live fallback"),
		helpTopicLine("Prompt", "local reducer-owned input / submit / cancel / confirm"),
		contentActionLine("close", "Close Help"),
	}
	return ContentVM{
		Kind:   ContentHelp,
		Lines:  lines,
		Status: "help: concepts/actions",
		Cursor: Cursor{Visible: false},
		HitRegions: []HitRegion{{
			Kind:     HitRegionContentAction,
			Rect:     Rect{Y: len(lines) - 1, W: contentActionWidth, H: 1},
			ActionID: "help.close",
		}},
	}
}

func terminalPickerLine(row state.TerminalPickerItem) Line {
	marker := "  "
	style := StyleMuted
	if row.Selected {
		marker = "▌ "
		style = StyleAccent
	}
	terminalID := row.TerminalID
	if terminalID == "" {
		terminalID = "none"
	}
	source := "pane"
	if row.FromPool {
		source = "pool"
	}
	stateText := row.PoolState
	if stateText == "" {
		stateText = string(row.Kind)
	}
	return Line{Cells: []Cell{
		styledCell(marker, style),
		styledCell(row.Title, style),
		NewCell(" "),
		tokenCell(source, StyleInfo),
		NewCell(" "),
		tokenCell(stateText, StyleMuted),
		NewCell(" "),
		styledCell(terminalID, StyleMuted),
	}}
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
			ActionID: "pool.select",
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
			ActionID: "workbench.select",
		})
	}
	return regions
}

func terminalPickerHitRegions(rows []state.TerminalPickerItem, rowOffset int) []HitRegion {
	regions := make([]HitRegion, 0, len(rows)+1)
	for index, row := range rows {
		regions = append(regions, HitRegion{
			Kind:     HitRegionContentAction,
			Rect:     Rect{Y: rowOffset + index, W: 48, H: 1},
			PaneID:   row.PaneID,
			Row:      index,
			ActionID: "picker.attach",
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

func terminalPickerPreviewLine(rows []state.TerminalPickerItem) Line {
	if len(rows) == 0 {
		return Line{Cells: []Cell{styledCell("preview ", StyleMuted), NewCell("no terminal")}}
	}
	selected := rows[0]
	for _, row := range rows {
		if row.Selected {
			selected = row
			break
		}
	}
	terminalID := selected.TerminalID
	if terminalID == "" {
		terminalID = "none"
	}
	return Line{Cells: []Cell{
		styledCell("preview ", StyleMuted),
		NewCell("pane:" + selected.PaneID + " "),
		NewCell("term:" + terminalID + " "),
		styledCell("source:"+terminalPickerSource(selected), StyleMuted),
	}}
}

func terminalPickerPreviewText(rows []state.TerminalPickerItem) string {
	if len(rows) == 0 {
		return "no terminal"
	}
	selected := rows[0]
	for _, row := range rows {
		if row.Selected {
			selected = row
			break
		}
	}
	terminalID := selected.TerminalID
	if terminalID == "" {
		terminalID = "none"
	}
	return "pane:" + selected.PaneID + " term:" + terminalID + " source:" + terminalPickerSource(selected)
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
	case state.TerminalPoolReady:
		if len(pool.Items) == 0 {
			return Line{Cells: []Cell{styledCell("pool ", StyleMuted), NewCell("empty")}}, true
		}
	}
	return Line{}, false
}

func terminalPickerSource(row state.TerminalPickerItem) string {
	if row.FromPool {
		return "pool"
	}
	return "pane"
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

func contentActionRegions(prefix string, actions []string, paneID string) []HitRegion {
	regions := make([]HitRegion, len(actions))
	for index, action := range actions {
		regions[index] = HitRegion{
			Kind:     HitRegionContentAction,
			Rect:     Rect{Y: index + 2, W: contentActionWidth, H: 1},
			PaneID:   paneID,
			ActionID: prefix + "." + action,
		}
	}
	return regions
}
