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
	if legacyFiles, err := filepath.Glob("legacy_*.go"); err != nil {
		t.Fatalf("glob legacy go files: %v", err)
	} else if len(legacyFiles) > 0 {
		t.Fatalf("legacy command files must not be restored: %v", legacyFiles)
	}
	for _, path := range files {
		base := filepath.Base(path)
		if strings.HasSuffix(base, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(data)
		for _, forbidden := range []string{
			"\"github.com/anytty/anytty/termx-core\"",
			"\"github.com/anytty/anytty/tuiv2",
			"\"github.com/anytty/anytty/termx-remote\"",
			"\"github.com/anytty/anytty/termx-remote/",
			"\"github.com/anytty/anytty/termx-hub",
			"\"github.com/anytty/anytty/web-control",
			"github.com/anytty/anytty/private/",
			"ANYTTY_HUB_AGENT_TOKEN",
			"ANYTTY_HUB_URL",
			"session_token",
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
		"github.com/anytty/anytty/termx-core v0.0.0",
		"github.com/anytty/anytty/tuiv2 v0.0.0",
		"github.com/anytty/anytty/termx-remote v0.0.0",
		"github.com/anytty/anytty/termx-hub v0.0.0",
		"github.com/anytty/anytty/web-control v0.0.0",
		"github.com/anytty/anytty/private/",
	} {
		if strings.Contains(string(goMod), legacyModule) {
			t.Fatalf("legacy module %s must not remain in root go.mod:\n%s", legacyModule, goMod)
		}
	}
}
