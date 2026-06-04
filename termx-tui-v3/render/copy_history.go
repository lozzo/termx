package render

import (
	"fmt"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

type copySelectionRange struct {
	active bool
	start  state.CopyPosition
	end    state.CopyPosition
}

func copyHistoryLines(history state.HistoryStore, copyMode state.CopyModeStore) []Line {
	if len(history.Rows) == 0 {
		return nil
	}
	selection := normalizedCopySelection(copyMode)
	lines := make([]Line, len(history.Rows))
	for i, row := range history.Rows {
		lines[i] = copyHistoryLine(row, i, selection)
	}
	return lines
}

func copyHistoryLine(row state.HistoryRow, rowIndex int, selection copySelectionRange) Line {
	cells := []Cell{copyHistoryMarkerCell(row)}
	cells = append(cells, copyHistoryTextCells(row.Text, rowIndex, selection)...)
	if row.ClippedEnd {
		cells = append(cells, styledCell(" ⇣", StyleMuted))
	}
	return Line{Cells: cells}
}

// marker 只表达当前 authoritative row 的 UI 语义，不写回历史 truth。
func copyHistoryMarkerCell(row state.HistoryRow) Cell {
	marker := "● "
	if row.RowInLine > 0 {
		marker = "╎ "
	}
	if row.ClippedStart {
		marker = "⇡ "
	}
	return styledCell(marker, StyleMuted)
}

func copyHistoryTextCells(text string, row int, selection copySelectionRange) []Cell {
	if text == "" {
		return []Cell{NewCell("")}
	}
	if !selection.active || row < selection.start.Row || row > selection.end.Row {
		return []Cell{NewCell(text)}
	}
	runes := []rune(text)
	from := 0
	to := len(runes)
	if row == selection.start.Row {
		from = clampCopyColumn(selection.start.Col, 0, len(runes))
	}
	if row == selection.end.Row {
		to = clampCopyColumn(selection.end.Col, 0, len(runes))
	}
	if from > to {
		from, to = to, from
	}
	cells := make([]Cell, 0, 3)
	if from > 0 {
		cells = append(cells, NewCell(string(runes[:from])))
	}
	if from < to {
		cells = append(cells, styledCell(string(runes[from:to]), StyleAccent))
	}
	if to < len(runes) {
		cells = append(cells, NewCell(string(runes[to:])))
	}
	if len(cells) == 0 {
		cells = append(cells, NewCell(text))
	}
	return cells
}

func normalizedCopySelection(copyMode state.CopyModeStore) copySelectionRange {
	if copyMode.Selection == nil {
		return copySelectionRange{}
	}
	start := copyMode.Selection.Anchor
	end := copyMode.Selection.Focus
	if copyPositionAfter(start, end) {
		start, end = end, start
	}
	return copySelectionRange{active: true, start: start, end: end}
}

func copyHistoryCursor(history state.HistoryStore, copyMode state.CopyModeStore) Cursor {
	if !copyMode.Active || len(history.Rows) == 0 {
		return Cursor{}
	}
	row := clampCopyColumn(copyMode.Cursor.Row, 0, len(history.Rows)-1)
	text := history.Rows[row].Text
	runes := []rune(text)
	colRunes := clampCopyColumn(copyMode.Cursor.Col, 0, len(runes))
	return Cursor{
		Visible: true,
		Row:     row,
		Col:     copyHistoryMarkerCell(history.Rows[row]).Width + DisplayWidth(string(runes[:colRunes])),
		Shape:   CursorShapeBlock,
	}
}

func copyHistoryStatus(history state.HistoryStore, copyMode state.CopyModeStore) string {
	if copyMode.Empty {
		return "copy: empty"
	}
	if len(history.Rows) == 0 {
		return "copy"
	}
	row := clampCopyColumn(copyMode.Cursor.Row, 0, len(history.Rows)-1)
	lineID := history.Rows[row].LineID
	return fmt.Sprintf("copy: row %d/%d line:%d cols:%d", row+1, len(history.Rows), lineID, history.Cols)
}

func copyHistoryHitRegions(history state.HistoryStore) []HitRegion {
	regions := make([]HitRegion, len(history.Rows))
	for i, row := range history.Rows {
		regions[i] = HitRegion{
			Kind:   HitRegionHistoryRow,
			Rect:   Rect{Y: i, W: history.Cols + copyHistoryMarkerCell(row).Width + 2, H: 1},
			LineID: row.LineID,
			Row:    i,
		}
	}
	return regions
}

func styledCell(text string, style StyleToken) Cell {
	return Cell{Text: SafeLine(text), Width: DisplayWidth(text), Style: style, Safe: true}
}

func copyPositionAfter(left state.CopyPosition, right state.CopyPosition) bool {
	if left.Row != right.Row {
		return left.Row > right.Row
	}
	return left.Col > right.Col
}

func clampCopyColumn(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
