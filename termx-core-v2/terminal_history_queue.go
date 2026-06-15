package termxcorev2

import (
	"context"
	"strings"
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

func (queue *terminalHistoryIngestQueue) Run(ingest func(string) error) {
	defer close(queue.done)
	for {
		batch, ok := queue.nextBatch()
		if !ok {
			return
		}
		text := joinTerminalHistoryIngestBatch(batch)
		if ingest != nil && text != "" {
			_ = ingest(text)
		}
		if target, ok := terminalHistoryIngestBatchFlushTarget(batch); ok {
			queue.markFlushed(target)
		}
		queue.finishBatch()
	}
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
	copy(queue.pending, queue.pending[count:])
	queue.pending = queue.pending[:len(queue.pending)-count]
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

func joinTerminalHistoryIngestBatch(batch []terminalHistoryIngestItem) string {
	if len(batch) == 0 {
		return ""
	}
	if len(batch) == 1 {
		return batch[0].text
	}
	var builder strings.Builder
	for _, part := range batch {
		builder.WriteString(part.text)
	}
	return builder.String()
}
