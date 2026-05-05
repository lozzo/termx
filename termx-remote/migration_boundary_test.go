package remote_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWF003RemoteProtocolSessionPackagesMigratedFromCore(t *testing.T) {
	for _, dir := range []string{
		"protocol/hubv1",
		"pairing",
		"discovery",
		"session/rtc",
		"cert",
		"identity",
		"bridge",
		"fileapi",
	} {
		requireGoPackageDir(t, dir)
	}

	for _, dir := range []string{
		"../termx-core/remote/hubv1",
		"../termx-core/internal/remote/pairing",
		"../termx-core/internal/remote/discovery",
		"../termx-core/internal/remote/rtc",
		"../termx-core/internal/remote/cert",
		"../termx-core/internal/remote/identity",
		"../termx-core/internal/remote/bridge",
		"../termx-core/internal/remote/fileapi",
	} {
		if files := nonTestGoFiles(t, dir); len(files) > 0 {
			t.Fatalf("termx-core still owns remote implementation files in %s: %v", dir, files)
		}
	}
}

func TestWF003CoreRedirectsToTermxRemotePackages(t *testing.T) {
	for _, root := range []string{"../termx-core", "."} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == ".git" || entry.Name() == "tmp" || entry.Name() == "third_party" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, forbidden := range []string{
				"github.com/lozzow/termx/termx-core/remote/hubv1",
				"github.com/lozzow/termx/termx-core/internal/remote/pairing",
				"github.com/lozzow/termx/termx-core/internal/remote/discovery",
				"github.com/lozzow/termx/termx-core/internal/remote/rtc",
				"github.com/lozzow/termx/termx-core/internal/remote/cert",
				"github.com/lozzow/termx/termx-core/internal/remote/identity",
				"github.com/lozzow/termx/termx-core/internal/remote/bridge",
				"github.com/lozzow/termx/termx-core/internal/remote/fileapi",
			} {
				if strings.Contains(string(data), forbidden) {
					t.Fatalf("%s still imports old core remote package %s", path, forbidden)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}

func nonTestGoFiles(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	var out []string
	for _, match := range matches {
		if strings.HasSuffix(match, "_test.go") || filepath.Base(match) == "doc.go" {
			continue
		}
		out = append(out, filepath.Base(match))
	}
	return out
}
