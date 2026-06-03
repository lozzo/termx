package termx

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-vterm/vterm"
)

func TestHistoryLineSpansUseLogicalLineIDs(t *testing.T) {
	spans := historyLineSpans([]bool{false, false, false}, []string{"a", "a", "b"}, []uint64{101, 101, 103}, 3, false)
	want := []HistoryLineSpan{
		{StartRow: 0, EndRow: 1, RowKind: "a", LogicalLineID: 101},
		{StartRow: 2, EndRow: 2, RowKind: "b", LogicalLineID: 103},
	}
	if !reflect.DeepEqual(spans, want) {
		t.Fatalf("expected same logical line id to group spans despite wrapped=false, got %#v want %#v", spans, want)
	}

	spans = historyLineSpans([]bool{true, false}, []string{"a", "b"}, []uint64{201, 202}, 2, false)
	want = []HistoryLineSpan{
		{StartRow: 0, EndRow: 0, RowKind: "a", LogicalLineID: 201},
		{StartRow: 1, EndRow: 1, RowKind: "b", LogicalLineID: 202},
	}
	if !reflect.DeepEqual(spans, want) {
		t.Fatalf("expected distinct logical line ids to split spans despite wrapped=true, got %#v want %#v", spans, want)
	}
}

func TestHistoryLineSpansDoNotInferFromWrappedRowsWithoutLogicalLineIDs(t *testing.T) {
	spans := historyLineSpans([]bool{true, false}, []string{"a", "a"}, nil, 2, false)
	if len(spans) != 0 {
		t.Fatalf("expected no authoritative line spans without logical line ids, got %#v", spans)
	}
}

func TestHistoryWindowFiltersRowsWithoutAuthoritativeLogicalLineIDs(t *testing.T) {
	viewport := terminalGridViewport{
		Rows:           [][]vterm.Cell{vtermCells("a"), vtermCells("lost"), vtermCells("b")},
		Timestamps:     []time.Time{time.Unix(1, 0), time.Unix(2, 0), time.Unix(3, 0)},
		RowKinds:       []string{"a", "lost", "b"},
		Wrapped:        []bool{false, true, false},
		Ownership:      []string{RowOwnershipPersisted, RowOwnershipLiveTailLive, RowOwnershipLiveTailLive},
		LogicalLineIDs: []uint64{10, 0, 11},
		LoadedRows:     1,
		TotalRows:      3,
		LogicalTotal:   2,
	}
	window := historyWindowFromCoreGridViewport("filter-missing-line-id", 0, viewport)
	if got := historyWindowRowTexts(window); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("expected rows without authoritative logical line ids to be omitted, got %#v", got)
	}
	if len(window.Lines) != 2 || window.Lines[0].StartRow != 0 || window.Lines[1].StartRow != 1 {
		t.Fatalf("expected line spans to cover filtered rows contiguously, got %#v", window.Lines)
	}
	if window.TotalRows != 2 {
		t.Fatalf("expected total rows to drop omitted non-authoritative row, got %d", window.TotalRows)
	}
}

func TestHistoryWindowDropsMissingLogicalLineIDsFromLogicalTotal(t *testing.T) {
	viewport := terminalGridViewport{
		Rows:           [][]vterm.Cell{vtermCells("a"), vtermCells("lost"), vtermCells("b")},
		Ownership:      []string{RowOwnershipPersisted, RowOwnershipPersisted, RowOwnershipPersisted},
		LogicalLineIDs: []uint64{10, 0, 11},
		LoadedRows:     3,
		TotalRows:      3,
		LogicalTotal:   3,
		Generation:     7,
		FirstRowID:     20,
		LastRowID:      22,
	}

	window := historyWindowFromCoreGridViewport("filter-total-missing-line-id", 0, viewport)
	if got := historyWindowRowTexts(window); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("expected only authoritative rows after filtering, got %#v", got)
	}
	if window.LoadedRows != 2 || window.TotalRows != 2 {
		t.Fatalf("expected loaded and total rows to drop non-authoritative row, loaded=%d total=%d", window.LoadedRows, window.TotalRows)
	}
	if window.LogicalTotal != 2 {
		t.Fatalf("expected logical total to drop missing-id logical row, got %d", window.LogicalTotal)
	}
}

