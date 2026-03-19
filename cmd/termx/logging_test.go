package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLogFilePathPrefersExplicitValue(t *testing.T) {
	t.Setenv("TERMX_LOG_FILE", filepath.Join(t.TempDir(), "ignored.log"))
	got := resolveLogFilePath("/tmp/termx-explicit.log")
	if got != "/tmp/termx-explicit.log" {
		t.Fatalf("expected explicit log path to win, got %q", got)
	}
}

func TestResolveLogFilePathUsesEnvironmentOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "termx-env.log")
	t.Setenv("TERMX_LOG_FILE", want)
	if got := resolveLogFilePath(""); got != want {
		t.Fatalf("expected TERMX_LOG_FILE path %q, got %q", want, got)
	}
}

func TestResolveLogFilePathFallsBackToXDGStateHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("TERMX_LOG_FILE", "")
	t.Setenv("XDG_STATE_HOME", base)
	got := resolveLogFilePath("")
	want := filepath.Join(base, "termx", "termx.log")
	if got != want {
		t.Fatalf("expected XDG fallback %q, got %q", want, got)
	}
}

func TestResolveWorkspaceStatePathFallsBackToXDGStateHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", base)
	got := resolveWorkspaceStatePath()
	want := filepath.Join(base, "termx", "workspace-state.json")
	if got != want {
		t.Fatalf("expected workspace state path %q, got %q", want, got)
	}
}

func TestOpenLogFileLoggerCreatesFileAndWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "termx.log")
	logger, closeFn, resolved, err := openLogFileLogger(path)
	if err != nil {
		t.Fatalf("openLogFileLogger returned error: %v", err)
	}
	defer closeFn()

	if resolved != path {
		t.Fatalf("expected resolved path %q, got %q", path, resolved)
	}

	logger.Info("hello-log", "component", "test")
	if err := closeFn(); err != nil {
		t.Fatalf("closeFn returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "hello-log") || !strings.Contains(text, "component=test") {
		t.Fatalf("expected log file to contain structured record, got:\n%s", text)
	}
}
