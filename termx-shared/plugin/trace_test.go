package plugin

import (
	"errors"
	"strings"
	"testing"
)

func TestTraceManagerRejectsForgedParent(t *testing.T) {
	manager := newTestTraceManager(t, 8)
	trace, parent, err := manager.NewRootTraceWithID("trace-1", "plugin-a")
	if err != nil {
		t.Fatalf("new root trace: %v", err)
	}
	if trace.TraceID != "trace-1" || !trace.ContainsActor("plugin-a") {
		t.Fatalf("unexpected root trace %#v", trace)
	}

	next, nextParent, err := manager.DeriveActionTrace(parent, "plugin-b", "termx.client.panel.created")
	if err != nil {
		t.Fatalf("derive action trace: %v", err)
	}
	if next.Depth != 1 || next.LastPluginID != "plugin-b" || !next.ContainsActor("plugin-b") || nextParent.TraceID != "trace-1" {
		t.Fatalf("unexpected derived trace %#v parent=%#v", next, nextParent)
	}

	forged := parent
	forged.Token += "x"
	if _, err := manager.TraceFromParent(forged); !errors.Is(err, ErrInvalidTraceToken) {
		t.Fatalf("forged token must be rejected, got %v", err)
	}

	wrongTrace := parent
	wrongTrace.TraceID = "other"
	if _, err := manager.TraceFromParent(wrongTrace); !errors.Is(err, ErrInvalidTraceToken) {
		t.Fatalf("wrong trace id must be rejected, got %v", err)
	}
}

func TestTraceManagerEnforcesDepth(t *testing.T) {
	manager := newTestTraceManager(t, 1)
	_, parent, err := manager.NewRootTraceWithID("trace-depth", "")
	if err != nil {
		t.Fatalf("new root trace: %v", err)
	}
	_, parent, err = manager.DeriveActionTrace(parent, "plugin-a", "first")
	if err != nil {
		t.Fatalf("first derive should pass: %v", err)
	}
	if _, _, err := manager.DeriveActionTrace(parent, "plugin-b", "second"); !errors.Is(err, ErrTraceDepthExceeded) {
		t.Fatalf("second derive should exceed depth, got %v", err)
	}
}

func TestTraceParentTokenIsOpaque(t *testing.T) {
	manager := newTestTraceManager(t, 8)
	_, parent, err := manager.NewRootTraceWithID("trace-opaque", "plugin-a")
	if err != nil {
		t.Fatalf("new root trace: %v", err)
	}
	if strings.Contains(parent.Token, "plugin-a") || strings.Contains(parent.Token, "trace-opaque") {
		t.Fatalf("trace token should be opaque, got %q", parent.Token)
	}
}

func newTestTraceManager(t *testing.T, maxDepth int) *TraceManager {
	t.Helper()
	manager, err := NewTraceManager(TraceManagerConfig{
		SigningKey: []byte("test signing key"),
		MaxDepth:   maxDepth,
	})
	if err != nil {
		t.Fatalf("new trace manager: %v", err)
	}
	return manager
}
