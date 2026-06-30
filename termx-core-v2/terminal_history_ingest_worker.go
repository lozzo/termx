package termxcorev2

import (
	"context"
	"io"
	"os"
	"sync"
)

const terminalHistoryIngestBatchMaxBytes = 1024 * 1024

// terminalHistoryIngestQueue 是 R358 留下的 legacy raw PTY history backlog。
// R372 后它只能作为 R373 替换前的隔离对象：当前失败条件是它仍会把 raw text
// 交给独立 history SemanticSource 追平，不能被包装成新的 single-tap 队列或
// history truth。R373 必须把 backlog 单位切到 tap 产出的 semantic transaction。
type terminalHistoryIngestQueue struct {
	mu        sync.Mutex
	cond      *sync.Cond
	pending   []terminalHistoryIngestItem
	spool     *terminalHistoryIngestSpool
	flushSeq  uint64
	doneSeq   uint64
	inFlight  bool
	closed    bool
	done      chan struct{}
	closeOnce sync.Once
}

type terminalHistoryIngestItem struct {
	offset   int64
	length   int
	text     string
	flushSeq uint64
	err      error
}

func newTerminalHistoryIngestQueue() *terminalHistoryIngestQueue {
	queue := &terminalHistoryIngestQueue{
		spool: newTerminalHistoryIngestSpool(),
		done:  make(chan struct{}),
	}
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
	item := queue.spool.append(text)
	queue.pending = append(queue.pending, item)
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

func (queue *terminalHistoryIngestQueue) Run(ingest func([]string) error) {
	defer func() {
		queue.closeOnce.Do(func() {
			if queue.spool != nil {
				queue.spool.close()
			}
		})
		close(queue.done)
	}()
	for {
		batch, ok := queue.nextBatch()
		if !ok {
			return
		}
		if ingest != nil {
			texts := terminalHistoryIngestBatchTexts(batch)
			if len(texts) > 0 {
				_ = ingest(texts)
			}
		}
		if target, ok := terminalHistoryIngestBatchFlushTarget(batch); ok {
			queue.markFlushed(target)
		}
		queue.finishBatch()
		// 中文说明：history 批次已经交给 store/backend 后才尝试归还临时页；
		// 这里不删除 history truth，也不是后台定时 scrub。
		maybeReclaimDaemonBoundaryHeap()
	}
}

func terminalHistoryIngestBatchTexts(batch []terminalHistoryIngestItem) []string {
	if len(batch) == 0 {
		return nil
	}
	texts := make([]string, 0, len(batch))
	for _, item := range batch {
		if item.err != nil {
			continue
		}
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
		nextBytes := queue.pending[count].length
		if count > 0 && bytes+nextBytes > terminalHistoryIngestBatchMaxBytes {
			break
		}
		bytes += nextBytes
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
	copy(batch, items)
	for i := range batch {
		if batch[i].length > 0 {
			text, err := queue.spool.read(batch[i])
			if err != nil {
				batch[i].err = err
				continue
			}
			// 中文说明：spool 只在 batch 出队时短暂还原字符串；pending 队列本身只持有
			// offset/length，避免 history worker 落后时把 PTY payload 常驻 Go heap。
			batch[i].text = text
		}
	}
	return batch
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

// terminalHistoryIngestSpool 是 history worker 的 raw PTY backlog 驻留边界。
// domain owner 是 history ingest queue：spool 只暂存尚未被 semantic source 消费的
// 原始 PTY bytes，不解释内容、不生成 history truth，也不参与 live latest path。
type terminalHistoryIngestSpool struct {
	file   *os.File
	offset int64
	err    error
}

func newTerminalHistoryIngestSpool() *terminalHistoryIngestSpool {
	file, err := os.CreateTemp("", "termx-history-ingest-*.spool")
	return &terminalHistoryIngestSpool{file: file, err: err}
}

func (spool *terminalHistoryIngestSpool) append(text string) terminalHistoryIngestItem {
	item := terminalHistoryIngestItem{length: len(text)}
	if spool == nil || spool.err != nil || spool.file == nil {
		item.err = spoolError(spool)
		return item
	}
	offset := spool.offset
	n, err := io.WriteString(spool.file, text)
	if err != nil {
		spool.err = err
		item.err = err
		return item
	}
	spool.offset += int64(n)
	item.offset = offset
	item.length = n
	return item
}

func (spool *terminalHistoryIngestSpool) read(item terminalHistoryIngestItem) (string, error) {
	if item.length == 0 {
		return "", item.err
	}
	if item.err != nil {
		return "", item.err
	}
	if spool == nil || spool.file == nil {
		return "", spoolError(spool)
	}
	buf := make([]byte, item.length)
	if _, err := spool.file.ReadAt(buf, item.offset); err != nil {
		return "", err
	}
	return string(buf), nil
}

func (spool *terminalHistoryIngestSpool) close() {
	if spool == nil || spool.file == nil {
		return
	}
	name := spool.file.Name()
	_ = spool.file.Close()
	_ = os.Remove(name)
	spool.file = nil
}

func spoolError(spool *terminalHistoryIngestSpool) error {
	if spool == nil {
		return os.ErrInvalid
	}
	if spool.err != nil {
		return spool.err
	}
	return os.ErrInvalid
}
