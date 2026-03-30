package runtime

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/tuiv2/shared"
)

func (r *Runtime) ResizeTerminal(ctx context.Context, paneID string, cols, rows uint16) error {
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
	return nil
}
