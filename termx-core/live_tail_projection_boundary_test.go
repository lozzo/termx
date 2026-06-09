package termx

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestTerminalLatestCombinedProjectionKeepsCommittedMetadataPersistedOnly(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	vt.LoadSnapshot(
		localvterm.ScreenData{Cells: [][]localvterm.Cell{localVTermCellsFromString("live")}},
		localvterm.CursorState{Row: 0, Col: 4, Visible: true},
		localvterm.TerminalModes{AutoWrap: true},
	)
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "latest-metadata-persisted-only",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("hist"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail"), WrappedSet: true, Wrapped: true},
		},
		LiveTailAppendRows: 1,
	}
	term.appendGridFromDamageLocked(damage)

	_, generation, persistedRows := store.coordinates()
	if persistedRows != 1 {
		t.Fatalf("expected one committed persisted row in setup, got %d", persistedRows)
	}

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 10, Cols: 4})
	if viewport == nil {
		t.Fatal("expected viewport")
	}
	if got := rowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"hist", "tail"}) {
		t.Fatalf("expected latest viewport rows to include committed persisted row plus live-tail row, got %#v", got)
	}
	if viewport.ScrollbackTotal != 2 {
		t.Fatalf("expected combined total rows 2 including live tail, got %d", viewport.ScrollbackTotal)
	}
	if viewport.LoadedRows != 1 {
		t.Fatalf("expected loaded committed rows to stay persisted-only, got %d", viewport.LoadedRows)
	}
	if viewport.FirstRowID != 0 || viewport.LastRowID != 0 {
		t.Fatalf("expected committed row ids to stay on persisted window 0..0, got %d..%d", viewport.FirstRowID, viewport.LastRowID)
	}
	if viewport.HistoryGeneration != generation {
		t.Fatalf("expected committed history generation %d, got %d", generation, viewport.HistoryGeneration)
	}
	if viewport.ScrollbackLogicalTotal != 1 {
		t.Fatalf("expected logical total to describe committed persisted lines only, got %d", viewport.ScrollbackLogicalTotal)
	}

	snapshot := term.Snapshot(0, 10)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if got := rowsToStrings(snapshot.Scrollback); !reflect.DeepEqual(got, []string{"hist", "tail"}) {
		t.Fatalf("expected latest snapshot scrollback to include committed persisted row plus live-tail row, got %#v", got)
	}
	if snapshot.ScrollbackTotal != 2 {
		t.Fatalf("expected combined snapshot total rows 2 including live tail, got %d", snapshot.ScrollbackTotal)
	}
	if snapshot.ScrollbackLoadedRows != 1 {
		t.Fatalf("expected snapshot loaded committed rows to stay persisted-only, got %d", snapshot.ScrollbackLoadedRows)
	}
	if snapshot.ScrollbackFirstRowID != 0 || snapshot.ScrollbackLastRowID != 0 {
		t.Fatalf("expected snapshot committed row ids to stay on persisted window 0..0, got %d..%d", snapshot.ScrollbackFirstRowID, snapshot.ScrollbackLastRowID)
	}
	if snapshot.HistoryGeneration != generation {
		t.Fatalf("expected snapshot committed history generation %d, got %d", generation, snapshot.HistoryGeneration)
	}
	if snapshot.ScrollbackLogicalTotal != 1 {
		t.Fatalf("expected snapshot logical total to describe committed persisted lines only, got %d", snapshot.ScrollbackLogicalTotal)
	}
}

func TestTerminalOlderOffsetStaysPersistedOnlyWhenLiveTailHasMultipleRows(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "older-offset-persisted-only",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("hist"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail0"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail1"), WrappedSet: true, Wrapped: true},
		},
		LiveTailAppendRows: 2,
	}
	term.appendGridFromDamageLocked(damage)

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 1, ScrollbackLimit: 10, Cols: 4})
	if viewport == nil {
		t.Fatal("expected viewport metadata")
	}
	if got := rowsToStrings(viewport.Rows); len(got) != 0 {
		t.Fatalf("expected loaded committed depth offset=1 to have no older persisted rows beyond it, got %#v", got)
	}
	if viewport.LoadedRows != 1 {
		t.Fatalf("expected loaded committed rows to stay on persisted window, got %d", viewport.LoadedRows)
	}
	if viewport.HistoryGeneration == 0 {
		t.Fatal("expected committed persisted viewport to carry generation")
	}
	if viewport.FirstRowID != 0 || viewport.LastRowID != 0 {
		t.Fatalf("expected no materialized committed row ids for exhausted older page, got %d..%d", viewport.FirstRowID, viewport.LastRowID)
	}
}

