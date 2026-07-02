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
