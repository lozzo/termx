package runtime

import "context"

func (r *Runtime) recoverSnapshot(terminalID string) {
	if r == nil || terminalID == "" {
		return
	}
	limit := 0
	if terminal := r.registry.Get(terminalID); terminal != nil && terminal.ScrollbackLoadedLimit > 0 {
		limit = terminal.ScrollbackLoadedLimit
	}
	if _, err := r.LoadSnapshot(context.Background(), terminalID, 0, limit); err == nil {
		r.invalidate()
	}
}
