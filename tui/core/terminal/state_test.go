package terminal

import (
	"testing"

	"github.com/lozzow/termx/tui/core/types"
)

func TestMetadataConnectionRole(t *testing.T) {
	meta := Metadata{
		ID:              types.TerminalID("term-1"),
		OwnerPaneID:     types.PaneID("pane-1"),
		AttachedPaneIDs: []types.PaneID{types.PaneID("pane-1"), types.PaneID("pane-2")},
	}

	if got := meta.ConnectionRole(types.PaneID("pane-1")); got != types.ConnectionRoleOwner {
		t.Fatalf("expected owner role, got %q", got)
	}
	if got := meta.ConnectionRole(types.PaneID("pane-2")); got != types.ConnectionRoleFollower {
		t.Fatalf("expected follower role, got %q", got)
	}
	if got := meta.ConnectionRole(types.PaneID("pane-3")); got != "" {
		t.Fatalf("expected empty role for unrelated pane, got %q", got)
	}
}
