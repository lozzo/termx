package runtime

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
)

func (r *Runtime) LoadSnapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	if r == nil || r.client == nil {
		return nil, shared.UserVisibleError{Op: "snapshot terminal", Err: fmt.Errorf("runtime client is nil")}
	}
	snapshot, err := r.client.Snapshot(ctx, terminalID, offset, limit)
	if err != nil {
		return nil, shared.UserVisibleError{Op: "snapshot terminal", Err: err}
	}
	terminal := r.registry.GetOrCreate(terminalID)
	if terminal != nil {
		terminal.Snapshot = snapshot
	}
	return snapshot, nil
}
