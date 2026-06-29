package termxcorev2

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-core-v2/live"
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
