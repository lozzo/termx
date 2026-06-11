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

func TestHistoryWindowProjectsStyledCellsAsRenderTruth(t *testing.T) {
	track := NewHistoryTrack()
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventWritePrimaryCells, Cells: []Cell{
		{Text: "ERR", Width: 3, Style: CellStyle{FG: "ansi:1", Bold: true}},
		{Text: " ", Width: 1},
		{Text: "好", Width: 2, Style: CellStyle{FG: "#ffcc00", Underline: true}, LinkURL: "file://build.log", LinkParams: "line=7"},
		{Text: "ok", Width: 2, Style: CellStyle{FG: "idx:42"}},
	}})
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventForceCommitFrontier})

	window, err := track.LatestWindow(HistoryWindowRequest{Cols: 6, Rows: 4})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}

	if got := rowTexts(window.Rows); !reflect.DeepEqual(got, []string{"ERR 好", "ok"}) {
		t.Fatalf("plain text should be derived from styled cells, got %v", got)
	}
	first := window.Rows[0].Cells
	if len(first) != 3 {
		t.Fatalf("expected first visual row to keep styled cells, got %#v", first)
	}
	if first[0].Text != "ERR" || first[0].Width != 3 || first[0].Style.FG != "ansi:1" || !first[0].Style.Bold {
		t.Fatalf("lost first cell style %#v", first[0])
	}
	if first[2].Text != "好" || first[2].Width != 2 || first[2].Style.FG != "#ffcc00" || !first[2].Style.Underline || first[2].LinkURL == "" || first[2].LinkParams == "" {
		t.Fatalf("lost wide styled linked cell %#v", first[2])
	}
	if second := window.Rows[1].Cells; len(second) != 1 || second[0].Text != "ok" || second[0].Style.FG != "idx:42" {
		t.Fatalf("unexpected wrapped styled row %#v", second)
	}
}

func TestHistoryWindowSplitsWideStyledTextCellsDuringReflow(t *testing.T) {
	track := NewHistoryTrack()
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventWritePrimaryCells, Cells: []Cell{
		{Text: "abcdef", Width: 6, Style: CellStyle{FG: "ansi:4", Underline: true}, LinkURL: "file://long.txt"},
	}})
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventForceCommitFrontier})

	window, err := track.LatestWindow(HistoryWindowRequest{Cols: 3, Rows: 4})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := rowTexts(window.Rows); !reflect.DeepEqual(got, []string{"abc", "def"}) {
		t.Fatalf("styled text cell should reflow by display cells, got %v", got)
	}
	for _, row := range window.Rows {
		if len(row.Cells) != 3 {
			t.Fatalf("reflow should split multi-cell text into display cells, got %#v", row.Cells)
		}
		for _, cell := range row.Cells {
			if cell.Width != 1 || cell.Style.FG != "ansi:4" || !cell.Style.Underline || cell.LinkURL != "file://long.txt" {
				t.Fatalf("split cell lost style/link/width metadata %#v", cell)
			}
		}
	}
}

func TestHistoryWindowSplitsMixedWidthStyledTextCells(t *testing.T) {
	track := NewHistoryTrack()
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventWritePrimaryCells, Cells: []Cell{
		{Text: "a好", Width: 3, Style: CellStyle{FG: "ansi:5", Bold: true}},
	}})
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventForceCommitFrontier})

	window, err := track.LatestWindow(HistoryWindowRequest{Cols: 2, Rows: 4})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := rowTexts(window.Rows); !reflect.DeepEqual(got, []string{"a", "好"}) {
		t.Fatalf("mixed-width styled text should reflow by display cells, got %v", got)
	}
	if first := window.Rows[0].Cells; len(first) != 1 || first[0].Text != "a" || first[0].Width != 1 || first[0].Style.FG != "ansi:5" || !first[0].Style.Bold {
		t.Fatalf("unexpected first split row %#v", first)
	}
	if second := window.Rows[1].Cells; len(second) != 1 || second[0].Text != "好" || second[0].Width != 2 || second[0].Style.FG != "ansi:5" || !second[0].Style.Bold {
		t.Fatalf("unexpected second split row %#v", second)
	}
}

func TestHistoryWindowSplitsStyledRunsAtRemainingColumnBoundary(t *testing.T) {
	track := NewHistoryTrack()
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventWritePrimaryCells, Cells: []Cell{
		{Text: "x", Width: 1},
		{Text: "ab", Width: 2, Style: CellStyle{FG: "ansi:6", Italic: true}, LinkURL: "file://run.txt"},
	}})
	applyHistoryEvents(t, track, HistoryEvent{Kind: EventForceCommitFrontier})

	window, err := track.LatestWindow(HistoryWindowRequest{Cols: 2, Rows: 4})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := rowTexts(window.Rows); !reflect.DeepEqual(got, []string{"xa", "b"}) {
		t.Fatalf("styled run should split at remaining column boundary, got %v", got)
	}
	if first := window.Rows[0].Cells; len(first) != 2 || first[1].Text != "a" || first[1].Style.FG != "ansi:6" || !first[1].Style.Italic || first[1].LinkURL != "file://run.txt" {
		t.Fatalf("unexpected first row cells %#v", first)
	}
	if second := window.Rows[1].Cells; len(second) != 1 || second[0].Text != "b" || second[0].Style.FG != "ansi:6" || !second[0].Style.Italic || second[0].LinkURL != "file://run.txt" {
		t.Fatalf("unexpected second row cells %#v", second)
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

func TestCommittedCursorValidIgnoresViewportRowCount(t *testing.T) {
	track := NewHistoryTrack()
	commitLine(t, track, "abcdef")
	commitLine(t, track, "ghij")

	latest, err := track.LatestWindow(HistoryWindowRequest{Cols: 3, Rows: 2})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if !latest.Cursor.Valid {
		t.Fatalf("expected authoritative older cursor, got %#v", latest.Cursor)
	}
	if !track.CommittedCursorValid(3, latest.Cursor) {
		t.Fatalf("cursor should remain valid for same cols regardless of viewport rows, got %#v", latest.Cursor)
	}
	if !track.CommittedCursorValid(4, latest.Cursor) {
		t.Fatalf("logical boundary cursor should remain a valid committed boundary under reprojection, got %#v", latest.Cursor)
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
