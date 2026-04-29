package render

import (
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func fillRect(canvas *composedCanvas, rect workbench.Rect, cell drawCell) {
	if canvas == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	rect, ok := clipRectToViewport(rect, canvas.width, canvas.height)
	if !ok {
		return
	}
	if cell == blankDrawCell() {
		blankRow := cachedBlankFillRow(rect.W)
		if canvas.currentOwner != 0 {
			blankRow = make([]drawCell, rect.W)
			for i := range blankRow {
				blankRow[i] = canvas.blankCell()
			}
		}
		for y := rect.Y; y < rect.Y+rect.H; y++ {
			clearBlankFillBoundaryFootprint(canvas, rect.X, y)
			if rect.W > 1 {
				clearBlankFillBoundaryFootprint(canvas, rect.X+rect.W-1, y)
			}
			copy(canvas.cells[y][rect.X:rect.X+rect.W], blankRow)
			canvas.markRowDirtyRange(y, rect.X, rect.X+rect.W-1)
		}
		return
	}
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		for x := rect.X; x < rect.X+rect.W; x++ {
			canvas.set(x, y, cell)
		}
	}
}

func clearBlankFillBoundaryFootprint(canvas *composedCanvas, x, y int) {
	if canvas == nil || x < 0 || y < 0 || x >= canvas.width || y >= canvas.height {
		return
	}
	cell := canvas.cells[y][x]
	// When a blank fill starts or ends in the middle of a wide-cell footprint,
	// clear the entire footprint first so we do not leave a stale lead or
	// continuation cell straddling the fill boundary.
	if !cell.Continuation && canvas.cellFootprintWidth(x, y) <= 1 {
		return
	}
	canvas.clearOverlappingCellFootprints(x, y, 1)
}

func projectPaneCursor(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, scrollOffset int) {
	projectPaneCursorSource(canvas, rect, renderSource(snapshot, nil), scrollOffset)
}

func projectPaneCursorSource(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, scrollOffset int) {
	if canvas == nil || source == nil || !source.Cursor().Visible || scrollOffset > 0 {
		return
	}
	cursor := source.Cursor()
	x := rect.X + cursor.Col
	y := rect.Y + cursor.Row
	if x < rect.X || y < rect.Y || x >= rect.X+rect.W || y >= rect.Y+rect.H {
		return
	}
	drawSyntheticCursor(canvas, x, y, cursor)
}

func drawSyntheticCursor(canvas *composedCanvas, x, y int, cursor protocol.CursorState) {
	if canvas == nil || y < 0 || y >= canvas.height || x < 0 || x >= canvas.width {
		return
	}
	if canvas.syntheticCursorVisibleFn != nil && !canvas.syntheticCursorVisibleFn(cursor) {
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
	if cell.Content == "" || !cell.TerminalContent {
		cell = blankDrawCell()
	}
	style := cell.Style
	style.Reverse = false
	style.FG = "#000000"
	style.BG = "#ffffff"
	switch cursor.Shape {
	case "underline":
		style.Underline = true
	case "bar":
		style.Bold = true
	}
	cell.Style = style
	canvas.set(leadX, y, cell)
}
