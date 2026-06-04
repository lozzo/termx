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
	if got := rowTexts(store.Rows); !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Fatalf("unexpected rows %v", got)
	}

	store.Rows[0].Text = "mutated"
	if window.Rows[0].Text != "one" {
		t.Fatal("store should detach accepted rows")
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

func TestHistoryStoreRecordsOlderExhaustedMarker(t *testing.T) {
	cursor := HistoryCursor{Valid: true, BeforeLineID: 10}
	boundary := HistoryBoundary{FirstLineID: 10, LastLineID: 20}
	store, err := (HistoryStore{}).BeginOlder(HistoryPendingRequest{
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

func TestCopyModeBindsLatestAndAdjustsOlderViewport(t *testing.T) {
	copyMode := CopyModeStore{}.BindLatest("term-1", 1, 80, 20)
	if !copyMode.Active || !copyMode.Empty || copyMode.TerminalID != "term-1" || copyMode.BoundCols != 80 || copyMode.ViewRows != 20 {
		t.Fatalf("unexpected bound copy mode %#v", copyMode)
	}

	latest := historyWindow(HistoryWindowReplace, "term-1", "tok-1", 80, 7, []HistoryRow{{Text: "new", LineID: 20}})
	copyMode = copyMode.AcceptLatest(latest)
	if copyMode.BoundToken != "tok-1" || copyMode.Empty {
		t.Fatalf("unexpected latest binding %#v", copyMode)
	}

	older := historyWindow(HistoryWindowPrepend, "term-1", "tok-1", 80, 7, []HistoryRow{{Text: "old", LineID: 10}, {Text: "older", LineID: 11}})
	copyMode = copyMode.AcceptOlder(2, older)
	if copyMode.ViewportTop != 2 {
		t.Fatalf("expected viewport adjusted by inserted rows, got %d", copyMode.ViewportTop)
	}
}

func TestCopyModeResizeInvalidatesBindingAndSelection(t *testing.T) {
	mark := CopyPosition{Row: 1, Col: 2}
	copyMode := CopyModeStore{
		Active:      true,
		BoundToken:  "tok-1",
		BoundCols:   80,
		ViewportTop: 4,
		Cursor:      CopyPosition{Row: 2, Col: 3},
		Mark:        &mark,
		Selection:   &CopySelection{Anchor: mark, Focus: CopyPosition{Row: 2, Col: 3}},
	}

	copyMode = copyMode.Resize(100, 30)
	if copyMode.BoundToken != "" || copyMode.BoundCols != 100 || copyMode.ViewRows != 30 || copyMode.ViewportTop != 0 {
		t.Fatalf("unexpected resized copy mode %#v", copyMode)
	}
	if !copyMode.Empty {
		t.Fatalf("resize should enter pending/empty state, got %#v", copyMode)
	}
	if copyMode.Mark != nil || copyMode.Selection != nil {
		t.Fatalf("resize should clear selection, got mark=%#v selection=%#v", copyMode.Mark, copyMode.Selection)
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
	if len(matches) != 2 || matches[0] != (CopyMatch{Row: 0, StartCol: 6, EndCol: 10}) || matches[1].Row != 1 {
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
	if copyMode.ViewportTop != 2 {
		t.Fatalf("scroll should clamp to max top for one visible row, got %#v", copyMode)
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
		TerminalID: terminalID,
		Token:      token,
		Op:         op,
		Cols:       cols,
		Rows:       rows,
		Generation: generation,
		Boundary:   HistoryBoundary{FirstLineID: firstLine, LastLineID: lastLine},
	}
	if len(rows) > 0 {
		window.Lines = []HistoryLineSpan{{LineID: firstLine, StartRow: 0, EndRow: len(rows) - 1}}
	}
	return window
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
