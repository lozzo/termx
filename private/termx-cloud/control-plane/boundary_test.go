package controlplane_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestControlPlaneDoesNotImportTerminalAuthorizationOwners(t *testing.T) {
	forbidden := []string{
		"/termx-core-v2",
		"/termx-remote-v2",
		"/termx-shared/remoteauth",
		"/internal/protocol",
	}
	walkGoImports(t, ".", func(path, imported string) {
		for _, fragment := range forbidden {
			if strings.Contains(imported, fragment) {
				t.Errorf("%s imports forbidden terminal authorization owner %q", path, imported)
			}
		}
	})
}

func TestPublicNamespaceDoesNotImportPrivateCloud(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	entries, err := filepath.Glob(filepath.Join(repositoryRoot, "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Base(entry) == "private" || filepath.Base(entry) == ".git" {
			continue
		}
		info, err := os.Stat(entry)
		if err != nil || !info.IsDir() {
			continue
		}
		walkGoImports(t, entry, func(path, imported string) {
			if strings.Contains(imported, "/private/termx-cloud/") {
				t.Errorf("public source %s imports private cloud package %q", path, imported)
			}
		})
	}
}

func walkGoImports(t *testing.T, root string, visit func(path, imported string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			importSpec, ok := node.(*ast.ImportSpec)
			if !ok {
				return true
			}
			imported, err := strconv.Unquote(importSpec.Path.Value)
			if err == nil {
				visit(path, imported)
			}
			return false
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
