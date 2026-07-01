package termxcorev2

import (
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core-v2/history"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestR372SemanticTapOwnsSingleVTermForLiveAndHistory(t *testing.T) {
	tap := NewSemanticTap("term-r372", Size{Cols: 12, Rows: 3}, nil)
	result, err := tap.ApplyPTYWrite([]byte("one\r\ntwo"))
	if err != nil {
		t.Fatalf("apply PTY write: %v", err)
	}
	tx := result.Transaction()
	if tx.Seq != 1 {
		t.Fatalf("first semantic transaction should be seq 1, got %d", tx.Seq)
	}
	snapshot := tap.NativeScreenSnapshot()
	if result.Revision() != 1 || snapshot.Revision != 1 {
		t.Fatalf("live revision should come from same tap result, got result=%d snapshot=%d", result.Revision(), snapshot.Revision)
	}
	if got := semanticTapSnapshotText(snapshot); !strings.Contains(got, "one") || !strings.Contains(got, "two") {
		t.Fatalf("live snapshot should reflect same vterm write as history tx, got %q tx=%#v", got, tx)
	}
	if tx.PrimaryFrame != nil {
		t.Fatalf("ordinary stream transaction should not carry full frame side proof: %#v", tx.PrimaryFrame)
	}
}

func TestR372SemanticTapOrdersPTYBytesAndResizeInOneSequence(t *testing.T) {
	tap := NewSemanticTap("term-r372", Size{Cols: 8, Rows: 2}, nil)
	first, err := tap.ApplyPTYWrite([]byte("abcdef"))
	if err != nil {
		t.Fatalf("apply first write: %v", err)
	}
	resized, err := tap.Resize(Size{Cols: 4, Rows: 3})
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	second, err := tap.ApplyPTYWrite([]byte("\r\nzz"))
	if err != nil {
		t.Fatalf("apply second write: %v", err)
	}
	firstTx := first.Transaction()
	resizeTx := resized.Transaction()
	secondTx := second.Transaction()
	if firstTx.Seq != 1 || resizeTx.Seq != 2 || secondTx.Seq != 3 {
		t.Fatalf("tap should preserve write/resize/write semantic seq, got %d/%d/%d", firstTx.Seq, resizeTx.Seq, secondTx.Seq)
	}
	if resizeTx.Size.Cols != 4 || resizeTx.Size.Rows != 3 {
		t.Fatalf("resize tx should carry target size, got %#v", resizeTx.Size)
	}
	if resizeTx.FullReplaceReason != "resize" {
		t.Fatalf("resize should be explicit semantic boundary, got reason=%q tx=%#v", resizeTx.FullReplaceReason, resizeTx)
	}
	records := tap.InputRecords()
	if len(records) != 3 {
		t.Fatalf("expected three ordered input records, got %#v", records)
	}
	wantKinds := []SemanticTapInputKind{SemanticTapInputWrite, SemanticTapInputResize, SemanticTapInputWrite}
	for i, want := range wantKinds {
		if records[i].Seq != uint64(i+1) || records[i].Kind != want {
			t.Fatalf("input record %d lost sequence/kind, got %#v want kind %s", i, records[i], want)
		}
	}
	if got := semanticTapSnapshotText(tap.NativeScreenSnapshot()); !strings.Contains(got, "zz") {
		t.Fatalf("post-resize write should update same latest screen, got %q", got)
	}
}

