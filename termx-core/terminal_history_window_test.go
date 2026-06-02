package termx

import (
	"context"
	"reflect"
	"testing"

	"github.com/lozzow/termx/termx-vterm/vterm"
)

func TestHistoryLineSpansGroupsWrappedRows(t *testing.T) {
	// 行0、行1 wrapped 续接到行2，组成一条逻辑行；行3 独立。
	wrapped := []bool{true, true, false, false}
	kinds := []string{"a", "a", "a", "b"}
	spans := historyLineSpans(wrapped, kinds, 4)
	want := []HistoryLineSpan{
		{StartRow: 0, EndRow: 2, RowKind: "a"},
		{StartRow: 3, EndRow: 3, RowKind: "b"},
	}
	if !reflect.DeepEqual(spans, want) {
		t.Fatalf("unexpected line spans, got %#v want %#v", spans, want)
	}
}

func TestHistoryLineSpansTrailingWrappedDoesNotOverrun(t *testing.T) {
	// 末行即使 wrapped=true，也必须收口成一条逻辑行，不能越界。
	wrapped := []bool{false, true}
	spans := historyLineSpans(wrapped, nil, 2)
	want := []HistoryLineSpan{
		{StartRow: 0, EndRow: 0},
		{StartRow: 1, EndRow: 1},
	}
	if !reflect.DeepEqual(spans, want) {
		t.Fatalf("unexpected trailing wrapped spans, got %#v want %#v", spans, want)
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
	if err := store.appendRows([]terminalGridRow{
		{cells: vtermCells("alpha")},
		{cells: vtermCells("bravo")},
		{cells: vtermCells("charlie")},
	}); err != nil {
		t.Fatalf("append grid rows: %v", err)
	}
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
	if len(window.Lines) != 3 {
		t.Fatalf("expected 3 logical line spans, got %#v", window.Lines)
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
	if err := store.appendRows(rows); err != nil {
		t.Fatalf("append grid rows: %v", err)
	}
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
	if older.BeforeOffset != 2 {
		t.Fatalf("expected before offset 2, got %d", older.BeforeOffset)
	}
	if len(older.Rows) == 0 {
		t.Fatal("expected older window to contain rows")
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
