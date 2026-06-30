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
	mu         sync.Mutex
	cond       *sync.Cond
	pending    []terminalHistoryIngestItem
	targetSeq  uint64
	appliedSeq uint64
	waiters    map[uint64][]chan struct{}
	inFlight   bool
	closed     bool
	done       chan struct{}
	closeOnce  sync.Once
}

type terminalHistoryIngestItem struct {
	tx  history.TerminalSemanticTransaction
	seq uint64
}

func newTerminalHistoryIngestQueue(startSeq uint64) *terminalHistoryIngestQueue {
	queue := &terminalHistoryIngestQueue{
		appliedSeq: startSeq,
		targetSeq:  startSeq,
		done:       make(chan struct{}),
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
	queue.targetSeq++
	queue.pending = append(queue.pending, terminalHistoryIngestItem{
		tx:  cloneSemanticTapTransaction(tx),
		seq: queue.targetSeq,
	})
	queue.cond.Signal()
	return true
}

func (queue *terminalHistoryIngestQueue) Flush(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	queue.mu.Lock()
	if err := ctx.Err(); err != nil {
		queue.mu.Unlock()
		return err
	}
	target := queue.targetSeq
	if target == 0 || queue.appliedSeq >= target {
		queue.mu.Unlock()
		return nil
	}
	if queue.waiters == nil {
		queue.waiters = make(map[uint64][]chan struct{})
	}
	waiter := make(chan struct{})
	queue.waiters[target] = append(queue.waiters[target], waiter)
	queue.mu.Unlock()

	select {
	case <-waiter:
		return nil
	case <-ctx.Done():
		queue.mu.Lock()
		completed := queue.appliedSeq >= target
		if !completed {
			queue.removeWaiterLocked(target, waiter)
		}
		queue.mu.Unlock()
		if completed {
			return nil
		}
		return ctx.Err()
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
		queue.finishBatch(terminalHistoryIngestBatchCompleteSeq(batch))
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

func (queue *terminalHistoryIngestQueue) finishBatch(seq uint64) {
	queue.mu.Lock()
	if seq > queue.appliedSeq {
		queue.appliedSeq = seq
	}
	queue.inFlight = false
	for target, waiters := range queue.waiters {
		if target <= queue.appliedSeq {
			for _, waiter := range waiters {
				close(waiter)
			}
			delete(queue.waiters, target)
		}
	}
	queue.mu.Unlock()
}

func terminalHistoryIngestBatchCompleteSeq(batch []terminalHistoryIngestItem) uint64 {
	for i := len(batch) - 1; i >= 0; i-- {
		if batch[i].seq != 0 {
			return batch[i].seq
		}
	}
	return 0
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

func (queue *terminalHistoryIngestQueue) removeWaiterLocked(target uint64, waiter chan struct{}) {
	waiters := queue.waiters[target]
	for index, candidate := range waiters {
		if candidate != waiter {
			continue
		}
		copy(waiters[index:], waiters[index+1:])
		waiters[len(waiters)-1] = nil
		waiters = waiters[:len(waiters)-1]
		break
	}
	if len(waiters) == 0 {
		delete(queue.waiters, target)
		return
	}
	queue.waiters[target] = waiters
}

// Status 返回 history backlog 的已应用/目标序号诊断。
// 调用方只能用它判断追平进度或 materialized projection 能力，不能据此读取或构造
// history payload；payload truth 仍只在 HistoryStore/window/copy。
func (queue *terminalHistoryIngestQueue) Status() HistoryBacklogStatus {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return HistoryBacklogStatus{
		HistoryEnabled:      true,
		AppliedSeq:          queue.appliedSeq,
		TargetSeq:           queue.targetSeq,
		CatchupPending:      queue.appliedSeq < queue.targetSeq,
		PendingTransactions: len(queue.pending),
		InFlight:            queue.inFlight,
		Closed:              queue.closed,
	}
}