func TestR372SemanticTapOwnsTerminalResponsesExactlyOnce(t *testing.T) {
	responses := make(chan string, 4)
	tap := NewSemanticTap("term-r372", Size{Cols: 12, Rows: 3}, func(data []byte) {
		responses <- string(data)
	})
	if _, err := tap.ApplyPTYWrite([]byte("\x1b[6n")); err != nil {
		t.Fatalf("DSR write: %v", err)
	}
	select {
	case got := <-responses:
		if got == "" {
			t.Fatal("expected non-empty terminal response")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for semantic tap terminal response")
	}
	select {
	case extra := <-responses:
		t.Fatalf("response must be owned by single tap exactly once, got extra %q", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestR372SemanticTapLiveSnapshotCoalescingDoesNotDropEmulatorState(t *testing.T) {
	tap := NewSemanticTap("term-r372", Size{Cols: 20, Rows: 4}, nil)
	if _, err := tap.ApplyPTYWrite([]byte("stale-frame")); err != nil {
		t.Fatalf("apply stale write: %v", err)
	}
	// 中文说明：live consumer 可以丢弃这张 stale snapshot；tap 内的 vterm state
	// 仍必须继续接收后续 PTY bytes，不能由 consumer 补一套 screen。
	if _, err := tap.ApplyPTYWrite([]byte("\r\nlatest-frame")); err != nil {
		t.Fatalf("apply latest write: %v", err)
	}
	snapshot := tap.NativeScreenSnapshot()
	if snapshot.Revision != 2 {
		t.Fatalf("expected latest revision 2 after coalesced live wakeups, got %d", snapshot.Revision)
	}
	text := semanticTapSnapshotText(snapshot)
	if !strings.Contains(text, "stale-frame") || !strings.Contains(text, "latest-frame") {
		t.Fatalf("dropping old live snapshot must not drop tap emulator state, got %q", text)
	}
}

func TestR372SemanticTapHistoryConsumerReceivesOnlyTransaction(t *testing.T) {
	tap := NewSemanticTap("term-r372", Size{Cols: 16, Rows: 3}, nil)
	result, err := tap.ApplyPTYWrite([]byte("history-only\r\n"))
	if err != nil {
		t.Fatalf("apply PTY write: %v", err)
	}
	tx := result.Transaction()
	consumer := &r372TransactionOnlyHistoryConsumer{}
	consumer.Consume(tx)
	if len(consumer.rawPTY) != 0 {
		t.Fatalf("history consumer must not receive raw PTY replay payloads: %#v", consumer.rawPTY)
	}
	if len(consumer.transactions) != 1 || consumer.transactions[0].Seq != tx.Seq {
		t.Fatalf("history consumer should receive exactly the tap transaction, got %#v", consumer.transactions)
	}
	if tx.Raw != "" {
		t.Fatalf("tap handoff must not expose raw PTY replay payload, got %q", tx.Raw)
	}
}

func TestR372SemanticTapFanOutKeepsConsumersAfterParserBoundary(t *testing.T) {
	tap := NewSemanticTap("term-r372", Size{Cols: 16, Rows: 3}, nil)
	result, err := tap.ApplyPTYWrite([]byte("fanout\r\n"))
	if err != nil {
		t.Fatalf("apply PTY write: %v", err)
	}
	liveConsumer := &r372LiveProjectionConsumer{}
	historyConsumer := &r372TransactionOnlyHistoryConsumer{}

	liveConsumer.Invalidate(result.Revision())
	liveConsumer.ObserveSnapshot(tap.NativeScreenSnapshot())
	historyConsumer.Consume(result.Transaction())

	if liveConsumer.rawPTYSeen || historyConsumer.rawPTYSeen {
		t.Fatalf("tap fan-out consumers must stay after parser boundary, live=%v history=%v", liveConsumer.rawPTYSeen, historyConsumer.rawPTYSeen)
	}
	if liveConsumer.revisions[0] != result.Revision() {
		t.Fatalf("live consumer should receive revision only, got %#v", liveConsumer.revisions)
	}
	if len(historyConsumer.transactions) != 1 || historyConsumer.transactions[0].Raw != "" {
		t.Fatalf("history consumer should receive sanitized semantic transaction only, got %#v", historyConsumer.transactions)
	}
}

func TestR372SemanticTapTransactionHandoffIsDeepCopied(t *testing.T) {
	tap := NewSemanticTap("term-r372", Size{Cols: 12, Rows: 3}, nil)
	result, err := tap.ApplyPTYWrite([]byte("\x1b[?2026hcopy-proof\r\n\x1b[?2026l"))
	if err != nil {
		t.Fatalf("apply PTY write: %v", err)
	}
	first := result.Transaction()
	if len(first.Ops) == 0 || len(first.Ops[1].Runs) == 0 || first.PrimaryFrame == nil || len(first.PrimaryFrame.Rows) == 0 || len(first.PrimaryFrame.Rows[0]) == 0 || len(first.SourceDamage.DirectDamageTouchedRows) == 0 {
		t.Fatalf("expected transaction with mutable-looking slices for copy test, got %#v", first)
	}
	first.Ops[1].Runs[0].Text = "X"
	first.PrimaryFrame.Rows[0][0].Content = "Y"
	first.SourceDamage.DirectDamageTouchedRows[0] = 99

	second := result.Transaction()
	if got := semanticTapFrameText(second.PrimaryFrame); strings.Contains(got, "Y") {
		t.Fatalf("mutating one transaction consumer must not affect another, got frame %q", got)
	}
	if second.Ops[1].Runs[0].Text == "X" || second.SourceDamage.DirectDamageTouchedRows[0] == 99 {
		t.Fatalf("transaction handoff should deep-copy op/source damage slices, got tx=%#v", second)
	}
}

func TestR378OrdinaryStressTransactionHandoffStaysLightweight(t *testing.T) {
	tap := NewSemanticTap("term-r378", Size{Cols: 120, Rows: 36}, nil)
	var payload strings.Builder
	for i := 0; i < 200; i++ {
		payload.WriteString("stress line ")
		payload.WriteString(strings.Repeat("x", 100))
		payload.WriteString("\r\n")
	}
	result, err := tap.ApplyPTYWrite([]byte(payload.String()))
	if err != nil {
		t.Fatalf("apply ordinary stress write: %v", err)
	}
	tx := result.Transaction()
	if len(tx.Ops) == 0 {
		t.Fatalf("ordinary stress must still expose ordered ops: %#v", tx)
	}
	if len(tx.PrimaryScrollOut) != 0 {
		t.Fatalf("ordinary stress must not duplicate ordered ops as transaction scroll-out proof: %#v", tx.PrimaryScrollOut)
	}
	if tx.PrimaryFrame != nil || tx.AltFrame != nil {
		t.Fatalf("ordinary stress handoff must not enqueue full screen frame side proof: %#v", tx.PrimaryFrame)
	}
	if len(tx.SourceDamage.Ops) != 0 || len(tx.SourceDamage.SemanticOps) != 0 || len(tx.SourceDamage.ScrollbackAppend) != 0 {
		t.Fatalf("ordinary stress handoff must not retain duplicate SourceDamage payload: %#v", tx.SourceDamage)
	}

	renderer := history.NewHistoryLogicalRenderer(nil, nil)
	batch, err := renderer.Apply(tx, history.HistoryDecision{
		Mode:                  history.HistoryOutputModeOrdinaryStream,
		ConsumeStreamOps:      true,
		ConsumeScrollOutProof: true,
	})
	if err != nil {
		t.Fatalf("ordinary lightweight tx must remain renderable: %v", err)
	}
	if len(batch.Mutations) == 0 {
		t.Fatalf("ordinary lightweight tx should still produce history mutations")
	}
	openLineUpserts := 0
	for _, mutation := range batch.Mutations {
		if mutation.Kind == history.HistoryMutationAppendTimelineRecord && mutation.Record != nil && mutation.Record.Kind == history.HistoryRecordPrimaryScrollOutLine {
			t.Fatalf("ordinary lightweight tx must not seal duplicate scroll-out records: %#v", batch.Mutations)
		}
		if mutation.Kind == history.HistoryMutationUpsertOpenLine {
			openLineUpserts++
		}
	}
	if openLineUpserts > 1 {
		t.Fatalf("ordinary stress tx must flush at most one mutable open line, got %d mutations=%d", openLineUpserts, len(batch.Mutations))
	}
}

func TestR386SemanticTapOrdinaryStressJournalHandoffAvoidsFullTransactionHotPath(t *testing.T) {
	tap := NewSemanticTap("term-r386", Size{Cols: 120, Rows: 36}, nil)
	var payload strings.Builder
	for i := 0; i < 200; i++ {
		payload.WriteString("journal stress line ")
		payload.WriteString(strings.Repeat("x", 80))
		payload.WriteString("\r\n")
	}
	result, err := tap.ApplyPTYWrite([]byte(payload.String()))
	if err != nil {
		t.Fatalf("apply ordinary stress write: %v", err)
	}
	journal := result.HistoryJournal()
	if journal.Source != history.HistoryJournalSourceSemanticTapTransaction {
		t.Fatalf("journal handoff must preserve semantic tap source, got %#v", journal)
	}
	if len(journal.Items) != 1 || journal.Items[0].Kind != history.HistoryJournalItemOrdinaryLineBatch || journal.Items[0].Ordinary == nil {
		t.Fatalf("ordinary stress should hand off one compact line batch, got %#v", journal.Items)
	}
	if got := len(journal.Items[0].Ordinary.Lines); got != 200 {
		t.Fatalf("ordinary stress journal should batch sealed lines, got %d", got)
	}
	if got := len(journal.Items[0].Ordinary.Commands); got != 0 {
		t.Fatalf("linear ordinary stress must not replay per-op commands, got %d", got)
	}
	if len(journal.Items[0].Ordinary.Lines[0].Runs) > 0 {
		journal.Items[0].Ordinary.Lines[0].Runs[0].Text = "mutated"
	} else {
		journal.Items[0].Ordinary.Lines[0].Cells[0].Text = "mutated"
	}
	again := result.HistoryJournal()
	got := ""
	if len(again.Items[0].Ordinary.Lines[0].Runs) > 0 {
		got = again.Items[0].Ordinary.Lines[0].Runs[0].Text
	} else {
		got = again.Items[0].Ordinary.Lines[0].Cells[0].Text
	}
	if got == "mutated" {
		t.Fatalf("journal handoff must deep-copy payload per consumer")
	}
}

func TestR407ChunkedStressJournalDoesNotCarryOpenUpdatesForCompleteLines(t *testing.T) {
	tap := NewSemanticTap("term-r407-chunked", Size{Cols: 120, Rows: 30}, nil)
	payload, _ := r407OrdinaryStressPayload(1000)
	remaining := payload
	var sealedLines int
	var openUpdates int
	var journals int
	for len(remaining) > 0 {
		head, tail := splitLiveIngestPayload(remaining, terminalHistoryTapIngestBatchMaxBytes)
		result, err := tap.ApplyPTYWrite([]byte(head))
		if err != nil {
			t.Fatalf("tap write: %v", err)
		}
		journals++
		journal := result.HistoryJournal()
		for itemIndex, item := range journal.Items {
			if item.Ordinary == nil {
				continue
			}
			sealedLines += len(item.Ordinary.Lines)
			if len(item.Ordinary.Lines) > 0 && len(item.Ordinary.Commands) > 0 {
				t.Fatalf("complete-line stress batch must not replay commands, journal=%d item=%d lines=%d commands=%d open=%v commandKinds=%v", journals, itemIndex, len(item.Ordinary.Lines), len(item.Ordinary.Commands), item.Ordinary.OpenUpdate != nil, r407CommandKinds(item.Ordinary.Commands))
			}
			if item.Ordinary.OpenUpdate != nil {
				openUpdates++
			}
		}
		remaining = tail
	}
	if sealedLines != 1000 {
		t.Fatalf("chunked stress should produce one sealed logical line per input line, got %d journals=%d openUpdates=%d", sealedLines, journals, openUpdates)
	}
}

func r407CommandKinds(commands []history.JournalOpenLineCommand) []string {
	out := make([]string, 0, minInt(len(commands), 12))
	for index, command := range commands {
		if index >= 12 {
			out = append(out, "...")
			break
		}
		out = append(out, string(command.Kind))
	}
	return out
}

func TestR389SemanticTapBuildsHistoryJournalOnlyOnHistoryFanout(t *testing.T) {
	tap := NewSemanticTap("term-r389", Size{Cols: 120, Rows: 36}, nil)
	builds := 0
	previousHook := history.HistoryJournalBuildHook
	history.HistoryJournalBuildHook = func() { builds++ }
	defer func() { history.HistoryJournalBuildHook = previousHook }()

	result, err := tap.ApplyPTYWrite([]byte("lazy journal\r\n"))
	if err != nil {
		t.Fatalf("apply PTY write: %v", err)
	}
	if result.Revision() != 1 || builds != 0 {
		t.Fatalf("tap write must not build history journal before live wake, revision=%d builds=%d", result.Revision(), builds)
	}
	journal := result.HistoryJournal()
	if builds != 1 || len(journal.Items) == 0 {
		t.Fatalf("history fanout should build compact journal on demand, builds=%d journal=%#v", builds, journal)
	}
	resized, err := tap.Resize(Size{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	if resized.Revision() != 2 || builds != 1 {
		t.Fatalf("tap resize must also avoid eager journal build, revision=%d builds=%d", resized.Revision(), builds)
	}
	_ = resized.HistoryJournal()
	if builds != 2 {
		t.Fatalf("resize journal should build only when requested, builds=%d", builds)
	}
}

func TestR381SemanticTapSnapshotHandoffIsPulledOnDemand(t *testing.T) {
	tap := NewSemanticTap("term-r372", Size{Cols: 12, Rows: 3}, nil)
	if _, err := tap.ApplyPTYWrite([]byte("snapshot\r\n")); err != nil {
		t.Fatalf("apply PTY write: %v", err)
	}
	first := tap.NativeScreenSnapshot()
	if len(first.Rows) == 0 || len(first.Rows[0].Cells) == 0 {
		t.Fatalf("expected snapshot cells for copy test, got %#v", first)
	}
	first.Rows[0].Cells[0].Content = "X"
	second := tap.NativeScreenSnapshot()
	if semanticTapSnapshotText(second) == "" || second.Rows[0].Cells[0].Content == "X" {
		t.Fatalf("mutating one live snapshot consumer must not affect another, got %#v", second.Rows[0].Cells)
	}
}

func TestR381SemanticTapWriteAndResizeDoNotBuildSnapshot(t *testing.T) {
	tap := NewSemanticTap("term-r381", Size{Cols: 12, Rows: 3}, nil)
	snapshots := 0
	previousHook := semanticTapSnapshotBuildHook
	semanticTapSnapshotBuildHook = func() { snapshots++ }
	defer func() { semanticTapSnapshotBuildHook = previousHook }()

	result, err := tap.ApplyPTYWrite([]byte("snapshot\r\n"))
	if err != nil {
		t.Fatalf("apply PTY write: %v", err)
	}
	if result.Revision() != 1 || snapshots != 0 {
		t.Fatalf("write result must only carry revision/transaction, revision=%d snapshots=%d", result.Revision(), snapshots)
	}
	resized, err := tap.Resize(Size{Cols: 10, Rows: 4})
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	if resized.Revision() != 2 || snapshots != 0 {
		t.Fatalf("resize result must not build native snapshot, revision=%d snapshots=%d", resized.Revision(), snapshots)
	}
	snapshot := tap.NativeScreenSnapshot()
	if snapshot.Revision != 2 || snapshots != 1 {
		t.Fatalf("snapshot should be built only when pulled, revision=%d snapshots=%d", snapshot.Revision, snapshots)
	}
}

type r372TransactionOnlyHistoryConsumer struct {
	transactions []vterm.TerminalSemanticTransaction
	rawPTY       []string
	rawPTYSeen   bool
}

func (consumer *r372TransactionOnlyHistoryConsumer) Consume(tx vterm.TerminalSemanticTransaction) {
	consumer.transactions = append(consumer.transactions, tx)
}

type r372LiveProjectionConsumer struct {
	revisions  []LiveRevision
	snapshots  []NativeScreenSnapshot
	rawPTYSeen bool
}

func (consumer *r372LiveProjectionConsumer) Invalidate(revision LiveRevision) {
	consumer.revisions = append(consumer.revisions, revision)
}

func (consumer *r372LiveProjectionConsumer) ObserveSnapshot(snapshot NativeScreenSnapshot) {
	consumer.snapshots = append(consumer.snapshots, snapshot)
}

func semanticTapSnapshotText(snapshot NativeScreenSnapshot) string {
	var builder strings.Builder
	for rowIndex, row := range snapshot.Rows {
		if rowIndex > 0 {
			builder.WriteByte('\n')
		}
		for _, cell := range row.Cells {
			builder.WriteString(cell.Content)
		}
	}
	return builder.String()
}

func semanticTapFrameText(frame *vterm.TerminalSemanticFrame) string {
	if frame == nil {
		return ""
	}
	var builder strings.Builder
	for rowIndex, row := range frame.Rows {
		if rowIndex > 0 {
			builder.WriteByte('\n')
		}
		for _, cell := range row {
			builder.WriteString(cell.Content)
		}
	}
	return builder.String()
}