func TestTerminalOlderOffsetUsesCommittedPersistedDepthWithoutLiveTailShift(t *testing.T) {
	vt := localvterm.New(16, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows := make([][]localvterm.Cell, 0, 13000)
	for i := 0; i < 13000; i++ {
		rows = append(rows, localVTermCellsFromString(rowLabel("persisted", i)))
	}
	if err := store.AppendRows(rows); err != nil {
		t.Fatalf("append persisted rows: %v", err)
	}
	term := &Terminal{
		id:    "older-persisted-depth-only",
		size:  Size{Cols: 16, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	term.primaryLiveTail.replaceRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("live-tail-0"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("live-tail-1"), WrappedSet: true, Wrapped: false},
	}, terminalLiveTailOriginLive, false)

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 12000, ScrollbackLimit: 500, Cols: 16})
	if viewport == nil {
		t.Fatal("expected viewport")
	}
	if got, want := viewport.LoadedRows, 12500; got != want {
		t.Fatalf("expected loaded committed rows %d, got %d", want, got)
	}
	if got, want := viewport.FirstRowID, uint64(500); got != want {
		t.Fatalf("expected first committed row id %d, got %d", want, got)
	}
	if got, want := viewport.LastRowID, uint64(999); got != want {
		t.Fatalf("expected last committed row id %d, got %d", want, got)
	}
	gotRows := rowsToStrings(viewport.Rows)
	if len(gotRows) != 500 {
		t.Fatalf("expected 500 rows in viewport, got %d", len(gotRows))
	}
	if got, want := gotRows[0], rowLabel("persisted", 500); got != want {
		t.Fatalf("expected oldest row %q, got %q", want, got)
	}
	if got, want := gotRows[len(gotRows)-1], rowLabel("persisted", 999); got != want {
		t.Fatalf("expected newest row %q, got %q", want, got)
	}
}

func TestTerminalLatestLiveTailOnlyProjectionDoesNotInventCanonicalMetadata(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "live-tail-only-no-canonical-metadata",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("tail0"), Timestamp: time.Unix(30, 0).UTC(), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail1"), Timestamp: time.Unix(10, 0).UTC(), WrappedSet: true, Wrapped: true},
		},
		LiveTailAppendRows: 2,
	}
	term.appendGridFromDamageLocked(damage)

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 10, Cols: 4})
	if viewport == nil {
		t.Fatal("expected viewport")
	}
	if got := rowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"tail0", "tail1"}) {
		t.Fatalf("expected live-tail-only latest viewport rows, got %#v", got)
	}
	if viewport.LoadedRows != 0 {
		t.Fatalf("expected no committed loaded rows for live-tail-only latest viewport, got %d", viewport.LoadedRows)
	}
	if viewport.HistoryGeneration != 0 {
		t.Fatalf("expected no canonical generation for live-tail-only latest viewport, got %d", viewport.HistoryGeneration)
	}
	if viewport.FirstRowID != 0 || viewport.LastRowID != 0 {
		t.Fatalf("expected no canonical row window for live-tail-only latest viewport, got %d..%d", viewport.FirstRowID, viewport.LastRowID)
	}
	coreViewport, err := term.combinedGridViewport(0, 10, 4, term.primaryLiveTail.clone())
	if err != nil {
		t.Fatalf("combined viewport: %v", err)
	}
	if got := coreViewport.LogicalLineIDs; len(got) != 2 || got[0] < terminalLiveTailLogicalLineIDBase || got[0] != got[1] {
		t.Fatalf("expected live-tail-only internal viewport rows to share runtime logical line id, got %#v", got)
	}
	window := historyWindowFromCoreGridViewport(term.id, 0, coreViewport)
	if len(window.Lines) != 1 || window.Lines[0].LogicalLineID != coreViewport.LogicalLineIDs[0] {
		t.Fatalf("expected history window to expose live-tail runtime logical line id, lines=%#v row_ids=%#v", window.Lines, coreViewport.LogicalLineIDs)
	}
	if !window.Lines[0].TimestampStart.Equal(time.Unix(10, 0).UTC()) || !window.Lines[0].TimestampEnd.Equal(time.Unix(30, 0).UTC()) {
		t.Fatalf("expected history window live-tail span to carry logical line timestamp range, got %#v", window.Lines[0])
	}
	if window.LoadedLines != 1 || window.LogicalTotal != 1 {
		t.Fatalf("expected live-tail-only history window to count its mutable logical line, loaded=%d total=%d", window.LoadedLines, window.LogicalTotal)
	}
	lineMetadata, err := readTerminalGridLineMetadata(store.dir)
	if err != nil {
		t.Fatalf("read live tail line metadata: %v", err)
	}
	if len(lineMetadata.LiveRecords) != 1 {
		t.Fatalf("expected one live tail line metadata record, got %#v", lineMetadata.LiveRecords)
	}
	liveRecord := lineMetadata.LiveRecords[0]
	if liveRecord.ID != coreViewport.LogicalLineIDs[0] || liveRecord.Residency != terminalLogicalLineResidencyLiveTail || !liveRecord.Dirty || liveRecord.Origin != terminalLiveTailOriginLive {
		t.Fatalf("unexpected live tail line metadata record: %#v row_ids=%#v", liveRecord, coreViewport.LogicalLineIDs)
	}

	snapshot := term.Snapshot(0, 10)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if got := rowsToStrings(snapshot.Scrollback); !reflect.DeepEqual(got, []string{"tail0", "tail1"}) {
		t.Fatalf("expected live-tail-only latest snapshot rows, got %#v", got)
	}
	if snapshot.ScrollbackLoadedRows != 0 {
		t.Fatalf("expected no committed loaded rows for live-tail-only latest snapshot, got %d", snapshot.ScrollbackLoadedRows)
	}
	if snapshot.HistoryGeneration != 0 {
		t.Fatalf("expected no canonical generation for live-tail-only latest snapshot, got %d", snapshot.HistoryGeneration)
	}
	if snapshot.ScrollbackFirstRowID != 0 || snapshot.ScrollbackLastRowID != 0 {
		t.Fatalf("expected no canonical row window for live-tail-only latest snapshot, got %d..%d", snapshot.ScrollbackFirstRowID, snapshot.ScrollbackLastRowID)
	}
}

