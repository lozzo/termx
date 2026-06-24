package termxcorev2

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-core-v2/live"
)

func TestTerminalLifecycleAndPipeline(t *testing.T) {
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
	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "hello" || window.Rows[1].Text != "world" {
		t.Fatalf("unexpected history window %#v", window)
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

func TestTerminalLiveSurfaceRepliesToOSCBackgroundQuery(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-osc-query",
		Command: []string{"codex"},
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

func TestTerminalHistoryPipelineClearsParserSegmentsAfterIngest(t *testing.T) {
	pipeline := newTerminalHistoryPipeline(20, 4)
	if err := pipeline.Ingest("\x1b[31mstyled\x1b[0m\nplain\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	if got := len(pipeline.ingest.segments); got != 0 {
		t.Fatalf("parser segments should be cleared after ingest, got %d", got)
	}
	window, err := pipeline.LatestWindow(20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "styled" || window.Rows[1].Text != "plain" {
		t.Fatalf("clearing parser segments must not clear stored history, got %#v", window.Rows)
	}
}

func TestTerminalIngestOutputPublishesLiveChangedEvent(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventTerminalChanged}})
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "live update\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	event := assertEventValue(t, events, EventTerminalChanged, "term-1")
	if event.Terminal == nil || event.Terminal.State != TerminalStateRunning {
		t.Fatalf("expected running terminal info on live changed event, got %#v", event)
	}
}

func TestTerminalLiveSnapshotDoesNotWaitForHistoryIngest(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	enteredHistory := make(chan struct{})
	releaseHistory := make(chan struct{})
	terminalHistoryPipelineBeforeIngestHook = func() {
		close(enteredHistory)
		<-releaseHistory
	}
	defer func() {
		terminalHistoryPipelineBeforeIngestHook = nil
	}()
	ingestDone := make(chan error, 1)
	go func() {
		ingestDone <- server.IngestOutput(context.Background(), "term-1", "live-first\nhistory-later")
	}()
	select {
	case <-enteredHistory:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for history ingest hook")
	}
	rows, err := server.LiveRows("term-1")
	if err != nil {
		t.Fatalf("live rows: %v", err)
	}
	if len(rows) == 0 || rows[0] != "live-first" {
		t.Fatalf("live snapshot should be available before history ingest resumes, got %#v", rows)
	}
	close(releaseHistory)
	if err := <-ingestDone; err != nil {
		t.Fatalf("ingest output: %v", err)
	}
}

func TestProcessOutputLiveSurfaceDoesNotWaitForHistoryQueue(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process("term-1")
	enteredHistory := make(chan struct{})
	releaseHistory := make(chan struct{})
	terminalHistoryPipelineBeforeIngestHook = func() {
		select {
		case <-enteredHistory:
		default:
			close(enteredHistory)
		}
		<-releaseHistory
	}
	defer func() {
		terminalHistoryPipelineBeforeIngestHook = nil
	}()

	process.emitOutput("process-live\nhistory-later")
	select {
	case <-enteredHistory:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async history worker")
	}
	assertEventually(t, time.Second, func() bool {
		rows, err := server.LiveRows("term-1")
		return err == nil && len(rows) > 0 && rows[0] == "process-live"
	}, "process live output should become visible while history worker is blocked")
	close(releaseHistory)
}

func TestTerminalIngestOutputNormalizesPTYCRLF(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "alpha\r\nbeta\r\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	rows, err := server.LiveRows("term-1")
	if err != nil {
		t.Fatalf("live rows: %v", err)
	}
	if len(rows) < 2 || rows[0] != "alpha" || rows[1] != "beta" {
		t.Fatalf("expected CRLF-normalized live rows, got %#v", rows)
	}
	window, err := server.LatestWindow("term-1", 80, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "alpha" || window.Rows[1].Text != "beta" {
		t.Fatalf("expected CRLF-normalized history rows, got %#v", window.Rows)
	}
}

func TestTerminalIngestOutputExpandsTabsForHistoryColumns(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "col1\tcol2\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	window, err := server.LatestWindow("term-1", 80, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "col1    col2" {
		t.Fatalf("history tab should materialize to next tab stop, got %#v", window.Rows)
	}
}

func TestTerminalIngestOutputNewlineOnlySealsUntilLineLeavesPrimaryScreen(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 40, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	terminal, err := server.Terminal("term-1")
	if err != nil {
		t.Fatalf("lookup terminal: %v", err)
	}
	terminal.mu.Lock()
	committed := terminal.history.CommittedIDs()
	frontier := terminal.history.FrontierIDs()
	committable := terminal.history.CommittableIDs()
	terminal.mu.Unlock()

	if got := committed; !reflect.DeepEqual(got, []history.LogicalLineID{1, 2}) {
		t.Fatalf("bottom newline should scroll out older sealed lines, got %v", got)
	}
	if got := frontier; !reflect.DeepEqual(got, []history.LogicalLineID{3}) {
		t.Fatalf("newest sealed line should remain screen-owned frontier, got %v", got)
	}
	if len(committable) != 0 {
		t.Fatalf("after commit, visible sealed lines must not remain committable, got %v", committable)
	}
}

func TestTerminalIngestOutputCarriageReturnOverwritesMutableTailWithoutCommitting(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\rT"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "Tne" {
		t.Fatalf("history latest should reflect CR overwrite in mutable tail, got %#v", window)
	}
	if window.TotalLines != 0 {
		t.Fatalf("carriage return overwrite must not create committed history, got total lines %d", window.TotalLines)
	}
}

func TestTerminalIngestOutputTreatsCRLFAsLineFeedWithoutLosingBareCR(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\r\ntwo\rT\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "one" || window.Rows[1].Text != "Two" {
		t.Fatalf("CRLF should commit a line while bare CR still edits tail, got %#v", window.Rows)
	}
}

func TestTerminalIngestOutputCursorBackwardOverwritesMutableTailWithoutCommitting(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "abcdef\x1b[3DXY"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "abcXYf" {
		t.Fatalf("history latest should reflect CUB overwrite in mutable tail, got %#v", window)
	}
	if window.TotalLines != 0 {
		t.Fatalf("cursor overwrite must not create committed history, got total lines %d", window.TotalLines)
	}
}

func TestTerminalIngestOutputCursorUpRewritesScreenOwnedHistoryLine(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 40, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	output := "Working\n\n\x1b[2A\rDone\x1b[K"
	if err := server.IngestOutput(context.Background(), "term-1", output); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 40, 8)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	text := historyWindowText(window.Rows)
	if strings.Contains(text, "Working") {
		t.Fatalf("screen row rewrite should remove transient Working, got %q rows=%#v", text, window.Rows)
	}
	if count := strings.Count(text, "Done"); count != 1 {
		t.Fatalf("screen row rewrite should keep one final Done, count=%d text=%q rows=%#v", count, text, window.Rows)
	}
	if len(window.Rows) < 2 || window.Rows[1].Text != "" {
		t.Fatalf("explicit blank row between dynamic lines should be preserved, got %#v", window.Rows)
	}
}

