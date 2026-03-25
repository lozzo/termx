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

func swapProgramRunnerForTest(runner tuiruntime.ProgramRunner) func() {
	previous := programRunner
	programRunner = runner
	return func() {
		programRunner = previous
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
