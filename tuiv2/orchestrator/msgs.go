package orchestrator

import "github.com/lozzow/termx/internal/protocol"

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
	Offset     int
	Limit      int
	Paged      bool
	// CopyModeRequest marks paged history loaded on behalf of a copy-mode
	// frozen snapshot instead of the shared live runtime snapshot.
	CopyModeRequest bool
}
