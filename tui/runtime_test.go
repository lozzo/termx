package tui

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
	tuiruntime "github.com/lozzow/termx/tui/runtime"
)

type captureProgramRunner struct {
	runCalls int
	model    tea.Model
	input    io.Reader
	output   io.Writer
	err      error
}

func (r *captureProgramRunner) Run(model tea.Model, input io.Reader, output io.Writer) error {
	r.runCalls++
	r.model = model
	r.input = input
	r.output = output
	return r.err
}

func TestRunStartsWithWorkbenchScreen(t *testing.T) {
	runner := &captureProgramRunner{}
	restore := swapProgramRunnerForTest(runner)
	t.Cleanup(restore)

	if err := Run(nil, Config{Workspace: "main"}, nil, io.Discard); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if runner.runCalls != 1 {
		t.Fatalf("expected one run call, got %d", runner.runCalls)
	}

	root := capturedAppModelForRunTest(t, runner.model)
	if root.Screen != app.ScreenWorkbench {
		t.Fatalf("expected workbench screen, got %q", root.Screen)
	}
}

func TestRunBootstrapsSessionSnapshotForRootTUI(t *testing.T) {
	runner := &captureProgramRunner{}
	restore := swapProgramRunnerForTest(runner)
	t.Cleanup(restore)

	client := &stubClientForRunTest{
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{ID: "created", Name: "shell", State: "running"}}},
		snapshotByID: map[string]*protocol.Snapshot{
			"created": {
				TerminalID: "created",
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "bootstrapped body"}}}},
			},
		},
	}
	if err := Run(client, Config{Workspace: "main", DefaultShell: "/bin/sh"}, nil, io.Discard); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	root := capturedAppModelForRunTest(t, runner.model)
	session := root.Workbench.Sessions[types.TerminalID("created")]
	if session.Snapshot == nil || session.Snapshot.TerminalID != "created" {
		t.Fatalf("expected bootstrap session snapshot, got %#v", root.Workbench.Sessions)
	}
	if got := root.Workbench.ActivePane().TerminalID; got != types.TerminalID("created") {
		t.Fatalf("expected active pane attached to created terminal, got %q", got)
	}
}

func TestRunRestoresWorkspaceStateAndRebindsSessions(t *testing.T) {
	runner := &captureProgramRunner{}
	restore := swapProgramRunnerForTest(runner)
	t.Cleanup(restore)

	statePath := filepath.Join(t.TempDir(), "workspace.json")
	store := tuiruntime.NewWorkspaceStore(statePath)
	model := app.NewModel("main")
	model.Screen = app.ScreenTerminalPool
	model.Workbench.BindActivePane(coreterminal.Metadata{
		ID:    types.TerminalID("term-restore"),
		Name:  "restored-shell",
		State: coreterminal.StateRunning,
	})
	if err := store.Save(context.Background(), model); err != nil {
		t.Fatalf("save workspace state: %v", err)
	}

	client := &stubClientForRunTest{
		snapshotByID: map[string]*protocol.Snapshot{
			"term-restore": {
				TerminalID: "term-restore",
				Screen: protocol.ScreenData{
					Cells: [][]protocol.Cell{{{Content: "restored body"}}},
				},
			},
		},
	}
	if err := Run(client, Config{
		Workspace:          "main",
		WorkspaceStatePath: statePath,
	}, nil, io.Discard); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	root := capturedAppModelForRunTest(t, runner.model)
	if root.Screen != app.ScreenTerminalPool {
		t.Fatalf("expected restored terminal pool screen, got %q", root.Screen)
	}
	session, ok := root.Workbench.Sessions[types.TerminalID("term-restore")]
	if !ok || session.Snapshot == nil || session.Snapshot.TerminalID != "term-restore" {
		t.Fatalf("expected rebound session snapshot, got %#v", root.Workbench.Sessions)
	}
}

func swapProgramRunnerForTest(runner tuiruntime.ProgramRunner) func() {
	previous := programRunner
	programRunner = runner
	return func() {
		programRunner = previous
	}
}

func capturedAppModelForRunTest(t *testing.T, model tea.Model) app.Model {
	t.Helper()
	root, ok := model.(app.Model)
	if !ok {
		t.Fatalf("expected app.Model, got %T", model)
	}
	return root
}

type stubClientForRunTest struct {
	listResult   *protocol.ListResult
	snapshotByID map[string]*protocol.Snapshot
}

func (c *stubClientForRunTest) Close() error { return nil }
func (c *stubClientForRunTest) Create(ctx context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	return &protocol.CreateResult{TerminalID: "created", State: "running"}, nil
}
func (c *stubClientForRunTest) SetTags(ctx context.Context, terminalID string, tags map[string]string) error {
	return nil
}
func (c *stubClientForRunTest) SetMetadata(ctx context.Context, terminalID string, name string, tags map[string]string) error {
	return nil
}
func (c *stubClientForRunTest) List(ctx context.Context) (*protocol.ListResult, error) {
	if c.listResult != nil {
		return c.listResult, nil
	}
	return &protocol.ListResult{}, nil
}
func (c *stubClientForRunTest) Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error) {
	return make(chan protocol.Event), nil
}
func (c *stubClientForRunTest) Attach(ctx context.Context, terminalID string, mode string) (*protocol.AttachResult, error) {
	return &protocol.AttachResult{Mode: mode, Channel: 7}, nil
}
func (c *stubClientForRunTest) Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	if snap, ok := c.snapshotByID[terminalID]; ok {
		return snap, nil
	}
	return &protocol.Snapshot{TerminalID: terminalID}, nil
}
func (c *stubClientForRunTest) Input(ctx context.Context, channel uint16, data []byte) error {
	return nil
}
func (c *stubClientForRunTest) Resize(ctx context.Context, channel uint16, cols, rows uint16) error {
	return nil
}
func (c *stubClientForRunTest) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	return make(chan protocol.StreamFrame), func() {}
}
func (c *stubClientForRunTest) Kill(ctx context.Context, terminalID string) error   { return nil }
func (c *stubClientForRunTest) Remove(ctx context.Context, terminalID string) error { return nil }

func TestWaitForSocketRejectsNilProbe(t *testing.T) {
	err := WaitForSocket("", time.Millisecond, nil)
	if err == nil || err.Error() != "probe is nil" {
		t.Fatalf("expected nil probe error, got %v", err)
	}
}
