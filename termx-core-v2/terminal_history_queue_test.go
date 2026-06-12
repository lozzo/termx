package termxcorev2

import (
	"strings"
	"testing"
	"time"
)

func TestTerminalHistoryIngestQueueEnqueueDoesNotWaitForSlowIngest(t *testing.T) {
	queue := newTerminalHistoryIngestQueue()
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
		t.Fatal("enqueue should not wait for slow history ingest")
	}
	close(release)
	queue.Close()
	queue.Wait()
}

func TestTerminalHistoryIngestQueueSplitsLargePendingBatch(t *testing.T) {
	queue := newTerminalHistoryIngestQueue()
	chunk := strings.Repeat("x", terminalHistoryIngestBatchMaxBytes/2+1)
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
