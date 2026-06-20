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

func TestDaemonHistoryStorageFactoryDefaultsToStateDirectory(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)
	t.Setenv(daemonHistoryFileBackendDirEnv, "")
	t.Setenv(daemonHistoryBackendEnv, "")

	got := resolveDaemonHistoryFileBackendDir()
	want := filepath.Join(stateDir, "termx", "core-v2-history")
	if got != want {
		t.Fatalf("unexpected default history backend dir got=%q want=%q", got, want)
	}
	factory := newDaemonHistoryStorageFactory(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if factory == nil {
		t.Fatal("expected default history storage factory")
	}
	backend, err := factory("term-default")
	if err != nil {
		t.Fatalf("create default history backend: %v", err)
	}
	if closer, ok := backend.(interface{ Close() error }); ok {
		t.Cleanup(func() {
			_ = closer.Close()
		})
	}
	if _, err := os.Stat(filepath.Join(want, daemonHistoryBackendFileName("term-default"))); err != nil {
		t.Fatalf("expected default compact file: %v", err)
	}
}

func TestDaemonHistoryStorageFactoryCanBeDisabledForDiagnostics(t *testing.T) {
	t.Setenv(daemonHistoryBackendEnv, "memory")
	t.Setenv(daemonHistoryFileBackendDirEnv, t.TempDir())

	if got := resolveDaemonHistoryFileBackendDir(); got != "" {
		t.Fatalf("expected disabled history backend dir, got %q", got)
	}
	if factory := newDaemonHistoryStorageFactory(slog.New(slog.NewTextHandler(io.Discard, nil))); factory != nil {
		t.Fatal("expected nil factory when memory backend is requested")
	}
}
