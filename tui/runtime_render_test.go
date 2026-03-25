package tui

import (
	"io"
	"strings"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/domain/types"
)

func TestRunUsesRichDefaultRendererAndBindsTerminalScreens(t *testing.T) {
	planner := &stubRunPlanner{
		plan: StartupPlan{
			State: connectedRunAppState(),
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
					Snapshot:   runtimeRendererTestSnapshot("hello from screen"),
				},
			},
		},
	}
	runner := &stubProgramRunner{}

	err := runWithDependencies(&stubStartupClient{}, Config{DebugUI: true}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		TerminalSize: func(io.Reader, io.Writer) protocol.Size {
			return protocol.Size{Cols: 120, Rows: 40}
		},
	})
	if err != nil {
		t.Fatalf("expected runtime orchestration to succeed, got %v", err)
	}
	if !strings.Contains(runner.view, "screen_shell:") {
		t.Fatalf("expected default renderer to keep rich screen shell output, got %q", runner.view)
	}
	if !strings.Contains(runner.view, "hello from screen") {
		t.Fatalf("expected default renderer to bind runtime terminal snapshots, got %q", runner.view)
	}
	if strings.Contains(runner.view, "\nactive_pane: pane-1\n") {
		t.Fatalf("expected default renderer not to regress to skeleton output, got %q", runner.view)
	}
}

func runtimeRendererTestSnapshot(line string) *protocol.Snapshot {
	return &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 120, Rows: 40},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{
				{
					{Content: line, Width: len(line)},
				},
			},
		},
	}
}
