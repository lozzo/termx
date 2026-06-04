package state

// Root 是 reducer-owned TUI-v3 state root。
type Root struct {
	Generation   uint64
	History      HistoryStore
	CopyMode     CopyModeStore
	Surface      TerminalSurfaceStore
	Session      TerminalSessionStore
	TerminalPool TerminalPoolStore
	Viewport     ViewportStore
	Shell        ShellStore
}

// Advance 返回 generation 递增后的副本。
func (r Root) Advance() Root {
	r.Generation++
	return r
}
