package termx

import (
	"fmt"
	"reflect"
	"testing"

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
			{Cells: localVTermCellsFromString("tail0"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail1"), WrappedSet: true, Wrapped: true},
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

func TestTerminalLatestGridViewportExpandsLiveTailWrappedStartToLogicalLine(t *testing.T) {
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