func TestTerminalIngestOutputBackspaceOverwritesMutableTailWithoutCommitting(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "abc\bX"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "abX" {
		t.Fatalf("history latest should reflect backspace overwrite in mutable tail, got %#v", window)
	}
	if window.TotalLines != 0 {
		t.Fatalf("backspace overwrite must not create committed history, got total lines %d", window.TotalLines)
	}
}

func TestTerminalIngestOutputDeletedAutosuggestionDoesNotPersistInHistory(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	// 中文说明：zsh/fish autosuggestion 会先把灰色建议写到屏幕上，
	// 再把光标退回并用 EL 清掉；history 只能保存清掉后的最终文本。
	if err := server.IngestOutput(context.Background(), "term-1", "l\x1b[90ms\x1b[0m\x1b[1D\x1b[K"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || strings.TrimRight(window.Rows[0].Text, " ") != "l" {
		t.Fatalf("deleted autosuggestion must not persist in history, got %#v", window)
	}
	if window.TotalLines != 0 {
		t.Fatalf("autosuggestion edit must stay mutable, got total lines %d", window.TotalLines)
	}
}

func TestTerminalIngestOutputWrapOnlyAffectsProjectionNotHistoryTruth(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "abcdef"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 3, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "abc" || window.Rows[1].Text != "def" {
		t.Fatalf("expected wrapped visual rows from one logical line, got %#v", window.Rows)
	}
	if window.TotalLines != 0 {
		t.Fatalf("plain wrap must not create committed history, got total lines %d", window.TotalLines)
	}

	terminal, err := server.Terminal("term-1")
	if err != nil {
		t.Fatalf("lookup terminal: %v", err)
	}
	terminal.mu.Lock()
	lineIDs := terminal.history.LineIDs()
	committed := terminal.history.CommittedIDs()
	frontier := terminal.history.FrontierIDs()
	line, ok := terminal.history.Line(1)
	terminal.mu.Unlock()

	if !ok {
		t.Fatal("expected wrapped content to stay in first logical line")
	}
	if !reflect.DeepEqual(lineIDs, []history.LogicalLineID{1}) {
		t.Fatalf("wrap must not create extra logical lines, got %v", lineIDs)
	}
	if line.Seal != history.SealStateOpen {
		t.Fatalf("wrap must not seal logical line, got %#v", line)
	}
	if len(committed) != 0 {
		t.Fatalf("wrap must not create committed history, got %v", committed)
	}
	if !reflect.DeepEqual(frontier, []history.LogicalLineID{1}) {
		t.Fatalf("wrap must keep logical line mutable in frontier, got %v", frontier)
	}
}

func TestTerminalIngestOutputEraseInLineMutatesMutableTailWithoutCommitting(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "hello\rhe\x1b[K"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || strings.TrimRight(window.Rows[0].Text, " ") != "he" {
		t.Fatalf("history latest should reflect EL mutation in mutable tail, got %#v", window)
	}
	if window.TotalLines != 0 {
		t.Fatalf("erase-in-line must not create committed history, got total lines %d", window.TotalLines)
	}
}

func TestTerminalIngestOutputStyledEraseInLineMatchesLiveBackgroundTail(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 8, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "\x1b[48;5;24mBG\x1b[K\x1b[0m"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	liveSnapshot, err := server.LiveSnapshot("term-1")
	if err != nil {
		t.Fatalf("live snapshot: %v", err)
	}
	if got := liveSnapshot.Screen.Cells[0][7].Style.BG; got != "idx:24" {
		t.Fatalf("live erase-to-EOL should keep bg on row tail, got %#v", liveSnapshot.Screen.Cells[0][7])
	}

	window, err := server.LatestWindow("term-1", 8, 4)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "BG      " {
		t.Fatalf("history latest should include styled erase blank footprint, got %#v", window.Rows)
	}
	cells := window.Rows[0].Cells
	if len(cells) != 8 {
		t.Fatalf("history erase footprint should project as 8 cells, got %#v", cells)
	}
	for i, cell := range cells {
		if cell.Width != 1 || cell.Style.BG != "idx:24" {
			t.Fatalf("expected cell %d to keep bg from erase-to-EOL, got %#v", i, cell)
		}
	}
}

func TestTerminalIngestOutputPlainStyledTextDoesNotPadBackgroundTail(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 8, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "\x1b[48;5;24mBG\x1b[0m"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 8, 4)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "BG" {
		t.Fatalf("plain styled text should not synthesize row tail blanks, got %#v", window.Rows)
	}
}

func TestTerminalIngestOutputScrollWrappedRowPreservesBackgroundFootprint(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 8, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	output := "seed1\nseed2\n\x1b[48;5;24mabcdefghij\x1b[0m\n"
	if err := server.IngestOutput(context.Background(), "term-1", output); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	liveSnapshot, err := server.LiveSnapshot("term-1")
	if err != nil {
		t.Fatalf("live snapshot: %v", err)
	}
	if got := liveSnapshot.Screen.Cells[1][7].Style.BG; got != "idx:24" {
		t.Fatalf("live scrolled wrapped row tail should keep bg, got %#v", liveSnapshot.Screen.Cells[1])
	}

	window, err := server.LatestWindow("term-1", 8, 6)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) < 3 || window.Rows[len(window.Rows)-2].Text != "abcdefgh" || window.Rows[len(window.Rows)-1].Text != "ij" {
		t.Fatalf("history window should carry scrolled wrapped row bg footprint, got %#v", window.Rows)
	}
	cells := window.Rows[len(window.Rows)-1].Cells
	if len(cells) != 2 {
		t.Fatalf("wrapped continuation row should not materialize tail blanks as cells, got %#v", cells)
	}
	if cells[0].Text != "i" || cells[0].Width != 1 || cells[0].Style.BG != "idx:24" {
		t.Fatalf("first wrapped cell should keep bg, got %#v", cells[0])
	}
	if cells[1].Text != "j" || cells[1].Width != 1 || cells[1].Style.BG != "idx:24" {
		t.Fatalf("second wrapped cell should keep bg, got %#v", cells[1])
	}
	tail := window.Rows[len(window.Rows)-1].TailFill
	if tail == nil || tail.Style.BG != "idx:24" {
		t.Fatalf("tail footprint should be row tail fill metadata, got %#v row=%#v", tail, window.Rows[len(window.Rows)-1])
	}
}

