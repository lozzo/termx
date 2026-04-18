package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
)

const (
	synchronizedOutputBegin           = "\x1b[?2026h"
	synchronizedOutputEnd             = "\x1b[?2026l"
	clientOutputBatchDelay            = 2 * time.Millisecond
	interactiveOutputBatchDelay       = 500 * time.Microsecond
	remoteClientOutputBatchDelay      = 1 * time.Millisecond
	remoteInteractiveOutputBatchDelay = 250 * time.Microsecond
	// If a synchronized-output group is still open when the normal batch timer
	// fires, wait in small increments for the trailing sync-end chunk so we can
	// keep the whole redraw in one local VTerm.Write()/render pass.
	synchronizedOutputWaitStep       = 500 * time.Microsecond
	remoteSynchronizedOutputWaitStep = 250 * time.Microsecond
	// Cap the extra wait so an incomplete or delayed sync group cannot stall the
	// first visible frame indefinitely. This is a pragmatic latency ceiling, not
	// a protocol guarantee.
	synchronizedOutputWaitBudget       = 5 * time.Millisecond
	remoteSynchronizedOutputWaitBudget = 1500 * time.Microsecond
)

type screenUpdateApplier interface {
	ApplyScreenUpdate(update protocol.ScreenUpdate) bool
}

func visibleScreenUpdateSummary(update protocol.ScreenUpdate) VisibleScreenUpdateSummary {
	summary := VisibleScreenUpdateSummary{
		FullReplace:  update.FullReplace,
		ScreenScroll: update.ScreenScroll,
	}
	if len(update.ChangedRows) == 0 {
		return summary
	}
	summary.ChangedRows = make([]int, 0, len(update.ChangedRows))
	seen := make(map[int]struct{}, len(update.ChangedRows))
	for _, row := range update.ChangedRows {
		if _, ok := seen[row.Row]; ok {
			continue
		}
		seen[row.Row] = struct{}{}
		summary.ChangedRows = append(summary.ChangedRows, row.Row)
	}
	return summary
}

func (r *Runtime) StartStream(ctx context.Context, terminalID string) error {
	if r == nil || r.client == nil {
		return shared.UserVisibleError{Op: "start terminal stream", Err: fmt.Errorf("runtime client is nil")}
	}
	terminal := r.registry.GetOrCreate(terminalID)
	if terminal == nil {
		return shared.UserVisibleError{Op: "start terminal stream", Err: fmt.Errorf("terminal registry unavailable")}
	}
	if terminal.Channel == 0 {
		return shared.UserVisibleError{Op: "start terminal stream", Err: fmt.Errorf("terminal %s is not attached", terminalID)}
	}
	r.ensureVTerm(terminal)
	if terminal.Stream.Active {
		return nil
	}
	terminal.Stream.Generation++
	generation := terminal.Stream.Generation
	terminal.BootstrapPending = true
	stream, stop := r.client.Stream(terminal.Channel)
	terminal.Stream.Active = true
	terminal.Stream.Stop = stop
	go func(generation uint64) {
		defer func() {
			if terminal.Stream.Generation != generation {
				return
			}
			if stop != nil {
				stop()
			}
			terminal.Stream.Active = false
			terminal.Stream.Stop = nil
		}()
		reconnectStream := func() bool {
			if terminal.Stream.Generation != generation {
				return false
			}
			terminal.Stream.Active = false
			terminal.Stream.Stop = nil
			if terminal.State == "exited" || ctx.Err() != nil {
				return false
			}
			attempt := terminal.Stream.RetryCount
			if attempt > 5 {
				attempt = 5
			}
			backoff := time.Duration(1<<attempt) * 200 * time.Millisecond
			terminal.Stream.RetryCount++
			select {
			case <-ctx.Done():
				return false
			case <-time.After(backoff):
			}
			if stop != nil {
				stop()
			}
			if terminal.Stream.Generation != generation {
				return false
			}
			stream, stop = r.client.Stream(terminal.Channel)
			terminal.Stream.Active = true
			terminal.Stream.Stop = stop
			return true
		}
		var (
			pending    protocol.StreamFrame
			hasPending bool
		)
		for {
			frame, ok := nextClientStreamFrame(ctx, stream, &pending, &hasPending)
			if !ok {
				if !reconnectStream() {
					return
				}
				hasPending = false
				continue
			}
			if frame.Type == protocol.TypeOutput {
				frame, pending, hasPending, ok = coalesceClientOutputFrames(frame, stream, r.clientOutputBatchDelay(), terminal.Stream)
			}
			if terminal.Stream.Generation != generation {
				return
			}
			terminal.Stream.RetryCount = 0
			r.handleStreamFrame(terminalID, frame)
			if frame.Type == protocol.TypeClosed {
				return
			}
			if !ok {
				if !reconnectStream() {
					return
				}
				hasPending = false
			}
		}
	}(generation)
	return nil
}

