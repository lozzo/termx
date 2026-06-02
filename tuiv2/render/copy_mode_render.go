package render

import (
	"strconv"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func copyModeTimestampLabel(snapshot *protocol.Snapshot, row int) string {
	ts := snapshotRowTimestamp(snapshot, row)
	if ts.IsZero() {
		return ""
	}
	return formatSnapshotRowTimestamp(ts)
}

func copyModeRowPositionLabel(snapshot *protocol.Snapshot, logicalLine, fallbackRow int) string {
	totalLines := snapshotLogicalLineCount(snapshot)
	if totalLines <= 0 {
		return ""
	}
	if logicalLine < 0 {
		logicalLine = snapshotLogicalLineIndex(snapshot, fallbackRow)
	}
	if logicalLine < 0 || logicalLine >= totalLines {
		return ""
	}
	return strconv.Itoa(logicalLine+1) + "/" + strconv.Itoa(totalLines)
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

func snapshotRowWrapped(snapshot *protocol.Snapshot, row int) bool {
	if snapshot == nil || row < 0 {
		return false
	}
	if row < len(snapshot.Scrollback) {
		return row < len(snapshot.ScrollbackWrapped) && snapshot.ScrollbackWrapped[row]
	}
	row -= len(snapshot.Scrollback)
	if row < 0 || row >= len(snapshot.Screen.Cells) {
		return false
	}
	return row < len(snapshot.ScreenWrapped) && snapshot.ScreenWrapped[row]
}

func snapshotLogicalLineCount(snapshot *protocol.Snapshot) int {
	totalRows := snapshotTotalRows(snapshot)
	if totalRows <= 0 {
		return 0
	}
	count := 0
	for row := 0; row < totalRows; row++ {
		if row == 0 || !snapshotRowWrapped(snapshot, row-1) {
			count++
		}
	}
	return count
}

func snapshotLogicalLineIndex(snapshot *protocol.Snapshot, row int) int {
	totalRows := snapshotTotalRows(snapshot)
	if totalRows <= 0 || row < 0 || row >= totalRows {
		return -1
	}
	index := -1
	for current := 0; current <= row; current++ {
		if current == 0 || !snapshotRowWrapped(snapshot, current-1) {
			index++
		}
	}
	return index
}

func snapshotLogicalLineBounds(snapshot *protocol.Snapshot, logicalLine int) (int, int, bool) {
	totalRows := snapshotTotalRows(snapshot)
	if totalRows <= 0 || logicalLine < 0 {
		return 0, 0, false
	}
	index := -1
	for row := 0; row < totalRows; row++ {
		if row == 0 || !snapshotRowWrapped(snapshot, row-1) {
			index++
		}
		if index != logicalLine {
			continue
		}
		end := row
		for end < totalRows-1 && snapshotRowWrapped(snapshot, end) {
			end++
		}
		return row, end, true
	}
	return 0, 0, false
}

func snapshotPointForLogicalPos(snapshot *protocol.Snapshot, logicalLine, logicalCol int) (int, int, bool) {
	start, end, ok := snapshotLogicalLineBounds(snapshot, logicalLine)
	if !ok {
		return 0, 0, false
	}
	offset := 0
	lastRow, lastCol := start, 0
	for row := start; row <= end; row++ {
		cells := snapshotRow(snapshot, row)
		for col, cell := range cells {
			if cell.Content == "" && cell.Width == 0 {
				continue
			}
			lastRow, lastCol = row, col
			if offset >= logicalCol {
				return row, col, true
			}
			offset++
		}
	}
	row, col := clampCopyPoint(snapshot, lastRow, lastCol)
	return row, col, true
}

func drawCopyModeOverlay(canvas *composedCanvas, rect workbench.Rect, copyMode RenderCopyModeVM, snapshot *protocol.Snapshot, theme uiTheme, contentOffsetX, contentOffsetY int) {
	if canvas == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	totalRows := copyModeTotalRows(copyMode, snapshot)
	if totalRows <= 0 {
		return
	}
	cursorRow := copyMode.CursorRow
	cursorCol := copyMode.CursorCol
	if row, col, ok := copyModePointForLogicalPos(copyMode, snapshot, copyMode.CursorLogicalLine, copyMode.CursorLogicalCol); ok {
		cursorRow, cursorCol = row, col
	} else {
		cursorRow, cursorCol = clampCopyPointForMode(copyMode, snapshot, cursorRow, cursorCol)
	}
	selectionStartRow, selectionStartCol := copyMode.MarkRow, copyMode.MarkCol
	selectionEndRow, selectionEndCol := cursorRow, cursorCol
	if copyMode.MarkSet {
		if row, col, ok := copyModePointForLogicalPos(copyMode, snapshot, copyMode.MarkLogicalLine, copyMode.MarkLogicalCol); ok {
			selectionStartRow, selectionStartCol = row, col
		} else {
			selectionStartRow, selectionStartCol = clampCopyPointForMode(copyMode, snapshot, selectionStartRow, selectionStartCol)
		}
		if row, col, ok := copyModePointForLogicalPos(copyMode, snapshot, copyMode.CursorLogicalLine, copyMode.CursorLogicalCol); ok {
			selectionEndRow, selectionEndCol = row, col
		} else {
			selectionEndRow, selectionEndCol = clampCopyPointForMode(copyMode, snapshot, selectionEndRow, selectionEndCol)
		}
		if selectionStartRow > selectionEndRow || (selectionStartRow == selectionEndRow && selectionStartCol > selectionEndCol) {
			selectionStartRow, selectionEndRow = selectionEndRow, selectionStartRow
			selectionStartCol, selectionEndCol = selectionEndCol, selectionStartCol
		}
	}
	start := clampCopyViewportTopForMode(copyMode, snapshot, rect.H, copyMode.ViewTopRow)
	selectionBG := ensureContrast(mixHex(theme.info, theme.chromeAccent, 0.35), theme.hostBG, 1.2)
	cursorBG := ensureContrast(theme.warning, theme.hostBG, 1.2)
	for visibleRow := 0; visibleRow < rect.H; visibleRow++ {
		rowIndex := start + visibleRow
		if rowIndex < 0 || rowIndex >= totalRows {
			continue
		}
		if copyMode.MarkSet && rowIndex >= selectionStartRow && rowIndex <= selectionEndRow {
			firstCol := 0
			lastCol := rowMaxColForMode(copyMode, snapshot, rowIndex)
			if rowIndex == selectionStartRow {
				firstCol = selectionStartCol
			}
			if rowIndex == selectionEndRow {
				lastCol = selectionEndCol
			}
			for col := firstCol; col <= lastCol; col++ {
				drawCopyModeCellHighlight(canvas, rect.X+contentOffsetX+col, rect.Y+contentOffsetY+visibleRow, selectionBG)
			}
		}
	}
	screenRow := cursorRow - start
	if screenRow >= 0 && screenRow < rect.H {
		drawCopyModeCellHighlight(canvas, rect.X+contentOffsetX+cursorCol, rect.Y+contentOffsetY+screenRow, cursorBG)
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
	if viewTopRow < len(snapshot.Scrollback) && offset == 0 && totalRows <= maxInt(1, height) {
		offset = 1
	}
	return offset
}

func scrollOffsetForCopyMode(copyMode RenderCopyModeVM, snapshot *protocol.Snapshot, height, viewTopRow int) int {
	if copyMode.Projection == nil {
		return scrollOffsetForViewportTop(snapshot, height, viewTopRow)
	}
	totalRows := len(copyMode.Projection.Rows)
	viewTopRow = clampCopyViewportTopForMode(copyMode, snapshot, height, viewTopRow)
	offset := totalRows - (viewTopRow + maxInt(1, height))
	if offset < 0 {
		return 0
	}
	return offset
}

func snapshotRow(snapshot *protocol.Snapshot, rowIndex int) []protocol.Cell {
	if snapshot == nil || rowIndex < 0 {
		return nil
	}
	if rowIndex < len(snapshot.Scrollback) {
		return snapshot.Scrollback[rowIndex].DecodeCells()
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

func rowMaxColForMode(copyMode RenderCopyModeVM, snapshot *protocol.Snapshot, rowIndex int) int {
	row := copyModeRowCells(copyMode, snapshot, rowIndex)
	if len(row) > 0 {
		return len(row) - 1
	}
	if copyMode.Projection != nil {
		if copyMode.Projection.Size.Cols == 0 {
			return 0
		}
		return int(copyMode.Projection.Size.Cols) - 1
	}
	return rowMaxCol(snapshot, rowIndex)
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

func clampCopyViewportTopForMode(copyMode RenderCopyModeVM, snapshot *protocol.Snapshot, height, viewTopRow int) int {
	if copyMode.Projection == nil {
		return clampCopyViewportTop(snapshot, height, viewTopRow)
	}
	totalRows := len(copyMode.Projection.Rows)
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

func clampCopyPointForMode(copyMode RenderCopyModeVM, snapshot *protocol.Snapshot, row, col int) (int, int) {
	if copyMode.Projection == nil {
		return clampCopyPoint(snapshot, row, col)
	}
	totalRows := len(copyMode.Projection.Rows)
	if totalRows <= 0 {
		return 0, 0
	}
	if row < 0 {
		row = 0
	}
	if row >= totalRows {
		row = totalRows - 1
	}
	maxCol := rowMaxColForMode(copyMode, snapshot, row)
	if col < 0 {
		col = 0
	}
	if col > maxCol {
		col = maxCol
	}
	rowCells := copyModeRowCells(copyMode, snapshot, row)
	for col > 0 && col < len(rowCells) && rowCells[col].Content == "" && rowCells[col].Width == 0 {
		col--
	}
	return row, col
}
