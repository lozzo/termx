package history

// applyPrimaryFrameRows 将 primary pseudo-TUI frame proof 应用到 physical rows。
// domain owner 是 ScreenHistoryBuffer；truth source 是同一 vterm semantic pass 的
// TerminalSemanticFrame/touched rows。它只修改 mutable main screen，不创建 logical
// line mutation，也不把 frame proof 直接写入 sealed timeline。
func (buffer *ScreenHistoryBuffer) applyPrimaryFrameRows(frame TerminalSemanticFrame, touchedRows []int, seq uint64) error {
	if buffer == nil {
		return nil
	}
	buffer.ensure()
	buffer.InAlt = false
	rows := touchedRows
	if len(rows) == 0 {
		rows = make([]int, buffer.Rows)
		for index := range rows {
			rows[index] = index
		}
	}
	for _, rowIndex := range rows {
		if rowIndex < 0 || rowIndex >= buffer.Rows {
			continue
		}
		var cells []Cell
		if rowIndex < len(frame.Rows) {
			cells = historyCellsFromTerminal(frame.Rows[rowIndex])
		}
		row := buffer.Main.Rows[rowIndex]
		// 中文说明：primary frame 是 fixed-grid 屏幕内容，尾部普通空格也可能是
		// 用户可见布局；不能沿用 ordinary stream 的 trailing blank trim。
		row.Cells = cells
		row.Version++
		row.OwnerSeq = seq
		row.OwnerKind = RowOwnerPrimaryFrame
		row.Sealed = false
		buffer.Main.Rows[rowIndex] = row
	}
	return nil
}

func (buffer *ScreenHistoryBuffer) clearPrimaryFrameRows(seq uint64) {
	if buffer == nil {
		return
	}
	buffer.ensure()
	for rowIndex := range buffer.Main.Rows {
		row := buffer.Main.Rows[rowIndex]
		row.Cells = nil
		row.Wrapped = false
		row.Continued = false
		row.Version++
		row.OwnerSeq = seq
		row.OwnerKind = RowOwnerPrimaryFrame
		row.Sealed = false
		buffer.Main.Rows[rowIndex] = row
	}
}

func (buffer *ScreenHistoryBuffer) applyAltFrameRows(frame TerminalSemanticFrame, seq uint64) error {
	if buffer == nil {
		return nil
	}
	buffer.ensure()
	buffer.InAlt = true
	for rowIndex := range buffer.Alt.Rows {
		var cells []Cell
		if rowIndex < len(frame.Rows) {
			cells = historyCellsFromTerminal(frame.Rows[rowIndex])
		}
		row := buffer.Alt.Rows[rowIndex]
		row.Cells = cells
		row.Version++
		row.OwnerSeq = seq
		row.OwnerKind = RowOwnerAltScreen
		row.Sealed = false
		buffer.Alt.Rows[rowIndex] = row
	}
	return nil
}

func (buffer *ScreenHistoryBuffer) clearAltRows(seq uint64) {
	if buffer == nil {
		return
	}
	buffer.ensure()
	buffer.Alt = buffer.newGrid(RowOwnerAltScreen)
	for index := range buffer.Alt.Rows {
		buffer.Alt.Rows[index].OwnerSeq = seq
	}
	buffer.InAlt = false
}

func (buffer *ScreenHistoryBuffer) sealPrimaryVisibleRows(seq uint64) error {
	if buffer == nil {
		return nil
	}
	buffer.ensure()
	for rowIndex, row := range buffer.Main.Rows {
		if !screenProjectionShouldIncludeRow(row, false) {
			continue
		}
		if _, exists := buffer.sealedRowIDs[row.ID]; !exists {
			if err := buffer.sealRow(row); err != nil {
				return err
			}
		}
		buffer.Main.Rows[rowIndex] = buffer.newPhysicalRow(RowOwnerShellStream, seq)
	}
	return nil
}

func (buffer *ScreenHistoryBuffer) sealScrollOutProofRows(proofs []TerminalSemanticScrollOut, seq uint64) error {
	if buffer == nil || len(proofs) == 0 {
		return nil
	}
	buffer.ensure()
	for _, proof := range proofs {
		if buffer.InAlt {
			continue
		}
		if proof.RowSet && proof.Row >= 0 && proof.Row < len(buffer.Main.Rows) {
			row := buffer.Main.Rows[proof.Row]
			if !screenProjectionShouldIncludeRow(row, false) {
				row.Cells = cellsFromScrollOutProof(proof)
				row.OwnerKind = RowOwnerPrimaryFrame
				row.OwnerSeq = seq
				row.Version++
			}
			if _, exists := buffer.sealedRowIDs[row.ID]; !exists {
				if err := buffer.sealRow(row); err != nil {
					return err
				}
			}
			buffer.Main.Rows[proof.Row] = buffer.newPhysicalRow(RowOwnerShellStream, seq)
			continue
		}
		row := buffer.newPhysicalRow(RowOwnerPrimaryFrame, seq)
		row.Cells = cellsFromScrollOutProof(proof)
		row.Sealed = true
		if len(row.Cells) == 0 {
			continue
		}
		if err := buffer.sealRow(row); err != nil {
			return err
		}
	}
	return nil
}

func (buffer *ScreenHistoryBuffer) resizeMutableScreen(cols int, rows int, seq uint64) {
	if buffer == nil {
		return
	}
	buffer.ensure()
	if cols <= 0 {
		cols = buffer.Cols
	}
	if rows <= 0 {
		rows = buffer.Rows
	}
	if cols == buffer.Cols && rows == buffer.Rows {
		return
	}
	oldMain := buffer.Main
	oldAlt := buffer.Alt
	buffer.Cols = cols
	buffer.Rows = rows
	buffer.Margins = MarginState{Top: 0, Bottom: rows, Left: 0, Right: cols}
	buffer.Main = resizeScreenGridRows(buffer, oldMain, RowOwnerShellStream, rows, seq)
	buffer.Alt = resizeScreenGridRows(buffer, oldAlt, RowOwnerAltScreen, rows, seq)
	buffer.Cursor.X = clampInt(buffer.Cursor.X, 0, maxInt(0, cols-1))
	buffer.Cursor.Y = clampInt(buffer.Cursor.Y, 0, maxInt(0, rows-1))
}

func resizeScreenGridRows(buffer *ScreenHistoryBuffer, old *ScreenGrid, owner RowOwnerKind, rows int, seq uint64) *ScreenGrid {
	grid := &ScreenGrid{Rows: make([]PhysicalRow, rows)}
	for index := range grid.Rows {
		if old != nil && index < len(old.Rows) {
			row := old.Rows[index]
			row.OwnerSeq = seq
			grid.Rows[index] = row
			continue
		}
		grid.Rows[index] = buffer.newPhysicalRow(owner, seq)
	}
	return grid
}
