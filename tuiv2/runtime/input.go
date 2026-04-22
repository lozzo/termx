package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/tuiv2/shared"
)

func (r *Runtime) SendInput(ctx context.Context, paneID string, data []byte) error {
	finish := perftrace.Measure("runtime.input.send")
	defer func() {
		finish(len(data))
	}()
	binding := r.Binding(paneID)
	if r == nil || r.client == nil {
		return shared.UserVisibleError{Op: "send terminal input", Err: fmt.Errorf("runtime client is nil")}
	}
	if binding == nil || binding.Channel == 0 {
		return shared.UserVisibleError{Op: "send terminal input", Err: fmt.Errorf("pane %s is not attached", paneID)}
	}
	r.noteLocalInput()
	if err := r.client.Input(ctx, binding.Channel, data); err != nil {
		return shared.UserVisibleError{Op: "send terminal input", Err: err}
	}
	return nil
}

func (r *Runtime) noteLocalInput() {
	if r == nil {
		return
	}
	r.recentInputAt.Store(time.Now().UnixNano())
	r.inputBypassArmed.Store(true)
}

func (r *Runtime) RecentLocalInput() bool {
	if r == nil {
		return false
	}
	at := r.recentInputAt.Load()
	if at == 0 {
		return false
	}
	return time.Since(time.Unix(0, at)) <= effectiveInteractiveLatencyWindow()
}

func (r *Runtime) consumeInteractiveBypass() bool {
	if r == nil || !r.RecentLocalInput() {
		return false
	}
	return r.inputBypassArmed.Swap(false)
}

func effectiveInteractiveLatencyWindow() time.Duration {
	window := interactiveLatencyWindow
	if shared.RemoteLatencyProfileEnabled() && (window <= 0 || window < remoteInteractiveLatencyWindow) {
		window = remoteInteractiveLatencyWindow
	}
	return shared.DurationOverride("TERMX_INTERACTIVE_LATENCY_WINDOW", window)
}
