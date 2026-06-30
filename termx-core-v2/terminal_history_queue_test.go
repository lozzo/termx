package termxcorev2

import (
	"context"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core-v2/history"
)

func TestTerminalHistoryIngestQueueEnqueueDoesNotWaitForSlowIngest(t *testing.T) {
	queue := newTerminalHistoryIngestQueue(0)
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
	queue := newTerminalHistoryIngestQueue(0)
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
	queue := newTerminalHistoryIngestQueue(0)
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
	queue := newTerminalHistoryIngestQueue(0)
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
	queue := newTerminalHistoryIngestQueue(0)
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
	queue := newTerminalHistoryIngestQueue(0)
	ingested := make(chan history.TerminalSemanticTransaction, 3)
	releaseFirst := make(chan struct{})
	releaseSecond := make(chan struct{})
	go queue.Run(func(txs []history.TerminalSemanticTransaction) error {
		for _, tx := range txs {
			ingested <- tx
		}
		if len(txs) > 0 && txs[0].Seq == 1 {
			<-releaseFirst
		}
		if len(txs) > 0 && txs[0].Seq == 2 {
			<-releaseSecond
		}
		return nil
	})

	if !queue.Enqueue(r373HistoryQueueTx(1, "before")) {
		t.Fatal("expected enqueue before flush")
	}
	select {
	case got := <-ingested:
		if got.Seq != 1 {
			t.Fatalf("expected first tx to enter in-flight batch, got %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first history tx")
	}
	flushed := make(chan error, 1)
	go func() {
		flushed <- queue.Flush(context.Background())
	}()
	waitForHistoryQueueWaitTargetForTest(t, queue, 1, 1)
	if !queue.Enqueue(r373HistoryQueueTx(2, "after")) {
		t.Fatal("expected enqueue after flush")
	}
	status := queue.Status()
	if status.TargetSeq != 2 || status.AppliedSeq != 0 || !status.CatchupPending {
		t.Fatalf("expected future tx to raise diagnostic target without satisfying first flush, status=%#v", status)
	}
	close(releaseFirst)
	select {
	case err := <-flushed:
		if err != nil {
			t.Fatalf("flush: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("flush should complete after original target")
	}
	select {
	case got := <-ingested:
		if got.Seq != 2 {
			t.Fatalf("future output should stay in later batch, got %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for future history tx")
	}
	close(releaseSecond)
	queue.Close()
	queue.Wait()
}

func TestR374TerminalHistoryIngestQueueReportsAppliedTargetSeq(t *testing.T) {
	queue := newTerminalHistoryIngestQueue(41)
	if got := queue.Status(); got.AppliedSeq != 41 || got.TargetSeq != 41 || got.CatchupPending {
		t.Fatalf("unexpected initial status %#v", got)
	}
	if !queue.Enqueue(r373HistoryQueueTx(1, "one")) || !queue.Enqueue(r373HistoryQueueTx(2, "two")) {
		t.Fatal("expected enqueue")
	}
	status := queue.Status()
	if status.AppliedSeq != 41 || status.TargetSeq != 43 || !status.CatchupPending || status.PendingTransactions != 2 {
		t.Fatalf("unexpected pending status %#v", status)
	}
	batch, ok := queue.nextBatch()
	if !ok || len(batch) != 2 {
		t.Fatalf("expected two tx batch, got len=%d ok=%v", len(batch), ok)
	}
	queue.finishBatch(terminalHistoryIngestBatchCompleteSeq(batch))
	status = queue.Status()
	if status.AppliedSeq != 43 || status.TargetSeq != 43 || status.CatchupPending {
		t.Fatalf("unexpected applied status %#v", status)
	}
	close(queue.done)
}

func TestR374TerminalHistoryIngestQueueFlushAllowsConcurrentWaitersForSameTarget(t *testing.T) {
	queue := newTerminalHistoryIngestQueue(0)
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
	if !queue.Enqueue(r373HistoryQueueTx(1, "pending")) {
		t.Fatal("expected enqueue")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for history worker")
	}
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() {
		first <- queue.Flush(context.Background())
	}()
	go func() {
		second <- queue.Flush(context.Background())
	}()
	waitForHistoryQueueWaitTargetForTest(t, queue, 1, 2)
	close(release)
	for name, ch := range map[string]chan error{"first": first, "second": second} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("%s flush failed: %v", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s flush should finish after target completes", name)
		}
	}
	queue.Close()
	queue.Wait()
}

func waitForHistoryQueueWaitTargetForTest(t *testing.T, queue *terminalHistoryIngestQueue, target uint64, wantWaiters int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		queue.mu.Lock()
		waiting := len(queue.waiters[target])
		queue.mu.Unlock()
		if waiting >= wantWaiters {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for history queue flush registration")
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
