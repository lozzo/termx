package companion_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCompanionDoesNotImportPublicRuntimeSecurityOrTerminalOwners(t *testing.T) {
	forbiddenImports := []string{
		"/core",
		"/remote",
		"/shared/remoteauth",
		"/shared/transport/datachannel",
		"/internal/protocol",
		"github.com/pion/",
	}
	forbiddenExportedNames := []string{"CapabilityGrant", "PrivateKey", "DataChannel", "TerminalID", "TerminalScope", "ProtocolFrame"}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			pathValue, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			for _, forbidden := range forbiddenImports {
				if strings.Contains(pathValue, forbidden) {
					t.Errorf("%s imports forbidden owner %q", path, pathValue)
				}
			}
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
				for _, forbidden := range forbiddenExportedNames {
					if strings.Contains(name.Name, forbidden) {
						t.Errorf("%s exports forbidden companion field %q", path, name.Name)
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