func TestTerminalIngestOutputControlledHistorySemantics(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 40, Rows: 8},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	output := "" +
		"HSEM_BEGIN\n" +
		"\x1b[48;5;24mHSEM_EL_TO_EOL\x1b[K\x1b[0m\n" +
		"HSEM_CR_OLD_TRAIL\rHSEM_CR_FINAL\x1b[K\n" +
		"HSEM_GAP\x1b[3CX\n" +
		"HSEM_T\tX\n" +
		"HSEM_SUGGEST\x1b[90m_TMP\x1b[0m\x1b[4D\x1b[K\n"
	if err := server.IngestOutput(context.Background(), "term-1", output); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 40, 16)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	text := historyWindowText(window.Rows)
	for _, want := range []string{
		"HSEM_BEGIN",
		"HSEM_EL_TO_EOL",
		"HSEM_CR_FINAL",
		"HSEM_GAP   X",
		"HSEM_T  X",
		"HSEM_SUGGEST",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected history text to contain %q, got %q rows=%#v", want, text, window.Rows)
		}
	}
	for _, notWant := range []string{"HSEM_CR_OLD_TRAIL", "HSEM_SUGGEST_TMP"} {
		if strings.Contains(text, notWant) {
			t.Fatalf("history text should not contain transient text %q, got %q rows=%#v", notWant, text, window.Rows)
		}
	}
	var eraseRow *history.VisualRow
	for i := range window.Rows {
		if strings.HasPrefix(window.Rows[i].Text, "HSEM_EL_TO_EOL") {
			eraseRow = &window.Rows[i]
			break
		}
	}
	if eraseRow == nil {
		t.Fatalf("expected erase-to-EOL row in %#v", window.Rows)
	}
	if len(eraseRow.Cells) < 40 {
		t.Fatalf("erase-to-EOL row should keep styled blank footprint to screen cols, got %#v", eraseRow.Cells)
	}
	for i, cell := range eraseRow.Cells {
		if cell.Style.BG != "idx:24" {
			t.Fatalf("erase-to-EOL cell %d should keep bg, got %#v", i, cell)
		}
	}
}

func TestTerminalIngestOutputEraseInLineModeOneClearsMutablePrefixWithoutCommitting(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "hello\rhe\x1b[1K"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "   lo" {
		t.Fatalf("history latest should reflect EL 1 mutation in mutable tail, got %#v", window)
	}
	if window.TotalLines != 0 {
		t.Fatalf("erase-in-line mode 1 must not create committed history, got total lines %d", window.TotalLines)
	}
}

func TestTerminalIngestOutputEraseInLineModeTwoClearsWholeMutableLineWithoutCommitting(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "hello\rhe\x1b[2K"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "     " {
		t.Fatalf("history latest should reflect EL 2 mutation in mutable tail, got %#v", window)
	}
	if window.TotalLines != 0 {
		t.Fatalf("erase-in-line mode 2 must not create committed history, got total lines %d", window.TotalLines)
	}
}

func TestTerminalIngestOutputClearScreenCommitsCurrentScreenPage(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	before, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest before clear: %v", err)
	}
	if before.TotalLines == 0 {
		t.Fatalf("expected committed history before clear, got %#v", before)
	}

	if err := server.IngestOutput(context.Background(), "term-1", "\x1b[2J"); err != nil {
		t.Fatalf("ingest clear screen: %v", err)
	}
	after, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest after clear: %v", err)
	}
	if len(after.Rows) != 4 || after.Rows[0].Text != "one" || after.Rows[1].Text != "two" || after.Rows[2].Text != "three" || after.Rows[3].Text != "four" {
		t.Fatalf("ED 2 should keep the clear-screen page in history, got %#v", after)
	}
	if after.TotalLines != 4 || after.TotalLines <= before.TotalLines {
		t.Fatalf("ED 2 should commit current screen page before clearing, before=%d after=%d", before.TotalLines, after.TotalLines)
	}
}

func TestTerminalIngestOutputEraseDisplayFromCursorClearsMutableTailOnly(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour\rfo\x1b[0J"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest after ED 0: %v", err)
	}
	if len(window.Rows) != 4 || window.Rows[0].Text != "one" || window.Rows[1].Text != "two" || window.Rows[2].Text != "three" || strings.TrimRight(window.Rows[3].Text, " ") != "fo" {
		t.Fatalf("ED 0 should keep committed rows and clear only mutable tail below cursor, got %#v", window)
	}
	if window.TotalLines != 2 {
		t.Fatalf("ED 0 must not create or truncate committed history, got total lines %d", window.TotalLines)
	}
}

func TestTerminalIngestOutputEraseDisplayToCursorClearsMutablePrefixOnly(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour\rfo\x1b[1J"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest after ED 1: %v", err)
	}
	if len(window.Rows) != 3 || window.Rows[0].Text != "one" || window.Rows[1].Text != "two" || window.Rows[2].Text != "   r" {
		t.Fatalf("ED 1 should clear mutable rows above cursor and active prefix only, got %#v", window)
	}
	if window.TotalLines != 2 {
		t.Fatalf("ED 1 must not create or truncate committed history, got total lines %d", window.TotalLines)
	}
}

func TestTerminalIngestOutputClearScrollbackTruncatesCommittedHistory(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "\x1b[3J"); err != nil {
		t.Fatalf("ingest clear scrollback: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest after clear scrollback: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "three" || window.Rows[1].Text != "four" || window.TotalLines != 0 {
		t.Fatalf("ED 3 should clear committed history but keep mutable tail, got %#v", window)
	}
}

func TestTerminalIngestOutputAltScreenExitAppendsFinalFrameToHistory(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\n\x1b[?1049halt-tail\n\x1b[?1049lafter"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest after alt-screen: %v", err)
	}
	if len(window.Rows) != 3 || window.Rows[0].Text != "one" || window.Rows[1].Text != "alt-tail" || window.Rows[2].Text != "after" || window.TotalLines != 2 {
		t.Fatalf("alt-screen final frame should enter primary history before following primary output, got %#v", window)
	}
}

