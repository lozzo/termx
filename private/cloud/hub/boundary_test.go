package hub_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestHubExportsNoTerminalGrantOrBearerSchema(t *testing.T) {
	forbidden := []string{"Terminal", "Grant", "Bearer", "SessionToken", "AgentToken", "Inventory", "Heartbeat", "Kick"}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			field, ok := node.(*ast.Field)
			if !ok {
				return true
			}
			for _, name := range field.Names {
				if !name.IsExported() {
					continue
				}
				for _, fragment := range forbidden {
					if strings.Contains(name.Name, fragment) {
						t.Errorf("%s exports forbidden Hub field %q", path, name.Name)
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
