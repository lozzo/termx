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
	screenCols := frame.Cols
	if screenCols <= 0 {
		screenCols = buffer.Cols
	}
	rows := touchedRows
	if len(rows) == 0 {
		rows = make([]int, buffer.Rows)
		for index := range rows {
			rows[index] = index
		}
	}
	frameRows := trimmedFrameRows(frame.Rows)
	for _, rowIndex := range rows {
		if rowIndex < 0 || rowIndex >= buffer.Rows {
			continue
		}
		var cells []Cell
		if rowIndex < len(frameRows) {
			cells = cloneHistoryCells(frameRows[rowIndex])
		}
		row := buffer.Main.Rows[rowIndex]
		// 中文说明：primary frame 是 fixed-grid 屏幕内容，行内普通空格可能是
		// 用户可见布局；但尾部整行 default blank 按 R315/R328 不能进入
		// history projection，所以这里使用 frame 级裁尾结果。
		row.Cells = cells
		row.Version++
		row.OwnerSeq = seq
		row.OwnerKind = RowOwnerPrimaryFrame
		row.Sealed = false
		row.ScreenCols = screenCols
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
	screenCols := frame.Cols
	if screenCols <= 0 {
		screenCols = buffer.Cols
	}
	frameRows := trimmedFrameRows(frame.Rows)
	for rowIndex := range buffer.Alt.Rows {
		var cells []Cell
		if rowIndex < len(frameRows) {
			cells = cloneHistoryCells(frameRows[rowIndex])
		}
		row := buffer.Alt.Rows[rowIndex]
		row.Cells = cells
		row.Version++
		row.OwnerSeq = seq
		row.OwnerKind = RowOwnerAltScreen
		row.Sealed = false
		row.ScreenCols = screenCols
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
	return buffer.sealPrimaryVisibleRowsAs(seq, "")
}

func (buffer *ScreenHistoryBuffer) sealPrimaryVisibleRowsAs(seq uint64, segment HistorySegment) error {
	if buffer == nil {
		return nil
	}
	buffer.ensure()
	for rowIndex, row := range buffer.Main.Rows {
		if !screenProjectionShouldIncludeRow(row, false) {
			if row.OwnerKind == RowOwnerPrimaryFrame {
				// 中文说明：primary frame 关闭时，尾部默认空白行不会进入
				// projection，但它们仍可能持有 frame ownership。必须释放成新
				// RowID 的 shell row，否则后续普通 prompt 会继承 screen-frame
				// owner，形成第二份错误 current frame truth。
				buffer.Main.Rows[rowIndex] = buffer.newPhysicalRow(RowOwnerShellStream, seq)
			}
			continue
		}
		if _, exists := buffer.sealedRowIDs[row.ID]; !exists {
			if segment != "" {
				row.SealSegment = segment
			}
			if err := buffer.sealRow(row); err != nil {
				return err
			}
		}
		buffer.Main.Rows[rowIndex] = buffer.newPhysicalRow(RowOwnerShellStream, seq)
	}
	return nil
}

func (buffer *ScreenHistoryBuffer) sealPrimaryRowAt(rowIndex int, seq uint64) error {
	if buffer == nil {
		return nil
	}
	buffer.ensure()
	if rowIndex < 0 || rowIndex >= len(buffer.Main.Rows) {
		return nil
	}
	row := buffer.Main.Rows[rowIndex]
	if screenProjectionShouldIncludeRow(row, false) {
		if _, exists := buffer.sealedRowIDs[row.ID]; !exists {
			row.SealSegment = HistorySegmentCommitted
			if err := buffer.sealRow(row); err != nil {
				return err
			}
		}
	}
	// 中文说明：hard line boundary 后，即使该 physical row 仍在 viewport
	// 可见，它的历史 truth 已经进入 sealed rows；current screen 必须换新 RowID，
	// 防止后续 projection 把同一 RowID 同时当 sealed/current。
	buffer.Main.Rows[rowIndex] = buffer.newPhysicalRow(RowOwnerShellStream, seq)
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
			// 中文说明：Row/RowSet 只标识 proof 来源行，不能在 transaction
			// 末尾再从 current screen 反读正文。scroll-out truth 是 proof
			// payload 本身；否则同步输出后置消费会把 sync01 误读成当前尾屏。
			if err := buffer.sealDetachedScrollOutProofRow(proof, seq); err != nil {
				return err
			}
			continue
		}
		if err := buffer.sealDetachedScrollOutProofRow(proof, seq); err != nil {
			return err
		}
	}
	return nil
}

func (buffer *ScreenHistoryBuffer) sealDetachedScrollOutProofRow(proof TerminalSemanticScrollOut, seq uint64) error {
	row := buffer.newPhysicalRow(RowOwnerShellStream, seq)
	row.Cells = cellsFromScrollOutProof(proof)
	row.Wrapped = proof.Wrapped
	row.ScreenCols = buffer.Cols
	row.SealSegment = HistorySegmentCommitted
	if len(row.Cells) == 0 && !row.Wrapped {
		return nil
	}
	return buffer.sealRow(row)
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
