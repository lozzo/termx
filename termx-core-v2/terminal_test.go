package termxcorev2

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

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
	if len(rows) != 2 || rows[0] != "hello" || rows[1] != "world" {
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
	if err := server.ResizeTerminal(context.Background(), "term-1", 10, 3); err != nil {
		t.Fatalf("grow resize: %v", err)
	}
	grown, err := server.LatestWindow("term-1", 10, 10)
	if err != nil {
		t.Fatalf("latest after grow: %v", err)
	}
	if grown.TotalLines != 1 || len(grown.Rows) != 2 || grown.Rows[0].Text != "one" || grown.Rows[1].Text != "two" {
		t.Fatalf("expected grow resize to reclaim committed suffix into visible frontier, got %#v", grown)
	}
	if err := server.ResizeTerminal(context.Background(), "term-1", 10, 2); err != nil {
		t.Fatalf("shrink resize: %v", err)
	}
	shrunk, err := server.LatestWindow("term-1", 10, 10)
	if err != nil {
		t.Fatalf("latest after shrink: %v", err)
	}
	if shrunk.TotalLines != 1 || len(shrunk.Rows) != 1 || shrunk.Rows[0].Text != "one" {
		t.Fatalf("expected shrink resize to hide reclaimed frontier tail, got %#v", shrunk)
	}
}

func TestTerminalRestartReplacesProcessAndClearsLiveAndHistory(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	if _, err := server.RegisterTerminal(TerminalRecord{ID: "term-1", Command: []string{"shell"}}); err != nil {
		t.Fatalf("register terminal: %v", err)
	}
	first := factory.process("term-1")
	if err := server.IngestOutput(context.Background(), "term-1", "before\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
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
	if len(rows) != 1 || rows[0] != "" {
		t.Fatalf("expected live rows reset, got %#v", rows)
	}
	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 0 {
		t.Fatalf("expected history reset after restart, got %#v", window)
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
	info, err := server.GetTerminal("term-1")
	if err != nil {
		t.Fatalf("get terminal: %v", err)
	}
	if info.State != TerminalStateExited || info.ExitCode == nil || *info.ExitCode != 7 {
		t.Fatalf("unexpected terminal info after exit %#v", info)
	}
	window, err := server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "open-tail" || window.TotalLines != 1 {
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
	window, err = server.LatestWindow("term-1", 20, 10)
	if err != nil {
		t.Fatalf("latest window after late output: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "open-tail" || window.TotalLines != 1 {
		t.Fatalf("late output after exit must not create history, got %#v", window)
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
}

func newRecordingProcessFactory() *recordingProcessFactory {
	return &recordingProcessFactory{processes: make(map[string][]*recordingProcess)}
}

func (factory *recordingProcessFactory) Spawn(_ context.Context, spec ProcessSpec) (TerminalProcess, error) {
	process := &recordingProcess{
		id:     spec.TerminalID,
		waitCh: make(chan ProcessExit, 1),
	}
	factory.mu.Lock()
	factory.processes[spec.TerminalID] = append(factory.processes[spec.TerminalID], process)
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

type recordingProcess struct {
	mu        sync.Mutex
	id        string
	inputs    [][]byte
	resizes   []Size
	resizeErr error
	waitCh    chan ProcessExit
	exitOnce  sync.Once
	killed    bool
	closed    bool
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
		process.waitCh <- ProcessExit{Code: code}
		close(process.waitCh)
	})
}
