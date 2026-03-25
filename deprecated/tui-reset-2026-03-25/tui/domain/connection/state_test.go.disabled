package connection

import (
	"testing"

	"github.com/lozzow/termx/tui/domain/types"
)

func TestStateConnectFirstPaneBecomesOwner(t *testing.T) {
	state := NewState(types.TerminalID("term-1"))

	state.Connect(types.PaneID("pane-1"))

	if got := state.Owner(); got != types.PaneID("pane-1") {
		t.Fatalf("expected first connected pane to become owner, got %q", got)
	}
}

func TestStateConnectSecondPaneDefaultsToFollower(t *testing.T) {
	state := NewState(types.TerminalID("term-1"))
	state.Connect(types.PaneID("pane-1"))
	state.Connect(types.PaneID("pane-2"))

	if !state.HasControl(types.PaneID("pane-1")) {
		t.Fatalf("expected first pane to keep owner control")
	}
	if state.HasControl(types.PaneID("pane-2")) {
		t.Fatalf("expected second pane to default to follower")
	}
}

func TestStateDisconnectMigratesOwner(t *testing.T) {
	state := NewState(types.TerminalID("term-1"))
	state.Connect(types.PaneID("pane-1"))
	state.Connect(types.PaneID("pane-2"))

	state.Disconnect(types.PaneID("pane-1"))

	if got := state.Owner(); got != types.PaneID("pane-2") {
		t.Fatalf("expected owner to migrate to remaining pane, got %q", got)
	}
}

func TestStateAcquireTransfersOwner(t *testing.T) {
	state := NewState(types.TerminalID("term-1"))
	state.Connect(types.PaneID("pane-1"))
	state.Connect(types.PaneID("pane-2"))

	if ok := state.Acquire(types.PaneID("pane-2")); !ok {
		t.Fatalf("expected acquire to succeed")
	}
	if got := state.Owner(); got != types.PaneID("pane-2") {
		t.Fatalf("expected pane-2 to become owner, got %q", got)
	}
}
