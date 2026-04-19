package main

import (
	"log/slog"
	"os"
	"path/filepath"
)

func resolveLogFilePath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if path := os.Getenv("TERMX_LOG_FILE"); path != "" {
		return path
	}
	if stateDir := os.Getenv("XDG_STATE_HOME"); stateDir != "" {
		return filepath.Join(stateDir, "termx", "termx.log")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "state", "termx", "termx.log")
	}
	return filepath.Join(os.TempDir(), "termx.log")
}

func resolveWorkspaceStatePath() string {
	if stateDir := os.Getenv("XDG_STATE_HOME"); stateDir != "" {
		return filepath.Join(stateDir, "termx", "workspace-state.json")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "state", "termx", "workspace-state.json")
	}
	return filepath.Join(os.TempDir(), "termx-workspace-state.json")
}

func openLogFileLogger(explicit string) (*slog.Logger, func() error, string, error) {
	path := resolveLogFilePath(explicit)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, path, err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, path, err
	}
	handler := slog.NewTextHandler(file, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler).With("pid", os.Getpid())
	closeFn := func() error {
		return file.Close()
	}
	return logger, closeFn, path, nil
}
