package render

import (
	"strings"
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
	localvterm "github.com/lozzow/termx/termx-core/vterm"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func applyScrollbackOffset(snapshot *protocol.Snapshot, offset int, height int) *protocol.Snapshot {
	if snapshot == nil || offset <= 0 || height <= 0 {
		return snapshot
	}
	rows := make([][]protocol.Cell, 0, len(snapshot.Scrollback)+len(snapshot.Screen.Cells))
	rows = append(rows, snapshot.Scrollback...)
	rows = append(rows, snapshot.Screen.Cells...)
	if len(rows) == 0 {
		return snapshot
	}
	end := len(rows) - offset
	if end < 0 {
		end = 0
	}
	start := end - height
	if start < 0 {
		start = 0
	}
	window := rows[start:end]
	cells := make([][]protocol.Cell, 0, len(window))
	for _, row := range window {
		cells = append(cells, append([]protocol.Cell(nil), row...))
	}
	cloned := *snapshot
	cloned.Screen = protocol.ScreenData{
		Cells:             cells,
		IsAlternateScreen: snapshot.Screen.IsAlternateScreen,
	}
	return &cloned
}

func drawSnapshotWithOffset(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, offset int, theme uiTheme) {
	drawTerminalSourceWithOffset(canvas, rect, renderSource(snapshot, nil), offset, theme)
}

func drawTerminalSourceWithOffset(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, offset int, theme uiTheme) {
	drawTerminalSourceWithOffsetAndMetrics(canvas, rect, source, offset, theme, terminalVisibleMetricsForSource(source))
}

func drawTerminalSourceWithOffsetAndMetrics(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, offset int, theme uiTheme, metrics renderTerminalMetrics) {
	drawTerminalSourceWithPlacementAndMetrics(canvas, rect, source, offset, 0, 0, theme, metrics)
}

func drawTerminalSourceWithPlacementAndMetrics(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, offset, contentOffsetX, contentOffsetY int, theme uiTheme, metrics renderTerminalMetrics) {
	if canvas == nil || source == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	contentOffsetX = clampTerminalContentOffset(contentOffsetX, rect.W, metrics.Cols)
	contentOffsetY = clampTerminalContentOffset(contentOffsetY, rect.H, metrics.Rows)
	if offset <= 0 {
		drawTerminalSourceInRectWithPlacement(canvas, rect, source, contentOffsetX, contentOffsetY)
		drawTerminalExtentHintsWithMetricsAndPlacement(canvas, rect, source, theme, metrics, contentOffsetX, contentOffsetY)
		return
	}
	totalRows := source.TotalRows()
	if totalRows == 0 {
		drawTerminalExtentHintsWithMetricsAndPlacement(canvas, rect, source, theme, metrics, contentOffsetX, contentOffsetY)
		return
	}
	for line := 0; line < rect.H; line++ {
		rowIndex := terminalSourceWindowRowIndexWithPlacement(source, rect.H, offset, line, contentOffsetY)
		if rowIndex < 0 {
			continue
		}
		drawTerminalSourceRowInRectWithPlacement(canvas, rect, source, rowIndex, rect.Y+line, contentOffsetX, theme)
	}
	hintMetrics := terminalWindowHintMetrics(source, rect, offset, metrics)
	if rect.W > hintMetrics.Cols {
		hintMetrics.Cols = rect.W
	}
	drawTerminalExtentHintsWithMetricsAndPlacement(canvas, rect, terminalExtentHintsView(source, totalRows), theme, hintMetrics, contentOffsetX, contentOffsetY)
}

func drawTerminalSourceWindowRowsWithMetrics(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, offset int, theme uiTheme, metrics renderTerminalMetrics, lines []int) {
	drawTerminalSourceWindowRowsWithPlacementAndMetrics(canvas, rect, source, offset, 0, 0, theme, metrics, lines)
}

func drawTerminalSourceWindowRowsWithPlacementAndMetrics(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, offset, contentOffsetX, contentOffsetY int, theme uiTheme, metrics renderTerminalMetrics, lines []int) {
	if canvas == nil || source == nil || rect.W <= 0 || rect.H <= 0 || len(lines) == 0 {
		return
	}
	contentOffsetX = clampTerminalContentOffset(contentOffsetX, rect.W, metrics.Cols)
	contentOffsetY = clampTerminalContentOffset(contentOffsetY, rect.H, metrics.Rows)
	hintMetrics := terminalWindowHintMetrics(source, rect, offset, metrics)
	for _, line := range lines {
		if line < 0 || line >= rect.H {
			continue
		}
		targetY := rect.Y + line
		fillRect(canvas, workbench.Rect{X: rect.X, Y: targetY, W: rect.W, H: 1}, blankDrawCell())
		if rowIndex := terminalSourceWindowRowIndexWithPlacement(source, rect.H, offset, line, contentOffsetY); rowIndex >= 0 {
			drawTerminalSourceRowInRectWithPlacement(canvas, rect, source, rowIndex, targetY, contentOffsetX, theme)
		}
		drawTerminalExtentHintsRowWithMetricsAndPlacement(canvas, rect, source, targetY, theme, hintMetrics, contentOffsetX, contentOffsetY)
	}
}

func terminalWindowHintMetrics(source terminalRenderSource, rect workbench.Rect, offset int, metrics renderTerminalMetrics) renderTerminalMetrics {
	if source == nil {
		return metrics
	}
	if offset <= 0 {
		return metrics
	}
	totalRows := source.TotalRows()
	end := totalRows - offset
	if end < 0 {
		end = 0
	}
	start := end - rect.H
	if start < 0 {
		start = 0
	}
	drawnRows := end - start
	if drawnRows > metrics.Rows {
		metrics.Rows = drawnRows
	}
	if rect.W > metrics.Cols {
		metrics.Cols = rect.W
	}
	return metrics
}

func terminalSourceWindowRowIndexWithPlacement(source terminalRenderSource, height, offset, line, contentOffsetY int) int {
	if source == nil || height <= 0 || line < 0 || line >= height {
		return -1
	}
	contentLine := line - contentOffsetY
	if contentLine < 0 || contentLine >= height {
		return -1
	}
	return terminalSourceWindowRowIndex(source, height, offset, contentLine)
}

func drawTerminalSourceInRect(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource) {
	drawTerminalSourceInRectWithPlacement(canvas, rect, source, 0, 0)
}

func drawTerminalSourceInRectWithPlacement(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, offsetX, offsetY int) {
	if canvas == nil || source == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	base := source.ScrollbackRows()
	if cellSource, ok := source.(terminalCellRowSource); ok {
		for y := 0; y < source.ScreenRows(); y++ {
			targetY := rect.Y + offsetY + y
			if targetY < rect.Y || targetY >= rect.Y+rect.H {
				continue
			}
			if row := cellSource.RowView(base + y); row != nil {
				drawVTermRowInRectWithPlacement(canvas, rect, targetY, row, offsetX)
				continue
			}
			drawProtocolRowInRectWithPlacement(canvas, rect, targetY, source.Row(base+y), offsetX)
		}
		return
	}
	for y := 0; y < source.ScreenRows(); y++ {
		targetY := rect.Y + offsetY + y
		if targetY < rect.Y || targetY >= rect.Y+rect.H {
			continue
		}
		drawProtocolRowInRectWithPlacement(canvas, rect, targetY, source.Row(base+y), offsetX)
	}
}

func drawTerminalSourceRowInRect(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, rowIndex int, targetY int, theme uiTheme) {
	drawTerminalSourceRowInRectWithPlacement(canvas, rect, source, rowIndex, targetY, 0, theme)
}

func drawTerminalSourceRowInRectWithPlacement(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, rowIndex int, targetY int, offsetX int, theme uiTheme) {
	if source == nil {
		return
	}
	if targetY < rect.Y || targetY >= rect.Y+rect.H {
		return
	}
	if kind := source.RowKind(rowIndex); kind != "" {
		if drawSnapshotMarkerRow(canvas, rect, targetY, kind, source.RowTimestamp(rowIndex), theme) {
			return
		}
	}
	drawProtocolRowInRectWithPlacement(canvas, rect, targetY, source.Row(rowIndex), offsetX)
}

func drawTerminalSourceRowInRectCleared(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, rowIndex int, targetY int, theme uiTheme) {
	if source == nil {
		return
	}
	if kind := source.RowKind(rowIndex); kind != "" {
		if drawSnapshotMarkerRow(canvas, rect, targetY, kind, source.RowTimestamp(rowIndex), theme) {
			return
		}
	}
	if cellSource, ok := source.(terminalCellRowSource); ok {
		if row := cellSource.RowView(rowIndex); row != nil {
			canvas.drawVTermRowInRectCleared(rect, targetY, row)
			return
		}
	}
	canvas.drawProtocolRowInRectCleared(rect, targetY, source.Row(rowIndex))
}

func drawTerminalExtentHintsRowWithMetrics(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, targetY int, theme uiTheme, metrics renderTerminalMetrics) {
	drawTerminalExtentHintsRowWithMetricsAndPlacement(canvas, rect, source, targetY, theme, metrics, 0, 0)
}

func drawTerminalExtentHintsRowWithMetricsAndPlacement(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, targetY int, theme uiTheme, metrics renderTerminalMetrics, offsetX, offsetY int) {
	if canvas == nil || source == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	if targetY < rect.Y || targetY >= rect.Y+rect.H {
		return
	}
	if metrics.Cols <= 0 || metrics.Rows <= 0 {
		return
	}

	dotStyle := drawStyle{FG: theme.panelBorder}
	contentLeft := rect.X + offsetX
	contentTop := rect.Y + offsetY
	contentRight := contentLeft + metrics.Cols
	contentBottom := contentTop + metrics.Rows

	if targetY < contentTop || targetY >= contentBottom {
		for x := rect.X; x < rect.X+rect.W; x++ {
			canvas.set(x, targetY, drawCell{Content: "·", Width: 1, Style: dotStyle})
		}
		return
	}
	for x := rect.X; x < rect.X+rect.W; x++ {
		if x >= contentLeft && x < contentRight {
			continue
		}
		canvas.set(x, targetY, drawCell{Content: "·", Width: 1, Style: dotStyle})
	}
}

func drawSnapshotMarkerRow(canvas *composedCanvas, rect workbench.Rect, targetY int, kind string, ts time.Time, theme uiTheme) bool {
	if canvas == nil || rect.W <= 0 {
		return false
	}
	label := snapshotMarkerLabel(kind, ts)
	if strings.TrimSpace(label) == "" {
		return false
	}
	canvas.drawText(rect.X, targetY, centerText(label, rect.W), drawStyle{FG: theme.panelMuted})
	return true
}

func snapshotMarkerLabel(kind string, ts time.Time) string {
	switch kind {
	case protocol.SnapshotRowKindRestart:
		label := "[ restarted ]"
		if formatted := formatSnapshotRowTimestamp(ts); formatted != "" {
			label = "[ restarted " + formatted + " ]"
		}
		return label
	default:
		return ""
	}
}

func snapshotExtentHintsView(snapshot *protocol.Snapshot, rows int) *protocol.Snapshot {
	if snapshot == nil || rows <= 0 {
		return snapshot
	}
	if int(snapshot.Size.Rows) >= rows {
		return snapshot
	}
	cloned := *snapshot
	if rows > int(^uint16(0)) {
		rows = int(^uint16(0))
	}
	cloned.Size.Rows = uint16(rows)
	return &cloned
}

func terminalExtentHintsView(source terminalRenderSource, rows int) terminalRenderSource {
	if source == nil || rows <= 0 {
		return source
	}
	if size := source.Size(); int(size.Rows) >= rows {
		return source
	}
	switch typed := source.(type) {
	case snapshotRenderSource:
		return renderSource(snapshotExtentHintsView(typed.snapshot, rows), nil)
	default:
		return source
	}
}

func drawTerminalExtentHints(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, theme uiTheme) {
	drawTerminalExtentHintsWithMetrics(canvas, rect, source, theme, terminalVisibleMetricsForSource(source))
}

func drawTerminalExtentHintsWithMetrics(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, theme uiTheme, metrics renderTerminalMetrics) {
	drawTerminalExtentHintsWithMetricsAndPlacement(canvas, rect, source, theme, metrics, 0, 0)
}

func drawTerminalExtentHintsWithMetricsAndPlacement(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, theme uiTheme, metrics renderTerminalMetrics, offsetX, offsetY int) {
	if canvas == nil || source == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	if metrics.Cols <= 0 || metrics.Rows <= 0 {
		return
	}

	dotStyle := drawStyle{FG: theme.panelBorder}
	contentLeft := rect.X + offsetX
	contentTop := rect.Y + offsetY
	contentRight := contentLeft + metrics.Cols
	contentBottom := contentTop + metrics.Rows

	for y := rect.Y; y < rect.Y+rect.H; y++ {
		for x := rect.X; x < rect.X+rect.W; x++ {
			if x >= contentLeft && x < contentRight && y >= contentTop && y < contentBottom {
				continue
			}
			canvas.set(x, y, drawCell{Content: "·", Width: 1, Style: dotStyle})
		}
	}
}

func clampTerminalContentOffset(offset, viewportSize, contentSize int) int {
	if viewportSize <= 0 || contentSize <= 0 {
		return 0
	}
	minOffset := minInt(0, viewportSize-contentSize)
	maxOffset := maxInt(0, viewportSize-contentSize)
	if offset < minOffset {
		return minOffset
	}
	if offset > maxOffset {
		return maxOffset
	}
	return offset
}

func drawProtocolRowInRectWithPlacement(canvas *composedCanvas, rect workbench.Rect, targetY int, row []protocol.Cell, offsetX int) {
	if canvas == nil || rect.W <= 0 || targetY < 0 || targetY >= canvas.height {
		return
	}
	for col, index := 0, 0; index < len(row); index++ {
		cell := drawCellFromProtocolCell(row[index])
		if cell.Continuation {
			continue
		}
		if cell.Content == "" {
			cell.Content = " "
			cell.Width = 1
		}
		targetX := rect.X + offsetX + col
		if targetX >= rect.X && targetX+cell.Width <= rect.X+rect.W {
			canvas.set(targetX, targetY, cell)
			canvas.materializeRawAmbiguousContinuation(targetX, targetY, cell)
		}
		col += maxInt(1, cell.Width)
	}
}

func drawVTermRowInRectWithPlacement(canvas *composedCanvas, rect workbench.Rect, targetY int, row []localvterm.Cell, offsetX int) {
	if canvas == nil || rect.W <= 0 || targetY < 0 || targetY >= canvas.height {
		return
	}
	for col, index := 0, 0; index < len(row); index++ {
		cell := drawCellFromVTermCell(row[index])
		if cell.Continuation {
			continue
		}
		if cell.Content == "" {
			cell.Content = " "
			cell.Width = 1
		}
		targetX := rect.X + offsetX + col
		if targetX >= rect.X && targetX+cell.Width <= rect.X+rect.W {
			canvas.set(targetX, targetY, cell)
			canvas.materializeRawAmbiguousContinuation(targetX, targetY, cell)
		}
		col += maxInt(1, cell.Width)
	}
}