func TestHistoryWindowCountsSeparateMissingLogicalLineIDSegments(t *testing.T) {
	viewport := terminalGridViewport{
		Rows:           [][]vterm.Cell{vtermCells("lost-a"), vtermCells("kept"), vtermCells("lost-b")},
		Ownership:      []string{RowOwnershipPersisted, RowOwnershipPersisted, RowOwnershipPersisted},
		LogicalLineIDs: []uint64{0, 10, 0},
		LoadedRows:     3,
		TotalRows:      3,
		LogicalTotal:   3,
	}

	window := historyWindowFromCoreGridViewport("filter-total-separated-missing-line-id", 0, viewport)
	if got := historyWindowRowTexts(window); !reflect.DeepEqual(got, []string{"kept"}) {
		t.Fatalf("expected only stable-id row after filtering, got %#v", got)
	}
	if window.LogicalTotal != 1 {
		t.Fatalf("expected logical total to drop both separate missing-id segments, got %d", window.LogicalTotal)
	}
}

func TestHistoryWindowDropsDistinctFallbackLogicalLinesFromLogicalTotal(t *testing.T) {
	viewport := terminalGridViewport{
		Rows:                       [][]vterm.Cell{vtermCells("fallback-a"), vtermCells("fallback-b"), vtermCells("kept")},
		Ownership:                  []string{RowOwnershipPersisted, RowOwnershipPersisted, RowOwnershipPersisted},
		LogicalLineIDs:             []uint64{10, 11, 12},
		LogicalLineIDAuthoritative: []bool{false, false, true},
		LoadedRows:                 3,
		TotalRows:                  3,
		LogicalTotal:               3,
	}

	window := historyWindowFromCoreGridViewport("filter-total-fallback-lines", 0, viewport)
	if got := historyWindowRowTexts(window); !reflect.DeepEqual(got, []string{"kept"}) {
		t.Fatalf("expected only authoritative row after filtering, got %#v", got)
	}
	if window.LogicalTotal != 1 {
		t.Fatalf("expected logical total to drop both fallback logical lines, got %d", window.LogicalTotal)
	}
	if window.LoadedRows != 1 || window.TotalRows != 1 {
		t.Fatalf("expected loaded and total rows to drop fallback rows, loaded=%d total=%d", window.LoadedRows, window.TotalRows)
	}
}

func TestHistoryWindowClearsBoundaryWhenAllRowsLackAuthoritativeLogicalLineIDs(t *testing.T) {
	viewport := terminalGridViewport{
		Rows:           [][]vterm.Cell{vtermCells("lost-a"), vtermCells("lost-b")},
		Timestamps:     []time.Time{time.Unix(1, 0), time.Unix(2, 0)},
		RowKinds:       []string{"lost", "lost"},
		Wrapped:        []bool{false, false},
		Ownership:      []string{RowOwnershipPersisted, RowOwnershipLiveTailReclaimed},
		LogicalLineIDs: []uint64{0, 0},
		BeforeOffset:   4,
		LoadedRows:     6,
		TotalRows:      6,
		LogicalTotal:   3,
		Generation:     7,
		FirstRowID:     10,
		LastRowID:      11,
		HasMore:        true,
	}

	window := historyWindowFromCoreGridViewport("filter-all-missing-line-id", 4, viewport)
	if len(window.Rows) != 0 || len(window.Lines) != 0 {
		t.Fatalf("expected all non-authoritative rows to be omitted, rows=%#v lines=%#v", window.Rows, window.Lines)
	}
	if window.Token != "" || window.Generation != 0 || window.FirstRowID != 0 || window.LastRowID != 0 || window.FirstLineID != 0 || window.LastLineID != 0 {
		t.Fatalf("expected empty authoritative window to clear boundary metadata, got %#v", window)
	}
	if window.TotalRows != 0 || window.LogicalTotal != 0 || window.LoadedRows != 4 || window.BeforeOffset != 4 || window.HasMore {
		t.Fatalf("expected empty authoritative window to keep only request cursor, got loaded=%d before=%d total=%d logical=%d has_more=%v", window.LoadedRows, window.BeforeOffset, window.TotalRows, window.LogicalTotal, window.HasMore)
	}
}

