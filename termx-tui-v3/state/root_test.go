package state

import "testing"

func TestRootAdvance(t *testing.T) {
	next := (Root{}).Advance()
	if next.Generation != 1 {
		t.Fatalf("expected generation 1, got %d", next.Generation)
	}
}

func TestRootContainsReducerOwnedShellStore(t *testing.T) {
	root := Root{Shell: DefaultShell()}
	if root.Shell.ActivePaneID != DefaultPaneID {
		t.Fatalf("expected root shell active pane, got %#v", root.Shell)
	}
}
