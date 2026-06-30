package termxcorev2

import (
	"context"
	"sync"

	"github.com/lozzow/termx/termx-core-v2/history"
)

const terminalHistoryIngestBatchMaxTransactions = 1024

// terminalHistoryIngestQueue 是 SemanticTap 之后的 history transaction backlog。
// domain owner 是 core terminal ingest：queue 只接收 tap 产出的 immutable semantic
// transaction 副本，不保存 raw PTY bytes、不启动第二个 vterm，也不解释 history
// truth。失败条件是任何 consumer 绕过 tap 往这里塞 raw PTY replay。
type terminalHistoryIngestQueue struct {
	mu        sync.Mutex
	cond      *sync.Cond
	pending   []terminalHistoryIngestItem
	flushSeq  uint64
	doneSeq   uint64
	inFlight  bool
	closed    bool
	done      chan struct{}
	closeOnce sync.Once
}

type terminalHistoryIngestItem struct {
	tx       history.TerminalSemanticTransaction
	flushSeq uint64
}

func newTerminalHistoryIngestQueue() *terminalHistoryIngestQueue {
	queue := &terminalHistoryIngestQueue{
		done: make(chan struct{}),
	}
	queue.cond = sync.NewCond(&queue.mu)
	return queue
}

func (queue *terminalHistoryIngestQueue) Enqueue(tx history.TerminalSemanticTransaction) bool {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.closed {
		return false
	}
	queue.pending = append(queue.pending, terminalHistoryIngestItem{tx: cloneSemanticTapTransaction(tx)})
	queue.cond.Signal()
	return true
}

func (queue *terminalHistoryIngestQueue) Flush(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan error, 1)
	queue.mu.Lock()
	if err := ctx.Err(); err != nil {
		queue.mu.Unlock()
		return err
	}
	if queue.closed && len(queue.pending) == 0 && !queue.inFlight {
		queue.mu.Unlock()
		return nil
	}
	queue.flushSeq++
	target := queue.flushSeq
	queue.pending = append(queue.pending, terminalHistoryIngestItem{flushSeq: target})
	queue.cond.Signal()
	go func() {
		queue.mu.Lock()
		defer queue.mu.Unlock()
		for queue.doneSeq < target {
			if err := ctx.Err(); err != nil {
				done <- err
				return
			}
			queue.cond.Wait()
		}
		done <- nil
	}()
	queue.mu.Unlock()

	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			queue.mu.Lock()
			queue.cond.Broadcast()
			queue.mu.Unlock()
			select {
			case err := <-done:
				if err != nil {
					return err
				}
				return ctx.Err()
			default:
				return ctx.Err()
			}
		}
	}
}

func (queue *terminalHistoryIngestQueue) Close() {
	queue.mu.Lock()
	queue.closed = true
	queue.cond.Broadcast()
	queue.mu.Unlock()
}

func (queue *terminalHistoryIngestQueue) Wait() {
	<-queue.done
}

func (queue *terminalHistoryIngestQueue) Run(ingest func([]history.TerminalSemanticTransaction) error) {
	defer func() {
		queue.closeOnce.Do(func() {})
		close(queue.done)
	}()
	for {
		batch, ok := queue.nextBatch()
		if !ok {
			return
		}
		if ingest != nil {
			txs := terminalHistoryIngestBatchTransactions(batch)
			if len(txs) > 0 {
				_ = ingest(txs)
			}
		}
		if target, ok := terminalHistoryIngestBatchFlushTarget(batch); ok {
			queue.markFlushed(target)
		}
		queue.finishBatch()
		// 中文说明：history transaction 已经交给 store/backend 后才尝试归还临时页；
		// 这里不删除 history truth，也不是后台定时 scrub。
		maybeReclaimDaemonBoundaryHeap()
	}
}

func terminalHistoryIngestBatchTransactions(batch []terminalHistoryIngestItem) []history.TerminalSemanticTransaction {
	if len(batch) == 0 {
		return nil
	}
	txs := make([]history.TerminalSemanticTransaction, 0, len(batch))
	for _, item := range batch {
		if item.flushSeq != 0 {
			continue
		}
		txs = append(txs, cloneSemanticTapTransaction(item.tx))
	}
	return txs
}

func (queue *terminalHistoryIngestQueue) nextBatch() ([]terminalHistoryIngestItem, bool) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	for len(queue.pending) == 0 && !queue.closed {
		queue.cond.Wait()
	}
	if len(queue.pending) == 0 && queue.closed {
		return nil, false
	}
	count := 0
	for count < len(queue.pending) {
		if queue.pending[count].flushSeq != 0 {
			count++
			break
		}
		if count > 0 && count >= terminalHistoryIngestBatchMaxTransactions {
			break
		}
		count++
	}
	batch := queue.cloneBatchLocked(queue.pending[:count])
	queue.dropPendingPrefixLocked(count)
	if len(queue.pending) > 0 {
		queue.cond.Signal()
	}
	queue.inFlight = true
	return batch, true
}

func (queue *terminalHistoryIngestQueue) cloneBatchLocked(items []terminalHistoryIngestItem) []terminalHistoryIngestItem {
	if len(items) == 0 {
		return nil
	}
	batch := make([]terminalHistoryIngestItem, len(items))
	for i, item := range items {
		batch[i] = item
		batch[i].tx = cloneSemanticTapTransaction(item.tx)
	}
	return batch
}

func (queue *terminalHistoryIngestQueue) markFlushed(target uint64) {
	queue.mu.Lock()
	if target > queue.doneSeq {
		queue.doneSeq = target
	}
	queue.cond.Broadcast()
	queue.mu.Unlock()
}

func (queue *terminalHistoryIngestQueue) finishBatch() {
	queue.mu.Lock()
	queue.inFlight = false
	queue.cond.Broadcast()
	queue.mu.Unlock()
}

func terminalHistoryIngestBatchFlushTarget(batch []terminalHistoryIngestItem) (uint64, bool) {
	for i := len(batch) - 1; i >= 0; i-- {
		if batch[i].flushSeq != 0 {
			return batch[i].flushSeq, true
		}
	}
	return 0, false
}

func (queue *terminalHistoryIngestQueue) dropPendingPrefixLocked(count int) {
	if count <= 0 {
		return
	}
	remaining := len(queue.pending) - count
	copy(queue.pending, queue.pending[count:])
	for i := remaining; i < len(queue.pending); i++ {
		queue.pending[i] = terminalHistoryIngestItem{}
	}
	if remaining == 0 {
		queue.pending = nil
		return
	}
	queue.pending = queue.pending[:remaining]
}
