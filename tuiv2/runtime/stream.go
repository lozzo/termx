package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
)

const (
	synchronizedOutputBegin = "\x1b[?2026h"
	synchronizedOutputEnd   = "\x1b[?2026l"
)

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
	stream, stop := r.client.Stream(terminal.Channel)
	terminal.Stream.Active = true
	terminal.Stream.Stop = stop
	go func() {
		defer func() {
			if stop != nil {
				stop()
			}
			terminal.Stream.Active = false
			terminal.Stream.Stop = nil
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case frame, ok := <-stream:
				if !ok {
					terminal.Stream.Active = false
					terminal.Stream.Stop = nil
					if terminal.State == "exited" || ctx.Err() != nil {
						return
					}
					attempt := terminal.Stream.RetryCount
					if attempt > 5 {
						attempt = 5
					}
					backoff := time.Duration(1<<attempt) * 200 * time.Millisecond
					terminal.Stream.RetryCount++
					select {
					case <-ctx.Done():
						return
					case <-time.After(backoff):
					}
					if stop != nil {
						stop()
					}
					stream, stop = r.client.Stream(terminal.Channel)
					terminal.Stream.Active = true
					terminal.Stream.Stop = stop
					continue
				}
				terminal.Stream.RetryCount = 0
				r.handleStreamFrame(terminalID, frame)
				if frame.Type == protocol.TypeClosed {
					return
				}
			}
		}
	}()
	return nil
}

func (r *Runtime) handleStreamFrame(terminalID string, frame protocol.StreamFrame) {
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
		r.refreshSnapshot(terminalID)
	case protocol.TypeResize:
		cols, rows, err := protocol.DecodeResizePayload(frame.Payload)
		if err != nil || cols == 0 || rows == 0 {
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
			r.refreshSnapshot(terminalID)
		}
	case protocol.TypeSyncLost:
		resetSynchronizedOutputState(&terminal.Stream)
		terminal.Recovery.SyncLost = true
		dropped, err := protocol.DecodeSyncLostPayload(frame.Payload)
		if err == nil {
			terminal.Recovery.DroppedBytes += dropped
		}
		r.recoverSnapshot(terminalID)
	case protocol.TypeClosed:
		terminal.Stream.Active = false
		resetSynchronizedOutputState(&terminal.Stream)
		code, err := protocol.DecodeClosedPayload(frame.Payload)
		if err == nil {
			exitCode := int(code)
			terminal.ExitCode = &exitCode
		}
		terminal.State = "exited"
		r.refreshSnapshot(terminalID)
		r.invalidate()
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
