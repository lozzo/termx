package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultRuntimeSourceDoesNotImportLegacyCoreOrTUI(t *testing.T) {
	root := filepath.Join("..", "..")
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob go files: %v", err)
	}
	for _, path := range files {
		base := filepath.Base(path)
		if strings.HasSuffix(base, "_test.go") ||
			strings.HasPrefix(base, "legacy_") ||
			strings.HasPrefix(base, "remote_") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(data)
		for _, forbidden := range []string{
			"\"github.com/lozzow/termx/termx-core\"",
			"\"github.com/lozzow/termx/tuiv2",
		} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("default runtime file %s must not import legacy dependency %s", path, forbidden)
			}
		}
	}

	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	for _, legacyModule := range []string{
		"github.com/lozzow/termx/termx-core v0.0.0",
		"github.com/lozzow/termx/tuiv2 v0.0.0",
	} {
		if !strings.Contains(string(goMod), legacyModule) {
			t.Fatalf("legacy module %s must remain explicit while legacy/remote fallback files are compiled", legacyModule)
		}
	}
}
