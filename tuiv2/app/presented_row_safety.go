package app

func presentedRowHasWidthSafetyState(row presentedRow) bool {
	return row.hasWide ||
		row.hasErase ||
		row.hasHiddenEmojiCompensation ||
		row.hasHostWidthStabilizer
}

func presentedCellSafeForLinearDiff(cell presentedCell) bool {
	return cell.Width == 1 && !cell.ReanchorBefore && !cell.Erase
}

func presentedRowSafeForLinearOps(row presentedRow) bool {
	if presentedRowHasWidthSafetyState(row) {
		return false
	}
	for _, cell := range row.cells {
		if !presentedCellSafeForLinearDiff(cell) {
			return false
		}
	}
	return true
}

func hostCellHasWidthSafetyState(cell hostCell) bool {
	return cell.Wide ||
		cell.Continuation ||
		cell.HiddenEmojiCompensation ||
		cell.HostWidthStabilizer
}
