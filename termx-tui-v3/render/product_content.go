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
	rows := terminalPickerRows(root, shell)
	for _, row := range rows {
		lines = append(lines, terminalPickerLine(row, shell.Overlay.TargetID))
	}
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
		Status:     fmt.Sprintf("terminal picker: %d items", len(rows)),
		Cursor:     Cursor{Visible: true, Row: 0, Col: DisplayWidth("search "), Shape: CursorShapeBar},
		HitRegions: regions,
	}
}

type terminalPickerRow struct {
	PaneID     string
	Title      string
	Kind       state.PaneKind
	TerminalID string
	Active     bool
}

func terminalPickerRows(root state.Root, shell state.ShellStore) []terminalPickerRow {
	tab := activeTab(shell)
	rows := make([]terminalPickerRow, 0, len(tab.Panes))
	for _, pane := range tab.Panes {
		terminalID := pickerTerminalID(root, pane)
		if pane.Kind == state.PaneEmpty && terminalID == "" {
			continue
		}
		rows = append(rows, terminalPickerRow{
			PaneID:     pane.ID,
			Title:      activePaneTitle(pane),
			Kind:       pane.Kind,
			TerminalID: terminalID,
			Active:     pane.Active,
		})
	}
	if len(rows) == 0 {
		rows = append(rows, terminalPickerRow{
			PaneID:     shell.ActivePaneID,
			Title:      "current pane",
			Kind:       state.PaneEmpty,
			TerminalID: "none",
			Active:     true,
		})
	}
	return rows
}

func pickerTerminalID(root state.Root, pane state.PaneState) string {
	if pane.TerminalID != "" {
		return pane.TerminalID
	}
	if pane.Active && root.Session.TerminalID != "" {
		return root.Session.TerminalID
	}
	if pane.Active && root.Surface.TerminalID != "" {
		return root.Surface.TerminalID
	}
	if pane.Active && root.History.TerminalID != "" {
		return root.History.TerminalID
	}
	return ""
}

func terminalPickerLine(row terminalPickerRow, targetPaneID string) Line {
	marker := "  "
	style := StyleMuted
	if row.Active || row.PaneID == targetPaneID {
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

func terminalPickerHitRegions(rows []terminalPickerRow) []HitRegion {
	regions := make([]HitRegion, 0, len(rows)+1)
	for index, row := range rows {
		regions = append(regions, HitRegion{
			Kind:     HitRegionContentAction,
			Rect:     Rect{Y: index + 1, W: 48, H: 1},
			PaneID:   row.PaneID,
			ActionID: "picker.attach",
		})
	}
	return regions
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
