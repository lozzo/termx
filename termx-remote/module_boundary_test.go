package remote_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTermxRemoteModuleSkeleton(t *testing.T) {
	repoRoot := filepath.Clean("..")
	remoteRoot := "."

	requireFile(t, filepath.Join(remoteRoot, "AGENTS.md"))
	for _, dir := range []string{"agent", "hub", "protocol", "client"} {
		requireGoPackageDir(t, filepath.Join(remoteRoot, dir))
	}

	goMod := string(readFile(t, filepath.Join(remoteRoot, "go.mod")))
	for _, want := range []string{
		"module github.com/lozzow/termx/termx-remote",
		"github.com/lozzow/termx/termx-core v0.0.0",
		"replace github.com/lozzow/termx/termx-core => ../termx-core",
	} {
		if !strings.Contains(goMod, want) {
			t.Fatalf("termx-remote/go.mod missing %q:\n%s", want, goMod)
		}
	}
	for _, forbidden := range []string{
		"github.com/lozzow/termx/termx-cli",
		"github.com/lozzow/termx/termx-hub",
		"remote-ui",
	} {
		if strings.Contains(goMod, forbidden) {
			t.Fatalf("termx-remote/go.mod must not depend on %q:\n%s", forbidden, goMod)
		}
	}

	coreGoMod := string(readFile(t, filepath.Join(repoRoot, "termx-core", "go.mod")))
	if strings.Contains(coreGoMod, "github.com/lozzow/termx/termx-remote") {
		t.Fatalf("termx-core/go.mod must not depend on termx-remote:\n%s", coreGoMod)
	}

	goWork := string(readFile(t, filepath.Join(repoRoot, "go.work")))
	if !strings.Contains(goWork, "./termx-remote") {
		t.Fatalf("go.work does not include ./termx-remote:\n%s", goWork)
	}
}

func requireFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected file %s: %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("expected file %s, got directory", path)
	}
}

func requireGoPackageDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected package directory %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected package directory %s, got file", path)
	}
	matches, err := filepath.Glob(filepath.Join(path, "*.go"))
	if err != nil {
		t.Fatalf("glob %s: %v", path, err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected Go package files in %s", path)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
