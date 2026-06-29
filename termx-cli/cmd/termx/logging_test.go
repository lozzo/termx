package main

import (
	"context"
	"io"
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

func TestResolveGridStatePathPrefersEnvironmentOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "grid")
	t.Setenv("TERMX_GRID_DIR", want)
	if got := resolveGridStatePath(); got != want {
		t.Fatalf("expected TERMX_GRID_DIR path %q, got %q", want, got)
	}
}

func TestResolveGridStatePathFallsBackToXDGStateHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("TERMX_GRID_DIR", "")
	t.Setenv("XDG_STATE_HOME", base)
	got := resolveGridStatePath()
	want := filepath.Join(base, "termx", "grid")
	if got != want {
		t.Fatalf("expected grid state path %q, got %q", want, got)
	}
}

func TestV3PathPolicy(t *testing.T) {
	runtimeDir := t.TempDir()
	stateHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	if got := resolveV3Socket(""); got != filepath.Join(runtimeDir, "termx-v2-wire2.sock") {
		t.Fatalf("expected v3 socket in runtime dir, got %q", got)
	}
	explicitSocket := filepath.Join(t.TempDir(), "explicit.sock")
	if got := resolveV3Socket(explicitSocket); got != explicitSocket {
		t.Fatalf("expected explicit v3 socket to win, got %q", got)
	}
	if got := resolveV3LogFilePath(""); got != filepath.Join(stateHome, "termx", "termx.log") {
		t.Fatalf("expected v3 log path to reuse global log policy, got %q", got)
	}
	if got := v3ConfigPathPolicy(); got != filepath.Join(configHome, "termx", "tui-v3.yaml") {
		t.Fatalf("expected v3 config path policy to use tui-v3.yaml, got %q", got)
	}
	if got := v3StatePathPolicy(); got != "unused" {
		t.Fatalf("expected v3 state path policy unused, got %q", got)
	}
}

func TestV3AttachDoesNotCreateTuiv2Config(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunAttach := runV3Attach
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runV3Attach = oldRunAttach
	})

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	isInteractiveTerminal = func() bool { return true }
	runV3Attach = func(ctx context.Context, cfg v3AttachConfig) error {
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--log-file", filepath.Join(t.TempDir(), "termx.log"), "v3", "attach", "term-1"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("v3 attach returned error: %v", err)
	}
	configPath := filepath.Join(configHome, "termx", "termx.yaml")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("v3 attach must not create tuiv2 config at %s, stat err=%v", configPath, err)
	}
}

func TestV3AttachLoadsTUIV3Config(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunAttach := runV3Attach
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runV3Attach = oldRunAttach
	})

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "termx")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "tui-v3.yaml"), []byte(`
version: 1
tui:
  theme:
    primary: "#d65cff"
`), 0o644); err != nil {
		t.Fatalf("write tui-v3 config: %v", err)
	}

	isInteractiveTerminal = func() bool { return true }
	var got v3AttachConfig
	runV3Attach = func(ctx context.Context, cfg v3AttachConfig) error {
		got = cfg
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--log-file", filepath.Join(t.TempDir(), "termx.log"), "v3", "attach", "term-1"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("v3 attach returned error: %v", err)
	}
	if got.TUIConfig.Theme.Primary != "#d65cff" {
		t.Fatalf("expected v3 attach to load tui-v3 config, got %#v", got.TUIConfig)
	}
}

func TestV3RootLoadsTUIV3Config(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunRoot := runV3Root
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runV3Root = oldRunRoot
	})

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "termx")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "tui-v3.yaml"), []byte(`
version: 1
tui:
  theme:
    secondary: "#66e3ff"
`), 0o644); err != nil {
		t.Fatalf("write tui-v3 config: %v", err)
	}

	isInteractiveTerminal = func() bool { return true }
	var got v3RootConfig
	runV3Root = func(ctx context.Context, cfg v3RootConfig) error {
		got = cfg
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--log-file", filepath.Join(t.TempDir(), "termx.log")})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("root command returned error: %v", err)
	}
	if got.TUIConfig.Theme.Secondary != "#66e3ff" {
		t.Fatalf("expected root command to load tui-v3 config, got %#v", got.TUIConfig)
	}
}

func TestV3AttachRejectsInvalidTUIV3Config(t *testing.T) {
	oldInteractive := isInteractiveTerminal
	oldRunAttach := runV3Attach
	t.Cleanup(func() {
		isInteractiveTerminal = oldInteractive
		runV3Attach = oldRunAttach
	})

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "termx")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "tui-v3.yaml"), []byte(`
version: 1
tui:
  theme:
    primary: red
`), 0o644); err != nil {
		t.Fatalf("write bad tui-v3 config: %v", err)
	}

	isInteractiveTerminal = func() bool { return true }
	runV3Attach = func(ctx context.Context, cfg v3AttachConfig) error {
		t.Fatalf("runV3Attach should not be called for invalid config")
		return nil
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--log-file", filepath.Join(t.TempDir(), "termx.log"), "v3", "attach", "term-1"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "must be empty or #RRGGBB") {
		t.Fatalf("expected invalid config error, got %v", err)
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