func nextClientStreamFrame(ctx context.Context, stream <-chan protocol.StreamFrame, pending *protocol.StreamFrame, hasPending *bool) (protocol.StreamFrame, bool) {
	if hasPending != nil && *hasPending {
		*hasPending = false
		return *pending, true
	}
	select {
	case <-ctx.Done():
		return protocol.StreamFrame{}, false
	case frame, ok := <-stream:
		return frame, ok
	}
}

func (r *Runtime) clientOutputBatchDelay() time.Duration {
	delay := effectiveClientOutputBatchDelay()
	if delay <= 0 {
		return 0
	}
	if r != nil && r.consumeInteractiveBypass() {
		perftrace.Count("runtime.stream.output.interactive_bypass", 0)
		return effectiveInteractiveOutputBatchDelay()
	}
	return delay
}

func coalesceClientOutputFrames(first protocol.StreamFrame, stream <-chan protocol.StreamFrame, batchDelay time.Duration, syncState StreamState) (protocol.StreamFrame, protocol.StreamFrame, bool, bool) {
	merged := protocol.StreamFrame{
		Type:    protocol.TypeOutput,
		Payload: append([]byte(nil), first.Payload...),
	}
	_ = updateSynchronizedOutputState(&syncState, first.Payload)
	handle := func(frame protocol.StreamFrame) (protocol.StreamFrame, bool, bool) {
		if frame.Type != protocol.TypeOutput {
			return frame, true, true
		}
		if len(merged.Payload) > 0 && len(merged.Payload)+len(frame.Payload) > protocol.MaxFrameSize {
			return frame, true, true
		}
		merged.Payload = append(merged.Payload, frame.Payload...)
		_ = updateSynchronizedOutputState(&syncState, frame.Payload)
		return protocol.StreamFrame{}, false, true
	}
	drainReady := func() (protocol.StreamFrame, bool, bool) {
		for {
			select {
			case frame, ok := <-stream:
				if !ok {
					return protocol.StreamFrame{}, false, false
				}
				if pending, hasPending, ok := handle(frame); hasPending || !ok {
					return pending, hasPending, ok
				}
			default:
				return protocol.StreamFrame{}, false, true
			}
		}
	}
	if batchDelay <= 0 || len(merged.Payload) >= protocol.MaxFrameSize {
		pending, hasPending, ok := drainReady()
		return merged, pending, hasPending, ok
	}
	timer := time.NewTimer(batchDelay)
	defer timer.Stop()
	waitBudget := effectiveSynchronizedOutputWaitBudget()
	waitStep := effectiveSynchronizedOutputWaitStep()
	waitedForSync := time.Duration(0)
	for {
		select {
		case frame, ok := <-stream:
			if !ok {
				return merged, protocol.StreamFrame{}, false, false
			}
			if pending, hasPending, ok := handle(frame); hasPending || !ok {
				return merged, pending, hasPending, ok
			}
			if len(merged.Payload) >= protocol.MaxFrameSize {
				return merged, protocol.StreamFrame{}, false, true
			}
		case <-timer.C:
			if syncState.synchronizedOutputActive && waitBudget > waitedForSync {
				nextWait := waitStep
				if remaining := waitBudget - waitedForSync; nextWait > remaining {
					nextWait = remaining
				}
				if nextWait <= 0 {
					pending, hasPending, ok := drainReady()
					return merged, pending, hasPending, ok
				}
				waitedForSync += nextWait
				timer.Reset(nextWait)
				continue
			}
			pending, hasPending, ok := drainReady()
			return merged, pending, hasPending, ok
		}
	}
}

