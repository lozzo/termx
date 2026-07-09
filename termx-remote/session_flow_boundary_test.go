package remote_test

import (
	"os"
	"strings"
	"testing"
)

func TestWF004ManagedUsesSharedHubSessionFlow(t *testing.T) {
	requireNoGoPackageDir(t, "hub/sessionflow")
	requireSourceContains(t, "agent/runtime/hub_stream.go", "answerer.AnswerOffer")
	requireSourceNotContains(t, "service.go", []string{"sessionflow.AnswerLocal", "LocalPlan("})
}

func TestServiceManagerDoesNotRetainServiceBackReference(t *testing.T) {
	requireSourceContains(t, "service.go", "daemonRuntimeAdapter{daemon: daemon}")
	requireSourceContains(t, "service.go", "pairClaimer{store: s.pairing}")
	requireSourceNotContains(t, "service.go", []string{
		"inventoryProvider{service:",
		"terminalManagementRouter{service:",
		"pairClaimer{service:",
		"type inventoryProvider struct",
		"type localRTCTransportSink struct",
	})
}

func requireNoGoPackageDir(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".go") {
			t.Fatalf("%s still contains Go source %s", path, entry.Name())
		}
	}
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
