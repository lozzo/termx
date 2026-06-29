package perftrace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEnableFromEnvWritesSnapshot(t *testing.T) {
	Disable()
	path := filepath.Join(t.TempDir(), "perf", "trace.json")
	t.Setenv(envPath, path)
	t.Setenv(envIntervalMs, "10000")
	stop, gotPath, ok := EnableFromEnv(context.Background())
	if !ok || gotPath != path {
		t.Fatalf("expected perftrace enabled at %q, got enabled=%v path=%q", path, ok, gotPath)
	}
	finish := Measure("unit.measure")
	finish(42)
	Count("unit.count", 7)
	stop()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty perftrace snapshot")
	}
	if Current() != nil {
		t.Fatal("stop should disable active recorder")
	}
}
