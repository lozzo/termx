package remotev2_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicRemoteRuntimeDoesNotDependOnPrivateServices(t *testing.T) {
	forbidden := []string{
		"github.com/lozzow/termx/termx-hub",
		"github.com/lozzow/termx/web-control",
		"github.com/lozzow/termx/private/",
		"session_token",
		"/api/v1/sessions",
	}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, fragment := range forbidden {
			if strings.Contains(string(data), fragment) {
				t.Fatalf("public remote runtime %s contains forbidden dependency or legacy field %q", path, fragment)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan public remote runtime: %v", err)
	}
	goMod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	for _, fragment := range forbidden[:3] {
		if strings.Contains(string(goMod), fragment) {
			t.Fatalf("public remote go.mod contains forbidden dependency %q", fragment)
		}
	}
}