func TestTerminalIngestOutputAltScreenExitAppendsLastLiveFrame(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "primary\n\x1b[?1049h\x1b[2Jalt-final\x1b[?1049l"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	liveSnapshot, err := server.LiveSnapshot("term-1")
	if err != nil {
		t.Fatalf("live snapshot: %v", err)
	}
	if liveSnapshot.Modes.AlternateScreen || liveSnapshot.Screen.IsAlternateScreen {
		t.Fatalf("alt exit should keep final frame as primary live screen, got modes=%#v screen=%#v", liveSnapshot.Modes, liveSnapshot.Screen)
	}
	liveRows, err := server.LiveRows("term-1")
	if err != nil {
		t.Fatalf("live rows: %v", err)
	}
	if got := strings.Join(liveRows, "\n"); !strings.Contains(got, "alt-final") || !strings.Contains(got, "primary") {
		t.Fatalf("live surface should keep primary tail and append alt final frame, got %q", got)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "primary" || window.Rows[1].Text != "alt-final" || window.TotalLines != 2 {
		t.Fatalf("alt final frame should enter primary history, got %#v", window)
	}
}

func TestTerminalIngestOutputEnterAltScreenCommitsPrimaryPageFirst(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour\x1b[?1049h\x1b[2Jhalt-tail\x1b[?1049lafter"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest after alt-screen: %v", err)
	}
	if len(window.Rows) != 6 || window.Rows[0].Text != "one" || window.Rows[1].Text != "two" || window.Rows[2].Text != "three" || window.Rows[3].Text != "four" || window.Rows[4].Text != "halt-tail" || window.Rows[5].Text != "after" {
		t.Fatalf("enter alt-screen should preserve primary tail, append alt final frame, then continue primary, got %#v", window)
	}
	if window.TotalLines != 5 {
		t.Fatalf("enter alt-screen should commit primary page and alt final frame, got total=%d", window.TotalLines)
	}
}

func TestTerminalIngestOutputAltScreenFinalFrameKeepsHistoryStyle(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "primary\n\x1b[?1049h\x1b[2J\x1b[31;44mALT\x1b[0m\x1b[?1049l"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest after styled alt-screen: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[1].Text != "ALT" {
		t.Fatalf("expected styled alt final frame in history, got %#v", window.Rows)
	}
	cells := window.Rows[1].Cells
	if len(cells) < 1 || cells[0].Style.FG != "ansi:1" || cells[0].Style.BG != "ansi:4" {
		t.Fatalf("expected alt final frame style in history, got %#v", cells)
	}
}

func TestTerminalIngestOutputSplitAltScreenCSIStillCapturesFinalFrame(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	for _, chunk := range []string{"primary\n\x1b[?10", "49h\x1b[2Jalt-final\x1b[?1049l"} {
		if err := server.IngestOutput(context.Background(), "term-1", chunk); err != nil {
			t.Fatalf("ingest chunk %q: %v", chunk, err)
		}
	}

	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest after split alt-screen CSI: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "primary" || window.Rows[1].Text != "alt-final" {
		t.Fatalf("split alt-screen CSI should preserve final frame, got %#v", window.Rows)
	}
}

func TestTerminalIngestOutputEnterAltScreenPreservesStressTail(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 120, Rows: 20},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	var output strings.Builder
	for i := 1; i <= 100; i++ {
		output.WriteString(stressHistoryLine(i))
		output.WriteByte('\n')
	}
	output.WriteString("\x1b[?1049h\x1b[2JALT_SCREEN_MARK\x1b[?1049l")
	if err := server.IngestOutput(context.Background(), "term-1", output.String()); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 120, 30)
	if err != nil {
		t.Fatalf("latest after stress alt-screen: %v", err)
	}
	if !historyWindowContainsText(window, "000100") {
		t.Fatalf("enter alt-screen must preserve primary stress tail in history, got %#v", window.Rows)
	}
	if !historyWindowContainsText(window, "ALT_SCREEN_MARK") {
		t.Fatalf("alt-screen final frame should enter primary history on exit, got %#v", window.Rows)
	}
}

func TestTerminalIngestOutputFullscreenHomeClearPreservesStressTail(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 120, Rows: 42},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	var output strings.Builder
	for i := 1; i <= 100; i++ {
		output.WriteString(stressHistoryLine(i))
		output.WriteByte('\n')
	}
	output.WriteString("\x1b[?25l\x1b[H\x1b[JCODEX_FULLSCREEN_MARK")
	if err := server.IngestOutput(context.Background(), "term-1", output.String()); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 120, 120)
	if err != nil {
		t.Fatalf("latest after fullscreen home clear: %v", err)
	}
	for i := 59; i <= 100; i++ {
		marker := fmt.Sprintf("%06d", i)
		if !historyWindowContainsText(window, marker) {
			t.Fatalf("fullscreen home clear must preserve primary screen line %s, total=%d rows=%#v", marker, window.TotalLines, window.Rows)
		}
	}
	if window.TotalLines < 100 {
		t.Fatalf("fullscreen home clear must commit all primary stress lines, got total=%d rows=%#v", window.TotalLines, window.Rows)
	}
	if !historyWindowContainsText(window, "CODEX_FULLSCREEN_MARK") {
		t.Fatalf("active primary fullscreen current frame should stay visible in latest, got %#v", window.Rows)
	}
	if window.TotalLines != 100 {
		t.Fatalf("active primary fullscreen current frame must not count as committed history, got total=%d rows=%#v", window.TotalLines, window.Rows)
	}
}

func TestTerminalIngestOutputRepeatedFullscreenHomeClearKeepsLatestFrameOnly(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 80, Rows: 8},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	output := strings.Join([]string{
		"shell-one\nshell-two",
		"\x1b[?25l\x1b[H\x1b[Jframe-one\nframe-old",
		"\x1b[H\x1b[Jframe-two\nframe-new",
	}, "")
	if err := server.IngestOutput(context.Background(), "term-1", output); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 80, 10)
	if err != nil {
		t.Fatalf("latest after repeated fullscreen clear: %v", err)
	}
	for _, want := range []string{"shell-one", "shell-two"} {
		if !historyWindowContainsText(window, want) {
			t.Fatalf("latest should contain %q, total=%d rows=%#v", want, window.TotalLines, window.Rows)
		}
	}
	for _, stale := range []string{"frame-one", "frame-old"} {
		if historyWindowContainsText(window, stale) {
			t.Fatalf("latest should not contain stale fullscreen repaint %q, total=%d rows=%#v", stale, window.TotalLines, window.Rows)
		}
	}
	for _, want := range []string{"frame-two", "frame-new"} {
		if !historyWindowContainsText(window, want) {
			t.Fatalf("latest should contain current fullscreen frame %q, total=%d rows=%#v", want, window.TotalLines, window.Rows)
		}
	}
	if window.TotalLines != 2 {
		t.Fatalf("only pre-fullscreen page should count as committed history, got total=%d rows=%#v", window.TotalLines, window.Rows)
	}
}

func TestTerminalIngestOutputPrimaryFullscreenCursorShowKeepsRunningFrameMutable(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 80, Rows: 8},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	output := strings.Join([]string{
		"shell-one\nshell-two",
		"\x1b[?25l\x1b[H\x1b[Jframe-header\nframe-body",
		"\x1b[4;1H> Summarize recent commits\x1b[?25h",
	}, "")
	if err := server.IngestOutput(context.Background(), "term-1", output); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 80, 10)
	if err != nil {
		t.Fatalf("latest after cursor-show repaint: %v", err)
	}
	for _, want := range []string{"shell-one", "shell-two"} {
		if !historyWindowContainsText(window, want) {
			t.Fatalf("latest should preserve pre-fullscreen shell history %q, total=%d rows=%#v", want, window.TotalLines, window.Rows)
		}
	}
	for _, want := range []string{"frame-header", "frame-body", "Summarize recent commits"} {
		if !historyWindowContainsText(window, want) {
			t.Fatalf("cursor-show inside primary fullscreen should expose current frame %q as mutable latest tail, total=%d rows=%#v", want, window.TotalLines, window.Rows)
		}
	}
	if window.TotalLines != 2 {
		t.Fatalf("running primary fullscreen frame should stay outside committed history depth, got total=%d rows=%#v", window.TotalLines, window.Rows)
	}
}

