package history

import (
	"errors"
	"reflect"
	"testing"
)

func TestLatestWindowReturnsReplaceProjectionFromCommittedTail(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "alpha")
	commitLine(t, track, "beta")

	window, err := track.LatestWindow(HistoryWindowRequest{Cols: 10, Rows: 10})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}

	if window.Op != HistoryWindowReplace {
		t.Fatalf("expected replace op, got %q", window.Op)
	}
	if window.Generation != track.Generation() {
		t.Fatalf("expected generation %d, got %d", track.Generation(), window.Generation)
	}
	if window.FirstLineID != 1 || window.LastLineID != 2 {
		t.Fatalf("unexpected boundaries first=%d last=%d", window.FirstLineID, window.LastLineID)
	}
	if got := rowTexts(window.Rows); !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("unexpected rows %v", got)
	}
	if got := rowLineIDs(window.Rows); !reflect.DeepEqual(got, []LogicalLineID{1, 2}) {
		t.Fatalf("unexpected row line ids %v", got)
	}
	if got := windowSpans(window.Spans); !reflect.DeepEqual(got, []lineSpan{{id: 1, first: 0, last: 0}, {id: 2, first: 1, last: 1}}) {
		t.Fatalf("unexpected spans %v", got)
	}
	if window.HasMore {
		t.Fatal("expected no older rows")
	}
	if window.Cursor.Valid {
		t.Fatalf("expected empty cursor, got %#v", window.Cursor)
	}
	if window.TotalLines != 2 || window.LoadedLines != 2 {
		t.Fatalf("unexpected line counts total=%d loaded=%d", window.TotalLines, window.LoadedLines)
	}
}

func TestLatestWindowClipsRowsAndReturnsOlderCursor(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "abcdef")
	commitLine(t, track, "ghij")

	window, err := track.LatestWindow(HistoryWindowRequest{Cols: 3, Rows: 2})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}

	if got := rowTexts(window.Rows); !reflect.DeepEqual(got, []string{"ghi", "j"}) {
		t.Fatalf("unexpected clipped rows %v", got)
	}
	if window.FirstLineID != 2 || window.LastLineID != 2 {
		t.Fatalf("unexpected boundaries first=%d last=%d", window.FirstLineID, window.LastLineID)
	}
	if !window.HasMore {
		t.Fatal("expected older rows")
	}
	if window.Cursor != (HistoryCursor{Valid: true, BeforeLineID: 2, BeforeRowInLine: 0}) {
		t.Fatalf("unexpected older cursor %#v", window.Cursor)
	}
	if len(window.Spans) != 1 {
		t.Fatalf("expected one span, got %d", len(window.Spans))
	}
	if window.Spans[0].ClippedAfter {
		t.Fatal("latest tail should include complete final line")
	}
}

func TestOlderWindowReturnsPrependProjection(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "abcdef")
	commitLine(t, track, "ghij")
	latest, err := track.LatestWindow(HistoryWindowRequest{Cols: 3, Rows: 2})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}

	older, err := track.OlderWindow(HistoryWindowRequest{Cols: 3, Rows: 2, Cursor: latest.Cursor})
	if err != nil {
		t.Fatalf("older window: %v", err)
	}

	if older.Op != HistoryWindowPrepend {
		t.Fatalf("expected prepend op, got %q", older.Op)
	}
	if got := rowTexts(older.Rows); !reflect.DeepEqual(got, []string{"abc", "def"}) {
		t.Fatalf("unexpected older rows %v", got)
	}
	if got := rowLineIDs(older.Rows); !reflect.DeepEqual(got, []LogicalLineID{1, 1}) {
		t.Fatalf("unexpected older row line ids %v", got)
	}
	if older.HasMore {
		t.Fatal("expected older exhausted after prepend")
	}
	if older.Cursor.Valid {
		t.Fatalf("expected exhausted cursor, got %#v", older.Cursor)
	}
}

