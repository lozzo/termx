package protocol

import "testing"

func TestSnapshotCommittedLoadedDepthPrefersAuthoritativeLoadedRows(t *testing.T) {
	snapshot := &Snapshot{
		Scrollback:           CompactRowsFromCells(repeatedProtocolRowsForOwnershipTest(150)),
		ScrollbackOwnership:  repeatedProtocolOwnershipForTest(RowOwnershipPersisted, 150),
		ScrollbackLoadedRows: 100,
		HistoryGeneration:    7,
		ScrollbackFirstRowID: 0,
		ScrollbackLastRowID:  99,
	}

	if got := SnapshotCommittedLoadedDepth(snapshot); got != 100 {
		t.Fatalf("expected explicit loaded depth 100 to win over reflowed materialized rows, got %d", got)
	}
}

func TestSnapshotCommittedLoadedDepthFallsBackToOwnershipWhenLoadedRowsMissing(t *testing.T) {
	snapshot := &Snapshot{
		Scrollback:           CompactRowsFromCells(repeatedProtocolRowsForOwnershipTest(3)),
		ScrollbackOwnership:  repeatedProtocolOwnershipForTest(RowOwnershipPersisted, 3),
		ScrollbackOffset:     5,
		HistoryGeneration:    7,
		ScrollbackFirstRowID: 5,
		ScrollbackLastRowID:  7,
	}

	if got := SnapshotCommittedLoadedDepth(snapshot); got != 8 {
		t.Fatalf("expected fallback loaded depth 8 from offset plus committed rows, got %d", got)
	}
}

func TestSnapshotCommittedLoadedDepthKeepsAuthoritativeLoadedRowsAfterProjectionTrim(t *testing.T) {
	snapshot := &Snapshot{
		ScrollbackLoadedRows: 81,
		HistoryGeneration:    7,
		ScrollbackFirstRowID: 0,
		ScrollbackLastRowID:  80,
	}

	if got := SnapshotCommittedLoadedDepth(snapshot); got != 81 {
		t.Fatalf("expected explicit loaded depth 81 without materialized rows, got %d", got)
	}
}

func repeatedProtocolRowsForOwnershipTest(count int) [][]Cell {
	rows := make([][]Cell, count)
	for i := range rows {
		rows[i] = []Cell{{Content: "x", Width: 1}}
	}
	return rows
}

func repeatedProtocolOwnershipForTest(value string, count int) []string {
	values := make([]string, count)
	for i := range values {
		values[i] = value
	}
	return values
}
