package runtime

import (
	"context"
	"fmt"
	"slices"

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
	r.hydrateTerminalMetadata(ctx, terminalID)
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

func (r *Runtime) hydrateTerminalMetadata(ctx context.Context, terminalID string) {
	if r == nil || r.client == nil || r.registry == nil || terminalID == "" {
		return
	}
	result, err := r.client.List(ctx)
	if err != nil || result == nil {
		return
	}
	terminal := r.registry.GetOrCreate(terminalID)
	if terminal == nil {
		return
	}
	for _, info := range result.Terminals {
		if info.ID != terminalID {
			continue
		}
		terminal.Name = info.Name
		terminal.Command = slices.Clone(info.Command)
		terminal.Tags = cloneTags(info.Tags)
		terminal.State = info.State
		terminal.ExitCode = cloneExitCode(info.ExitCode)
		r.touch()
		return
	}
}

func cloneExitCode(code *int) *int {
	if code == nil {
		return nil
	}
	value := *code
	return &value
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
