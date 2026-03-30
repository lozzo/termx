package runtime

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/tuiv2/shared"
)

func (r *Runtime) SendInput(ctx context.Context, paneID string, data []byte) error {
	binding := r.Binding(paneID)
	if r == nil || r.client == nil {
		return shared.UserVisibleError{Op: "send terminal input", Err: fmt.Errorf("runtime client is nil")}
	}
	if binding == nil || binding.Channel == 0 {
		return shared.UserVisibleError{Op: "send terminal input", Err: fmt.Errorf("pane %s is not attached", paneID)}
	}
	if err := r.client.Input(ctx, binding.Channel, data); err != nil {
		return shared.UserVisibleError{Op: "send terminal input", Err: err}
	}
	return nil
}
