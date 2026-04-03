package runtime

import "github.com/lozzow/termx/protocol"

type VisibleRuntime struct {
	Terminals []VisibleTerminal
	Bindings  []VisiblePaneBinding
}

type VisibleTerminal struct {
	TerminalID   string
	Name         string
	State        string
	ExitCode     *int
	Title        string
	AttachMode   string
	OwnerPaneID  string
	BoundPaneIDs []string
	Snapshot     *protocol.Snapshot
}

type VisiblePaneBinding struct {
	PaneID    string
	Role      string
	Connected bool
}
