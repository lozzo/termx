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
	r.resetTerminalLiveState(terminal)
	terminal.Channel = attached.Channel
	terminal.AttachMode = attached.Mode
	r.hydrateTerminalMetadata(ctx, terminalID)
	r.ensureVTerm(terminal)
	// If the VTerm was seeded from a preserved snapshot, bump SurfaceVersion
	// so the surface is immediately visible for the first render frame.
	// This closes the timing gap between attach and bootstrap replay arrival.
	if terminal.VTerm != nil && terminal.Snapshot != nil {
		r.bumpSurfaceVersion(terminal)
	}
	r.unbindPaneFromTerminalCache(paneID, "")
	binding := r.BindPane(paneID)
	if binding != nil {
		binding.Channel = attached.Channel
		binding.Connected = true
		terminal.BoundPaneIDs = appendBoundPaneID(terminal.BoundPaneIDs, paneID)
		if terminal.OwnerPaneID == "" {
			terminal.OwnerPaneID = paneID
			terminal.ControlPaneID = paneID
			terminal.RequiresExplicitOwner = false
		}
	}
	r.syncTerminalOwnership(terminal)
	r.touch()
	return terminal, nil
}

func (r *Runtime) resetTerminalLiveState(terminal *TerminalRuntime) {
	if r == nil || terminal == nil {
		return
	}
	if terminal.Stream.Stop != nil {
		terminal.Stream.Stop()
	}
	terminal.Stream = StreamState{}
	terminal.Recovery = RecoveryState{}
	// Preserve terminal.Snapshot as a transitional render source: the old
	// snapshot provides content for the first render frame while the bootstrap
	// replay from the stream is still in-flight.  ensureVTerm will seed the
	// new VTerm from this snapshot so the surface is immediately visible.
	// The snapshot (and surface) will be superseded once the first stream
	// frames arrive and bumpSurfaceVersion / refreshSnapshot run.
	terminal.SnapshotVersion = 0
	terminal.SurfaceVersion = 0
	terminal.BootstrapPending = false
	terminal.ScrollbackLoadedLimit = 0
	terminal.ScrollbackLoadingLimit = 0
	terminal.ScrollbackExhausted = false
	terminal.VTerm = nil
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
