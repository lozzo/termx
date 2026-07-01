package termxcorev2

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core-v2/history"
)

func TestTerminalHistoryIngestQueueEnqueueDoesNotWaitForSlowIngest(t *testing.T) {
	queue := newTerminalHistoryIngestQueue(0)
	started := make(chan struct{})
	release := make(chan struct{})
	go queue.Run(func([]history.HistoryJournal) error {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	})
	if !queue.Enqueue(r385HistoryQueueJournal(1, "one")) {
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
			if !queue.Enqueue(r385HistoryQueueJournal(uint64(i+2), "more")) {
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
	for seq := uint64(1); seq <= terminalHistoryIngestBatchMaxJournals+2; seq++ {
		if !queue.Enqueue(r385HistoryQueueJournal(seq, "payload")) {
			t.Fatal("expected enqueue before close")
		}
	}
	first, ok := queue.nextBatch()
	if !ok || len(first) != terminalHistoryIngestBatchMaxJournals {
		t.Fatalf("expected first capped batch, got batch=%d ok=%v", len(first), ok)
	}
	second, ok := queue.nextBatch()
	if !ok || len(second) != 2 {
		t.Fatalf("expected second capped batch with remaining journals, got batch=%d ok=%v", len(second), ok)
	}
	queue.Close()
	third, ok := queue.nextBatch()
	if ok || third != nil {
		t.Fatalf("expected closed queue to drain, got batch=%#v ok=%v", third, ok)
	}
	close(queue.done)
}

func TestTerminalHistoryIngestQueueDropsConsumedJournalReferences(t *testing.T) {
	queue := newTerminalHistoryIngestQueue(0)
	if !queue.Enqueue(r385HistoryQueueJournal(1, "alpha")) || !queue.Enqueue(r385HistoryQueueJournal(2, "beta")) {
		t.Fatal("expected enqueue before close")
	}

	first, ok := queue.nextBatch()
	if !ok || len(first) != 2 {
		t.Fatalf("expected first batch, got batch=%d ok=%v", len(first), ok)
	}
	retained := queue.pending[:cap(queue.pending)]
	if len(queue.pending) != 0 {
		t.Fatalf("all journals should be consumed, got %d pending", len(queue.pending))
	}
	for index, item := range retained {
		if item.journal.Seq != 0 || len(item.journal.Items) != 0 {
			t.Fatalf("consumed history journal %d should not remain in backing array: %#v", index, item)
		}
	}
	close(queue.done)
}

func TestR385TerminalHistoryIngestQueueStoresCompactJournalsNotTransactions(t *testing.T) {
	queue := newTerminalHistoryIngestQueue(0)
	journal := r385HistoryQueueJournal(1, "immutable")
	if !queue.Enqueue(journal) {
		t.Fatal("expected enqueue before close")
	}
	journal.Items[0].Frame.Frame.Rows[0][0].Content = "mutated-after-enqueue"
	batch, ok := queue.nextBatch()
	if !ok || len(batch) != 1 {
		t.Fatalf("expected one journal batch, got len=%d ok=%v", len(batch), ok)
	}
	journals := terminalHistoryIngestBatchJournals(batch)
	if len(journals) != 1 {
		t.Fatalf("expected one compact journal, got %#v", journals)
	}
	if journals[0].Source != history.HistoryJournalSourceSemanticTapTransaction {
		t.Fatalf("history backlog must preserve semantic tap journal source, got %#v", journals[0])
	}
	if got := journals[0].Items[0].Frame.Frame.Rows[0][0].Content; got != "immutable" {
		t.Fatalf("history queue must deep-copy compact journal at enqueue, got %q", got)
	}
	journals[0].Items[0].Frame.Frame.Rows[0][0].Content = "mutated-consumer"
	again := terminalHistoryIngestBatchJournals(batch)
	if got := again[0].Items[0].Frame.Frame.Rows[0][0].Content; got != "immutable" {
		t.Fatalf("history queue batch handoff must deep-copy journal per consumer, got %q", got)
	}
	close(queue.done)
}

func TestR385TerminalHistoryIngestWorkerDoesNotStoreFullTransactions(t *testing.T) {
	source, err := os.ReadFile("terminal_history_ingest_worker.go")
	if err != nil {
		t.Fatalf("read queue source: %v", err)
	}
	text := string(source)
	if strings.Contains(text, "TerminalSemanticTransaction") || strings.Contains(text, "cloneSemanticTapTransaction") {
		t.Fatalf("history backlog worker must store compact journals, not full semantic transaction queues")
	}
	if !strings.Contains(text, "HistoryJournal") || !strings.Contains(text, "CloneHistoryJournal") {
		t.Fatalf("history backlog worker should explicitly queue cloned HistoryJournal payloads")
	}
}

func TestR385TerminalHistoryIngestQueueClassifierFlushTracksBacklogRisk(t *testing.T) {
	queue := newTerminalHistoryIngestQueue(0)
	plainNext := r385HistoryQueueOrdinaryJournal(10, "next")
	frameNext := r385HistoryQueueJournal(11, "next-frame")
	if !queue.Enqueue(r385HistoryQueueOrdinaryJournal(1, "plain")) {
		t.Fatal("expected ordinary journal enqueue")
	}
	if queue.NeedsClassifierFlush(plainNext) {
		t.Fatal("sealed ordinary backlog before another ordinary journal should stay async")
	}
	if !queue.NeedsClassifierFlush(frameNext) {
		t.Fatal("sealed ordinary backlog before frame/boundary journal must flush for classifier state")
	}
	ordinary, ok := queue.nextBatch()
	if !ok || len(ordinary) != 1 {
		t.Fatalf("expected ordinary batch, got len=%d ok=%v", len(ordinary), ok)
	}
	if queue.NeedsClassifierFlush(plainNext) {
		t.Fatal("in-flight sealed ordinary backlog before another ordinary journal should stay async")
	}
	if !queue.NeedsClassifierFlush(frameNext) {
		t.Fatal("in-flight sealed ordinary backlog before frame/boundary journal must flush")
	}
	queue.finishBatch(terminalHistoryIngestBatchCompleteSeq(ordinary))

	if !queue.Enqueue(r385HistoryQueueJournal(2, "frame")) {
		t.Fatal("expected frame journal enqueue")
	}
	if !queue.NeedsClassifierFlush(plainNext) {
		t.Fatal("frame journal must force classifier flush while pending")
	}
	frame, ok := queue.nextBatch()
	if !ok || len(frame) != 1 {
		t.Fatalf("expected frame batch, got len=%d ok=%v", len(frame), ok)
	}
	if !queue.NeedsClassifierFlush(plainNext) {
		t.Fatal("frame journal must force classifier flush while in-flight")
	}
	queue.finishBatch(terminalHistoryIngestBatchCompleteSeq(frame))
	if queue.NeedsClassifierFlush(plainNext) {
		t.Fatal("classifier flush should clear after frame journal is applied")
	}
	close(queue.done)
}

func TestTerminalHistoryIngestQueueFlushWaitsForInFlightBatch(t *testing.T) {
	queue := newTerminalHistoryIngestQueue(0)
	started := make(chan struct{})
	release := make(chan struct{})
	ingested := make(chan history.HistoryJournal, 2)
	go queue.Run(func(journals []history.HistoryJournal) error {
		for _, journal := range journals {
			ingested <- journal
		}
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	})

	if !queue.Enqueue(r385HistoryQueueJournal(1, "latest-tail")) {
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
		t.Fatalf("unexpected ingested journal %#v", got)
	}
	queue.Close()
	queue.Wait()
}

func TestTerminalHistoryIngestQueueFlushDoesNotPullFutureOutputIntoSameBatch(t *testing.T) {
	queue := newTerminalHistoryIngestQueue(0)
	ingested := make(chan history.HistoryJournal, 3)
	releaseFirst := make(chan struct{})
	releaseSecond := make(chan struct{})
	go queue.Run(func(journals []history.HistoryJournal) error {
		for _, journal := range journals {
			ingested <- journal
		}
		if len(journals) > 0 && journals[0].Seq == 1 {
			<-releaseFirst
		}
		if len(journals) > 0 && journals[0].Seq == 2 {
			<-releaseSecond
		}
		return nil
	})

	if !queue.Enqueue(r385HistoryQueueJournal(1, "before")) {
		t.Fatal("expected enqueue before flush")
	}
	select {
	case got := <-ingested:
		if got.Seq != 1 {
			t.Fatalf("expected first journal to enter in-flight batch, got %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first history journal")
	}
	flushed := make(chan error, 1)
	go func() {
		flushed <- queue.Flush(context.Background())
	}()
	waitForHistoryQueueWaitTargetForTest(t, queue, 1, 1)
	if !queue.Enqueue(r385HistoryQueueJournal(2, "after")) {
		t.Fatal("expected enqueue after flush")
	}
	status := queue.Status()
	if status.TargetSeq != 2 || status.AppliedSeq != 0 || !status.CatchupPending {
		t.Fatalf("expected future journal to raise diagnostic target without satisfying first flush, status=%#v", status)
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
		t.Fatal("timed out waiting for future history journal")
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
	if !queue.Enqueue(r385HistoryQueueJournal(1, "one")) || !queue.Enqueue(r385HistoryQueueJournal(2, "two")) {
		t.Fatal("expected enqueue")
	}
	status := queue.Status()
	if status.AppliedSeq != 41 || status.TargetSeq != 43 || !status.CatchupPending || status.PendingTransactions != 2 {
		t.Fatalf("unexpected pending status %#v", status)
	}
	batch, ok := queue.nextBatch()
	if !ok || len(batch) != 2 {
		t.Fatalf("expected two journal batch, got len=%d ok=%v", len(batch), ok)
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
	go queue.Run(func([]history.HistoryJournal) error {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	})
	if !queue.Enqueue(r385HistoryQueueJournal(1, "pending")) {
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

func r385HistoryQueueJournal(seq uint64, text string) history.HistoryJournal {
	return history.HistoryJournalFromTransaction("term-r385-queue", history.TerminalSemanticTransaction{
		Seq: seq,
		PrimaryFrame: &history.TerminalSemanticFrame{
			Cols: 20,
			Rows: [][]history.TerminalSemanticCell{{
				{Content: text, Width: 1},
			}},
		},
	})
}

func r385HistoryQueueOrdinaryJournal(seq uint64, text string) history.HistoryJournal {
	return history.HistoryJournal{
		TerminalID: "term-r385-queue",
		Seq:        seq,
		Source:     history.HistoryJournalSourceSemanticTapTransaction,
		Items: []history.HistoryJournalItem{{
			Kind: history.HistoryJournalItemOrdinaryLineBatch,
			Ordinary: &history.OrdinaryLineBatch{
				Lines: []history.JournalLogicalLine{{
					Cells:  []history.Cell{{Text: text, Width: len(text)}},
					Origin: history.HistoryJournalOriginOrdinaryPrimary,
				}},
				Origin: history.HistoryJournalOriginOrdinaryPrimary,
			},
		}},
	}
}
