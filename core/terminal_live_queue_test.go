package core

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTerminalLiveIngestQueueEnqueueDoesNotWaitForSlowIngest(t *testing.T) {
	queue := newTerminalLiveIngestQueue()
	started := make(chan struct{})
	release := make(chan struct{})
	go queue.Run(func(string) error {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	})
	if !queue.Enqueue("one") {
		t.Fatal("expected first enqueue")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for worker")
	}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			if !queue.Enqueue("more") {
				t.Errorf("enqueue rejected before close")
				break
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("enqueue should not wait for slow live ingest")
	}
	close(release)
	queue.Close()
	queue.Wait()
}

func TestTerminalLiveIngestQueueSplitsLargePendingBatch(t *testing.T) {
	queue := newTerminalLiveIngestQueue()
	chunk := strings.Repeat("x", terminalLiveIngestBatchMaxBytes/2+1)
	for _, payload := range []string{chunk, chunk, "tail"} {
		if !queue.Enqueue(payload) {
			t.Fatal("expected enqueue before close")
		}
	}
	first, ok := queue.nextBatch()
	if !ok || len(first) != 1 {
		t.Fatalf("expected first capped batch with one chunk, got batch=%d ok=%v", len(first), ok)
	}
	second, ok := queue.nextBatch()
	if !ok || len(second) != 2 {
		t.Fatalf("expected second capped batch with remaining chunks, got batch=%d ok=%v", len(second), ok)
	}
	queue.Close()
	third, ok := queue.nextBatch()
	if ok || third != nil {
		t.Fatalf("expected closed queue to drain, got batch=%#v ok=%v", third, ok)
	}
	close(queue.done)
}

func TestTerminalLiveIngestQueueUsesInteractiveBatchLimit(t *testing.T) {
	if terminalLiveIngestBatchMaxBytes > ptyReadBufferBytes {
		t.Fatalf("live batch limit must not exceed PTY read chunk, got batch=%d pty=%d", terminalLiveIngestBatchMaxBytes, ptyReadBufferBytes)
	}
	queue := newTerminalLiveIngestQueue()
	chunk := strings.Repeat("x", terminalLiveIngestBatchMaxBytes)
	for i := 0; i < 3; i++ {
		if !queue.Enqueue(chunk) {
			t.Fatal("expected enqueue before close")
		}
	}

	batches := 0
	totalBytes := 0
	for {
		batch, ok := queue.nextBatch()
		if !ok {
			t.Fatal("queue closed before pending chunks drained")
		}
		batches++
		batchBytes := 0
		for _, item := range batch {
			batchBytes += len(item)
		}
		if batchBytes > terminalLiveIngestBatchMaxBytes {
			t.Fatalf("live batch exceeded interactive limit: got %d limit %d", batchBytes, terminalLiveIngestBatchMaxBytes)
		}
		totalBytes += batchBytes
		if totalBytes == len(chunk)*3 {
			break
		}
	}
	if batches != 3 {
		t.Fatalf("PTY-sized stress chunks must not be merged into larger live batches, got %d", batches)
	}
	close(queue.done)
}

func TestR407HistoryTapQueueUsesLargeSemanticBatchWithoutChangingLiveLimit(t *testing.T) {
	if terminalHistoryTapIngestBatchMaxBytes <= terminalLiveIngestBatchMaxBytes {
		t.Fatalf("history tap batch must be larger than live interactive batch, history=%d live=%d", terminalHistoryTapIngestBatchMaxBytes, terminalLiveIngestBatchMaxBytes)
	}
	liveQueue := newTerminalLiveIngestQueue()
	historyQueue := newTerminalHistoryTapIngestQueue()
	chunk := strings.Repeat("x", terminalLiveIngestBatchMaxBytes)
	for i := 0; i < 8; i++ {
		if !liveQueue.Enqueue(chunk) || !historyQueue.Enqueue(chunk) {
			t.Fatal("expected enqueue before close")
		}
	}
	liveBatch, ok := liveQueue.nextBatch()
	if !ok {
		t.Fatal("expected live batch")
	}
	if got := liveIngestBatchBytes(liveBatch); got > terminalLiveIngestBatchMaxBytes {
		t.Fatalf("live queue must keep interactive batch limit, got %d", got)
	}
	historyBatch, ok := historyQueue.nextBatch()
	if !ok {
		t.Fatal("expected history tap batch")
	}
	if got := liveIngestBatchBytes(historyBatch); got != len(chunk)*8 {
		t.Fatalf("history tap queue should coalesce semantic backlog into a larger batch, got %d want %d", got, len(chunk)*8)
	}
	close(liveQueue.done)
	close(historyQueue.done)
}

