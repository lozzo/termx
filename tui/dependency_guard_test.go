package tui_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTUIDirectoryDependencies 保证 TUI 的 application port、adapter 和测试资产保持单向依赖。
func TestTUIDirectoryDependencies(t *testing.T) {
	assertImportsExclude(t, "port", []string{
		"github.com/lozzow/termx/internal/protocol",
		"github.com/lozzow/termx/tui/adapter",
		"github.com/lozzow/termx/tui/testkit",
		"os/exec",
	})
	for _, root := range []string{"state", "app", "render"} {
		assertImportsExclude(t, root, []string{
			"github.com/lozzow/termx/tui/adapter",
			"github.com/lozzow/termx/tui/testkit",
		})
	}
}

// TestTUIPortContainsOnlyProductionContracts 防止 fake 或宿主 IO 实现重新进入 application port。
func TestTUIPortContainsOnlyProductionContracts(t *testing.T) {
	err := filepath.WalkDir("port", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, forbidden := range []string{"type Fake", "SystemClipboardService", "exec.Command"} {
			if strings.Contains(string(content), forbidden) {
				t.Errorf("%s contains forbidden port implementation %q", path, forbidden)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertImportsExclude(t *testing.T, root string, forbidden []string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range parsed.Imports {
			importPath := strings.Trim(imported.Path.Value, `"`)
			for _, prefix := range forbidden {
				if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
					t.Errorf("%s imports forbidden dependency %s", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
