package client_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientPackagesRespectDependencyDirection(t *testing.T) {
	commonForbidden := []string{
		"github.com/muxvia/muxvia/tui",
		"github.com/muxvia/muxvia/cmd/muxvia",
		"github.com/muxvia/muxvia/private",
	}
	assertClientImportsExclude(t, "endpoint", append(commonForbidden,
		"github.com/muxvia/muxvia/client/runtime",
		"github.com/muxvia/muxvia/client/port",
		"github.com/muxvia/muxvia/client/adapter",
		"github.com/muxvia/muxvia/internal/protocol",
		"github.com/muxvia/muxvia/remote/client",
		"github.com/muxvia/muxvia/remote/webrtc",
	))
	assertClientImportsExclude(t, "runtime", append(commonForbidden,
		"github.com/muxvia/muxvia/client/adapter",
		"github.com/muxvia/muxvia/internal/protocol",
		"github.com/muxvia/muxvia/remote/client",
		"github.com/muxvia/muxvia/remote/webrtc",
		"os",
		"path/filepath",
		"syscall/js",
	))
	assertClientImportsLimitedToFiles(t, "runtime", []string{
		"github.com/muxvia/muxvia/shared/transport",
		"github.com/muxvia/muxvia/shared/remoteauth",
	}, map[string]bool{"pairing.go": true})
	assertClientImportsExclude(t, "port", append(commonForbidden,
		"github.com/muxvia/muxvia/client/runtime",
		"github.com/muxvia/muxvia/client/adapter",
		"github.com/muxvia/muxvia/internal/protocol",
		"github.com/muxvia/muxvia/shared/transport",
		"github.com/muxvia/muxvia/shared/remoteauth",
		"github.com/muxvia/muxvia/remote/client",
		"github.com/muxvia/muxvia/remote/webrtc",
	))
	// binding 是 JNI/C/WASM 的公开 composition root，可以装配 concrete platform adapter；
	// 它仍不能反向依赖 TUI、CLI 或 private owner。
	assertClientImportsExclude(t, "binding", commonForbidden)
	assertClientImportsExclude(t, "adapter", commonForbidden)
}

// TestLegacyCloudPackagesStayDeleted 防止新 Cloud 实现重新依赖已经作废的目录与契约。
func TestLegacyCloudPackagesStayDeleted(t *testing.T) {
	for _, path := range []string{
		"adapter/managed",
		"../proto/cloudpb",
		"../shared/cloudcompanion",
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("legacy Cloud package must stay deleted: %s (stat error=%v)", path, err)
		}
	}
}

func assertClientImportsLimitedToFiles(t *testing.T, root string, prefixes []string, allowed map[string]bool) {
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
			for _, prefix := range prefixes {
				if (importPath == prefix || strings.HasPrefix(importPath, prefix+"/")) && !allowed[filepath.Base(path)] {
					t.Errorf("%s imports owner %s reserved for explicit pairing boundary", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
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