func TestR446HistoryTapQueueStatusTracksPendingBytesAndBackpressureConfig(t *testing.T) {
	queue := newTerminalHistoryTapIngestQueue(HistoryBackpressureConfig{
		Mode:        HistoryBackpressureBounded,
		BufferBytes: 64,
	})
	if !queue.Enqueue("abcd") || !queue.Enqueue("ef") {
		t.Fatal("expected enqueue before close")
	}
	status := queue.Status()
	if status.PendingItems != 2 || status.PendingBytes != 6 {
		t.Fatalf("unexpected pending status %#v", status)
	}
	if status.BackpressureMode != HistoryBackpressureBounded || status.BufferLimitBytes != 64 {
		t.Fatalf("history tap queue lost backpressure config: %#v", status)
	}
	batch, ok := queue.nextBatch()
	if !ok || strings.Join(batch, "") != "abcdef" {
		t.Fatalf("unexpected batch %#v ok=%v", batch, ok)
	}
	if status = queue.Status(); status.PendingItems != 0 || status.PendingBytes != 0 {
		t.Fatalf("pending bytes must drop after dequeue, got %#v", status)
	}
	close(queue.done)
}

func TestR446QueueStatusTracksPendingBytesAcrossSplitHead(t *testing.T) {
	queue := newTerminalLiveIngestQueueWithBatchLimit(4)
	if !queue.Enqueue("abcdef") {
		t.Fatal("expected enqueue before close")
	}
	if status := queue.Status(); status.PendingBytes != 6 {
		t.Fatalf("expected full pending bytes before split, got %#v", status)
	}
	batch, ok := queue.nextBatch()
	if !ok || strings.Join(batch, "") != "abcd" {
		t.Fatalf("unexpected split batch %#v ok=%v", batch, ok)
	}
	if status := queue.Status(); status.PendingBytes != 2 || status.PendingItems != 1 {
		t.Fatalf("split head should leave only tail pending, got %#v", status)
	}
	close(queue.done)
}

func TestR447BoundedHistoryTapQueueBackpressuresUntilPendingBytesDrain(t *testing.T) {
	queue := newTerminalHistoryTapIngestQueue(HistoryBackpressureConfig{
		Mode:        HistoryBackpressureBounded,
		BufferBytes: 8,
	})
	if !queue.Enqueue("12345678") {
		t.Fatal("expected initial enqueue before close")
	}
	second := make(chan bool, 1)
	go func() {
		second <- queue.Enqueue("ab")
	}()
	waitForLiveQueueStatusForTest(t, queue, func(status terminalLiveIngestQueueStatus) bool {
		return status.PendingBytes == 8 && status.BackpressureEvents == 1
	}, "bounded enqueue did not enter backpressure wait")
	select {
	case <-second:
		t.Fatal("bounded history enqueue returned before pending bytes drained")
	case <-time.After(20 * time.Millisecond):
	}
	batch, ok := queue.nextBatch()
	if !ok || strings.Join(batch, "") != "12345678" {
		t.Fatalf("unexpected drained batch %#v ok=%v", batch, ok)
	}
	select {
	case ok := <-second:
		if !ok {
			t.Fatal("bounded enqueue should succeed after pending bytes drain")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bounded enqueue after pending bytes drain")
	}
	status := queue.Status()
	if status.PendingBytes != 2 || status.BackpressureEvents != 1 || status.BackpressureWaitNanos <= 0 {
		t.Fatalf("bounded queue lost backpressure diagnostics: %#v", status)
	}
	close(queue.done)
}

func TestR447LowLatencyHistoryTapQueueDoesNotBackpressureOnBufferLimit(t *testing.T) {
	queue := newTerminalHistoryTapIngestQueue(HistoryBackpressureConfig{
		Mode:        HistoryBackpressureLowLatency,
		BufferBytes: 4,
	})
	done := make(chan bool, 1)
	go func() {
		done <- queue.Enqueue("12345678")
	}()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("low-latency enqueue should succeed before close")
		}
	case <-time.After(time.Second):
		t.Fatal("low-latency history queue must not wait on buffer limit")
	}
	status := queue.Status()
	if status.PendingBytes != 8 || status.BackpressureEvents != 0 || status.BackpressureWaitNanos != 0 {
		t.Fatalf("low-latency queue should expose pending bytes without backpressure: %#v", status)
	}
	close(queue.done)
}

