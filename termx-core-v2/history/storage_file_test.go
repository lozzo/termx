package history

import (
	"go/parser"
	"go/token"
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

func TestR342FileStorageBackendDoesNotUseJSON(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "storage_file.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse storage file: %v", err)
	}
	for _, spec := range file.Imports {
		if spec.Path.Value == `"encoding/json"` {
			t.Fatalf("history file backend must stay binary; encoding/json import is not allowed")
		}
	}
	backend, err := NewFileStorageBackend(t.TempDir(), "term-r342-binary")
	if err != nil {
		t.Fatalf("create file backend: %v", err)
	}
	fileBackend := backend.(*fileStorageBackend)
	if strings.HasSuffix(fileBackend.path, ".jsonl") {
		t.Fatalf("history file backend must not use JSONL path: %s", fileBackend.path)
	}
}
