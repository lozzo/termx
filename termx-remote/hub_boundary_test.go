package remote_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWF003HubProductImplementationOwnedByTermxRemote(t *testing.T) {
	for _, dir := range []string{
		"hub/cloud",
		"hub/httpapi",
		"hub/ice",
		"hub/registry",
	} {
		requireGoPackageDir(t, dir)
	}
	forbiddenControlClientPackage := strings.Join([]string{"control", "client"}, "")
	for _, dir := range []string{
		"../termx-hub/internal/cloud",
		filepath.Join("..", "termx-hub", "internal", forbiddenControlClientPackage),
		"../termx-hub/internal/httpapi",
		"../termx-hub/internal/ice",
		"../termx-hub/internal/registry",
	} {
		if files := nonTestGoFiles(t, dir); len(files) > 0 {
			t.Fatalf("termx-hub still owns hub product implementation files in %s: %v", dir, files)
		}
		if testFiles, err := filepath.Glob(filepath.Join(dir, "*_test.go")); err == nil && len(testFiles) > 0 {
			t.Fatalf("termx-hub still owns hub product tests in %s: %v", dir, testFiles)
		}
	}
	requireSourceNotContains(t, "../termx-hub/cmd/termx-hub/main.go", []string{
		"type hubHeartbeatConfig",
		"func runHubHeartbeatLoop",
		"func postHubHeartbeat",
		forbiddenControlClientPackage,
		"TERMX_HUB_CONTROL_URL",
		"TERMX_HUB_CONTROL_SECRET",
	})
}

func requireSourceNotContains(t *testing.T, path string, forbidden []string) {
	t.Helper()
	data := string(readFile(t, path))
	for _, value := range forbidden {
		if strings.Contains(data, value) {
			t.Fatalf("%s still contains hub product implementation symbol %q", path, value)
		}
	}
}
