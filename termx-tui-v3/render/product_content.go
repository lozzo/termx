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
		HitRegions: contentActionRegions("exited", []string{"restart", "reconnect", "close"}, pane.ID),
	}
}

// Terminal Picker 只消费 reducer-owned root；服务端 terminal list 必须先回投 TerminalPoolStore。
func buildTerminalPickerContent(root state.Root, shell state.ShellStore) ContentVM {
	shell = shell.EnsureDefaults()
	query := strings.TrimSpace(shell.Overlay.Query)
	lines := []Line{
		{Cells: []Cell{styledCell("search ", StyleMuted), NewCell(searchLabel(query))}},
	}
	if poolLine, ok := terminalPoolStateLine(root.TerminalPool); ok {
		lines = append(lines, poolLine)
	}
	rowOffset := len(lines)
	rows := state.TerminalPickerItems(root)
	for _, row := range rows {
		lines = append(lines, terminalPickerLine(row))
	}
	lines = append(lines, terminalPickerPreviewLine(rows))
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
		Cursor:     Cursor{Visible: true, Row: 0, Col: DisplayWidth("search ") + DisplayWidth(query), Shape: CursorShapeBar},
		HitRegions: regions,
	}
}

// Terminal Pool Page 是独立管理页面；renderer 只消费 reducer-owned page/list state。
func buildTerminalPoolContent(root state.Root, shell state.ShellStore) ContentVM {
	shell = shell.EnsureDefaults()
	query := strings.TrimSpace(shell.Overlay.Query)
	rows := state.TerminalPoolPageItems(root)
	lines := []Line{
		{Cells: []Cell{styledCell("Terminal Pool", StyleAccent), NewCell(" global terminal manager")}},
		{Cells: []Cell{styledCell("search ", StyleMuted), NewCell(searchLabel(query))}},
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
		Cursor:     Cursor{Visible: true, Row: 1, Col: DisplayWidth("search ") + DisplayWidth(query), Shape: CursorShapeBar},
		HitRegions: regions,
		Pending:    root.TerminalPool.Status == state.TerminalPoolLoading,
		Empty:      root.TerminalPool.Status == state.TerminalPoolReady && len(rows) == 0,
		Error:      root.TerminalPool.LastError,
	}
}

func terminalPickerLine(row state.TerminalPickerItem) Line {
	marker := "  "
	style := StyleMuted
	if row.Selected {
		marker = "> "
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
		NewCell("  "),
		styledCell(terminalID, StyleMuted),
		NewCell("  "),
		styledCell(source, StyleMuted),
		NewCell("  "),
		styledCell(stateText, StyleMuted),
	}}
}

func terminalPoolPageRowLine(row state.TerminalPoolPageItem) Line {
	marker := "  "
	style := StyleMuted
	if row.Selected {
		marker = "> "
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
		NewCell("  "),
		styledCell(stateText, StyleMuted),
		NewCell("  "),
		styledCell(attached, StyleMuted),
		NewCell("  "),
		styledCell(terminalPoolSizeLabel(row.Cols, row.Rows), StyleMuted),
		NewCell("  "),
		styledCell(row.TerminalID, StyleMuted),
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
		{Cells: []Cell{styledCell("detail ", StyleMuted), styledCell(selected.Title, StyleAccent)}},
		NewLine("id: " + selected.TerminalID + "  state: " + stateText + "  size: " + terminalPoolSizeLabel(selected.Cols, selected.Rows)),
		NewLine("cwd: " + cwd),
		NewLine("metadata: " + terminalPoolTagsLabel(selected.Tags)),
		{Cells: []Cell{styledCell("preview ", StyleMuted), NewCell(terminalPoolPreviewLabel(selected))}},
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
		styledCell("["+action+"]", StyleAccent),
		NewCell(" " + label),
	}}
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
