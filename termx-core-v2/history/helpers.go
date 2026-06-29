package history

func cloneLogicalLine(line LogicalLine) LogicalLine {
	line.Cells = cloneHistoryCells(line.Cells)
	if line.TailFill != nil {
		fill := *line.TailFill
		line.TailFill = &fill
	}
	return line
}

func cloneHistoryCells(cells []Cell) []Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]Cell, len(cells))
	copy(out, cells)
	return out
}

func cloneScreenFrame(frame ScreenFrame) ScreenFrame {
	frame.Rows = cloneFrameRows(frame.Rows)
	return frame
}

func cloneFrameRows(rows [][]Cell) [][]Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]Cell, len(rows))
	for i := range rows {
		out[i] = cloneHistoryCells(rows[i])
	}
	return out
}

func cloneFrameRecord(record FrameRecord) FrameRecord {
	record.LineIDs = append([]LogicalLineID(nil), record.LineIDs...)
	return record
}

func cloneHistoryRecord(record HistoryRecord) HistoryRecord {
	record.LineIDs = append([]LogicalLineID(nil), record.LineIDs...)
	return record
}

func removeLineID(ids []LogicalLineID, id LogicalLineID) []LogicalLineID {
	if len(ids) == 0 {
		return nil
	}
	out := ids[:0]
	for _, current := range ids {
		if current != id {
			out = append(out, current)
		}
	}
	return out
}
