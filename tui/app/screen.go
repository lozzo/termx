package app

type Screen string

const (
	ScreenWorkbench    Screen = "workbench"
	ScreenTerminalPool Screen = "terminal-pool"
)

type FocusTarget string

const (
	FocusWorkbench    FocusTarget = "workbench"
	FocusTerminalPool FocusTarget = "terminal-pool"
)

func normalizeScreen(screen Screen) Screen {
	switch screen {
	case ScreenTerminalPool:
		return ScreenTerminalPool
	case ScreenWorkbench:
		fallthrough
	default:
		return ScreenWorkbench
	}
}

func defaultFocusTarget(screen Screen) FocusTarget {
	switch normalizeScreen(screen) {
	case ScreenTerminalPool:
		return FocusTerminalPool
	default:
		return FocusWorkbench
	}
}
