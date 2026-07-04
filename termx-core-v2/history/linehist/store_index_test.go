package linehist

import (
	"os"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-core-v2/history"
)

func TestStoreRowIndexPersistsAndExtendsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	const terminalID = "term-row-index"
	file, err := OpenLineFile(dir, terminalID)
	if err != nil {
		t.Fatalf("open line file: %v", err)
	}
	if err := file.AppendLines([]Line{
		{Runs: []Run{{Text: "abcdef"}}, HardEnd: true},
		{Runs: []Run{{Text: "xy"}}, HardEnd: true},
	}); err != nil {
		t.Fatalf("append initial lines: %v", err)
	}
	rowIndexPath := file.RowIndexPath(4)
	store := NewStore(terminalID, NewEngine(file))
	first, err := store.LatestWindow(history.HistoryWindowRequest{Cols: 4, Limit: 100})
	if err != nil {
		t.Fatalf("first latest window: %v", err)
	}
	if strings.Join(windowTextsForTest(first), "|") != "abcd|ef|xy" {
		t.Fatalf("first projection = %v", windowTextsForTest(first))
	}
	assertRowIndexSizeForTest(t, rowIndexPath, 2)
	if err := store.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	reopenedFile, err := OpenLineFile(dir, terminalID)
	if err != nil {
		t.Fatalf("reopen line file: %v", err)
	}
	if err := reopenedFile.AppendLines([]Line{{Runs: []Run{{Text: "zzzzzzzz"}}, HardEnd: true}}); err != nil {
		t.Fatalf("append line after row index exists: %v", err)
	}
	reopened := NewStore(terminalID, NewEngine(reopenedFile))
	t.Cleanup(func() { _ = reopened.Close() })
	latest, err := reopened.LatestWindow(history.HistoryWindowRequest{Cols: 4, Limit: 100})
	if err != nil {
		t.Fatalf("latest after reopen: %v", err)
	}
	if strings.Join(windowTextsForTest(latest), "|") != "abcd|ef|xy|zzzz|zzzz" {
		t.Fatalf("projection after row index extension = %v", windowTextsForTest(latest))
	}
	assertRowIndexSizeForTest(t, rowIndexPath, 3)
}

func TestStoreRowIndexRebuildsAfterCorruption(t *testing.T) {
	dir := t.TempDir()
	const terminalID = "term-row-index-corrupt"
	file, err := OpenLineFile(dir, terminalID)
	if err != nil {
		t.Fatalf("open line file: %v", err)
	}
	if err := file.AppendLines([]Line{
		{Runs: []Run{{Text: "abcd"}}, HardEnd: true},
		{Runs: []Run{{Text: "efghij"}}, HardEnd: true},
	}); err != nil {
		t.Fatalf("append lines: %v", err)
	}
	rowIndexPath := file.RowIndexPath(3)
	store := NewStore(terminalID, NewEngine(file))
	if _, err := store.LatestWindow(history.HistoryWindowRequest{Cols: 3, Limit: 100}); err != nil {
		t.Fatalf("build initial row index: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close initial store: %v", err)
	}
	if err := os.WriteFile(rowIndexPath, []byte("bad-row-index"), 0o600); err != nil {
		t.Fatalf("corrupt row index: %v", err)
	}

	reopenedFile, err := OpenLineFile(dir, terminalID)
	if err != nil {
		t.Fatalf("reopen line file: %v", err)
	}
	reopened := NewStore(terminalID, NewEngine(reopenedFile))
	t.Cleanup(func() { _ = reopened.Close() })
	latest, err := reopened.LatestWindow(history.HistoryWindowRequest{Cols: 3, Limit: 100})
	if err != nil {
		t.Fatalf("latest after corrupt row index: %v", err)
	}
	if strings.Join(windowTextsForTest(latest), "|") != "abc|d|efg|hij" {
		t.Fatalf("projection after row index rebuild = %v", windowTextsForTest(latest))
	}
	assertRowIndexSizeForTest(t, rowIndexPath, 2)
}

func assertRowIndexSizeForTest(t *testing.T, path string, lineCount int) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat row index: %v", err)
	}
	want := int64(coldRowIndexHeaderSize + lineCount*coldRowIndexEntrySize)
	if info.Size() != want {
		t.Fatalf("row index size = %d, want %d", info.Size(), want)
	}
}
