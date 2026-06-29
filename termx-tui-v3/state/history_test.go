package state

import (
	"errors"
	"reflect"
	"strings"
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

func TestHistoryStoreAppliesOldestReplaceAndNewerAppend(t *testing.T) {
	store := HistoryStore{
		PaneID:     "pane-main",
		ViewID:     "pane:pane-main",
		TerminalID: "term-1",
		Token:      "tok-1",
		Cols:       80,
		Rows: []HistoryRow{{
			Text:               "tail",
			LineID:             30,
			ProjectionRowIndex: 4,
		}},
		SourceLines: []HistoryLogicalLine{{Text: "tail", LineID: 30}},
		Lines:       []HistoryLineSpan{{LineID: 30, StartRow: 0, EndRow: 0}},
		Cursor:      HistoryCursor{Valid: true, BeforeLineID: 30, BeforeRowIndex: 4, Segment: HistoryCursorSegmentCommitted},
		Generation:  9,
		Boundary:    HistoryBoundary{FirstLineID: 30, LastLineID: 50},
		HasMore:     true,
	}

	store, err := store.BeginOldest(HistoryPendingRequest{
		ID:         21,
		PaneID:     "pane-main",
		ViewID:     "pane:pane-main",
		TerminalID: "term-1",
		Cols:       80,
		Token:      "tok-1",
		Generation: 9,
		Boundary:   store.Boundary,
	})
	if err != nil {
		t.Fatalf("begin oldest: %v", err)
	}
	got, inserted, err := store.ApplyWindow(21, HistoryWindow{
		PaneID:     "pane-main",
		ViewID:     "pane:pane-main",
		TerminalID: "term-1",
		Token:      "tok-1",
		Op:         HistoryWindowReplace,
		Cols:       80,
		Rows: []HistoryRow{{
			Text:               "head",
			LineID:             10,
			ProjectionRowIndex: 0,
		}},
		SourceLines: []HistoryLogicalLine{{Text: "head", LineID: 10}},
		Lines:       []HistoryLineSpan{{LineID: 10, StartRow: 0, EndRow: 0}},
		Cursor:      HistoryCursor{Valid: false},
		Generation:  9,
		Boundary:    HistoryBoundary{FirstLineID: 10, LastLineID: 50},
		HasMore:     false,
	})
	if err != nil {
		t.Fatalf("apply oldest: %v", err)
	}
	if inserted != 1 || len(got.Rows) != 1 || got.Rows[0].Text != "head" || got.Boundary.FirstLineID != 10 || got.Boundary.LastLineID != 50 {
		t.Fatalf("oldest replace not accepted correctly: inserted=%d store=%#v", inserted, got)
	}
	if got.NewerRequestState() != NewerRequestReady {
		t.Fatalf("head-only window should be able to page newer, got %s store=%#v", got.NewerRequestState(), got)
	}

	got, err = got.BeginNewer(HistoryPendingRequest{
		ID:         22,
		PaneID:     "pane-main",
		ViewID:     "pane:pane-main",
		TerminalID: "term-1",
		Cols:       80,
		Token:      "tok-1",
		Generation: 9,
		Cursor:     HistoryCursor{Valid: true, BeforeLineID: 10, BeforeRowIndex: 0, Segment: HistoryCursorSegmentCommitted},
		Boundary:   got.Boundary,
	})
	if err != nil {
		t.Fatalf("begin newer: %v", err)
	}
	got, inserted, err = got.ApplyWindow(22, HistoryWindow{
		PaneID:     "pane-main",
		ViewID:     "pane:pane-main",
		TerminalID: "term-1",
		Token:      "tok-1",
		Op:         HistoryWindowAppend,
		Cols:       80,
		Rows: []HistoryRow{{
			Text:               "next",
			LineID:             20,
			ProjectionRowIndex: 1,
		}},
		SourceLines: []HistoryLogicalLine{{Text: "next", LineID: 20}},
		Lines:       []HistoryLineSpan{{LineID: 20, StartRow: 0, EndRow: 0}},
		Cursor:      HistoryCursor{Valid: true, BeforeLineID: 20, BeforeRowIndex: 1, Segment: HistoryCursorSegmentCommitted},
		Generation:  9,
		Boundary:    HistoryBoundary{FirstLineID: 10, LastLineID: 50},
		HasMore:     true,
	})
	if err != nil {
		t.Fatalf("apply newer: %v", err)
	}
	if inserted != 1 || len(got.Rows) != 2 || got.Rows[0].Text != "head" || got.Rows[1].Text != "next" {
		t.Fatalf("newer append not accepted correctly: inserted=%d store=%#v", inserted, got)
	}
	if got.Lines[1].StartRow != 1 || got.Boundary.FirstLineID != 10 || got.Boundary.LastLineID != 50 || got.Cursor.BeforeRowIndex != 1 {
		t.Fatalf("newer append did not preserve authoritative boundary/cursor: %#v", got)
	}
}

func TestHistoryStoreReflowsFrozenLogicalLinesAtLocalCols(t *testing.T) {
	lines := []HistoryLogicalLine{{
		Text:   "abcdef",
		LineID: 10,
		Cells: []HistoryCell{
			{Text: "abc", Width: 3},
			{Text: "def", Width: 3},
		},
	}}
	rows, spans := ReflowHistoryLogicalLines(lines, 3)
	if got := historyTestRowTexts(rows); !reflect.DeepEqual(got, []string{"abc", "def"}) {
		t.Fatalf("expected local reflow rows, got %v", got)
	}
	if len(spans) != 1 || spans[0].StartRow != 0 || spans[0].EndRow != 1 || rows[1].RowInLine != 1 {
		t.Fatalf("reflow span/row metadata wrong: rows=%#v spans=%#v", rows, spans)
	}
}

func TestHistoryStoreReflowPreservesAuthoritativeCellPadding(t *testing.T) {
	lines := []HistoryLogicalLine{{
		LineID: 10,
		Cells: []HistoryCell{
			{Text: "AGENTS.md", Width: 9},
			{Text: "go.work", Width: 9},
			{Text: "README.md", Width: 9},
		},
	}}
	rows, _ := ReflowHistoryLogicalLines(lines, 40)
	if got := rows[0].Text; got != "AGENTS.mdgo.work  README.md" {
		t.Fatalf("reflow text should materialize authoritative cell padding, got %q", got)
	}
}

func TestHistoryStoreDoesNotReflowFixedGridRows(t *testing.T) {
	lines := []HistoryLogicalLine{{
		LineID:     10,
		Kind:       "screen-frame",
		FixedGrid:  true,
		ScreenCols: 80,
		Cells: []HistoryCell{
			{Text: "model:", Width: 19},
			{Text: "gpt-5.5 xhigh", Width: 13},
		},
	}}
	rows, spans := ReflowHistoryLogicalLines(lines, 12)
	if len(rows) != 1 || rows[0].Kind != "screen-frame" || rows[0].Text != "model:             gpt-5.5 xhigh" {
		t.Fatalf("screen frame row must remain one fixed-grid row with padding, rows=%#v", rows)
	}
	if spans[0].StartRow != 0 || spans[0].EndRow != 0 {
		t.Fatalf("fixed-grid span should stay one row, got %#v", spans)
	}
}

func TestHistoryStoreTrimRowsPreservesProjectionCursorAndFrozenTail(t *testing.T) {
	source := make([]HistoryLogicalLine, 0, 8)
	for i := 0; i < 8; i++ {
		source = append(source, HistoryLogicalLine{
			Text:               "line",
			LineID:             uint64(240 + i),
			Segment:            HistoryCursorSegmentCommitted,
			ProjectionRowIndex: 240 + i,
		})
	}
	store := HistoryStore{
		Cols:        80,
		SourceLines: source,
		Cursor:      HistoryCursor{Valid: true, BeforeLineID: 240, BeforeRowIndex: 240, Segment: HistoryCursorSegmentCommitted},
		Boundary:    HistoryBoundary{FirstLineID: 240, LastLineID: 247},
	}
	store.Rows, store.Lines = ReflowHistoryLogicalLines(store.SourceLines, store.Cols)

	trimmed, result := store.TrimRows(2, 6)

	if result.DroppedRowsBefore != 2 || result.DroppedRowsAfter != 1 || result.DroppedLinesBefore != 2 || result.DroppedLinesAfter != 1 {
		t.Fatalf("unexpected trim accounting: %#v", result)
	}
	if len(trimmed.Rows) != 5 || len(trimmed.SourceLines) != 5 {
		t.Fatalf("trim should keep only local source window, rows=%d sources=%d", len(trimmed.Rows), len(trimmed.SourceLines))
	}
	if trimmed.Boundary.FirstLineID != 242 || trimmed.Boundary.LastLineID != 247 {
		t.Fatalf("trim must update local first boundary but keep frozen tail guard, got %#v", trimmed.Boundary)
	}
	if trimmed.Cursor.BeforeLineID != 242 || trimmed.Cursor.BeforeRowIndex != 242 || trimmed.Cursor.Segment != HistoryCursorSegmentCommitted {
		t.Fatalf("trim must preserve authoritative projection cursor, got %#v", trimmed.Cursor)
	}
}

func TestHistoryStoreTrimRowsUsesSourceIdentityNotLineID(t *testing.T) {
	store := HistoryStore{
		Cols: 80,
		SourceLines: []HistoryLogicalLine{
			{Text: "old", LineID: 42, Kind: "screen-frame", Segment: HistoryCursorSegmentCurrentPrimaryFrame, SessionID: 3, FrameID: 10, FixedGrid: true, ScreenCols: 80, ProjectionRowIndex: 240},
			{Text: "new", LineID: 42, Kind: "screen-frame", Segment: HistoryCursorSegmentCurrentPrimaryFrame, SessionID: 3, FrameID: 11, FixedGrid: true, ScreenCols: 80, ProjectionRowIndex: 241},
		},
		Cursor:   HistoryCursor{Valid: true, BeforeLineID: 42, BeforeRowIndex: 240, Segment: HistoryCursorSegmentCurrentPrimaryFrame},
		Boundary: HistoryBoundary{FirstLineID: 42, LastLineID: 42},
	}
	store.Rows, store.Lines = ReflowHistoryLogicalLines(store.SourceLines, store.Cols)

	trimmed, result := store.TrimRows(1, 1)

	if result.DroppedLinesBefore != 1 || len(trimmed.SourceLines) != 1 {
		t.Fatalf("trim should keep only second source identity, result=%#v store=%#v", result, trimmed)
	}
	if trimmed.SourceLines[0].FrameID != 11 || trimmed.Cursor.BeforeRowIndex != 241 {
		t.Fatalf("trim must key by frame identity and projection row, got cursor=%#v source=%#v", trimmed.Cursor, trimmed.SourceLines)
	}
}

func TestHistoryStoreMergesBoundaryOverlapWhenPrependingOlderWindow(t *testing.T) {
	store := HistoryStore{
		TerminalID: "term-1",
		Token:      "tok-1",
		Cols:       4,
		SourceLines: []HistoryLogicalLine{{
			Text:          "cdef",
			LineID:        10,
			Segment:       HistoryCursorSegmentCommitted,
			ClippedBefore: true,
			ClippedAfter:  false,
		}},
		Cursor:     HistoryCursor{Valid: true, BeforeLineID: 10, Segment: HistoryCursorSegmentCommitted},
		Generation: 7,
		Boundary:   HistoryBoundary{FirstLineID: 10, LastLineID: 10},
	}
	store.Rows, store.Lines = ReflowHistoryLogicalLines(store.SourceLines, store.Cols)
	store, err := store.BeginOlder(HistoryPendingRequest{ID: 31, TerminalID: "term-1", Cols: 4, Token: "tok-1", Generation: 7, Cursor: store.Cursor, Boundary: store.Boundary})
	if err != nil {
		t.Fatalf("begin older: %v", err)
	}
	window := HistoryWindow{
		TerminalID: "term-1",
		Token:      "tok-1",
		Op:         HistoryWindowPrepend,
		Cols:       4,
		SourceLines: []HistoryLogicalLine{{
			Text:         "ab",
			LineID:       10,
			Segment:      HistoryCursorSegmentCommitted,
			ClippedAfter: true,
		}},
		Cursor:     HistoryCursor{Valid: false},
		Generation: 7,
		Boundary:   HistoryBoundary{FirstLineID: 10, LastLineID: 10},
	}
	window.Rows, window.Lines = ReflowHistoryLogicalLines(window.SourceLines, window.Cols)

	got, _, err := store.ApplyWindow(31, window)
	if err != nil {
		t.Fatalf("apply older overlap: %v", err)
	}
	if len(got.SourceLines) != 1 || got.SourceLines[0].Text != "abcdef" || got.SourceLines[0].ClippedBefore || got.SourceLines[0].ClippedAfter {
		t.Fatalf("overlap partials must merge into one logical source, got %#v", got.SourceLines)
	}
	if gotText := strings.Join(historyTestRowTexts(got.Rows), "|"); gotText != "abcd|ef" {
		t.Fatalf("merged source should reflow at local cols, got %q rows=%#v", gotText, got.Rows)
	}
}

func TestCopyModeLogicalSelectionUsesLogicalLineDisplayColumns(t *testing.T) {
	history := HistoryStore{
		Cols: 4,
		SourceLines: []HistoryLogicalLine{
			{Text: "abcdefgh", LineID: 10, Segment: HistoryCursorSegmentCommitted},
		},
	}
	history.Rows, history.Lines = ReflowHistoryLogicalLines(history.SourceLines, history.Cols)

	copyMode := CopyModeStore{}.SetMark(CopyPosition{Row: 0, Col: 2})
	copyMode = copyMode.MoveCursor(CopyPosition{Row: 1, Col: 3})
	copyMode = copyMode.RefreshLogicalSelection(history)

	if copyMode.Selection == nil {
		t.Fatal("expected selection")
	}
	if copyMode.Selection.LogicalAnchor != (CopyLogicalPosition{Valid: true, LineID: 10, Col: 2}) {
		t.Fatalf("anchor should map to logical col in first row, got %#v", copyMode.Selection.LogicalAnchor)
	}
	if copyMode.Selection.LogicalFocus != (CopyLogicalPosition{Valid: true, LineID: 10, Col: 7}) {
		t.Fatalf("focus should include previous reflow row width, got %#v rows=%#v", copyMode.Selection.LogicalFocus, history.Rows)
	}
	start, end, ok := copyMode.SelectionLogicalRange(history)
	if !ok || start.LineID != 10 || start.Col != 2 || end.LineID != 10 || end.Col != 7 {
		t.Fatalf("logical range not returned from selection, start=%#v end=%#v ok=%v", start, end, ok)
	}
}

func TestCopyModeSelectionNeedsBackendAfterTrimmedAnchor(t *testing.T) {
	original := HistoryStore{
		Rows: []HistoryRow{
			{Text: "old", LineID: 10},
			{Text: "current", LineID: 30},
		},
	}
	copyMode := CopyModeStore{}.SetMark(CopyPosition{Row: 0, Col: 0})
	copyMode = copyMode.MoveCursor(CopyPosition{Row: 1, Col: 3})
	copyMode = copyMode.RefreshLogicalSelection(original)

	trimmed := HistoryStore{
		Rows: []HistoryRow{
			{Text: "current", LineID: 30},
		},
	}
	if !copyMode.SelectionNeedsBackend(trimmed) {
		t.Fatalf("trimmed-away logical anchor should require backend copy, selection=%#v", copyMode.Selection)
	}
	start, end, ok := copyMode.SelectionLogicalRange(trimmed)
	if !ok || start.LineID != 10 || end.LineID != 30 || end.Col != 3 {
		t.Fatalf("logical range should preserve trimmed anchor and current focus, start=%#v end=%#v ok=%v", start, end, ok)
	}
	copyMode = copyMode.RefreshLogicalSelectionFocus(trimmed)
	if copyMode.Selection == nil || copyMode.Selection.LogicalAnchor.LineID != 10 || copyMode.Selection.LogicalFocus.LineID != 30 {
		t.Fatalf("focus refresh must not clobber trimmed anchor, got %#v", copyMode.Selection)
	}
}

func TestHistoryTraceWindowSummarySamplesIdentityAndEscapesText(t *testing.T) {
	rows := make([]HistoryRow, 10)
	for i := range rows {
		rows[i] = HistoryRow{
			Text:               "row\ntext",
			LineID:             uint64(100 + i),
			RowInLine:          i % 2,
			Segment:            HistoryCursorSegmentCommitted,
			Kind:               "logical",
			SessionID:          3,
			FrameID:            4,
			FixedGrid:          true,
			ScreenCols:         80,
			ProjectionRowIndex: i,
			ClippedStart:       i == 0,
			ClippedEnd:         i == len(rows)-1,
		}
	}

	summary := HistoryTraceWindowSummary(rows)
	if strings.Count(summary, " || ") != 7 {
		t.Fatalf("summary should sample first and last rows, got %q", summary)
	}
	for _, want := range []string{
		`i=0 projection=0 line=100 row=0`,
		`i=9 projection=9 line=109 row=1`,
		`segment=committed`,
		`fixed=true cols=80`,
		`text="row\\ntext"`,
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q in %q", want, summary)
		}
	}
}

func historyTestRowTexts(rows []HistoryRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Text)
	}
	return out
}