func effectiveClientOutputBatchDelay() time.Duration {
	delay := clientOutputBatchDelay
	if shared.RemoteLatencyProfileEnabled() && (delay <= 0 || delay > remoteClientOutputBatchDelay) {
		delay = remoteClientOutputBatchDelay
	}
	return shared.DurationOverride("TERMX_CLIENT_OUTPUT_BATCH_DELAY", delay)
}

func effectiveInteractiveOutputBatchDelay() time.Duration {
	delay := interactiveOutputBatchDelay
	if shared.RemoteLatencyProfileEnabled() && (delay <= 0 || delay > remoteInteractiveOutputBatchDelay) {
		delay = remoteInteractiveOutputBatchDelay
	}
	return shared.DurationOverride("TERMX_INTERACTIVE_OUTPUT_BATCH_DELAY", delay)
}

func effectiveSynchronizedOutputWaitStep() time.Duration {
	delay := synchronizedOutputWaitStep
	if shared.RemoteLatencyProfileEnabled() && (delay <= 0 || delay > remoteSynchronizedOutputWaitStep) {
		delay = remoteSynchronizedOutputWaitStep
	}
	return shared.DurationOverride("TERMX_SYNC_OUTPUT_WAIT_STEP", delay)
}

func effectiveSynchronizedOutputWaitBudget() time.Duration {
	delay := synchronizedOutputWaitBudget
	if shared.RemoteLatencyProfileEnabled() && (delay <= 0 || delay > remoteSynchronizedOutputWaitBudget) {
		delay = remoteSynchronizedOutputWaitBudget
	}
	return shared.DurationOverride("TERMX_SYNC_OUTPUT_WAIT_BUDGET", delay)
}

