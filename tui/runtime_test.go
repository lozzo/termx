package tui

import (
	"context"
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
	deps := runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		TerminalSize: func(io.Reader, io.Writer) protocol.Size {
			return protocol.Size{Cols: 120, Rows: 40}
		},
	}

	err := runWithDependencies(&stubStartupClient{}, Config{DefaultShell: "/bin/zsh"}, nil, io.Discard, deps)
	if !errors.Is(err, errRuntimeLoopNotImplemented) {
		t.Fatalf("expected runtime placeholder error after bootstrap, got %v", err)
	}
	if planner.calls != 1 {
		t.Fatalf("expected planner to run once, got %d", planner.calls)
	}
	if executor.calls != 1 || executor.size.Cols != 120 {
		t.Fatalf("expected executor to receive calculated size, got calls=%d size=%+v", executor.calls, executor.size)
	}
	if bootstrapper.calls != 1 {
		t.Fatalf("expected session bootstrapper to run once, got %d", bootstrapper.calls)
	}
	if bootstrapperStopCalls != 1 {
		t.Fatalf("expected bootstrap session stop on placeholder exit, got %d", bootstrapperStopCalls)
	}
}

func TestRunReturnsPlannerErrorBeforeBootstrap(t *testing.T) {
	planner := &stubRunPlanner{err: errRuntimeRunBoom}

	err := runWithDependencies(&stubStartupClient{}, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     &stubRunTaskExecutor{},
		SessionBootstrap: &stubRunSessionBootstrapper{},
	})
	if !errors.Is(err, errRuntimeRunBoom) {
		t.Fatalf("expected planner error, got %v", err)
	}
}

func TestRunReturnsTaskExecutorError(t *testing.T) {
	planner := &stubRunPlanner{plan: StartupPlan{State: buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty)}}
	executor := &stubRunTaskExecutor{err: errRuntimeRunBoom}

	err := runWithDependencies(&stubStartupClient{}, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: &stubRunSessionBootstrapper{},
	})
	if !errors.Is(err, errRuntimeRunBoom) {
		t.Fatalf("expected task executor error, got %v", err)
	}
}

func TestRunReturnsSessionBootstrapError(t *testing.T) {
	planner := &stubRunPlanner{plan: StartupPlan{State: connectedRunAppState()}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: connectedRunAppState()}}
	bootstrapper := &stubRunSessionBootstrapper{err: errRuntimeRunBoom}

	err := runWithDependencies(&stubStartupClient{}, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
	})
	if !errors.Is(err, errRuntimeRunBoom) {
		t.Fatalf("expected session bootstrap error, got %v", err)
	}
}

var (
	errRuntimeRunBoom     = errors.New("run boom")
	bootstrapperStopCalls int
)

type stubRunPlanner struct {
	plan  StartupPlan
	err   error
	calls int
}

func (p *stubRunPlanner) Plan(context.Context, Config) (StartupPlan, error) {
	p.calls++
	if p.err != nil {
		return StartupPlan{}, p.err
	}
	return p.plan, nil
}

type stubRunTaskExecutor struct {
	plan  StartupPlan
	err   error
	calls int
	size  protocol.Size
}

func (e *stubRunTaskExecutor) Execute(_ context.Context, _ Client, size protocol.Size, plan StartupPlan) (StartupPlan, error) {
	e.calls++
	e.size = size
	if e.err != nil {
		return StartupPlan{}, e.err
	}
	if e.plan.State.Domain.Workspaces == nil {
		return plan, nil
	}
	return e.plan, nil
}

type stubRunSessionBootstrapper struct {
	sessions RuntimeSessions
	err      error
	calls    int
}

func (b *stubRunSessionBootstrapper) Bootstrap(context.Context, Client, types.AppState) (RuntimeSessions, error) {
	b.calls++
	if b.err != nil {
		return RuntimeSessions{}, b.err
	}
	return b.sessions, nil
}

func connectedRunAppState() types.AppState {
	state := buildSinglePaneAppState("main", "shell", types.PaneSlotConnected)
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	pane := tab.Panes[types.PaneID("pane-1")]
	pane.TerminalID = types.TerminalID("term-1")
	tab.Panes[types.PaneID("pane-1")] = pane
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:    types.TerminalID("term-1"),
		State: types.TerminalRunStateRunning,
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	return state
}
