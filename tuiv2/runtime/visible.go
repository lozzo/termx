package runtime

import (
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
)

type VisibleRuntime struct {
	Terminals         []VisibleTerminal
	Bindings          []VisiblePaneBinding
	HostDefaultFG     string
	HostDefaultBG     string
	HostPalette       map[int]string
	HostEmojiVS16Mode shared.AmbiguousEmojiVariationSelectorMode
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
