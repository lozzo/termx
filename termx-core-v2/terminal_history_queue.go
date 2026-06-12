package termxcorev2

import (
	"strings"
	"sync"
)

const terminalHistoryIngestBatchMaxBytes = 1024 * 1024

// terminalHistoryIngestQueue 让真实 PTY 输出和历史解析解耦。
// enqueue 只追加内存批次并唤醒 worker，不因 history 慢解析反压 live surface。
type terminalHistoryIngestQueue struct {
	mu      sync.Mutex
	cond    *sync.Cond
	pending []string
	closed  bool
	done    chan struct{}
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
	queue.pending = append(queue.pending, text)
	queue.cond.Signal()
	return true
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
		if ingest != nil {
			_ = ingest(strings.Join(batch, ""))
		}
	}
}

func (queue *terminalHistoryIngestQueue) nextBatch() ([]string, bool) {
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
		if count > 0 && bytes+nextBytes > terminalHistoryIngestBatchMaxBytes {
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