func TestHistoryWindowDoesNotMergeLogicalLineAcrossFilteredRows(t *testing.T) {
	viewport := terminalGridViewport{
		Rows:           [][]vterm.Cell{vtermCells("a"), vtermCells("lost"), vtermCells("b"), vtermCells("c")},
		Timestamps:     []time.Time{time.Unix(1, 0), time.Unix(2, 0), time.Unix(3, 0), time.Unix(4, 0)},
		RowKinds:       []string{"line-a", "lost", "line-b", "line-c"},
		Wrapped:        []bool{true, true, false, false},
		Ownership:      []string{RowOwnershipPersisted, RowOwnershipPersisted, RowOwnershipPersisted, RowOwnershipPersisted},
		LogicalLineIDs: []uint64{10, 0, 10, 11},
		LoadedRows:     4,
		TotalRows:      4,
		LogicalTotal:   2,
		Generation:     7,
		FirstRowID:     20,
		LastRowID:      23,
	}

	window := historyWindowFromCoreGridViewport("filter-discontiguous-line-id", 0, viewport)
	if got := historyWindowRowTexts(window); !reflect.DeepEqual(got, []string{"c"}) {
		t.Fatalf("expected discontiguous logical line id rows to be omitted, got %#v", got)
	}
	if len(window.Lines) != 1 || window.Lines[0].LogicalLineID != 11 || window.Lines[0].StartRow != 0 || window.Lines[0].EndRow != 0 {
		t.Fatalf("expected only unaffected logical line span, got %#v", window.Lines)
	}
	if window.LoadedRows != 1 || window.TotalRows != 1 {
		t.Fatalf("expected committed depth and total rows to drop filtered rows, loaded=%d total=%d", window.LoadedRows, window.TotalRows)
	}
	if window.LogicalTotal != 1 {
		t.Fatalf("expected logical total to drop discontiguous filtered logical line id, got %d", window.LogicalTotal)
	}
}

func TestHistoryWindowFiltersRowIDBoundaryWithRows(t *testing.T) {
	viewport := terminalGridViewport{
		Rows:                       [][]vterm.Cell{vtermCells("fallback-a"), vtermCells("kept"), vtermCells("fallback-b")},
		Ownership:                  []string{RowOwnershipPersisted, RowOwnershipPersisted, RowOwnershipPersisted},
		LogicalLineIDs:             []uint64{10, 11, 12},
		LogicalLineIDAuthoritative: []bool{false, true, false},
		LoadedRows:                 3,
		TotalRows:                  3,
		LogicalTotal:               3,
		Generation:                 7,
		FirstRowID:                 20,
		LastRowID:                  22,
	}

	window := historyWindowFromCoreGridViewport("filter-row-boundary", 0, viewport)
	if got := historyWindowRowTexts(window); !reflect.DeepEqual(got, []string{"kept"}) {
		t.Fatalf("expected only authoritative row after filtering, got %#v", got)
	}
	if window.FirstRowID != 21 || window.LastRowID != 21 {
		t.Fatalf("expected row boundaries to follow kept authoritative row, first=%d last=%d window=%#v", window.FirstRowID, window.LastRowID, window)
	}
}

