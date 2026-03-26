package app

import (
	"github.com/lozzow/termx/protocol"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
)

type Message interface{}

type MessageTerminalDisconnected struct {
	PaneID types.PaneID
}

type MessageTerminalConnected struct {
	Terminal coreterminal.Metadata
	Snapshot *protocol.Snapshot
}

type MessageTerminalExited struct {
	TerminalID types.TerminalID
}

type MessageTerminalRemoved struct {
	TerminalID types.TerminalID
}

type MessageTerminalPoolLoaded struct {
	Terminals []coreterminal.Metadata
}
