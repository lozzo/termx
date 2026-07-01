package termxcorev2

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-core-v2/live"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestTerminalLifecycleAndLiveSurface(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	info, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 10, Rows: 3},
	})
	if err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if info.State != TerminalStateRunning {
		t.Fatalf("unexpected state %q", info.State)
	}
	process := factory.process("term-1")
	if process == nil {
		t.Fatal("expected process to be spawned")
	}
	if err := server.WriteInput(context.Background(), "term-1", []byte("echo hi\n")); err != nil {
		t.Fatalf("write input: %v", err)
	}
	inputs, _, _, _ := process.snapshot()
	if got := inputs[0]; string(got) != "echo hi\n" {
		t.Fatalf("unexpected input %q", string(got))
	}
	if err := server.IngestOutput(context.Background(), "term-1", "hello\nworld"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	rows, err := server.LiveRows("term-1")
	if err != nil {
		t.Fatalf("live rows: %v", err)
	}
	if len(rows) != 2 || rows[0] != "hello" || !strings.Contains(rows[1], "world") {
		t.Fatalf("unexpected live rows %#v", rows)
	}
	if err := server.ResizeTerminal(context.Background(), "term-1", 20, 5); err != nil {
		t.Fatalf("resize: %v", err)
	}
	_, resizes, _, _ := process.snapshot()
	if got := resizes[0]; got != (Size{Cols: 20, Rows: 5}) {
		t.Fatalf("unexpected resize %#v", got)
	}
	info, err = server.GetTerminal("term-1")
	if err != nil {
		t.Fatalf("get terminal after resize: %v", err)
	}
	if info.Size != (Size{Cols: 20, Rows: 5}) {
		t.Fatalf("expected registry size to follow resize, got %#v", info.Size)
	}
}

func TestR373ResizeSerializesProcessOutputWithTapResize(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r373-resize-order",
		Command: []string{"shell"},
		Size:    Size{Cols: 5, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process("term-r373-resize-order")
	if process == nil {
		t.Fatal("expected process to be spawned")
	}
	process.setResizeHook(func(size Size) {
		process.emitOutput("\x1b[2Jabcdefg")
	})
	if err := server.ResizeTerminal(context.Background(), "term-r373-resize-order", 8, 3); err != nil {
		t.Fatalf("resize terminal: %v", err)
	}
	terminal, err := server.Terminal("term-r373-resize-order")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	assertEventually(t, time.Second, func() bool {
		snapshot := terminal.NativeScreenSnapshot("term-r373-resize-order")
		return snapshot.Size.Cols == 8 && len(snapshot.Rows) > 0 && len(snapshot.Rows[0].Cells) >= 7
	}, "resize output should be applied after tap resize")
	snapshot := terminal.NativeScreenSnapshot("term-r373-resize-order")
	if snapshot.Size.Cols != 8 {
		t.Fatalf("tap latest screen should use resized width, got %#v", snapshot.Size)
	}
	if got := len(snapshot.Rows[0].Cells); got != 7 {
		t.Fatalf("resize-triggered output should not wrap at old width, row cells=%d row=%#v", got, snapshot.Rows[0])
	}
}

func TestR405TerminalResizeQueuesDecisionAwareBoundaryJournal(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r405-resize-journal",
		Command: []string{"shell"},
		Size:    Size{Cols: 6, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	terminal, err := server.Terminal("term-r405-resize-journal")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	queue := newTerminalHistoryIngestQueue(0)
	defer queue.Close()
	terminal.queueMu.Lock()
	terminal.historyQ = queue
	terminal.queueMu.Unlock()

	if err := server.ResizeTerminal(context.Background(), "term-r405-resize-journal", 12, 4); err != nil {
		t.Fatalf("resize terminal: %v", err)
	}
	batch, ok := queue.nextBatch()
	if !ok || len(batch) != 1 {
		t.Fatalf("expected one resize journal batch, ok=%v batch=%#v", ok, batch)
	}
	journal := batch[0].journal
	labels := historyJournalLabelsForTerminalTest(journal)
	if got := strings.Join(labels, "|"); got != "boundary:resize" {
		t.Fatalf("resize must enqueue boundary-only decision journal, got labels=%q journal=%#v", got, journal)
	}
	for _, item := range journal.Items {
		if item.Kind == history.HistoryJournalItemOrdinaryLineBatch || item.Kind == history.HistoryJournalItemFrameEvent || item.Kind == history.HistoryJournalItemScrollOutProof {
			t.Fatalf("resize journal must not carry ordinary/frame/scroll-out payload, labels=%v item=%#v", labels, item)
		}
	}
}

func TestTerminalLiveSurfaceRepliesToOSCBackgroundQuery(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-osc-query",
		Command: []string{"shell"},
		Size:    Size{Cols: 80, Rows: 24},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process("term-osc-query")
	if process == nil {
		t.Fatal("expected process to be spawned")
	}
	if err := server.IngestOutput(context.Background(), "term-osc-query", "\x1b]11;?\x1b\\"); err != nil {
		t.Fatalf("ingest OSC background query: %v", err)
	}
	assertEventually(t, time.Second, func() bool {
		inputs, _, _, _ := process.snapshot()
		for _, input := range inputs {
			if strings.Contains(string(input), "\x1b]11;") {
				return true
			}
		}
		return false
	}, "expected live terminal query response to be written back to process input")
}

func TestTerminalIngestOutputPublishesLiveInvalidatedEvent(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventTerminalLiveInvalidated}})
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "live update\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	event := assertEventValue(t, events, EventTerminalLiveInvalidated, "term-1")
	if event.Live == nil || event.Live.Revision == 0 {
		t.Fatalf("expected live invalidation revision, got %#v", event)
	}
}

func TestTerminalHistoryDisabledUsesNativeScreenOnlyWritePath(t *testing.T) {
	historyDir := t.TempDir()
	server := NewServer(
		WithProcessFactory(newRecordingProcessFactory()),
		WithHistoryStorageDir(historyDir),
		WithHistoryDisabled(),
	)
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-live-only",
		Command: []string{"shell"},
		Size:    Size{Cols: 24, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-live-only", "alpha\r\nbeta\r\nlatest"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	if _, err := os.Stat(filepath.Join(historyDir, "term-live-only.history-lines.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("history disabled must not write payload file, err=%v", err)
	}
	snapshot, err := server.LiveSnapshot("term-live-only")
	if err != nil {
		t.Fatalf("live snapshot: %v", err)
	}
	if got := strings.Join(liveSnapshotRows(snapshot), "\n"); !strings.Contains(got, "latest") {
		t.Fatalf("native screen must still update in history disabled mode, got %q", got)
	}
}

