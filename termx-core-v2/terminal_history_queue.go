package termxcorev2

import (
	"context"
	"sync"
)

const terminalHistoryIngestBatchMaxBytes = 1024 * 1024

// terminalHistoryIngestQueue 让真实 PTY 输出和历史解析解耦。
// enqueue 只追加内存批次并唤醒 worker，不因 history 慢解析反压 live surface。
type terminalHistoryIngestQueue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	pending  []terminalHistoryIngestItem
	flushSeq uint64
	doneSeq  uint64
	inFlight bool
	closed   bool
	done     chan struct{}
}

type terminalHistoryIngestItem struct {
	text     string
	batch    *terminalSemanticBatch
	flushSeq uint64
}

func newTerminalHistoryIngestQueue() *terminalHistoryIngestQueue {
	queue := &terminalHistoryIngestQueue{done: make(chan struct{})}
	queue.cond = sync.NewCond(&queue.mu)
	return queue
}

func (queue *terminalHistoryIngestQueue) Enqueue(text string) bool {
	if text == "" {
		return true
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.closed {
		return false
	}
	queue.pending = append(queue.pending, terminalHistoryIngestItem{text: text})
	queue.cond.Signal()
	return true
}

func (queue *terminalHistoryIngestQueue) EnqueueBatch(batch terminalSemanticBatch) bool {
	if batch.Raw == "" && len(batch.Damages) == 0 && len(batch.AltExitFrame) == 0 {
		return true
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.closed {
		return false
	}
	batchCopy := batch
	queue.pending = append(queue.pending, terminalHistoryIngestItem{batch: &batchCopy})
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

func (queue *terminalHistoryIngestQueue) Run(ingest func([]string) error, ingestBatches ...func([]terminalSemanticBatch) error) {
	defer close(queue.done)
	var ingestBatch func([]terminalSemanticBatch) error
	if len(ingestBatches) > 0 {
		ingestBatch = ingestBatches[0]
	}
	for {
		batch, ok := queue.nextBatch()
		if !ok {
			return
		}
		if ingest != nil {
			// 不再把同一批输入拼成一个大字符串，避免 daemon 在高吞吐日志下制造短生命周期大对象。
			texts := terminalHistoryIngestBatchTexts(batch)
			if len(texts) > 0 {
				_ = ingest(texts)
			}
		}
		if ingestBatch != nil {
			batches := terminalHistoryIngestSemanticBatches(batch)
			if len(batches) > 0 {
				_ = ingestBatch(batches)
			}
		}
		if target, ok := terminalHistoryIngestBatchFlushTarget(batch); ok {
			queue.markFlushed(target)
		}
		queue.finishBatch()
		// 中文说明：history 批次已经写入 LogicalLineStore/file backend 后，
		// 归还解析/编码临时页；不删除 history truth，也不做后台定时 scrub。
		maybeReclaimDaemonBoundaryHeap()
	}
}

func terminalHistoryIngestSemanticBatches(batch []terminalHistoryIngestItem) []terminalSemanticBatch {
	if len(batch) == 0 {
		return nil
	}
	batches := make([]terminalSemanticBatch, 0, len(batch))
	for _, item := range batch {
		if item.batch != nil {
			batches = append(batches, *item.batch)
		}
	}
	return batches
}

func terminalHistoryIngestBatchTexts(batch []terminalHistoryIngestItem) []string {
	if len(batch) == 0 {
		return nil
	}
	texts := make([]string, 0, len(batch))
	for _, item := range batch {
		if item.text != "" {
			texts = append(texts, item.text)
		}
	}
	return texts
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
	bytes := 0
	for count < len(queue.pending) {
		if queue.pending[count].flushSeq != 0 {
			count++
			break
		}
		nextBytes := len(queue.pending[count].text)
		if count > 0 && bytes+nextBytes > terminalHistoryIngestBatchMaxBytes {
			break
		}
		bytes += nextBytes
		count++
	}
	batch := append([]terminalHistoryIngestItem(nil), queue.pending[:count]...)
	queue.dropPendingPrefixLocked(count)
	if len(queue.pending) > 0 {
		queue.cond.Signal()
	}
	queue.inFlight = true
	return batch, true
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
