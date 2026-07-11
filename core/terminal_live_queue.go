package core

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lozzow/termx/shared/perftrace"
)

// terminalLiveIngestBatchMaxBytes 是 PTY 输出消费者热路径的交互批次上限。
// core 仍只维护 latest screen，不承诺补放中间 frame；这个上限只防止 PTY
// 高频输出被攒成超大块后才推进 live 或 history semantic consumer。
const terminalLiveIngestBatchMaxBytes = 16 * 1024
const terminalHistoryTapIngestBatchMaxBytes = 1024 * 1024

const terminalLiveIngestQueuePageSize = 256

// terminalLiveIngestQueueStatus 是 PTY output consumer 队列的诊断快照。
// 它描述调度层 pending 状态和背压统计；这些字段不能被当作 history
// payload truth，也不能替代 linehist 的 applied/window/cursor 边界。
type terminalLiveIngestQueueStatus struct {
	PendingItems          int
	PendingBytes          int64
	BackpressureMode      HistoryBackpressureMode
	BufferLimitBytes      int64
	BackpressureEvents    uint64
	BackpressureWaitNanos int64
	Closed                bool
}

// terminalLiveIngestQueue 把 PTY 高频输出压成 consumer 批次。
// domain owner 是 core terminal ingest；R396 后 live queue 推进 SurfaceTrack，
// history tap queue 推进 history semantic consumer。它不保存 history truth，也不
// 解释 PTY bytes，只提供当前已入队 payload 的 flush fence。
// 中文说明：pending 使用固定页链表而不是 slice 队列。history consumer 慢时可能积压
// 很多 PTY payload；reader 给 history tap 入队不能在锁内做大 slice 扩容或前移复制，
// 否则会间接拖住后续 PTY read，从而影响 live SurfaceTrack 看到最新输出。
type terminalLiveIngestQueue struct {
	mu                    sync.Mutex
	cond                  *sync.Cond
	head                  *terminalLiveIngestPage
	tail                  *terminalLiveIngestPage
	pendingCount          int
	pendingBytes          int64
	closed                bool
	enqueuedSeq           uint64
	completedSeq          uint64
	flushWait             map[uint64][]chan struct{}
	batchMaxBytes         int
	backpressure          HistoryBackpressureConfig
	backpressureEvents    uint64
	backpressureWaitNanos int64
	done                  chan struct{}
}

type terminalLiveIngestItem struct {
	text string
	seq  uint64
}

type terminalLiveIngestPage struct {
	items [terminalLiveIngestQueuePageSize]terminalLiveIngestItem
	start int
	end   int
	next  *terminalLiveIngestPage
}

func newTerminalLiveIngestQueue() *terminalLiveIngestQueue {
	return newTerminalLiveIngestQueueWithBatchLimit(terminalLiveIngestBatchMaxBytes)
}

func newTerminalHistoryTapIngestQueue(backpressure ...HistoryBackpressureConfig) *terminalLiveIngestQueue {
	queue := newTerminalLiveIngestQueueWithBatchLimit(terminalHistoryTapIngestBatchMaxBytes)
	if len(backpressure) > 0 {
		queue.backpressure = backpressure[0].Normalize()
	}
	return queue
}

func newTerminalLiveIngestQueueWithBatchLimit(batchMaxBytes int) *terminalLiveIngestQueue {
	if batchMaxBytes <= 0 {
		batchMaxBytes = terminalLiveIngestBatchMaxBytes
	}
	queue := &terminalLiveIngestQueue{
		batchMaxBytes: batchMaxBytes,
		backpressure: HistoryBackpressureConfig{
			Mode:        HistoryBackpressureLowLatency,
			BufferBytes: DefaultHistoryBackpressureBufferBytes,
		}.Normalize(),
		done: make(chan struct{}),
	}
	queue.cond = sync.NewCond(&queue.mu)
	return queue
}

func (queue *terminalLiveIngestQueue) Enqueue(text string) bool {
	if text == "" {
		return true
	}
	for len(text) > 0 {
		head := text
		tail := ""
		if queue.backpressure.Mode == HistoryBackpressureBounded && queue.backpressure.BufferBytes > 0 && int64(len(text)) > queue.backpressure.BufferBytes {
			head, tail = splitLiveIngestPayload(text, int(queue.backpressure.BufferBytes))
		}
		if !queue.enqueueOne(head) {
			return false
		}
		text = tail
	}
	return true
}

func (queue *terminalLiveIngestQueue) enqueueOne(text string) bool {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	startedWaiting := time.Time{}
	for queue.shouldWaitForBackpressureLocked(len(text)) && !queue.closed {
		if startedWaiting.IsZero() {
			startedWaiting = time.Now()
			queue.backpressureEvents++
		}
		queue.cond.Wait()
	}
	if !startedWaiting.IsZero() {
		queue.backpressureWaitNanos += time.Since(startedWaiting).Nanoseconds()
	}
	if queue.closed {
		return false
	}
	queue.enqueuedSeq++
	queue.pushPendingLocked(terminalLiveIngestItem{text: text, seq: queue.enqueuedSeq})
	queue.cond.Signal()
	return true
}

