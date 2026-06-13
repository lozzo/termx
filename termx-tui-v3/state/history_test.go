package state

import (
	"errors"
	"reflect"
	"testing"
)

func TestHistoryStoreAcceptsLatestReplace(t *testing.T) {
	store, err := (HistoryStore{}).BeginLatest(HistoryPendingRequest{
		ID:         1,
		TerminalID: "term-1",
		Cols:       80,
	})
	if err != nil {
		t.Fatalf("begin latest: %v", err)
	}

	window := historyWindow(HistoryWindowReplace, "term-1", "tok-1", 80, 7, []HistoryRow{
		{Text: "one", LineID: 10},
		{Text: "two", LineID: 11},
	})
	store, inserted, err := store.ApplyWindow(1, window)
	if err != nil {
		t.Fatalf("apply latest: %v", err)
	}

	if inserted != 2 {
		t.Fatalf("expected 2 inserted rows, got %d", inserted)
	}
	if store.Pending != nil {
		t.Fatal("expected pending request cleared")
	}
	if store.Token != "tok-1" || store.Generation != 7 || store.Cols != 80 {
		t.Fatalf("unexpected store header %#v", store)
	}
	if len(store.SourceLines) != 2 || store.SourceLines[0].Text != "one" || store.SourceLines[1].Text != "two" {
		t.Fatalf("expected frozen source lines, got %#v", store.SourceLines)
	}
	if got := rowTexts(store.Rows); !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Fatalf("unexpected rows %v", got)
	}

	store.Rows[0].Text = "mutated"
	if window.Rows[0].Text != "one" {
		t.Fatal("store should detach accepted rows")
	}
}

func TestHistoryStoreDetachesStyledRowCells(t *testing.T) {
	store, err := (HistoryStore{}).BeginLatest(HistoryPendingRequest{
		ID:         1,
		TerminalID: "term-1",
		Cols:       80,
	})
	if err != nil {
		t.Fatalf("begin latest: %v", err)
	}
	window := historyWindow(HistoryWindowReplace, "term-1", "tok-1", 80, 7, []HistoryRow{{
		Text:   "ERR 好",
		LineID: 10,
		Cells: []HistoryCell{
			{Text: "ERR", Width: 3, Style: HistoryCellStyle{FG: "ansi:1", Bold: true}},
			{Text: " ", Width: 1},
			{Text: "好", Width: 2, Style: HistoryCellStyle{FG: "#ffcc00", Underline: true}, LinkURL: "file://build.log", LinkParams: "line=7"},
		},
	}})

	store, _, err = store.ApplyWindow(1, window)
	if err != nil {
		t.Fatalf("apply latest: %v", err)
	}
	store.Rows[0].Cells[0].Text = "mutated"
	store.Rows[0].Cells[2].Style.FG = "ansi:2"
	if window.Rows[0].Cells[0].Text != "ERR" || window.Rows[0].Cells[2].Style.FG != "#ffcc00" {
		t.Fatalf("history rows should detach styled cells, window=%#v store=%#v", window.Rows[0].Cells, store.Rows[0].Cells)
	}
}