func TestTerminalIngestOutputPrimaryFullscreenNewlinesDoNotCommitRunningFrame(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 80, Rows: 8},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	output := strings.Join([]string{
		"shell-one\nshell-two",
		"\x1b[?25l\x1b[H\x1b[Jframe-a\nframe-b\nframe-c",
		"\x1b[H\x1b[Jframe-d\nframe-e",
	}, "")
	if err := server.IngestOutput(context.Background(), "term-1", output); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 80, 20)
	if err != nil {
		t.Fatalf("latest after fullscreen frame newlines: %v", err)
	}
	for _, stale := range []string{"frame-a", "frame-b", "frame-c"} {
		if historyWindowContainsText(window, stale) {
			t.Fatalf("primary fullscreen newlines must not keep stale repaint frame %q, total=%d rows=%#v", stale, window.TotalLines, window.Rows)
		}
	}
	for _, want := range []string{"frame-d", "frame-e"} {
		if !historyWindowContainsText(window, want) {
			t.Fatalf("primary fullscreen latest should show current repaint frame %q, total=%d rows=%#v", want, window.TotalLines, window.Rows)
		}
	}
	if window.TotalLines != 2 {
		t.Fatalf("only pre-fullscreen shell history should count as committed, got total=%d rows=%#v", window.TotalLines, window.Rows)
	}
}

func TestTerminalIngestOutputCodexTmuxRawFrameKeepsCurrentInputAndStatus(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 160, Rows: 48},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	output := strings.Join([]string{
		"before-one\nbefore-two\n",
		"\x1b[?2026h",
		"\x1b[?1004h",
		"\x1b[5;1H\x1b[J",
		"\x1b[5;48r\x1b[5;1H\x1bM\x1bM\x1bM\x1bM\x1bM\x1bM\x1bM",
		"\x1b[r\x1b[1;11r\x1b[4;1H",
		"\x1b[39;49m\x1b[K╭─ update ─╮",
		"\x1b[13;1H╭─ OpenAI Codex ─╮",
		"\x1b[21;1H› \x1b[21;3HSummarize recent commits",
		"\x1b[23;3Hgpt-5.5 xhigh · ~/Documents/workdir/termx",
		"\x1b[?2026l",
	}, "")
	if err := server.IngestOutput(context.Background(), "term-1", output); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := server.LatestWindow("term-1", 160, 20)
	if err != nil {
		t.Fatalf("latest after codex tmux raw frame: %v", err)
	}
	for _, want := range []string{"before-one", "before-two", "Summarize recent commits", "gpt-5.5 xhigh"} {
		if !historyWindowContainsText(window, want) {
			t.Fatalf("latest should contain %q, total=%d rows=%#v", want, window.TotalLines, window.Rows)
		}
	}
	if window.TotalLines != 2 {
		t.Fatalf("Codex raw frame must stay mutable outside committed history depth, got total=%d rows=%#v", window.TotalLines, window.Rows)
	}

	repaint := strings.Join([]string{
		"\x1b[?2026h",
		"\x1b[5;1H\x1b[J",
		"\x1b[5;48r\x1b[5;1H\x1bM\x1bM",
		"\x1b[r\x1b[1;11r\x1b[4;1H",
		"\x1b[21;1H› \x1b[21;3HExplain this codebase",
		"\x1b[23;3Hgpt-5.5 xhigh · ~/Documents/workdir/termx",
		"\x1b[?2026l",
	}, "")
	if err := server.IngestOutput(context.Background(), "term-1", repaint); err != nil {
		t.Fatalf("ingest repaint: %v", err)
	}
	window, err = server.LatestWindow("term-1", 160, 20)
	if err != nil {
		t.Fatalf("latest after codex tmux repaint: %v", err)
	}
	if historyWindowContainsText(window, "Summarize recent commits") {
		t.Fatalf("repeated Codex raw repaint must replace previous input frame, got rows=%#v", window.Rows)
	}
	for _, want := range []string{"before-one", "before-two", "Explain this codebase", "gpt-5.5 xhigh"} {
		if !historyWindowContainsText(window, want) {
			t.Fatalf("repaint latest should contain %q, total=%d rows=%#v", want, window.TotalLines, window.Rows)
		}
	}
	if window.TotalLines != 2 {
		t.Fatalf("Codex raw repaint must not increase committed history depth, got total=%d rows=%#v", window.TotalLines, window.Rows)
	}
}

func stressHistoryLine(n int) string {
	return fmt.Sprintf("%06d [DEBUG ] stream pending id=%06d path=/var/tmp/alpha/beta/gamma wrap============================================== tail-marker", n, n)
}

func historyWindowContainsText(window history.HistoryWindow, want string) bool {
	for _, row := range window.Rows {
		if strings.Contains(row.Text, want) {
			return true
		}
	}
	return false
}

func TestTerminalIngestOutputPreservesANSIStylesInHistoryCells(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}, Size: Size{Cols: 20, Rows: 4}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	output := "\x1b[1;31mERR\x1b[0m \x1b[4;38;2;255;204;0m好\x1b[0m \x1b[48;5;12mBG\x1b[49m\nplain"
	if err := server.IngestOutput(context.Background(), "term-1", output); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "ERR 好 BG" || window.Rows[1].Text != "plain" {
		t.Fatalf("unexpected styled history rows %#v", window.Rows)
	}
	cells := window.Rows[0].Cells
	if len(cells) != 5 {
		t.Fatalf("expected styled runs to survive ingest, got %#v", cells)
	}
	if cells[0].Text != "ERR" || cells[0].Width != 3 || cells[0].Style.FG != "ansi:1" || !cells[0].Style.Bold {
		t.Fatalf("expected red bold ERR cell, got %#v", cells[0])
	}
	if cells[2].Text != "好" || cells[2].Width != 2 || cells[2].Style.FG != "#ffcc00" || !cells[2].Style.Underline {
		t.Fatalf("expected truecolor underlined wide cell, got %#v", cells[2])
	}
	if cells[4].Text != "BG" || cells[4].Style.BG != "idx:12" || cells[4].Style.FG != "" {
		t.Fatalf("expected indexed background cell with reset foreground, got %#v", cells[4])
	}
	if window.Rows[1].Cells[0].Text != "plain" || window.Rows[1].Cells[0].Style != (history.CellStyle{}) {
		t.Fatalf("expected SGR reset to keep following line plain, got %#v", window.Rows[1].Cells)
	}
}

