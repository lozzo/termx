package main

import (
	"fmt"
	"os"

	"github.com/lozzow/termx/termx-proto/wire"
	tuiconfig "github.com/lozzow/termx/termx-tui-v3/config"
)

func resolveV3Socket(path string) string {
	if path != "" {
		return path
	}
	name := fmt.Sprintf("termx-v2-wire%d.sock", wire.Version)
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return runtimeDir + "/" + name
	}
	return fmt.Sprintf("%s/termx-v2-wire%d-%d.sock", os.TempDir(), wire.Version, os.Getuid())
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
