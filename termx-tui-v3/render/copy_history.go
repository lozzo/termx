package render

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

func buildCopyHistoryContent(history state.HistoryStore, copyMode state.CopyModeStore) ContentVM {
	if copyMode.Entering {
		return ContentVM{
			Kind:    ContentCopyHistory,
			Lines:   []Line{{Cells: []Cell{styledCell("copy history ", StyleMuted), NewCell("loading")}}},
			Status:  "copy: loading history",
			Pending: true,
			Extent:  ContentExtent{Known: true, Cols: maxInt(1, copyMode.BoundCols), Rows: maxInt(1, copyMode.ViewRows)},
		}
	}
	rows := history.Rows
	// 中文说明：这里的 rows 是 core-v2 HistoryWindow 的 TUI 投影；
	// renderer 不能从 VTerm scrollback 或 live surface 推断历史内容。
	if len(rows) == 0 {
		return ContentVM{
			Kind:   ContentCopyHistory,
			Lines:  []Line{{Cells: []Cell{styledCell("copy history ", StyleMuted), NewCell("empty")}}},
			Status: "copy: empty",
			Empty:  true,
			Extent: ContentExtent{Known: true, Cols: maxInt(1, copyMode.BoundCols), Rows: maxInt(1, copyMode.ViewRows)},
		}
	}
	viewRows := copyMode.ViewRows
	if viewRows <= 0 {
		viewRows = len(rows)
	}
	if viewRows <= 0 {
		viewRows = 1
	}
	top := clampRenderInt(copyMode.ViewportTop, 0, maxInt(0, len(rows)-1))
	if top > len(rows)-1 {
		top = len(rows) - 1
	}
	bottom := minInt(len(rows), top+viewRows)
	if bottom < top {
		bottom = top
	}
	lines := make([]Line, 0, bottom-top)
	regions := make([]HitRegion, 0, bottom-top)
	for rowIndex := top; rowIndex < bottom; rowIndex++ {
		row := rows[rowIndex]
		highlight := rowIndex == copyMode.Cursor.Row || copyHistoryRowSelected(copyMode, rowIndex)
		line := Line{Cells: copyHistoryRowCells(row, highlight)}
		lines = append(lines, line)
		regions = append(regions, HitRegion{
			Kind: HitRegionHistoryRow,
			Row:  rowIndex,
			Rect: Rect{Y: len(lines) - 1, W: maxInt(1, lineDisplayWidth(line)), H: 1},
		})
	}
	cursor := copyHistoryCursor(copyMode, history, top, len(lines))
	return ContentVM{
		Kind:       ContentCopyHistory,
		Lines:      lines,
		Status:     copyHistoryStatus(history, copyMode),
		Cursor:     cursor,
		HitRegions: regions,
		Extent:     ContentExtent{Known: true, Cols: maxInt(1, history.Cols), Rows: maxInt(1, len(rows))},
	}
}

func copyHistoryRowCells(row state.HistoryRow, highlight bool) []Cell {
	cells := make([]Cell, 0, len(row.Cells)+3)
	if row.ClippedStart {
		cells = append(cells, styledCell("...", StyleMuted))
	}
	if len(row.Cells) == 0 {
		text := SafeLine(strings.ReplaceAll(row.Text, "\n", " "))
		if text == "" {
			text = " "
		}
		cell := NewCell(text)
		cell.TerminalContent = true
		if highlight {
			cell.Style = StyleAccent
		}
		cells = append(cells, cell)
	} else {
		// 中文说明：copy/history 直接投影 core-v2 authoritative history cell，
		// 只保留 ANSI 样式和链接语义，不重新解析 live ANSI 文本。
		for _, historyCell := range row.Cells {
			cell := copyHistoryCell(historyCell)
			if highlight && cell.Style == "" && cell.ANSIStyle.IsZero() {
				cell.Style = StyleAccent
			}
			cells = append(cells, cell)
		}
	}
	if row.ClippedEnd {
		cells = append(cells, styledCell("...", StyleMuted))
	}
	if len(cells) == 0 {
		cells = append(cells, NewCell(" "))
	}
	return cells
}

func copyHistoryCell(cell state.HistoryCell) Cell {
	text := SafeLine(strings.ReplaceAll(cell.Text, "\n", " "))
	width := cell.Width
	if width <= 0 {
		width = DisplayWidth(text)
	}
	return Cell{
		Text:            text,
		Width:           width,
		ANSIStyle:       copyHistoryANSIStyle(cell.Style),
		LinkURL:         cell.LinkURL,
		LinkParams:      cell.LinkParams,
		TerminalContent: true,
		Safe:            true,
	}
}

func copyHistoryANSIStyle(style state.HistoryCellStyle) ANSICellStyle {
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

func copyHistoryCursor(copyMode state.CopyModeStore, history state.HistoryStore, top int, renderedRows int) Cursor {
	if renderedRows <= 0 || len(history.Rows) == 0 {
		return Cursor{}
	}
	rowIndex := clampRenderInt(copyMode.Cursor.Row, 0, len(history.Rows)-1)
	rowOffset := rowIndex - top
	if rowOffset < 0 || rowOffset >= renderedRows {
		return Cursor{}
	}
	width := state.HistoryRowDisplayWidth(history.Rows[rowIndex])
	return Cursor{
		Visible: true,
		Row:     rowOffset,
		Col:     clampRenderInt(copyMode.Cursor.Col, 0, width),
		Shape:   CursorShapeBlock,
	}
}

func copyHistoryStatus(history state.HistoryStore, copyMode state.CopyModeStore) string {
	total := len(history.Rows)
	if total == 0 {
		return "copy: empty"
	}
	row := clampRenderInt(copyMode.Cursor.Row, 0, total-1)
	older := string(history.OlderRequestState())
	newer := string(history.NewerRequestState())
	return fmt.Sprintf("copy: row %d/%d older=%s newer=%s y=copy esc=exit", row+1, total, older, newer)
}

func copyHistoryRowSelected(copyMode state.CopyModeStore, rowIndex int) bool {
	if copyMode.Selection == nil {
		return false
	}
	start, end := copyMode.Selection.Anchor.Row, copyMode.Selection.Focus.Row
	if start > end {
		start, end = end, start
	}
	return rowIndex >= start && rowIndex <= end
}

func lineDisplayWidth(line Line) int {
	width := 0
	for _, cell := range line.Cells {
		if cell.Width > 0 {
			width += cell.Width
			continue
		}
		width += DisplayWidth(cell.Text)
	}
	return width
}

func clampRenderInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