func TestHistoryWindowRejectsFallbackOnlyPersistedLogicalLineIDs(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	if err := store.AppendRows([][]vterm.Cell{vtermCells("fallback")}); err != nil {
		t.Fatalf("append fallback persisted row: %v", err)
	}

	viewport, err := store.Viewport(0, 10, 10)
	if err != nil {
		t.Fatalf("fallback viewport: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"fallback"}) {
		t.Fatalf("expected legacy viewport to keep fallback row, got %#v", got)
	}
	if got := viewport.LogicalLineIDs; !reflect.DeepEqual(got, []uint64{1}) {
		t.Fatalf("expected legacy viewport fallback line id, got %#v", got)
	}
	if got := viewport.LogicalLineIDAuthoritative; !reflect.DeepEqual(got, []bool{false}) {
		t.Fatalf("expected fallback line id to be marked non-authoritative, got %#v", got)
	}

	window := historyWindowFromCoreGridViewport("fallback-only", 0, viewport)
	if len(window.Rows) != 0 || len(window.Lines) != 0 {
		t.Fatalf("expected authoritative history window to reject fallback-only row, rows=%#v lines=%#v", window.Rows, window.Lines)
	}
	if window.Token != "" || window.LogicalTotal != 0 || window.TotalRows != 0 {
		t.Fatalf("expected fallback-only history window to clear authoritative boundary, got %#v", window)
	}
}

func TestServerHistoryWindowLogicalTotalIgnoresWindowExternalFallback(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "history-total-explicit-tail")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	if err := store.AppendRows([][]vterm.Cell{vtermCells("fallback-prefix")}); err != nil {
		t.Fatalf("append fallback prefix: %v", err)
	}
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: vtermCells("explicit-tail")},
	})
	if err := store.Close(); err != nil {
		t.Fatalf("close grid store: %v", err)
	}

	srv := NewServer(WithGridRoot(root), WithDefaultSize(20, 2))
	window, err := srv.HistoryWindow(ctx, "history-total-explicit-tail", HistoryWindowOptions{Limit: 1, Cols: 20})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if got := historyWindowRowTexts(window); !reflect.DeepEqual(got, []string{"explicit-tail"}) {
		t.Fatalf("expected latest authoritative window to expose explicit tail only, got %#v", got)
	}
	if window.LogicalTotal != 1 || window.LoadedLines != 1 {
		t.Fatalf("expected logical totals to ignore fallback prefix, loaded=%d total=%d window=%#v", window.LoadedLines, window.LogicalTotal, window)
	}
	if window.HasMore || window.TotalRows != 1 {
		t.Fatalf("expected fallback prefix not to expose authoritative older boundary, has_more=%v total_rows=%d window=%#v", window.HasMore, window.TotalRows, window)
	}
}

func TestHistoryWindowFindsDiscontiguousLogicalLineIDs(t *testing.T) {
	got := historyWindowDiscontiguousLogicalLineIDs([]uint64{10, 10, 0, 10, 11, 11, 12, 11}, 8)
	if _, ok := got[10]; !ok {
		t.Fatalf("expected logical line id 10 to be marked discontiguous, got %#v", got)
	}
	if _, ok := got[11]; !ok {
		t.Fatalf("expected logical line id 11 to be marked discontiguous, got %#v", got)
	}
	if _, ok := got[12]; ok {
		t.Fatalf("expected contiguous single id 12 not to be marked invalid, got %#v", got)
	}
}

func TestViewportTrimDoesNotInferClippedLineFromWrappedRowsWithoutLogicalLineIDs(t *testing.T) {
	viewport := terminalGridViewport{
		Rows:           [][]vterm.Cell{vtermCells("old"), vtermCells("new")},
		Timestamps:     []time.Time{time.Unix(1, 0), time.Unix(2, 0)},
		RowKinds:       []string{"old", "new"},
		Wrapped:        []bool{true, false},
		Ownership:      []string{RowOwnershipPersisted, RowOwnershipPersisted},
		LogicalLineIDs: []uint64{0, 0},
		LoadedRows:     2,
		FirstRowID:     10,
		LastRowID:      11,
	}

	if cropped := trimTerminalGridViewportToTail(&viewport, 1); !cropped {
		t.Fatal("expected viewport trim")
	}
	if viewport.FirstLineClippedBefore {
		t.Fatalf("expected missing logical line ids not to infer clipped-before from wrapped rows, got %#v", viewport)
	}
}

