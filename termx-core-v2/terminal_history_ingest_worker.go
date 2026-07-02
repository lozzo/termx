package termxcorev2

import (
	"context"
	"sync"

	"github.com/lozzow/termx/termx-core-v2/history"
)

const terminalHistoryIngestBatchMaxJournals = 1024
const terminalHistoryIngestQueuePageSize = 256

// terminalHistoryIngestQueue 是 SemanticTap 之后的 compact history journal backlog。
// domain owner 是 core terminal ingest：queue 只接收同一次 semantic pass 裁剪出的
// HistoryJournal 副本，不保存 raw PTY bytes、不保存 full transaction、不启动第二个
// vterm，也不解释 history truth。失败条件是任何 consumer 绕过 tap 往这里塞 raw
// PTY replay 或把 queue 当作第二份 HistoryStore。
type terminalHistoryIngestQueue struct {
	mu         sync.Mutex
	cond       *sync.Cond
	head       *terminalHistoryIngestPage
	tail       *terminalHistoryIngestPage
	pendingLen int
	targetSeq  uint64
	appliedSeq uint64
	waiters    map[uint64][]chan struct{}
	// decisionFence 只记录 backlog 中是否存在会改变后续 classifier state 的 journal。
	// 它不保存 history payload，也不能替代 HistoryStore；用途只是让 producer 必要时 flush。
	pendingDecisionFence  bool
	inFlight              bool
	inFlightDecisionFence bool
	closed                bool
	done                  chan struct{}
	closeOnce             sync.Once
}

type terminalHistoryIngestItem struct {
	journal       history.HistoryJournal
	seq           uint64
	decisionFence bool
}

type terminalHistoryIngestPage struct {
	items [terminalHistoryIngestQueuePageSize]terminalHistoryIngestItem
	start int
	end   int
	next  *terminalHistoryIngestPage
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

func (queue *terminalHistoryIngestQueue) Enqueue(journal history.HistoryJournal) bool {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.closed {
		return false
	}
	queue.targetSeq++
	decisionFence := terminalHistoryJournalRequiresDecisionFence(journal)
	queue.pushPendingLocked(terminalHistoryIngestItem{
		journal:       history.CloneHistoryJournal(journal),
		seq:           queue.targetSeq,
		decisionFence: decisionFence,
	})
	queue.pendingDecisionFence = queue.pendingDecisionFence || decisionFence
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

func (queue *terminalHistoryIngestQueue) Run(ingest func([]history.HistoryJournal) error) {
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
			journals := terminalHistoryIngestBatchJournals(batch)
			if len(journals) > 0 {
				_ = ingest(journals)
			}
		}
		queue.finishBatch(terminalHistoryIngestBatchCompleteSeq(batch))
		// 中文说明：history journal 已经交给 store/backend 后才尝试归还临时页；
		// 这里不删除 history truth，也不是后台定时 scrub。
		maybeReclaimDaemonBoundaryHeap()
	}
}

func terminalHistoryIngestBatchJournals(batch []terminalHistoryIngestItem) []history.HistoryJournal {
	if len(batch) == 0 {
		return nil
	}
	journals := make([]history.HistoryJournal, 0, len(batch))
	for _, item := range batch {
		// 中文说明：nextBatch 已经把 queue-owned journal 副本移交给 worker；
		// 这里不再做第三次 payload deep-copy，避免 100K 普通输出 history backlog
		// 在 handoff 阶段反复复制同一 logical-line cells。
		journals = append(journals, item.journal)
	}
	return journals
}

func (queue *terminalHistoryIngestQueue) nextBatch() ([]terminalHistoryIngestItem, bool) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	for queue.pendingLen == 0 && !queue.closed {
		queue.cond.Wait()
	}
	if queue.pendingLen == 0 && queue.closed {
		return nil, false
	}
	count := queue.pendingLen
	if count > terminalHistoryIngestBatchMaxJournals {
		count = terminalHistoryIngestBatchMaxJournals
	}
	batch := queue.pendingBatchLocked(count)
	queue.dropPendingPrefixLocked(count)
	if queue.pendingLen > 0 {
		queue.cond.Signal()
	}
	queue.inFlight = true
	queue.inFlightDecisionFence = terminalHistoryIngestBatchRequiresDecisionFence(batch)
	return batch, true
}

