package main

import (
	"fmt"
	"os"
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
	return "unused"
}

func v3StatePathPolicy() string {
	return "unused"
}
