package render

import "strings"

// RenderContentViewport 是所有 pane/floating 内容投影的基础合同：
// terminal extent 内外都保持空白安全；遮挡只通过 overflow hint 交给 chrome。
func RenderContentViewport(request ContentRenderRequest) ContentRenderResult {
	rect := request.Rect
	if rect.W <= 0 || rect.H <= 0 {
		return ContentRenderResult{}
	}
	content := request.Content
	if content.Kind == ContentEmptyPane {
		return renderEmptyPaneContentViewport(request)
	}
	lines := content.Lines
	if len(lines) == 0 {
		lines = []Line{NewLine(content.Status)}
	}
	extent := normalizeContentExtent(content.Extent, rect)
	rendered := make([]Line, 0, rect.H)
	overflow := contentViewportOverflow(lines, extent, rect)
	if content.Kind == ContentTerminalLive {
		overflow = ContentOverflow{}
	}
	for row := 0; row < rect.H; row++ {
		rendered = append(rendered, renderContentViewportRow(lines, extent, rect.W, row))
	}
	return ContentRenderResult{
		Lines:      rendered,
		Cursor:     content.Cursor,
		HitRegions: content.HitRegions,
		Metadata:   RenderMetadata{Width: rect.W, Height: rect.H},
		Overflow:   overflow,
	}
}

func renderEmptyPaneContentViewport(request ContentRenderRequest) ContentRenderResult {
	rect := request.Rect
	content := request.Content
	lines := content.Lines
	if len(lines) == 0 {
		lines = []Line{NewLine(content.Status)}
	}
	rendered := make([]Line, rect.H)
	for row := 0; row < rect.H; row++ {
		rendered[row] = NewLine(strings.Repeat(" ", rect.W))
	}
	startY := 0
	if rect.H >= len(lines)+2 {
		startY = 1
	}
	translatedRegions := make([]HitRegion, 0, len(content.HitRegions))
	for index, line := range lines {
		y := startY + index
		if y < 0 || y >= rect.H {
			continue
		}
		centered := centerContentLine(line, rect.W)
		rendered[y] = centered
		if index > 0 && index-1 < len(content.HitRegions) {
			region := content.HitRegions[index-1]
			region.Rect.X = centeredLineTextX(line, rect.W)
			region.Rect.Y = y
			region.Rect.W = minInt(line.Width(), rect.W)
			region.Rect.H = 1
			translatedRegions = append(translatedRegions, region)
		}
	}
	return ContentRenderResult{Lines: rendered, Cursor: Cursor{}, HitRegions: translatedRegions, Metadata: RenderMetadata{Width: rect.W, Height: rect.H}}
}

func centerContentLine(line Line, width int) Line {
	if width <= 0 {
		return Line{}
	}
	lineWidth := minInt(line.Width(), width)
	left := maxInt(0, (width-lineWidth)/2)
	right := maxInt(0, width-left-lineWidth)
	cells := make([]Cell, 0, len(line.Cells)+2)
	if left > 0 {
		cells = append(cells, NewCell(strings.Repeat(" ", left)))
	}
	cells = append(cells, contentViewportLineWindow(line, 0, lineWidth).Cells...)
	if right > 0 {
		cells = append(cells, NewCell(strings.Repeat(" ", right)))
	}
	return Line{Cells: cells}
}

func centeredLineTextX(line Line, width int) int {
	if width <= 0 {
		return 0
	}
	return maxInt(0, (width-minInt(line.Width(), width))/2)
}

func normalizeContentExtent(extent ContentExtent, rect Rect) ContentExtent {
	if !extent.Known {
		return ContentExtent{Known: true, Cols: maxInt(0, rect.W), Rows: maxInt(0, rect.H)}
	}
	if extent.Cols < 0 {
		extent.Cols = 0
	}
	if extent.Rows < 0 {
		extent.Rows = 0
	}
	return extent
}

func contentViewportOverflow(lines []Line, extent ContentExtent, rect Rect) ContentOverflow {
	overflow := ContentOverflow{
		Left:   extent.X < 0,
		Right:  extent.X+extent.Cols > rect.W,
		Top:    extent.Y < 0,
		Bottom: extent.Y+extent.Rows > rect.H,
	}
	if len(lines) > extent.Rows {
		overflow.Bottom = true
	}
	for _, line := range lines {
		if line.Width() > extent.Cols {
			overflow.Right = true
			break
		}
	}
	return overflow
}

func renderContentViewportRow(lines []Line, extent ContentExtent, width int, row int) Line {
	cells := make([]Cell, 0, width)
	if width <= 0 {
		return Line{}
	}
	for col := 0; col < width; {
		if !contentViewportInsideExtent(extent, col, row) {
			cells = append(cells, contentViewportBlankCell())
			col++
			continue
		}
		runEnd := minInt(width, extent.X+extent.Cols)
		if runEnd <= col {
			runEnd = col + 1
		}
		line := Line{}
		sourceRow := row - extent.Y
		if sourceRow >= 0 && sourceRow < len(lines) {
			line = lines[sourceRow]
		}
		visible := contentViewportLineWindow(line, col-extent.X, runEnd-col)
		cells = append(cells, visible.Cells...)
		col += visible.Width()
		if visible.Width() == 0 {
			cells = append(cells, contentViewportBlankCell())
			col++
		}
	}
	return Line{Cells: cells}
}

func contentViewportInsideExtent(extent ContentExtent, col int, row int) bool {
	return row >= extent.Y &&
		row < extent.Y+extent.Rows &&
		col >= extent.X &&
		col < extent.X+extent.Cols
}

func contentViewportLineWindow(line Line, start int, width int) Line {
	if width <= 0 {
		return Line{}
	}
	if start < 0 {
		start = 0
	}
	end := start + width
	sourceCol := 0
	outWidth := 0
	cells := make([]Cell, 0, minInt(width, len(line.Cells)+1))
	for _, cell := range line.Cells {
		if outWidth >= width {
			break
		}
		cellWidth := maxInt(0, cell.Width)
		if cellWidth == 0 {
			continue
		}
		cellStart := sourceCol
		cellEnd := sourceCol + cellWidth
		sourceCol = cellEnd
		if cellEnd <= start {
			continue
		}
		if cellStart >= end {
			break
		}
		visibleStart := maxInt(cellStart, start)
		visibleEnd := minInt(cellEnd, end)
		visibleWidth := maxInt(0, visibleEnd-visibleStart)
		if visibleWidth == 0 {
			continue
		}
		if cellStart >= start && cellEnd <= end && cellWidth <= width-outWidth {
			cells = append(cells, cell)
			outWidth += cellWidth
			continue
		}
		if cell.Text != "" {
			text := SliceCells(cell.Text, visibleStart-cellStart, visibleEnd-cellStart)
			if text != "" {
				visibleCell := cell
				visibleCell.Text = text
				visibleCell.Width = DisplayWidth(text)
				if visibleCell.Width > 0 && visibleCell.Width <= width-outWidth {
					cells = append(cells, visibleCell)
					outWidth += visibleCell.Width
					continue
				}
			}
		}
		for outWidth < minInt(width, outWidth+visibleWidth) {
			cells = append(cells, contentViewportBlankCell())
			outWidth++
		}
	}
	for outWidth < width {
		cells = append(cells, contentViewportBlankCell())
		outWidth++
	}
	return Line{Cells: cells}
}

func contentViewportBlankCell() Cell {
	return Cell{Text: " ", Width: 1, Safe: true}
}