func TestTerminalLatestLiveTailLimitDoesNotInventCommittedCursor(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	var tail terminalPrimaryLiveTail
	firstID := terminalLiveTailLogicalLineIDBase + 10
	secondID := terminalLiveTailLogicalLineIDBase + 11
	tail.replaceLiveRowsWithLogicalLineIDs([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("a0"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("b0"), WrappedSet: true, Wrapped: false},
	}, []uint64{firstID, secondID}, false)

	viewport, err := historyCombinedGridViewportFromStore(store, 0, 1, 4, tail)
	if err != nil {
		t.Fatalf("history viewport for limited mutable live tail: %v", err)
	}
	window := historyWindowFromCoreGridViewport("live-tail-limit", 0, viewport)
	if got := historyWindowRowTexts(window); !reflect.DeepEqual(got, []string{"b0"}) {
		t.Fatalf("expected latest limit to return only newest mutable live row, got %#v", got)
	}
	if window.BeforeOffset != 0 || window.LoadedRows != 0 || window.Generation != 0 || window.FirstRowID != 0 || window.LastRowID != 0 {
		t.Fatalf("expected mutable latest limit not to invent committed cursor or row boundary, window=%#v", window)
	}
	if window.LoadedLines != 1 || window.LogicalTotal != 2 || !window.HasMore {
		t.Fatalf("expected mutable latest limit to count returned line separately from total live lines, loaded=%d total=%d has_more=%v", window.LoadedLines, window.LogicalTotal, window.HasMore)
	}
	if len(window.Lines) != 1 || window.Lines[0].LogicalLineID != secondID || window.Lines[0].ClippedBefore || window.Lines[0].ClippedAfter {
		t.Fatalf("expected latest limit to expose complete newest mutable line only, got %#v", window.Lines)
	}
}

