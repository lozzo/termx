package termxcorev2

import (
	"context"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core-v2/history"
)

func TestTerminalHistoryIngestQueueEnqueueDoesNotWaitForSlowIngest(t *testing.T) {
	queue := newTerminalHistoryIngestQueue()
	started := make(chan struct{})
	release := make(chan struct{})
	go queue.Run(func([]history.TerminalSemanticTransaction) error {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	})
	if !queue.Enqueue(r373HistoryQueueTx(1, "one")) {
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
			if !queue.Enqueue(r373HistoryQueueTx(uint64(i+2), "more")) {
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
	for seq := uint64(1); seq <= terminalHistoryIngestBatchMaxTransactions+2; seq++ {
		if !queue.Enqueue(r373HistoryQueueTx(seq, "payload")) {
			t.Fatal("expected enqueue before close")
		}
	}
	first, ok := queue.nextBatch()
	if !ok || len(first) != terminalHistoryIngestBatchMaxTransactions {
		t.Fatalf("expected first capped batch, got batch=%d ok=%v", len(first), ok)
	}
	second, ok := queue.nextBatch()
	if !ok || len(second) != 2 {
		t.Fatalf("expected second capped batch with remaining txs, got batch=%d ok=%v", len(second), ok)
	}
	queue.Close()
	third, ok := queue.nextBatch()
	if ok || third != nil {
		t.Fatalf("expected closed queue to drain, got batch=%#v ok=%v", third, ok)
	}
	close(queue.done)
}

func TestTerminalHistoryIngestQueueDropsConsumedTransactionReferences(t *testing.T) {
	queue := newTerminalHistoryIngestQueue()
	if !queue.Enqueue(r373HistoryQueueTx(1, "alpha")) || !queue.Enqueue(r373HistoryQueueTx(2, "beta")) {
		t.Fatal("expected enqueue before close")
	}

	first, ok := queue.nextBatch()
	if !ok || len(first) != 2 {
		t.Fatalf("expected first batch, got batch=%d ok=%v", len(first), ok)
	}
	retained := queue.pending[:cap(queue.pending)]
	if len(queue.pending) != 0 {
		t.Fatalf("all transactions should be consumed, got %d pending", len(queue.pending))
	}
	for index, item := range retained {
		if item.tx.Seq != 0 || item.tx.PrimaryFrame != nil {
			t.Fatalf("consumed history transaction %d should not remain in backing array: %#v", index, item)
		}
	}
	close(queue.done)
}

func TestR373TerminalHistoryIngestQueueStoresSemanticTransactionsNotRawPTY(t *testing.T) {
	queue := newTerminalHistoryIngestQueue()
	tx := r373HistoryQueueTx(1, "immutable")
	if !queue.Enqueue(tx) {
		t.Fatal("expected enqueue before close")
	}
	tx.PrimaryFrame.Rows[0][0].Content = "mutated-after-enqueue"
	batch, ok := queue.nextBatch()
	if !ok || len(batch) != 1 {
		t.Fatalf("expected one transaction batch, got len=%d ok=%v", len(batch), ok)
	}
	txs := terminalHistoryIngestBatchTransactions(batch)
	if len(txs) != 1 {
		t.Fatalf("expected one semantic transaction, got %#v", txs)
	}
	if txs[0].Raw != "" {
		t.Fatalf("history backlog must not expose raw PTY replay payload, got %q", txs[0].Raw)
	}
	if got := txs[0].PrimaryFrame.Rows[0][0].Content; got != "immutable" {
		t.Fatalf("history queue must deep-copy tap transaction at enqueue, got %q", got)
	}
	txs[0].PrimaryFrame.Rows[0][0].Content = "mutated-consumer"
	again := terminalHistoryIngestBatchTransactions(batch)
	if got := again[0].PrimaryFrame.Rows[0][0].Content; got != "immutable" {
		t.Fatalf("history queue batch handoff must deep-copy transaction per consumer, got %q", got)
	}
	close(queue.done)
}

func TestTerminalHistoryIngestQueueFlushWaitsForInFlightBatch(t *testing.T) {
	queue := newTerminalHistoryIngestQueue()
	started := make(chan struct{})
	release := make(chan struct{})
	ingested := make(chan history.TerminalSemanticTransaction, 2)
	go queue.Run(func(txs []history.TerminalSemanticTransaction) error {
		for _, tx := range txs {
			ingested <- tx
		}
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	})

	if !queue.Enqueue(r373HistoryQueueTx(1, "latest-tail")) {
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
	if got := <-ingested; got.Seq != 1 {
		t.Fatalf("unexpected ingested tx %#v", got)
	}
	queue.Close()
	queue.Wait()
}

func TestTerminalHistoryIngestQueueFlushDoesNotPullFutureOutputIntoSameBatch(t *testing.T) {
	queue := newTerminalHistoryIngestQueue()
	ingested := make(chan history.TerminalSemanticTransaction, 3)
	go queue.Run(func(txs []history.TerminalSemanticTransaction) error {
		for _, tx := range txs {
			ingested <- tx
		}
		return nil
	})

	if !queue.Enqueue(r373HistoryQueueTx(1, "before")) {
		t.Fatal("expected enqueue before flush")
	}
	if err := queue.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if !queue.Enqueue(r373HistoryQueueTx(2, "after")) {
		t.Fatal("expected enqueue after flush")
	}
	if got := <-ingested; got.Seq != 1 {
		t.Fatalf("flush should process pre-marker tx before returning, got %#v", got)
	}
	if got := <-ingested; got.Seq != 2 {
		t.Fatalf("future output should stay in later batch, got %#v", got)
	}
	queue.Close()
	queue.Wait()
}

func r373HistoryQueueTx(seq uint64, text string) history.TerminalSemanticTransaction {
	tx := history.TerminalSemanticTransaction{
		Seq: seq,
		PrimaryFrame: &history.TerminalSemanticFrame{
			Cols: 20,
			Rows: [][]history.TerminalSemanticCell{{
				{Content: text, Width: 1},
			}},
		},
	}
	tx.Raw = ""
	return tx
}
