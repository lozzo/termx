package pool

import (
	"testing"
	"time"

	"github.com/lozzow/termx/tui/state/terminal"
	"github.com/lozzow/termx/tui/state/types"
)

func TestBuildConnectItemsUsesGlobalTerminalScopeAndRecentUserInteractionSort(t *testing.T) {
	now := time.Unix(200, 0)
	items := BuildConnectItems(map[types.TerminalID]terminal.Metadata{
		types.TerminalID("term-1"): {
			ID:              types.TerminalID("term-1"),
			Name:            "api-dev",
			State:           terminal.StateRunning,
			OwnerPaneID:     types.PaneID("pane-1"),
			LastOutputAt:    now.Add(10 * time.Minute),
			LastInteraction: now.Add(-2 * time.Minute),
		},
		types.TerminalID("term-2"): {
			ID:              types.TerminalID("term-2"),
			Name:            "recent-input-terminal",
			State:           terminal.StateRunning,
			LastInteraction: now.Add(-1 * time.Minute),
		},
		types.TerminalID("term-3"): {
			ID:              types.TerminalID("term-3"),
			Name:            "old-api",
			State:           terminal.StateExited,
			LastInteraction: now.Add(-10 * time.Minute),
		},
	})

	if len(items) != 3 {
		t.Fatalf("expected all terminals in picker scope, got %d", len(items))
	}
	if items[0].TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected recent interaction first, got %q", items[0].TerminalID)
	}
	if items[1].TerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected api-dev second, got %q", items[1].TerminalID)
	}
	if items[1].OwnerSummary != "owner elsewhere" {
		t.Fatalf("expected owner summary, got %q", items[1].OwnerSummary)
	}
	if items[2].StateSummary != "exited" {
		t.Fatalf("expected exited summary, got %q", items[2].StateSummary)
	}
}
