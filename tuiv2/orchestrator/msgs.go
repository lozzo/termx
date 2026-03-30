package orchestrator

import "github.com/lozzow/termx/protocol"

type TerminalAttachedMsg struct {
	PaneID     string
	TerminalID string
	Channel    uint16
}

type SnapshotLoadedMsg struct {
	TerminalID string
	Snapshot   *protocol.Snapshot
}