func TestTerminalLatestLiveTailLimitMarksClippedMutableLineBefore(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	var tail terminalPrimaryLiveTail
	lineID := terminalLiveTailLogicalLineIDBase + 20
	tail.replaceLiveRowsWithLogicalLineIDs([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("a0"), RowKind: "live-line", WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("a1"), RowKind: "live-line", WrappedSet: true, Wrapped: false},
	}, []uint64{lineID, lineID}, false)

	viewport, err := historyCombinedGridViewportFromStore(store, 0, 1, 4, tail)
	if err != nil {
		t.Fatalf("history viewport for clipped mutable live tail: %v", err)
	}
	window := historyWindowFromCoreGridViewport("live-tail-limit-clipped", 0, viewport)
	if got := historyWindowRowTexts(window); !reflect.DeepEqual(got, []string{"a1"}) {
		t.Fatalf("expected latest limit to return the visible tail of the mutable live line, got %#v", got)
	}
	if window.BeforeOffset != 0 || window.LoadedRows != 0 || window.Generation != 0 || window.FirstRowID != 0 || window.LastRowID != 0 {
		t.Fatalf("expected clipped mutable latest limit not to invent committed cursor or row boundary, window=%#v", window)
	}
	if window.LoadedLines != 0 || window.LogicalTotal != 1 || window.TotalRows != 2 || !window.HasMore {
		t.Fatalf("expected clipped mutable latest limit to expose clipped prefix without loaded line start, loaded=%d total=%d rows=%d has_more=%v", window.LoadedLines, window.LogicalTotal, window.TotalRows, window.HasMore)
	}
	if window.FirstLineID != 0 || window.LastLineID != 0 {
		t.Fatalf("expected clipped-before mutable line not to expose loaded line boundaries, first=%d last=%d", window.FirstLineID, window.LastLineID)
	}
	if len(window.Lines) != 1 {
		t.Fatalf("expected one clipped mutable logical line span, got %#v", window.Lines)
	}
	span := window.Lines[0]
	if span.StartRow != 0 || span.EndRow != 0 || span.RowKind != "live-line" || span.LogicalLineID != lineID || !span.ClippedBefore || span.ClippedAfter {
		t.Fatalf("expected latest limit to mark mutable live line clipped-before only, got %#v", span)
	}
}

func TestTerminalLatestCombinedViewportLogicalTotalIncludesVisiblePersistedAndLiveTailLines(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("p0"), wrapped: false},
		{cells: localVTermCellsFromString("r0"), wrapped: false},
	})
	var tail terminalPrimaryLiveTail
	tail.replaceReclaimedPrefixWithLogicalLineIDs([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("r0"), WrappedSet: true, Wrapped: false},
	}, []uint64{2}, store.generation, 1, 1)
	tail.replaceLiveRowsWithLogicalLineIDs([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("live"), WrappedSet: true, Wrapped: false},
	}, []uint64{terminalLiveTailLogicalLineIDBase + 1}, false)

	viewport, err := combinedGridViewportFromStore(store, 0, 10, 4, tail)
	if err != nil {
		t.Fatalf("combined viewport: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"p0", "r0", "live"}) {
		t.Fatalf("expected visible persisted prefix plus reclaimed/live tail rows, got %#v", got)
	}
	if viewport.LogicalTotal != 2 {
		t.Fatalf("expected legacy viewport logical total to stay committed-only, got %d", viewport.LogicalTotal)
	}
	if viewport.WindowLogicalTotal != 3 {
		t.Fatalf("expected history-window logical total to include visible persisted prefix and live tail lines without double-counting reclaimed suffix, got %d", viewport.WindowLogicalTotal)
	}
	window := historyWindowFromCoreGridViewport("combined-logical-total", 0, viewport)
	if window.LoadedLines != 3 || window.LogicalTotal != 3 {
		t.Fatalf("expected history window loaded/total lines to stay consistent, loaded=%d total=%d lines=%#v", window.LoadedLines, window.LogicalTotal, window.Lines)
	}
}

func TestTerminalLatestCombinedViewportCountsReclaimedCommittedRowsOnce(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("p0"), wrapped: false},
		{cells: localVTermCellsFromString("p1"), wrapped: false},
		{cells: localVTermCellsFromString("r0"), wrapped: false},
		{cells: localVTermCellsFromString("r1"), wrapped: false},
	})
	var tail terminalPrimaryLiveTail
	tail.replaceReclaimedPrefixWithLogicalLineIDs([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("r0"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("r1"), WrappedSet: true, Wrapped: false},
	}, []uint64{3, 4}, store.generation, 2, 3)

	viewport, err := combinedGridViewportFromStore(store, 0, 3, 20, tail)
	if err != nil {
		t.Fatalf("combined viewport: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"p1", "r0", "r1"}) {
		t.Fatalf("expected visible persisted prefix plus reclaimed suffix once, got %#v", got)
	}
	if viewport.LoadedRows != 3 {
		t.Fatalf("expected loaded committed depth to count visible persisted plus reclaimed suffix once, got %d", viewport.LoadedRows)
	}
	if !viewport.HasMore {
		t.Fatal("expected older persisted prefix to remain available")
	}
	latest := historyWindowFromCoreGridViewport("reclaimed-depth", 0, viewport)
	if latest.BeforeOffset != 3 {
		t.Fatalf("expected latest before cursor to cover three committed rows once, got %d", latest.BeforeOffset)
	}

	older, err := combinedGridViewportFromStore(store, latest.BeforeOffset, 10, 20, tail)
	if err != nil {
		t.Fatalf("older viewport: %v", err)
	}
	if got := vtermRowsToStrings(older.Rows); !reflect.DeepEqual(got, []string{"p0"}) {
		t.Fatalf("expected older request to return only unreclaimed prefix without duplicates, got %#v", got)
	}
	if older.LoadedRows != 4 {
		t.Fatalf("expected exhausted older cursor to stay at committed depth 4, got %d", older.LoadedRows)
	}
}