func TestR447BoundedHistoryTapQueueSplitsPayloadLargerThanBuffer(t *testing.T) {
	queue := newTerminalHistoryTapIngestQueue(HistoryBackpressureConfig{
		Mode:        HistoryBackpressureBounded,
		BufferBytes: 4,
	})
	done := make(chan bool, 1)
	go func() {
		done <- queue.Enqueue("abcdef")
	}()
	waitForLiveQueueStatusForTest(t, queue, func(status terminalLiveIngestQueueStatus) bool {
		return status.PendingBytes == 4 && status.BackpressureEvents == 1
	}, "bounded queue did not cap oversized payload at buffer limit")
	select {
	case <-done:
		t.Fatal("oversized bounded enqueue must wait before queuing tail")
	case <-time.After(20 * time.Millisecond):
	}
	batch, ok := queue.nextBatch()
	if !ok || strings.Join(batch, "") != "abcd" {
		t.Fatalf("unexpected first capped batch %#v ok=%v", batch, ok)
	}
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("oversized bounded enqueue should succeed after space is released")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for oversized bounded enqueue")
	}
	if status := queue.Status(); status.PendingBytes != 2 {
		t.Fatalf("bounded oversized enqueue should leave only tail pending, got %#v", status)
	}
	close(queue.done)
}

func TestR447BoundedHistoryBackpressureStopsProcessOutputConsumption(t *testing.T) {
	factory := newR447OutputProcessFactory()
	server := NewServer(
		WithProcessFactory(factory),
		WithHistoryBackpressureConfig(HistoryBackpressureConfig{
			Mode:        HistoryBackpressureBounded,
			BufferBytes: 4,
		}),
	)
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r447-output-backpressure",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	terminal, err := server.Terminal("term-r447-output-backpressure")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	process := factory.process
	if process == nil {
		t.Fatal("expected spawned test process")
	}
	terminal.tapOpMu.Lock()
	tapLocked := true
	defer func() {
		if tapLocked {
			terminal.tapOpMu.Unlock()
		}
		_ = server.RemoveTerminal("term-r447-output-backpressure")
	}()

	sendAndWaitForOutputConsumedForTest(t, process, "aaaa")
	waitForLiveQueueStateForTest(t, terminal.historyTapQ, func(queue *terminalLiveIngestQueue) bool {
		return queue.enqueuedSeq >= 1 && queue.pendingBytes == 0 && queue.pendingCount == 0
	}, "first history batch did not move into in-flight writer")
	sendAndWaitForOutputConsumedForTest(t, process, "bbbb")
	waitForLiveQueueStateForTest(t, terminal.historyTapQ, func(queue *terminalLiveIngestQueue) bool {
		return queue.enqueuedSeq >= 2 && queue.pendingBytes == 4 && queue.pendingCount == 1
	}, "second history batch did not fill bounded pending buffer")
	sendAndWaitForOutputConsumedForTest(t, process, "cccc")
	waitForLiveQueueStatusForTest(t, terminal.historyTapQ, func(status terminalLiveIngestQueueStatus) bool {
		return status.PendingBytes == 4 && status.BackpressureEvents == 1
	}, "third history batch did not stop at bounded pending buffer")

	fourth := sendOutputForTest(process, "dddd")
	select {
	case <-fourth:
		t.Fatal("process output should stop being consumed while history pending buffer is full")
	case <-time.After(20 * time.Millisecond):
	}
	terminal.tapOpMu.Unlock()
	tapLocked = false
	select {
	case <-fourth:
	case <-time.After(time.Second):
		t.Fatal("process output did not resume after history writer released pending space")
	}
}