func TestR396ProcessOutputFansOutLiveSurfaceAndHistorySemanticConsumer(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	events := server.Events(context.Background(), EventFilter{
		TerminalID: "term-r373-fanout",
		Types:      []EventType{EventTerminalLiveInvalidated},
	})
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r373-fanout",
		Command: []string{"shell"},
		Size:    Size{Cols: 30, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process("term-r373-fanout")
	if process == nil {
		t.Fatal("expected recording process")
	}
	process.emitOutput("alpha\r\nbeta\r\n")
	event := assertEventValue(t, events, EventTerminalLiveInvalidated, "term-r373-fanout")
	if event.Live == nil || event.Live.Revision == 0 {
		t.Fatalf("expected live invalidation from live SurfaceTrack output, got %#v", event)
	}

	rows, err := server.LiveRows("term-r373-fanout")
	if err != nil {
		t.Fatalf("live rows: %v", err)
	}
	if got := strings.Join(rows, "|"); !strings.Contains(got, "beta") {
		t.Fatalf("process output must update latest native screen, rows=%#v", rows)
	}
	window, err := server.TerminalHistoryWindow(context.Background(), "term-r373-fanout", history.HistoryWindowRequest{
		TerminalID: "term-r373-fanout",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       30,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history.window should flush history semantic backlog: %v", err)
	}
	if got := strings.Join(historyRowTexts(window.Rows), "|"); got != "alpha|beta" {
		t.Fatalf("history queue must consume tap semantic transaction, got %q window=%#v", got, window)
	}
	terminal, err := server.Terminal("term-r373-fanout")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	records := terminal.tap.InputRecords()
	if len(records) != 1 || records[0].Kind != SemanticTapInputWrite {
		t.Fatalf("history semantic consumer must receive process output once, records=%#v", records)
	}
}

func TestR389ProcessOutputPublishesLiveBeforeBlockedHistoryFanout(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	events := server.Events(context.Background(), EventFilter{
		TerminalID: "term-r389-live-before-history",
		Types:      []EventType{EventTerminalLiveInvalidated},
	})
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r389-live-before-history",
		Command: []string{"shell"},
		Size:    Size{Cols: 30, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	terminal, err := server.Terminal("term-r389-live-before-history")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	store := newBlockingHistoryStore("term-r389-live-before-history")
	store.block()
	terminal.historyMu.Lock()
	terminal.historyStore = store
	terminal.historyMu.Unlock()

	process := factory.process("term-r389-live-before-history")
	if process == nil {
		t.Fatal("expected recording process")
	}
	process.emitOutput("alpha\r\n")
	event := assertEventValue(t, events, EventTerminalLiveInvalidated, "term-r389-live-before-history")
	if event.Live == nil || event.Live.Revision == 0 {
		t.Fatalf("expected live invalidation before releasing history worker, got %#v", event)
	}
	select {
	case <-store.applyStarted:
	case <-time.After(time.Second):
		t.Fatal("history worker did not reach blocked Apply")
	}
	store.release()
	window, err := server.TerminalHistoryWindow(context.Background(), "term-r389-live-before-history", history.HistoryWindowRequest{
		TerminalID: "term-r389-live-before-history",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       30,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history.window after release: %v", err)
	}
	if got := strings.Join(historyRowTexts(window.Rows), "|"); got != "alpha" {
		t.Fatalf("history should still receive journal after release, got %q", got)
	}
}

func TestR404TerminalJournalPipelineHandlesOrdinaryFrameAndPrompt(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r404-journal-pipeline",
		Command: []string{"shell"},
		Size:    Size{Cols: 30, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r404-journal-pipeline", "plain-1\r\nplain-2\r\n"); err != nil {
		t.Fatalf("ingest ordinary output: %v", err)
	}
	window, err := server.TerminalHistoryWindow(context.Background(), "term-r404-journal-pipeline", history.HistoryWindowRequest{
		TerminalID: "term-r404-journal-pipeline",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       30,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("latest ordinary window: %v", err)
	}
	if got := strings.Join(historyRowTexts(window.Rows), "|"); got != "plain-1|plain-2" {
		t.Fatalf("journal pipeline must preserve sealed lines, got %q rows=%#v", got, window.Rows)
	}
	if !window.Rows[0].Committed || !window.Rows[1].Committed {
		t.Fatalf("journal pipeline should preserve sealed ownership, rows=%#v", window.Rows)
	}

	if err := server.IngestOutput(context.Background(), "term-r404-journal-pipeline", "\x1b[?2026hframe\r\ncurrent\x1b[?2026l"); err != nil {
		t.Fatalf("ingest complex output: %v", err)
	}
	window, err = server.TerminalHistoryWindow(context.Background(), "term-r404-journal-pipeline", history.HistoryWindowRequest{
		TerminalID: "term-r404-journal-pipeline",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       30,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("latest after frame journal: %v", err)
	}
	texts := strings.Join(historyRowTexts(window.Rows), "|")
	if !strings.Contains(texts, "plain-1|plain-2") || !historyRowsContainSegment(window.Rows, history.HistorySegmentCurrentPrimaryFrame) {
		t.Fatalf("frame journal should preserve prior ordinary rows without id/order corruption, texts=%q rows=%#v", texts, window.Rows)
	}

	if err := server.IngestOutput(context.Background(), "term-r404-journal-pipeline", "PROMPT_AFTER"); err != nil {
		t.Fatalf("ingest prompt after frame: %v", err)
	}
	window, err = server.TerminalHistoryWindow(context.Background(), "term-r404-journal-pipeline", history.HistoryWindowRequest{
		TerminalID: "term-r404-journal-pipeline",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       30,
		Limit:      20,
	})
	if err != nil {
		t.Fatalf("latest after prompt journal: %v", err)
	}
	textRows := historyRowTexts(window.Rows)
	promptIndex := historyTextIndex(textRows, "PROMPT_AFTER")
	frameIndex := historyTextIndex(textRows, "current")
	if promptIndex < 0 || frameIndex < 0 || promptIndex < frameIndex {
		t.Fatalf("journal pipeline must close frame before ordinary prompt, prompt=%d frame=%d rows=%#v", promptIndex, frameIndex, window.Rows)
	}
}

func TestR404TerminalJournalRendererAcceptsOpenLineAndBoundaryCommands(t *testing.T) {
	renderer := history.NewHistoryJournalRenderer()
	sealed := history.HistoryJournalFromDecision("term-r404-gate", history.TerminalSemanticTransaction{
		Seq:  1,
		Size: history.TerminalSemanticSize{Cols: 30, Rows: 4},
		Ops: []history.TerminalSemanticOp{
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: []history.TerminalSemanticCell{{Content: "sealed", Width: 6}}},
			{Code: vterm.ScreenOpControl, Control: "lf"},
		},
	}, history.HistoryDecision{Mode: history.HistoryOutputModeOrdinaryStream})
	if _, err := renderer.ApplyJournal(sealed); err != nil {
		t.Fatalf("sealed ordinary journal should apply: %v journal=%#v", err, sealed)
	}

	open := history.HistoryJournalFromDecision("term-r404-gate", history.TerminalSemanticTransaction{
		Seq:  2,
		Size: history.TerminalSemanticSize{Cols: 30, Rows: 4},
		Ops:  []history.TerminalSemanticOp{{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: []history.TerminalSemanticCell{{Content: "open", Width: 4}}}},
	}, history.HistoryDecision{Mode: history.HistoryOutputModeOrdinaryStream})
	if _, err := renderer.ApplyJournal(open); err != nil {
		t.Fatalf("open-line journal should apply through stream reducer: %v journal=%#v", err, open)
	}

	lfOnly := history.HistoryJournalFromDecision("term-r404-gate", history.TerminalSemanticTransaction{
		Seq:  3,
		Size: history.TerminalSemanticSize{Cols: 30, Rows: 4},
		Ops:  []history.TerminalSemanticOp{{Code: vterm.ScreenOpControl, Control: "lf"}},
	}, history.HistoryDecision{Mode: history.HistoryOutputModeOrdinaryStream})
	batch, err := renderer.ApplyJournal(lfOnly)
	if err != nil {
		t.Fatalf("LF-only journal should seal renderer-owned open line: %v journal=%#v", err, lfOnly)
	}
	if got := strings.Join(historyRowTextsFromLinesForTest(batch.Mutations), "|"); got != "open" {
		t.Fatalf("LF-only journal should seal open line, got %q batch=%#v", got, batch)
	}
}

func TestR386TerminalJournalQueueGateAvoidsTransactionClassifierForPlainSealedBatch(t *testing.T) {
	plain := history.HistoryJournal{
		TerminalID: "term-r386-gate",
		Source:     history.HistoryJournalSourceSemanticTapTransaction,
		Items: []history.HistoryJournalItem{{
			Kind: history.HistoryJournalItemOrdinaryLineBatch,
			Ordinary: &history.OrdinaryLineBatch{
				Lines: []history.JournalLogicalLine{{Cells: []history.Cell{{Text: "plain", Width: 5}}}},
			},
		}},
	}
	if !historyJournalAllowsTerminalQueue(plain) {
		t.Fatalf("plain sealed ordinary journal should enter backlog without synchronous journal apply")
	}
	withCommands := history.HistoryJournalFromTransaction("term-r386-gate", history.TerminalSemanticTransaction{
		Ops: []history.TerminalSemanticOp{
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: []history.TerminalSemanticCell{{Content: "plain", Width: 5}}},
			{Code: vterm.ScreenOpControl, Control: "lf"},
		},
	})
	if !historyJournalAllowsTerminalQueue(withCommands) {
		t.Fatalf("ordinary command journal should enter backlog without synchronous journal apply")
	}
	frame := history.HistoryJournalFromTransaction("term-r386-gate", history.TerminalSemanticTransaction{
		PrimaryFrame: &history.TerminalSemanticFrame{Cols: 20, Rows: [][]history.TerminalSemanticCell{{{Content: "frame", Width: 5}}}},
	})
	if historyJournalAllowsTerminalQueue(frame) {
		t.Fatalf("frame journal must force synchronous journal apply after backlog flush")
	}
}

func TestR404TerminalJournalRendererAppliesBoundaryProofAndFramePayload(t *testing.T) {
	renderer := history.NewHistoryJournalRenderer()
	boundaryOnly := history.HistoryJournalFromDecision("term-r404-renderer", history.TerminalSemanticTransaction{
		Seq:  1,
		Size: history.TerminalSemanticSize{Cols: 30, Rows: 4},
		Ops: []history.TerminalSemanticOp{
			{Code: vterm.ScreenOpControl, Control: "ed", Mode: 3},
			{Code: vterm.ScreenOpResize, Size: vterm.Size{Cols: 40, Rows: 6}},
		},
	}, history.HistoryDecision{Mode: history.HistoryOutputModeBoundaryOnly, NonHistoryBoundary: true})
	if _, err := renderer.ApplyJournal(boundaryOnly); err != nil {
		t.Fatalf("boundary-only journal should apply: %v journal=%#v", err, boundaryOnly)
	}

	withScrollOut := history.HistoryJournalFromDecision("term-r404-renderer", history.TerminalSemanticTransaction{
		Seq:  2,
		Size: history.TerminalSemanticSize{Cols: 30, Rows: 4},
		Ops: []history.TerminalSemanticOp{{
			Code:      vterm.ScreenOpControl,
			Control:   "ed",
			Mode:      2,
			ScrollOut: []vterm.ScrollbackRowAppend{{Runs: []vterm.CellRun{{Text: "proof"}}}},
		}},
	}, history.HistoryDecision{Mode: history.HistoryOutputModePrimaryFrameSession, ConsumeScrollOutProof: true, ConsumeClearTimeScrollOutProof: true, ConsumeClearBoundary: true})
	if _, err := renderer.ApplyJournal(withScrollOut); err != nil {
		t.Fatalf("scroll-out proof journal should apply: %v journal=%#v", err, withScrollOut)
	}

	withFrame := history.HistoryJournalFromDecision("term-r404-renderer", history.TerminalSemanticTransaction{
		Seq:                     3,
		Size:                    history.TerminalSemanticSize{Cols: 30, Rows: 4},
		SynchronizedBegin:       true,
		SynchronizedEnd:         true,
		PrimaryFrameTouchedRows: []int{0},
		PrimaryFrame:            &history.TerminalSemanticFrame{Cols: 30, Rows: [][]history.TerminalSemanticCell{{{Content: "f", Width: 1}}}},
		Ops: []history.TerminalSemanticOp{
			{Code: vterm.ScreenOpModes, Private: true, Mode: 2026, Enabled: true},
			{Code: vterm.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: []history.TerminalSemanticCell{{Content: "f", Width: 1}}},
			{Code: vterm.ScreenOpModes, Private: true, Mode: 2026, Enabled: false},
		},
	}, history.HistoryDecision{Mode: history.HistoryOutputModePrimaryFrameSession, PublishPrimaryFrame: true})
	if _, err := renderer.ApplyJournal(withFrame); err != nil {
		t.Fatalf("frame payload journal should apply: %v journal=%#v", err, withFrame)
	}
}

func TestR404TerminalBoundaryAndFrameJournalThenPrompt(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r404-boundary-frame",
		Command: []string{"shell"},
		Size:    Size{Cols: 32, Rows: 5},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r404-boundary-frame", "old-a\r\n\x1b[3J\x1bcnew-a\r\n"); err != nil {
		t.Fatalf("ingest boundary journal output: %v", err)
	}
	window, err := server.TerminalHistoryWindow(context.Background(), "term-r404-boundary-frame", history.HistoryWindowRequest{
		TerminalID: "term-r404-boundary-frame",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       40,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("latest after boundary journal: %v", err)
	}
	if got := strings.Join(historyRowTexts(window.Rows), "|"); got != "old-a|new-a" {
		t.Fatalf("boundary journal should preserve sealed ordinary history, got %q rows=%#v", got, window.Rows)
	}

	if err := server.IngestOutput(context.Background(), "term-r404-boundary-frame", "\x1b[?2026hframe-a\r\nframe-b\x1b[?2026l"); err != nil {
		t.Fatalf("ingest sync frame journal: %v", err)
	}
	window, err = server.TerminalHistoryWindow(context.Background(), "term-r404-boundary-frame", history.HistoryWindowRequest{
		TerminalID: "term-r404-boundary-frame",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       40,
		Limit:      20,
	})
	if err != nil {
		t.Fatalf("latest after frame journal: %v", err)
	}
	texts := strings.Join(historyRowTexts(window.Rows), "|")
	if !strings.Contains(texts, "old-a|new-a") || !historyRowsContainSegment(window.Rows, history.HistorySegmentCurrentPrimaryFrame) {
		t.Fatalf("frame journal should publish current primary frame, texts=%q rows=%#v", texts, window.Rows)
	}
	if err := server.IngestOutput(context.Background(), "term-r404-boundary-frame", "PROMPT_AFTER"); err != nil {
		t.Fatalf("ingest prompt journal: %v", err)
	}
	window, err = server.TerminalHistoryWindow(context.Background(), "term-r404-boundary-frame", history.HistoryWindowRequest{
		TerminalID: "term-r404-boundary-frame",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       40,
		Limit:      20,
	})
	if err != nil {
		t.Fatalf("latest after prompt journal: %v", err)
	}
	textRows := historyRowTexts(window.Rows)
	promptIndex := historyTextIndex(textRows, "PROMPT_AFTER")
	frameIndex := historyTextIndex(textRows, "frame-b")
	if promptIndex < 0 || frameIndex < 0 || promptIndex < frameIndex {
		t.Fatalf("prompt after journal frame must close frame through journal reducer, prompt=%d frame=%d rows=%#v", promptIndex, frameIndex, window.Rows)
	}
}

func TestR374HistoryBacklogStatusAndFlushBoundary(t *testing.T) {
	factory := newRecordingProcessFactory()
	store := newBlockingHistoryStore("term-r374-backlog")
	server := NewServer(
		WithProcessFactory(factory),
		WithHistoryStoreFactory(func(terminalID string) (history.HistoryStore, error) {
			if terminalID != "term-r374-backlog" {
				return history.NewInMemoryHistoryStore(terminalID), nil
			}
			return store, nil
		}),
	)
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r374-backlog",
		Command: []string{"shell"},
		Size:    Size{Cols: 30, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process("term-r374-backlog")
	if process == nil {
		t.Fatal("expected recording process")
	}
	store.block()
	process.emitOutput("alpha\r\n")
	select {
	case <-store.applyStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for blocked history apply")
	}
	status, err := server.TerminalHistoryBacklogStatus("term-r374-backlog")
	if err != nil {
		t.Fatalf("history backlog status: %v", err)
	}
	if !status.HistoryEnabled || status.AppliedSeq != 0 || status.TargetSeq != 1 || !status.CatchupPending || !status.InFlight {
		t.Fatalf("unexpected blocked backlog status %#v", status)
	}
	if rows, err := server.LiveRows("term-r374-backlog"); err != nil || !strings.Contains(strings.Join(rows, "|"), "alpha") {
		t.Fatalf("live hot path should read latest screen without waiting for history backlog, rows=%#v err=%v", rows, err)
	}
	windowDone := make(chan error, 1)
	go func() {
		_, err := server.TerminalHistoryWindow(context.Background(), "term-r374-backlog", history.HistoryWindowRequest{
			TerminalID: "term-r374-backlog",
			Mode:       history.HistoryWindowModeLatest,
			Cols:       30,
			Limit:      10,
		})
		windowDone <- err
	}()
	select {
	case err := <-windowDone:
		t.Fatalf("authoritative history.window returned before backlog catchup: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	store.release()
	select {
	case err := <-windowDone:
		if err != nil {
			t.Fatalf("history.window after catchup: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for history.window after backlog release")
	}
	status, err = server.TerminalHistoryBacklogStatus("term-r374-backlog")
	if err != nil {
		t.Fatalf("history backlog status after catchup: %v", err)
	}
	if status.AppliedSeq != 1 || status.TargetSeq != 1 || status.CatchupPending {
		t.Fatalf("unexpected caught-up backlog status %#v", status)
	}
}

func TestR376CopyEntryProjectionDoesNotFlushHistoryBacklog(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r376-copy-entry",
		Command: []string{"shell"},
		Size:    Size{Cols: 40, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r376-copy-entry", "applied\r\n"); err != nil {
		t.Fatalf("ingest applied output: %v", err)
	}
	if _, err := server.TerminalHistoryWindow(context.Background(), "term-r376-copy-entry", history.HistoryWindowRequest{
		TerminalID: "term-r376-copy-entry",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       40,
		Limit:      10,
	}); err != nil {
		t.Fatalf("initial history.window: %v", err)
	}
	terminal, err := server.Terminal("term-r376-copy-entry")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	queue := newTerminalHistoryIngestQueue(1)
	if !queue.Enqueue(r385HistoryQueueJournal(1, "pending")) {
		t.Fatal("expected pending history journal")
	}
	defer queue.Close()
	terminal.queueMu.Lock()
	terminal.historyQ = queue
	terminal.queueMu.Unlock()

	done := make(chan history.CopyEntryProjection, 1)
	errCh := make(chan error, 1)
	go func() {
		projection, err := server.TerminalHistoryCopyEntryProjection(context.Background(), "term-r376-copy-entry", history.CopyEntryProjectionRequest{
			TerminalID: "term-r376-copy-entry",
			Cols:       40,
			Limit:      10,
		})
		if err != nil {
			errCh <- err
			return
		}
		done <- projection
	}()
	select {
	case err := <-errCh:
		t.Fatalf("copy entry projection: %v", err)
	case projection := <-done:
		if got := strings.Join(historyRowTexts(projection.Window.Rows), "|"); got != "applied" {
			t.Fatalf("copy entry must return already-applied frontier without waiting, got %q projection=%#v", got, projection)
		}
		if !projection.CatchupPending || projection.AppliedHistorySeq != 1 || projection.TargetHistorySeq != 2 {
			t.Fatalf("projection must expose backlog catchup boundary, got %#v", projection)
		}
		if projection.NativeCols != 40 || projection.Window.Token != "" {
			t.Fatalf("projection must carry native cols and no frozen token, got %#v", projection)
		}
		if projection.Capabilities.Copyable || projection.Capabilities.Searchable || projection.Capabilities.Pageable {
			t.Fatalf("catchup projection must not claim complete copy/search/page capabilities, got %#v", projection.Capabilities)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("copy entry projection must not wait for full history backlog flush")
	}
}

func TestR374HistoryBacklogSeqDoesNotResetAcrossRestart(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	events := server.Events(context.Background(), EventFilter{
		TerminalID: "term-r374-restart-seq",
		Types:      []EventType{EventTerminalLiveInvalidated},
	})
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r374-restart-seq",
		Command: []string{"shell"},
		Size:    Size{Cols: 30, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	first := factory.process("term-r374-restart-seq")
	if first == nil {
		t.Fatal("expected first process")
	}
	first.emitOutput("alpha\r\n")
	assertEventValue(t, events, EventTerminalLiveInvalidated, "term-r374-restart-seq")
	assertEventually(t, time.Second, func() bool {
		rows, err := server.LiveRows("term-r374-restart-seq")
		return err == nil && strings.Contains(strings.Join(rows, "|"), "alpha")
	}, "first output should reach live screen")
	if _, err := server.TerminalHistoryWindow(context.Background(), "term-r374-restart-seq", history.HistoryWindowRequest{
		TerminalID: "term-r374-restart-seq",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       30,
		Limit:      10,
	}); err != nil {
		t.Fatalf("history.window before restart: %v", err)
	}
	before, err := server.TerminalHistoryBacklogStatus("term-r374-restart-seq")
	if err != nil {
		t.Fatalf("status before restart: %v", err)
	}
	if before.AppliedSeq == 0 || before.TargetSeq == 0 {
		t.Fatalf("expected non-zero backlog seq before restart, status=%#v", before)
	}
	first.exit(0)
	waitForTerminalState(t, server, "term-r374-restart-seq", TerminalStateExited)
	if err := server.RestartTerminal(context.Background(), "term-r374-restart-seq"); err != nil {
		t.Fatalf("restart terminal: %v", err)
	}
	second := factory.process("term-r374-restart-seq")
	if second == nil || second == first {
		t.Fatal("expected second process")
	}
	second.emitOutput("beta\r\n")
	assertEventually(t, time.Second, func() bool {
		rows, err := server.LiveRows("term-r374-restart-seq")
		return err == nil && strings.Contains(strings.Join(rows, "|"), "beta")
	}, "second output should reach live screen")
	if _, err := server.TerminalHistoryWindow(context.Background(), "term-r374-restart-seq", history.HistoryWindowRequest{
		TerminalID: "term-r374-restart-seq",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       30,
		Limit:      10,
	}); err != nil {
		t.Fatalf("history.window after restart: %v", err)
	}
	after, err := server.TerminalHistoryBacklogStatus("term-r374-restart-seq")
	if err != nil {
		t.Fatalf("status after restart: %v", err)
	}
	if after.AppliedSeq <= before.AppliedSeq || after.TargetSeq <= before.TargetSeq {
		t.Fatalf("history backlog seq must remain terminal-scoped across restart, before=%#v after=%#v", before, after)
	}
}

func TestR396RestartFlushesPendingLiveQueueBeforePreservingScreen(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-r396-restart-live", Command: []string{"shell"}, Size: Size{Cols: 30, Rows: 4}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	terminal, err := server.Terminal("term-r396-restart-live")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	first := factory.process("term-r396-restart-live")
	if first == nil {
		t.Fatal("expected first process")
	}
	queue := newTerminalLiveIngestQueue()
	terminal.setLiveQueue(first, queue)
	defer func() {
		queue.Close()
		queue.Wait()
		terminal.clearLiveQueue(first, queue)
	}()
	queue.Enqueue("pending-before-restart\r\n")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	go queue.Run(func(output string) error {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return terminal.ingestProcessLiveOutput(first, output)
	})
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for live queue worker: %v", ctx.Err())
	}
	done := make(chan error, 1)
	go func() {
		done <- server.RestartTerminal(ctx, "term-r396-restart-live")
	}()
	select {
	case err := <-done:
		t.Fatalf("restart returned before pending live queue write was released: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("restart terminal: %v", err)
	}
	rows, err := server.LiveRows("term-r396-restart-live")
	if err != nil {
		t.Fatalf("live rows after restart: %v", err)
	}
	if !strings.Contains(strings.Join(rows, "|"), "pending-before-restart") {
		t.Fatalf("restart must preserve live queue output that was already enqueued, rows=%#v", rows)
	}
}

func TestR396HistoryDisabledProcessOutputKeepsLiveOnlyFastPath(t *testing.T) {
	historyDir := t.TempDir()
	factory := newRecordingProcessFactory()
	server := NewServer(
		WithProcessFactory(factory),
		WithHistoryStorageDir(historyDir),
		WithHistoryDisabled(),
	)
	events := server.Events(context.Background(), EventFilter{
		TerminalID: "term-r373-live-only",
		Types:      []EventType{EventTerminalLiveInvalidated},
	})
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r373-live-only",
		Command: []string{"shell"},
		Size:    Size{Cols: 24, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process("term-r373-live-only")
	if process == nil {
		t.Fatal("expected recording process")
	}
	process.emitOutput("alpha\r\nlive-only-tail")
	event := assertEventValue(t, events, EventTerminalLiveInvalidated, "term-r373-live-only")
	if event.Live == nil || event.Live.Revision == 0 {
		t.Fatalf("expected live invalidation in history disabled mode, got %#v", event)
	}

	snapshot, err := server.LiveSnapshot("term-r373-live-only")
	if err != nil {
		t.Fatalf("live snapshot: %v", err)
	}
	if got := strings.Join(liveSnapshotRows(snapshot), "\n"); !strings.Contains(got, "live-only-tail") {
		t.Fatalf("history disabled process output must still update live native screen, got %q", got)
	}
	terminal, err := server.Terminal("term-r373-live-only")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	terminal.queueMu.Lock()
	historyQueue := terminal.historyQ
	historyTapQueue := terminal.historyTapQ
	terminal.queueMu.Unlock()
	if historyQueue != nil {
		t.Fatal("history disabled live-only mode must not create a history transaction queue")
	}
	if historyTapQueue != nil {
		t.Fatal("history disabled live-only mode must not create a history semantic tap queue")
	}
	if _, err := server.TerminalHistoryWindow(context.Background(), "term-r373-live-only", history.HistoryWindowRequest{
		TerminalID: "term-r373-live-only",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       24,
		Limit:      10,
	}); !errors.Is(err, ErrHistoryDisabled) {
		t.Fatalf("history disabled window must stay disabled, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(historyDir, "term-r373-live-only.history-lines.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("history disabled process path must not write payload file, err=%v", err)
	}
	if terminal.tap != nil {
		t.Fatalf("history disabled live-only mode must not create history SemanticTap, got %#v", terminal.tap.InputRecords())
	}
}

func TestR396ProcessOutputResponseOwnerRemainsLiveSurfaceOnly(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r373-response",
		Command: []string{"shell"},
		Size:    Size{Cols: 80, Rows: 24},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process("term-r373-response")
	if process == nil {
		t.Fatal("expected recording process")
	}
	process.emitOutput("\x1b]11;?\x1b\\")
	assertEventually(t, time.Second, func() bool {
		return terminalProcessResponseCount(process, "\x1b]11;") >= 1
	}, "process PTY query must be answered exactly once by live SurfaceTrack")
	time.Sleep(20 * time.Millisecond)
	if got := terminalProcessResponseCount(process, "\x1b]11;"); got != 1 {
		t.Fatalf("process PTY query must be answered exactly once by live SurfaceTrack, got %d responses", got)
	}
	terminal, err := server.Terminal("term-r373-response")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	if records := terminal.tap.InputRecords(); len(records) != 1 || records[0].Kind != SemanticTapInputWrite {
		t.Fatalf("history semantic consumer must still observe response-producing output without writing response, records=%#v", records)
	}
}

func TestR373SingleTapSynchronizedTransactionPublishesPrimaryFrame(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r373-sync-one-tx",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r373-sync-one-tx", "\x1b[?2026hline1\r\nline2\r\nline3\x1b[?2026l"); err != nil {
		t.Fatalf("ingest synchronized single transaction: %v", err)
	}
	window, err := server.TerminalHistoryWindow(context.Background(), "term-r373-sync-one-tx", history.HistoryWindowRequest{
		TerminalID: "term-r373-sync-one-tx",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       20,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history.window: %v", err)
	}
	if !historyRowsContainSegment(window.Rows, history.HistorySegmentCurrentPrimaryFrame) {
		t.Fatalf("history semantic sync begin/payload/end transaction must publish current frame, rows=%#v", window.Rows)
	}
	if got := strings.Join(historyRowTexts(window.Rows), "|"); !strings.Contains(got, "line3") {
		t.Fatalf("current frame should preserve final screen payload, got %q", got)
	}
}

func TestR405TerminalProcessExitSealsPrimaryFrameAsLifecycleFinalFrame(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r405-exit-final-frame",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r405-exit-final-frame", "\x1b[?2026hfinal-a\r\nfinal-b\x1b[?2026l"); err != nil {
		t.Fatalf("ingest synchronized frame: %v", err)
	}
	process := factory.process("term-r405-exit-final-frame")
	if process == nil {
		t.Fatal("expected process")
	}
	process.exit(0)
	assertEventually(t, time.Second, func() bool {
		info, err := server.GetTerminal("term-r405-exit-final-frame")
		return err == nil && info.State == TerminalStateExited
	}, "terminal should exit")
	window, err := server.TerminalHistoryWindow(context.Background(), "term-r405-exit-final-frame", history.HistoryWindowRequest{
		TerminalID: "term-r405-exit-final-frame",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       20,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history.window after exit: %v", err)
	}
	if historyRowsContainSegment(window.Rows, history.HistorySegmentCurrentPrimaryFrame) {
		t.Fatalf("process exit must clear mutable current frame, rows=%#v", window.Rows)
	}
	if !historyRowsContainSegment(window.Rows, history.HistorySegmentCommitted) || !historyRowsContain(window.Rows, "final-b") {
		t.Fatalf("process exit must seal final primary frame into committed history, rows=%#v", window.Rows)
	}
}

func TestR373SingleTapSynchronizedScrollOutKeepsFullPayload(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r373-sync-scrollout",
		Command: []string{"shell"},
		Size:    Size{Cols: 12, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	var output strings.Builder
	output.WriteString("\x1b[?2026h")
	for i := 1; i <= 8; i++ {
		output.WriteString(fmt.Sprintf("line%02d\r\n", i))
	}
	output.WriteString("\x1b[?2026l")
	if err := server.IngestOutput(context.Background(), "term-r373-sync-scrollout", output.String()); err != nil {
		t.Fatalf("ingest long synchronized transaction: %v", err)
	}
	rows, pageCount := r326CollectAllHistoryRows(t, server, "term-r373-sync-scrollout", 12, 2)
	text := strings.Join(historyRowTexts(rows), "\n")
	for _, want := range []string{"line01", "line08"} {
		if !strings.Contains(text, want) {
			t.Fatalf("single tap synchronized scroll-out must keep full payload, missing %q:\n%s\nrows=%#v", want, text, rows)
		}
	}
	if pageCount < 2 {
		t.Fatalf("long synchronized transaction should page beyond latest frame, page_count=%d rows=%#v", pageCount, rows)
	}
}

func TestR373SplitSynchronizedEndScrollOutKeepsPayload(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r373-split-sync-end",
		Command: []string{"shell"},
		Size:    Size{Cols: 12, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r373-split-sync-end", "\x1b[?2026hline01\r\n"); err != nil {
		t.Fatalf("ingest synchronized begin: %v", err)
	}
	var output strings.Builder
	for i := 2; i <= 8; i++ {
		output.WriteString(fmt.Sprintf("line%02d\r\n", i))
	}
	output.WriteString("\x1b[?2026l")
	if err := server.IngestOutput(context.Background(), "term-r373-split-sync-end", output.String()); err != nil {
		t.Fatalf("ingest synchronized end flush: %v", err)
	}
	rows, _ := r326CollectAllHistoryRows(t, server, "term-r373-split-sync-end", 12, 2)
	text := strings.Join(historyRowTexts(rows), "\n")
	for _, want := range []string{"line01", "line08"} {
		if !strings.Contains(text, want) {
			t.Fatalf("split sync-end transaction must keep full payload, missing %q:\n%s\nrows=%#v", want, text, rows)
		}
	}
}

func TestR373SingleTapAltTransactionDoesNotEnterPrimaryHistory(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r373-alt-one-tx",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r373-alt-one-tx", "before\r\n"); err != nil {
		t.Fatalf("seed primary history: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r373-alt-one-tx", "\x1b[?1049hALT\x1b[?1049l"); err != nil {
		t.Fatalf("ingest alt single transaction: %v", err)
	}
	window, err := server.TerminalHistoryWindow(context.Background(), "term-r373-alt-one-tx", history.HistoryWindowRequest{
		TerminalID: "term-r373-alt-one-tx",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       20,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history.window: %v", err)
	}
	for _, row := range window.Rows {
		if strings.Contains(historyCellsText(row.Cells), "ALT") && row.Segment != history.HistorySegmentCurrentAltFrame {
			t.Fatalf("alt enter/write/exit in one tap transaction must not enter primary history, row=%#v rows=%#v", row, window.Rows)
		}
	}
	if got := historyTextCount(window.Rows, "ALT"); got != 0 {
		t.Fatalf("alt transient already exited; latest authoritative primary history must not contain ALT, count=%d rows=%#v", got, window.Rows)
	}
}

func TestR398SecondSingleTapRedrawReplacesMutablePrimaryFrame(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r398-second-redraw",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r398-second-redraw", "\x1b[?2026h\x1b[Hfirst\r\ncurrent\x1b[?2026l"); err != nil {
		t.Fatalf("ingest first redraw: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r398-second-redraw", "\x1b[?2026h\x1b[Hsecond\r\ncurrent\x1b[?2026l"); err != nil {
		t.Fatalf("ingest second redraw: %v", err)
	}
	rows, _ := r326CollectAllHistoryRows(t, server, "term-r398-second-redraw", 20, 2)
	if got := historyTextCount(rows, "first"); got != 0 {
		t.Fatalf("second redraw must replace mutable current frame without sealing first repaint, count=%d rows=%#v", got, rows)
	}
	if got := historyTextCount(rows, "second"); got != 1 {
		t.Fatalf("second redraw must publish new current frame once, count=%d rows=%#v", got, rows)
	}
	if current := currentPrimaryFrameRowTexts(rows); len(current) == 0 || !strings.Contains(strings.Join(current, "\n"), "second") {
		t.Fatalf("latest current frame should be the second redraw, current=%#v rows=%#v", current, rows)
	}
}

func TestR398SynchronizedProgressRepaintDoesNotAppendEveryFrame(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r398-progress-redraw",
		Command: []string{"shell"},
		Size:    Size{Cols: 80, Rows: 12},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	for i := 0; i < 8; i++ {
		output := fmt.Sprintf("\x1b[?2026h\x1b[Hgpt-5.5 xhigh · ~/Documents/workdir/termx\r\nStarting MCP servers (1/2): computer-use (%ds · esc to interrupt)\r\n\r\n> Improve documentation in @filename\x1b[?2026l", i)
		if err := server.IngestOutput(context.Background(), "term-r398-progress-redraw", output); err != nil {
			t.Fatalf("ingest progress repaint %d: %v", i, err)
		}
	}
	rows, _ := r326CollectAllHistoryRows(t, server, "term-r398-progress-redraw", 80, 6)
	if got := historyTextCount(rows, "Starting MCP servers"); got != 1 {
		t.Fatalf("progress repaint must expose only latest mutable frame, count=%d rows=%#v", got, rows)
	}
	if got := strings.Join(currentPrimaryFrameRowTexts(rows), "\n"); !strings.Contains(got, "7s · esc to interrupt") {
		t.Fatalf("current frame should keep latest progress text, got %q rows=%#v", got, rows)
	}
	if historyRowsContainSegment(rows, history.HistorySegmentArchivedPrimaryFrame) {
		t.Fatalf("pure synchronized progress repaint must not archive intermediate frames, rows=%#v", rows)
	}
}

func TestR401CursorAddressedProgressRepaintDoesNotAppendEveryFrame(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r401-cursor-progress-redraw",
		Command: []string{"shell"},
		Size:    Size{Cols: 80, Rows: 12},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	for i := 0; i < 8; i++ {
		output := fmt.Sprintf("\x1b[Hgpt-5.5 xhigh · ~/Documents/workdir/termx\x1b[K\r\nStarting MCP servers (1/2): computer-use (%ds · esc to interrupt)\x1b[K\r\n\r\n> Improve  documentation   in @filename\x1b[K", i)
		if err := server.IngestOutput(context.Background(), "term-r401-cursor-progress-redraw", output); err != nil {
			t.Fatalf("ingest cursor-addressed progress repaint %d: %v", i, err)
		}
	}
	rows, _ := r326CollectAllHistoryRows(t, server, "term-r401-cursor-progress-redraw", 80, 6)
	if got := historyTextCount(rows, "Starting MCP servers"); got != 1 {
		t.Fatalf("cursor-addressed progress repaint must expose only latest mutable frame, count=%d rows=%#v", got, rows)
	}
	current := strings.Join(currentPrimaryFrameRowTexts(rows), "\n")
	if !strings.Contains(current, "7s · esc to interrupt") {
		t.Fatalf("current frame should keep latest progress text, got %q rows=%#v", current, rows)
	}
	if !strings.Contains(current, "> Improve  documentation   in @filename") {
		t.Fatalf("current frame must preserve intra-line spaces, got %q rows=%#v", current, rows)
	}
	if historyRowsContainSegment(rows, history.HistorySegmentArchivedPrimaryFrame) {
		t.Fatalf("cursor-addressed progress repaint must not archive intermediate frames, rows=%#v", rows)
	}
}

func TestR373SingleTapSyncThenAltKeepsPrimaryHistory(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r373-sync-alt-one-tx",
		Command: []string{"shell"},
		Size:    Size{Cols: 12, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r373-sync-alt-one-tx", "prelude\r\n"); err != nil {
		t.Fatalf("seed prelude: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r373-sync-alt-one-tx", "\x1b[?2026hsync01\r\nsync02\x1b[?2026l\x1b[?1049hALT-TRANSIENT\x1b[?1049l"); err != nil {
		t.Fatalf("ingest sync then alt transaction: %v", err)
	}
	rows, _ := r326CollectAllHistoryRows(t, server, "term-r373-sync-alt-one-tx", 12, 2)
	text := strings.Join(historyRowTexts(rows), "\n")
	for _, want := range []string{"prelude", "sync01", "sync02"} {
		if !strings.Contains(text, want) {
			t.Fatalf("sync content must survive same tap transaction as alt enter, missing %q:\n%s\nrows=%#v", want, text, rows)
		}
	}
	if strings.Contains(text, "ALT-TRANSIENT") {
		t.Fatalf("alt transient must not enter primary history:\n%s\nrows=%#v", text, rows)
	}
	if !historyRowsContainSegment(rows, history.HistorySegmentArchivedPrimaryFrame) {
		t.Fatalf("alt enter in same tap transaction must archive synchronized primary frame, rows=%#v", rows)
	}
}

func TestR373SingleTapPreludeThenSyncThenAltKeepsPrimaryHistory(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r373-prelude-sync-alt-one-tx",
		Command: []string{"shell"},
		Size:    Size{Cols: 12, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	var output strings.Builder
	output.WriteString("prelude\r\n")
	output.WriteString("\x1b[?2026h")
	for i := 1; i <= 8; i++ {
		output.WriteString(fmt.Sprintf("sync%02d\r\n", i))
	}
	output.WriteString("\x1b[?2026l\x1b[?1049hALT-TRANSIENT\x1b[?1049l")
	if err := server.IngestOutput(context.Background(), "term-r373-prelude-sync-alt-one-tx", output.String()); err != nil {
		t.Fatalf("ingest prelude sync alt transaction: %v", err)
	}
	rows, _ := r326CollectAllHistoryRows(t, server, "term-r373-prelude-sync-alt-one-tx", 12, 2)
	text := strings.Join(historyRowTexts(rows), "\n")
	for _, want := range []string{"prelude", "sync01", "sync08"} {
		if !strings.Contains(text, want) {
			t.Fatalf("prelude and synchronized payload must survive same tap transaction as alt enter, missing %q:\n%s\nrows=%#v", want, text, rows)
		}
	}
	if strings.Contains(text, "ALT-TRANSIENT") {
		t.Fatalf("alt transient must not enter primary history:\n%s\nrows=%#v", text, rows)
	}
	if !historyRowsContainSegment(rows, history.HistorySegmentArchivedPrimaryFrame) {
		t.Fatalf("same-transaction alt enter must archive pre-alt primary frame, rows=%#v", rows)
	}
}

func TestServerNextLiveInvalidationReplaysOnlyWhenObservedRevisionIsBehind(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-live-next", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-live-next", "live update\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	terminal, err := server.Terminal("term-live-next")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	currentRevision := terminal.LiveRevision()
	if currentRevision == 0 {
		t.Fatalf("expected live revision after output")
	}
	event, err := server.NextLiveInvalidation(context.Background(), "term-live-next", currentRevision-1)
	if err != nil {
		t.Fatalf("behind observed revision should get immediate wake: %v", err)
	}
	if event.Live == nil || event.Live.Revision != currentRevision {
		t.Fatalf("unexpected immediate wake %#v current=%#v", event, currentRevision)
	}
	if strings.Contains(fmt.Sprintf("%#v", event.Live), "Snapshot") {
		t.Fatalf("immediate wake must not carry screen payload, got %#v", event.Live)
	}
	snapshot := terminal.NativeScreenSnapshot("term-live-next")
	if snapshot.Revision != currentRevision || !strings.Contains(strings.Join(terminalLiveRowsFromNativeSnapshot(snapshot), "\n"), "live update") {
		t.Fatalf("latest native snapshot should remain pull-based, got %#v", snapshot)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if event, err := server.NextLiveInvalidation(ctx, "term-live-next", currentRevision); err == nil {
		t.Fatalf("observed current revision must wait for a future wake instead of replaying current revision: %#v", event)
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected wait timeout without future invalidation, got %v", err)
	}
}

func TestServerNextLiveInvalidationWaitsForNextWake(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-live-wait", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan Event, 1)
	errs := make(chan error, 1)
	go func() {
		event, err := server.NextLiveInvalidation(ctx, "term-live-wait", 0)
		if err != nil {
			errs <- err
			return
		}
		done <- event
	}()
	select {
	case event := <-done:
		t.Fatalf("one-shot arm returned before next wake: %#v", event)
	case err := <-errs:
		t.Fatalf("one-shot arm failed before next wake: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if err := server.IngestOutput(context.Background(), "term-live-wait", "live update\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	select {
	case err := <-errs:
		t.Fatalf("one-shot arm failed: %v", err)
	case event := <-done:
		if event.Type != EventTerminalLiveInvalidated || event.TerminalID != "term-live-wait" || event.Live == nil || event.Live.Revision == 0 {
			t.Fatalf("expected next live invalidation, got %#v", event)
		}
		if strings.Contains(fmt.Sprintf("%#v", event.Live), "Snapshot") {
			t.Fatalf("waited wake must not carry screen payload, got %#v", event.Live)
		}
		terminal, err := server.Terminal("term-live-wait")
		if err != nil {
			t.Fatalf("terminal: %v", err)
		}
		snapshot := terminal.NativeScreenSnapshot("term-live-wait")
		if !strings.Contains(strings.Join(terminalLiveRowsFromNativeSnapshot(snapshot), "\n"), "live update") {
			t.Fatalf("latest native snapshot should remain pull-based, got %#v", snapshot)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for live invalidation: %v", ctx.Err())
	}
}

func TestServerNextLiveInvalidationCoalescesMissedRevisionsToLatestWake(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-live-coalesce", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	for _, output := range []string{"one\r\n", "two\r\n", "three\r\n"} {
		if err := server.IngestOutput(context.Background(), "term-live-coalesce", output); err != nil {
			t.Fatalf("ingest output: %v", err)
		}
	}
	terminal, err := server.Terminal("term-live-coalesce")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	currentRevision := terminal.LiveRevision()
	if currentRevision < 3 {
		t.Fatalf("expected multiple live revisions, got %d", currentRevision)
	}
	event, err := server.NextLiveInvalidation(context.Background(), "term-live-coalesce", 1)
	if err != nil {
		t.Fatalf("missed revisions should coalesce to latest wake: %v", err)
	}
	if event.Live == nil || event.Live.Revision != currentRevision {
		t.Fatalf("expected latest coalesced revision %d, got %#v", currentRevision, event)
	}
	if strings.Contains(fmt.Sprintf("%#v", event.Live), "Snapshot") {
		t.Fatalf("coalesced wake must not carry screen payload, got %#v", event.Live)
	}
	snapshot := terminal.NativeScreenSnapshot("term-live-coalesce")
	if snapshot.Revision != currentRevision || !strings.Contains(strings.Join(terminalLiveRowsFromNativeSnapshot(snapshot), "\n"), "three") {
		t.Fatalf("latest native snapshot should remain pull-based, got %#v", snapshot)
	}
}

func TestServerNextLiveInvalidationFlushesPendingTapQueueWithoutSnapshot(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-live-flush", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	terminal, err := server.Terminal("term-live-flush")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-live-flush", "first\r\n"); err != nil {
		t.Fatalf("first output: %v", err)
	}
	observed := terminal.LiveRevision()
	process := factory.process("term-live-flush")
	if process == nil {
		t.Fatalf("expected recording process")
	}
	queue := newTerminalLiveIngestQueue()
	terminal.setLiveQueue(process, queue)
	defer func() {
		queue.Close()
		queue.Wait()
		terminal.clearLiveQueue(process, queue)
	}()
	queue.Enqueue("second\r\n")
	queue.Enqueue("third\r\n")
	done := make(chan Event, 1)
	errs := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		event, err := server.NextLiveInvalidation(ctx, "term-live-flush", observed)
		if err != nil {
			errs <- err
			return
		}
		done <- event
	}()
	select {
	case event := <-done:
		t.Fatalf("wake returned before pending tap queue flush: %#v", event)
	case err := <-errs:
		t.Fatalf("wake failed before pending tap queue flush: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	go queue.Run(func(output string) error {
		return terminal.IngestOutput(output)
	})
	select {
	case err := <-errs:
		t.Fatalf("wake failed: %v", err)
	case event := <-done:
		current := terminal.LiveRevision()
		if event.Live == nil || event.Live.Revision != current || current <= observed {
			t.Fatalf("wake should coalesce to latest revision after tap flush, event=%#v current=%d observed=%d", event, current, observed)
		}
		if strings.Contains(fmt.Sprintf("%#v", event.Live), "Snapshot") {
			t.Fatalf("coalesced wake must remain wake-only, got %#v", event.Live)
		}
		snapshot := terminal.NativeScreenSnapshot("term-live-flush")
		if snapshot.Revision != current || !strings.Contains(strings.Join(terminalLiveRowsFromNativeSnapshot(snapshot), "\n"), "third") {
			t.Fatalf("latest native snapshot should be pull-based after flush, got %#v", snapshot)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for coalesced wake: %v", ctx.Err())
	}
}

func TestR396NextLiveInvalidationFlushesLiveQueueNotHistoryTapQueue(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-r396-live-split", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	terminal, err := server.Terminal("term-r396-live-split")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r396-live-split", "first\r\n"); err != nil {
		t.Fatalf("first output: %v", err)
	}
	observed := terminal.LiveRevision()
	process := factory.process("term-r396-live-split")
	if process == nil {
		t.Fatalf("expected recording process")
	}
	liveQueue := newTerminalLiveIngestQueue()
	historyTapQueue := newTerminalLiveIngestQueue()
	terminal.setLiveQueue(process, liveQueue)
	terminal.setHistoryTapQueue(process, historyTapQueue)
	defer func() {
		liveQueue.Close()
		liveQueue.Wait()
		terminal.clearLiveQueue(process, liveQueue)
		historyTapQueue.Close()
		terminal.clearHistoryTapQueue(process, historyTapQueue)
	}()
	liveQueue.Enqueue("live-latest\r\n")
	historyTapQueue.Enqueue("history-must-not-block-live\r\n")
	done := make(chan Event, 1)
	errs := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		event, err := server.NextLiveInvalidation(ctx, "term-r396-live-split", observed)
		if err != nil {
			errs <- err
			return
		}
		done <- event
	}()
	select {
	case event := <-done:
		t.Fatalf("wake returned before live queue worker drained pending bytes: %#v", event)
	case err := <-errs:
		t.Fatalf("wake failed before live queue drain: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	go liveQueue.Run(func(output string) error {
		return terminal.ingestProcessLiveOutput(process, output)
	})
	select {
	case err := <-errs:
		t.Fatalf("wake failed: %v", err)
	case event := <-done:
		current := terminal.LiveRevision()
		if event.Live == nil || event.Live.Revision != current || current <= observed {
			t.Fatalf("wake should coalesce to latest live revision without history tap, event=%#v current=%d observed=%d", event, current, observed)
		}
		snapshot := terminal.NativeScreenSnapshot("term-r396-live-split")
		if snapshot.Revision != current || !strings.Contains(strings.Join(terminalLiveRowsFromNativeSnapshot(snapshot), "\n"), "live-latest") {
			t.Fatalf("latest live snapshot should come from live SurfaceTrack, got %#v", snapshot)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for live wake while history tap queue was undrained: %v", ctx.Err())
	}
}

func TestR397LiveIngestQueueDoesNotShiftBacklogOnBatchDrain(t *testing.T) {
	queue := newTerminalLiveIngestQueue()
	for i := 0; i < 2048; i++ {
		if !queue.Enqueue("x") {
			t.Fatal("enqueue should accept open queue")
		}
	}
	batch, completeSeq, ok := queue.nextBatchWithSeq()
	if !ok {
		t.Fatal("expected first batch")
	}
	if len(batch) != 2048 || completeSeq != 2048 {
		t.Fatalf("expected full small backlog batch, len=%d seq=%d", len(batch), completeSeq)
	}
	if queue.pendingCount != 0 || queue.head != nil || queue.tail != nil {
		t.Fatalf("fully drained queue should release pending pages, count=%d head=%#v tail=%#v", queue.pendingCount, queue.head, queue.tail)
	}

	for i := 0; i < 2048; i++ {
		if !queue.Enqueue(strings.Repeat("a", 16)) {
			t.Fatal("enqueue should accept reopened backlog")
		}
	}
	batch, completeSeq, ok = queue.nextBatchWithSeq()
	if !ok {
		t.Fatal("expected bounded batch")
	}
	if len(batch) == 0 || completeSeq == 0 {
		t.Fatalf("expected bounded batch with completion seq, len=%d seq=%d", len(batch), completeSeq)
	}
	if queue.pendingCount != 1024 || queue.head == nil || queue.head.items[queue.head.start].seq != completeSeq+1 {
		t.Fatalf("partial drain should release consumed pages and keep remaining order, count=%d seq=%d head=%#v", queue.pendingCount, completeSeq, queue.head)
	}
	if queue.pendingLenLocked() != queue.pendingCount {
		t.Fatalf("pending length helper mismatch, count=%d", queue.pendingCount)
	}
}

func TestR324TerminalHistoryReturnsAuthoritativeWindow(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-history-r324",
		Command: []string{"shell"},
		Size:    Size{Cols: 30, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-history-r324", "alpha\r\nbeta\r\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	window, err := server.TerminalHistoryWindow(context.Background(), "term-history-r324", history.HistoryWindowRequest{
		TerminalID: "term-history-r324",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       30,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history.window should return rebuilt authoritative history: %v", err)
	}
	if got := historyRowTexts(window.Rows); strings.Join(got, "|") != "alpha|beta" {
		t.Fatalf("history.window rows mismatch: %v window=%#v", got, window)
	}
	snapshot, err := server.TerminalHistoryFreeze(context.Background(), "term-history-r324", history.FreezeHistoryRequest{
		TerminalID: "term-history-r324",
		Cols:       30,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history.freeze should create token: %v", err)
	}
	text, err := server.TerminalHistoryCopy(context.Background(), "term-history-r324", history.HistoryCopyRequest{
		TerminalID: "term-history-r324",
		Token:      snapshot.Token,
		Start:      history.HistoryCursor{LineID: window.Rows[0].LineID, Valid: true},
		End:        history.HistoryCursor{LineID: window.Rows[1].LineID, Valid: true},
	})
	if err != nil {
		t.Fatalf("history.copy should use authoritative frozen token: %v", err)
	}
	if text != "alpha\nbeta" {
		t.Fatalf("history.copy mismatch: %q", text)
	}
	if err := server.TerminalHistoryRelease(context.Background(), "term-history-r324", snapshot.Token); err != nil {
		t.Fatalf("history.release should release token: %v", err)
	}
}

func TestR370ProtocolLatestReadsFrozenTokenWithoutSecondFlush(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r370-no-second-flush",
		Command: []string{"shell"},
		Size:    Size{Cols: 30, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r370-no-second-flush", "alpha\r\nbeta\r\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	snapshot, err := server.TerminalHistoryFreeze(context.Background(), "term-r370-no-second-flush", history.FreezeHistoryRequest{
		TerminalID: "term-r370-no-second-flush",
		Cols:       30,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("freeze latest: %v", err)
	}
	window, err := server.terminalHistoryWindow(context.Background(), "term-r370-no-second-flush", history.HistoryWindowRequest{
		TerminalID: "term-r370-no-second-flush",
		Mode:       history.HistoryWindowModeLatest,
		Token:      snapshot.Token,
		Cols:       30,
		Limit:      10,
	}, false)
	if err != nil {
		t.Fatalf("frozen latest window should not need a second flush: %v", err)
	}
	if got := historyRowTexts(window.Rows); strings.Join(got, "|") != "alpha|beta" {
		t.Fatalf("unexpected frozen latest rows %v window=%#v", got, window)
	}
	if _, err := server.terminalHistoryWindow(context.Background(), "term-r370-no-second-flush", history.HistoryWindowRequest{
		TerminalID: "term-r370-no-second-flush",
		Mode:       history.HistoryWindowModeLatest,
		Cols:       30,
		Limit:      10,
	}, false); !errors.Is(err, history.ErrHistoryInvalidMutation) {
		t.Fatalf("no-flush window must require a frozen token, got %v", err)
	}
}

func TestR360TerminalHistoryOldestReturnsReplaceWindow(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-history-r360-oldest",
		Command: []string{"shell"},
		Size:    Size{Cols: 30, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-history-r360-oldest", "line-1\r\nline-2\r\nline-3\r\nline-4\r\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	snapshot, err := server.TerminalHistoryFreeze(context.Background(), "term-history-r360-oldest", history.FreezeHistoryRequest{
		TerminalID: "term-history-r360-oldest",
		Cols:       30,
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("history.freeze should create token: %v", err)
	}
	oldest, err := server.TerminalHistoryWindow(context.Background(), "term-history-r360-oldest", history.HistoryWindowRequest{
		TerminalID: "term-history-r360-oldest",
		Mode:       history.HistoryWindowModeOldest,
		Token:      snapshot.Token,
		Cols:       30,
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("oldest window: %v", err)
	}
	if oldest.Op != history.HistoryWindowReplace {
		t.Fatalf("oldest must be a replace window, got %s", oldest.Op)
	}
	if got := strings.Join(historyRowTexts(oldest.Rows), "|"); got != "line-1|line-2" {
		t.Fatalf("oldest should return frozen head rows, got %q window=%#v", got, oldest)
	}
}

func TestR346TerminalUsesFileBackedHistoryStoreWhenConfigured(t *testing.T) {
	historyDir := t.TempDir()
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()), WithHistoryStorageDir(historyDir))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r346-file-backed",
		Command: []string{"shell"},
		Size:    Size{Cols: 30, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r346-file-backed", "alpha\r\nbeta\r\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	payloadPath := filepath.Join(historyDir, "term-r346-file-backed.history-lines.bin")
	if info, err := os.Stat(payloadPath); err != nil || info.Size() == 0 {
		t.Fatalf("expected file-backed history payload at %s, info=%#v err=%v", payloadPath, info, err)
	}
	window, err := server.TerminalHistoryWindow(context.Background(), "term-r346-file-backed", history.HistoryWindowRequest{
		TerminalID: "term-r346-file-backed",
		Mode:       history.HistoryWindowModeLatest,
		Limit:      2,
		Cols:       30,
	})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if got := strings.Join(historyRowTexts(window.Rows), "|"); got != "alpha|beta" {
		t.Fatalf("file-backed terminal history window mismatch: %q", got)
	}
}

func TestR346TerminalDoesNotSilentlyFallbackWhenHistoryBackendCannotOpen(t *testing.T) {
	blockingFile := filepath.Join(t.TempDir(), "blocking-file")
	badDir := filepath.Join(blockingFile, "child")
	if err := os.WriteFile(blockingFile, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatalf("seed blocking file: %v", err)
	}
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()), WithHistoryStorageDir(badDir))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-r346-bad-backend", Command: []string{"shell"}}); err == nil {
		t.Fatal("register terminal should fail when configured file-backed history cannot be created")
	}
}

func TestR324TerminalHistoryPrimaryRepaintScrollOutAltResizeAndExit(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-history-screen", Command: []string{"shell"}, Size: Size{Cols: 8, Rows: 3}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-history-screen", "\x1b[?2026hline1\r\nline2\r\nline3\r\nline4\x1b[?2026l"); err != nil {
		t.Fatalf("ingest synchronized output: %v", err)
	}
	window, err := server.TerminalHistoryWindow(context.Background(), "term-history-screen", history.HistoryWindowRequest{TerminalID: "term-history-screen", Mode: history.HistoryWindowModeLatest, Cols: 8, Limit: 10})
	if err != nil {
		t.Fatalf("history.window after sync output: %v", err)
	}
	if !historyRowsContainSegment(window.Rows, history.HistorySegmentCommitted) || !historyRowsContainSegment(window.Rows, history.HistorySegmentCurrentPrimaryFrame) || !historyRowsContain(window.Rows, "line4") {
		t.Fatalf("history should include sealed scroll-out proof and current frame, rows=%v", historyRowTexts(window.Rows))
	}

	if err := server.IngestOutput(context.Background(), "term-history-screen", "\x1b[?1049hALT\x1b[?1049l"); err != nil {
		t.Fatalf("ingest alt transient: %v", err)
	}
	window, err = server.TerminalHistoryWindow(context.Background(), "term-history-screen", history.HistoryWindowRequest{TerminalID: "term-history-screen", Mode: history.HistoryWindowModeLatest, Cols: 8, Limit: 10})
	if err != nil {
		t.Fatalf("history.window after alt: %v", err)
	}
	for _, row := range window.Rows {
		if strings.Contains(historyCellsText(row.Cells), "ALT") && row.Segment != history.HistorySegmentCurrentAltFrame {
			t.Fatalf("alt content must not enter primary timeline, row=%#v rows=%v", row, historyRowTexts(window.Rows))
		}
	}
	if err := server.ResizeTerminal(context.Background(), "term-history-screen", 12, 4); err != nil {
		t.Fatalf("resize terminal: %v", err)
	}
	windowAfterResize, err := server.TerminalHistoryWindow(context.Background(), "term-history-screen", history.HistoryWindowRequest{TerminalID: "term-history-screen", Mode: history.HistoryWindowModeLatest, Cols: 12, Limit: 10})
	if err != nil {
		t.Fatalf("history.window after resize: %v", err)
	}
	if len(windowAfterResize.Rows) < len(window.Rows) {
		t.Fatalf("resize boundary must not delete history rows: before=%v after=%v", historyRowTexts(window.Rows), historyRowTexts(windowAfterResize.Rows))
	}

	process := factory.process("term-history-screen")
	if process == nil {
		t.Fatal("expected recording process")
	}
	process.exit(0)
	waitForTerminalState(t, server, "term-history-screen", TerminalStateExited)
	windowAfterExit, err := server.TerminalHistoryWindow(context.Background(), "term-history-screen", history.HistoryWindowRequest{TerminalID: "term-history-screen", Mode: history.HistoryWindowModeLatest, Cols: 12, Limit: 20})
	if err != nil {
		t.Fatalf("history.window after exit: %v", err)
	}
	if len(windowAfterExit.Rows) == 0 {
		t.Fatal("process exit should leave authoritative history rows")
	}
}

func TestR324TerminalHistoryRemoveClosesOpenLine(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-history-remove", Command: []string{"shell"}, Size: Size{Cols: 20, Rows: 3}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-history-remove", "partial"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	terminal, err := server.Terminal("term-history-remove")
	if err != nil {
		t.Fatalf("terminal handle before remove: %v", err)
	}
	if err := server.RemoveTerminal("term-history-remove"); err != nil {
		t.Fatalf("remove terminal: %v", err)
	}
	window, err := terminal.HistoryWindow(history.HistoryWindowRequest{TerminalID: "term-history-remove", Mode: history.HistoryWindowModeLatest, Cols: 20, Limit: 10})
	if err != nil {
		t.Fatalf("history.window after remove should read closed store: %v", err)
	}
	if got := historyRowTexts(window.Rows); strings.Join(got, "|") != "partial" {
		t.Fatalf("remove close should seal open line before process close, got %v", got)
	}
}

func historyRowTexts(rows []history.HistoryRow) []string {
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, historyCellsText(row.Cells))
	}
	return texts
}

func historyRowTextsFromLinesForTest(mutations []history.HistoryMutation) []string {
	texts := make([]string, 0, len(mutations))
	for _, mutation := range mutations {
		if mutation.Line == nil {
			continue
		}
		texts = append(texts, historyCellsText(mutation.Line.Cells))
	}
	return texts
}

func historyJournalLabelsForTerminalTest(journal history.HistoryJournal) []string {
	labels := make([]string, 0, len(journal.Items))
	for _, item := range journal.Items {
		switch item.Kind {
		case history.HistoryJournalItemOrdinaryLineBatch:
			labels = append(labels, "batch")
		case history.HistoryJournalItemBoundary:
			if item.Boundary == nil {
				labels = append(labels, "boundary:<nil>")
				continue
			}
			labels = append(labels, "boundary:"+string(item.Boundary.Kind))
		case history.HistoryJournalItemFrameEvent:
			if item.Frame == nil {
				labels = append(labels, "frame:<nil>")
				continue
			}
			labels = append(labels, "frame:"+string(item.Frame.Kind))
		case history.HistoryJournalItemScrollOutProof:
			labels = append(labels, "scroll-out")
		default:
			labels = append(labels, string(item.Kind))
		}
	}
	return labels
}

func historyRowsContain(rows []history.HistoryRow, needle string) bool {
	for _, row := range rows {
		if strings.Contains(historyCellsText(row.Cells), needle) {
			return true
		}
	}
	return false
}

func historyRowsContainSegment(rows []history.HistoryRow, segment history.HistorySegment) bool {
	for _, row := range rows {
		if row.Segment == segment {
			return true
		}
	}
	return false
}

func historyCellsText(cells []history.Cell) string {
	var out string
	for _, cell := range cells {
		out += cell.Text
	}
	return out
}

func liveSnapshotRows(snapshot live.SurfaceSnapshot) []string {
	rows := make([]string, 0, len(snapshot.Screen.Cells))
	for _, row := range snapshot.Screen.Cells {
		var text string
		for _, cell := range row {
			text += cell.Content
		}
		rows = append(rows, strings.TrimRight(text, " "))
	}
	return rows
}

func terminalLiveRowsFromNativeSnapshot(snapshot NativeScreenSnapshot) []string {
	if len(snapshot.Rows) == 0 {
		return nil
	}
	out := make([]string, len(snapshot.Rows))
	for index, row := range snapshot.Rows {
		out[index] = strings.TrimRight(terminalTestVTermRowText(row.Cells), " ")
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func terminalTestVTermRowText(row []vterm.Cell) string {
	var builder strings.Builder
	for _, cell := range row {
		builder.WriteString(cell.Content)
	}
	return builder.String()
}

func terminalProcessResponseCount(process *recordingProcess, needle string) int {
	inputs, _, _, _ := process.snapshot()
	count := 0
	for _, input := range inputs {
		if strings.Contains(string(input), needle) {
			count++
		}
	}
	return count
}

func TestTerminalRestartReplacesProcessAndClearsExitMetadata(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}, Size: Size{Cols: 20, Rows: 4}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "before\n"); err != nil {
		t.Fatalf("ingest before: %v", err)
	}
	first := factory.process("term-1")
	first.exit(0)
	waitForTerminalState(t, server, "term-1", TerminalStateExited)
	if err := server.RestartTerminal(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	second := factory.process("term-1")
	if second == nil || second == first {
		t.Fatalf("expected new process, first=%p second=%p", first, second)
	}
	_, _, _, firstClosed := first.snapshot()
	if !firstClosed {
		t.Fatal("restart should close old process")
	}
	info, err := server.GetTerminal("term-1")
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if info.State != TerminalStateRunning || info.ExitCode != nil || !info.ExitedAt.IsZero() {
		t.Fatalf("restart should clear exit metadata, got %#v", info)
	}
}

func TestTerminalKillAndRemoveCloseProcess(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process("term-1")
	if err := server.KillTerminal(context.Background(), "term-1"); err != nil {
		t.Fatalf("kill terminal: %v", err)
	}
	_, _, killed, _ := process.snapshot()
	if !killed {
		t.Fatal("expected process to be killed")
	}
	if err := server.RemoveTerminal("term-1"); err != nil {
		t.Fatalf("remove terminal: %v", err)
	}
	_, _, _, closed := process.snapshot()
	if !closed {
		t.Fatal("expected process to be closed")
	}
	if _, err := server.Terminal("term-1"); !errors.Is(err, ErrTerminalNotFound) {
		t.Fatalf("expected ErrTerminalNotFound, got %v", err)
	}
}

func TestServerShutdownRejectsLaterTerminalRegistration(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); !errors.Is(err, ErrServerClosed) {
		t.Fatalf("expected ErrServerClosed, got %v", err)
	}
	if process := factory.process("term-1"); process != nil {
		t.Fatal("shutdown server must not spawn process for later registration")
	}
}

type recordingProcessFactory struct {
	mu        sync.Mutex
	processes map[string][]*recordingProcess
	specs     map[string][]ProcessSpec
}

func newRecordingProcessFactory() *recordingProcessFactory {
	return &recordingProcessFactory{processes: make(map[string][]*recordingProcess), specs: make(map[string][]ProcessSpec)}
}

func (factory *recordingProcessFactory) Spawn(_ context.Context, spec ProcessSpec) (TerminalProcess, error) {
	process := &recordingProcess{
		id:       spec.TerminalID,
		outputCh: make(chan []byte, 16),
		waitCh:   make(chan ProcessExit, 1),
	}
	factory.mu.Lock()
	factory.processes[spec.TerminalID] = append(factory.processes[spec.TerminalID], process)
	factory.specs[spec.TerminalID] = append(factory.specs[spec.TerminalID], cloneProcessSpec(spec))
	factory.mu.Unlock()
	return process, nil
}

func (factory *recordingProcessFactory) process(id string) *recordingProcess {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	processes := factory.processes[id]
	if len(processes) == 0 {
		return nil
	}
	return processes[len(processes)-1]
}

func (factory *recordingProcessFactory) spawnedSpecs(id string) []ProcessSpec {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	specs := factory.specs[id]
	out := make([]ProcessSpec, 0, len(specs))
	for _, spec := range specs {
		out = append(out, cloneProcessSpec(spec))
	}
	return out
}

type sessionBoundRecordingProcessFactory struct {
	*recordingProcessFactory
}

func newSessionBoundRecordingProcessFactory() *sessionBoundRecordingProcessFactory {
	return &sessionBoundRecordingProcessFactory{recordingProcessFactory: newRecordingProcessFactory()}
}

func (factory *sessionBoundRecordingProcessFactory) Spawn(ctx context.Context, spec ProcessSpec) (TerminalProcess, error) {
	process, err := factory.recordingProcessFactory.Spawn(ctx, spec)
	if err != nil {
		return nil, err
	}
	recording, ok := process.(*recordingProcess)
	if !ok {
		return process, nil
	}
	go func() {
		<-ctx.Done()
		recording.exit(-1)
	}()
	return recording, nil
}

type blockingHistoryStore struct {
	history.HistoryStore
	mu           sync.Mutex
	blocked      bool
	releaseCh    chan struct{}
	applyStarted chan struct{}
	startOnce    sync.Once
}

func newBlockingHistoryStore(terminalID string) *blockingHistoryStore {
	return &blockingHistoryStore{
		HistoryStore: history.NewInMemoryHistoryStore(terminalID),
		releaseCh:    make(chan struct{}),
		applyStarted: make(chan struct{}),
	}
}

func (store *blockingHistoryStore) block() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.blocked = true
}

func (store *blockingHistoryStore) release() {
	store.mu.Lock()
	releaseCh := store.releaseCh
	store.blocked = false
	store.mu.Unlock()
	close(releaseCh)
}

func (store *blockingHistoryStore) Apply(batch history.HistoryMutationBatch) error {
	store.mu.Lock()
	blocked := store.blocked
	releaseCh := store.releaseCh
	store.mu.Unlock()
	if blocked {
		store.startOnce.Do(func() {
			close(store.applyStarted)
		})
		<-releaseCh
	}
	return store.HistoryStore.Apply(batch)
}

type recordingProcess struct {
	mu         sync.Mutex
	id         string
	inputs     [][]byte
	resizes    []Size
	resizeErr  error
	resizeHook func(Size)
	outputCh   chan []byte
	waitCh     chan ProcessExit
	exitOnce   sync.Once
	outputOnce sync.Once
	killed     bool
	closed     bool
}

func (process *recordingProcess) Input(data []byte) error {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.closed {
		return io.ErrClosedPipe
	}
	process.inputs = append(process.inputs, append([]byte(nil), data...))
	return nil
}

func (process *recordingProcess) Resize(size Size) error {
	process.mu.Lock()
	if process.closed {
		process.mu.Unlock()
		return io.ErrClosedPipe
	}
	if process.resizeErr != nil {
		err := process.resizeErr
		process.mu.Unlock()
		return err
	}
	process.resizes = append(process.resizes, size)
	hook := process.resizeHook
	process.mu.Unlock()
	if hook != nil {
		hook(size)
	}
	return nil
}

func (process *recordingProcess) setResizeErr(err error) {
	process.mu.Lock()
	defer process.mu.Unlock()
	process.resizeErr = err
}

func (process *recordingProcess) setResizeHook(hook func(Size)) {
	process.mu.Lock()
	defer process.mu.Unlock()
	process.resizeHook = hook
}

func (process *recordingProcess) Output() <-chan []byte {
	return process.outputCh
}

func (process *recordingProcess) emitOutput(output string) {
	process.outputCh <- []byte(output)
}

func (process *recordingProcess) Kill() error {
	process.mu.Lock()
	process.killed = true
	process.mu.Unlock()
	process.exit(-1)
	return nil
}

func (process *recordingProcess) Wait() <-chan ProcessExit {
	return process.waitCh
}

func (process *recordingProcess) Close() error {
	process.mu.Lock()
	process.closed = true
	process.mu.Unlock()
	process.closeOutput()
	process.exit(-1)
	return nil
}

func (process *recordingProcess) snapshot() ([][]byte, []Size, bool, bool) {
	process.mu.Lock()
	defer process.mu.Unlock()
	inputs := make([][]byte, len(process.inputs))
	for i, input := range process.inputs {
		inputs[i] = append([]byte(nil), input...)
	}
	resizes := append([]Size(nil), process.resizes...)
	return inputs, resizes, process.killed, process.closed
}

func (process *recordingProcess) exit(code int) {
	process.exitOnce.Do(func() {
		process.closeOutput()
		process.waitCh <- ProcessExit{Code: code}
		close(process.waitCh)
	})
}

func (process *recordingProcess) closeOutput() {
	process.outputOnce.Do(func() {
		close(process.outputCh)
	})
}

func assertEventually(t *testing.T, timeout time.Duration, check func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(message)
}