func TestViewportTrimDoesNotClipFromFallbackLogicalLineIDs(t *testing.T) {
	viewport := terminalGridViewport{
		Rows:                       [][]vterm.Cell{vtermCells("old"), vtermCells("new")},
		Timestamps:                 []time.Time{time.Unix(1, 0), time.Unix(2, 0)},
		RowKinds:                   []string{"fallback", ""},
		Wrapped:                    []bool{true, false},
		Ownership:                  []string{RowOwnershipPersisted, RowOwnershipPersisted},
		LogicalLineIDs:             []uint64{10, 10},
		LogicalLineIDAuthoritative: []bool{false, false},
		LoadedRows:                 2,
		FirstRowID:                 10,
		LastRowID:                  11,
	}

	if cropped := trimTerminalGridViewportToTail(&viewport, 1); !cropped {
		t.Fatal("expected viewport trim")
	}
	if viewport.FirstLineClippedBefore {
		t.Fatalf("expected fallback logical line id not to mark clipped-before, got %#v", viewport)
	}
	if got := stringAt(viewport.RowKinds, 0); got != "" {
		t.Fatalf("expected fallback logical line id not to inherit row kind, got %q", got)
	}
}

func TestViewportReclaimStartUsesLogicalLineIDs(t *testing.T) {
	viewport := terminalGridViewport{
		Rows:           [][]vterm.Cell{vtermCells("old"), vtermCells("new"), vtermCells("tail")},
		Wrapped:        []bool{false, false, false},
		LogicalLineIDs: []uint64{10, 10, 11},
	}
	if got := terminalGridViewportReclaimStart(viewport, 2); got != 0 {
		t.Fatalf("expected reclaim start to expand to logical line start by id, got %d", got)
	}
}

func TestViewportReclaimStartDoesNotInferFromWrappedWithoutLogicalLineIDs(t *testing.T) {
	viewport := terminalGridViewport{
		Rows:           [][]vterm.Cell{vtermCells("old"), vtermCells("new"), vtermCells("tail")},
		Wrapped:        []bool{true, true, false},
		LogicalLineIDs: []uint64{0, 0, 0},
	}
	if got := terminalGridViewportReclaimStart(viewport, 2); got != 1 {
		t.Fatalf("expected missing logical line ids not to expand reclaim start from wrapped rows, got %d", got)
	}
}

func TestViewportReclaimStartDoesNotUseFallbackLogicalLineIDs(t *testing.T) {
	viewport := terminalGridViewport{
		Rows:                       [][]vterm.Cell{vtermCells("old"), vtermCells("new"), vtermCells("tail")},
		Wrapped:                    []bool{false, false, false},
		LogicalLineIDs:             []uint64{10, 10, 11},
		LogicalLineIDAuthoritative: []bool{false, false, true},
	}
	if got := terminalGridViewportReclaimStart(viewport, 2); got != 1 {
		t.Fatalf("expected fallback logical line ids not to expand reclaim start, got %d", got)
	}
}

func TestClippedViewportLeadingRowKindUsesLogicalLineIDs(t *testing.T) {
	rowKinds := []string{"line", "", "next"}
	if got := clippedViewportLeadingRowKind(rowKinds, []uint64{10, 10, 11}, nil, 1); got != "line" {
		t.Fatalf("expected row kind inherited within same logical line id, got %q", got)
	}
	if got := clippedViewportLeadingRowKind(rowKinds, []uint64{0, 0, 11}, nil, 1); got != "" {
		t.Fatalf("expected missing logical line ids not to inherit row kind from wrapped rows, got %q", got)
	}
	if got := clippedViewportLeadingRowKind(rowKinds, []uint64{10, 11, 11}, nil, 1); got != "" {
		t.Fatalf("expected distinct logical line ids not to inherit row kind, got %q", got)
	}
	if got := clippedViewportLeadingRowKind(rowKinds, []uint64{10, 10, 11}, []bool{false, false, true}, 1); got != "" {
		t.Fatalf("expected fallback logical line ids not to inherit row kind, got %q", got)
	}
}

