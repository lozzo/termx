package termxcorev2

import (
	"context"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/lozzow/termx/termx-shared/perftrace"
)

// terminalLiveIngestBatchMaxBytes 是 SemanticTap 热路径的交互批次上限。
// core 仍只维护 latest screen，不承诺补放中间 frame；这个上限只防止 PTY
// 高频输出被攒成超大块后才推进唯一 vterm owner 和 live invalidation。
const terminalLiveIngestBatchMaxBytes = 16 * 1024

// terminalLiveIngestQueue 把 PTY 高频输出压成 single SemanticTap 批次。
// enqueue 不写 vterm；worker 只取当前积压批次推进一次 tap，tap result 再 fan-out
// live latest invalidation 和 history transaction。
type terminalLiveIngestQueue struct {
	mu           sync.Mutex
	cond         *sync.Cond
	pending      []terminalLiveIngestItem
	closed       bool
	enqueuedSeq  uint64
	completedSeq uint64
	flushWait    map[uint64][]chan struct{}
	done         chan struct{}
}

type terminalLiveIngestItem struct {
	text string
	seq  uint64
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
	queue.enqueuedSeq++
	queue.pending = append(queue.pending, terminalLiveIngestItem{text: text, seq: queue.enqueuedSeq})
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

// Flush 等待调用时已经进入 live/tap 队列的 PTY payload 全部推进 single SemanticTap。
// 它只建立 tap 输入顺序 fence，不等待客户端 render，也不会把未来输出纳入当前等待范围。
func (queue *terminalLiveIngestQueue) Flush(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	queue.mu.Lock()
	target := queue.enqueuedSeq
	if target == 0 || queue.completedSeq >= target {
		queue.mu.Unlock()
		return nil
	}
	if queue.flushWait == nil {
		queue.flushWait = make(map[uint64][]chan struct{})
	}
	waiter := make(chan struct{})
	queue.flushWait[target] = append(queue.flushWait[target], waiter)
	queue.mu.Unlock()

	select {
	case <-waiter:
		return nil
	case <-ctx.Done():
		queue.mu.Lock()
		completed := queue.completedSeq >= target
		if !completed {
			queue.removeFlushWaiterLocked(target, waiter)
		}
		queue.mu.Unlock()
		if completed {
			return nil
		}
		return ctx.Err()
	}
}

func (queue *terminalLiveIngestQueue) Run(ingest func(string) error) {
	defer close(queue.done)
	for {
		batch, completeSeq, ok := queue.nextBatchWithSeq()
		if !ok {
			return
		}
		perftrace.Count("core.live.queue_batch", liveIngestBatchBytes(batch))
		if ingest != nil {
			_ = ingest(strings.Join(batch, ""))
		}
		queue.completeBatch(completeSeq)
	}
}

func (queue *terminalLiveIngestQueue) nextBatch() ([]string, bool) {
	batch, _, ok := queue.nextBatchWithSeq()
	return batch, ok
}

func (queue *terminalLiveIngestQueue) nextBatchWithSeq() ([]string, uint64, bool) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	for len(queue.pending) == 0 && !queue.closed {
		queue.cond.Wait()
	}
	if len(queue.pending) == 0 && queue.closed {
		return nil, 0, false
	}
	count := 0
	bytes := 0
	for count < len(queue.pending) {
		nextBytes := len(queue.pending[count].text)
		if bytes == 0 && nextBytes > terminalLiveIngestBatchMaxBytes {
			head, tail := splitLiveIngestPayload(queue.pending[count].text, terminalLiveIngestBatchMaxBytes)
			queue.pending[count].text = tail
			return []string{head}, 0, true
		}
		if count > 0 && bytes+nextBytes > terminalLiveIngestBatchMaxBytes {
			break
		}
		bytes += nextBytes
		count++
	}
	items := append([]terminalLiveIngestItem(nil), queue.pending[:count]...)
	batch := make([]string, len(items))
	for index, item := range items {
		batch[index] = item.text
	}
	completeSeq := items[len(items)-1].seq
	queue.dropPendingPrefixLocked(count)
	if len(queue.pending) > 0 {
		queue.cond.Signal()
	}
	return batch, completeSeq, true
}

func splitLiveIngestPayload(text string, limit int) (string, string) {
	if limit <= 0 || len(text) <= limit {
		return text, ""
	}
	split := liveIngestSplitOffset(text, limit)
	return text[:split], text[split:]
}

func liveIngestBatchBytes(batch []string) int {
	total := 0
	for _, text := range batch {
		total += len(text)
	}
	return total
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
		queue.pending[i] = terminalLiveIngestItem{}
	}
	if remaining == 0 {
		queue.pending = nil
		return
	}
	queue.pending = queue.pending[:remaining]
}

func (queue *terminalLiveIngestQueue) completeBatch(seq uint64) {
	if seq == 0 {
		return
	}
	queue.mu.Lock()
	if seq > queue.completedSeq {
		queue.completedSeq = seq
	}
	for target, waiters := range queue.flushWait {
		if target <= queue.completedSeq {
			for _, waiter := range waiters {
				close(waiter)
			}
			delete(queue.flushWait, target)
		}
	}
	queue.mu.Unlock()
}

func (queue *terminalLiveIngestQueue) removeFlushWaiterLocked(target uint64, waiter chan struct{}) {
	waiters := queue.flushWait[target]
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
		delete(queue.flushWait, target)
		return
	}
	queue.flushWait[target] = waiters
}