func TestTerminalLatestReclaimedLiveTailLimitMarksClippedCommittedLineBefore(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("p0"), wrapped: false},
		{cells: localVTermCellsFromString("p1"), wrapped: false},
		{cells: localVTermCellsFromString("r0"), wrapped: true},
		{cells: localVTermCellsFromString("r1"), wrapped: false},
	})
	var tail terminalPrimaryLiveTail
	tail.replaceReclaimedPrefixWithLogicalLineIDs([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("r0"), RowKind: "reclaimed-line", WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("r1"), RowKind: "reclaimed-line", WrappedSet: true, Wrapped: false},
	}, []uint64{3, 3}, store.generation, 2, 3)

	viewport, err := historyCombinedGridViewportFromStore(store, 0, 1, 4, tail)
	if err != nil {
		t.Fatalf("combined viewport: %v", err)
	}
	latest := historyWindowFromCoreGridViewport("reclaimed-limit-clipped", 0, viewport)
	if got := historyWindowRowTexts(latest); !reflect.DeepEqual(got, []string{"r1"}) {
		t.Fatalf("expected latest limit to return only visible reclaimed line tail, got %#v", got)
	}
	if latest.BeforeOffset != 1 || latest.LoadedRows != 1 {
		t.Fatalf("expected latest cursor to count only returned reclaimed committed row, before=%d loaded=%d", latest.BeforeOffset, latest.LoadedRows)
	}
	if latest.Generation != store.generation || latest.FirstRowID != 3 || latest.LastRowID != 3 {
		t.Fatalf("expected latest row boundary to follow kept reclaimed row, gen=%d rows=%d..%d", latest.Generation, latest.FirstRowID, latest.LastRowID)
	}
	if latest.LoadedLines != 0 || latest.LogicalTotal != 3 || latest.TotalRows != 4 || !latest.HasMore {
		t.Fatalf("expected clipped reclaimed line to preserve pagination signal without loaded line start, loaded=%d total=%d rows=%d has_more=%v", latest.LoadedLines, latest.LogicalTotal, latest.TotalRows, latest.HasMore)
	}
	if latest.FirstLineID != 0 || latest.LastLineID != 0 {
		t.Fatalf("expected clipped-before reclaimed line not to expose loaded line boundaries, first=%d last=%d", latest.FirstLineID, latest.LastLineID)
	}
	if len(latest.Rows) != 1 || latest.Rows[0].Ownership != RowOwnershipLiveTailReclaimed {
		t.Fatalf("expected latest row to stay reclaimed live-tail owned, rows=%#v", latest.Rows)
	}
	if len(latest.Lines) != 1 {
		t.Fatalf("expected one clipped reclaimed line span, got %#v", latest.Lines)
	}
	span := latest.Lines[0]
	if span.StartRow != 0 || span.EndRow != 0 || span.RowKind != "reclaimed-line" || span.LogicalLineID != 3 || !span.ClippedBefore || span.ClippedAfter {
		t.Fatalf("expected latest limit to mark reclaimed line clipped-before only, got %#v", span)
	}

	older, err := historyCombinedGridViewportFromStore(store, latest.BeforeOffset, 10, 4, tail)
	if err != nil {
		t.Fatalf("older viewport after clipped reclaimed latest: %v", err)
	}
	if got := vtermRowsToStrings(older.Rows); !reflect.DeepEqual(got, []string{"p0", "p1", "r0"}) {
		t.Fatalf("expected older request to return unexposed reclaimed prefix without repeating r1, got %#v", got)
	}
	if older.LoadedRows != 4 {
		t.Fatalf("expected older cursor to reach full committed depth, got %d", older.LoadedRows)
	}
}

