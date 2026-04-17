package render

import (
	"strings"
	"time"

	"github.com/lozzow/termx/protocol"
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
	if canvas == nil || source == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	if offset <= 0 {
		drawTerminalSourceInRect(canvas, rect, source)
		drawTerminalExtentHints(canvas, rect, source, theme)
		return
	}
	totalRows := source.TotalRows()
	if totalRows == 0 {
		drawTerminalExtentHints(canvas, rect, source, theme)
		return
	}
	end := totalRows - offset
	if end < 0 {
		end = 0
	}
	start := end - rect.H
	if start < 0 {
		start = 0
	}
	targetY := rect.Y
	for rowIndex := start; rowIndex < end && targetY < rect.Y+rect.H; rowIndex++ {
		drawTerminalSourceRowInRect(canvas, rect, source, rowIndex, targetY, theme)
		targetY++
	}
	drawTerminalExtentHints(canvas, rect, terminalExtentHintsView(source, totalRows), theme)
}

func drawSnapshotRowInRect(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, rowIndex int, targetY int, theme uiTheme) {
	drawTerminalSourceRowInRect(canvas, rect, renderSource(snapshot, nil), rowIndex, targetY, theme)
}

func drawTerminalSourceInRect(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource) {
	if canvas == nil || source == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	base := source.ScrollbackRows()
	for y := 0; y < rect.H && y < source.ScreenRows(); y++ {
		canvas.drawProtocolRowInRect(rect, rect.Y+y, source.Row(base+y))
	}
}

func drawTerminalSourceRowInRect(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, rowIndex int, targetY int, theme uiTheme) {
	if source == nil {
		return
	}
	if kind := source.RowKind(rowIndex); kind != "" {
		if drawSnapshotMarkerRow(canvas, rect, targetY, kind, source.RowTimestamp(rowIndex), theme) {
			return
		}
	}
	canvas.drawProtocolRowInRect(rect, targetY, source.Row(rowIndex))
}

func drawTerminalExtentHintsRow(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, targetY int, theme uiTheme) {
	if canvas == nil || source == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	if targetY < rect.Y || targetY >= rect.Y+rect.H {
		return
	}
	metrics := terminalMetricsForSource(source)
	if metrics.Cols <= 0 || metrics.Rows <= 0 {
		return
	}

	dotStyle := drawStyle{FG: theme.panelBorder}
	visibleCols := minInt(rect.W, metrics.Cols)
	visibleRows := minInt(rect.H, metrics.Rows)

	if targetY >= rect.Y+visibleRows {
		for x := rect.X; x < rect.X+rect.W; x++ {
			canvas.set(x, targetY, drawCell{Content: "·", Width: 1, Style: dotStyle})
		}
		return
	}
	if metrics.Cols >= rect.W {
		return
	}
	for x := rect.X + visibleCols; x < rect.X+rect.W; x++ {
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

func drawSnapshotExtentHints(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, theme uiTheme) {
	drawTerminalExtentHints(canvas, rect, renderSource(snapshot, nil), theme)
}

func drawTerminalExtentHints(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, theme uiTheme) {
	if canvas == nil || source == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	metrics := terminalMetricsForSource(source)
	if metrics.Cols <= 0 || metrics.Rows <= 0 {
		return
	}

	dotStyle := drawStyle{FG: theme.panelBorder}

	visibleCols := minInt(rect.W, metrics.Cols)
	visibleRows := minInt(rect.H, metrics.Rows)

	if metrics.Cols < rect.W {
		startX := rect.X + visibleCols
		endX := rect.X + rect.W
		for y := rect.Y; y < rect.Y+visibleRows; y++ {
			for x := startX; x < endX; x++ {
				canvas.set(x, y, drawCell{Content: "·", Width: 1, Style: dotStyle})
			}
		}
	}
	if metrics.Rows < rect.H {
		startY := rect.Y + visibleRows
		endY := rect.Y + rect.H
		for y := startY; y < endY; y++ {
			for x := rect.X; x < rect.X+rect.W; x++ {
				canvas.set(x, y, drawCell{Content: "·", Width: 1, Style: dotStyle})
			}
		}
	}
}

func renderTerminalMetricsForSnapshot(snapshot *protocol.Snapshot) renderTerminalMetrics {
	return terminalMetricsForSource(renderSource(snapshot, nil))
}