func TestTerminalIngestOutputBatchesTextRunsWithoutCrossingControlEvents(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}, Size: Size{Cols: 20, Rows: 4}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	output := "\x1b[31mred\x1b[0m-\x1b[32mgreen\x1b[0m\rOK\x1b[K\nnext"
	if err := server.IngestOutput(context.Background(), "term-1", output); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 2 || strings.TrimRight(window.Rows[0].Text, " ") != "OK" || window.Rows[1].Text != "next" {
		t.Fatalf("control events must still split batched text runs correctly, got %#v", window.Rows)
	}
	cells := window.Rows[0].Cells
	if len(cells) == 0 || cells[0].Text != "O" || cells[0].Style != (history.CellStyle{}) {
		t.Fatalf("carriage return and erase-in-line should leave only plain OK, got %#v", cells)
	}
}

func TestTerminalIngestOutputCarriesANSIStateAcrossChunks(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}, Size: Size{Cols: 20, Rows: 4}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	for _, chunk := range []string{"\x1b[", "31mred ", "tail\x1b[0m\n"} {
		if err := server.IngestOutput(context.Background(), "term-1", chunk); err != nil {
			t.Fatalf("ingest output chunk %q: %v", chunk, err)
		}
	}
	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "red tail" {
		t.Fatalf("unexpected history rows %#v", window.Rows)
	}
	cells := window.Rows[0].Cells
	if len(cells) != 2 {
		t.Fatalf("expected same SGR style to carry across output chunks, got %#v", cells)
	}
	if cells[0].Text != "red " || cells[0].Style.FG != "ansi:1" || cells[1].Text != "tail" || cells[1].Style.FG != "ansi:1" {
		t.Fatalf("expected red style across chunks, got %#v", cells)
	}
}

func TestTerminalIngestOutputPreservesOSC8LinksAndSkipsControls(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}, Size: Size{Cols: 40, Rows: 4}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	output := "\x1b]2;ignored title\a\x1b]8;id=termx;https://example.test\a"
	output += "linked\x1b]8;;\aplain\x1b[?25l\n"
	if err := server.IngestOutput(context.Background(), "term-1", output); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	window, err := server.LatestWindow("term-1", 40, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "linkedplain" {
		t.Fatalf("control sequences must not leak into history text, got %#v", window.Rows)
	}
	cells := window.Rows[0].Cells
	if len(cells) != 2 {
		t.Fatalf("expected link and plain cells, got %#v", cells)
	}
	if cells[0].Text != "linked" || cells[0].LinkURL != "https://example.test" || cells[0].LinkParams != "id=termx" {
		t.Fatalf("expected OSC 8 link metadata on first cell, got %#v", cells[0])
	}
	if cells[1].Text != "plain" || cells[1].LinkURL != "" || cells[1].LinkParams != "" {
		t.Fatalf("expected OSC 8 reset before plain text, got %#v", cells[1])
	}
}

func TestTerminalIngestOutputSkipsStringControlsAcrossChunks(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}, Size: Size{Cols: 40, Rows: 4}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	for _, chunk := range []string{"before", "\x1bPignored ", "payload\x1b\\after\n"} {
		if err := server.IngestOutput(context.Background(), "term-1", chunk); err != nil {
			t.Fatalf("ingest output chunk %q: %v", chunk, err)
		}
	}
	window, err := server.LatestWindow("term-1", 40, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "beforeafter" {
		t.Fatalf("DCS payload must not leak into history text, got %#v", window.Rows)
	}
}

func TestTerminalIngestOutputSGRTrailingDefaultResetsStyle(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}, Size: Size{Cols: 40, Rows: 4}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "\x1b[31;mplain\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	window, err := server.LatestWindow("term-1", 40, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 1 || len(window.Rows[0].Cells) != 1 || window.Rows[0].Cells[0].Style != (history.CellStyle{}) {
		t.Fatalf("trailing empty SGR parameter should reset style, got %#v", window.Rows)
	}
}

func TestTerminalRestartResetsHistoryIngestParserState(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "\x1b[31mred \x1b]8;id=old;https://old.test\alinked \x1b["); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	if err := server.RestartTerminal(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "plain\n"); err != nil {
		t.Fatalf("ingest output after restart: %v", err)
	}
	window, err := server.LatestWindow("term-1", 40, 10)
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "red linked " || window.Rows[1].Text != "plain" || len(window.Rows[1].Cells) != 1 {
		t.Fatalf("unexpected history after restart %#v", window.Rows)
	}
	cell := window.Rows[1].Cells[0]
	if cell.Style != (history.CellStyle{}) || cell.LinkURL != "" || cell.LinkParams != "" {
		t.Fatalf("restart should reset style/link/pending parser state, got %#v", cell)
	}
}

func TestTerminalResizeProcessFailureDoesNotChangeRegistryOrLiveSize(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 10, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process("term-1")
	resizeErr := errors.New("resize failed")
	process.setResizeErr(resizeErr)
	if err := server.ResizeTerminal(context.Background(), "term-1", 20, 5); !errors.Is(err, resizeErr) {
		t.Fatalf("expected resize failure, got %v", err)
	}
	info, err := server.GetTerminal("term-1")
	if err != nil {
		t.Fatalf("get terminal: %v", err)
	}
	if info.Size != (Size{Cols: 10, Rows: 3}) {
		t.Fatalf("expected registry size to remain unchanged, got %#v", info.Size)
	}
	terminal, err := server.Terminal("term-1")
	if err != nil {
		t.Fatalf("get terminal handle: %v", err)
	}
	if got := terminal.live.Size(); got != (live.SurfaceSize{Cols: 10, Rows: 3}) {
		t.Fatalf("expected live size to remain unchanged, got %#v", got)
	}
}

