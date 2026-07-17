package client_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientPackagesRespectDependencyDirection(t *testing.T) {
	commonForbidden := []string{
		"github.com/lozzow/termx/tui",
		"github.com/lozzow/termx/cmd/termx",
		"github.com/lozzow/termx/private",
	}
	assertClientImportsExclude(t, "endpoint", append(commonForbidden,
		"github.com/lozzow/termx/client/runtime",
		"github.com/lozzow/termx/client/port",
		"github.com/lozzow/termx/client/adapter",
		"github.com/lozzow/termx/internal/protocol",
		"github.com/lozzow/termx/remote/client",
		"github.com/lozzow/termx/remote/webrtc",
	))
	assertClientImportsExclude(t, "runtime", append(commonForbidden,
		"github.com/lozzow/termx/client/adapter",
		"github.com/lozzow/termx/internal/protocol",
		"github.com/lozzow/termx/shared/transport",
		"github.com/lozzow/termx/shared/cloudcompanion",
		"github.com/lozzow/termx/shared/remoteauth",
		"github.com/lozzow/termx/remote/client",
		"github.com/lozzow/termx/remote/webrtc",
		"os",
		"path/filepath",
		"syscall/js",
	))
	assertClientImportsExclude(t, "port", append(commonForbidden,
		"github.com/lozzow/termx/client/runtime",
		"github.com/lozzow/termx/client/adapter",
		"github.com/lozzow/termx/internal/protocol",
		"github.com/lozzow/termx/shared/transport",
		"github.com/lozzow/termx/shared/cloudcompanion",
		"github.com/lozzow/termx/shared/remoteauth",
		"github.com/lozzow/termx/remote/client",
		"github.com/lozzow/termx/remote/webrtc",
	))
	assertClientImportsExclude(t, "binding", append(commonForbidden,
		"github.com/lozzow/termx/core",
		"github.com/lozzow/termx/client/adapter",
		"github.com/lozzow/termx/internal/protocol",
		"github.com/lozzow/termx/remote",
		"github.com/pion/webrtc",
	))
	assertClientImportsExclude(t, "adapter", commonForbidden)
}

func assertClientImportsExclude(t *testing.T, root string, forbidden []string) {
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