func TestHistoryStoreLatestReplaceReflowsFrozenSourceAtPendingCols(t *testing.T) {
	store, err := (HistoryStore{}).BeginLatest(HistoryPendingRequest{
		ID:         1,
		TerminalID: "term-1",
		Cols:       6,
	})
	if err != nil {
		t.Fatalf("begin latest: %v", err)
	}

	window := HistoryWindow{
		TerminalID: "term-1",
		Token:      "tok-1",
		Op:         HistoryWindowReplace,
		Cols:       3,
		SourceLines: []HistoryLogicalLine{{
			Text:   "abcdef",
			LineID: 10,
			Cells: []HistoryCell{
				{Text: "abc", Width: 3},
				{Text: "def", Width: 3},
			},
		}},
		Rows: []HistoryRow{
			{Text: "abc", LineID: 10, RowInLine: 0},
			{Text: "def", LineID: 10, RowInLine: 1},
		},
		Lines: []HistoryLineSpan{{LineID: 10, StartRow: 0, EndRow: 1}},
	}

	store, inserted, err := store.ApplyWindow(1, window)
	if err != nil {
		t.Fatalf("apply latest: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("expected inserted rows to reflect source response rows, got %d", inserted)
	}
	if store.Cols != 6 {
		t.Fatalf("expected history store to keep local pending cols, got %d", store.Cols)
	}
	if got := rowTexts(store.Rows); !reflect.DeepEqual(got, []string{"abcdef"}) {
		t.Fatalf("expected frozen source to reflow at local pending cols, got %v", got)
	}
}

func TestHistoryStorePrependsOlderAndRebasesExistingSpans(t *testing.T) {
	store := HistoryStore{
		TerminalID: "term-1",
		Token:      "tok-1",
		Cols:       80,
		Rows: []HistoryRow{
			{Text: "new", LineID: 20},
		},
		Lines:      []HistoryLineSpan{{LineID: 20, StartRow: 0, EndRow: 0}},
		Generation: 7,
		Boundary:   HistoryBoundary{FirstLineID: 20, LastLineID: 20},
	}
	cursor := HistoryCursor{Valid: true, BeforeLineID: 20}
	store, err := store.BeginOlder(HistoryPendingRequest{
		ID:         2,
		TerminalID: "term-1",
		Cols:       80,
		Token:      "tok-1",
		Generation: 7,
		Cursor:     cursor,
		Boundary:   store.Boundary,
	})
	if err != nil {
		t.Fatalf("begin older: %v", err)
	}

	window := historyWindow(HistoryWindowPrepend, "term-1", "tok-1", 80, 7, []HistoryRow{
		{Text: "old", LineID: 10},
	})
	window.Cursor = cursor
	window.Boundary = HistoryBoundary{FirstLineID: 10, LastLineID: 20}
	window.Lines = []HistoryLineSpan{{LineID: 10, StartRow: 0, EndRow: 0}}
	store, inserted, err := store.ApplyWindow(2, window)
	if err != nil {
		t.Fatalf("apply older: %v", err)
	}

	if inserted != 1 {
		t.Fatalf("expected 1 inserted row, got %d", inserted)
	}
	if got := rowTexts(store.Rows); !reflect.DeepEqual(got, []string{"old", "new"}) {
		t.Fatalf("unexpected rows %v", got)
	}
	if got := spanRows(store.Lines); !reflect.DeepEqual(got, []spanRow{{id: 10, start: 0, end: 0}, {id: 20, start: 1, end: 1}}) {
		t.Fatalf("unexpected rebased spans %v", got)
	}
}

func TestHistoryStorePrependMergesBoundaryOverlapForSameLogicalLine(t *testing.T) {
	store := HistoryStore{
		TerminalID: "term-1",
		Token:      "tok-1",
		Cols:       6,
		SourceLines: []HistoryLogicalLine{{
			Text:          "ghi",
			Cells:         []HistoryCell{{Text: "ghi", Width: 3}},
			LineID:        10,
			ClippedBefore: true,
		}},
		Rows: []HistoryRow{
			{Text: "ghi", LineID: 10, RowInLine: 0, ClippedStart: true},
		},
		Lines:      []HistoryLineSpan{{LineID: 10, StartRow: 0, EndRow: 0, ClippedBefore: true}},
		Generation: 7,
		Boundary:   HistoryBoundary{FirstLineID: 10, LastLineID: 10},
	}
	cursor := HistoryCursor{Valid: true, BeforeLineID: 10, BeforeRowInLine: 0}
	store, err := store.BeginOlder(HistoryPendingRequest{
		ID:         2,
		TerminalID: "term-1",
		Cols:       6,
		Token:      "tok-1",
		Generation: 7,
		Cursor:     cursor,
		Boundary:   store.Boundary,
	})
	if err != nil {
		t.Fatalf("begin older: %v", err)
	}

	window := HistoryWindow{
		TerminalID: "term-1",
		Token:      "tok-1",
		Op:         HistoryWindowPrepend,
		Cols:       6,
		SourceLines: []HistoryLogicalLine{{
			Text:         "def",
			Cells:        []HistoryCell{{Text: "def", Width: 3}},
			LineID:       10,
			ClippedAfter: true,
		}},
		Rows: []HistoryRow{
			{Text: "def", LineID: 10, RowInLine: 0, ClippedEnd: true},
		},
		Lines:      []HistoryLineSpan{{LineID: 10, StartRow: 0, EndRow: 0, ClippedAfter: true}},
		Cursor:     HistoryCursor{},
		Generation: 7,
		Boundary:   HistoryBoundary{FirstLineID: 10, LastLineID: 10},
		HasMore:    false,
	}
	beforeRows := len(store.Rows)
	store, inserted, err := store.ApplyWindow(2, window)
	if err != nil {
		t.Fatalf("apply older: %v", err)
	}
	if inserted != len(store.Rows)-beforeRows {
		t.Fatalf("expected inserted row count to reflect real local reflow delta, got inserted=%d before=%d after=%d", inserted, beforeRows, len(store.Rows))
	}
	if len(store.SourceLines) != 1 || store.SourceLines[0].Text != "defghi" || store.SourceLines[0].ClippedBefore || store.SourceLines[0].ClippedAfter {
		t.Fatalf("boundary-overlap prepend should merge clipped source into one logical line, got %#v", store.SourceLines)
	}
	if got := rowTexts(store.Rows); !reflect.DeepEqual(got, []string{"defghi"}) {
		t.Fatalf("reflowed rows should stay stable after merged source, got %v", got)
	}
	if got := spanRows(store.Lines); !reflect.DeepEqual(got, []spanRow{{id: 10, start: 0, end: 0}}) {
		t.Fatalf("merged logical line should produce one span, got %v", got)
	}
}

func TestHistoryStoreEnsureSourceLinesPreservesClippedFlagsFromSpans(t *testing.T) {
	store := HistoryStore{
		Rows: []HistoryRow{
			{Text: "abc", LineID: 10, RowInLine: 0, ClippedStart: true},
			{Text: "def", LineID: 10, RowInLine: 1, ClippedEnd: true},
		},
		Lines: []HistoryLineSpan{{
			LineID:        10,
			StartRow:      0,
			EndRow:        1,
			ClippedBefore: true,
			ClippedAfter:  true,
		}},
	}

	store = store.EnsureSourceLines()
	if len(store.SourceLines) != 1 {
		t.Fatalf("expected one merged source line, got %#v", store.SourceLines)
	}
	if got := store.SourceLines[0]; got.Text != "abcdef" || !got.ClippedBefore || !got.ClippedAfter {
		t.Fatalf("rows fallback must preserve clipped flags from authoritative spans, got %#v", got)
	}
}

func TestHistoryStoreRecordsOlderExhaustedMarker(t *testing.T) {
	cursor := HistoryCursor{Valid: true, BeforeLineID: 10}
	boundary := HistoryBoundary{FirstLineID: 10, LastLineID: 20}
	store := HistoryStore{
		Token:    "tok-1",
		Cols:     100,
		Cursor:   cursor,
		Boundary: boundary,
	}
	store, err := store.BeginOlder(HistoryPendingRequest{
		ID:         3,
		TerminalID: "term-1",
		Cols:       100,
		Token:      "tok-1",
		Generation: 9,
		Cursor:     cursor,
		Boundary:   boundary,
	})
	if err != nil {
		t.Fatalf("begin older: %v", err)
	}

	window := historyWindow(HistoryWindowPrepend, "term-1", "tok-1", 100, 9, nil)
	window.Cursor = cursor
	window.HasMore = false
	store, inserted, err := store.ApplyWindow(3, window)
	if err != nil {
		t.Fatalf("apply exhausted: %v", err)
	}

	if inserted != 0 {
		t.Fatalf("expected no inserted rows, got %d", inserted)
	}
	if !store.Exhausted.Valid || store.Exhausted.RequestID != 3 || store.Exhausted.Cursor != cursor || store.Exhausted.Cols != 100 {
		t.Fatalf("unexpected exhausted marker %#v", store.Exhausted)
	}
	if got := store.OlderRequestState(); got != OlderRequestExhausted {
		t.Fatalf("expected exhausted older state, got %s", got)
	}

	store.Cols = 80
	if got := store.OlderRequestState(); got != OlderRequestExhausted {
		t.Fatalf("local reflow cols must not clear frozen exhausted marker, got %s", got)
	}

	store.Token = "tok-next"
	if got := store.OlderRequestState(); got != OlderRequestReady {
		t.Fatalf("exhausted marker mismatch should fall back to current cursor readiness, got %s", got)
	}
}

func TestHistoryStoreOlderRequestStateUsesAuthoritativeBoundary(t *testing.T) {
	cursor := HistoryCursor{Valid: true, BeforeLineID: 20}
	boundary := HistoryBoundary{FirstLineID: 20, LastLineID: 30}
	store := HistoryStore{
		Token:      "tok-1",
		Cols:       80,
		Cursor:     cursor,
		Boundary:   boundary,
		HasMore:    true,
		Generation: 7,
	}
	if got := store.OlderRequestState(); got != OlderRequestReady {
		t.Fatalf("has-more window should be ready for older request, got %s", got)
	}

	store.Pending = &HistoryPendingRequest{ID: 2, Kind: HistoryRequestOlder, Token: "tok-1", Cursor: cursor, Boundary: boundary}
	if got := store.OlderRequestState(); got != OlderRequestPending {
		t.Fatalf("pending older request should win, got %s", got)
	}

	store.Pending = &HistoryPendingRequest{ID: 3, Kind: HistoryRequestLatest, Token: "tok-1"}
	if got := store.OlderRequestState(); got != OlderRequestPending {
		t.Fatalf("any pending history request should block older request, got %s", got)
	}

	store.Pending = nil
	store.HasMore = false
	if got := store.OlderRequestState(); got != OlderRequestReady {
		t.Fatalf("valid cursor should remain enough to request older even when HasMore is false, got %s", got)
	}

	store.Cursor = HistoryCursor{}
	if got := store.OlderRequestState(); got != OlderRequestMissing {
		t.Fatalf("missing authoritative cursor should not request older, got %s", got)
	}
}

func TestHistoryStoreRejectsStaleResponses(t *testing.T) {
	store, err := (HistoryStore{}).BeginOlder(HistoryPendingRequest{
		ID:         4,
		TerminalID: "term-1",
		Cols:       80,
		Token:      "tok-1",
		Generation: 7,
		Cursor:     HistoryCursor{Valid: true, BeforeLineID: 10},
	})
	if err != nil {
		t.Fatalf("begin older: %v", err)
	}

	window := historyWindow(HistoryWindowPrepend, "term-1", "tok-stale", 80, 7, []HistoryRow{{Text: "old", LineID: 1}})
	window.Cursor = HistoryCursor{Valid: true, BeforeLineID: 10}
	if _, _, err := store.ApplyWindow(4, window); !errors.Is(err, ErrStaleHistoryResponse) {
		t.Fatalf("expected stale token rejection, got %v", err)
	}

	window = historyWindow(HistoryWindowPrepend, "term-1", "tok-1", 100, 7, []HistoryRow{{Text: "old", LineID: 1}})
	window.Cursor = HistoryCursor{Valid: true, BeforeLineID: 10}
	if _, _, err := store.ApplyWindow(4, window); !errors.Is(err, ErrHistoryWindowMismatch) {
		t.Fatalf("expected cols mismatch rejection, got %v", err)
	}

	if _, _, err := store.ApplyWindow(99, window); !errors.Is(err, ErrStaleHistoryResponse) {
		t.Fatalf("expected request id rejection, got %v", err)
	}
}

func TestHistoryStoreRejectsDifferentViewResponse(t *testing.T) {
	store, err := (HistoryStore{}).BeginLatest(HistoryPendingRequest{
		ID:         5,
		PaneID:     "pane-1",
		ViewID:     "pane:pane-1",
		TerminalID: "term-1",
		Cols:       80,
	})
	if err != nil {
		t.Fatalf("begin latest: %v", err)
	}

	window := historyWindow(HistoryWindowReplace, "term-1", "tok-1", 80, 7, []HistoryRow{{Text: "other-view", LineID: 20}})
	window.PaneID = "pane-2"
	window.ViewID = "pane:pane-2"
	if _, _, err := store.ApplyWindow(5, window); !errors.Is(err, ErrStaleHistoryResponse) {
		t.Fatalf("expected different view response to be stale, got %v", err)
	}
}

func TestHistoryStoreAcceptsOldestReplaceForBoundFrozenToken(t *testing.T) {
	store := HistoryStore{
		TerminalID: "term-1",
		Token:      "tok-1",
		Cols:       80,
		Generation: 7,
		Boundary:   HistoryBoundary{FirstLineID: 90, LastLineID: 100},
	}
	store, err := store.BeginOldest(HistoryPendingRequest{
		ID:         6,
		TerminalID: "term-1",
		Cols:       80,
		Token:      "tok-1",
		Generation: 7,
		Boundary:   store.Boundary,
	})
	if err != nil {
		t.Fatalf("begin oldest: %v", err)
	}

	window := historyWindow(HistoryWindowReplace, "term-1", "tok-1", 80, 7, []HistoryRow{{Text: "oldest", LineID: 1}})
	store, inserted, err := store.ApplyWindow(6, window)
	if err != nil {
		t.Fatalf("apply oldest: %v", err)
	}
	if inserted != 1 || store.Pending != nil || store.Rows[0].Text != "oldest" || store.Boundary.FirstLineID != 1 {
		t.Fatalf("unexpected oldest replace state inserted=%d store=%#v", inserted, store)
	}
}

func TestHistoryStoreRejectsOldestFromDifferentFrozenToken(t *testing.T) {
	store, err := (HistoryStore{}).BeginOldest(HistoryPendingRequest{
		ID:         7,
		TerminalID: "term-1",
		Cols:       80,
		Token:      "tok-1",
		Generation: 7,
		Boundary:   HistoryBoundary{FirstLineID: 90, LastLineID: 100},
	})
	if err != nil {
		t.Fatalf("begin oldest: %v", err)
	}

	window := historyWindow(HistoryWindowReplace, "term-1", "tok-stale", 80, 7, []HistoryRow{{Text: "oldest", LineID: 1}})
	if _, _, err := store.ApplyWindow(7, window); !errors.Is(err, ErrStaleHistoryResponse) {
		t.Fatalf("expected stale oldest token rejection, got %v", err)
	}
}

func TestCopyModeBindsLatestAndAdjustsOlderViewport(t *testing.T) {
	copyMode := CopyModeStore{}.BindLatest("pane-1", "pane:pane-1", "term-1", 1, 80, 20)
	if !copyMode.Active || !copyMode.Empty || copyMode.PaneID != "pane-1" || copyMode.ViewID != "pane:pane-1" || copyMode.TerminalID != "term-1" || copyMode.BoundCols != 80 || copyMode.ViewRows != 20 {
		t.Fatalf("unexpected bound copy mode %#v", copyMode)
	}

	latest := historyWindow(HistoryWindowReplace, "term-1", "tok-1", 80, 7, []HistoryRow{{Text: "new", LineID: 20}})
	copyMode = copyMode.AcceptLatest(latest, latest.Cols, len(latest.Rows))
	if copyMode.BoundToken != "tok-1" || copyMode.Empty {
		t.Fatalf("unexpected latest binding %#v", copyMode)
	}

	older := historyWindow(HistoryWindowPrepend, "term-1", "tok-1", 80, 7, []HistoryRow{{Text: "old", LineID: 10}, {Text: "older", LineID: 11}})
	before := HistoryStore{
		Cols:        80,
		SourceLines: []HistoryLogicalLine{{Text: "new", LineID: 20}},
		Rows:        []HistoryRow{{Text: "new", LineID: 20, RowInLine: 0}},
		Lines:       []HistoryLineSpan{{LineID: 20, StartRow: 0, EndRow: 0}},
	}
	after := HistoryStore{
		Cols: 100,
		SourceLines: []HistoryLogicalLine{
			{Text: "old", LineID: 10},
			{Text: "older", LineID: 11},
			{Text: "new", LineID: 20},
		},
	}
	after.Rows, after.Lines = ReflowHistoryLogicalLines(after.SourceLines, after.Cols)
	copyMode = copyMode.AcceptOlder(2, before, after, older, 100)
	if copyMode.ViewportTop != 2 || copyMode.BoundCols != 100 {
		t.Fatalf("older accept should keep local bound cols while adjusting viewport, got %#v", copyMode)
	}
}

func TestCopyModeAcceptLatestStartsAtNewestTail(t *testing.T) {
	rows := []HistoryRow{
		{Text: "one", LineID: 1},
		{Text: "two", LineID: 2},
		{Text: "three", LineID: 3},
		{Text: "four", LineID: 4},
		{Text: "five", LineID: 5},
	}
	latest := historyWindow(HistoryWindowReplace, "term-1", "tok-1", 80, 7, rows)
	copyMode := CopyModeStore{ViewRows: 3}.AcceptLatest(latest, latest.Cols, len(rows))

	if copyMode.Cursor != (CopyPosition{Row: 4}) || copyMode.ViewportTop != 2 {
		t.Fatalf("latest should start at newest visible tail, got %#v", copyMode)
	}
}

func TestCopyModeAcceptOldestStartsAtOldestHead(t *testing.T) {
	rows := []HistoryRow{
		{Text: "one", LineID: 1},
		{Text: "two", LineID: 2},
		{Text: "three", LineID: 3},
		{Text: "four", LineID: 4},
		{Text: "five", LineID: 5},
	}
	oldest := historyWindow(HistoryWindowReplace, "term-1", "tok-1", 80, 7, rows)
	copyMode := CopyModeStore{ViewRows: 3}.AcceptOldest(oldest, oldest.Cols, len(rows))

	if copyMode.Cursor != (CopyPosition{}) || copyMode.ViewportTop != 0 {
		t.Fatalf("oldest should start at oldest visible head, got %#v", copyMode)
	}
}

func TestCopyModeResizeInvalidatesBindingAndSelection(t *testing.T) {
	mark := CopyPosition{Row: 1, Col: 2}
	copyMode := CopyModeStore{
		Active:      true,
		PaneID:      "pane-1",
		ViewID:      "pane:pane-1",
		TerminalID:  "term-1",
		BoundToken:  "tok-1",
		BoundCols:   80,
		ViewRows:    20,
		ViewportTop: 4,
		Cursor:      CopyPosition{Row: 2, Col: 3},
		Mark:        &mark,
		Selection:   &CopySelection{Anchor: mark, Focus: CopyPosition{Row: 2, Col: 3}},
	}

	copyMode = copyMode.Resize(100, 30)
	if copyMode.BoundToken != "tok-1" || copyMode.BoundCols != 100 || copyMode.ViewRows != 30 || copyMode.ViewportTop != 4 {
		t.Fatalf("unexpected resized copy mode %#v", copyMode)
	}
	if copyMode.TerminalID != "term-1" || copyMode.ViewID != "pane:pane-1" || copyMode.PaneID != "pane-1" {
		t.Fatalf("resize should preserve frozen binding identity, got %#v", copyMode)
	}
	if copyMode.Mark == nil || copyMode.Selection == nil {
		t.Fatalf("state resize should not clear selection by itself, got mark=%#v selection=%#v", copyMode.Mark, copyMode.Selection)
	}
}

func TestCopyModeAcceptOlderShiftsCursorMarkAndSelectionWithPrependedRows(t *testing.T) {
	mark := CopyPosition{Row: 1, Col: 2}
	copyMode := CopyModeStore{
		Active:      true,
		Cursor:      CopyPosition{Row: 3, Col: 4},
		Mark:        &mark,
		Selection:   &CopySelection{Anchor: mark, Focus: CopyPosition{Row: 3, Col: 4}},
		ViewportTop: 2,
	}

	before := HistoryStore{
		Cols: 80,
		SourceLines: []HistoryLogicalLine{
			{Text: "line-1", LineID: 10},
			{Text: "line-2", LineID: 11},
			{Text: "line-3", LineID: 12},
			{Text: "line-4", LineID: 13},
		},
	}
	before.Rows, before.Lines = ReflowHistoryLogicalLines(before.SourceLines, before.Cols)
	after := HistoryStore{
		Cols: 90,
		SourceLines: []HistoryLogicalLine{
			{Text: "older-1", LineID: 8},
			{Text: "older-2", LineID: 9},
			{Text: "line-1", LineID: 10},
			{Text: "line-2", LineID: 11},
			{Text: "line-3", LineID: 12},
			{Text: "line-4", LineID: 13},
		},
	}
	after.Rows, after.Lines = ReflowHistoryLogicalLines(after.SourceLines, after.Cols)
	copyMode = copyMode.AcceptOlder(5, before, after, HistoryWindow{Token: "tok-older", Cols: 80}, 90)

	if copyMode.ViewportTop != 4 {
		t.Fatalf("expected viewport top to keep pointing at original content after prepend, got %d", copyMode.ViewportTop)
	}
	if copyMode.Cursor != (CopyPosition{Row: 5, Col: 4}) {
		t.Fatalf("expected cursor to keep pointing at original content after prepend, got %#v", copyMode.Cursor)
	}
	if copyMode.Mark == nil || *copyMode.Mark != (CopyPosition{Row: 3, Col: 2}) {
		t.Fatalf("expected mark to keep pointing at original content after prepend, got %#v", copyMode.Mark)
	}
	if copyMode.Selection == nil {
		t.Fatal("expected selection to be preserved")
	}
	if copyMode.Selection.Anchor != (CopyPosition{Row: 3, Col: 2}) || copyMode.Selection.Focus != (CopyPosition{Row: 5, Col: 4}) {
		t.Fatalf("expected selection to keep pointing at original content after prepend, got %#v", copyMode.Selection)
	}
	if copyMode.BoundToken != "tok-older" || copyMode.BoundCols != 90 {
		t.Fatalf("expected authoritative binding to update with older accept, got %#v", copyMode)
	}
}

func TestCopyModeAcceptOlderFastShiftsSimplePrependWithoutRebind(t *testing.T) {
	copyMode := CopyModeStore{
		Active:      true,
		Cursor:      CopyPosition{Row: 1, Col: 2},
		ViewportTop: 1,
	}
	before := HistoryStore{
		Cols: 6,
		SourceLines: []HistoryLogicalLine{
			{Text: "abcdef", LineID: 10},
			{Text: "ghijkl", LineID: 11},
		},
	}
	before.Rows, before.Lines = ReflowHistoryLogicalLines(before.SourceLines, before.Cols)
	after := HistoryStore{
		Cols: 6,
		SourceLines: []HistoryLogicalLine{
			{Text: "older", LineID: 9},
			{Text: "abcdef", LineID: 10},
			{Text: "ghijkl", LineID: 11},
		},
	}
	after.Rows, after.Lines = ReflowHistoryLogicalLines(after.SourceLines, after.Cols)

	copyMode = copyMode.AcceptOlder(1, before, after, HistoryWindow{Token: "tok-older", Cols: 6}, 6)

	if copyMode.ViewportTop != 2 || copyMode.Cursor != (CopyPosition{Row: 2, Col: 2}) {
		t.Fatalf("simple prepend should shift row positions without content rebind, got %#v", copyMode)
	}
}

func TestCopyModeApplyDeferredOlderScrollConsumesPendingRows(t *testing.T) {
	copyMode := CopyModeStore{Active: true, ViewRows: 5, ViewportTop: 8}
	copyMode = copyMode.ApplyDeferredOlderScroll(1, 30)

	if copyMode.ViewportTop != 7 {
		t.Fatalf("expected deferred scroll to consume one row, got %d", copyMode.ViewportTop)
	}

	copyMode = (CopyModeStore{Active: true, ViewRows: 20}).ApplyDeferredOlderScroll(8, 30)
	if copyMode.ViewportTop != 0 {
		t.Fatalf("deferred scroll should clamp at history top, got %d", copyMode.ViewportTop)
	}
}

func TestCopyModeRebindToReflowedHistoryKeepsCursorAndSelectionOnOriginalContent(t *testing.T) {
	before := HistoryStore{
		Cols: 3,
		SourceLines: []HistoryLogicalLine{{
			Text:   "abcdef",
			LineID: 10,
		}},
	}
	before.Rows, before.Lines = ReflowHistoryLogicalLines(before.SourceLines, before.Cols)
	after := HistoryStore{
		Cols:        6,
		SourceLines: cloneHistoryLogicalLines(before.SourceLines),
	}
	after.Rows, after.Lines = ReflowHistoryLogicalLines(after.SourceLines, after.Cols)
	mark := CopyPosition{Row: 0, Col: 1}
	copyMode := CopyModeStore{
		Active:      true,
		ViewportTop: 1,
		Cursor:      CopyPosition{Row: 1, Col: 2},
		Mark:        &mark,
		Selection:   &CopySelection{Anchor: mark, Focus: CopyPosition{Row: 1, Col: 2}},
		Query:       "ef",
		Matches:     []CopyMatch{{StartRow: 1, StartCol: 1, EndRow: 1, EndCol: 3}},
		ActiveMatch: 0,
	}

	copyMode = copyMode.RebindToReflowedHistory(before, after)

	if copyMode.ViewportTop != 0 {
		t.Fatalf("expected viewport top to rebind to containing row, got %#v", copyMode)
	}
	if copyMode.Cursor != (CopyPosition{Row: 0, Col: 5}) {
		t.Fatalf("expected cursor to keep pointing at original content after local reflow, got %#v", copyMode.Cursor)
	}
	if copyMode.Mark == nil || *copyMode.Mark != (CopyPosition{Row: 0, Col: 1}) {
		t.Fatalf("expected mark to keep pointing at original content after local reflow, got %#v", copyMode.Mark)
	}
	if copyMode.Selection == nil {
		t.Fatal("expected selection to be preserved after local reflow")
	}
	if copyMode.Selection.Anchor != (CopyPosition{Row: 0, Col: 1}) || copyMode.Selection.Focus != (CopyPosition{Row: 0, Col: 5}) {
		t.Fatalf("expected selection to rebind to original content after local reflow, got %#v", copyMode.Selection)
	}
	if len(copyMode.Matches) != 1 || copyMode.Matches[0] != (CopyMatch{StartRow: 0, StartCol: 4, EndRow: 0, EndCol: 6}) {
		t.Fatalf("expected query matches to reflow with local rows, got %#v", copyMode.Matches)
	}
	if copyMode.ActiveMatch != 0 {
		t.Fatalf("expected active match index to stay on rebound match, got %#v", copyMode)
	}
}

func TestCopyModeRebindAfterOlderBoundaryOverlapKeepsCursorAndSelectionOnOriginalContent(t *testing.T) {
	before := HistoryStore{
		Cols: 4,
		SourceLines: []HistoryLogicalLine{{
			Text:          "cdef",
			LineID:        10,
			ClippedBefore: true,
		}},
	}
	before.Rows, before.Lines = ReflowHistoryLogicalLines(before.SourceLines, before.Cols)
	after := HistoryStore{
		Cols: 4,
		SourceLines: []HistoryLogicalLine{{
			Text:          "abcdef",
			LineID:        10,
			ClippedBefore: true,
		}},
	}
	after.Rows, after.Lines = ReflowHistoryLogicalLines(after.SourceLines, after.Cols)
	mark := CopyPosition{Row: 0, Col: 1}
	copyMode := CopyModeStore{
		Active:      true,
		ViewportTop: 0,
		Cursor:      CopyPosition{Row: 0, Col: 3},
		Mark:        &mark,
		Selection:   &CopySelection{Anchor: mark, Focus: CopyPosition{Row: 0, Col: 3}},
	}

	copyMode = copyMode.RebindToReflowedHistory(before, after)

	if copyMode.Cursor != (CopyPosition{Row: 1, Col: 1}) {
		t.Fatalf("expected cursor to keep pointing at original suffix after boundary overlap, got %#v", copyMode.Cursor)
	}
	if copyMode.Mark == nil || *copyMode.Mark != (CopyPosition{Row: 0, Col: 3}) {
		t.Fatalf("expected mark to keep pointing at original suffix after boundary overlap, got %#v", copyMode.Mark)
	}
	if copyMode.Selection == nil {
		t.Fatal("expected selection after boundary overlap rebind")
	}
	if copyMode.Selection.Anchor != (CopyPosition{Row: 0, Col: 3}) || copyMode.Selection.Focus != (CopyPosition{Row: 1, Col: 1}) {
		t.Fatalf("expected selection to keep original suffix after boundary overlap, got %#v", copyMode.Selection)
	}
}

func TestHistoryStoreReflowsFrozenLogicalLinesAtNewCols(t *testing.T) {
	lines := []HistoryLogicalLine{{
		Text:   "abcdef",
		LineID: 10,
		Cells: []HistoryCell{
			{Text: "abc", Width: 3},
			{Text: "def", Width: 3},
		},
	}}
	rows, spans := ReflowHistoryLogicalLines(lines, 3)
	if got := rowTexts(rows); !reflect.DeepEqual(got, []string{"abc", "def"}) {
		t.Fatalf("expected local reflow rows, got %v", got)
	}
	if got := spanRows(spans); !reflect.DeepEqual(got, []spanRow{{id: 10, start: 0, end: 1}}) {
		t.Fatalf("expected reflowed span, got %v", got)
	}
}

func TestHistoryStoreReflowsFrozenLogicalLineTextWithoutCellsAtNewCols(t *testing.T) {
	lines := []HistoryLogicalLine{{
		Text:   "abcdef",
		LineID: 10,
	}}
	rows, spans := ReflowHistoryLogicalLines(lines, 3)
	if got := rowTexts(rows); !reflect.DeepEqual(got, []string{"abc", "def"}) {
		t.Fatalf("text-only frozen source should still reflow by display cols, got %v", got)
	}
	if got := spanRows(spans); !reflect.DeepEqual(got, []spanRow{{id: 10, start: 0, end: 1}}) {
		t.Fatalf("expected text-only frozen source span, got %v", got)
	}
}

func TestHistoryInvalidateWindowClearsAuthoritativeRowsAndPending(t *testing.T) {
	store := HistoryStore{
		TerminalID: "term-1",
		Token:      "tok-1",
		Cols:       80,
		Rows:       []HistoryRow{{Text: "old", LineID: 10}},
		Lines:      []HistoryLineSpan{{LineID: 10, StartRow: 0, EndRow: 0}},
		Cursor:     HistoryCursor{Valid: true, BeforeLineID: 10},
		Generation: 7,
		Boundary:   HistoryBoundary{FirstLineID: 10, LastLineID: 10},
		HasMore:    true,
		Exhausted:  ExhaustedMarker{Valid: true, Token: "tok-1", Cols: 80},
		Pending:    &HistoryPendingRequest{ID: 9, TerminalID: "term-1", Cols: 80},
	}

	store = store.InvalidateWindow()
	if store.Token != "" || store.Cols != 0 || len(store.Rows) != 0 || len(store.Lines) != 0 || store.Pending != nil {
		t.Fatalf("history invalidate must clear window and pending request, got %#v", store)
	}
	if store.TerminalID != "term-1" {
		t.Fatalf("history invalidate should keep terminal binding, got %#v", store)
	}
}

func TestCopyModeSelectionFollowsMarkAndCursor(t *testing.T) {
	copyMode := CopyModeStore{}.SetMark(CopyPosition{Row: 1, Col: 0})
	copyMode = copyMode.MoveCursor(CopyPosition{Row: 2, Col: 4})

	if copyMode.Selection == nil {
		t.Fatal("expected selection")
	}
	if copyMode.Selection.Anchor != (CopyPosition{Row: 1, Col: 0}) || copyMode.Selection.Focus != (CopyPosition{Row: 2, Col: 4}) {
		t.Fatalf("unexpected selection %#v", copyMode.Selection)
	}
}

func TestCopyModeSearchMatchesAndScrollClamp(t *testing.T) {
	history := HistoryStore{Rows: []HistoryRow{
		{Text: "alpha beta", LineID: 1},
		{Text: "beta gamma", LineID: 2},
		{Text: "delta", LineID: 3},
	}}
	matches := FindCopyMatches(history, "beta")
	if len(matches) != 2 || matches[0] != (CopyMatch{StartRow: 0, StartCol: 6, EndRow: 0, EndCol: 10}) || matches[1].StartRow != 1 {
		t.Fatalf("unexpected matches %#v", matches)
	}
	copyMode := (CopyModeStore{ViewRows: 3}).SetQuery("beta", matches)
	if copyMode.Cursor.Row != 0 || copyMode.Cursor.Col != 6 {
		t.Fatalf("expected cursor on first match, got %#v", copyMode)
	}
	copyMode = copyMode.MoveMatch(1)
	if copyMode.ActiveMatch != 1 || copyMode.Cursor.Row != 1 {
		t.Fatalf("expected second active match, got %#v", copyMode)
	}
	copyMode = copyMode.Scroll(20, len(history.Rows))
	if copyMode.ViewportTop != 0 {
		t.Fatalf("scroll should clamp to max top for fully visible rows, got %#v", copyMode)
	}
}

func TestCopyModeSearchMatchesUseDisplayColumns(t *testing.T) {
	history := HistoryStore{Rows: []HistoryRow{
		{Text: "a好bc", LineID: 1},
		{Text: "x好b", LineID: 2, Cells: []HistoryCell{
			{Text: "x", Width: 1},
			{Text: "好", Width: 2},
			{Text: "b", Width: 1},
		}},
	}}
	matches := FindCopyMatches(history, "好b")
	if len(matches) != 2 {
		t.Fatalf("expected two display-column matches, got %#v", matches)
	}
	if matches[0] != (CopyMatch{StartRow: 0, StartCol: 1, EndRow: 0, EndCol: 4}) || matches[1] != (CopyMatch{StartRow: 1, StartCol: 1, EndRow: 1, EndCol: 4}) {
		t.Fatalf("matches should use display columns, got %#v", matches)
	}
	copyMode := (CopyModeStore{}).SetQuery("好b", matches)
	if copyMode.Cursor != (CopyPosition{Row: 0, Col: 1}) {
		t.Fatalf("cursor should use match display column, got %#v", copyMode.Cursor)
	}
}

func TestCopyModeSearchMatchesUseGraphemeDisplayColumns(t *testing.T) {
	family := "👨‍👩‍👧‍👦"
	history := HistoryStore{Rows: []HistoryRow{
		{Text: family + "x", LineID: 1},
		{Text: "e\u0301x", LineID: 2},
	}}
	familyMatches := FindCopyMatches(history, family)
	if len(familyMatches) != 1 || familyMatches[0] != (CopyMatch{StartRow: 0, StartCol: 0, EndRow: 0, EndCol: 2}) {
		t.Fatalf("emoji family match should use grapheme display columns, got %#v", familyMatches)
	}
	xMatches := FindCopyMatches(history, "x")
	if len(xMatches) != 2 || xMatches[0] != (CopyMatch{StartRow: 0, StartCol: 2, EndRow: 0, EndCol: 3}) || xMatches[1] != (CopyMatch{StartRow: 1, StartCol: 1, EndRow: 1, EndCol: 2}) {
		t.Fatalf("following x matches should not be swallowed by previous grapheme, got %#v", xMatches)
	}
	combiningMatches := FindCopyMatches(history, "e\u0301")
	if len(combiningMatches) != 1 || combiningMatches[0] != (CopyMatch{StartRow: 1, StartCol: 0, EndRow: 1, EndCol: 1}) {
		t.Fatalf("combining mark match should use one display column, got %#v", combiningMatches)
	}
}

func TestCopyModeSearchMatchesUseAuthoritativeCellWidth(t *testing.T) {
	history := HistoryStore{Rows: []HistoryRow{{
		Text:   "abx",
		LineID: 1,
		Cells: []HistoryCell{
			{Text: "ab", Width: 4},
			{Text: "x", Width: 1},
		},
	}}}
	matches := FindCopyMatches(history, "x")
	if len(matches) != 1 || matches[0] != (CopyMatch{StartRow: 0, StartCol: 4, EndRow: 0, EndCol: 5}) {
		t.Fatalf("match should use authoritative history cell width, got %#v", matches)
	}
}

func TestCopyModeSearchMatchesAcrossReflowRowsOfSameLogicalLine(t *testing.T) {
	history := HistoryStore{
		Rows: []HistoryRow{
			{Text: "alphabe", LineID: 10, RowInLine: 0},
			{Text: "tagamma", LineID: 10, RowInLine: 1},
		},
		Lines: []HistoryLineSpan{{LineID: 10, StartRow: 0, EndRow: 1}},
	}
	matches := FindCopyMatches(history, "beta")
	if len(matches) != 1 {
		t.Fatalf("expected cross-row logical-line match, got %#v", matches)
	}
	if matches[0] != (CopyMatch{StartRow: 0, StartCol: 5, EndRow: 1, EndCol: 2}) {
		t.Fatalf("cross-row match should preserve row/col boundaries, got %#v", matches[0])
	}
	copyMode := (CopyModeStore{}).SetQuery("beta", matches)
	if copyMode.Cursor != (CopyPosition{Row: 0, Col: 5}) {
		t.Fatalf("cursor should jump to cross-row match start, got %#v", copyMode.Cursor)
	}
}

func TestCopyModeRefreshQueryMatchesKeepsCurrentActiveMatch(t *testing.T) {
	copyMode := CopyModeStore{
		Query:       "beta",
		Cursor:      CopyPosition{Row: 1, Col: 0},
		Matches:     []CopyMatch{{StartRow: 0, StartCol: 6, EndRow: 0, EndCol: 10}, {StartRow: 1, StartCol: 0, EndRow: 1, EndCol: 4}},
		ActiveMatch: 1,
	}

	copyMode = copyMode.RefreshQueryMatches([]CopyMatch{
		{StartRow: 1, StartCol: 0, EndRow: 1, EndCol: 4},
		{StartRow: 2, StartCol: 5, EndRow: 2, EndCol: 9},
	})

	if copyMode.ActiveMatch != 0 {
		t.Fatalf("expected refresh to keep active match on current cursor, got %#v", copyMode)
	}
	if copyMode.Cursor != (CopyPosition{Row: 1, Col: 0}) {
		t.Fatalf("expected refresh to keep cursor on current match, got %#v", copyMode.Cursor)
	}
}

func TestHistoryRowGraphemeDisplayColumnsUsesAuthoritativeCellWidth(t *testing.T) {
	row := HistoryRow{Text: "abx", Cells: []HistoryCell{
		{Text: "ab", Width: 4},
		{Text: "x", Width: 1},
	}}
	if got := HistoryRowGraphemeDisplayColumns(row); !reflect.DeepEqual(got, []int{0, 2, 4, 5}) {
		t.Fatalf("unexpected authoritative grapheme columns %#v", got)
	}
}

func TestHistoryRowSliceDisplayUsesDisplayColumns(t *testing.T) {
	row := HistoryRow{Text: "a好bc", Cells: []HistoryCell{
		{Text: "a", Width: 1},
		{Text: "好", Width: 2},
		{Text: "bc", Width: 2},
	}}
	if got := HistoryRowDisplayWidth(row); got != 5 {
		t.Fatalf("unexpected display width %d", got)
	}
	if got := HistoryRowSliceDisplay(row, 1, 4); got != "好b" {
		t.Fatalf("display slice should include full wide cell and following ASCII, got %q", got)
	}
}

func TestHistoryRowSliceDisplayUsesAuthoritativeCellWidth(t *testing.T) {
	row := HistoryRow{Text: "ab", Cells: []HistoryCell{
		{Text: "ab", Width: 4},
	}}
	if got := HistoryRowDisplayWidth(row); got != 4 {
		t.Fatalf("display width should use authoritative cell width, got %d", got)
	}
	if got := HistoryRowSliceDisplay(row, 0, 4); got != "ab" {
		t.Fatalf("full-width slice should preserve cell text, got %q", got)
	}
}

func historyWindow(
	op HistoryWindowOp,
	terminalID string,
	token string,
	cols int,
	generation uint64,
	rows []HistoryRow,
) HistoryWindow {
	firstLine := uint64(0)
	lastLine := uint64(0)
	if len(rows) > 0 {
		firstLine = rows[0].LineID
		lastLine = rows[len(rows)-1].LineID
	}
	window := HistoryWindow{
		TerminalID:  terminalID,
		Token:       token,
		Op:          op,
		Cols:        cols,
		SourceLines: historyLogicalLinesFromRows(rows),
		Rows:        rows,
		Generation:  generation,
		Boundary:    HistoryBoundary{FirstLineID: firstLine, LastLineID: lastLine},
	}
	if len(rows) > 0 {
		window.Lines = []HistoryLineSpan{{LineID: firstLine, StartRow: 0, EndRow: len(rows) - 1}}
	}
	return window
}

func historyLogicalLinesFromRows(rows []HistoryRow) []HistoryLogicalLine {
	if len(rows) == 0 {
		return nil
	}
	lines := make([]HistoryLogicalLine, len(rows))
	for i, row := range rows {
		lines[i] = HistoryLogicalLine{
			Text:   row.Text,
			Cells:  cloneHistoryCells(row.Cells),
			LineID: row.LineID,
		}
	}
	return lines
}

type spanRow struct {
	id    uint64
	start int
	end   int
}

func rowTexts(rows []HistoryRow) []string {
	texts := make([]string, len(rows))
	for i, row := range rows {
		texts[i] = row.Text
	}
	return texts
}

func spanRows(spans []HistoryLineSpan) []spanRow {
	out := make([]spanRow, len(spans))
	for i, span := range spans {
		out[i] = spanRow{id: span.LineID, start: span.StartRow, end: span.EndRow}
	}
	return out
}