func (queue *terminalHistoryIngestQueue) pendingBatchLocked(count int) []terminalHistoryIngestItem {
	if count <= 0 {
		return nil
	}
	batch := make([]terminalHistoryIngestItem, 0, count)
	for page, remaining := queue.head, count; page != nil && remaining > 0; page = page.next {
		for index := page.start; index < page.end && remaining > 0; index++ {
			batch = append(batch, page.items[index])
			remaining--
		}
	}
	return batch
}

func (queue *terminalHistoryIngestQueue) finishBatch(seq uint64) {
	queue.mu.Lock()
	if seq > queue.appliedSeq {
		queue.appliedSeq = seq
	}
	queue.inFlight = false
	queue.inFlightDecisionFence = false
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
			page.items[page.start+index] = terminalHistoryIngestItem{}
		}
		page.start += take
		queue.pendingLen -= take
		count -= take
		if page.start == page.end {
			queue.head = page.next
			if queue.head == nil {
				queue.tail = nil
			}
		}
	}
	if queue.pendingLen <= 0 {
		queue.pendingLen = 0
		queue.head = nil
		queue.tail = nil
		queue.pendingDecisionFence = false
		return
	}
	queue.pendingDecisionFence = queue.pendingRequiresDecisionFenceLocked()
}

func (queue *terminalHistoryIngestQueue) pushPendingLocked(item terminalHistoryIngestItem) {
	if queue.tail == nil || queue.tail.end == len(queue.tail.items) {
		page := &terminalHistoryIngestPage{}
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
	queue.pendingLen++
}

func (queue *terminalHistoryIngestQueue) pendingRequiresDecisionFenceLocked() bool {
	for page := queue.head; page != nil; page = page.next {
		for index := page.start; index < page.end; index++ {
			if page.items[index].decisionFence {
				return true
			}
		}
	}
	return false
}

func (queue *terminalHistoryIngestQueue) NeedsClassifierFlush(next history.HistoryJournal) bool {
	queue.mu.Lock()
	hasBacklog := queue.appliedSeq < queue.targetSeq
	hasDecisionFence := queue.pendingDecisionFence || queue.inFlightDecisionFence
	queue.mu.Unlock()
	if hasDecisionFence {
		return true
	}
	return hasBacklog && terminalHistoryJournalRequiresDecisionFence(next)
}

func terminalHistoryIngestBatchRequiresDecisionFence(items []terminalHistoryIngestItem) bool {
	for _, item := range items {
		if item.decisionFence {
			return true
		}
	}
	return false
}

func terminalHistoryJournalRequiresDecisionFence(journal history.HistoryJournal) bool {
	for _, item := range journal.Items {
		if item.Kind != history.HistoryJournalItemOrdinaryLineBatch {
			return true
		}
		if item.Ordinary == nil {
			continue
		}
		if item.Ordinary.OpenUpdate != nil || len(item.Ordinary.Commands) > 0 {
			return true
		}
	}
	return false
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
// 调用方只能用它判断追平进度或 history.window 内部调度，不能据此读取或构造
// history payload；PendingTransactions 是既有协议字段名，R385 后计数的是 backlog
// journal。payload truth 仍只在 HistoryStore/window/copy。
func (queue *terminalHistoryIngestQueue) Status() HistoryBacklogStatus {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return HistoryBacklogStatus{
		HistoryEnabled:      true,
		AppliedSeq:          queue.appliedSeq,
		TargetSeq:           queue.targetSeq,
		CatchupPending:      queue.appliedSeq < queue.targetSeq,
		PendingTransactions: queue.pendingLen,
		InFlight:            queue.inFlight,
		Closed:              queue.closed,
	}
}