func TestHistoryLineSpansTrailingWrappedDoesNotOverrun(t *testing.T) {
	// 末行即使 wrapped=true，也必须收口成一条逻辑行，不能越界。
	wrapped := []bool{false, true}
	spans := historyLineSpans(wrapped, nil, []uint64{11, 12}, 2, false)
	want := []HistoryLineSpan{
		{StartRow: 0, EndRow: 0, LogicalLineID: 11},
		{StartRow: 1, EndRow: 1, LogicalLineID: 12, ClippedAfter: true},
	}
	if !reflect.DeepEqual(spans, want) {
		t.Fatalf("unexpected trailing wrapped spans, got %#v want %#v", spans, want)
	}
}

func TestHistoryLineSpansMarksWindowClipping(t *testing.T) {
	spans := historyLineSpans([]bool{false, true}, []string{"a", "b"}, []uint64{20, 21}, 2, false)
	want := []HistoryLineSpan{
		{StartRow: 0, EndRow: 0, RowKind: "a", LogicalLineID: 20},
		{StartRow: 1, EndRow: 1, RowKind: "b", LogicalLineID: 21, ClippedAfter: true},
	}
	if !reflect.DeepEqual(spans, want) {
		t.Fatalf("expected older offset alone not to mark clipped-before, got %#v want %#v", spans, want)
	}

	spans = historyLineSpans([]bool{false, true}, []string{"a", "b"}, []uint64{20, 21}, 2, true)
	want = []HistoryLineSpan{
		{StartRow: 0, EndRow: 0, RowKind: "a", LogicalLineID: 20, ClippedBefore: true},
		{StartRow: 1, EndRow: 1, RowKind: "b", LogicalLineID: 21, ClippedAfter: true},
	}
	if !reflect.DeepEqual(spans, want) {
		t.Fatalf("expected explicit viewport clipping to mark clipped-before, got %#v want %#v", spans, want)
	}
}

func TestHistoryWindowOpForOffset(t *testing.T) {
	if got := historyWindowOpForOffset(0); got != HistoryWindowReplace {
		t.Fatalf("expected offset 0 to be replace, got %q", got)
	}
	if got := historyWindowOpForOffset(5); got != HistoryWindowPrepend {
		t.Fatalf("expected positive offset to be prepend, got %q", got)
	}
}

func TestServerHistoryWindowLatestReplaceFromPersistedStore(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "history-latest-1")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: vtermCells("alpha")},
		{cells: vtermCells("bravo")},
		{cells: vtermCells("charlie")},
	})
	if err := store.Close(); err != nil {
		t.Fatalf("close grid store: %v", err)
	}

	srv := NewServer(WithGridRoot(root), WithDefaultSize(10, 2))
	window, err := srv.HistoryWindow(ctx, "history-latest-1", HistoryWindowOptions{Limit: 10, Cols: 10})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if window.Op != HistoryWindowReplace {
		t.Fatalf("expected latest window to be replace, got %q", window.Op)
	}
	if window.Token == "" {
		t.Fatal("expected latest window token")
	}
	if len(window.Rows) != 3 {
		t.Fatalf("expected 3 projected rows, got %d", len(window.Rows))
	}
	if window.BeforeOffset != 3 {
		t.Fatalf("expected latest before cursor 3, got %d", window.BeforeOffset)
	}
	if len(window.Lines) != 3 {
		t.Fatalf("expected 3 logical line spans, got %#v", window.Lines)
	}
	if window.LoadedLines != 3 || window.FirstLineID == 0 || window.LastLineID == 0 || window.FirstLineID == window.LastLineID {
		t.Fatalf("expected explicit logical line boundaries, loaded=%d first=%d last=%d lines=%#v", window.LoadedLines, window.FirstLineID, window.LastLineID, window.Lines)
	}
	if got := rowTextFromHistoryRow(window.Rows[0]); got != "alpha" {
		t.Fatalf("expected first row alpha, got %q", got)
	}
}

