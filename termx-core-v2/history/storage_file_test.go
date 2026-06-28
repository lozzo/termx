package history

import (
	"strings"
	"testing"
)

func TestR341FileStorageBackendServesWindowPayloads(t *testing.T) {
	backend, err := NewFileStorageBackend(t.TempDir(), "term-r341-file")
	if err != nil {
		t.Fatalf("create file backend: %v", err)
	}
	store := NewBackendHistoryStore("term-r341-file", backend)
	for i := 1; i <= 20; i++ {
		applyStoreBatch(t, store, sealedLineMutations(LogicalLineID(i), HistoryRecordID(i), "file-line"))
	}
	raw := store.(*inMemoryHistoryStore)
	if got := len(raw.lines); got != 0 {
		t.Fatalf("file-backed store must spill sealed payloads out of hot map, got %d", got)
	}
	window, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r341-file", Limit: 3, Cols: 80})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := strings.Join(rowTexts(window.Rows), "|"); got != "file-line|file-line|file-line" {
		t.Fatalf("file backend should load payloads for window, got %q", got)
	}
}
