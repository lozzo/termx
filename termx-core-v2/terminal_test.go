package termxcorev2

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core-v2/history"
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

func TestR304OrdinaryOutputCommitsFromSemanticScrollOut(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-history",
		Command: []string{"shell"},
		Size:    Size{Cols: 12, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-history", "one\r\ntwo\r\nthree\r\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	window := latestTerminalHistory(t, server, "term-history")
	if got := terminalHistoryCommittedRows(window); len(got) == 0 || got[0] != "one" {
		t.Fatalf("ordinary output should commit scrolled logical lines from semantic tx, got %#v window=%#v", got, window)
	}
}

func TestR304ControlOverwriteMutatesFrontierWithoutCommit(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-progress",
		Command: []string{"shell"},
		Size:    Size{Cols: 30, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-progress", "progress 10%\r\x1b[Kprogress 20%"); err != nil {
		t.Fatalf("ingest progress: %v", err)
	}
	window := latestTerminalHistory(t, server, "term-progress")
	if window.LogicalTotal != 0 {
		t.Fatalf("CR/EL overwrite should not commit intermediate progress, total=%d rows=%#v", window.LogicalTotal, window.Rows)
	}
	if got := terminalHistoryAllRows(window); len(got) != 1 || !strings.Contains(got[0], "progress 20%") {
		t.Fatalf("frontier should expose latest progress state only, got %#v", got)
	}
}

func TestR304ProcessExitForceCommitsPrimaryFrontier(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-exit-history",
		Command: []string{"shell"},
		Size:    Size{Cols: 40, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process("term-exit-history")
	process.emitOutput("unterminated tail")
	process.exit(0)
	waitForTerminalState(t, server, "term-exit-history", TerminalStateExited)

	window := latestTerminalHistory(t, server, "term-exit-history")
	if got := terminalHistoryCommittedRows(window); countStringForTerminalTest(got, "unterminated tail") != 1 {
		t.Fatalf("process exit should force commit primary mutable frontier once, got %#v", got)
	}
}

func TestR305PrimaryScreenRepaintPublishesCurrentOnly(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-primary-frame",
		Command: []string{"shell"},
		Size:    Size{Cols: 24, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-primary-frame", "\x1b[?2026hfirst frame\x1b[?2026l"); err != nil {
		t.Fatalf("ingest first synchronized frame: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-primary-frame", "\x1b[?2026h\r\x1b[Ksecond frame\x1b[?2026l"); err != nil {
		t.Fatalf("ingest repaint synchronized frame: %v", err)
	}

	window := latestTerminalHistory(t, server, "term-primary-frame")
	if window.LogicalTotal != 0 {
		t.Fatalf("primary repaint current frame must not grow ordinary committed depth, total=%d rows=%#v", window.LogicalTotal, window.Rows)
	}
	current := terminalHistoryRowsBySegment(window, history.HistorySegmentCurrentPrimaryFrame)
	if len(current) == 0 {
		t.Fatalf("latest history should expose current primary frame, window=%#v", window)
	}
	if got := terminalHistoryPlainText(current[0].Cells); !strings.Contains(got, "second frame") {
		t.Fatalf("repaint should replace current primary frame with latest content, got %q rows=%#v", got, current)
	}
	for _, row := range current {
		if row.Committed || !row.FixedGrid || row.ScreenCols != 24 {
			t.Fatalf("current primary frame should be uncommitted fixed-grid at source width: %#v", row)
		}
	}
}

func TestR305FrozenSnapshotCapturesPrimaryCurrentFrameBoundary(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-primary-freeze",
		Command: []string{"shell"},
		Size:    Size{Cols: 30, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-primary-freeze", "\x1b[?2026hcurrent frame\x1b[?2026l"); err != nil {
		t.Fatalf("ingest synchronized frame: %v", err)
	}
	snapshot, err := server.TerminalHistoryFreeze(context.Background(), "term-primary-freeze", history.FreezeHistoryRequest{
		TerminalID: "term-primary-freeze",
		Cols:       30,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("freeze history: %v", err)
	}
	if snapshot.Boundary.Cursor.Segment != history.HistorySegmentCurrentPrimaryFrame || !snapshot.Boundary.Cursor.Valid {
		t.Fatalf("freeze should capture current primary frame segment boundary, got %#v", snapshot)
	}
}

func TestR306PureAltTransientIsSelectableAndNeverOrdinaryCommitted(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-alt-transient",
		Command: []string{"shell"},
		Size:    Size{Cols: 20, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-alt-transient", "\x1b[?1049halt frame"); err != nil {
		t.Fatalf("ingest alt enter: %v", err)
	}
	window := latestTerminalHistory(t, server, "term-alt-transient")
	altRows := terminalHistoryRowsBySegment(window, history.HistorySegmentCurrentAltFrame)
	if len(altRows) == 0 {
		t.Fatalf("running alt frame should be visible for selection, window=%#v", window)
	}
	if window.LogicalTotal != 0 || altRows[0].Committed {
		t.Fatalf("alt transient must not ordinary commit, total=%d rows=%#v", window.LogicalTotal, altRows)
	}
	if err := server.IngestOutput(context.Background(), "term-alt-transient", "\x1b[?1049l"); err != nil {
		t.Fatalf("ingest alt exit: %v", err)
	}
	window = latestTerminalHistory(t, server, "term-alt-transient")
	if rows := terminalHistoryRowsBySegment(window, history.HistorySegmentCurrentAltFrame); len(rows) != 0 {
		t.Fatalf("alt exit should release transient current frame, got %#v", rows)
	}
	if window.LogicalTotal != 0 {
		t.Fatalf("pure alt exit must not commit history, total=%d rows=%#v", window.LogicalTotal, window.Rows)
	}
}

func TestR306PrimaryCurrentArchivesBeforeAltAndPostAltPrimaryIsNewFrame(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-primary-alt",
		Command: []string{"shell"},
		Size:    Size{Cols: 28, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-primary-alt", "\x1b[?2026hpre alt primary\x1b[?2026l"); err != nil {
		t.Fatalf("ingest primary frame: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-primary-alt", "\x1b[?1049halt choice"); err != nil {
		t.Fatalf("ingest alt enter: %v", err)
	}
	duringAlt := latestTerminalHistory(t, server, "term-primary-alt")
	archived := terminalHistoryRowsBySegment(duringAlt, history.HistorySegmentArchivedPrimaryFrame)
	if len(archived) == 0 || !strings.Contains(terminalHistoryPlainText(archived[0].Cells), "pre alt primary") {
		t.Fatalf("alt enter should archive pre-alt primary frame, got %#v", archived)
	}
	if current := terminalHistoryRowsBySegment(duringAlt, history.HistorySegmentCurrentPrimaryFrame); len(current) != 0 {
		t.Fatalf("alt enter should hide pre-alt current primary frame, got %#v", current)
	}
	if alt := terminalHistoryRowsBySegment(duringAlt, history.HistorySegmentCurrentAltFrame); len(alt) == 0 {
		t.Fatalf("alt enter should publish transient alt frame, window=%#v", duringAlt)
	}

	if err := server.IngestOutput(context.Background(), "term-primary-alt", "\x1b[?1049l"); err != nil {
		t.Fatalf("ingest alt exit: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-primary-alt", "\x1b[?2026hpost alt primary\x1b[?2026l"); err != nil {
		t.Fatalf("ingest post alt primary: %v", err)
	}
	afterAlt := latestTerminalHistory(t, server, "term-primary-alt")
	current := terminalHistoryRowsBySegment(afterAlt, history.HistorySegmentCurrentPrimaryFrame)
	if len(current) == 0 || !strings.Contains(strings.Join(terminalHistoryRowsText(current), ""), "post alt primary") {
		t.Fatalf("post-alt primary output should publish a new current frame, got %#v", current)
	}
	if current[0].FrameID == archived[0].FrameID {
		t.Fatalf("post-alt current frame must be a new frame, archived=%#v current=%#v", archived[0], current[0])
	}
}

func TestR307ResizeDoesNotRewriteCommittedHistory(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-resize-history",
		Command: []string{"shell"},
		Size:    Size{Cols: 12, Rows: 2},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-resize-history", "alpha\r\nbeta\r\ngamma\r\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	before := terminalHistoryCommittedRows(latestTerminalHistory(t, server, "term-resize-history"))
	if len(before) == 0 {
		t.Fatalf("expected committed ordinary rows before resize")
	}
	if err := server.ResizeTerminal(context.Background(), "term-resize-history", 40, 6); err != nil {
		t.Fatalf("resize terminal: %v", err)
	}
	after := terminalHistoryCommittedRows(latestTerminalHistory(t, server, "term-resize-history"))
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("resize must not rewrite committed logical history, before=%#v after=%#v", before, after)
	}
}

func TestR307ProcessExitCommitsFinalScreenFrameOnceAtOriginalWidth(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-final-frame",
		Command: []string{"shell"},
		Size:    Size{Cols: 26, Rows: 3},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	process := factory.process("term-final-frame")
	process.emitOutput("\x1b[?2026hfinal frame\x1b[?2026l")
	process.exit(0)
	waitForTerminalState(t, server, "term-final-frame", TerminalStateExited)
	process.exit(0)

	window := latestTerminalHistory(t, server, "term-final-frame")
	finalRows := terminalHistoryCommittedRowsByKind(window, history.LineKindScreenFrame)
	if len(finalRows) == 0 {
		t.Fatalf("process exit should commit final primary screen-frame, window=%#v", window)
	}
	if countRowsContaining(finalRows, "final frame") != 1 {
		t.Fatalf("final frame should be committed once, rows=%#v", finalRows)
	}
	for _, row := range window.Rows {
		if row.Kind == history.LineKindScreenFrame && row.Committed {
			if !row.FixedGrid || row.ScreenCols != 26 {
				t.Fatalf("final screen-frame must stay fixed-grid at original width: %#v", row)
			}
		}
	}
}

func TestR308HistoryPreservesTerminalColorTokens(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-style-history",
		Command: []string{"shell"},
		Size:    Size{Cols: 80, Rows: 4},
	}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-style-history",
		"default \x1b[31mred\x1b[0m \x1b[38;5;123midx\x1b[0m \x1b[38;2;1;2;3mtrue\x1b[0m\r\n",
	); err != nil {
		t.Fatalf("ingest styled output: %v", err)
	}
	window := latestTerminalHistory(t, server, "term-style-history")
	if style, ok := terminalHistoryCellStyleForWord(window, "default"); !ok || style.FG != "" || style.BG != "" {
		t.Fatalf("default style should stay semantic empty fg/bg, style=%#v ok=%v", style, ok)
	}
	if style, ok := terminalHistoryCellStyleForWord(window, "red"); !ok || style.FG != "ansi:1" {
		t.Fatalf("16-color SGR should be stored as ansi token, style=%#v ok=%v", style, ok)
	}
	if style, ok := terminalHistoryCellStyleForWord(window, "idx"); !ok || style.FG != "idx:123" {
		t.Fatalf("256-color SGR should be stored as idx token, style=%#v ok=%v", style, ok)
	}
	if style, ok := terminalHistoryCellStyleForWord(window, "true"); !ok || style.FG != "#010203" {
		t.Fatalf("truecolor SGR should be stored as RGB content token, style=%#v ok=%v", style, ok)
	}
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

func latestTerminalHistory(t *testing.T, server *Server, terminalID string) history.HistoryWindow {
	t.Helper()
	window, err := server.TerminalHistoryWindow(context.Background(), terminalID, history.HistoryWindowRequest{
		TerminalID: terminalID,
		Mode:       history.HistoryWindowModeLatest,
		Cols:       80,
		Limit:      100,
	})
	if err != nil {
		t.Fatalf("terminal history window: %v", err)
	}
	return window
}

func terminalHistoryCommittedRows(window history.HistoryWindow) []string {
	var rows []string
	for _, row := range window.Rows {
		if row.Committed {
			rows = append(rows, terminalHistoryPlainText(row.Cells))
		}
	}
	return rows
}

func terminalHistoryCommittedRowsByKind(window history.HistoryWindow, kind history.LineKind) []string {
	var rows []string
	for _, row := range window.Rows {
		if row.Committed && row.Kind == kind {
			rows = append(rows, terminalHistoryPlainText(row.Cells))
		}
	}
	return rows
}

func terminalHistoryAllRows(window history.HistoryWindow) []string {
	rows := make([]string, 0, len(window.Rows))
	for _, row := range window.Rows {
		rows = append(rows, terminalHistoryPlainText(row.Cells))
	}
	return rows
}

func terminalHistoryRowsBySegment(window history.HistoryWindow, segment history.HistorySegment) []history.HistoryRow {
	var rows []history.HistoryRow
	for _, row := range window.Rows {
		if row.Segment == segment {
			rows = append(rows, row)
		}
	}
	return rows
}

func terminalHistoryRowsText(rows []history.HistoryRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, terminalHistoryPlainText(row.Cells))
	}
	return out
}

func terminalHistoryPlainText(cells []history.Cell) string {
	var builder strings.Builder
	for _, cell := range cells {
		builder.WriteString(cell.Text)
	}
	return builder.String()
}

func terminalHistoryCellStyleForWord(window history.HistoryWindow, word string) (history.CellStyle, bool) {
	for _, row := range window.Rows {
		for i := range row.Cells {
			var got strings.Builder
			for j := i; j < len(row.Cells) && j < i+len(word); j++ {
				got.WriteString(row.Cells[j].Text)
			}
			if got.String() == word {
				return row.Cells[i].Style, true
			}
		}
	}
	return history.CellStyle{}, false
}

func countStringForTerminalTest(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}

func countRowsContaining(values []string, want string) int {
	count := 0
	for _, value := range values {
		if strings.Contains(value, want) {
			count++
		}
	}
	return count
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
