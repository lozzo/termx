package render

import (
	"strconv"
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func copyModeTimestampLabel(snapshot *protocol.Snapshot, row int) string {
	ts := snapshotRowTimestamp(snapshot, row)
	if ts.IsZero() {
		return ""
	}
	return formatSnapshotRowTimestamp(ts)
}

func copyModeRowPositionLabel(snapshot *protocol.Snapshot, row int) string {
	totalRows := snapshotTotalRows(snapshot)
	if totalRows <= 0 || row < 0 || row >= totalRows {
		return ""
	}
	return strconv.Itoa(row+1) + "/" + strconv.Itoa(totalRows)
}

func formatSnapshotRowTimestamp(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Local().Format("2006-01-02 15:04:05")
}

func snapshotRowTimestamp(snapshot *protocol.Snapshot, row int) time.Time {
	if snapshot == nil || row < 0 {
		return time.Time{}
	}
	if row < len(snapshot.Scrollback) {
		if row < len(snapshot.ScrollbackTimestamps) {
			return snapshot.ScrollbackTimestamps[row]
		}
		return time.Time{}
	}
	row -= len(snapshot.Scrollback)
	if row < 0 || row >= len(snapshot.Screen.Cells) {
		return time.Time{}
	}
	if row < len(snapshot.ScreenTimestamps) {
		return snapshot.ScreenTimestamps[row]
	}
	return time.Time{}
}

func snapshotRowKind(snapshot *protocol.Snapshot, row int) string {
	if snapshot == nil || row < 0 {
		return ""
	}
	if row < len(snapshot.Scrollback) {
		if row < len(snapshot.ScrollbackRowKinds) {
			return snapshot.ScrollbackRowKinds[row]
		}
		return ""
	}
	row -= len(snapshot.Scrollback)
	if row < 0 || row >= len(snapshot.Screen.Cells) {
		return ""
	}
	if row < len(snapshot.ScreenRowKinds) {
		return snapshot.ScreenRowKinds[row]
	}
	return ""
}

func snapshotTotalRows(snapshot *protocol.Snapshot) int {
	if snapshot == nil {
		return 0
	}
	return len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
}

func drawCopyModeOverlay(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, theme uiTheme, cursorRow, cursorCol, viewTopRow int, markSet bool, markRow, markCol int) {
	if canvas == nil || snapshot == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	totalRows := len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
	if totalRows <= 0 {
		return
	}
	cursorRow, cursorCol = clampCopyPoint(snapshot, cursorRow, cursorCol)
	selectionStartRow, selectionStartCol := markRow, markCol
	selectionEndRow, selectionEndCol := cursorRow, cursorCol
	if markSet {
		selectionStartRow, selectionStartCol = clampCopyPoint(snapshot, selectionStartRow, selectionStartCol)
		selectionEndRow, selectionEndCol = clampCopyPoint(snapshot, selectionEndRow, selectionEndCol)
		if selectionStartRow > selectionEndRow || (selectionStartRow == selectionEndRow && selectionStartCol > selectionEndCol) {
			selectionStartRow, selectionEndRow = selectionEndRow, selectionStartRow
			selectionStartCol, selectionEndCol = selectionEndCol, selectionStartCol
		}
	}
	start := clampCopyViewportTop(snapshot, rect.H, viewTopRow)
	selectionBG := ensureContrast(mixHex(theme.info, theme.chromeAccent, 0.35), theme.hostBG, 1.2)
	cursorBG := ensureContrast(theme.warning, theme.hostBG, 1.2)
	for visibleRow := 0; visibleRow < rect.H; visibleRow++ {
		rowIndex := start + visibleRow
		if rowIndex < 0 || rowIndex >= totalRows {
			continue
		}
		if markSet && rowIndex >= selectionStartRow && rowIndex <= selectionEndRow {
			firstCol := 0
			lastCol := rowMaxCol(snapshot, rowIndex)
			if rowIndex == selectionStartRow {
				firstCol = selectionStartCol
			}
			if rowIndex == selectionEndRow {
				lastCol = selectionEndCol
			}
			for col := firstCol; col <= lastCol; col++ {
				drawCopyModeCellHighlight(canvas, rect.X+col, rect.Y+visibleRow, selectionBG)
			}
		}
	}
	screenRow := cursorRow - start
	if screenRow >= 0 && screenRow < rect.H {
		drawCopyModeCellHighlight(canvas, rect.X+cursorCol, rect.Y+screenRow, cursorBG)
	}
}

func drawCopyModeCellHighlight(canvas *composedCanvas, x, y int, bg string) {
	if canvas == nil || x < 0 || y < 0 || x >= canvas.width || y >= canvas.height {
		return
	}
	leadX := x
	for leadX > 0 && canvas.cells[y][leadX].Continuation {
		leadX--
	}
	cell := canvas.cells[y][leadX]
	if cell.Continuation {
		cell = blankDrawCell()
	}
	if cell.Content == "" {
		cell = blankDrawCell()
	}
	style := cell.Style
	style.BG = bg
	style.FG = contrastTextColor(bg)
	cell.Style = style
	canvas.set(leadX, y, cell)
}

func clampCopyViewportTop(snapshot *protocol.Snapshot, height, viewTopRow int) int {
	totalRows := len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
	if totalRows <= 0 {
		return 0
	}
	maxTop := maxInt(0, totalRows-maxInt(1, height))
	if viewTopRow < 0 {
		viewTopRow = 0
	}
	if viewTopRow > maxTop {
		viewTopRow = maxTop
	}
	return viewTopRow
}

func scrollOffsetForViewportTop(snapshot *protocol.Snapshot, height, viewTopRow int) int {
	if snapshot == nil {
		return 0
	}
	totalRows := len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
	viewTopRow = clampCopyViewportTop(snapshot, height, viewTopRow)
	offset := totalRows - (viewTopRow + maxInt(1, height))
	if offset < 0 {
		offset = 0
	}
	if viewTopRow < len(snapshot.Scrollback) && offset == 0 && len(snapshot.Scrollback) > 0 {
		offset = 1
	}
	return offset
}

func snapshotRow(snapshot *protocol.Snapshot, rowIndex int) []protocol.Cell {
	if snapshot == nil || rowIndex < 0 {
		return nil
	}
	if rowIndex < len(snapshot.Scrollback) {
		return snapshot.Scrollback[rowIndex]
	}
	rowIndex -= len(snapshot.Scrollback)
	if rowIndex < 0 || rowIndex >= len(snapshot.Screen.Cells) {
		return nil
	}
	return snapshot.Screen.Cells[rowIndex]
}

func rowMaxCol(snapshot *protocol.Snapshot, rowIndex int) int {
	row := snapshotRow(snapshot, rowIndex)
	if len(row) > 0 {
		return len(row) - 1
	}
	if snapshot == nil || snapshot.Size.Cols == 0 {
		return 0
	}
	return int(snapshot.Size.Cols) - 1
}

func clampCopyPoint(snapshot *protocol.Snapshot, row, col int) (int, int) {
	totalRows := len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
	if totalRows <= 0 {
		return 0, 0
	}
	if row < 0 {
		row = 0
	}
	if row >= totalRows {
		row = totalRows - 1
	}
	maxCol := rowMaxCol(snapshot, row)
	if col < 0 {
		col = 0
	}
	if col > maxCol {
		col = maxCol
	}
	rowCells := snapshotRow(snapshot, row)
	for col > 0 && col < len(rowCells) && rowCells[col].Content == "" && rowCells[col].Width == 0 {
		col--
	}
	return row, col
}