func (queue *terminalLiveIngestQueue) shouldWaitForBackpressureLocked(nextBytes int) bool {
	if queue.backpressure.Mode != HistoryBackpressureBounded || nextBytes <= 0 {
		return false
	}
	limit := queue.backpressure.BufferBytes
	if limit <= 0 {
		return false
	}
	return queue.pendingBytes+int64(nextBytes) > limit
}

// Status 返回队列调度诊断快照。pending bytes 只表示尚未交给对应 owner
// 处理的 PTY payload 驻留，不表示 history 已落盘或可查询。
func (queue *terminalLiveIngestQueue) Status() terminalLiveIngestQueueStatus {
	if queue == nil {
		return terminalLiveIngestQueueStatus{}
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return terminalLiveIngestQueueStatus{
		PendingItems:          queue.pendingCount,
		PendingBytes:          queue.pendingBytes,
		BackpressureMode:      queue.backpressure.Mode,
		BufferLimitBytes:      queue.backpressure.BufferBytes,
		BackpressureEvents:    queue.backpressureEvents,
		BackpressureWaitNanos: queue.backpressureWaitNanos,
		Closed:                queue.closed,
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

// Flush 等待调用时已经进入该 consumer 队列的 PTY payload 全部推进对应 owner。
// 它只建立输入顺序 fence，不等待客户端 render，也不会把未来输出纳入当前等待范围。
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
	for queue.pendingLenLocked() == 0 && !queue.closed {
		queue.cond.Wait()
	}
	if queue.pendingLenLocked() == 0 && queue.closed {
		return nil, 0, false
	}
	count := 0
	bytes := 0
	for page, index := queue.head, 0; page != nil && count < queue.pendingLenLocked(); {
		item := &page.items[page.start+index]
		nextBytes := len(item.text)
		if bytes == 0 && nextBytes > queue.batchMaxBytes {
			head, tail := splitLiveIngestPayload(item.text, queue.batchMaxBytes)
			item.text = tail
			queue.pendingBytes -= int64(len(head))
			queue.cond.Broadcast()
			return []string{head}, 0, true
		}
		if count > 0 && bytes+nextBytes > queue.batchMaxBytes {
			break
		}
		bytes += nextBytes
		count++
		index++
		if page.start+index >= page.end {
			page = page.next
			index = 0
		}
	}
	batch, completeSeq := queue.pendingBatchLocked(count)
	queue.dropPendingPrefixLocked(count)
	if queue.pendingLenLocked() > 0 {
		queue.cond.Signal()
	}
	queue.cond.Broadcast()
	return batch, completeSeq, true
}

func (queue *terminalLiveIngestQueue) pendingLenLocked() int {
	return queue.pendingCount
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
	for count > 0 && queue.head != nil {
		page := queue.head
		available := page.end - page.start
		if available <= 0 {
			queue.head = page.next
			if queue.head == nil {
				queue.tail = nil
			}
			continue
		}
		take := count
		if take > available {
			take = available
		}
		for index := 0; index < take; index++ {
			queue.pendingBytes -= int64(len(page.items[page.start+index].text))
			page.items[page.start+index] = terminalLiveIngestItem{}
		}
		page.start += take
		queue.pendingCount -= take
		count -= take
		if page.start == page.end {
			queue.head = page.next
			if queue.head == nil {
				queue.tail = nil
			}
		}
	}
	if queue.pendingCount < 0 {
		queue.pendingCount = 0
	}
}

func (queue *terminalLiveIngestQueue) pushPendingLocked(item terminalLiveIngestItem) {
	if queue.tail == nil || queue.tail.end == len(queue.tail.items) {
		page := &terminalLiveIngestPage{}
		if queue.tail == nil {
			queue.head = page
			queue.tail = page
		} else {
			queue.tail.next = page
			queue.tail = page
		}
	}
	queue.tail.items[queue.tail.end] = item
	queue.tail.end++
	queue.pendingCount++
	queue.pendingBytes += int64(len(item.text))
}

func (queue *terminalLiveIngestQueue) pendingBatchLocked(count int) ([]string, uint64) {
	if count <= 0 {
		return nil, 0
	}
	batch := make([]string, 0, count)
	var completeSeq uint64
	for page, remaining := queue.head, count; page != nil && remaining > 0; page = page.next {
		for index := page.start; index < page.end && remaining > 0; index++ {
			item := page.items[index]
			batch = append(batch, item.text)
			if item.seq != 0 {
				completeSeq = item.seq
			}
			remaining--
		}
	}
	return batch, completeSeq
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
