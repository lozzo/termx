package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
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
			r.recoverSnapshot(terminalID)
			return
		}
		terminal.Recovery = RecoveryState{}
		r.refreshSnapshot(terminalID)
	case protocol.TypeSyncLost:
		terminal.Recovery.SyncLost = true
		dropped, err := protocol.DecodeSyncLostPayload(frame.Payload)
		if err == nil {
			terminal.Recovery.DroppedBytes += dropped
		}
		r.recoverSnapshot(terminalID)
	case protocol.TypeClosed:
		terminal.Stream.Active = false
		code, err := protocol.DecodeClosedPayload(frame.Payload)
		if err == nil {
			exitCode := int(code)
			terminal.ExitCode = &exitCode
		}
		terminal.State = "exited"
		r.invalidate()
	}
}
