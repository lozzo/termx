package history

import (
	"fmt"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func vtermScreenOpWriteSpan() vterm.ScreenOpCode {
	return vterm.ScreenOpWriteSpan
}

func vtermScreenOpControl() vterm.ScreenOpCode {
	return vterm.ScreenOpControl
}

func vtermScreenOpClearRect() vterm.ScreenOpCode {
	return vterm.ScreenOpClearRect
}

func vtermScreenOpClearToEOL() vterm.ScreenOpCode {
	return vterm.ScreenOpClearToEOL
}

func vtermScreenOpScrollRect() vterm.ScreenOpCode {
	return vterm.ScreenOpScrollRect
}

func vtermScreenOpModes() vterm.ScreenOpCode {
	return vterm.ScreenOpModes
}

func vtermScreenOpResize() vterm.ScreenOpCode {
	return vterm.ScreenOpResize
}

func vtermScreenOpCursor() vterm.ScreenOpCode {
	return vterm.ScreenOpCursor
}

func vtermScreenOpTitle() vterm.ScreenOpCode {
	return vterm.ScreenOpTitle
}

func (buffer *ScreenHistoryBuffer) activeGrid() *ScreenGrid {
	if buffer.InAlt {
		return buffer.Alt
	}
	return buffer.Main
}

func (buffer *ScreenHistoryBuffer) activeOwner() RowOwnerKind {
	if buffer.InAlt {
		return RowOwnerAltScreen
	}
	return RowOwnerShellStream
}

func (buffer *ScreenHistoryBuffer) writeSpan(op TerminalSemanticOp, seq uint64) error {
	grid := buffer.activeGrid()
	if grid == nil {
		return nil
	}
	rowIndex := clampInt(op.Row, 0, buffer.Rows-1)
	col := maxInt(0, op.Col)
	cells := historyCellsFromTerminal(op.Cells)
	if len(cells) == 0 && len(op.Runs) > 0 {
		cells = appendJournalRuns(nil, op.Runs, 0)
	}
	if len(cells) == 0 {
		buffer.Cursor.X = clampInt(col, 0, maxInt(0, buffer.Cols-1))
		buffer.Cursor.Y = rowIndex
		return nil
	}
	row := grid.Rows[rowIndex]
	row.Cells = writeCellsAt(row.Cells, col, cells)
	row.Cells = trimTrailingBlankCells(row.Cells)
	row.Version++
	row.OwnerSeq = seq
	if row.OwnerKind == RowOwnerUnknown {
		row.OwnerKind = buffer.activeOwner()
	}
	if op.WrappedSet {
		row.Wrapped = op.Wrapped
	}
	grid.Rows[rowIndex] = row
	buffer.Cursor.X = clampInt(col+historyCellsDisplayWidth(cells), 0, maxInt(0, buffer.Cols-1))
	buffer.Cursor.Y = rowIndex
	return nil
}

func (buffer *ScreenHistoryBuffer) applyControl(op TerminalSemanticOp, seq uint64) error {
	switch op.Control {
	case "cr":
		buffer.setCursorYFromOp(op)
		buffer.Cursor.X = 0
		return nil
	case "lf", "ind":
		buffer.setCursorYFromOp(op)
		return buffer.lineFeed(seq)
	case "nel":
		buffer.setCursorYFromOp(op)
		buffer.Cursor.X = 0
		return buffer.lineFeed(seq)
	case "bs":
		if buffer.Cursor.X > 0 {
			buffer.Cursor.X--
		}
		return nil
	case "tab", "ht":
		if op.RowSet || op.Col > 0 {
			buffer.setCursor(op.Row, op.Col)
			return nil
		}
		buffer.Cursor.X = clampInt(nextTabStop(buffer.Cursor.X), 0, maxInt(0, buffer.Cols-1))
		return nil
	case "cbt":
		buffer.setCursor(op.Row, op.Col)
		return nil
	case "cub":
		buffer.Cursor.X = clampInt(buffer.Cursor.X-controlCount(op), 0, maxInt(0, buffer.Cols-1))
		return nil
	case "cuf":
		buffer.Cursor.X = clampInt(buffer.Cursor.X+controlCount(op), 0, maxInt(0, buffer.Cols-1))
		return nil
	case "cuu":
		buffer.Cursor.Y = clampInt(buffer.Cursor.Y-controlCount(op), 0, maxInt(0, buffer.Rows-1))
		return nil
	case "cud":
		buffer.Cursor.Y = clampInt(buffer.Cursor.Y+controlCount(op), 0, maxInt(0, buffer.Rows-1))
		return nil
	case "cha", "hpa":
		buffer.Cursor.X = clampInt(maxInt(0, op.Col), 0, maxInt(0, buffer.Cols-1))
		return nil
	case "vpa":
		buffer.Cursor.Y = clampInt(maxInt(0, op.Row), 0, maxInt(0, buffer.Rows-1))
		return nil
	case "cup", "hvp":
		buffer.setCursor(op.Row, op.Col)
		return nil
	case "el":
		return buffer.eraseLine(op.Row, op.Col, op.Mode, seq)
	case "ed":
		if err := buffer.applyScrollOutProofs(op.ScrollOut, seq); err != nil {
			return err
		}
		return buffer.eraseDisplay(op.Row, op.Col, op.Mode, seq)
	case "soft-wrap":
		return buffer.softWrap(op, seq)
	case "ech":
		return buffer.eraseCharacters(op.Row, op.Col, controlCount(op), seq)
	case "dch":
		return buffer.deleteCharacters(op.Row, op.Col, controlCount(op), seq)
	case "ich":
		return buffer.insertCharacters(op.Row, op.Col, controlCount(op), seq)
	case "il":
		top, bottom := buffer.regionForLineOp(op)
		return buffer.scrollDownRegion(top, bottom, controlCount(op), seq)
	case "dl":
		top, bottom := buffer.regionForLineOp(op)
		return buffer.scrollUpRegion(top, bottom, controlCount(op), seq, false)
	case "su":
		top, bottom := buffer.regionForScrollOp(op)
		return buffer.scrollUpRegion(top, bottom, controlCount(op), seq, true)
	case "sd":
		top, bottom := buffer.regionForScrollOp(op)
		return buffer.scrollDownRegion(top, bottom, controlCount(op), seq)
	case "ri":
		return buffer.reverseIndex(op, seq)
	case "ris":
		// 中文说明：RIS 是 terminal reset boundary。screen-backed history 先把
		// 当前 primary physical rows 按 lifecycle 离开 mutable screen 处理，再
		// 重建 main/alt mutable grids；不能把 reset 后的空屏当成旧 history fallback。
		if err := buffer.sealPrimaryVisibleRows(seq); err != nil {
			return err
		}
		buffer.Main = buffer.newGrid(RowOwnerSystem)
		buffer.Alt = buffer.newGrid(RowOwnerAltScreen)
		for index := range buffer.Main.Rows {
			buffer.Main.Rows[index].OwnerSeq = seq
		}
		for index := range buffer.Alt.Rows {
			buffer.Alt.Rows[index].OwnerSeq = seq
		}
		buffer.InAlt = false
		buffer.Cursor = CursorState{}
		buffer.Margins = MarginState{Top: 0, Bottom: buffer.Rows, Left: 0, Right: buffer.Cols}
		return nil
	case "decstbm":
		buffer.setVerticalMargins(op)
		return nil
	case "decslrm", "hts", "tbc", "decst8c", "scs", "da", "da2", "dsr", "decxcpr", "decrqm", "decscusr":
		// 中文说明：这些 control 只影响 terminal 模式、tab stop 或 response，
		// 不修改 history physical rows，也不能触发 seal。
		return nil
	default:
		// TODO(R421): 新增 VT control 时必须先补 screen buffer harness，
		// 再在这里声明 owner/row/seal 语义，不能静默吞掉未知控制序列。
		return fmt.Errorf("screen history buffer unsupported control %q", op.Control)
	}
}

func (buffer *ScreenHistoryBuffer) lineFeed(seq uint64) error {
	top, bottom := buffer.effectiveScrollRegionForCursor()
	if buffer.Cursor.Y >= bottom-1 {
		if err := buffer.scrollUpRegion(top, bottom, 1, seq, true); err != nil {
			return err
		}
		buffer.Cursor.Y = clampInt(bottom-1, 0, maxInt(0, buffer.Rows-1))
		return nil
	}
	buffer.Cursor.Y++
	return nil
}

func (buffer *ScreenHistoryBuffer) scrollMainUpOne(seq uint64) error {
	return buffer.scrollUpRegion(0, buffer.Rows, 1, seq, true)
}

func (buffer *ScreenHistoryBuffer) setCursor(row int, col int) {
	buffer.Cursor.Y = clampInt(maxInt(0, row), 0, maxInt(0, buffer.Rows-1))
	buffer.Cursor.X = clampInt(maxInt(0, col), 0, maxInt(0, buffer.Cols-1))
}

func (buffer *ScreenHistoryBuffer) setCursorYFromOp(op TerminalSemanticOp) {
	if op.RowSet || op.Row > 0 {
		buffer.Cursor.Y = clampInt(maxInt(0, op.Row), 0, maxInt(0, buffer.Rows-1))
	}
}

func (buffer *ScreenHistoryBuffer) mutateRow(rowIndex int, seq uint64, mutate func([]Cell) []Cell) {
	grid := buffer.activeGrid()
	if grid == nil || rowIndex < 0 || rowIndex >= len(grid.Rows) || mutate == nil {
		return
	}
	row := grid.Rows[rowIndex]
	row.Cells = trimTrailingBlankCells(mutate(cloneHistoryCells(row.Cells)))
	row.Version++
	row.OwnerSeq = seq
	if row.OwnerKind == RowOwnerUnknown {
		row.OwnerKind = buffer.activeOwner()
	}
	grid.Rows[rowIndex] = row
}

func (buffer *ScreenHistoryBuffer) eraseLine(row int, col int, mode int, seq uint64) error {
	row = clampInt(row, 0, maxInt(0, buffer.Rows-1))
	col = clampInt(col, 0, maxInt(0, buffer.Cols-1))
	buffer.mutateRow(row, seq, func(cells []Cell) []Cell {
		switch mode {
		case 1:
			return blankDisplayRange(cells, 0, col+1)
		case 2:
			return nil
		default:
			index := cellIndexForDisplayColumn(cells, col)
			return cells[:index]
		}
	})
	buffer.setCursor(row, col)
	return nil
}

func (buffer *ScreenHistoryBuffer) eraseDisplay(row int, col int, mode int, seq uint64) error {
	row = clampInt(row, 0, maxInt(0, buffer.Rows-1))
	col = clampInt(col, 0, maxInt(0, buffer.Cols-1))
	switch mode {
	case 1:
		for current := 0; current < row; current++ {
			buffer.mutateRow(current, seq, func([]Cell) []Cell { return nil })
		}
		return buffer.eraseLine(row, col, 1, seq)
	case 2:
		for current := 0; current < buffer.Rows; current++ {
			buffer.mutateRow(current, seq, func([]Cell) []Cell { return nil })
		}
		return nil
	case 3:
		// 中文说明：ED3 是 clear scrollback/boundary 信号；R421 的 screen
		// buffer 不删除 committed physical history。R428 默认 history path 需要
		// 把当前可见 primary page seal 成上一页，再让后续输出进入新 RowID。
		return buffer.sealPrimaryVisibleRows(seq)
	default:
		if err := buffer.eraseLine(row, col, 0, seq); err != nil {
			return err
		}
		for current := row + 1; current < buffer.Rows; current++ {
			buffer.mutateRow(current, seq, func([]Cell) []Cell { return nil })
		}
		return nil
	}
}

func (buffer *ScreenHistoryBuffer) softWrap(op TerminalSemanticOp, seq uint64) error {
	grid := buffer.activeGrid()
	if grid == nil || len(grid.Rows) == 0 {
		return nil
	}
	rowIndex := clampInt(op.Row, 0, buffer.Rows-1)
	row := grid.Rows[rowIndex]
	row.Wrapped = true
	row.Version++
	row.OwnerSeq = seq
	grid.Rows[rowIndex] = row
	nextRow := rowIndex + 1
	if nextRow < len(grid.Rows) {
		next := grid.Rows[nextRow]
		next.Continued = true
		next.Version++
		next.OwnerSeq = seq
		grid.Rows[nextRow] = next
	}
	// 中文说明：soft-wrap 只标记 physical row continuation；正文 truth 仍在
	// 后续 WriteSpan cells 中，不能把 wrap 事件自己投影成一行文本。
	buffer.Cursor.X = 0
	buffer.Cursor.Y = clampInt(nextRow, 0, maxInt(0, buffer.Rows-1))
	return nil
}

func (buffer *ScreenHistoryBuffer) eraseCharacters(row int, col int, count int, seq uint64) error {
	row = clampInt(row, 0, maxInt(0, buffer.Rows-1))
	col = maxInt(0, col)
	buffer.mutateRow(row, seq, func(cells []Cell) []Cell {
		return blankDisplayRange(cells, col, count)
	})
	buffer.setCursor(row, col)
	return nil
}

func (buffer *ScreenHistoryBuffer) deleteCharacters(row int, col int, count int, seq uint64) error {
	row = clampInt(row, 0, maxInt(0, buffer.Rows-1))
	col = maxInt(0, col)
	count = maxInt(0, count)
	buffer.mutateRow(row, seq, func(cells []Cell) []Cell {
		if count == 0 || col >= historyCellsDisplayWidth(cells) {
			return cells
		}
		start := cellIndexForDisplayColumn(cells, col)
		end := cellIndexAfterDisplayColumn(cells, col+count)
		if start >= end {
			return cells
		}
		return append(cells[:start], cells[end:]...)
	})
	buffer.setCursor(row, col)
	return nil
}

func (buffer *ScreenHistoryBuffer) insertCharacters(row int, col int, count int, seq uint64) error {
	row = clampInt(row, 0, maxInt(0, buffer.Rows-1))
	col = maxInt(0, col)
	count = maxInt(0, count)
	buffer.mutateRow(row, seq, func(cells []Cell) []Cell {
		if count == 0 {
			return cells
		}
		cells = ensureCellWidth(cells, col)
		index := cellIndexForDisplayColumn(cells, col)
		blanks := blankCells(count)
		return append(cells[:index], append(blanks, cells[index:]...)...)
	})
	buffer.setCursor(row, col)
	return nil
}

func (buffer *ScreenHistoryBuffer) clearRect(op TerminalSemanticOp, seq uint64) error {
	if op.Rect.Width <= 0 || op.Rect.Height <= 0 {
		return nil
	}
	startRow := clampInt(op.Rect.Y, 0, buffer.Rows)
	endRow := clampInt(op.Rect.Y+op.Rect.Height, 0, buffer.Rows)
	for row := startRow; row < endRow; row++ {
		buffer.mutateRow(row, seq, func(cells []Cell) []Cell {
			return blankDisplayRange(cells, maxInt(0, op.Rect.X), op.Rect.Width)
		})
	}
	return nil
}

func (buffer *ScreenHistoryBuffer) applyScrollRect(op TerminalSemanticOp, seq uint64) error {
	if op.Rect.Height <= 0 || op.Dy == 0 {
		return nil
	}
	// 中文说明：ScreenHistoryBuffer 以 physical row 为 truth。R421 只声明
	// full-row 垂直 scroll rect 语义；局部列区域滚动不能伪装成 row seal。
	if op.Rect.X != 0 || op.Rect.Width < buffer.Cols {
		return fmt.Errorf("screen history buffer unsupported partial scroll rect %#v", op.Rect)
	}
	top := clampInt(op.Rect.Y, 0, buffer.Rows)
	bottom := clampInt(op.Rect.Y+op.Rect.Height, top, buffer.Rows)
	count := maxInt(0, op.Dy)
	if op.Dy < 0 {
		count = -op.Dy
		return buffer.scrollUpRegion(top, bottom, count, seq, true)
	}
	return buffer.scrollDownRegion(top, bottom, count, seq)
}

func (buffer *ScreenHistoryBuffer) applyMode(op TerminalSemanticOp, seq uint64) error {
	if !op.Private {
		return nil
	}
	switch op.Mode {
	case 47, 1047:
		if op.Enabled {
			buffer.enterAltScreen(seq, false)
		} else {
			buffer.leaveAltScreen(false)
		}
	case 1048:
		if op.Enabled {
			buffer.saveCursor()
		} else {
			buffer.restoreCursor()
		}
	case 1049:
		if op.Enabled {
			buffer.saveCursor()
			buffer.enterAltScreen(seq, true)
		} else {
			buffer.leaveAltScreen(true)
			buffer.restoreCursor()
		}
	case 2026:
		// 中文说明：synchronized output 只划定 repaint/batch boundary，
		// 不应自己创建 sealed history，也不能把屏幕内容重复 seal。
		return nil
	}
	return nil
}

func (buffer *ScreenHistoryBuffer) enterAltScreen(seq uint64, reset bool) {
	if reset || buffer.Alt == nil {
		buffer.Alt = buffer.newGrid(RowOwnerAltScreen)
		for index := range buffer.Alt.Rows {
			buffer.Alt.Rows[index].OwnerSeq = seq
		}
	}
	buffer.InAlt = true
}

func (buffer *ScreenHistoryBuffer) leaveAltScreen(_ bool) {
	buffer.InAlt = false
}

func (buffer *ScreenHistoryBuffer) saveCursor() {
	buffer.Cursor.SavedX = buffer.Cursor.X
	buffer.Cursor.SavedY = buffer.Cursor.Y
}

func (buffer *ScreenHistoryBuffer) restoreCursor() {
	buffer.Cursor.X = clampInt(buffer.Cursor.SavedX, 0, maxInt(0, buffer.Cols-1))
	buffer.Cursor.Y = clampInt(buffer.Cursor.SavedY, 0, maxInt(0, buffer.Rows-1))
}

func (buffer *ScreenHistoryBuffer) reverseIndex(op TerminalSemanticOp, seq uint64) error {
	buffer.setCursor(op.Row, op.Col)
	top, bottom := buffer.effectiveScrollRegionForCursor()
	if buffer.Cursor.Y == top {
		return buffer.scrollDownRegion(top, bottom, 1, seq)
	}
	buffer.Cursor.Y = clampInt(buffer.Cursor.Y-1, 0, maxInt(0, buffer.Rows-1))
	return nil
}

func (buffer *ScreenHistoryBuffer) setVerticalMargins(op TerminalSemanticOp) {
	top := 0
	if op.Mode > 0 {
		top = op.Mode - 1
	}
	bottom := op.Bottom
	if bottom <= 0 || bottom > buffer.Rows {
		bottom = buffer.Rows
	}
	top = clampInt(top, 0, maxInt(0, bottom-1))
	if bottom <= top {
		bottom = minInt(buffer.Rows, top+1)
	}
	buffer.Margins.Top = top
	buffer.Margins.Bottom = bottom
	buffer.Margins.Left = 0
	buffer.Margins.Right = buffer.Cols
	buffer.setCursor(top, 0)
}

func (buffer *ScreenHistoryBuffer) effectiveScrollRegionForCursor() (int, int) {
	top := clampInt(buffer.Margins.Top, 0, maxInt(0, buffer.Rows-1))
	bottom := buffer.Margins.Bottom
	if bottom <= top || bottom > buffer.Rows {
		bottom = buffer.Rows
	}
	if buffer.Cursor.Y >= top && buffer.Cursor.Y < bottom {
		return top, bottom
	}
	return 0, buffer.Rows
}

func (buffer *ScreenHistoryBuffer) regionForLineOp(op TerminalSemanticOp) (int, int) {
	top := clampInt(op.Row, 0, maxInt(0, buffer.Rows-1))
	bottom := op.Bottom
	if bottom <= top {
		if top >= buffer.Margins.Top && top < buffer.Margins.Bottom {
			bottom = buffer.Margins.Bottom
		} else {
			bottom = buffer.Rows
		}
	}
	return top, clampInt(bottom, top+1, buffer.Rows)
}

func (buffer *ScreenHistoryBuffer) regionForScrollOp(op TerminalSemanticOp) (int, int) {
	top := op.Row
	bottom := op.Bottom
	if bottom <= top {
		top = buffer.Margins.Top
		bottom = buffer.Margins.Bottom
	}
	top = clampInt(top, 0, maxInt(0, buffer.Rows-1))
	if bottom <= top || bottom > buffer.Rows {
		bottom = buffer.Rows
	}
	return top, bottom
}

func (buffer *ScreenHistoryBuffer) scrollUpRegion(top int, bottom int, count int, seq uint64, sealTop bool) error {
	grid := buffer.activeGrid()
	if grid == nil {
		return nil
	}
	top, bottom, count = normalizeScrollRegion(top, bottom, count, buffer.Rows)
	if count == 0 {
		return nil
	}
	if sealTop && !buffer.InAlt && top == 0 {
		for offset := 0; offset < count; offset++ {
			if err := buffer.sealRow(grid.Rows[top+offset]); err != nil {
				return err
			}
		}
	}
	copy(grid.Rows[top:bottom-count], grid.Rows[top+count:bottom])
	for index := bottom - count; index < bottom; index++ {
		grid.Rows[index] = buffer.newPhysicalRow(buffer.activeOwner(), seq)
	}
	return nil
}

func (buffer *ScreenHistoryBuffer) scrollDownRegion(top int, bottom int, count int, seq uint64) error {
	grid := buffer.activeGrid()
	if grid == nil {
		return nil
	}
	top, bottom, count = normalizeScrollRegion(top, bottom, count, buffer.Rows)
	if count == 0 {
		return nil
	}
	for index := bottom - 1; index >= top+count; index-- {
		grid.Rows[index] = grid.Rows[index-count]
	}
	for index := top; index < top+count; index++ {
		grid.Rows[index] = buffer.newPhysicalRow(buffer.activeOwner(), seq)
	}
	return nil
}

func normalizeScrollRegion(top int, bottom int, count int, rows int) (int, int, int) {
	if rows <= 0 {
		return 0, 0, 0
	}
	top = clampInt(top, 0, rows)
	bottom = clampInt(bottom, top, rows)
	if bottom <= top || count <= 0 {
		return top, bottom, 0
	}
	height := bottom - top
	if count > height {
		count = height
	}
	return top, bottom, count
}

func (buffer *ScreenHistoryBuffer) applyScrollOutProofs(proofs []TerminalSemanticScrollbackRowAppend, seq uint64) error {
	if buffer.InAlt || len(proofs) == 0 || buffer.Main == nil {
		return nil
	}
	seen := make(map[int]struct{}, len(proofs))
	for _, proof := range proofs {
		if !proof.RowSet || proof.Row < 0 || proof.Row >= len(buffer.Main.Rows) {
			continue
		}
		if _, ok := seen[proof.Row]; ok {
			continue
		}
		seen[proof.Row] = struct{}{}
		row := buffer.Main.Rows[proof.Row]
		row.SealSegment = HistorySegmentCommitted
		if err := buffer.sealRow(row); err != nil {
			return err
		}
		// 中文说明：ED2 clear-time proof 代表该 physical row 已离开
		// mutable screen；必须换成新 RowID，避免 current 与 sealed 重复。
		buffer.Main.Rows[proof.Row] = buffer.newPhysicalRow(RowOwnerShellStream, seq)
	}
	return nil
}

func (buffer *ScreenHistoryBuffer) sealRow(row PhysicalRow) error {
	if row.ID == 0 {
		return nil
	}
	if !screenProjectionShouldIncludeRow(row, false) {
		return nil
	}
	if _, exists := buffer.sealedRowIDs[row.ID]; exists {
		return fmt.Errorf("screen history row %d sealed twice", row.ID)
	}
	if row.ScreenCols <= 0 {
		row.ScreenCols = buffer.Cols
	}
	row.Sealed = true
	sealed := clonePhysicalRows([]PhysicalRow{row})[0]
	if buffer.sealedBackend != nil {
		if err := buffer.sealedBackend.AppendRows([]PhysicalRow{sealed}); err != nil {
			return err
		}
	}
	buffer.sealedRowIDs[row.ID] = struct{}{}
	buffer.recentCommitted = append(buffer.recentCommitted, sealed)
	if buffer.sealedBackend == nil {
		buffer.Committed = append(buffer.Committed, sealed)
	}
	return nil
}

func nextTabStop(col int) int {
	if col < 0 {
		return 0
	}
	return ((col / 8) + 1) * 8
}

func blankDisplayRange(cells []Cell, start int, count int) []Cell {
	start = maxInt(0, start)
	count = maxInt(0, count)
	if count == 0 {
		return cells
	}
	cells = ensureCellWidth(cells, start)
	startIndex := cellIndexForDisplayColumn(cells, start)
	endIndex := cellIndexAfterDisplayColumn(cells, start+count)
	if endIndex < startIndex {
		endIndex = startIndex
	}
	blanks := blankCells(count)
	return append(cells[:startIndex], append(blanks, cells[endIndex:]...)...)
}

func cellIndexAfterDisplayColumn(cells []Cell, col int) int {
	if col <= 0 {
		return 0
	}
	cursor := 0
	for index, cell := range cells {
		width := historyCellDisplayWidth(cell)
		next := cursor + width
		if col <= next {
			return index + 1
		}
		cursor = next
	}
	return len(cells)
}

func blankCells(count int) []Cell {
	if count <= 0 {
		return nil
	}
	out := make([]Cell, count)
	for index := range out {
		out[index] = blankHistoryCell()
	}
	return out
}
