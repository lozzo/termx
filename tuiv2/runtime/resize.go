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
	binding := r.Binding(paneID)
	if r == nil || r.client == nil {
		return shared.UserVisibleError{Op: "resize terminal", Err: fmt.Errorf("runtime client is nil")}
	}
	if binding == nil || binding.Channel == 0 {
		return shared.UserVisibleError{Op: "resize terminal", Err: fmt.Errorf("pane %s is not attached", paneID)}
	}
	if err := r.client.Resize(ctx, binding.Channel, cols, rows); err != nil {
		return shared.UserVisibleError{Op: "resize terminal", Err: err}
	}
	if terminal := r.registry.Get(terminalID); terminal != nil {
		if vt := r.ensureVTerm(terminal); vt != nil {
			vt.Resize(int(cols), int(rows))
		}
		r.refreshSnapshot(terminalID)
	}
	return nil
}
