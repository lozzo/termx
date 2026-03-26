package app

type Intent string

const (
	IntentOpenTerminalPool   Intent = "open-terminal-pool"
	IntentCloseScreen        Intent = "close-screen"
	IntentOpenConnectOverlay Intent = "open-connect-overlay"
)