func TestTerminalResizeAppliesHistoryDirection(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    Size{Cols: 10, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	beforeGrow, err := server.LatestWindow("term-1", 10, 10)
	if err != nil {
		t.Fatalf("latest before grow: %v", err)
	}
	if beforeGrow.TotalLines != 1 || len(beforeGrow.Rows) != 2 || beforeGrow.Rows[0].Text != "one" || beforeGrow.Rows[1].Text != "two" {
		t.Fatalf("bottom newline should scroll out oldest line before grow, got %#v", beforeGrow)
	}
	if err := server.ResizeTerminal(context.Background(), "term-1", 10, 3); err != nil {
		t.Fatalf("grow resize: %v", err)
	}
	grown, err := server.LatestWindow("term-1", 10, 10)
	if err != nil {
		t.Fatalf("latest after grow: %v", err)
	}
	if grown.TotalLines != 0 || len(grown.Rows) != 2 || grown.Rows[0].Text != "one" || grown.Rows[1].Text != "two" {
		t.Fatalf("grow resize should reveal hidden/visible frontier without manufacturing committed rows, got %#v", grown)
	}
	if err := server.ResizeTerminal(context.Background(), "term-1", 10, 2); err != nil {
		t.Fatalf("shrink resize: %v", err)
	}
	shrunk, err := server.LatestWindow("term-1", 10, 10)
	if err != nil {
		t.Fatalf("latest after shrink: %v", err)
	}
	if shrunk.TotalLines != 0 || len(shrunk.Rows) != 1 || shrunk.Rows[0].Text != "two" {
		t.Fatalf("shrink resize should hide the oldest visible frontier row, got %#v", shrunk)
	}
}

func TestTerminalRestartReplacesProcessAndPreservesLiveAndHistory(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	first := factory.process("term-1")
	if err := server.IngestOutput(context.Background(), "term-1", "before\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	beforeRestart, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest before restart: %v", err)
	}
	if err := server.RestartTerminal(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	_, _, _, firstClosed := first.snapshot()
	if !firstClosed {
		t.Fatal("expected old process to be closed")
	}
	second := factory.process("term-1")
	if second == nil || second == first {
		t.Fatal("expected replacement process")
	}
	rows, err := server.LiveRows("term-1")
	if err != nil {
		t.Fatalf("live rows: %v", err)
	}
	if len(rows) == 0 || rows[0] != "before" {
		t.Fatalf("restart should preserve live tail, got %#v", rows)
	}
	snapshot, err := server.LiveSnapshot("term-1")
	if err != nil {
		t.Fatalf("live snapshot after restart: %v", err)
	}
	if !snapshot.Cursor.Visible || snapshot.Cursor.Row != 1 || snapshot.Cursor.Col != 0 {
		t.Fatalf("restart should map live cursor to preserved-tail append row, got %#v", snapshot.Cursor)
	}
	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "before" {
		t.Fatalf("restart should preserve history before new output, got %#v", window)
	}
	if window.Generation <= beforeRestart.Generation {
		t.Fatalf("restart should force a new history boundary generation, before=%d after=%d", beforeRestart.Generation, window.Generation)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "after\n"); err != nil {
		t.Fatalf("ingest output after restart: %v", err)
	}
	rows, err = server.LiveRows("term-1")
	if err != nil {
		t.Fatalf("live rows after restart output: %v", err)
	}
	if !reflect.DeepEqual(rows, []string{"before", "after"}) {
		t.Fatalf("restart should keep old and new live tail rows, got %#v", rows)
	}
	snapshot, err = server.LiveSnapshot("term-1")
	if err != nil {
		t.Fatalf("live snapshot after restart output: %v", err)
	}
	if !snapshot.Cursor.Visible || snapshot.Cursor.Row != 2 || snapshot.Cursor.Col != 5 {
		t.Fatalf("restart output should advance the real live cursor, got %#v", snapshot.Cursor)
	}
	window, err = server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window after restart output: %v", err)
	}
	if len(window.Rows) != 2 || window.Rows[0].Text != "before" || window.Rows[1].Text != "after" {
		t.Fatalf("restart should append new output to preserved history, got %#v", window)
	}
}

func TestTerminalRestartExitsAltScreenHistoryBoundary(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}, Size: Size{Cols: 20, Rows: 4}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "primary-tail\x1b[?1049h\x1b[2JALT"); err != nil {
		t.Fatalf("ingest alt-screen output: %v", err)
	}
	if err := server.RestartTerminal(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "after-restart\n"); err != nil {
		t.Fatalf("ingest output after restart: %v", err)
	}
	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	text := historyWindowText(window.Rows)
	if !strings.Contains(text, "primary-tail") || !strings.Contains(text, "after-restart") {
		t.Fatalf("restart should leave alt-screen and keep new output in history, got %#v", window.Rows)
	}
	if strings.Contains(text, "ALT") {
		t.Fatalf("restart must not synthesize unfinished alt-screen frame into history, got %#v", window.Rows)
	}
}

func TestTerminalRestartAfterExitPreservesCommittedHistory(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventTerminalExited}})
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "open-tail"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	factory.process("term-1").exit(5)
	assertEventValue(t, events, EventTerminalExited, "term-1")
	if err := server.RestartTerminal(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart terminal: %v", err)
	}
	window, err := server.LatestWindow("term-1", 80, 10)
	if err != nil {
		t.Fatalf("latest window after restart: %v", err)
	}
	text := historyWindowText(window.Rows)
	if !strings.Contains(text, "open-tail") || !strings.Contains(text, "terminal exited: term-1 code:5 exited") {
		t.Fatalf("restart after exit should keep committed history, got %#v", window)
	}
}

func TestTerminalExitMarkerEntersLiveAndHistory(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventTerminalExited}})
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "termx-main", Command: []string{"/bin/zsh"}, Size: Size{Cols: 80, Rows: 10}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "termx-main", "before-exit"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	factory.process("termx-main").exit(0)
	event := assertEventValue(t, events, EventTerminalExited, "termx-main")
	exitedAt := event.Terminal.ExitedAt.UTC().Format(time.RFC3339)

	rows, err := server.LiveRows("termx-main")
	if err != nil {
		t.Fatalf("live rows: %v", err)
	}
	liveText := strings.Join(rows, "\n")
	for _, want := range []string{
		"before-exit",
		"terminal exited: termx-main code:0 exited",
		"exited at: " + exitedAt,
		"command: /bin/zsh",
	} {
		if !strings.Contains(liveText, want) {
			t.Fatalf("exit marker should enter live rows, missing %q in %#v", want, rows)
		}
	}

	window, err := server.LatestWindow("termx-main", 80, 20)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	historyText := historyWindowText(window.Rows)
	for _, want := range []string{
		"before-exit",
		"terminal exited: termx-main code:0 exited",
		"exited at: " + exitedAt,
		"command: /bin/zsh",
	} {
		if !strings.Contains(historyText, want) {
			t.Fatalf("exit marker should enter history, missing %q in %#v", want, window.Rows)
		}
	}
}

func TestTerminalExitMarkerEntersHistoryFromAltScreen(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventTerminalExited}})
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "termx-main", Command: []string{"/bin/zsh"}, Size: Size{Cols: 80, Rows: 10}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "termx-main", "before-alt\n\x1b[?1049h\x1b[2Jalt-only"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	factory.process("termx-main").exit(0)
	assertEventValue(t, events, EventTerminalExited, "termx-main")

	window, err := server.LatestWindow("termx-main", 80, 20)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	historyText := historyWindowText(window.Rows)
	if !strings.Contains(historyText, "before-alt") || !strings.Contains(historyText, "terminal exited: termx-main code:0 exited") {
		t.Fatalf("exit marker should enter history even when process dies in alt-screen, got %#v", window.Rows)
	}
	if strings.Contains(historyText, "alt-only") {
		t.Fatalf("process exit should not synthesize unfinished alt-screen content into history, got %#v", window.Rows)
	}
}

