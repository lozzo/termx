package termxcorev2

import (
	"context"
	"strings"
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
	if !queue.Enqueue(chunk) || !queue.Enqueue(chunk) || !queue.Enqueue("tail") {
		t.Fatal("expected enqueue before close")
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
	retained := queue.pending[:cap(queue.pending)]
	if len(queue.pending) != 1 || retained[1].text != "" || retained[1].seq != 0 {
		t.Fatalf("consumed live payload should not remain in backing array, len=%d retained=%#v", len(queue.pending), retained)
	}

	second, ok := queue.nextBatch()
	if !ok || len(second) != 1 {
		t.Fatalf("expected second batch, got batch=%d ok=%v", len(second), ok)
	}
	if queue.pending != nil {
		t.Fatalf("empty live buffer should release backing array, got len=%d cap=%d", len(queue.pending), cap(queue.pending))
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
