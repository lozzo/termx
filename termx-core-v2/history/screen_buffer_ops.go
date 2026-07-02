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

func (buffer *ScreenHistoryBuffer) activeGrid() *ScreenGrid {
	if buffer.InAlt {
		return buffer.Alt
	}
	return buffer.Main
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
	row.Version++
	row.OwnerSeq = seq
	if row.OwnerKind == RowOwnerUnknown {
		row.OwnerKind = RowOwnerShellStream
	}
	grid.Rows[rowIndex] = row
	buffer.Cursor.X = clampInt(col+historyCellsDisplayWidth(cells), 0, maxInt(0, buffer.Cols-1))
	buffer.Cursor.Y = rowIndex
	return nil
}

func (buffer *ScreenHistoryBuffer) applyControl(op TerminalSemanticOp, seq uint64) error {
	switch op.Control {
	case "cr":
		buffer.Cursor.X = 0
		return nil
	case "lf", "ind":
		return buffer.lineFeed(seq)
	case "nel":
		buffer.Cursor.X = 0
		return buffer.lineFeed(seq)
	case "bs":
		if buffer.Cursor.X > 0 {
			buffer.Cursor.X--
		}
		return nil
	case "tab", "ht":
		buffer.Cursor.X = clampInt(nextTabStop(buffer.Cursor.X), 0, maxInt(0, buffer.Cols-1))
		return nil
	default:
		return fmt.Errorf("screen history buffer unsupported control %q", op.Control)
	}
}

func (buffer *ScreenHistoryBuffer) lineFeed(seq uint64) error {
	if buffer.Cursor.Y >= buffer.Rows-1 {
		return buffer.scrollMainUpOne(seq)
	}
	buffer.Cursor.Y++
	return nil
}

func (buffer *ScreenHistoryBuffer) scrollMainUpOne(seq uint64) error {
	if buffer.Main == nil || len(buffer.Main.Rows) == 0 {
		return nil
	}
	if err := buffer.sealRow(buffer.Main.Rows[0]); err != nil {
		return err
	}
	copy(buffer.Main.Rows, buffer.Main.Rows[1:])
	buffer.Main.Rows[len(buffer.Main.Rows)-1] = buffer.newPhysicalRow(RowOwnerShellStream, seq)
	buffer.Cursor.Y = maxInt(0, buffer.Rows-1)
	return nil
}

func (buffer *ScreenHistoryBuffer) sealRow(row PhysicalRow) error {
	if row.ID == 0 {
		return nil
	}
	if _, exists := buffer.sealedRowIDs[row.ID]; exists {
		return fmt.Errorf("screen history row %d sealed twice", row.ID)
	}
	row.Sealed = true
	buffer.sealedRowIDs[row.ID] = struct{}{}
	buffer.Committed = append(buffer.Committed, clonePhysicalRows([]PhysicalRow{row})[0])
	return nil
}

func nextTabStop(col int) int {
	if col < 0 {
		return 0
	}
	return ((col / 8) + 1) * 8
}
