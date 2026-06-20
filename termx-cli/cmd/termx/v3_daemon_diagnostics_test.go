package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestDaemonMemstatsStageFileIsSanitized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stage.txt")
	if err := os.WriteFile(path, []byte("copy oldest!\n"), 0o644); err != nil {
		t.Fatalf("write stage file: %v", err)
	}
	t.Setenv(daemonMemstatsStageFileEnv, path)

	if got := daemonHeapProfileReason(readDaemonMemstatsStageFile()); got != "copyoldest" {
		t.Fatalf("unexpected sanitized stage %q", got)
	}
}

func TestDaemonHistoryStorageFactoryUsesFileBackendDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(daemonHistoryFileBackendDirEnv, dir)
	factory := newDaemonHistoryStorageFactory(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if factory == nil {
		t.Fatal("expected history storage factory")
	}
	backend, err := factory("term/one")
	if err != nil {
		t.Fatalf("create history backend: %v", err)
	}
	if closer, ok := backend.(interface{ Close() error }); ok {
		t.Cleanup(func() {
			_ = closer.Close()
		})
	}
	if _, err := os.Stat(filepath.Join(dir, daemonHistoryBackendFileName("term/one"))); err != nil {
		t.Fatalf("expected compact file: %v", err)
	}
}
