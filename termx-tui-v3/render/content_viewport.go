package render

import "strings"

// RenderContentViewport 是所有 pane/floating 内容投影的基础合同：
// terminal live 的真实 extent 外区域保留可见边界提示；遮挡只通过 overflow hint 交给 chrome。
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
	extent = applyContentLayoutToExtent(content.Layout, extent, rect)
	overflow := contentViewportOverflow(lines, extent, rect)
	markOutsideExtent := contentViewportMarksOutsideExtent(content)
	if contentViewportCanUseDirectRows(extent, rect, markOutsideExtent) {
		return ContentRenderResult{
			Lines:      contentViewportDirectRows(lines, rect.W, rect.H),
			Cursor:     contentViewportCursor(content.Cursor, extent, rect),
			HitRegions: content.HitRegions,
			Metadata:   RenderMetadata{Width: rect.W, Height: rect.H},
			Overflow:   overflow,
		}
	}
	rendered := make([]Line, 0, rect.H)
	for row := 0; row < rect.H; row++ {
		rendered = append(rendered, renderContentViewportRow(lines, extent, rect.W, row, markOutsideExtent))
	}
	return ContentRenderResult{
		Lines:      rendered,
		Cursor:     contentViewportCursor(content.Cursor, extent, rect),
		HitRegions: content.HitRegions,
		Metadata:   RenderMetadata{Width: rect.W, Height: rect.H},
		Overflow:   overflow,
	}
}

func contentViewportCursor(cursor Cursor, extent ContentExtent, rect Rect) Cursor {
	if !cursor.Visible && !cursor.Anchor {
		return cursor
	}
	cursor.Row += extent.Y
	cursor.Col += extent.X
	if cursor.Row < 0 || cursor.Row >= rect.H || cursor.Col < 0 || cursor.Col >= rect.W {
		cursor.Visible = false
	}
	return cursor
}

func applyContentLayoutToExtent(layout ContentLayoutVM, extent ContentExtent, rect Rect) ContentExtent {
	if !extent.Known || !layout.Known {
		return extent
	}
	if layout.Mode == "fit" {
		extent.X = 0
		extent.Y = 0
		extent.Cols = maxInt(0, rect.W)
		extent.Rows = maxInt(0, rect.H)
	} else {
		alignX := layout.AlignX
		alignY := layout.AlignY
		if layout.Mode == "center" {
			alignX = "center"
			alignY = "center"
		}
		extent.X = alignedContentOrigin(alignX, rect.W, extent.Cols)
		extent.Y = alignedContentOrigin(alignY, rect.H, extent.Rows)
	}
	extent.X -= layout.PanX
	extent.Y -= layout.PanY
	return extent
}

func alignedContentOrigin(align string, viewport int, size int) int {
	switch align {
	case "center", "base":
		return centeredContentOrigin(viewport - size)
	case "end":
		return viewport - size
	default:
		return 0
	}
}

func centeredContentOrigin(delta int) int {
	if delta >= 0 {
		return (delta + 1) / 2
	}
	return -((-delta + 1) / 2)
}

func contentViewportMarksOutsideExtent(content ContentVM) bool {
	// 只有真实 live surface 才展示 pane/terminal 尺寸差异；pending、空态和错误 fallback 保持降噪。
	return content.Kind == ContentTerminalLive && content.Extent.Known && !content.Pending && !content.Empty && content.Error == ""
}

func contentViewportCanUseDirectRows(extent ContentExtent, rect Rect, markOutsideExtent bool) bool {
	return !markOutsideExtent &&
		extent.Known &&
		extent.X == 0 &&
		extent.Y == 0 &&
		extent.Cols == rect.W &&
		extent.Rows == rect.H
}

func contentViewportDirectRows(lines []Line, width int, height int) []Line {
	rendered := make([]Line, height)
	for row := 0; row < height; row++ {
		if row >= len(lines) {
			rendered[row] = Line{Cells: []Cell{contentViewportBlankRun(width)}}
			continue
		}
		rendered[row] = contentViewportFitLine(lines[row], width)
	}
	return rendered
}

func contentViewportFitLine(line Line, width int) Line {
	if width <= 0 {
		return Line{}
	}
	lineWidth := line.Width()
	if lineWidth == width {
		return line
	}
	if lineWidth > width {
		return contentViewportLineWindow(line, 0, width)
	}
	cells := make([]Cell, 0, len(line.Cells)+1)
	cells = append(cells, line.Cells...)
	cells = append(cells, contentViewportBlankRun(width-lineWidth))
	return Line{Cells: cells}
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

func renderContentViewportRow(lines []Line, extent ContentExtent, width int, row int, markOutsideExtent bool) Line {
	cells := make([]Cell, 0, width)
	if width <= 0 {
		return Line{}
	}
	for col := 0; col < width; {
		if !contentViewportInsideExtent(extent, col, row) {
			cells = append(cells, contentViewportOutsideExtentCell(markOutsideExtent))
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
		blankWidth := minInt(width, outWidth+visibleWidth) - outWidth
		if blankWidth > 0 {
			cells = append(cells, contentViewportBlankRun(blankWidth))
			outWidth += blankWidth
		}
	}
	if outWidth < width {
		cells = append(cells, contentViewportBlankRun(width-outWidth))
	}
	return Line{Cells: cells}
}

func contentViewportBlankRun(width int) Cell {
	if width <= 0 {
		return Cell{}
	}
	return Cell{Text: strings.Repeat(" ", width), Width: width, Safe: true}
}

func contentViewportBlankCell() Cell {
	return Cell{Text: " ", Width: 1, Safe: true}
}

func contentViewportOutsideExtentCell(markOutsideExtent bool) Cell {
	if markOutsideExtent {
		return Cell{Text: "·", Width: 1, Safe: true}
	}
	return contentViewportBlankCell()
}
