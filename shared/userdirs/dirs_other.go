//go:build !windows

package userdirs

import (
	"os"
	"path/filepath"
	"strings"
)

func platformConfigHome() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".config")
	}
	return filepath.Join(os.TempDir(), "muxvia-config")
}

func platformStateHome() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".local", "state")
	}
	return filepath.Join(os.TempDir(), "muxvia-state")
}
