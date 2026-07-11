package routeplanner_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRoutePlannerMeasurementBaselineDoesNotImportAuthorizationOrRuntimeOwners(t *testing.T) {
	forbidden := []string{
		"/core",
		"/remote",
		"/shared/remoteauth",
		"/internal/protocol",
		"/private/cloud/hub",
		"/private/cloud/relay",
	}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, importSpec := range file.Imports {
			imported, err := strconv.Unquote(importSpec.Path.Value)
			if err != nil {
				return err
			}
			for _, fragment := range forbidden {
				if strings.Contains(imported, fragment) {
					t.Fatalf("measurement source %s imports forbidden owner %q", path, imported)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan route planner measurement boundary: %v", err)
	}
}
