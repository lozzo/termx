package termx

import (
	"reflect"
	"testing"
)

func TestTerminalGridProjectionReflowsLogicalLinesAtRequestedWidth(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("abcd"), rowKind: "output", wrapped: true},
		{cells: localVTermCellsFromString("ef"), rowKind: "output", wrapped: false},
		{cells: localVTermCellsFromString("WXYZ"), rowKind: "prompt", wrapped: false},
	}); err != nil {
		t.Fatalf("append logical rows: %v", err)
	}

	wide, err := store.Viewport(0, 10, 6)
	if err != nil {
		t.Fatalf("wide viewport: %v", err)
	}
	if got := vtermRowsToStrings(wide.Rows); !reflect.DeepEqual(got, []string{"abcdef", "WXYZ"}) {
		t.Fatalf("expected wide projection by logical line, got %#v", got)
	}
	if got := wide.Wrapped; !reflect.DeepEqual(got, []bool{false, false}) {
		t.Fatalf("expected wide projection wrapped flags to terminate each logical line, got %#v", got)
	}
	if wide.LogicalTotal != 2 || store.LogicalLineCount() != 2 {
		t.Fatalf("expected two persisted logical lines, viewport=%d store=%d", wide.LogicalTotal, store.LogicalLineCount())
	}

	narrow, err := store.Viewport(0, 10, 3)
	if err != nil {
		t.Fatalf("narrow viewport: %v", err)
	}
	if got := vtermRowsToStrings(narrow.Rows); !reflect.DeepEqual(got, []string{"abc", "def", "WXY", "Z"}) {
		t.Fatalf("expected narrow projection to reflow the same logical lines, got %#v", got)
	}
	if got := narrow.Wrapped; !reflect.DeepEqual(got, []bool{true, false, true, false}) {
		t.Fatalf("expected narrow projection wrapped flags to preserve logical line boundaries, got %#v", got)
	}
	if got := narrow.Ownership; !reflect.DeepEqual(got, []string{RowOwnershipPersisted, RowOwnershipPersisted, RowOwnershipPersisted, RowOwnershipPersisted}) {
		t.Fatalf("expected projected rows to stay persisted ownership, got %#v", got)
	}
}

func TestServerHistoryWindowMarksClippedLogicalLineAfterProjectionLimit(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "projection-clipped-window")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	if err := store.appendRows([]terminalGridRow{
		{cells: localVTermCellsFromString("aa"), rowKind: "line0", wrapped: false},
		{cells: localVTermCellsFromString("bbbb"), rowKind: "line1", wrapped: true},
		{cells: localVTermCellsFromString("cccc"), rowKind: "line1", wrapped: true},
		{cells: localVTermCellsFromString("dd"), rowKind: "line1", wrapped: false},
		{cells: localVTermCellsFromString("ee"), rowKind: "line2", wrapped: false},
	}); err != nil {
		t.Fatalf("append logical rows: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close grid store: %v", err)
	}

	srv := NewServer(WithGridRoot(root), WithDefaultSize(4, 2))
	window, err := srv.HistoryWindow(t.Context(), "projection-clipped-window", HistoryWindowOptions{BeforeOffset: 1, Limit: 2, Cols: 4})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if window.Op != HistoryWindowPrepend {
		t.Fatalf("expected older window to be prepend, got %q", window.Op)
	}
	if got := historyRowsToStrings(window.Rows); !reflect.DeepEqual(got, []string{"cccc", "dd"}) {
		t.Fatalf("expected projected window to contain the visible tail of the logical line, got %#v", got)
	}
	if got := historyRowsWrapped(window.Rows); !reflect.DeepEqual(got, []bool{true, false}) {
		t.Fatalf("expected projected wrapped flags for clipped logical line tail, got %#v", got)
	}
	if len(window.Lines) != 1 {
		t.Fatalf("expected one logical line span, got %#v", window.Lines)
	}
	span := window.Lines[0]
	if span.StartRow != 0 || span.EndRow != 1 || span.RowKind != "line1" || !span.ClippedBefore || span.ClippedAfter {
		t.Fatalf("expected clipped-before logical line span for visible tail, got %#v", span)
	}
	if window.FirstRowID != 1 || window.LastRowID != 3 {
		t.Fatalf("expected canonical row ids to cover the expanded logical line 1..3, got %d..%d", window.FirstRowID, window.LastRowID)
	}
	if !window.HasMore {
		t.Fatal("expected has more because an older logical line remains before the expanded window")
	}
}

func historyRowsToStrings(rows []HistoryRow) []string {
	if len(rows) == 0 {
		return nil
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowTextFromHistoryRow(row))
	}
	return out
}

func historyRowsWrapped(rows []HistoryRow) []bool {
	if len(rows) == 0 {
		return nil
	}
	out := make([]bool, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Wrapped)
	}
	return out
}
