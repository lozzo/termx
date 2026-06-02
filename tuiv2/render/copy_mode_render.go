package render

import (
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/workbench"
)

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

func drawCopyModeOverlay(canvas *composedCanvas, rect workbench.Rect, copyMode RenderCopyModeVM, theme uiTheme, contentOffsetX, contentOffsetY int) {
	if canvas == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	totalRows := copyModeTotalRows(copyMode)
	if totalRows <= 0 {
		return
	}
	cursorRow := copyMode.CursorRow
	cursorCol := copyMode.CursorCol
	if row, col, ok := copyModePointForLogicalPos(copyMode, copyMode.CursorLogicalLine, copyMode.CursorLogicalCol); ok {
		cursorRow, cursorCol = row, col
	} else {
		cursorRow, cursorCol = clampCopyPointForMode(copyMode, cursorRow, cursorCol)
	}
	selectionStartRow, selectionStartCol := copyMode.MarkRow, copyMode.MarkCol
	selectionEndRow, selectionEndCol := cursorRow, cursorCol
	if copyMode.MarkSet {
		if row, col, ok := copyModePointForLogicalPos(copyMode, copyMode.MarkLogicalLine, copyMode.MarkLogicalCol); ok {
			selectionStartRow, selectionStartCol = row, col
		} else {
			selectionStartRow, selectionStartCol = clampCopyPointForMode(copyMode, selectionStartRow, selectionStartCol)
		}
		if row, col, ok := copyModePointForLogicalPos(copyMode, copyMode.CursorLogicalLine, copyMode.CursorLogicalCol); ok {
			selectionEndRow, selectionEndCol = row, col
		} else {
			selectionEndRow, selectionEndCol = clampCopyPointForMode(copyMode, selectionEndRow, selectionEndCol)
		}
		if selectionStartRow > selectionEndRow || (selectionStartRow == selectionEndRow && selectionStartCol > selectionEndCol) {
			selectionStartRow, selectionEndRow = selectionEndRow, selectionStartRow
			selectionStartCol, selectionEndCol = selectionEndCol, selectionStartCol
		}
	}
	start := clampCopyViewportTopForMode(copyMode, rect.H, copyMode.ViewTopRow)
	selectionBG := ensureContrast(mixHex(theme.info, theme.chromeAccent, 0.35), theme.hostBG, 1.2)
	cursorBG := ensureContrast(theme.warning, theme.hostBG, 1.2)
	for visibleRow := 0; visibleRow < rect.H; visibleRow++ {
		rowIndex := start + visibleRow
		if rowIndex < 0 || rowIndex >= totalRows {
			continue
		}
		if copyMode.MarkSet && rowIndex >= selectionStartRow && rowIndex <= selectionEndRow {
			firstCol := 0
			lastCol := rowMaxColForMode(copyMode, rowIndex)
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

func scrollOffsetForCopyMode(copyMode RenderCopyModeVM, height, viewTopRow int) int {
	if copyMode.Projection == nil {
		return 0
	}
	totalRows := len(copyMode.Projection.Rows)
	viewTopRow = clampCopyViewportTopForMode(copyMode, height, viewTopRow)
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

func rowMaxColForMode(copyMode RenderCopyModeVM, rowIndex int) int {
	row := copyModeRowCells(copyMode, rowIndex)
	if len(row) > 0 {
		return len(row) - 1
	}
	if copyMode.Projection == nil {
		return 0
	}
	if copyMode.Projection.Size.Cols == 0 {
		return 0
	}
	return int(copyMode.Projection.Size.Cols) - 1
}

func clampCopyViewportTopForMode(copyMode RenderCopyModeVM, height, viewTopRow int) int {
	if copyMode.Projection == nil {
		return 0
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

func clampCopyPointForMode(copyMode RenderCopyModeVM, row, col int) (int, int) {
	if copyMode.Projection == nil {
		return 0, 0
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
	maxCol := rowMaxColForMode(copyMode, row)
	if col < 0 {
		col = 0
	}
	if col > maxCol {
		col = maxCol
	}
	rowCells := copyModeRowCells(copyMode, row)
	for col > 0 && col < len(rowCells) && rowCells[col].Content == "" && rowCells[col].Width == 0 {
		col--
	}
	return row, col
}