func TestOlderWindowCanPageWithinSingleLogicalLine(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "abcdefghi")
	latest, err := track.LatestWindow(HistoryWindowRequest{Cols: 3, Rows: 1})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := rowTexts(latest.Rows); !reflect.DeepEqual(got, []string{"ghi"}) {
		t.Fatalf("unexpected latest rows %v", got)
	}
	if latest.Cursor != (HistoryCursor{Valid: true, BeforeLineID: 1, BeforeRowInLine: 2}) {
		t.Fatalf("unexpected latest cursor %#v", latest.Cursor)
	}

	older, err := track.OlderWindow(HistoryWindowRequest{Cols: 3, Rows: 1, Cursor: latest.Cursor})
	if err != nil {
		t.Fatalf("older window: %v", err)
	}
	if got := rowTexts(older.Rows); !reflect.DeepEqual(got, []string{"def"}) {
		t.Fatalf("unexpected older rows %v", got)
	}
	if older.Cursor != (HistoryCursor{Valid: true, BeforeLineID: 1, BeforeRowInLine: 1}) {
		t.Fatalf("unexpected older cursor %#v", older.Cursor)
	}
	if len(older.Spans) != 1 || !older.Spans[0].ClippedBefore || !older.Spans[0].ClippedAfter {
		t.Fatalf("expected clipped span inside logical line, got %#v", older.Spans)
	}
}

func TestOlderWindowReturnsEmptyWhenExhaustedOrStale(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "alpha")
	latest, err := track.LatestWindow(HistoryWindowRequest{Cols: 10, Rows: 10})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}

	exhausted, err := track.OlderWindow(HistoryWindowRequest{Cols: 10, Rows: 10, Cursor: latest.Cursor})
	if err != nil {
		t.Fatalf("older exhausted: %v", err)
	}
	if exhausted.Op != HistoryWindowPrepend {
		t.Fatalf("expected prepend op, got %q", exhausted.Op)
	}
	if len(exhausted.Rows) != 0 || exhausted.HasMore {
		t.Fatalf("expected exhausted empty older window, got rows=%v hasMore=%v", exhausted.Rows, exhausted.HasMore)
	}

	stale, err := track.OlderWindow(HistoryWindowRequest{
		Cols:   10,
		Rows:   10,
		Cursor: HistoryCursor{Valid: true, BeforeLineID: 99},
	})
	if err != nil {
		t.Fatalf("older stale: %v", err)
	}
	if len(stale.Rows) != 0 || stale.HasMore {
		t.Fatalf("expected stale cursor to return empty exhausted window, got rows=%v hasMore=%v", stale.Rows, stale.HasMore)
	}
}

func TestHistoryWindowReprojectsDifferentCols(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "abcdef")

	wide, err := track.LatestWindow(HistoryWindowRequest{Cols: 6, Rows: 10})
	if err != nil {
		t.Fatalf("wide latest: %v", err)
	}
	narrow, err := track.LatestWindow(HistoryWindowRequest{Cols: 3, Rows: 10})
	if err != nil {
		t.Fatalf("narrow latest: %v", err)
	}

	if got := rowTexts(wide.Rows); !reflect.DeepEqual(got, []string{"abcdef"}) {
		t.Fatalf("unexpected wide rows %v", got)
	}
	if got := rowTexts(narrow.Rows); !reflect.DeepEqual(got, []string{"abc", "def"}) {
		t.Fatalf("unexpected narrow rows %v", got)
	}
	if wide.Token == narrow.Token {
		t.Fatal("different cols should produce different window tokens")
	}
}