func TestServerHistoryWindowOlderPrependUsesPersistedDepth(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "history-older-1")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	rows := make([]terminalGridRow, 0, 6)
	for _, text := range []string{"l0", "l1", "l2", "l3", "l4", "l5"} {
		rows = append(rows, terminalGridRow{cells: vtermCells(text)})
	}
	appendExplicitTerminalGridRowsForTest(t, store, rows)
	if err := store.Close(); err != nil {
		t.Fatalf("close grid store: %v", err)
	}

	srv := NewServer(WithGridRoot(root), WithDefaultSize(10, 2))
	older, err := srv.HistoryWindow(ctx, "history-older-1", HistoryWindowOptions{BeforeOffset: 2, Limit: 2, Cols: 10})
	if err != nil {
		t.Fatalf("older history window: %v", err)
	}
	if older.Op != HistoryWindowPrepend {
		t.Fatalf("expected older window to be prepend, got %q", older.Op)
	}
	if older.BeforeOffset != 4 {
		t.Fatalf("expected older before cursor 4, got %d", older.BeforeOffset)
	}
	if len(older.Rows) == 0 {
		t.Fatal("expected older window to contain rows")
	}
	if older.LoadedLines != 2 || len(older.Lines) != 2 {
		t.Fatalf("expected older window to count complete logical line starts, loaded=%d lines=%#v", older.LoadedLines, older.Lines)
	}
	if older.Lines[0].ClippedBefore {
		t.Fatalf("expected older offset at row boundary not to mark clipped-before, got %#v", older.Lines)
	}
}

func TestServerHistoryWindowTokenTracksBoundary(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "history-token-1")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	if err := store.appendRows([]terminalGridRow{
		{cells: vtermCells("one")},
		{cells: vtermCells("two")},
	}); err != nil {
		t.Fatalf("append grid rows: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close grid store: %v", err)
	}

	srv := NewServer(WithGridRoot(root), WithDefaultSize(10, 2))
	latest, err := srv.HistoryWindow(ctx, "history-token-1", HistoryWindowOptions{Limit: 10, Cols: 10})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	again, err := srv.HistoryWindow(ctx, "history-token-1", HistoryWindowOptions{Limit: 10, Cols: 10})
	if err != nil {
		t.Fatalf("history window repeat: %v", err)
	}
	if latest.Token != again.Token {
		t.Fatalf("expected identical boundary to yield identical token, got %q vs %q", latest.Token, again.Token)
	}
}

func TestHistoryWindowTokenIncludesLogicalLineBoundary(t *testing.T) {
	viewport := terminalGridViewport{
		Rows:           [][]vterm.Cell{vtermCells("row")},
		TotalRows:      1,
		Generation:     7,
		FirstRowID:     10,
		LastRowID:      10,
		Size:           Size{Cols: 4},
		LogicalLineIDs: []uint64{101},
	}
	token := historyWindowToken(viewport)
	viewport.LogicalLineIDs = []uint64{202}
	if got := historyWindowToken(viewport); got == token {
		t.Fatalf("expected logical line boundary to affect history window token, got %q", got)
	}
}

