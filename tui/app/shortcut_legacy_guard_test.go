package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShortcutLegacyExecutionSymbolsDoNotReturn(t *testing.T) {
	repoRoot := shortcutAuditRepoRoot(t)
	banned := map[string]bool{
		"ShellContentActionMsg":       true,
		"ShortcutActionRenderID":      true,
		"ProjectionByIDString":        true,
		"ProjectionActionIDs":         true,
		"reduceShellContentAction":    true,
		"reduceOverlayKeyboardAction": true,
	}
	for _, root := range []string{"tui", filepath.Join("cmd", "termx")} {
		err := filepath.WalkDir(filepath.Join(repoRoot, root), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" {
				return nil
			}
			file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if parseErr != nil {
				return parseErr
			}
			ast.Inspect(file, func(node ast.Node) bool {
				identifier, ok := node.(*ast.Ident)
				if !ok {
					return true
				}
				if banned[identifier.Name] || strings.HasPrefix(identifier.Name, "ActionFooter") {
					relative, _ := filepath.Rel(repoRoot, path)
					t.Errorf("legacy shortcut execution symbol %s returned in %s", identifier.Name, filepath.ToSlash(relative))
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk shortcut source root %s: %v", root, err)
		}
	}
}
