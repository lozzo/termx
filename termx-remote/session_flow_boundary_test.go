package remote_test

import (
	"os"
	"strings"
	"testing"
)

func TestWF004ManagedUsesSharedHubSessionFlow(t *testing.T) {
	requireGoPackageDir(t, "hub/sessionflow")
	requireSourceContains(t, "agent/runtime/manager.go", "sessionflow.AnswerManaged")
	requireSourceNotContains(t, "service.go", []string{"sessionflow.AnswerLocal", "LocalPlan("})
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
