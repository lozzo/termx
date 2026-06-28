package main

import (
	"fmt"
	"os"

	tuiconfig "github.com/lozzow/termx/termx-tui-v3/config"
)

func resolveV3Socket(path string) string {
	if path != "" {
		return path
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return runtimeDir + "/termx-v2.sock"
	}
	return fmt.Sprintf("%s/termx-v2-%d.sock", os.TempDir(), os.Getuid())
}

func resolveV3LogFilePath(path string) string {
	return resolveLogFilePath(path)
}

func v3ConfigPathPolicy() string {
	return tuiconfig.DefaultPath()
}

func v3StatePathPolicy() string {
	return "unused"
}

func resolveV3HistoryStorageDir() string {
	return resolveStateFilePath("history-v2")
}
