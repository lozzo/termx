package remote_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWF003RemoteProtocolSessionPackagesMigratedFromCore(t *testing.T) {
	if _, err := os.Stat("../termx-core"); !os.IsNotExist(err) {
		t.Fatalf("legacy termx-core directory must remain deleted, stat err=%v", err)
	}
	for _, dir := range []string{
		"protocol/hubv1",
		"pairing",
		"discovery",
		"session/rtc",
		"identity",
		"bridge",
		"fileapi",
	} {
		requireGoPackageDir(t, dir)
	}

}

func TestWF003CoreRedirectsToTermxRemotePackages(t *testing.T) {
	for _, root := range []string{"."} {
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
