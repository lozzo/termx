package apilayer

import (
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPILayerAndAPIMappingRespectDependencyDirection(t *testing.T) {
	assertImportsExclude(t, ".", []string{
		"github.com/muxvia/muxvia/client",
		"github.com/muxvia/muxvia/cmd",
		"github.com/muxvia/muxvia/internal/protocol",
		"github.com/muxvia/muxvia/private",
		"github.com/muxvia/muxvia/proto/runtimepb",
		"github.com/muxvia/muxvia/proto/wirepb",
		"github.com/muxvia/muxvia/remote",
		"github.com/muxvia/muxvia/shared/transport",
		"github.com/muxvia/muxvia/tui",
	})
	assertImportsExclude(t, "../api_mapping", []string{
		"github.com/muxvia/muxvia/api_layer",
		"github.com/muxvia/muxvia/client",
		"github.com/muxvia/muxvia/cmd",
		"github.com/muxvia/muxvia/internal/protocol",
		"github.com/muxvia/muxvia/private",
		"github.com/muxvia/muxvia/proto/runtimepb",
		"github.com/muxvia/muxvia/proto/wirepb",
		"github.com/muxvia/muxvia/remote",
		"github.com/muxvia/muxvia/shared",
		"github.com/muxvia/muxvia/tui",
	})
	assertImportsExclude(t, "../core", []string{
		"github.com/muxvia/muxvia/api_layer",
		"github.com/muxvia/muxvia/api_mapping",
	})
	if _, err := os.Stat("../core/application_api.go"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy core application adapter must stay deleted: %v", err)
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
