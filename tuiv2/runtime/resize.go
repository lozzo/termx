package runtime

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/terminalmeta"
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
	appendSharedTerminalTrace("runtime.resize.request", terminal, "pane=%s channel=%d cols=%d rows=%d", paneID, binding.Channel, cols, rows)
	if terminal != nil {
		if terminalmeta.SizeLocked(terminal.Tags) {
			appendSharedTerminalTrace("runtime.resize.skip", terminal, "pane=%s reason=size_locked cols=%d rows=%d", paneID, cols, rows)
			return nil
		}
		ownerPaneID := r.syncTerminalOwnership(terminal)
		if ownerPaneID == "" {
			if terminal.RequiresExplicitOwner {
				appendSharedTerminalTrace("runtime.resize.skip", terminal, "pane=%s reason=explicit_owner_required cols=%d rows=%d", paneID, cols, rows)
				return nil
			}
		} else if ownerPaneID != paneID {
			appendSharedTerminalTrace("runtime.resize.skip", terminal, "pane=%s reason=not_owner owner=%s cols=%d rows=%d", paneID, ownerPaneID, cols, rows)
			return nil
		}
		forceResize := terminal.PendingOwnerResize
		if !forceResize && terminalAlreadySized(terminal, cols, rows) {
			appendSharedTerminalTrace("runtime.resize.skip", terminal, "pane=%s reason=already_sized cols=%d rows=%d", paneID, cols, rows)
			return nil
		}
	}
	if err := r.client.Resize(ctx, binding.Channel, cols, rows); err != nil {
		return shared.UserVisibleError{Op: "resize terminal", Err: err}
	}
	appendSharedTerminalTrace("runtime.resize.sent", terminal, "pane=%s channel=%d cols=%d rows=%d", paneID, binding.Channel, cols, rows)
	if terminal != nil {
		terminal.PendingOwnerResize = false
		if vt := r.ensureVTerm(terminal); vt != nil {
			vt.Resize(int(cols), int(rows))
		}
		if terminal.BootstrapPending {
			// Keep the local surface snapshot geometry in sync even before the
			// first bootstrap replay completes, otherwise owner handoff resizes
			// can stay stuck on the previous tab's size until the stream emits.
			r.publishSurface(terminal)
			r.refreshSnapshot(terminalID)
			return nil
		}
		r.publishSurface(terminal)
		r.refreshSnapshot(terminalID)
		appendSharedTerminalTrace("runtime.resize.applied", terminal, "pane=%s cols=%d rows=%d", paneID, cols, rows)
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
