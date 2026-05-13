package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/lozzow/termx/termx-core/perftrace"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
)

type screenUpdateApplier interface {
	ApplyScreenUpdate(update protocol.ScreenUpdate) bool
}

func recordScreenUpdateMetrics(update protocol.ScreenUpdate) {
	if update.FullReplace {
		perftrace.Count("runtime.stream.screen_update.full_replace", 1)
	}
	if changedRows := len(screenUpdateSummaryFromProtocol(update).ChangedRows); changedRows > 0 {
		perftrace.Count("runtime.stream.screen_update.changed_rows", changedRows)
	}
	if update.ScrollbackTrim > 0 {
		perftrace.Count("runtime.stream.screen_update.scrollback_trim_rows", update.ScrollbackTrim)
	}
	if appendedRows := len(update.ScrollbackAppend); appendedRows > 0 {
		perftrace.Count("runtime.stream.screen_update.scrollback_append_rows", appendedRows)
	}
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
		for {
			frame, ok := nextClientStreamFrame(ctx, stream)
			if !ok {
				if !reconnectStream() {
					return
				}
				continue
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
			}
		}
	}(generation)
	return nil
}

func nextClientStreamFrame(ctx context.Context, stream <-chan protocol.StreamFrame) (protocol.StreamFrame, bool) {
	select {
	case <-ctx.Done():
		return protocol.StreamFrame{}, false
	case frame, ok := <-stream:
		return frame, ok
	}
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
	case protocol.TypeScreenUpdate:
		r.handleStructuredScreenUpdateFrame(terminal, terminalID, frame)
	case protocol.TypeResize:
		r.handleResizeFrame(terminal, terminalID, frame)
	case protocol.TypeBootstrapDone:
		r.handleBootstrapDoneFrame(terminal, terminalID)
	case protocol.TypeSyncLost:
		r.handleSyncLostFrame(terminal, terminalID, frame)
	case protocol.TypeClosed:
		r.handleClosedFrame(terminal, frame)
	}
}

func (r *Runtime) handleStructuredScreenUpdateFrame(terminal *TerminalRuntime, terminalID string, frame protocol.StreamFrame) {
	decodeFinish := perftrace.Measure("runtime.stream.screen_update.decode")
	contract, err := DecodeScreenUpdateContractPayload(frame.Payload)
	decodeFinish(len(frame.Payload))
	if err != nil {
		return
	}
	r.applyDecodedScreenUpdateContract(terminal, terminalID, contract)
}

func (r *Runtime) handleResizeFrame(terminal *TerminalRuntime, terminalID string, frame protocol.StreamFrame) {
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
			r.bumpSurfaceVersion(terminal)
			r.refreshSnapshot(terminalID)
			return
		}
		r.bumpSurfaceVersion(terminal)
		r.refreshSnapshot(terminalID)
	}
}

func (r *Runtime) handleBootstrapDoneFrame(terminal *TerminalRuntime, terminalID string) {
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
}

func (r *Runtime) handleSyncLostFrame(terminal *TerminalRuntime, terminalID string, frame protocol.StreamFrame) {
	if terminal.PreferSnapshot && terminal.Snapshot != nil {
		terminal.BootstrapPending = false
		terminal.Recovery = RecoveryState{}
		terminal.SnapshotVersion++
		r.invalidate()
		return
	}
	terminal.BootstrapPending = false
	terminal.Recovery.SyncLost = true
	dropped, err := protocol.DecodeSyncLostPayload(frame.Payload)
	if err == nil {
		terminal.Recovery.DroppedBytes += dropped
	}
	r.recoverSnapshot(terminalID)
}

func (r *Runtime) handleClosedFrame(terminal *TerminalRuntime, frame protocol.StreamFrame) {
	terminal.Stream.Active = false
	terminal.BootstrapPending = false
	code, err := protocol.DecodeClosedPayload(frame.Payload)
	if err == nil {
		exitCode := int(code)
		terminal.ExitCode = &exitCode
	}
	terminal.State = "exited"
	syncSurfaceScrollbackState(terminal)
	r.invalidate()
}

func streamFrameMetric(frameType uint8) string {
	switch frameType {
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