func TestTerminalLatestCombinedViewportDoesNotHidePersistedRowsForNonAuthoritativeReclaimedTail(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("p0"), wrapped: false},
		{cells: localVTermCellsFromString("r0"), wrapped: false},
		{cells: localVTermCellsFromString("r1"), wrapped: false},
	})
	var tail terminalPrimaryLiveTail
	tail.replaceReclaimedPrefix([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("r0"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("r1"), WrappedSet: true, Wrapped: false},
	}, store.generation, 1, 2)

	viewport, err := historyCombinedGridViewportFromStore(store, 0, 10, 20, tail)
	if err != nil {
		t.Fatalf("combined viewport: %v", err)
	}
	window := historyWindowFromCoreGridViewport("non-authoritative-reclaimed", 0, viewport)
	if got := historyWindowRowTexts(window); !reflect.DeepEqual(got, []string{"p0", "r0", "r1"}) {
		t.Fatalf("expected non-authoritative reclaimed rows not to hide persisted authoritative rows, got %#v window=%#v", got, window)
	}
	if window.LoadedRows != 3 || window.LogicalTotal != 3 || window.BeforeOffset != 3 {
		t.Fatalf("expected persisted authoritative depth to stay intact, loaded=%d total=%d before=%d window=%#v", window.LoadedRows, window.LogicalTotal, window.BeforeOffset, window)
	}
}

func TestTerminalLatestCombinedViewportLogicalTotalUsesProjectionIDsNotRecoverableRecords(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("persisted"), wrapped: false},
	})

	tail := terminalPrimaryLiveTail{segments: []terminalLiveTailSegment{
		{
			origin:         terminalLiveTailOriginLive,
			sealState:      terminalLiveTailSealed,
			rows:           []localvterm.DamageOp{{Cells: localVTermCellsFromString("live0"), WrappedSet: true, Wrapped: false}},
			logicalLineIDs: []uint64{terminalLiveTailLogicalLineIDBase + 1},
		},
		{
			origin:         terminalLiveTailOriginLive,
			sealState:      terminalLiveTailSealed,
			rows:           []localvterm.DamageOp{{Cells: localVTermCellsFromString("lost0"), WrappedSet: true, Wrapped: false}},
			logicalLineIDs: []uint64{0},
		},
	}}
	if records := tail.logicalLineRecords(); len(records) != 0 {
		t.Fatalf("expected partial live tail metadata records to be suppressed, got %#v", records)
	}

	viewport, err := combinedGridViewportFromStore(store, 0, 10, 20, tail)
	if err != nil {
		t.Fatalf("combined viewport: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"persisted", "live0", "lost0"}) {
		t.Fatalf("expected visible persisted row plus both live tail rows, got %#v", got)
	}
	if viewport.WindowLogicalTotal != 2 {
		t.Fatalf("expected logical total to count persisted plus stable projected live line only, got %d", viewport.WindowLogicalTotal)
	}
	window := historyWindowFromCoreGridViewport("projection-total", 0, viewport)
	if got := historyWindowRowTexts(window); !reflect.DeepEqual(got, []string{"persisted", "live0"}) {
		t.Fatalf("expected history window to omit rows without authoritative logical line ids, got %#v", got)
	}
	if window.LoadedLines != 2 || window.LogicalTotal != 2 {
		t.Fatalf("expected history window to ignore rows without authoritative logical line ids, loaded=%d total=%d lines=%#v", window.LoadedLines, window.LogicalTotal, window.Lines)
	}
}

func TestTerminalMetadataOnlyLatestSnapshotKeepsCommittedWindowCanonicalOnly(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "metadata-only-latest-persisted-only",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("hist"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail0"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail1"), WrappedSet: true, Wrapped: true},
		},
		LiveTailAppendRows: 2,
	}
	term.appendGridFromDamageLocked(damage)

	_, generation, persistedRows := store.coordinates()
	if persistedRows != 1 {
		t.Fatalf("expected one committed persisted row in setup, got %d", persistedRows)
	}

	snapshot := term.Snapshot(0, 0)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if got := rowsToStrings(snapshot.Scrollback); len(got) != 0 {
		t.Fatalf("expected metadata-only latest snapshot to omit materialized scrollback rows, got %#v", got)
	}
	if got, want := snapshot.ScrollbackTotal, 3; got != want {
		t.Fatalf("expected latest total to include live tail, got %d want %d", got, want)
	}
	if !snapshot.ScrollbackHasMore {
		t.Fatal("expected latest metadata-only snapshot to report available history")
	}
	if got, want := snapshot.ScrollbackLoadedRows, 0; got != want {
		t.Fatalf("expected metadata-only latest snapshot to keep committed window unloaded, got %d want %d", got, want)
	}
	if got, want := snapshot.HistoryGeneration, generation; got != want {
		t.Fatalf("expected committed canonical generation %d, got %d", want, got)
	}
	if got, want := snapshot.ScrollbackFirstRowID, uint64(0); got != want {
		t.Fatalf("expected committed canonical first row id %d, got %d", want, got)
	}
	if got, want := snapshot.ScrollbackLastRowID, uint64(0); got != want {
		t.Fatalf("expected committed canonical last row id %d, got %d", want, got)
	}
}

