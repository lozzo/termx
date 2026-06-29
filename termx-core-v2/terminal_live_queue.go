package termxcorev2

import (
	"strings"
	"sync"
	"unicode/utf8"
)

// terminalLiveIngestBatchMaxBytes 是 live native screen 的交互批次上限。
// core 仍只维护 latest screen，不承诺补放中间 frame；这个上限只防止 PTY
// 高频输出被攒成超大块后才推进 vterm 和 live invalidation。
const terminalLiveIngestBatchMaxBytes = 16 * 1024

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
		if bytes == 0 && nextBytes > terminalLiveIngestBatchMaxBytes {
			head, tail := splitLiveIngestPayload(queue.pending[count], terminalLiveIngestBatchMaxBytes)
			queue.pending[count] = tail
			return []string{head}, true
		}
		if count > 0 && bytes+nextBytes > terminalLiveIngestBatchMaxBytes {
			break
		}
		bytes += nextBytes
		count++
	}
	batch := append([]string(nil), queue.pending[:count]...)
	queue.dropPendingPrefixLocked(count)
	if len(queue.pending) > 0 {
		queue.cond.Signal()
	}
	return batch, true
}

func splitLiveIngestPayload(text string, limit int) (string, string) {
	if limit <= 0 || len(text) <= limit {
		return text, ""
	}
	split := liveIngestSplitOffset(text, limit)
	return text[:split], text[split:]
}

func liveIngestSplitOffset(text string, limit int) int {
	if limit <= 0 || len(text) <= limit {
		return len(text)
	}
	if newline := strings.LastIndexByte(text[:limit], '\n'); newline >= 0 {
		return newline + 1
	}
	split := limit
	for split > 0 && !utf8.RuneStart(text[split]) {
		split--
	}
	if split == 0 {
		return limit
	}
	return split
}

func (queue *terminalLiveIngestQueue) dropPendingPrefixLocked(count int) {
	if count <= 0 {
		return
	}
	remaining := len(queue.pending) - count
	copy(queue.pending, queue.pending[count:])
	for i := remaining; i < len(queue.pending); i++ {
		queue.pending[i] = ""
	}
	if remaining == 0 {
		queue.pending = nil
		return
	}
	queue.pending = queue.pending[:remaining]
}
