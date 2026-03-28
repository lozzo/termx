package tui

import "context"

type Resizer struct {
	coordinator *TerminalCoordinator
}

func NewResizer(coordinator *TerminalCoordinator) *Resizer {
	return &Resizer{coordinator: coordinator}
}

func (r *Resizer) SyncPaneResize(pane *Pane, cols, rows int) {
	if r == nil || r.coordinator == nil {
		return
	}
	r.coordinator.ResizeTerminal(context.Background(), pane, cols, rows)
}