func TestLatestWindowIncludesEligibleMutableFrontier(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "committed")
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("draft")})

	window, err := track.LatestWindow(HistoryWindowRequest{Cols: 20, Rows: 10})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := rowTexts(window.Rows); !reflect.DeepEqual(got, []string{"committed", "draft"}) {
		t.Fatalf("unexpected latest rows %v", got)
	}
	if got := rowLineIDs(window.Rows); !reflect.DeepEqual(got, []LogicalLineID{1, 2}) {
		t.Fatalf("unexpected latest row line ids %v", got)
	}
	if !window.Rows[0].Committed || window.Rows[1].Committed {
		t.Fatalf("unexpected committed row markers %#v", window.Rows)
	}

	applyHistoryEvents(t, track, HistoryEvent{Kind: EventHideFrontier, LineIDs: []LogicalLineID{2}})
	window, err = track.LatestWindow(HistoryWindowRequest{Cols: 20, Rows: 10})
	if err != nil {
		t.Fatalf("latest after hide: %v", err)
	}
	if got := rowTexts(window.Rows); !reflect.DeepEqual(got, []string{"committed"}) {
		t.Fatalf("hidden frontier must be excluded from latest projection, got %v", got)
	}
}

func TestLatestWindowCursorCanPageBackFromMutableOnlyTail(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "committed")
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventWritePrimaryCells, Cells: cells("draft")})

	latest, err := track.LatestWindow(HistoryWindowRequest{Cols: 20, Rows: 1})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := rowTexts(latest.Rows); !reflect.DeepEqual(got, []string{"draft"}) {
		t.Fatalf("unexpected latest rows %v", got)
	}
	if !latest.HasMore || !latest.Cursor.Valid {
		t.Fatalf("expected cursor back to committed history, got hasMore=%v cursor=%#v", latest.HasMore, latest.Cursor)
	}

	older, err := track.OlderWindow(HistoryWindowRequest{Cols: 20, Rows: 1, Cursor: latest.Cursor})
	if err != nil {
		t.Fatalf("older window: %v", err)
	}
	if got := rowTexts(older.Rows); !reflect.DeepEqual(got, []string{"committed"}) {
		t.Fatalf("unexpected older rows %v", got)
	}
}

func TestHistoryWindowTokenChangesAfterResizeGenerationInvalidation(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "alpha")
	before, err := track.LatestWindow(HistoryWindowRequest{Cols: 10, Rows: 10})
	if err != nil {
		t.Fatalf("latest before resize: %v", err)
	}

	applyHistoryEvents(t, track, HistoryEvent{Kind: EventResize, ResizeDirection: ResizeSame})
	after, err := track.LatestWindow(HistoryWindowRequest{Cols: 10, Rows: 10})
	if err != nil {
		t.Fatalf("latest after resize: %v", err)
	}

	if before.Generation == after.Generation {
		t.Fatal("expected generation to change after resize")
	}
	if before.Token == after.Token {
		t.Fatal("expected token to change after resize")
	}
}

func TestHistoryWindowRejectsInvalidSize(t *testing.T) {
	track := NewHistoryTrack()
	if _, err := track.LatestWindow(HistoryWindowRequest{Cols: 0, Rows: 1}); !errors.Is(err, ErrInvalidWindowSize) {
		t.Fatalf("expected ErrInvalidWindowSize from latest, got %v", err)
	}
	if _, err := track.OlderWindow(HistoryWindowRequest{Cols: 1, Rows: 0}); !errors.Is(err, ErrInvalidWindowSize) {
		t.Fatalf("expected ErrInvalidWindowSize from older, got %v", err)
	}
}

type lineSpan struct {
	id    LogicalLineID
	first int
	last  int
}

func rowTexts(rows []VisualRow) []string {
	texts := make([]string, len(rows))
	for i, row := range rows {
		texts[i] = row.Text
	}
	return texts
}

func rowLineIDs(rows []VisualRow) []LogicalLineID {
	ids := make([]LogicalLineID, len(rows))
	for i, row := range rows {
		ids[i] = row.LineID
	}
	return ids
}

func windowSpans(spans []LogicalLineSpan) []lineSpan {
	result := make([]lineSpan, len(spans))
	for i, span := range spans {
		result[i] = lineSpan{id: span.LineID, first: span.FirstRow, last: span.LastRow}
	}
	return result
}
