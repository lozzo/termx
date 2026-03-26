package app

import "github.com/lozzow/termx/tui/core/types"

type Intent any

type SimpleIntent string

const (
	IntentOpenTerminalPool     SimpleIntent = "open-terminal-pool"
	IntentCloseScreen          SimpleIntent = "close-screen"
	IntentOpenConnectOverlay   SimpleIntent = "open-connect-overlay"
	IntentOpenHelpOverlay      SimpleIntent = "open-help-overlay"
	IntentDisconnectActivePane SimpleIntent = "disconnect-active-pane"
	IntentReconnectActivePane  SimpleIntent = "reconnect-active-pane"
	IntentPoolSelectNext       SimpleIntent = "pool-select-next"
	IntentPoolSelectPrev       SimpleIntent = "pool-select-prev"
)

type IntentConnectTerminal struct {
	TerminalID types.TerminalID
}

type IntentKillSelectedTerminal struct {
	TerminalID types.TerminalID
}

type IntentRemoveSelectedTerminal struct {
	TerminalID types.TerminalID
}