func TestTerminalExitForceCommitsOpenLineAndRejectsMutation(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventTerminalExited}})
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process("term-1")
	if err := server.IngestOutput(context.Background(), "term-1", "open-tail"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	process.exit(7)
	event := assertEventValue(t, events, EventTerminalExited, "term-1")
	if event.Terminal == nil || event.Terminal.ExitCode == nil || *event.Terminal.ExitCode != 7 {
		t.Fatalf("unexpected exit event %#v", event)
	}
	if event.Terminal.ExitedAt.IsZero() {
		t.Fatalf("expected exit event to carry exited_at, got %#v", event.Terminal)
	}
	info, err := server.GetTerminal("term-1")
	if err != nil {
		t.Fatalf("get terminal: %v", err)
	}
	if info.State != TerminalStateExited || info.ExitCode == nil || *info.ExitCode != 7 {
		t.Fatalf("unexpected terminal info after exit %#v", info)
	}
	if !info.ExitedAt.Equal(event.Terminal.ExitedAt) {
		t.Fatalf("registry and event exited_at should match, info=%s event=%s", info.ExitedAt, event.Terminal.ExitedAt)
	}
	window, err := server.LatestWindow("term-1", 80, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	text := historyWindowText(window.Rows)
	if !strings.Contains(text, "open-tail") || !strings.Contains(text, "terminal exited: term-1 code:7 exited") {
		t.Fatalf("expected process exit to force commit open line, got %#v", window)
	}
	if err := server.WriteInput(context.Background(), "term-1", []byte("nope")); !errors.Is(err, ErrTerminalExited) {
		t.Fatalf("expected ErrTerminalExited for input, got %v", err)
	}
	if err := server.ResizeTerminal(context.Background(), "term-1", 80, 24); !errors.Is(err, ErrTerminalExited) {
		t.Fatalf("expected ErrTerminalExited for resize, got %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "late-output"); !errors.Is(err, ErrTerminalExited) {
		t.Fatalf("expected ErrTerminalExited for late output, got %v", err)
	}
	window, err = server.LatestWindow("term-1", 80, 10)
	if err != nil {
		t.Fatalf("latest window after late output: %v", err)
	}
	text = historyWindowText(window.Rows)
	if !strings.Contains(text, "open-tail") || !strings.Contains(text, "terminal exited: term-1 code:7 exited") || strings.Contains(text, "late-output") {
		t.Fatalf("late output after exit must not create history, got %#v", window)
	}
}

func TestTerminalRestartClearsExitMetadata(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventTerminalExited}})
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	first := factory.process("term-1")
	first.exit(23)
	assertEventValue(t, events, EventTerminalExited, "term-1")
	info, err := server.GetTerminal("term-1")
	if err != nil {
		t.Fatalf("get terminal after exit: %v", err)
	}
	if info.ExitCode == nil || *info.ExitCode != 23 || info.ExitedAt.IsZero() {
		t.Fatalf("expected exit metadata before restart, got %#v", info)
	}
	if err := server.RestartTerminal(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart terminal: %v", err)
	}
	info, err = server.GetTerminal("term-1")
	if err != nil {
		t.Fatalf("get terminal: %v", err)
	}
	if info.State != TerminalStateRunning || info.ExitCode != nil || !info.ExitedAt.IsZero() {
		t.Fatalf("restart should clear exit metadata, got %#v", info)
	}
}

func TestTerminalRestartEventIsLifecycleBoundary(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventTerminalChanged}})
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "ordinary\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	ordinary := assertEventValue(t, events, EventTerminalChanged, "term-1")
	if ordinary.LifecycleKnown {
		t.Fatalf("ordinary output changed event must not be lifecycle authority, got %#v", ordinary)
	}
	if err := server.RestartTerminal(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart terminal: %v", err)
	}
	restarted := assertEventValue(t, events, EventTerminalChanged, "term-1")
	if !restarted.LifecycleKnown || restarted.Terminal == nil || restarted.Terminal.State != TerminalStateRunning {
		t.Fatalf("restart should publish running lifecycle boundary, got %#v", restarted)
	}
}

func TestTerminalProcessDrainsOutputBeforeExit(t *testing.T) {
	factory := &exitBeforeOutputProcessFactory{}
	server := NewServer(WithProcessFactory(factory))
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventTerminalExited}})
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process
	process.exitThenOutput(0, "tail\n")
	event := assertEventValue(t, events, EventTerminalExited, "term-1")
	if event.Terminal == nil || event.Terminal.ExitCode == nil || *event.Terminal.ExitCode != 0 {
		t.Fatalf("unexpected exit event %#v", event)
	}
	window, err := server.LatestWindow("term-1", 80, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	text := historyWindowText(window.Rows)
	if !strings.Contains(text, "tail") || !strings.Contains(text, "terminal exited: term-1 code:0 exited") {
		t.Fatalf("expected output produced before channel close to be committed before exit, got %#v", window)
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
	defer process.mu.Unlock()
	if process.closed {
		return io.ErrClosedPipe
	}
	if process.resizeErr != nil {
		return process.resizeErr
	}
	process.resizes = append(process.resizes, size)
	return nil
}

func (process *recordingProcess) setResizeErr(err error) {
	process.mu.Lock()
	defer process.mu.Unlock()
	process.resizeErr = err
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

type exitBeforeOutputProcessFactory struct {
	process *exitBeforeOutputProcess
}

func (factory *exitBeforeOutputProcessFactory) Spawn(_ context.Context, spec ProcessSpec) (TerminalProcess, error) {
	process := &exitBeforeOutputProcess{
		id:       spec.TerminalID,
		outputCh: make(chan []byte, 1),
		waitCh:   make(chan ProcessExit, 1),
	}
	factory.process = process
	return process, nil
}

type exitBeforeOutputProcess struct {
	id       string
	outputCh chan []byte
	waitCh   chan ProcessExit
}

func (process *exitBeforeOutputProcess) Input([]byte) error {
	return nil
}

func (process *exitBeforeOutputProcess) Resize(Size) error {
	return nil
}

func (process *exitBeforeOutputProcess) Output() <-chan []byte {
	return process.outputCh
}

func (process *exitBeforeOutputProcess) Kill() error {
	process.exitThenOutput(-1, "")
	return nil
}

func (process *exitBeforeOutputProcess) Wait() <-chan ProcessExit {
	return process.waitCh
}

func (process *exitBeforeOutputProcess) Close() error {
	process.exitThenOutput(-1, "")
	return nil
}

func (process *exitBeforeOutputProcess) exitThenOutput(code int, output string) {
	process.waitCh <- ProcessExit{Code: code}
	close(process.waitCh)
	if output != "" {
		process.outputCh <- []byte(output)
	}
	close(process.outputCh)
}

func historyWindowText(rows []history.VisualRow) string {
	parts := make([]string, len(rows))
	for i, row := range rows {
		parts[i] = row.Text
	}
	return strings.Join(parts, "\n")
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
