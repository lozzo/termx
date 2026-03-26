package pool

import (
	"testing"

	"github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
)

func TestBuildGroupsSplitsVisibleParkedAndExited(t *testing.T) {
	terminals := map[types.TerminalID]terminal.Metadata{
		types.TerminalID("term-visible"): {
			ID:    types.TerminalID("term-visible"),
			Name:  "shell-a",
			State: terminal.StateRunning,
		},
		types.TerminalID("term-parked"): {
			ID:    types.TerminalID("term-parked"),
			Name:  "shell-b",
			State: terminal.StateRunning,
		},
		types.TerminalID("term-exited"): {
			ID:    types.TerminalID("term-exited"),
			Name:  "shell-c",
			State: terminal.StateExited,
		},
	}

	groups := BuildGroups(terminals, map[types.TerminalID]bool{
		types.TerminalID("term-visible"): true,
	}, "")

	if len(groups.Visible) != 1 || groups.Visible[0].ID != types.TerminalID("term-visible") {
		t.Fatalf("expected visible group to contain term-visible, got %#v", groups.Visible)
	}
	if len(groups.Parked) != 1 || groups.Parked[0].ID != types.TerminalID("term-parked") {
		t.Fatalf("expected parked group to contain term-parked, got %#v", groups.Parked)
	}
	if len(groups.Exited) != 1 || groups.Exited[0].ID != types.TerminalID("term-exited") {
		t.Fatalf("expected exited group to contain term-exited, got %#v", groups.Exited)
	}
}

func TestBuildGroupsAppliesSearchFilter(t *testing.T) {
	terminals := map[types.TerminalID]terminal.Metadata{
		types.TerminalID("term-a"): {
			ID:    types.TerminalID("term-a"),
			Name:  "backend-shell",
			State: terminal.StateRunning,
			Tags:  map[string]string{"project": "api"},
		},
		types.TerminalID("term-b"): {
			ID:    types.TerminalID("term-b"),
			Name:  "frontend-shell",
			State: terminal.StateRunning,
		},
	}

	groups := BuildGroups(terminals, nil, "api")
	if len(groups.Parked) != 1 || groups.Parked[0].ID != types.TerminalID("term-a") {
		t.Fatalf("expected query to match term-a only, got %#v", groups.Parked)
	}
}
