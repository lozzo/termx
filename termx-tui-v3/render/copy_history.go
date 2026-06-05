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

type copySearchRange struct {
	active bool
	match  state.CopyMatch
}

func copyHistoryLines(history state.HistoryStore, copyMode state.CopyModeStore) []Line {
	if len(history.Rows) == 0 {
		return nil
	}
	selection := normalizedCopySelection(copyMode)
	rows := copyVisibleRows(history, copyMode)
	lines := make([]Line, 0, len(rows)+2)
	lines = append(lines, copyHistorySearchLine(copyMode, len(history.Rows)))
	for _, visible := range rows {
		lines = append(lines, copyHistoryLine(history.Rows[visible], visible, selection, activeSearchRange(copyMode, visible)))
	}
	lines = append(lines, copyHistoryScrollbarLine(history, copyMode, len(rows)))
	return lines
}

func copyHistoryLine(row state.HistoryRow, rowIndex int, selection copySelectionRange, search copySearchRange) Line {
	cells := []Cell{copyHistoryMarkerCell(row)}
	cells = append(cells, copyHistoryTextCells(row.Text, rowIndex, selection, search)...)
	if row.ClippedEnd {
		cells = append(cells, styledCell(" ⇣", StyleMuted))
	}
	return Line{Cells: cells}
}

