package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestAPIDoesNotDependOnImplementationWireOrConsumerPackages(t *testing.T) {
	packages, err := parser.ParseDir(token.NewFileSet(), ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{
		"github.com/lozzow/termx/client",
		"github.com/lozzow/termx/cmd",
		"github.com/lozzow/termx/internal/protocol",
		"github.com/lozzow/termx/private",
		"github.com/lozzow/termx/proto",
		"github.com/lozzow/termx/tui",
	}
	for _, pkg := range packages {
		for fileName, file := range pkg.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				importSpec, ok := node.(*ast.ImportSpec)
				if !ok {
					return true
				}
				path, unquoteErr := strconv.Unquote(importSpec.Path.Value)
				if unquoteErr != nil {
					t.Fatalf("%s: invalid import %s: %v", fileName, importSpec.Path.Value, unquoteErr)
				}
				for _, prefix := range forbidden {
					if path == prefix || strings.HasPrefix(path, prefix+"/") {
						t.Errorf("%s: core/api must not import %s", fileName, path)
					}
				}
				return false
			})
		}
	}
}
