package runtime

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/tuiv2/shared"
)

func (r *Runtime) ResizeTerminal(ctx context.Context, paneID, terminalID string, cols, rows uint16) error {
	return r.ResizePane(ctx, paneID, terminalID, cols, rows)
}

func (r *Runtime) ResizePane(ctx context.Context, paneID, terminalID string, cols, rows uint16) error {
	if r == nil || r.client == nil {
		return shared.UserVisibleError{Op: "resize terminal", Err: fmt.Errorf("runtime client is nil")}
	}
	binding := r.Binding(paneID)
	if binding == nil || binding.Channel == 0 {
		return shared.UserVisibleError{Op: "resize terminal", Err: fmt.Errorf("pane %s is not attached", paneID)}
	}
	terminal := r.registry.Get(terminalID)
	if terminal != nil {
		ownerPaneID := r.syncTerminalOwnership(terminal)
		if ownerPaneID == "" {
			if terminal.RequiresExplicitOwner {
				return nil
			}
		} else if ownerPaneID != paneID {
			return nil
		}
		if terminalAlreadySized(terminal, cols, rows) {
			return nil
		}
	}
	if err := r.client.Resize(ctx, binding.Channel, cols, rows); err != nil {
		return shared.UserVisibleError{Op: "resize terminal", Err: err}
	}
	if terminal != nil {
		if vt := r.ensureVTerm(terminal); vt != nil {
			vt.Resize(int(cols), int(rows))
		}
		r.refreshSnapshot(terminalID)
	}
	return nil
}

func terminalAlreadySized(terminal *TerminalRuntime, cols, rows uint16) bool {
	if terminal == nil || cols == 0 || rows == 0 {
		return false
	}
	if terminal.Snapshot != nil && terminal.Snapshot.Size.Cols == cols && terminal.Snapshot.Size.Rows == rows {
		return true
	}
	if terminal.VTerm == nil {
		return false
	}
	currentCols, currentRows := terminal.VTerm.Size()
	return currentCols == int(cols) && currentRows == int(rows)
}
