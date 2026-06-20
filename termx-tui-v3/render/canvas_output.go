package render

import "strings"

func (c *canvas) lines() []Line {
	lines := make([]Line, len(c.rows))
	for i, row := range c.rows {
		cells := make([]Cell, 0, canvasOutputCellCapacity(row))
		for _, cell := range row {
			if cell.continuation {
				continue
			}
			cells = appendCanvasOutputCell(cells, canvasOutputCellFromMatrix(cell))
		}
		lines[i] = Line{Cells: cells}
	}
	return lines
}

func (c *canvas) ansiLines(theme Theme) []string {
	lines := make([]string, len(c.rows))
	for rowIndex, row := range c.rows {
		lines[rowIndex] = ensureANSIReset(canvasRowANSIString(row, theme))
	}
	return lines
}

func canvasRowANSIString(row []canvasCell, theme Theme) string {
	if len(row) == 0 {
		return ""
	}
	var out strings.Builder
	modelCol := 1
	previous := Cell{}
	hasPrevious := false
	for _, cell := range row {
		if cell.continuation {
			continue
		}
		next := canvasOutputCellFromMatrix(cell)
		if hasPrevious && !canMergeCanvasOutputCell(previous, next) {
			// 中文说明：直接 ANSI 路径按输出 cell 边界复位；可合并 ASCII 段不插入额外列跳转。
			out.WriteString(ansiColumn(modelCol))
		}
		writeANSIStyledCell(&out, next, theme, modelCol)
		modelCol += maxInt(0, next.Width)
		previous = next
		hasPrevious = true
	}
	return out.String()
}

func canvasOutputCellCapacity(row []canvasCell) int {
	count := 0
	previousMergeable := false
	hasPrevious := false
	for _, cell := range row {
		if cell.continuation {
			continue
		}
		mergeable := canvasMatrixCellMergeable(cell)
		if hasPrevious && previousMergeable && mergeable {
			continue
		}
		previousMergeable = mergeable
		hasPrevious = true
		count++
	}
	if count <= 0 {
		return 1
	}
	return count
}

func canvasMatrixCellMergeable(cell canvasCell) bool {
	if cell.terminal ||
		cell.style != "" ||
		!cell.ansiStyle.IsZero() ||
		cell.linkURL != "" ||
		cell.linkParams != "" {
		return false
	}
	if cell.text == "" && cell.width == 0 {
		return true
	}
	width := cell.width
	if width <= 0 {
		width = 1
	}
	if width != len(cell.text) {
		return false
	}
	for i := 0; i < len(cell.text); i++ {
		if cell.text[i] < 0x20 || cell.text[i] > 0x7e {
			return false
		}
	}
	return true
}

func canvasOutputCellFromMatrix(cell canvasCell) Cell {
	if cell.text == "" && cell.width == 0 {
		return Cell{Text: " ", Width: 1, Safe: true}
	}
	width := cell.width
	if width <= 0 {
		width = 1
	}
	return Cell{
		Text:            cell.text,
		Width:           width,
		Style:           cell.style,
		ANSIStyle:       cell.ansiStyle,
		LinkURL:         cell.linkURL,
		LinkParams:      cell.linkParams,
		TerminalContent: cell.terminal,
		Safe:            cell.safe,
	}
}

func appendCanvasOutputCell(cells []Cell, next Cell) []Cell {
	if len(cells) == 0 || !canMergeCanvasOutputCell(cells[len(cells)-1], next) {
		return append(cells, next)
	}
	last := &cells[len(cells)-1]
	last.Text += next.Text
	last.Width += maxInt(0, next.Width)
	last.Safe = last.Safe && next.Safe
	return cells
}

func canMergeCanvasOutputCell(left Cell, right Cell) bool {
	return !left.TerminalContent &&
		!right.TerminalContent &&
		left.Style == "" &&
		right.Style == "" &&
		left.ANSIStyle.IsZero() &&
		right.ANSIStyle.IsZero() &&
		left.LinkURL == "" &&
		right.LinkURL == "" &&
		left.LinkParams == "" &&
		right.LinkParams == "" &&
		isASCIIWidthCell(left) &&
		isASCIIWidthCell(right)
}

func isASCIIWidthCell(cell Cell) bool {
	if cell.Width != len(cell.Text) {
		return false
	}
	for i := 0; i < len(cell.Text); i++ {
		if cell.Text[i] < 0x20 || cell.Text[i] > 0x7e {
			return false
		}
	}
	return true
}
