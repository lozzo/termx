package protocol

import (
	"strings"
	"time"
)

const minVisualScrollbackScreenOverlapRows = 2

// TrimSnapshotScrollbackScreenVisualOverlap removes duplicated projection rows
// when a latest history page already includes the current screen prefix.
func TrimSnapshotScrollbackScreenVisualOverlap(snapshot *Snapshot) int {
	if snapshot == nil || len(snapshot.Scrollback) == 0 || len(snapshot.Screen.Cells) == 0 {
		return 0
	}
	overlap := snapshotScrollbackScreenVisualOverlap(snapshot)
	if overlap < minVisualScrollbackScreenOverlapRows {
		return 0
	}
	keep := len(snapshot.Scrollback) - overlap
	snapshot.Scrollback = CloneCompactRows(snapshot.Scrollback[:keep])
	snapshot.ScrollbackTimestamps = trimSnapshotTimePrefix(snapshot.ScrollbackTimestamps, keep)
	snapshot.ScrollbackRowKinds = trimSnapshotStringPrefix(snapshot.ScrollbackRowKinds, keep)
	snapshot.ScrollbackWrapped = trimSnapshotBoolPrefix(snapshot.ScrollbackWrapped, keep)
	snapshot.ScrollbackOwnership = trimSnapshotStringPrefix(snapshot.ScrollbackOwnership, keep)
	return overlap
}

func snapshotScrollbackScreenVisualOverlap(snapshot *Snapshot) int {
	maxOverlap := minSnapshotInt(len(snapshot.Scrollback), len(snapshot.Screen.Cells))
	for overlap := maxOverlap; overlap >= minVisualScrollbackScreenOverlapRows; overlap-- {
		scrollbackStart := len(snapshot.Scrollback) - overlap
		if !compactRowsContainNonDefaultCells(snapshot.Scrollback[scrollbackStart:]) {
			continue
		}
		matched := true
		for i := 0; i < overlap; i++ {
			if !compactRowCellsVisualEqual(snapshot.Scrollback[scrollbackStart+i], snapshot.Screen.Cells[i]) {
				matched = false
				break
			}
		}
		if matched {
			return overlap
		}
	}
	return 0
}

func compactRowsContainNonDefaultCells(rows []CompactRow) bool {
	for _, row := range rows {
		for _, cell := range row.DecodeCells() {
			if !snapshotDefaultBlankCell(cell) {
				return true
			}
		}
	}
	return false
}

func compactRowCellsVisualEqual(left CompactRow, right []Cell) bool {
	return cellRowsVisualEqual(left.DecodeCells(), right)
}

func cellRowsVisualEqual(left, right []Cell) bool {
	left = trimSnapshotTrailingDefaultBlankCells(left)
	right = trimSnapshotTrailingDefaultBlankCells(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if snapshotCellsVisualEqual(left[i], right[i]) {
			continue
		}
		return false
	}
	return true
}

func trimSnapshotTrailingDefaultBlankCells(row []Cell) []Cell {
	for len(row) > 0 && snapshotDefaultBlankCell(row[len(row)-1]) {
		row = row[:len(row)-1]
	}
	return row
}

func snapshotCellsVisualEqual(left, right Cell) bool {
	if snapshotDefaultBlankCell(left) && snapshotDefaultBlankCell(right) {
		return true
	}
	return left == right
}

func snapshotDefaultBlankCell(cell Cell) bool {
	return strings.TrimSpace(cell.Content) == "" &&
		cell.Width <= 1 &&
		cell.Style == (CellStyle{}) &&
		cell.LinkURL == "" &&
		cell.LinkParams == ""
}

func trimSnapshotTimePrefix(values []time.Time, keep int) []time.Time {
	if keep <= 0 || len(values) < keep {
		return nil
	}
	return append([]time.Time(nil), values[:keep]...)
}

func trimSnapshotStringPrefix(values []string, keep int) []string {
	if keep <= 0 || len(values) < keep {
		return nil
	}
	return append([]string(nil), values[:keep]...)
}

func trimSnapshotBoolPrefix(values []bool, keep int) []bool {
	if keep <= 0 || len(values) < keep {
		return nil
	}
	return append([]bool(nil), values[:keep]...)
}

func minSnapshotInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
