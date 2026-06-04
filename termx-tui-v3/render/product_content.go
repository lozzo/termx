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

// Terminal Picker 一期只从 reducer-owned workspace panes 投影列表，不伪造 Terminal Pool 数据。
func buildTerminalPickerContent(root state.Root, shell state.ShellStore) ContentVM {
	shell = shell.EnsureDefaults()
	query := strings.TrimSpace(shell.Overlay.Query)
	lines := []Line{
		{Cells: []Cell{styledCell("search ", StyleMuted), NewCell(searchLabel(query))}},
	}
	rows := state.TerminalPickerItems(root)
	for _, row := range rows {
		lines = append(lines, terminalPickerLine(row))
	}
	lines = append(lines, terminalPickerPreviewLine(rows))
	lines = append(lines, contentActionLine("new", "new terminal"))
	regions := terminalPickerHitRegions(rows)
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
	return Line{Cells: []Cell{
		styledCell(marker, style),
		styledCell(row.Title, style),
		NewCell("  "),
		styledCell(terminalID, StyleMuted),
		NewCell("  "),
		styledCell(string(row.Kind), StyleMuted),
	}}
}

func terminalPickerHitRegions(rows []state.TerminalPickerItem) []HitRegion {
	regions := make([]HitRegion, 0, len(rows)+1)
	for index, row := range rows {
		regions = append(regions, HitRegion{
			Kind:     HitRegionContentAction,
			Rect:     Rect{Y: index + 1, W: 48, H: 1},
			PaneID:   row.PaneID,
			Row:      index,
			ActionID: "picker.attach",
		})
	}
	return regions
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
		styledCell("kind:"+string(selected.Kind), StyleMuted),
	}}
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
