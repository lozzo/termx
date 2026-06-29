package state

import (
	"errors"
	"testing"
)

func TestHistoryStoreAcceptsLatestReplaceOnlyForPendingRequest(t *testing.T) {
	store, err := (HistoryStore{}).BeginLatest(HistoryPendingRequest{
		ID:         7,
		PaneID:     "pane-main",
		ViewID:     "pane:pane-main",
		TerminalID: "term-1",
		Cols:       80,
	})
	if err != nil {
		t.Fatalf("begin latest: %v", err)
	}

	window := HistoryWindow{
		PaneID:     "pane-main",
		ViewID:     "pane:pane-main",
		TerminalID: "term-1",
		Token:      "tok-1",
		Op:         HistoryWindowReplace,
		Cols:       80,
		Rows: []HistoryRow{{
			Text:      "first",
			LineID:    11,
			Segment:   HistoryCursorSegmentCommitted,
			RowInLine: 0,
		}},
		SourceLines: []HistoryLogicalLine{{
			Text:    "first",
			LineID:  11,
			Segment: HistoryCursorSegmentCommitted,
		}},
		Cursor: HistoryCursor{
			Valid:           true,
			BeforeLineID:    11,
			BeforeRowInLine: 0,
			BeforeRowIndex:  3,
			Segment:         HistoryCursorSegmentCommitted,
		},
		Boundary:   HistoryBoundary{FirstLineID: 11, LastLineID: 11},
		HasMore:    true,
		Generation: 5,
	}

	got, inserted, err := store.ApplyWindow(7, window)
	if err != nil {
		t.Fatalf("apply latest: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("unexpected inserted rows: %d", inserted)
	}
	if got.Pending != nil || got.Token != "tok-1" || got.TerminalID != "term-1" || got.Cursor.BeforeRowIndex != 3 {
		t.Fatalf("latest window not accepted correctly: %#v", got)
	}
	if got.Rows[0].Text != "first" || got.SourceLines[0].LineID != 11 {
		t.Fatalf("authoritative rows not stored: %#v", got)
	}
}

func TestHistoryStoreRejectsStaleResponse(t *testing.T) {
	store, err := (HistoryStore{}).BeginLatest(HistoryPendingRequest{
		ID:         9,
		TerminalID: "term-1",
		Cols:       80,
	})
	if err != nil {
		t.Fatalf("begin latest: %v", err)
	}

	_, _, err = store.ApplyWindow(8, HistoryWindow{
		TerminalID: "term-1",
		Op:         HistoryWindowReplace,
	})
	if !errors.Is(err, ErrStaleHistoryResponse) {
		t.Fatalf("stale request must be rejected, got %v", err)
	}
}

func TestHistoryStorePrependsOlderWindowAndMarksExhausted(t *testing.T) {
	store := HistoryStore{
		PaneID:     "pane-main",
		ViewID:     "pane:pane-main",
		TerminalID: "term-1",
		Token:      "tok-1",
		Rows: []HistoryRow{{
			Text:               "newer",
			LineID:             20,
			ProjectionRowIndex: 5,
		}},
		Lines: []HistoryLineSpan{{
			LineID:   20,
			StartRow: 0,
			EndRow:   0,
		}},
		Cursor:     HistoryCursor{Valid: true, BeforeLineID: 20, BeforeRowIndex: 5, Segment: HistoryCursorSegmentCommitted},
		Generation: 3,
		Boundary:   HistoryBoundary{FirstLineID: 20, LastLineID: 20},
		HasMore:    true,
	}
	store, err := store.BeginOlder(HistoryPendingRequest{
		ID:         10,
		PaneID:     "pane-main",
		ViewID:     "pane:pane-main",
		TerminalID: "term-1",
		Cols:       80,
		Token:      "tok-1",
		Generation: 3,
		Cursor:     store.Cursor,
		Boundary:   store.Boundary,
	})
	if err != nil {
		t.Fatalf("begin older: %v", err)
	}

	got, inserted, err := store.ApplyWindow(10, HistoryWindow{
		PaneID:     "pane-main",
		ViewID:     "pane:pane-main",
		TerminalID: "term-1",
		Token:      "tok-1",
		Op:         HistoryWindowPrepend,
		Rows: []HistoryRow{{
			Text:               "older",
			LineID:             10,
			ProjectionRowIndex: 1,
		}},
		Lines: []HistoryLineSpan{{
			LineID:   10,
			StartRow: 0,
			EndRow:   0,
		}},
		Cursor:     HistoryCursor{Valid: true, BeforeLineID: 10, BeforeRowIndex: 1, Segment: HistoryCursorSegmentCommitted},
		Generation: 3,
		Boundary:   HistoryBoundary{FirstLineID: 10, LastLineID: 20},
		HasMore:    false,
	})
	if err != nil {
		t.Fatalf("apply older: %v", err)
	}
	if inserted != 1 || got.Rows[0].Text != "older" || got.Rows[1].Text != "newer" {
		t.Fatalf("older rows not prepended: inserted=%d store=%#v", inserted, got)
	}
	if got.Lines[1].StartRow != 1 || got.Boundary.FirstLineID != 10 || got.Boundary.LastLineID != 20 {
		t.Fatalf("line spans or boundary not rebased: %#v", got)
	}

	got, err = got.BeginOlder(HistoryPendingRequest{
		ID:         11,
		PaneID:     "pane-main",
		ViewID:     "pane:pane-main",
		TerminalID: "term-1",
		Cols:       80,
		Token:      "tok-1",
		Generation: 3,
		Cursor:     got.Cursor,
		Boundary:   got.Boundary,
	})
	if err != nil {
		t.Fatalf("begin exhausted older: %v", err)
	}
	got, inserted, err = got.ApplyWindow(11, HistoryWindow{
		PaneID:     "pane-main",
		ViewID:     "pane:pane-main",
		TerminalID: "term-1",
		Token:      "tok-1",
		Op:         HistoryWindowPrepend,
		Generation: 3,
		HasMore:    false,
	})
	if err != nil {
		t.Fatalf("apply exhausted older: %v", err)
	}
	if inserted != 0 || got.OlderRequestState() != OlderRequestExhausted {
		t.Fatalf("empty final page must mark exhausted, inserted=%d state=%s store=%#v", inserted, got.OlderRequestState(), got)
	}
}
