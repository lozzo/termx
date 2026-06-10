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
	lines = append(lines, copyHistorySearchLine(history, copyMode, len(history.Rows)))
	for _, visible := range rows {
		lines = append(lines, copyHistoryLine(history.Rows[visible], visible, selection, activeSearchRange(copyMode, visible)))
	}
	lines = append(lines, copyHistoryScrollbarLine(history, copyMode, len(rows)))
	return lines
}

func copyHistoryLine(row state.HistoryRow, rowIndex int, selection copySelectionRange, search copySearchRange) Line {
	cells := []Cell{copyHistoryMarkerCell(row)}
	cells = append(cells, copyHistoryRowCells(row, rowIndex, selection, search)...)
	if row.ClippedEnd {
		cells = append(cells, styledCell(" ⇣", StyleMuted))
	}
	return Line{Cells: cells}
}

func copyHistorySearchLine(history state.HistoryStore, copyMode state.CopyModeStore, totalRows int) Line {
	query := copyMode.Query
	if query == "" {
		return Line{Cells: []Cell{
			styledCell("⌕ search ", StyleMuted),
			styledCell("[/ query]", StyleMuted),
			NewCell(" "),
			styledCell(fmt.Sprintf(" rows:%d ", totalRows), StyleMuted),
			copyHistoryOlderToken(history),
		}}
	}
	return Line{Cells: []Cell{
		styledCell("⌕ search ", StyleMuted),
		styledCell(query, StyleAccent),
		NewCell(" "),
		styledCell(fmt.Sprintf(" match:%d/%d ", activeCopyMatchOrdinal(copyMode), len(copyMode.Matches)), StyleMuted),
		copyHistoryOlderToken(history),
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
	width := DisplayWidth(text)
	segments := make([]Cell, 0, 5)
	cursor := 0
	for cursor < width {
		nextBreak, style := copyHistoryNextStyleBreak(row, cursor, width, selection, search)
		if nextBreak <= cursor {
			nextBreak = cursor + 1
		}
		textPart := SliceCells(text, cursor, nextBreak)
		if textPart == "" {
			cursor = nextBreak
			continue
		}
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

func copyHistoryNextStyleBreak(row int, cursor int, lineWidth int, selection copySelectionRange, search copySearchRange) (int, StyleToken) {
	nextBreak := lineWidth
	style := StyleToken("")
	if selection.active && row >= selection.start.Row && row <= selection.end.Row {
		from, to := selectionColumnsForRow(selection, row, lineWidth)
		if cursor < from {
			nextBreak = minInt(nextBreak, from)
		} else if cursor < to {
			nextBreak = minInt(nextBreak, to)
			style = StyleAccent
		}
	}
	if search.active {
		from := clampCopyColumn(search.match.StartCol, 0, lineWidth)
		to := clampCopyColumn(search.match.EndCol, from, lineWidth)
		if cursor < from {
			nextBreak = minInt(nextBreak, from)
		} else if cursor < to {
			nextBreak = minInt(nextBreak, to)
			if style == "" {
				style = StyleWarning
			}
		}
	}
	return nextBreak, style
}

func copyHistoryStyledTextCells(text string, width int, base ANSICellStyle, row int, from int, selection copySelectionRange, search copySearchRange) []Cell {
	if text == "" {
		return nil
	}
	if width <= 0 {
		width = DisplayWidth(text)
	}
	segments := make([]Cell, 0, 3)
	globalCursor := from
	globalEnd := from + width
	for globalCursor < globalEnd {
		nextBreak, style := copyHistoryNextCellStyleBreak(row, globalCursor, globalEnd, maxInt(globalEnd, selectionLineWidth(selection, row)), selection, search)
		if nextBreak <= globalCursor {
			nextBreak = globalCursor + 1
		}
		part := SliceCells(text, globalCursor-from, nextBreak-from)
		if part == "" {
			globalCursor = nextBreak
			continue
		}
		renderWidth := DisplayWidth(part)
		if renderWidth <= 0 {
			renderWidth = len([]rune(part))
		}
		if style != "" {
			segments = append(segments, Cell{Text: SafeLine(part), Width: renderWidth, Style: style, Safe: true})
		} else {
			segments = append(segments, Cell{Text: SafeLine(part), Width: renderWidth, ANSIStyle: base, Safe: true})
		}
		globalCursor = nextBreak
	}
	return segments
}

func copyHistoryNextCellStyleBreak(row int, cursor int, cellEnd int, lineWidth int, selection copySelectionRange, search copySearchRange) (int, StyleToken) {
	nextBreak := cellEnd
	style := StyleToken("")
	if selection.active && row >= selection.start.Row && row <= selection.end.Row {
		from, to := selectionColumnsForRow(selection, row, lineWidth)
		if cursor < from {
			nextBreak = minInt(nextBreak, from)
		} else if cursor < to {
			nextBreak = minInt(nextBreak, to)
			style = StyleAccent
		}
	}
	if search.active {
		from := search.match.StartCol
		to := search.match.EndCol
		if cursor < from {
			nextBreak = minInt(nextBreak, from)
		} else if cursor < to {
			nextBreak = minInt(nextBreak, to)
			if style == "" {
				style = StyleWarning
			}
		}
	}
	if nextBreak > cellEnd {
		nextBreak = cellEnd
	}
	return nextBreak, style
}

func selectionLineWidth(selection copySelectionRange, row int) int {
	if !selection.active || row < selection.start.Row || row > selection.end.Row {
		return 0
	}
	width := 0
	if row == selection.start.Row {
		width = maxInt(width, selection.start.Col)
	}
	if row == selection.end.Row {
		width = maxInt(width, selection.end.Col)
	}
	return width
}

func copyHistoryRowCells(row state.HistoryRow, rowIndex int, selection copySelectionRange, search copySearchRange) []Cell {
	if len(row.Cells) == 0 {
		return copyHistoryTextCells(row.Text, rowIndex, selection, search)
	}
	out := make([]Cell, 0, len(row.Cells))
	cursor := 0
	for _, historyCell := range row.Cells {
		cellWidth := state.HistoryCellDisplayWidth(historyCell)
		out = append(out, renderCellsFromHistory(historyCell, rowIndex, cursor, selection, search)...)
		cursor += cellWidth
	}
	if len(out) == 0 {
		return []Cell{NewCell(row.Text)}
	}
	return out
}

func renderCellsFromHistory(cell state.HistoryCell, row int, from int, selection copySelectionRange, search copySearchRange) []Cell {
	text := SafeLine(cell.Text)
	width := cell.Width
	if width <= 0 {
		width = DisplayWidth(text)
	}
	if width <= 0 {
		width = len([]rune(text))
	}
	if selection.active || search.active {
		return copyHistoryStyledTextCells(text, width, ansiStyleFromHistory(cell.Style), row, from, selection, search)
	}
	return []Cell{{
		Text:      text,
		Width:     width,
		ANSIStyle: ansiStyleFromHistory(cell.Style),
		Safe:      true,
	}}
}

func ansiStyleFromHistory(style state.HistoryCellStyle) ANSICellStyle {
	return ANSICellStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
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
	col := clampCopyColumn(copyMode.Cursor.Col, 0, state.HistoryRowDisplayWidth(history.Rows[row]))
	return Cursor{
		Visible: true,
		Row:     visibleRow,
		Col:     copyHistoryMarkerCell(history.Rows[row]).Width + col,
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
	status += " " + copyHistoryBoundarySummary(history, copyMode)
	return status
}

func copyHistoryHitRegions(history state.HistoryStore, copyMode state.CopyModeStore) []HitRegion {
	rows := copyVisibleRows(history, copyMode)
	regions := make([]HitRegion, len(rows))
	for i, rowIndex := range rows {
		row := history.Rows[rowIndex]
		markerWidth := copyHistoryMarkerCell(row).Width
		rowWidth := state.HistoryRowDisplayWidth(row)
		if rowWidth == 0 {
			rowWidth = 1
		}
		regions[i] = HitRegion{
			Kind:   HitRegionHistoryRow,
			Rect:   Rect{X: markerWidth, Y: i + 1, W: rowWidth, H: 1},
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
		NewCell(" "),
		copyHistoryBottomToken(history, copyMode, visible),
		NewCell(" "),
		styledCell(copyHistoryBoundarySummary(history, copyMode), StyleMuted),
	}}
}

func copyHistoryOlderToken(history state.HistoryStore) Cell {
	switch history.OlderRequestState() {
	case state.OlderRequestPending:
		return styledCell("↑ loading", StyleWarning)
	case state.OlderRequestExhausted:
		return styledCell("↑ top", StyleMuted)
	case state.OlderRequestReady:
		if !history.HasMore {
			return styledCell("↑ older", StyleAccent)
		}
		return styledCell("↑ more", StyleAccent)
	default:
		return styledCell("↑ top", StyleMuted)
	}
}

func copyHistoryBottomToken(history state.HistoryStore, copyMode state.CopyModeStore, visible int) Cell {
	total := len(history.Rows)
	top := copyHistoryViewportTop(history, copyMode)
	if total == 0 || top+visible >= total {
		return styledCell("↓ latest", StyleMuted)
	}
	return styledCell("↓ loaded", StyleAccent)
}

func copyHistoryBoundarySummary(history state.HistoryStore, copyMode state.CopyModeStore) string {
	first := history.Boundary.FirstLineID
	last := history.Boundary.LastLineID
	if first == 0 && len(history.Rows) > 0 {
		first = history.Rows[0].LineID
	}
	if last == 0 && len(history.Rows) > 0 {
		last = history.Rows[len(history.Rows)-1].LineID
	}
	if first == 0 && last == 0 {
		return "lines:-"
	}
	top := copyHistoryViewportTop(history, copyMode)
	visible := copyHistoryVisibleHeight(copyMode)
	from := top + 1
	to := minInt(len(history.Rows), top+visible)
	return fmt.Sprintf("lines:%d-%d view:%d-%d", first, last, from, to)
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
