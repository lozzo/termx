package termxcorev2

import (
	"strings"
	"sync"
)

const terminalLiveIngestBatchMaxBytes = 1024 * 1024

// terminalLiveIngestQueue 把 PTY 高频输出压成 live latest 批次。
// enqueue 不写 vterm，不持 terminal live 锁；worker 只取当前积压批次写一次 screen。
type terminalLiveIngestQueue struct {
	mu      sync.Mutex
	cond    *sync.Cond
	pending []string
	closed  bool
	done    chan struct{}
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
	queue.pending = append(queue.pending, text)
	queue.cond.Signal()
	return true
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
			_ = ingest(strings.Join(batch, ""))
		}
	}
}

func (queue *terminalLiveIngestQueue) nextBatch() ([]string, bool) {
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
		nextBytes := len(queue.pending[count])
		if count > 0 && bytes+nextBytes > terminalLiveIngestBatchMaxBytes {
			break
		}
		bytes += nextBytes
		count++
	}
	batch := append([]string(nil), queue.pending[:count]...)
	copy(queue.pending, queue.pending[count:])
	queue.pending = queue.pending[:len(queue.pending)-count]
	if len(queue.pending) > 0 {
		queue.cond.Signal()
	}
	return batch, true
}
