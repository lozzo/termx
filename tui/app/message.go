package app

import (
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
)

type Message interface{}

type MessageTerminalDisconnected struct {
	PaneID types.PaneID
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