func TestTerminalLiveIngestQueueSplitsSinglePTYReadChunk(t *testing.T) {
	if ptyReadBufferBytes <= terminalLiveIngestBatchMaxBytes {
		t.Fatalf("test requires PTY read chunk larger than live batch, got pty=%d batch=%d", ptyReadBufferBytes, terminalLiveIngestBatchMaxBytes)
	}
	queue := newTerminalLiveIngestQueue()
	chunk := strings.Repeat("x", ptyReadBufferBytes)
	if !queue.Enqueue(chunk) {
		t.Fatal("expected enqueue before close")
	}

	batches := 0
	totalBytes := 0
	for totalBytes < len(chunk) {
		batch, ok := queue.nextBatch()
		if !ok {
			t.Fatal("queue closed before pending PTY chunk drained")
		}
		batches++
		batchBytes := 0
		for _, item := range batch {
			batchBytes += len(item)
		}
		if batchBytes == 0 || batchBytes > terminalLiveIngestBatchMaxBytes {
			t.Fatalf("live batch should split one PTY read into interactive chunks, got %d limit %d", batchBytes, terminalLiveIngestBatchMaxBytes)
		}
		totalBytes += batchBytes
	}

	wantBatches := (ptyReadBufferBytes + terminalLiveIngestBatchMaxBytes - 1) / terminalLiveIngestBatchMaxBytes
	if batches != wantBatches {
		t.Fatalf("expected PTY read chunk to split into %d live batches, got %d", wantBatches, batches)
	}
	close(queue.done)
}

func TestLiveIngestSplitPayloadPrefersLineAndUTF8Boundaries(t *testing.T) {
	head, tail := splitLiveIngestPayload("first\nsecond\nthird", len("first\nsecond"))
	if head != "first\n" || tail != "second\nthird" {
		t.Fatalf("expected split at last complete line, got head=%q tail=%q", head, tail)
	}

	text := "abc中文def"
	limitInsideSecondRune := len("abc中") + 1
	head, tail = splitLiveIngestPayload(text, limitInsideSecondRune)
	if head != "abc中" || tail != "文def" {
		t.Fatalf("expected split to avoid UTF-8 continuation byte, got head=%q tail=%q", head, tail)
	}
}

func TestTerminalLiveIngestQueueDropsConsumedPayloadReferences(t *testing.T) {
	queue := newTerminalLiveIngestQueue()
	chunk := strings.Repeat("x", terminalLiveIngestBatchMaxBytes)
	if !queue.Enqueue(chunk) || !queue.Enqueue("tail") {
		t.Fatal("expected enqueue before close")
	}

	first, ok := queue.nextBatch()
	if !ok || len(first) != 1 {
		t.Fatalf("expected first capped batch, got batch=%d ok=%v", len(first), ok)
	}
	if queue.pendingCount != 1 || queue.head == nil || queue.head.start != 1 || queue.head.items[0].text != "" || queue.head.items[0].seq != 0 {
		t.Fatalf("consumed live payload should not remain in active page, count=%d head=%#v", queue.pendingCount, queue.head)
	}

	second, ok := queue.nextBatch()
	if !ok || len(second) != 1 {
		t.Fatalf("expected second batch, got batch=%d ok=%v", len(second), ok)
	}
	if queue.pendingCount != 0 || queue.head != nil || queue.tail != nil {
		t.Fatalf("empty live buffer should release pages, count=%d head=%#v tail=%#v", queue.pendingCount, queue.head, queue.tail)
	}
	close(queue.done)
}

