package apilayer

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPILayerAndTransformerRespectDependencyDirection(t *testing.T) {
	assertImportsExclude(t, ".", []string{
		"github.com/lozzow/termx/client",
		"github.com/lozzow/termx/cmd",
		"github.com/lozzow/termx/internal/protocol",
		"github.com/lozzow/termx/private",
		"github.com/lozzow/termx/proto/runtimepb",
		"github.com/lozzow/termx/proto/wirepb",
		"github.com/lozzow/termx/remote",
		"github.com/lozzow/termx/shared/transport",
		"github.com/lozzow/termx/tui",
	})
	assertImportsExclude(t, "../transformer", []string{
		"github.com/lozzow/termx/api_layer",
		"github.com/lozzow/termx/client",
		"github.com/lozzow/termx/cmd",
		"github.com/lozzow/termx/internal/protocol",
		"github.com/lozzow/termx/private",
		"github.com/lozzow/termx/proto/runtimepb",
		"github.com/lozzow/termx/proto/wirepb",
		"github.com/lozzow/termx/remote",
		"github.com/lozzow/termx/shared",
		"github.com/lozzow/termx/tui",
	})
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
					t.Errorf("%s imports forbidden owner %s", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
