package render

const contentViewportDot = "·"

// RenderContentViewport 是所有 pane/floating 内容投影的基础合同：
// terminal extent 内保持真实空白，extent 外才画弱占位点；遮挡只通过 overflow hint 交给 chrome。
func RenderContentViewport(request ContentRenderRequest) ContentRenderResult {
	rect := request.Rect
	if rect.W <= 0 || rect.H <= 0 {
		return ContentRenderResult{}
	}
	content := request.Content
	lines := content.Lines
	if len(lines) == 0 {
		lines = []Line{NewLine(content.Status)}
	}
	extent := normalizeContentExtent(content.Extent, rect)
	rendered := make([]Line, 0, rect.H)
	overflow := contentViewportOverflow(lines, extent, rect)
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
			cells = append(cells, contentViewportDotCell())
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
			cells = append(cells, Cell{Text: " ", Width: 1, Safe: true})
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
	out := make([]canvasSegment, 0, width)
	for _, segment := range cellSegmentsFromLine(line, end, "content:viewport", LayerPanel) {
		if outWidth >= width {
			break
		}
		segmentStart := sourceCol
		segmentEnd := sourceCol + segment.width
		sourceCol = segmentEnd
		if segmentEnd <= start {
			continue
		}
		if segmentStart >= end {
			break
		}
		visibleStart := maxInt(segmentStart, start)
		visibleEnd := minInt(segmentEnd, end)
		visibleWidth := maxInt(0, visibleEnd-visibleStart)
		if visibleWidth == 0 {
			continue
		}
		if segmentStart >= start && segmentEnd <= end && segment.width <= width-outWidth {
			out = append(out, segment)
			outWidth += segment.width
			continue
		}
		for i := 0; i < visibleWidth && outWidth < width; i++ {
			out = append(out, contentViewportBlankSegment())
			outWidth++
		}
	}
	for outWidth < width {
		out = append(out, contentViewportBlankSegment())
		outWidth++
	}
	return Line{Cells: cellsFromSegments(out)}
}

func contentViewportDotCell() Cell {
	return Cell{Text: contentViewportDot, Width: 1, Style: StyleMuted, Safe: true}
}

func contentViewportBlankSegment() canvasSegment {
	return canvasSegment{text: " ", width: 1, safe: true}
}