func (r *Runtime) handleStreamFrame(terminalID string, frame protocol.StreamFrame) {
	finish := perftrace.Measure(streamFrameMetric(frame.Type))
	defer func() {
		finish(len(frame.Payload))
	}()
	terminal := r.registry.Get(terminalID)
	if terminal == nil {
		return
	}
	switch frame.Type {
	case protocol.TypeOutput:
		vt := r.ensureVTerm(terminal)
		if vt == nil {
			return
		}
		n, err := vt.Write(frame.Payload)
		if err != nil || n != len(frame.Payload) {
			terminal.Recovery.SyncLost = true
			if dropped := len(frame.Payload) - max(0, n); dropped > 0 {
				terminal.Recovery.DroppedBytes += uint64(dropped)
			}
			resetSynchronizedOutputState(&terminal.Stream)
			r.recoverSnapshot(terminalID)
			return
		}
		syncActive := updateSynchronizedOutputState(&terminal.Stream, frame.Payload)
		terminal.Recovery = RecoveryState{}
		if syncActive {
			return
		}
		terminal.BootstrapPending = false
		if terminal.PreferSnapshot {
			terminal.PreferSnapshot = false
		}
		r.bumpSurfaceVersion(terminal)
		if terminal.PreferSnapshot || terminal.Snapshot == nil {
			r.refreshSnapshot(terminalID)
			return
		}
		r.invalidate()
	case protocol.TypeScreenUpdate:
		decodeFinish := perftrace.Measure("runtime.stream.screen_update.decode")
		update, err := protocol.DecodeScreenUpdatePayload(frame.Payload)
		decodeFinish(len(frame.Payload))
		if err != nil {
			return
		}
		if update.FullReplace {
			perftrace.Count("runtime.stream.screen_update.full_replace", 1)
		}
		if changedRows := len(update.ChangedRows); changedRows > 0 {
			perftrace.Count("runtime.stream.screen_update.changed_rows", changedRows)
		}
		if update.ScrollbackTrim > 0 {
			perftrace.Count("runtime.stream.screen_update.scrollback_trim_rows", update.ScrollbackTrim)
		}
		if appendedRows := len(update.ScrollbackAppend); appendedRows > 0 {
			perftrace.Count("runtime.stream.screen_update.scrollback_append_rows", appendedRows)
		}
		if update.Title != "" && update.Title != terminal.Title {
			terminal.Title = update.Title
			r.touch()
			if r.onTitleChange != nil {
				r.onTitleChange(terminal.TerminalID, update.Title)
			}
		}
		if terminal.BootstrapPending && terminal.Snapshot != nil && screenUpdateSeemsBlank(update) {
			invalidateFinish := perftrace.Measure("runtime.stream.screen_update.invalidate")
			r.invalidate()
			invalidateFinish(0)
			return
		}
		screenUpdateSummary := visibleScreenUpdateSummary(update)
		snapshotApplyFinish := perftrace.Measure("runtime.stream.screen_update.snapshot_apply")
		terminal.Snapshot = applyScreenUpdateSnapshot(terminal.Snapshot, terminalID, update)
		snapshotApplyFinish(0)
		vt := r.ensureVTerm(terminal)
		if vt != nil && terminal.Snapshot != nil {
			appliedPartial := false
			if !update.FullReplace {
				if applier, ok := vt.(screenUpdateApplier); ok {
					loadFinish := perftrace.Measure("runtime.stream.screen_update.load_vterm_partial")
					appliedPartial = applier.ApplyScreenUpdate(update)
					loadFinish(0)
				}
			}
			if !appliedPartial {
				loadFinish := perftrace.Measure("runtime.stream.screen_update.load_vterm_full")
				loadSnapshotIntoVTerm(vt, terminal.Snapshot)
				loadFinish(0)
			}
			terminal.PreferSnapshot = false
			invalidateFinish := perftrace.Measure("runtime.stream.screen_update.invalidate")
			r.bumpSurfaceVersion(terminal)
			screenUpdateSummary.SurfaceVersion = terminal.SurfaceVersion
			terminal.ScreenUpdate = screenUpdateSummary
			terminal.SnapshotVersion = terminal.SurfaceVersion
			terminal.BootstrapPending = false
			terminal.Recovery = RecoveryState{}
			r.invalidate()
			invalidateFinish(0)
		} else {
			invalidateFinish := perftrace.Measure("runtime.stream.screen_update.invalidate")
			terminal.PreferSnapshot = true
			terminal.SnapshotVersion++
			screenUpdateSummary.SurfaceVersion = terminal.SurfaceVersion
			terminal.ScreenUpdate = screenUpdateSummary
			terminal.BootstrapPending = false
			terminal.Recovery = RecoveryState{}
			r.invalidate()
			invalidateFinish(0)
		}
	case protocol.TypeResize:
		cols, rows, err := protocol.DecodeResizePayload(frame.Payload)
		if err != nil || cols == 0 || rows == 0 {
			return
		}
		if terminal.PreferSnapshot && terminal.Snapshot != nil {
			terminal.Snapshot.Size = protocol.Size{Cols: cols, Rows: rows}
			terminal.Snapshot.Timestamp = time.Now()
			terminal.SnapshotVersion++
			r.invalidate()
			return
		}
		vt := r.ensureVTerm(terminal)
		if vt == nil {
			return
		}
		currentCols, currentRows := vt.Size()
		if currentCols != int(cols) || currentRows != int(rows) {
			vt.Resize(int(cols), int(rows))
			resetSynchronizedOutputState(&terminal.Stream)
			if terminal.PreferSnapshot {
				r.bumpSurfaceVersion(terminal)
				if terminal.Snapshot == nil {
					r.refreshSnapshot(terminalID)
					return
				}
				r.invalidate()
				return
			}
			if terminal.BootstrapPending {
				// Mirror local resize behavior during bootstrap: keep the
				// provisional snapshot geometry aligned with the live surface so
				// follower views don't stay pinned to stale dimensions while the
				// initial replay is still in-flight.
				r.bumpSurfaceVersion(terminal)
				r.refreshSnapshot(terminalID)
				return
			}
			r.bumpSurfaceVersion(terminal)
			r.refreshSnapshot(terminalID)
		}
	case protocol.TypeBootstrapDone:
		if !terminal.BootstrapPending {
			return
		}
		terminal.BootstrapPending = false
		if terminal.PreferSnapshot && terminal.Snapshot != nil {
			terminal.SnapshotVersion++
			r.invalidate()
			return
		}
		r.bumpSurfaceVersion(terminal)
		r.refreshSnapshot(terminalID)
	case protocol.TypeSyncLost:
		if terminal.PreferSnapshot && terminal.Snapshot != nil {
			terminal.BootstrapPending = false
			resetSynchronizedOutputState(&terminal.Stream)
			terminal.Recovery = RecoveryState{}
			terminal.SnapshotVersion++
			r.invalidate()
			return
		}
		terminal.BootstrapPending = false
		resetSynchronizedOutputState(&terminal.Stream)
		terminal.Recovery.SyncLost = true
		dropped, err := protocol.DecodeSyncLostPayload(frame.Payload)
		if err == nil {
			terminal.Recovery.DroppedBytes += dropped
		}
		r.recoverSnapshot(terminalID)
	case protocol.TypeClosed:
		terminal.Stream.Active = false
		terminal.BootstrapPending = false
		resetSynchronizedOutputState(&terminal.Stream)
		code, err := protocol.DecodeClosedPayload(frame.Payload)
		if err == nil {
			exitCode := int(code)
			terminal.ExitCode = &exitCode
		}
		terminal.State = "exited"
		syncSurfaceScrollbackState(terminal)
		r.invalidate()
	}
}