func TestHistoryWindowTokenIgnoresNonAuthoritativeLogicalLineIDs(t *testing.T) {
	viewport := terminalGridViewport{
		Rows:                       [][]vterm.Cell{vtermCells("fallback"), vtermCells("explicit")},
		TotalRows:                  2,
		Generation:                 7,
		FirstRowID:                 10,
		LastRowID:                  11,
		Size:                       Size{Cols: 4},
		LogicalLineIDs:             []uint64{101, 202},
		LogicalLineIDAuthoritative: []bool{false, true},
	}
	if got := historyWindowToken(viewport); !strings.Contains(got, ":l202-202:") {
		t.Fatalf("expected token to ignore non-authoritative fallback id, got %q", got)
	}
	viewport.LogicalLineIDs[0] = 303
	if got := historyWindowToken(viewport); !strings.Contains(got, ":l202-202:") {
		t.Fatalf("expected token to remain based on authoritative id only, got %q", got)
	}
}

func vtermCells(text string) []vterm.Cell {
	cells := make([]vterm.Cell, 0, len(text))
	for _, r := range text {
		cells = append(cells, vterm.Cell{Content: string(r), Width: 1})
	}
	return cells
}

func rowTextFromHistoryRow(row HistoryRow) string {
	var out string
	for _, cell := range row.Cells.DecodeCells() {
		out += cell.Content
	}
	return out
}

func TestProtocolHistoryWindowFromCoreMapsAllFields(t *testing.T) {
	core := &HistoryWindow{
		TerminalID:   "term-map",
		Token:        "g3:0-1:c40",
		Op:           HistoryWindowPrepend,
		Size:         Size{Cols: 40, Rows: 10},
		Rows:         []HistoryRow{{Cells: protocolCompactRowFromCoreWithOptions([]Cell{{Content: "x", Width: 1}}, true), RowKind: "output", Ownership: RowOwnershipPersisted, Wrapped: true}},
		Lines:        []HistoryLineSpan{{StartRow: 0, EndRow: 0, RowKind: "output", LogicalLineID: 42, ClippedBefore: true, ClippedAfter: true}},
		BeforeOffset: 2,
		LoadedRows:   6,
		LoadedLines:  1,
		TotalRows:    8,
		LogicalTotal: 3,
		HasMore:      true,
		Generation:   3,
		FirstRowID:   0,
		LastRowID:    1,
		FirstLineID:  42,
		LastLineID:   42,
	}
	got := protocolHistoryWindowFromCore(core)
	if got == nil {
		t.Fatal("expected non-nil protocol history window")
	}
	if got.TerminalID != "term-map" || got.Token != "g3:0-1:c40" || string(got.Op) != string(HistoryWindowPrepend) {
		t.Fatalf("unexpected mapped header: %#v", got)
	}
	if got.Size.Cols != 40 || got.Size.Rows != 10 {
		t.Fatalf("unexpected mapped size: %#v", got.Size)
	}
	if got.BeforeOffset != 2 || got.LoadedRows != 6 || got.TotalRows != 8 || got.LoadedLines != 1 || got.LogicalTotal != 3 || !got.HasMore {
		t.Fatalf("unexpected mapped metadata: %#v", got)
	}
	if got.Generation != 3 || got.FirstRowID != 0 || got.LastRowID != 1 || got.FirstLineID != 42 || got.LastLineID != 42 {
		t.Fatalf("unexpected mapped boundary: %#v", got)
	}
	if len(got.Rows) != 1 || len(got.RowKinds) != 1 || got.RowKinds[0] != "output" || len(got.RowWrapped) != 1 || !got.RowWrapped[0] || len(got.RowOwnership) != 1 || got.RowOwnership[0] != RowOwnershipPersisted {
		t.Fatalf("unexpected mapped row metadata: kinds=%#v wrapped=%#v ownership=%#v", got.RowKinds, got.RowWrapped, got.RowOwnership)
	}
	if len(got.Lines) != 1 || got.Lines[0].StartRow != 0 || got.Lines[0].EndRow != 0 || got.Lines[0].RowKind != "output" || got.Lines[0].LogicalLineID != 42 || !got.Lines[0].ClippedBefore || !got.Lines[0].ClippedAfter {
		t.Fatalf("unexpected mapped line spans: %#v", got.Lines)
	}
}
