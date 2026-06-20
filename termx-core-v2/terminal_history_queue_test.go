package termxcorev2

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTerminalHistoryIngestQueueEnqueueDoesNotWaitForSlowIngest(t *testing.T) {
	queue := newTerminalHistoryIngestQueue()
	started := make(chan struct{})
	release := make(chan struct{})
	go queue.Run(func([]string) error {
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

func TestTerminalHistoryIngestQueueDropsConsumedPayloadReferences(t *testing.T) {
	queue := newTerminalHistoryIngestQueue()
	chunk := strings.Repeat("x", terminalHistoryIngestBatchMaxBytes)
	if !queue.Enqueue(chunk) || !queue.Enqueue("tail") {
		t.Fatal("expected enqueue before close")
	}

	first, ok := queue.nextBatch()
	if !ok || len(first) != 1 {
		t.Fatalf("expected first capped batch, got batch=%d ok=%v", len(first), ok)
	}
	retained := queue.pending[:cap(queue.pending)]
	if len(queue.pending) != 1 || retained[1].text != "" {
		t.Fatalf("consumed history payload should not remain in backing array, len=%d retained=%#v", len(queue.pending), retained)
	}

	second, ok := queue.nextBatch()
	if !ok || len(second) != 1 {
		t.Fatalf("expected second batch, got batch=%d ok=%v", len(second), ok)
	}
	if queue.pending != nil {
		t.Fatalf("empty history buffer should release backing array, got len=%d cap=%d", len(queue.pending), cap(queue.pending))
	}
	close(queue.done)
}

func TestTerminalHistoryIngestQueueFlushWaitsForInFlightBatch(t *testing.T) {
	queue := newTerminalHistoryIngestQueue()
	started := make(chan struct{})
	release := make(chan struct{})
	ingested := make(chan string, 2)
	go queue.Run(func(texts []string) error {
		for _, text := range texts {
			ingested <- text
		}
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	})

	if !queue.Enqueue("latest-tail\n") {
		t.Fatal("expected enqueue")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for worker")
	}
	flushed := make(chan error, 1)
	go func() {
		flushed <- queue.Flush(context.Background())
	}()
	select {
	case err := <-flushed:
		t.Fatalf("flush returned before in-flight batch completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-flushed; err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := <-ingested; got != "latest-tail\n" {
		t.Fatalf("unexpected ingested text %q", got)
	}
	queue.Close()
	queue.Wait()
}

func TestTerminalHistoryIngestQueueFlushDoesNotPullFutureOutputIntoSameBatch(t *testing.T) {
	queue := newTerminalHistoryIngestQueue()
	ingested := make(chan string, 3)
	go queue.Run(func(texts []string) error {
		for _, text := range texts {
			ingested <- text
		}
		return nil
	})

	if !queue.Enqueue("before\n") {
		t.Fatal("expected enqueue before flush")
	}
	if err := queue.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if !queue.Enqueue("after\n") {
		t.Fatal("expected enqueue after flush")
	}
	if got := <-ingested; got != "before\n" {
		t.Fatalf("flush should process pre-marker output before returning, got %q", got)
	}
	if got := <-ingested; got != "after\n" {
		t.Fatalf("future output should stay in later batch, got %q", got)
	}
	queue.Close()
	queue.Wait()
}

func TestTerminalHistoryIngestQueueSegmentedIngestKeepsParserPendingAcrossChunks(t *testing.T) {
	queue := newTerminalHistoryIngestQueue()
	pipeline := newTerminalHistoryPipeline(80, 24)
	go queue.Run(func(texts []string) error {
		return pipeline.IngestBatch(texts)
	})

	if !queue.Enqueue("\x1b[31") || !queue.Enqueue("mred\x1b[0m\n") {
		t.Fatal("expected enqueue before flush")
	}
	if err := queue.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	queue.Close()
	queue.Wait()

	window, err := pipeline.LatestWindow(80, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "red" {
		t.Fatalf("expected segmented parser to keep text, got %#v", window.Rows)
	}
	if len(window.Rows[0].Cells) != 1 || window.Rows[0].Cells[0].Style.FG != "ansi:1" {
		t.Fatalf("expected split CSI style to survive segmented ingest, row=%#v", window.Rows[0])
	}
}
