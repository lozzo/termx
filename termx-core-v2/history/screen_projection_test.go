package history

import "testing"

func TestR422ScreenProjectionGroupsWrappedPhysicalRows(t *testing.T) {
	buffer := NewScreenHistoryBuffer(20, 3)
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq: 1,
		Ops: []TerminalSemanticOp{
			screenBufferWriteOp(0, 0, "hello-"),
			screenBufferWriteOp(1, 0, "world"),
			screenBufferWriteOp(2, 0, "done"),
		},
	}); err != nil {
		t.Fatalf("apply rows: %v", err)
	}
	buffer.Main.Rows[0].Wrapped = true
	buffer.Main.Rows[1].Continued = true
	firstID := buffer.Main.Rows[0].ID
	secondID := buffer.Main.Rows[1].ID

	projection := buffer.ProjectHistory()
	if len(projection.Lines) != 2 {
		t.Fatalf("expected two logical lines, got %#v", projection.Lines)
	}
	first := projection.Lines[0]
	if got := rowText(first.Cells); got != "hello-world" {
		t.Fatalf("wrapped physical rows should project into one logical line, got %q line=%#v", got, first)
	}
	if len(first.RowIDs) != 2 || first.RowIDs[0] != firstID || first.RowIDs[1] != secondID {
		t.Fatalf("logical line must preserve physical RowIDs, line=%#v firstID=%d secondID=%d", first, firstID, secondID)
	}
	if !first.Wrapped || !first.Continued {
		t.Fatalf("logical line must preserve wrapped/continued flags, line=%#v", first)
	}
	if projection.Rows[0].LineID != projection.Rows[1].LineID || projection.Rows[1].RowInLine != 1 {
		t.Fatalf("projected rows should point back to the grouped line, rows=%#v", projection.Rows)
	}
}

func TestR422ScreenProjectionDedupesSealedAndCurrentByRowID(t *testing.T) {
	buffer := NewScreenHistoryBuffer(12, 2)
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq: 1,
		Ops: []TerminalSemanticOp{
			screenBufferWriteOp(0, 0, "dupe"),
			screenBufferWriteOp(1, 0, "current"),
		},
	}); err != nil {
		t.Fatalf("apply rows: %v", err)
	}
	dupeID := buffer.Main.Rows[0].ID
	if err := buffer.sealRow(buffer.Main.Rows[0]); err != nil {
		t.Fatalf("seal row for projection harness: %v", err)
	}

	projection := buffer.ProjectHistory()
	if got := screenProjectionRowIDCount(projection.Rows, dupeID); got != 1 {
		t.Fatalf("latest projection must dedupe sealed/current by RowID, count=%d projection=%#v", got, projection)
	}
	if len(projection.Rows) != 2 {
		t.Fatalf("expected committed duplicate plus current second row, got %#v", projection.Rows)
	}
	if projection.Rows[0].Segment != HistorySegmentCommitted || !projection.Rows[0].Sealed {
		t.Fatalf("first projected row should be sealed history, row=%#v", projection.Rows[0])
	}
	if projection.Rows[1].RowID != buffer.Main.Rows[1].ID || projection.Rows[1].Segment != HistorySegmentCurrentPrimaryFrame {
		t.Fatalf("current non-duplicate row should remain projected, rows=%#v", projection.Rows)
	}
}

func TestR422ScreenProjectionUsesAltCurrentWithoutPrimaryPollution(t *testing.T) {
	buffer := NewScreenHistoryBuffer(12, 2)
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq: 1,
		Ops: []TerminalSemanticOp{
			screenBufferWriteOp(0, 0, "primary"),
			screenBufferModeOp(1049, true),
			screenBufferWriteOp(0, 0, "alt"),
		},
	}); err != nil {
		t.Fatalf("apply alt rows: %v", err)
	}

	projection := buffer.ProjectHistory()
	if len(projection.Rows) != 1 {
		t.Fatalf("active alt projection should expose only alt current rows without sealed primary, projection=%#v", projection)
	}
	row := projection.Rows[0]
	if got := rowText(row.Cells); got != "alt" {
		t.Fatalf("unexpected alt projected text %q row=%#v", got, row)
	}
	if row.Segment != HistorySegmentCurrentAltFrame || row.Kind != LineKindAltScreenFrame || row.Sealed {
		t.Fatalf("alt row must stay transient and out of primary sealed history, row=%#v", row)
	}
	if len(buffer.CommittedRows()) != 0 {
		t.Fatalf("alt projection must not create committed primary rows, committed=%#v", buffer.CommittedRows())
	}
}

func TestR422ScreenProjectionLatestWindowCarriesHistoryRows(t *testing.T) {
	buffer := NewScreenHistoryBuffer(12, 1)
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq: 1,
		Ops: []TerminalSemanticOp{
			screenBufferWriteOp(0, 0, "sealed"),
			screenBufferControlOp("lf"),
			screenBufferWriteOp(0, 0, "current"),
		},
	}); err != nil {
		t.Fatalf("apply scroll-out current rows: %v", err)
	}
	projection := buffer.ProjectHistory()
	rows := projection.HistoryRows()
	if len(rows) != 2 {
		t.Fatalf("projection should produce committed + current HistoryRows, rows=%#v projection=%#v", rows, projection)
	}
	if got := rowText(rows[0].Cells); got != "sealed" || !rows[0].Committed {
		t.Fatalf("first HistoryRow should be sealed physical row, got %q row=%#v", got, rows[0])
	}
	if got := rowText(rows[1].Cells); got != "current" || rows[1].Committed {
		t.Fatalf("second HistoryRow should be current physical row, got %q row=%#v", got, rows[1])
	}

	window := projection.LatestWindow(HistoryWindowRequest{TerminalID: "term-r422", Limit: 1, Cols: 12})
	if len(window.Rows) != 1 || rowText(window.Rows[0].Cells) != "current" {
		t.Fatalf("latest limit should return current tail row, window=%#v", window)
	}
	if !window.HasMore || !window.Boundary.Cursor.Valid || window.Boundary.Cursor.BeforeRowIndex != 1 {
		t.Fatalf("latest window should expose older cursor before current row, window=%#v", window)
	}
	if window.LogicalTotal != 2 {
		t.Fatalf("latest window total should come from screen projection rows, window=%#v", window)
	}
}

func screenProjectionRowIDCount(rows []ScreenProjectionRow, id uint64) int {
	count := 0
	for _, row := range rows {
		if row.RowID == id {
			count++
		}
	}
	return count
}
