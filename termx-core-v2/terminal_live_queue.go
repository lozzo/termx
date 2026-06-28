package termxcorev2

import (
	"context"
	"strings"
	"sync"
)

// terminalLiveIngestBatchMaxBytes 是 live latest 更新的交互批次上限。
// domain owner：core live ingest queue；truth source 仍是 PTY bytes。这里按
// PTY read 粒度切批，避免压力输出被 1MB 合并成很粗的 live screen 跳变。
const terminalLiveIngestBatchMaxBytes = ptyReadBufferBytes

// terminalLiveIngestQueue 把 PTY 高频输出压成 live latest 批次。
// enqueue 不写 vterm，不持 terminal live 锁；worker 只取当前积压批次写一次 screen。
type terminalLiveIngestQueue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	pending  []terminalLiveIngestItem
	flushSeq uint64
	doneSeq  uint64
	inFlight bool
	closed   bool
	done     chan struct{}
}

type terminalLiveIngestItem struct {
	text     string
	flushSeq uint64
}

func newTerminalLiveIngestQueue() *terminalLiveIngestQueue {
	queue := &terminalLiveIngestQueue{done: make(chan struct{})}
	queue.cond = sync.NewCond(&queue.mu)
	return queue
}

func (queue *terminalLiveIngestQueue) Enqueue(text string) bool {
	if text == "" {
		return true
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.closed {
		return false
	}
	queue.pending = append(queue.pending, terminalLiveIngestItem{text: text})
	queue.cond.Signal()
	return true
}

func (queue *terminalLiveIngestQueue) Flush(ctx context.Context) error {
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
	queue.pending = append(queue.pending, terminalLiveIngestItem{flushSeq: target})
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

func (queue *terminalLiveIngestQueue) Close() {
	queue.mu.Lock()
	queue.closed = true
	queue.cond.Broadcast()
	queue.mu.Unlock()
}

func (queue *terminalLiveIngestQueue) Wait() {
	<-queue.done
}

func (queue *terminalLiveIngestQueue) Run(ingest func(string) error) {
	defer close(queue.done)
	for {
		batch, ok := queue.nextBatch()
		if !ok {
			return
		}
		if ingest != nil {
			texts := terminalLiveIngestBatchTexts(batch)
			if len(texts) > 0 {
				_ = ingest(strings.Join(texts, ""))
			}
		}
		if target, ok := terminalLiveIngestBatchFlushTarget(batch); ok {
			queue.markFlushed(target)
		}
		queue.finishBatch()
	}
}

func terminalLiveIngestBatchTexts(batch []terminalLiveIngestItem) []string {
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

func (queue *terminalLiveIngestQueue) nextBatch() ([]terminalLiveIngestItem, bool) {
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
		if count > 0 && bytes+nextBytes > terminalLiveIngestBatchMaxBytes {
			break
		}
		bytes += nextBytes
		count++
	}
	batch := append([]terminalLiveIngestItem(nil), queue.pending[:count]...)
	queue.dropPendingPrefixLocked(count)
	if len(queue.pending) > 0 {
		queue.cond.Signal()
	}
	queue.inFlight = true
	return batch, true
}

func (queue *terminalLiveIngestQueue) markFlushed(target uint64) {
	queue.mu.Lock()
	if target > queue.doneSeq {
		queue.doneSeq = target
	}
	queue.cond.Broadcast()
	queue.mu.Unlock()
}

func (queue *terminalLiveIngestQueue) finishBatch() {
	queue.mu.Lock()
	queue.inFlight = false
	queue.cond.Broadcast()
	queue.mu.Unlock()
}

func terminalLiveIngestBatchFlushTarget(batch []terminalLiveIngestItem) (uint64, bool) {
	for i := len(batch) - 1; i >= 0; i-- {
		if batch[i].flushSeq != 0 {
			return batch[i].flushSeq, true
		}
	}
	return 0, false
}

func (queue *terminalLiveIngestQueue) dropPendingPrefixLocked(count int) {
	if count <= 0 {
		return
	}
	remaining := len(queue.pending) - count
	copy(queue.pending, queue.pending[count:])
	for i := remaining; i < len(queue.pending); i++ {
		queue.pending[i] = terminalLiveIngestItem{}
	}
	if remaining == 0 {
		queue.pending = nil
		return
	}
	queue.pending = queue.pending[:remaining]
}
