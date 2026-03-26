package tui

import (
	"io"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app"
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
