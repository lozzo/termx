package runtime

import "context"

func (r *Runtime) recoverSnapshot(terminalID string) {
	if r == nil || terminalID == "" {
		return
	}
	if _, err := r.LoadSnapshot(context.Background(), terminalID, 0, 0); err == nil {
		r.invalidate()
	}
}