func TestTerminalMetadataOnlyOlderSnapshotUsesCommittedDepthWithoutLiveTailShift(t *testing.T) {
	vt := localvterm.New(16, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	rows := make([][]localvterm.Cell, 0, 13000)
	for i := 0; i < 13000; i++ {
		rows = append(rows, localVTermCellsFromString(rowLabel("persisted", i)))
	}
	if err := store.AppendRows(rows); err != nil {
		t.Fatalf("append persisted rows: %v", err)
	}
	term := &Terminal{
		id:    "metadata-only-older-persisted-depth-only",
		size:  Size{Cols: 16, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	term.primaryLiveTail.replaceRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("live-tail-0"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("live-tail-1"), WrappedSet: true, Wrapped: false},
	}, terminalLiveTailOriginLive, false)

	snapshot := term.Snapshot(12000, 0)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if got := rowsToStrings(snapshot.Scrollback); len(got) != 0 {
		t.Fatalf("expected metadata-only older snapshot to omit materialized scrollback rows, got %#v", got)
	}
	if got, want := snapshot.ScrollbackTotal, 13000; got != want {
		t.Fatalf("expected older metadata total to stay on committed persisted depth, got %d want %d", got, want)
	}
	if !snapshot.ScrollbackHasMore {
		t.Fatal("expected older metadata-only snapshot to report older committed rows still available")
	}
	if got, want := snapshot.ScrollbackLoadedRows, 12000; got != want {
		t.Fatalf("expected older metadata-only loaded depth %d, got %d", want, got)
	}
	if snapshot.HistoryGeneration == 0 {
		t.Fatal("expected older metadata-only snapshot to keep committed history generation")
	}
	if got, want := snapshot.ScrollbackFirstRowID, uint64(0); got != want {
		t.Fatalf("expected older metadata window first row id %d, got %d", want, got)
	}
	if got, want := snapshot.ScrollbackLastRowID, uint64(999); got != want {
		t.Fatalf("expected older metadata window last row id %d, got %d", want, got)
	}
}

func TestTerminalMetadataOnlyExhaustedOlderSnapshotKeepsGenerationLikeGridViewport(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	if err := store.AppendRows([][]localvterm.Cell{localVTermCellsFromString("hist0")}); err != nil {
		t.Fatalf("append persisted row: %v", err)
	}
	term := &Terminal{
		id:    "metadata-only-exhausted-older-generation",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 1, ScrollbackLimit: 10, Cols: 4})
	if viewport == nil {
		t.Fatal("expected viewport")
	}
	if viewport.HistoryGeneration == 0 {
		t.Fatal("expected exhausted older viewport to keep committed generation")
	}

	snapshot := term.Snapshot(1, 0)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.HistoryGeneration != viewport.HistoryGeneration {
		t.Fatalf("expected metadata-only exhausted older snapshot to keep generation %d like grid viewport, got %d", viewport.HistoryGeneration, snapshot.HistoryGeneration)
	}
	if got, want := snapshot.ScrollbackLoadedRows, 1; got != want {
		t.Fatalf("expected exhausted older snapshot loaded depth %d, got %d", want, got)
	}
	if snapshot.ScrollbackFirstRowID != 0 || snapshot.ScrollbackLastRowID != 0 {
		t.Fatalf("expected exhausted older metadata snapshot to keep canonical row window 0..0, got %d..%d", snapshot.ScrollbackFirstRowID, snapshot.ScrollbackLastRowID)
	}
}

func TestTerminalMetadataOnlyLiveTailOnlySnapshotDoesNotInventCanonicalMetadata(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "metadata-only-live-tail-only-no-canonical",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("tail0"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail1"), WrappedSet: true, Wrapped: true},
		},
		LiveTailAppendRows: 2,
	}
	term.appendGridFromDamageLocked(damage)

	snapshot := term.Snapshot(0, 0)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if got := rowsToStrings(snapshot.Scrollback); len(got) != 0 {
		t.Fatalf("expected metadata-only live-tail-only snapshot to omit materialized rows, got %#v", got)
	}
	if got, want := snapshot.ScrollbackTotal, 2; got != want {
		t.Fatalf("expected latest total to include live-tail-only rows, got %d want %d", got, want)
	}
	if !snapshot.ScrollbackHasMore {
		t.Fatal("expected live-tail-only metadata snapshot to report latest rows available")
	}
	if snapshot.ScrollbackLoadedRows != 0 {
		t.Fatalf("expected no committed loaded rows for live-tail-only metadata snapshot, got %d", snapshot.ScrollbackLoadedRows)
	}
	if snapshot.HistoryGeneration != 0 {
		t.Fatalf("expected no canonical generation for live-tail-only metadata snapshot, got %d", snapshot.HistoryGeneration)
	}
	if snapshot.ScrollbackFirstRowID != 0 || snapshot.ScrollbackLastRowID != 0 {
		t.Fatalf("expected no canonical row window for live-tail-only metadata snapshot, got %d..%d", snapshot.ScrollbackFirstRowID, snapshot.ScrollbackLastRowID)
	}
}

