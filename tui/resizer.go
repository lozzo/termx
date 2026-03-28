package tui

import "context"

type Resizer struct {
	coordinator *TerminalCoordinator
}

func NewResizer(coordinator *TerminalCoordinator) *Resizer {
	return &Resizer{coordinator: coordinator}
}

func (r *Resizer) SyncPaneResize(pane *Pane, cols, rows int) {
	if r == nil || r.coordinator == nil || pane == nil || pane.Viewport == nil {
		return
	}
	if !pane.ResizeAcquired || pane.Channel == 0 || cols <= 0 || rows <= 0 {
		return
	}
	ctx := context.Background()
	_ = r.coordinator.client.Resize(ctx, pane.Channel, uint16(cols), uint16(rows))
}
