package remote_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRP005TopLevelHubWasReplacedByPrivateServices(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "termx-hub")); !os.IsNotExist(err) {
		t.Fatalf("legacy top-level termx-hub still exists: %v", err)
	}
	for _, dir := range []string{
		filepath.Join("..", "private", "termx-cloud", "hub"),
		filepath.Join("..", "private", "termx-cloud", "relay"),
	} {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
			t.Fatalf("private service module %s is missing: %v", dir, err)
		}
	}
}

func requireSourceNotContains(t *testing.T, path string, forbidden []string) {
	t.Helper()
	data := string(readFile(t, path))
	for _, value := range forbidden {
		if strings.Contains(data, value) {
			t.Fatalf("%s still contains forbidden implementation symbol %q", path, value)
		}
	}
}
