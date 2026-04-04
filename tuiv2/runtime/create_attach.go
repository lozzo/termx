package runtime

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/tuiv2/shared"
)

func (r *Runtime) AttachTerminal(ctx context.Context, paneID, terminalID, mode string) (*TerminalRuntime, error) {
	if r == nil || r.client == nil {
		return nil, shared.UserVisibleError{Op: "attach terminal", Err: fmt.Errorf("runtime client is nil")}
	}
	attached, err := r.client.Attach(ctx, terminalID, mode)
	if err != nil {
		return nil, shared.UserVisibleError{Op: "attach terminal", Err: err}
	}
	terminal := r.registry.GetOrCreate(terminalID)
	if terminal == nil {
		return nil, shared.UserVisibleError{Op: "attach terminal", Err: fmt.Errorf("terminal registry unavailable")}
	}
	terminal.Channel = attached.Channel
	terminal.AttachMode = attached.Mode
	r.ensureVTerm(terminal)
	r.unbindPaneFromTerminalCache(paneID, "")
	binding := r.BindPane(paneID)
	if binding != nil {
		binding.Channel = attached.Channel
		binding.Connected = true
		terminal.BoundPaneIDs = appendBoundPaneID(terminal.BoundPaneIDs, paneID)
	}
	r.syncTerminalOwnership(terminal)
	r.touch()
	if err := r.StartStream(ctx, terminalID); err != nil {
		return nil, err
	}
	return terminal, nil
}

func appendBoundPaneID(ids []string, paneID string) []string {
	for _, existing := range ids {
		if existing == paneID {
			return ids
		}
	}
	return append(ids, paneID)
}

func removeBoundPaneID(ids []string, paneID string) []string {
	if len(ids) == 0 || paneID == "" {
		return ids
	}
	kept := ids[:0]
	for _, existing := range ids {
		if existing == paneID {
			continue
		}
		kept = append(kept, existing)
	}
	return kept
}
