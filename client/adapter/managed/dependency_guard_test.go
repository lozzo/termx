package managed_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortableManagedAdapterDoesNotImportNativePlatformOwners(t *testing.T) {
	forbidden := []string{
		"github.com/pion/webrtc",
		"github.com/lozzow/termx/remote/webrtc",
		"syscall/js",
		"C",
	}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != "." && entry.Name() == "pion" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, "_test.go") || !strings.HasSuffix(path, ".go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range parsed.Imports {
			importPath := strings.Trim(imported.Path.Value, `"`)
			for _, prefix := range forbidden {
				if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
					t.Errorf("portable managed adapter %s imports native owner %s", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
