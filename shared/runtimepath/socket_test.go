package runtimepath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSocketPathUsesRuntimeDirectoryAndNativeSeparators(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	if got, want := SocketPath("muxvia.sock"), filepath.Join(runtimeDir, "muxvia.sock"); got != want {
		t.Fatalf("socket path = %q, want %q", got, want)
	}
}

func TestSocketPathFallsBackToUserScopedTemporaryPath(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	got := SocketPath("muxvia.sock")
	if filepath.Dir(got) != os.TempDir() || filepath.Base(got) == "muxvia.sock" || !strings.HasSuffix(got, ".sock") {
		t.Fatalf("fallback socket path is not user scoped: %q", got)
	}
}
