package termxcorev2

import (
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
	if len(queue.pending) != 1 || retained[1] != "" {
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
