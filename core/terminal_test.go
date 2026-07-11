package core

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

	"github.com/lozzow/termx/core/history"
	"github.com/lozzow/termx/core/history/linehist"
	"github.com/lozzow/termx/core/live"
	vterm "github.com/lozzow/termx/vterm/vterm"
)

func TestTerminalLifecycleAndLiveSurface(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	info, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 40, Rows: 6},
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
	rows, err := server.LiveRows("term-1")
	if err != nil {
		t.Fatalf("initial live rows: %v", err)
	}
	initial := strings.Join(rows, "\n")
	if !strings.Contains(initial, "terminal started: term-1") || !strings.Contains(initial, "started at: ") {
		t.Fatalf("live rows must include terminal start marker, got %#v", rows)
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
	rows, err = server.LiveRows("term-1")
	if err != nil {
		t.Fatalf("live rows: %v", err)
	}
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "hello") || !strings.Contains(joined, "world") {
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
		return snapshot.Size.Cols == 8 && terminalSnapshotContainsRowText(snapshot, "abcdefg")
	}, "resize output should be applied after tap resize")
	snapshot := terminal.NativeScreenSnapshot("term-r373-resize-order")
	if snapshot.Size.Cols != 8 {
		t.Fatalf("tap latest screen should use resized width, got %#v", snapshot.Size)
	}
	rowText, rowCells, ok := terminalSnapshotRowText(snapshot, "abcdefg")
	if !ok {
		t.Fatalf("resize-triggered output row missing, snapshot=%#v", snapshot)
	}
	if got := len(rowCells); got != 7 {
		t.Fatalf("resize-triggered output should not wrap at old width, row cells=%d text=%q cells=%#v", got, rowText, rowCells)
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

func TestR396RestartFlushesPendingLiveQueueBeforePreservingScreen(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-r396-restart-live", Command: []string{"shell"}, Size: Size{Cols: 40, Rows: 8}}); err != nil {
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
	if !strings.Contains(strings.Join(rows, "|"), "terminal started: term-r396-restart-live") || !strings.Contains(strings.Join(rows, "|"), "started at: ") {
		t.Fatalf("restart live rows must include new start marker, rows=%#v", rows)
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
	historyTapQueue := terminal.historyTapQ
	terminal.queueMu.Unlock()
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
		terminal, err := server.Terminal("term-live-wait")
		if err != nil {
			errs <- err
			return
		}
		event, err := server.NextLiveInvalidation(ctx, "term-live-wait", terminal.LiveRevision())
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
	startLineID, ok := historyLineIDForText(window.Rows, "alpha")
	if !ok {
		t.Fatalf("history.window missing alpha row: %#v", window)
	}
	endLineID, ok := historyLineIDForText(window.Rows, "beta")
	if !ok {
		t.Fatalf("history.window missing beta row: %#v", window)
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
		Start:      history.HistoryCursor{LineID: startLineID, Valid: true},
		End:        history.HistoryCursor{LineID: endLineID, Valid: true},
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
		Limit:      5,
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

func TestR436TerminalDefaultsToLineHistoryStore(t *testing.T) {
	historyDir := t.TempDir()
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()), WithHistoryStorageDir(historyDir))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r436-linehist-default",
		Command: []string{"shell"},
		Size:    Size{Cols: 30, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	terminal, err := server.Terminal("term-r436-linehist-default")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	if terminal.lineHistory == nil {
		t.Fatal("default terminal history path must own a linehist store")
	}
	if _, ok := terminal.historyStore.(*linehist.Store); !ok {
		t.Fatalf("default terminal history store must be linehist, got %T", terminal.historyStore)
	}
	if err := server.IngestOutput(context.Background(), "term-r436-linehist-default", "alpha\r\nbeta\r\ngamma"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	logicalPath := filepath.Join(historyDir, "term-r436-linehist-default.logical-lines.bin")
	if _, err := os.Stat(logicalPath); err != nil {
		t.Fatalf("R436 default storage dir should write logical lines %s: %v", logicalPath, err)
	}
	window, err := server.TerminalHistoryWindow(context.Background(), "term-r436-linehist-default", history.HistoryWindowRequest{
		TerminalID: "term-r436-linehist-default",
		Mode:       history.HistoryWindowModeLatest,
		Limit:      3,
		Cols:       30,
	})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if got := strings.Join(historyRowTexts(window.Rows), "|"); got != "alpha|beta|gamma" {
		t.Fatalf("linehist terminal history window mismatch: %q rows=%#v", got, window.Rows)
	}
}
func TestR436HistoryStorageDirRecoversLineHistoryRows(t *testing.T) {
	historyDir := t.TempDir()
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory), WithHistoryStorageDir(historyDir))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-r436-recover-linehist",
		Command: []string{"shell"},
		Size:    Size{Cols: 30, Rows: 2},
	}); err != nil {
		t.Fatalf("register first terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r436-recover-linehist", "alpha\r\nbeta\r\ngamma\r\n"); err != nil {
		t.Fatalf("ingest recover rows: %v", err)
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown first server: %v", err)
	}

	recovered := NewServer(WithProcessFactory(newRecordingProcessFactory()), WithHistoryStorageDir(historyDir))
	if _, err := recovered.RegisterTerminal(TerminalRecord{
		ID:      "term-r436-recover-linehist",
		Command: []string{"shell"},
		Size:    Size{Cols: 30, Rows: 2},
	}); err != nil {
		t.Fatalf("register recovered terminal: %v", err)
	}
	window, err := recovered.TerminalHistoryWindow(context.Background(), "term-r436-recover-linehist", history.HistoryWindowRequest{
		TerminalID: "term-r436-recover-linehist",
		Mode:       history.HistoryWindowModeLatest,
		Limit:      20,
		Cols:       30,
	})
	if err != nil {
		t.Fatalf("recovered history window: %v", err)
	}
	if got := strings.Join(historyRowTexts(window.Rows), "|"); got != "alpha|beta|gamma" {
		t.Fatalf("recovered linehist rows mismatch: %q rows=%#v", got, window.Rows)
	}
	for _, row := range window.Rows {
		if !row.Committed || row.Segment != history.HistorySegmentCommitted {
			t.Fatalf("shutdown lifecycle tail must recover as committed cold history, row=%#v", row)
		}
	}
}

func TestR436HistoryStorageDirFailsWhenLineHistoryCannotOpen(t *testing.T) {
	blockingFile := filepath.Join(t.TempDir(), "blocking-file")
	badDir := filepath.Join(blockingFile, "child")
	if err := os.WriteFile(blockingFile, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatalf("seed blocking file: %v", err)
	}
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()), WithHistoryStorageDir(badDir))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-r436-bad-linehist", Command: []string{"shell"}}); err == nil {
		t.Fatal("R436 must fail terminal creation when linehist storage cannot open")
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
	raw := rawHistoryRowTexts(rows)
	texts := make([]string, 0, len(raw))
	for index, text := range raw {
		if isLifecycleHistoryRowTextAt(raw, index) {
			continue
		}
		texts = append(texts, text)
	}
	return texts
}

func historyRowsContain(rows []history.HistoryRow, needle string) bool {
	texts := rawHistoryRowTexts(rows)
	for index, text := range texts {
		if isLifecycleHistoryRowTextAt(texts, index) {
			continue
		}
		if strings.Contains(text, needle) {
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

func terminalSnapshotContainsRowText(snapshot NativeScreenSnapshot, want string) bool {
	_, _, ok := terminalSnapshotRowText(snapshot, want)
	return ok
}

func terminalSnapshotRowText(snapshot NativeScreenSnapshot, want string) (string, []vterm.Cell, bool) {
	for _, row := range snapshot.Rows {
		text := strings.TrimRight(terminalTestVTermRowText(row.Cells), " ")
		if text == want {
			return text, row.Cells, true
		}
	}
	return "", nil, false
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
