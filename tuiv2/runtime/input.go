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
	if terminal := r.terminalBoundToPane(paneID); terminal != nil {
		terminal.ResizePreviewSource = nil
		terminal.PreferSnapshot = false
		r.invalidate()
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
}

// NoteLocalInteraction marks the current moment as a user-driven UI interaction
// (e.g. mouse drag). This makes the cursor writer's interactive-bypass fire so
// drag frames reach the host without waiting for the batch timer, which matters
// over SSH where the adaptive batch delay can grow to 50 ms.
func (r *Runtime) NoteLocalInteraction() {
	if r == nil {
		return
	}
	r.recentInteractionAt.Store(time.Now().UnixNano())
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

func (r *Runtime) RecentLocalInteraction() bool {
	if r == nil {
		return false
	}
	at := r.recentInteractionAt.Load()
	if at == 0 {
		return false
	}
	return time.Since(time.Unix(0, at)) <= effectiveInteractiveLatencyWindow()
}

func (r *Runtime) RecentLocalActivity() bool {
	return r != nil && (r.RecentLocalInput() || r.RecentLocalInteraction())
}

func (r *Runtime) consumeInteractiveBypass() bool {
	return r != nil && r.RecentLocalInput()
}

func effectiveInteractiveLatencyWindow() time.Duration {
	window := interactiveLatencyWindow
	if shared.RemoteLatencyProfileEnabled() && (window <= 0 || window < remoteInteractiveLatencyWindow) {
		window = remoteInteractiveLatencyWindow
	}
	return shared.DurationOverride("TERMX_INTERACTIVE_LATENCY_WINDOW", window)
}
