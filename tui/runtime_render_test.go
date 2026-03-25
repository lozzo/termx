package tui

import (
	"io"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	btui "github.com/lozzow/termx/tui/bt"
	"github.com/lozzow/termx/tui/domain/types"
	"github.com/lozzow/termx/tui/render"
)

func TestRunUsesRichDefaultRendererAndBindsTerminalScreens(t *testing.T) {
	for _, tc := range []struct {
		name          string
		debugUI       bool
		wantContains  string
		wantScreenRow string
	}{
		{
			name:          "debug branch",
			debugUI:       true,
			wantContains:  "screen_shell:",
			wantScreenRow: "hello from screen",
		},
		{
			name:          "default branch",
			debugUI:       false,
			wantContains:  "termx  [main]  [1:shell]",
			wantScreenRow: "hello from screen",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &stubProgramRunner{}
			deps := runtimeRendererDeps()
			deps.ProgramRunner = runner
			err := runWithDependencies(&stubStartupClient{}, Config{DebugUI: tc.debugUI}, nil, io.Discard, deps)
			if err != nil {
				t.Fatalf("expected runtime orchestration to succeed, got %v", err)
			}
			plainView := xansi.Strip(runner.view)
			if !strings.Contains(plainView, tc.wantContains) {
				t.Fatalf("expected default renderer to keep rich output, got %q", plainView)
			}
			if !strings.Contains(plainView, tc.wantScreenRow) {
				t.Fatalf("expected default renderer to bind runtime terminal snapshots, got %q", plainView)
			}
			if strings.Contains(plainView, "\nactive_pane: pane-1\n") {
				t.Fatalf("expected default renderer not to regress to skeleton output, got %q", plainView)
			}
		})
	}
}

func TestRenderRendererCompatPathAdaptsSnapshotOnlyStore(t *testing.T) {
	enabled := true
	base := render.NewRenderer(render.Config{
		DebugVisible: &enabled,
		Compat: runtimeRenderer{
			DebugVisible: &enabled,
		},
	})
	binder, ok := base.(render.TerminalStoreBinder)
	if !ok {
		t.Fatalf("expected render renderer to implement terminal store binder")
	}

	view := binder.WithTerminalStore(snapshotOnlyStore{
		snapshots: map[types.TerminalID]*protocol.Snapshot{
			types.TerminalID("term-1"): runtimeRendererTestSnapshot("snapshot only store"),
		},
	}).Render(connectedRunAppState(), []btui.Notice{})

	if !strings.Contains(view, "snapshot only store") {
		t.Fatalf("expected compat renderer to consume snapshot-only store, got %q", view)
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

func runtimeRendererDeps() runtimeDependencies {
	return runtimeDependencies{
		Planner: &stubRunPlanner{
			plan: StartupPlan{
				State: connectedRunAppState(),
			},
		},
		TaskExecutor: &stubRunTaskExecutor{
			plan: StartupPlan{
				State: connectedRunAppState(),
			},
		},
		SessionBootstrap: &stubRunSessionBootstrapper{
			sessions: RuntimeSessions{
				Terminals: map[types.TerminalID]TerminalRuntimeSession{
					types.TerminalID("term-1"): {
						TerminalID: types.TerminalID("term-1"),
						Snapshot:   runtimeRendererTestSnapshot("hello from screen"),
					},
				},
			},
		},
		TerminalSize: func(io.Reader, io.Writer) protocol.Size {
			return protocol.Size{Cols: 120, Rows: 40}
		},
	}
}

type snapshotOnlyStore struct {
	snapshots map[types.TerminalID]*protocol.Snapshot
}

func (s snapshotOnlyStore) Snapshot(terminalID types.TerminalID) (*protocol.Snapshot, bool) {
	snapshot, ok := s.snapshots[terminalID]
	return snapshot, ok
}