func copyHistorySearchLine(copyMode state.CopyModeStore, totalRows int) Line {
	query := copyMode.Query
	if query == "" {
		return Line{Cells: []Cell{
			styledCell("⌕ search ", StyleMuted),
			styledCell("[/ query]", StyleMuted),
			NewCell(" "),
			styledCell(fmt.Sprintf(" rows:%d ", totalRows), StyleMuted),
		}}
	}
	return Line{Cells: []Cell{
		styledCell("⌕ search ", StyleMuted),
		styledCell(query, StyleAccent),
		NewCell(" "),
		styledCell(fmt.Sprintf(" match:%d/%d ", activeCopyMatchOrdinal(copyMode), len(copyMode.Matches)), StyleMuted),
	}}
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

func copyHistoryTextCells(text string, row int, selection copySelectionRange, search copySearchRange) []Cell {
	if text == "" {
		return []Cell{NewCell("")}
	}
	if !selection.active && !search.active {
		return []Cell{NewCell(text)}
	}
	runes := []rune(text)
	segments := make([]Cell, 0, 5)
	cursor := 0
	for cursor < len(runes) {
		nextBreak := len(runes)
		style := StyleToken("")
		if selection.active && row >= selection.start.Row && row <= selection.end.Row {
			from, to := selectionColumnsForRow(selection, row, len(runes))
			if cursor < from {
				nextBreak = minInt(nextBreak, from)
			} else if cursor < to {
				nextBreak = minInt(nextBreak, to)
				style = StyleAccent
			}
		}
		if search.active {
			from := clampCopyColumn(search.match.StartCol, 0, len(runes))
			to := clampCopyColumn(search.match.EndCol, from, len(runes))
			if cursor < from {
				nextBreak = minInt(nextBreak, from)
			} else if cursor < to {
				nextBreak = minInt(nextBreak, to)
				if style == "" {
					style = StyleWarning
				}
			}
		}
		if nextBreak <= cursor {
			nextBreak = cursor + 1
		}
		textPart := string(runes[cursor:nextBreak])
		if style != "" {
			segments = append(segments, styledCell(textPart, style))
		} else {
			segments = append(segments, NewCell(textPart))
		}
		cursor = nextBreak
	}
	if len(segments) == 0 {
		return []Cell{NewCell(text)}
	}
	return segments
}

func selectionColumnsForRow(selection copySelectionRange, row int, lineLen int) (int, int) {
	from := 0
	to := lineLen
	if row == selection.start.Row {
		from = clampCopyColumn(selection.start.Col, 0, lineLen)
	}
	if row == selection.end.Row {
		to = clampCopyColumn(selection.end.Col, 0, lineLen)
	}
	if from > to {
		from, to = to, from
	}
	return from, to
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
	visibleTop := copyHistoryViewportTop(history, copyMode)
	visibleRow := row - visibleTop + 1
	if visibleRow < 1 {
		visibleRow = 1
	}
	text := history.Rows[row].Text
	runes := []rune(text)
	colRunes := clampCopyColumn(copyMode.Cursor.Col, 0, len(runes))
	return Cursor{
		Visible: true,
		Row:     visibleRow,
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
	historyRow := history.Rows[row]
	span := copyLineSpanForRow(history, row)
	status := fmt.Sprintf("copy: row %d/%d line:%d part:%d cols:%d", row+1, len(history.Rows), historyRow.LineID, historyRow.RowInLine+1, history.Cols)
	if span.LineID != 0 {
		status += fmt.Sprintf(" span:%d-%d", span.StartRow+1, span.EndRow+1)
	}
	if copyMode.Query != "" {
		status += fmt.Sprintf(" search:%d/%d", activeCopyMatchOrdinal(copyMode), len(copyMode.Matches))
	}
	if history.HasMore {
		status += " older:more"
	}
	return status
}

func copyHistoryHitRegions(history state.HistoryStore, copyMode state.CopyModeStore) []HitRegion {
	rows := copyVisibleRows(history, copyMode)
	regions := make([]HitRegion, len(rows))
	for i, rowIndex := range rows {
		row := history.Rows[rowIndex]
		regions[i] = HitRegion{
			Kind:   HitRegionHistoryRow,
			Rect:   Rect{Y: i + 1, W: history.Cols + copyHistoryMarkerCell(row).Width + 2, H: 1},
			LineID: row.LineID,
			Row:    rowIndex,
		}
	}
	return regions
}

func copyVisibleRows(history state.HistoryStore, copyMode state.CopyModeStore) []int {
	if len(history.Rows) == 0 {
		return nil
	}
	top := copyHistoryViewportTop(history, copyMode)
	height := copyHistoryVisibleHeight(copyMode)
	if height <= 0 || top+height > len(history.Rows) {
		height = len(history.Rows) - top
	}
	rows := make([]int, 0, height)
	for i := 0; i < height; i++ {
		rows = append(rows, top+i)
	}
	return rows
}

func copyHistoryVisibleHeight(copyMode state.CopyModeStore) int {
	if copyMode.ViewRows > 0 {
		return maxInt(1, copyMode.ViewRows-2)
	}
	return 8
}

func copyHistoryViewportTop(history state.HistoryStore, copyMode state.CopyModeStore) int {
	return clampCopyColumn(copyMode.ViewportTop, 0, maxInt(0, len(history.Rows)-1))
}

func activeSearchRange(copyMode state.CopyModeStore, row int) copySearchRange {
	if copyMode.Query == "" || len(copyMode.Matches) == 0 {
		return copySearchRange{}
	}
	index := clampCopyColumn(copyMode.ActiveMatch, 0, len(copyMode.Matches)-1)
	match := copyMode.Matches[index]
	if match.Row != row {
		return copySearchRange{}
	}
	return copySearchRange{active: true, match: match}
}

func copyHistoryScrollbarLine(history state.HistoryStore, copyMode state.CopyModeStore, visible int) Line {
	total := len(history.Rows)
	top := copyHistoryViewportTop(history, copyMode)
	thumb := "█"
	if total > 0 && visible < total {
		ratio := float64(top) / float64(maxInt(1, total-visible))
		switch {
		case ratio <= 0:
			thumb = "▁"
		case ratio >= 1:
			thumb = "▔"
		default:
			thumb = "█"
		}
	}
	return Line{Cells: []Cell{
		styledCell("SCROLL ", StyleMuted),
		styledCell(thumb, StyleAccent),
		NewCell(" "),
		styledCell(fmt.Sprintf("%d-%d/%d", top+1, minInt(total, top+visible), total), StyleMuted),
	}}
}

func copyLineSpanForRow(history state.HistoryStore, row int) state.HistoryLineSpan {
	for _, span := range history.Lines {
		if row >= span.StartRow && row <= span.EndRow {
			return span
		}
	}
	return state.HistoryLineSpan{}
}

func activeCopyMatchOrdinal(copyMode state.CopyModeStore) int {
	if len(copyMode.Matches) == 0 {
		return 0
	}
	return clampCopyColumn(copyMode.ActiveMatch, 0, len(copyMode.Matches)-1) + 1
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
