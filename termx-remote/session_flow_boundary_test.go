package remote_test

import (
	"os"
	"strings"
	"testing"
)

func TestWF004LocalAndManagedUseSharedHubSessionFlow(t *testing.T) {
	requireGoPackageDir(t, "hub/sessionflow")
	for _, path := range []string{
		"service.go",
		"agent/runtime/manager.go",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(data), "hub/sessionflow") {
			t.Fatalf("%s does not use shared hub/sessionflow boundary", path)
		}
	}
	requireSourceContains(t, "service.go", "sessionflow.AnswerLocal")
	requireSourceContains(t, "agent/runtime/manager.go", "sessionflow.AnswerManaged")
}

func requireSourceContains(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("%s does not contain %q", path, want)
	}
}
