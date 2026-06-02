package termx

import (
	"log/slog"
	"sync"

	"github.com/lozzow/termx/termx-vterm/vterm"
)

type terminalGridAppender struct {
	store           *terminalGridStore
	terminalID      string
	logger          *slog.Logger
	retentionPolicy terminalGridRetentionPolicy

	mu      sync.Mutex
	cond    *sync.Cond
	queue   []terminalGridAppendBatch
	closed  bool
	writing bool
	done    chan struct{}
	stop    sync.Once
}

type terminalGridAppendBatch struct {
	rows           []vterm.DamageOp
	logicalLineIDs []uint64
}

func newTerminalGridAppender(store *terminalGridStore, terminalID string, retentionPolicy terminalGridRetentionPolicy, logger *slog.Logger) *terminalGridAppender {
	if store == nil {
		return nil
	}
	store.SetRetentionPolicy(retentionPolicy)
	a := &terminalGridAppender{
		store:           store,
		terminalID:      terminalID,
		logger:          logger,
		retentionPolicy: retentionPolicy,
		done:            make(chan struct{}),
	}
	a.cond = sync.NewCond(&a.mu)
	go a.run()
	return a
}

func (a *terminalGridAppender) append(rows []vterm.DamageOp) {
	a.appendWithLogicalLineIDs(rows, nil)
}

func (a *terminalGridAppender) appendWithLogicalLineIDs(rows []vterm.DamageOp, logicalLineIDs []uint64) {
	if a == nil || len(rows) == 0 {
		return
	}
	rows = cloneGridDamageOps(rows)
	logicalLineIDs = alignGridAppenderLogicalLineIDs(logicalLineIDs, len(rows))
	a.mu.Lock()
	if !a.closed {
		a.queue = append(a.queue, terminalGridAppendBatch{rows: rows, logicalLineIDs: logicalLineIDs})
		traceGridDamageOps("core.grid_appender.enqueue", a.terminalID, rows, "queue_after", terminalGridAppendBatchRowCount(a.queue))
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
		batches := a.queue
		a.queue = nil
		a.writing = true
		a.mu.Unlock()

		rows, logicalLineIDs := terminalGridAppendRowsFromBatches(batches)
		traceGridDamageOps("core.grid_appender.flush", a.terminalID, rows)
		if err := a.store.AppendDamageRowsWithLogicalLineIDs(rows, logicalLineIDs); err != nil && a.logger != nil {
			a.logger.Warn("termx terminal grid append failed", "terminal_id", a.terminalID, "error", err)
		}

		a.mu.Lock()
		a.writing = false
		a.cond.Broadcast()
		a.mu.Unlock()
	}
}

func alignGridAppenderLogicalLineIDs(values []uint64, size int) []uint64 {
	if size <= 0 {
		return nil
	}
	out := make([]uint64, size)
	copy(out, values)
	return out
}

func terminalGridAppendBatchRowCount(batches []terminalGridAppendBatch) int {
	total := 0
	for _, batch := range batches {
		total += len(batch.rows)
	}
	return total
}

func terminalGridAppendRowsFromBatches(batches []terminalGridAppendBatch) ([]vterm.DamageOp, []uint64) {
	rowCount := terminalGridAppendBatchRowCount(batches)
	if rowCount == 0 {
		return nil, nil
	}
	rows := make([]vterm.DamageOp, 0, rowCount)
	logicalLineIDs := make([]uint64, 0, rowCount)
	for _, batch := range batches {
		rows = append(rows, batch.rows...)
		logicalLineIDs = append(logicalLineIDs, alignGridAppenderLogicalLineIDs(batch.logicalLineIDs, len(batch.rows))...)
	}
	return rows, logicalLineIDs
}

func cloneGridDamageOps(rows []vterm.DamageOp) []vterm.DamageOp {
	if len(rows) == 0 {
		return nil
	}
	out := make([]vterm.DamageOp, 0, len(rows))
	for _, row := range rows {
		out = append(out, cloneGridDamageOp(row))
	}
	return out
}

func cloneGridDamageOp(row vterm.DamageOp) vterm.DamageOp {
	row.Cells = cloneVTermCells(row.Cells)
	if len(row.Runs) > 0 {
		row.Runs = append([]vterm.CellRun(nil), row.Runs...)
	}
	return row
}

func cloneVTermCells(cells []vterm.Cell) []vterm.Cell {
	if len(cells) == 0 {
		return nil
	}
	return append([]vterm.Cell(nil), cells...)
}
