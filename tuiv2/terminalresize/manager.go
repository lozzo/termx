package terminalresize

import (
	"context"

	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/terminalcontrol"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type ViewportFunc func(paneID string, rect workbench.Rect) (workbench.Rect, bool)

type Target struct {
	PaneID               string
	TerminalID           string
	Rect                 workbench.Rect
	ExplicitTakeover     bool
	ImplicitSessionLease bool
}

type Manager struct {
	runtime  *runtime.Runtime
	control  *terminalcontrol.Manager
	viewport ViewportFunc
}

func NewManager(rt *runtime.Runtime, control *terminalcontrol.Manager, viewport ViewportFunc) *Manager {
	return &Manager{runtime: rt, control: control, viewport: viewport}
}

func (m *Manager) ResizeVisible(ctx context.Context, targets []Target) error {
	if m == nil {
		return nil
	}
	for _, target := range targets {
		if err := m.EnsureSized(ctx, target); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) EnsureSized(ctx context.Context, target Target) error {
	if m == nil || m.control == nil || target.PaneID == "" || target.TerminalID == "" {
		return nil
	}
	viewportRect, ok := m.viewportRect(target)
	if !ok {
		return nil
	}
	return m.control.Sync(ctx, terminalcontrol.SyncRequest{
		PaneID:               target.PaneID,
		TerminalID:           target.TerminalID,
		TargetCols:           uint16(maxInt(2, viewportRect.W)),
		TargetRows:           uint16(maxInt(2, viewportRect.H)),
		ResizeIfNeeded:       true,
		ExplicitTakeover:     target.ExplicitTakeover,
		ImplicitSessionLease: target.ImplicitSessionLease,
	})
}

func (m *Manager) PendingSatisfied(target Target) bool {
	if m == nil || m.runtime == nil || target.TerminalID == "" {
		return false
	}
	viewportRect, ok := m.viewportRect(target)
	if !ok {
		return false
	}
	terminal := m.runtime.Registry().Get(target.TerminalID)
	if terminal == nil || terminal.Snapshot == nil {
		return false
	}
	cols := uint16(maxInt(2, viewportRect.W))
	rows := uint16(maxInt(2, viewportRect.H))
	return terminal.Snapshot.Size.Cols == cols && terminal.Snapshot.Size.Rows == rows
}

func (m *Manager) viewportRect(target Target) (workbench.Rect, bool) {
	if m == nil || m.viewport == nil {
		return workbench.Rect{}, false
	}
	return m.viewport(target.PaneID, target.Rect)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
