package termxcorev2

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
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
