package app

import "github.com/lozzow/termx/tui/core/types"

type Effect interface{}

type EffectCreateTerminal struct{}

type EffectLoadTerminalPool struct{}

type EffectConnectTerminal struct {
	TerminalID types.TerminalID
}

type EffectDisconnectPane struct {
	PaneID types.PaneID
}

type EffectReconnectTerminal struct {
	TerminalID types.TerminalID
}

type EffectKillTerminal struct {
	TerminalID types.TerminalID
}

type EffectRemoveTerminal struct {
	TerminalID types.TerminalID
}