func TestTerminalLatestGridViewportExpandsLiveTailStartByLogicalLineID(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "live-tail-start-expands",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("hist"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail0"), WrappedSet: true, Wrapped: true},
			{Cells: localVTermCellsFromString("tail1"), WrappedSet: true, Wrapped: true},
		},
		LiveTailAppendRows: 2,
	}
	term.appendGridFromDamageLocked(damage)

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 1, Cols: 4})
	if viewport == nil {
		t.Fatal("expected viewport")
	}
	if got := rowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"tail0", "tail1"}) {
		t.Fatalf("expected viewport start inside live-tail continuation to expand back to live-tail logical-line start, got %#v", got)
	}
	if !reflect.DeepEqual(viewport.ScrollbackWrapped, []bool{true, true}) {
		t.Fatalf("expected wrapped metadata for expanded live-tail logical line, got %#v", viewport.ScrollbackWrapped)
	}
}

func TestExpandDamageWindowStartToLogicalLinePrefersLogicalLineIDs(t *testing.T) {
	rows := []localvterm.DamageOp{
		{Cells: localVTermCellsFromString("tail0"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("tail1"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("next"), WrappedSet: true, Wrapped: false},
	}
	lineID := terminalLiveTailLogicalLineIDBase + 10
	if got := expandDamageWindowStartToLogicalLine(rows, []uint64{lineID, lineID, lineID + 1}, 1); got != 0 {
		t.Fatalf("expected same logical line id to expand start to 0 despite wrapped=false, got %d", got)
	}
	if got := expandDamageWindowStartToLogicalLine(rows, []uint64{lineID, lineID, lineID + 1}, 2); got != 2 {
		t.Fatalf("expected distinct logical line id to keep start at 2, got %d", got)
	}
}

func TestExpandDamageWindowStartToLogicalLineDoesNotInferFromWrappedRows(t *testing.T) {
	rows := []localvterm.DamageOp{
		{Cells: localVTermCellsFromString("tail0"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("tail1"), WrappedSet: true, Wrapped: true},
	}
	if got := expandDamageWindowStartToLogicalLine(rows, nil, 1); got != 1 {
		t.Fatalf("expected missing logical line ids not to expand from wrapped rows, got %d", got)
	}
	if got := expandDamageWindowStartToLogicalLine(rows, []uint64{0, 0}, 1); got != 1 {
		t.Fatalf("expected zero logical line ids not to expand from wrapped rows, got %d", got)
	}
}

func TestTerminalLatestProjectionMaterializesLiveTailRunsToCells(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "live-tail-runs-materialize",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}
	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{{
			Runs: []localvterm.CellRun{
				{Style: localvterm.CellStyle{FG: "ansi:2", Bold: true}, Text: "ab"},
				{Text: "cd"},
			},
		}},
		LiveTailAppendRows: 1,
	}
	term.appendGridFromDamageLocked(damage)

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 10, Cols: 4})
	if viewport == nil {
		t.Fatal("expected viewport")
	}
	if got := rowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"abcd"}) {
		t.Fatalf("expected live-tail runs to materialize into visible cells, got %#v", got)
	}
	if len(viewport.Rows) != 1 || len(viewport.Rows[0]) != 4 {
		t.Fatalf("expected one 4-cell row materialized from live-tail runs, got %#v", viewport.Rows)
	}
	for i := 0; i < 2; i++ {
		if got := viewport.Rows[0][i].Style; got.FG != "ansi:2" || !got.Bold {
			t.Fatalf("expected styled run cell %d to survive live-tail materialization, got %#v", i, got)
		}
	}
	for i := 2; i < 4; i++ {
		if got := viewport.Rows[0][i].Style; got != (CellStyle{}) {
			t.Fatalf("expected unstyled run cell %d to remain default after materialization, got %#v", i, got)
		}
	}
}

func rowsToStrings(rows [][]Cell) []string {
	if len(rows) == 0 {
		return nil
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowToString(row))
	}
	return out
}

func rowLabel(prefix string, idx int) string {
	return prefix + "-" + fmt.Sprintf("%05d", idx)
}
