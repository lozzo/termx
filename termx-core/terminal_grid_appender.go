package termx

import (
	"log/slog"
	"sync"

	"github.com/lozzow/termx/termx-core/vterm"
)

type terminalGridAppender struct {
	store      *terminalGridStore
	terminalID string
	logger     *slog.Logger

	mu      sync.Mutex
	cond    *sync.Cond
	queue   []vterm.DamageOp
	closed  bool
	writing bool
	done    chan struct{}
	stop    sync.Once
}

func newTerminalGridAppender(store *terminalGridStore, terminalID string, logger *slog.Logger) *terminalGridAppender {
	if store == nil {
		return nil
	}
	a := &terminalGridAppender{
		store:      store,
		terminalID: terminalID,
		logger:     logger,
		done:       make(chan struct{}),
	}
	a.cond = sync.NewCond(&a.mu)
	go a.run()
	return a
}

func (a *terminalGridAppender) append(rows []vterm.DamageOp) {
	if a == nil || len(rows) == 0 {
		return
	}
	a.mu.Lock()
	if !a.closed {
		a.queue = append(a.queue, rows...)
		a.cond.Signal()
	}
	a.mu.Unlock()
}

func (a *terminalGridAppender) flush() {
	if a == nil {
		return
	}
	a.mu.Lock()
	for len(a.queue) > 0 || a.writing {
		a.cond.Wait()
	}
	a.mu.Unlock()
}

func (a *terminalGridAppender) close() {
	if a == nil {
		return
	}
	a.stop.Do(func() {
		a.mu.Lock()
		a.closed = true
		a.cond.Broadcast()
		a.mu.Unlock()
	})
	<-a.done
}

func (a *terminalGridAppender) run() {
	defer close(a.done)
	for {
		a.mu.Lock()
		for len(a.queue) == 0 && !a.closed {
			a.cond.Wait()
		}
		if len(a.queue) == 0 && a.closed {
			a.mu.Unlock()
			return
		}
		rows := a.queue
		a.queue = nil
		a.writing = true
		a.mu.Unlock()

		if err := a.store.AppendDamageRows(rows); err != nil && a.logger != nil {
			a.logger.Warn("termx terminal grid append failed", "terminal_id", a.terminalID, "error", err)
		}

		a.mu.Lock()
		a.writing = false
		a.cond.Broadcast()
		a.mu.Unlock()
	}
}

func cloneGridDamageOps(rows []vterm.DamageOp) []vterm.DamageOp {
	if len(rows) == 0 {
		return nil
	}
	out := make([]vterm.DamageOp, 0, len(rows))
	for _, row := range rows {
		row.Cells = cloneVTermCells(row.Cells)
		out = append(out, row)
	}
	return out
}

func cloneVTermCells(cells []vterm.Cell) []vterm.Cell {
	if len(cells) == 0 {
		return nil
	}
	return append([]vterm.Cell(nil), cells...)
}
