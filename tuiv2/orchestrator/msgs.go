package orchestrator

import "github.com/lozzow/termx/termx-core/protocol"

type TerminalAttachedMsg struct {
	TabID      string // optional: specific tab that owns the pane; empty = use current tab
	PaneID     string
	TerminalID string
	Channel    uint16
}

type SnapshotLoadedMsg struct {
	PaneID     string
	TerminalID string
	Snapshot   *protocol.Snapshot
}
