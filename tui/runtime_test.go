package tui

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app"
	tuiruntime "github.com/lozzow/termx/tui/runtime"
	stateterminal "github.com/lozzow/termx/tui/state/terminal"
	"github.com/lozzow/termx/tui/state/types"
	"github.com/lozzow/termx/tui/state/workspace"
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

func TestRunStartsProgramWithWorkbenchScreen(t *testing.T) {
	runner := &captureProgramRunner{}
	restore := swapProgramRunnerForTest(runner)
	t.Cleanup(restore)

	if err := Run(nil, Config{}, nil, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}
	if runner.runCalls != 1 {
		t.Fatalf("expected one run call, got %d", runner.runCalls)
	}

	root, ok := runner.model.(app.Model)
	if !ok {
		t.Fatalf("expected app.Model, got %T", runner.model)
	}
	if root.Screen != app.ScreenWorkbench {
		t.Fatalf("expected workbench screen, got %q", root.Screen)
	}
	if root.FocusTarget != app.FocusWorkbench {
		t.Fatalf("expected workbench focus, got %q", root.FocusTarget)
	}
	if root.Overlay.HasActive() {
		t.Fatalf("expected empty overlay stack, got %#v", root.Overlay)
	}
}

func TestRunReturnsProgramError(t *testing.T) {
	runner := &captureProgramRunner{err: errors.New("program failed")}
	restore := swapProgramRunnerForTest(runner)
	t.Cleanup(restore)

	err := Run(nil, Config{}, nil, io.Discard)
	if err == nil || err.Error() != "program failed" {
		t.Fatalf("expected propagated runner error, got %v", err)
	}
}

func TestRunRestoresWorkspaceState(t *testing.T) {
	runner := &captureProgramRunner{}
	restore := swapProgramRunnerForTest(runner)
	t.Cleanup(restore)

	statePath := filepath.Join(t.TempDir(), "workspace-state.json")
	store := tuiruntime.NewWorkspaceStore(statePath)
	model := app.NewModel()
	model.Screen = app.ScreenTerminalPool
	model.FocusTarget = app.FocusTerminalPool
	model.Pool.Query = "restored-query"
	model.Workspace = restoredWorkspaceForRunTest()
	model.Terminals[types.TerminalID("term-restore")] = stateterminal.Metadata{
		ID:      types.TerminalID("term-restore"),
		Name:    "restored-shell",
		Command: []string{"/bin/sh"},
		State:   stateterminal.StateRunning,
	}
	if err := store.Save(context.Background(), model); err != nil {
		t.Fatalf("save workspace state: %v", err)
	}

	if err := Run(nil, Config{WorkspaceStatePath: statePath, Workspace: "fallback"}, nil, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}

	root := capturedAppModelForRunTest(t, runner.model)
	if root.Workspace == nil || root.Workspace.ID != "restored-workspace" {
		t.Fatalf("expected restored workspace, got %#v", root.Workspace)
	}
	if root.Screen != app.ScreenTerminalPool || root.Pool.Query != "restored-query" {
		t.Fatalf("expected restored pool state, got %#v", root)
	}
}

func TestRunFallsBackToTemporaryWorkspaceWhenRestoreFails(t *testing.T) {
	runner := &captureProgramRunner{}
	restore := swapProgramRunnerForTest(runner)
	t.Cleanup(restore)

	statePath := filepath.Join(t.TempDir(), "workspace-state.json")
	if err := osWriteFile(statePath, []byte("{invalid-json")); err != nil {
		t.Fatalf("write invalid workspace state: %v", err)
	}

	if err := Run(nil, Config{WorkspaceStatePath: statePath, Workspace: "fallback"}, nil, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}

	root := capturedAppModelForRunTest(t, runner.model)
	if root.Workspace == nil || root.Workspace.ID != "fallback" {
		t.Fatalf("expected fallback temporary workspace, got %#v", root.Workspace)
	}
}

func TestRunAttachModeSkipsWorkspacePersistenceWrapper(t *testing.T) {
	runner := &captureProgramRunner{}
	restore := swapProgramRunnerForTest(runner)
	t.Cleanup(restore)

	if err := Run(nil, Config{
		AttachID:           "term-001",
		WorkspaceStatePath: filepath.Join(t.TempDir(), "workspace-state.json"),
		Workspace:          "main",
	}, nil, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}

	if _, ok := runner.model.(interface{ UnderlyingModel() tea.Model }); ok {
		t.Fatalf("expected attach mode to skip persistence wrapper, got %T", runner.model)
	}
}

func swapProgramRunnerForTest(runner tuiruntime.ProgramRunner) func() {
	previous := programRunner
	programRunner = runner
	return func() {
		programRunner = previous
	}
}

func restoredWorkspaceForRunTest() *workspace.WorkspaceState {
	ws := workspace.NewTemporary("restored-workspace")
	tab := ws.ActiveTab()
	pane, _ := tab.ActivePane()
	pane.TerminalID = types.TerminalID("term-restore")
	pane.SlotState = types.PaneSlotLive
	tab.TrackPane(pane)
	return ws
}

func capturedAppModelForRunTest(t *testing.T, model tea.Model) app.Model {
	t.Helper()
	switch typed := model.(type) {
	case app.Model:
		return typed
	case interface{ UnderlyingModel() tea.Model }:
		return capturedAppModelForRunTest(t, typed.UnderlyingModel())
	case interface{ AppModel() app.Model }:
		return typed.AppModel()
	default:
		t.Fatalf("expected app model, got %T", model)
		return app.Model{}
	}
}

func TestWaitForSocketRejectsNilProbe(t *testing.T) {
	err := WaitForSocket("", time.Millisecond, nil)
	if err == nil || err.Error() != "probe is nil" {
		t.Fatalf("expected nil probe error, got %v", err)
	}
}

func TestWaitForSocketReturnsWhenSocketAndProbeReady(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.sock")
	if err := osWriteFile(path, []byte("ready")); err != nil {
		t.Fatalf("write socket marker: %v", err)
	}
	probeCalled := false
	err := WaitForSocket(path, time.Second, func() error {
		probeCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("expected socket to become ready, got %v", err)
	}
	if !probeCalled {
		t.Fatal("expected probe to be called")
	}
}

func TestWaitForSocketTimesOutWhenProbeNeverSucceeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.sock")
	if err := osWriteFile(path, []byte("ready")); err != nil {
		t.Fatalf("write socket marker: %v", err)
	}
	err := WaitForSocket(path, 120*time.Millisecond, func() error {
		return errors.New("not ready")
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