func TestTerminalLiveIngestQueueFlushWaitsForCurrentTapBatch(t *testing.T) {
	queue := newTerminalLiveIngestQueue()
	started := make(chan struct{})
	release := make(chan struct{})
	go queue.Run(func(string) error {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	})
	if !queue.Enqueue("pending") {
		t.Fatal("expected enqueue before close")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for in-flight tap batch")
	}
	flushed := make(chan error, 1)
	go func() {
		flushed <- queue.Flush(context.Background())
	}()
	select {
	case err := <-flushed:
		t.Fatalf("flush returned before in-flight tap batch completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-flushed:
		if err != nil {
			t.Fatalf("flush failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for flush after tap batch completed")
	}
	queue.Close()
	queue.Wait()
}

func TestTerminalLiveIngestQueueFlushDoesNotWaitForFutureOutput(t *testing.T) {
	queue := newTerminalLiveIngestQueue()
	releaseFirst := make(chan struct{})
	releaseSecond := make(chan struct{})
	seen := make(chan string, 2)
	go queue.Run(func(text string) error {
		seen <- text
		if strings.HasPrefix(text, "first") {
			<-releaseFirst
		} else if text == "second" {
			<-releaseSecond
		}
		return nil
	})
	firstPayload := "first" + strings.Repeat("x", terminalLiveIngestBatchMaxBytes-len("first"))
	if !queue.Enqueue(firstPayload) {
		t.Fatal("expected first enqueue")
	}
	select {
	case got := <-seen:
		if got != firstPayload {
			t.Fatalf("expected first batch, got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first batch")
	}
	flushed := make(chan error, 1)
	go func() {
		flushed <- queue.Flush(context.Background())
	}()
	waitForLiveQueueFlushTargetForTest(t, queue, 1, 1)
	if !queue.Enqueue("second") {
		t.Fatal("expected second enqueue before close")
	}
	close(releaseFirst)
	select {
	case err := <-flushed:
		if err != nil {
			t.Fatalf("flush failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("flush should finish after first target, without waiting for future output")
	}
	select {
	case got := <-seen:
		if got != "second" {
			t.Fatalf("expected second batch, got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second batch")
	}
	close(releaseSecond)
	queue.Close()
	queue.Wait()
}

func TestTerminalLiveIngestQueueFlushAllowsConcurrentWaitersForSameTarget(t *testing.T) {
	queue := newTerminalLiveIngestQueue()
	started := make(chan struct{})
	release := make(chan struct{})
	go queue.Run(func(string) error {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	})
	if !queue.Enqueue("pending") {
		t.Fatal("expected enqueue before close")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for in-flight tap batch")
	}
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() {
		first <- queue.Flush(context.Background())
	}()
	go func() {
		second <- queue.Flush(context.Background())
	}()
	waitForLiveQueueFlushTargetForTest(t, queue, 1, 2)
	close(release)
	for name, ch := range map[string]chan error{"first": first, "second": second} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("%s flush failed: %v", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s flush should finish after shared target completes", name)
		}
	}
	queue.Close()
	queue.Wait()
}

func waitForLiveQueueFlushTargetForTest(t *testing.T, queue *terminalLiveIngestQueue, target uint64, wantWaiters int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		queue.mu.Lock()
		waiting := len(queue.flushWait[target])
		queue.mu.Unlock()
		if waiting >= wantWaiters {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for live queue flush registration")
}

func waitForLiveQueueStatusForTest(t *testing.T, queue *terminalLiveIngestQueue, check func(terminalLiveIngestQueueStatus) bool, message string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if check(queue.Status()) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(message)
}

func waitForLiveQueueStateForTest(t *testing.T, queue *terminalLiveIngestQueue, check func(*terminalLiveIngestQueue) bool, message string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		queue.mu.Lock()
		ok := check(queue)
		queue.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(message)
}

type r447OutputProcessFactory struct {
	process *r447OutputProcess
}

func newR447OutputProcessFactory() *r447OutputProcessFactory {
	return &r447OutputProcessFactory{}
}

func (factory *r447OutputProcessFactory) Spawn(context.Context, ProcessSpec) (TerminalProcess, error) {
	process := &r447OutputProcess{
		outputCh: make(chan []byte),
		waitCh:   make(chan ProcessExit, 1),
	}
	factory.process = process
	return process, nil
}

type r447OutputProcess struct {
	outputCh chan []byte
	waitCh   chan ProcessExit
	close    sync.Once
}

func (process *r447OutputProcess) Input([]byte) error {
	return nil
}

func (process *r447OutputProcess) Resize(Size) error {
	return nil
}

func (process *r447OutputProcess) Output() <-chan []byte {
	return process.outputCh
}

func (process *r447OutputProcess) Kill() error {
	return process.Close()
}

func (process *r447OutputProcess) Wait() <-chan ProcessExit {
	return process.waitCh
}

func (process *r447OutputProcess) Close() error {
	process.close.Do(func() {
		close(process.outputCh)
		process.waitCh <- ProcessExit{Code: -1, Err: io.ErrClosedPipe}
		close(process.waitCh)
	})
	return nil
}

func sendOutputForTest(process *r447OutputProcess, text string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		process.outputCh <- []byte(text)
		close(done)
	}()
	return done
}

func sendAndWaitForOutputConsumedForTest(t *testing.T, process *r447OutputProcess, text string) {
	t.Helper()
	done := sendOutputForTest(process, text)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for process output %q to be consumed", text)
	}
}