func streamFrameMetric(frameType uint8) string {
	switch frameType {
	case protocol.TypeOutput:
		return "runtime.stream.output"
	case protocol.TypeResize:
		return "runtime.stream.resize"
	case protocol.TypeScreenUpdate:
		return "runtime.stream.screen_update"
	case protocol.TypeBootstrapDone:
		return "runtime.stream.bootstrap_done"
	case protocol.TypeSyncLost:
		return "runtime.stream.sync_lost"
	case protocol.TypeClosed:
		return "runtime.stream.closed"
	default:
		return "runtime.stream.unknown"
	}
}

func updateSynchronizedOutputState(state *StreamState, payload []byte) bool {
	if state == nil {
		return false
	}
	if len(payload) == 0 {
		return state.synchronizedOutputActive
	}

	tail := state.synchronizedOutputTail
	combined := tail + string(payload)
	tailLen := len(tail)

	for i := 0; i < len(combined); {
		switch {
		case strings.HasPrefix(combined[i:], synchronizedOutputBegin):
			if i+len(synchronizedOutputBegin) <= tailLen {
				i++
				continue
			}
			state.synchronizedOutputActive = true
			i += len(synchronizedOutputBegin)
		case strings.HasPrefix(combined[i:], synchronizedOutputEnd):
			if i+len(synchronizedOutputEnd) <= tailLen {
				i++
				continue
			}
			state.synchronizedOutputActive = false
			i += len(synchronizedOutputEnd)
		default:
			i++
		}
	}

	maxTail := len(synchronizedOutputBegin) - 1
	if len(combined) > maxTail {
		state.synchronizedOutputTail = combined[len(combined)-maxTail:]
	} else {
		state.synchronizedOutputTail = combined
	}
	return state.synchronizedOutputActive
}

func resetSynchronizedOutputState(state *StreamState) {
	if state == nil {
		return
	}
	state.synchronizedOutputActive = false
	state.synchronizedOutputTail = ""
}

func screenUpdateSeemsBlank(update protocol.ScreenUpdate) bool {
	if !update.FullReplace || len(update.ChangedRows) > 0 || len(update.ScrollbackAppend) > 0 {
		return false
	}
	for _, row := range update.Screen.Cells {
		for _, cell := range row {
			if strings.TrimSpace(cell.Content) != "" {
				return false
			}
		}
	}
	return true
}
