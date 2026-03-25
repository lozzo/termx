package tui

import (
	"errors"
	"io"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/domain/types"
)

func TestRunOrchestratesStartupPlanBootstrapAndSessionLifecycle(t *testing.T) {
	bootstrapperStopCalls = 0
	planner := &stubRunPlanner{
		plan: StartupPlan{
			State: buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty),
		},
	}
	executor := &stubRunTaskExecutor{
		plan: StartupPlan{
			State: connectedRunAppState(),
		},
	}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Stop: func() {
						bootstrapperStopCalls++
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{}

	err := runWithDependencies(&stubStartupClient{}, Config{DefaultShell: "/bin/zsh"}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         staticProgramRenderer{view: "runtime"},
		TerminalSize: func(io.Reader, io.Writer) protocol.Size {
			return protocol.Size{Cols: 120, Rows: 40}
		},
	})
	if err != nil {
		t.Fatalf("expected runtime orchestration to succeed, got %v", err)
	}
	if planner.calls != 1 {
		t.Fatalf("expected planner to run once, got %d", planner.calls)
	}
	if executor.calls != 1 || executor.size.Cols != 120 || executor.size.Rows != 40 {
		t.Fatalf("expected executor to receive calculated size, got calls=%d size=%+v", executor.calls, executor.size)
	}
	if bootstrapper.calls != 1 {
		t.Fatalf("expected session bootstrapper to run once, got %d", bootstrapper.calls)
	}
	if runner.calls != 1 || runner.view != "runtime" {
		t.Fatalf("expected program runner to render static runtime view, got calls=%d view=%q", runner.calls, runner.view)
	}
	if bootstrapperStopCalls != 1 {
		t.Fatalf("expected bootstrap session stop on program exit, got %d", bootstrapperStopCalls)
	}
}

func TestRunReturnsPlannerError(t *testing.T) {
	err := runWithDependencies(&stubStartupClient{}, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          &stubRunPlanner{err: errRuntimeRunBoom},
		TaskExecutor:     &stubRunTaskExecutor{},
		SessionBootstrap: &stubRunSessionBootstrapper{},
		ProgramRunner:    &stubProgramRunner{},
		Renderer:         staticProgramRenderer{view: "runtime"},
	})
	if !errors.Is(err, errRuntimeRunBoom) {
		t.Fatalf("expected planner error, got %v", err)
	}
}

func TestRunReturnsTaskExecutorError(t *testing.T) {
	err := runWithDependencies(&stubStartupClient{}, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          &stubRunPlanner{plan: StartupPlan{State: buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty)}},
		TaskExecutor:     &stubRunTaskExecutor{err: errRuntimeRunBoom},
		SessionBootstrap: &stubRunSessionBootstrapper{},
		ProgramRunner:    &stubProgramRunner{},
		Renderer:         staticProgramRenderer{view: "runtime"},
	})
	if !errors.Is(err, errRuntimeRunBoom) {
		t.Fatalf("expected task executor error, got %v", err)
	}
}

func TestRunReturnsSessionBootstrapError(t *testing.T) {
	err := runWithDependencies(&stubStartupClient{}, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          &stubRunPlanner{plan: StartupPlan{State: connectedRunAppState()}},
		TaskExecutor:     &stubRunTaskExecutor{plan: StartupPlan{State: connectedRunAppState()}},
		SessionBootstrap: &stubRunSessionBootstrapper{err: errRuntimeRunBoom},
		ProgramRunner:    &stubProgramRunner{},
		Renderer:         staticProgramRenderer{view: "runtime"},
	})
	if !errors.Is(err, errRuntimeRunBoom) {
		t.Fatalf("expected session bootstrap error, got %v", err)
	}
}

func TestRunReturnsProgramRunnerError(t *testing.T) {
	err := runWithDependencies(&stubStartupClient{}, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          &stubRunPlanner{plan: StartupPlan{State: connectedRunAppState()}},
		TaskExecutor:     &stubRunTaskExecutor{plan: StartupPlan{State: connectedRunAppState()}},
		SessionBootstrap: &stubRunSessionBootstrapper{},
		ProgramRunner:    &stubProgramRunner{err: errRuntimeRunBoom},
		Renderer:         staticProgramRenderer{view: "runtime"},
	})
	if !errors.Is(err, errRuntimeRunBoom) {
		t.Fatalf("expected program runner error, got %v", err)
	}
}
