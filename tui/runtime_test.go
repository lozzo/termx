package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app/intent"
	btui "github.com/lozzow/termx/tui/bt"
	layoutresolvedomain "github.com/lozzow/termx/tui/domain/layoutresolve"
	promptdomain "github.com/lozzow/termx/tui/domain/prompt"
	terminalmanagerdomain "github.com/lozzow/termx/tui/domain/terminalmanager"
	terminalpickerdomain "github.com/lozzow/termx/tui/domain/terminalpicker"
	"github.com/lozzow/termx/tui/domain/types"
	workspacedomain "github.com/lozzow/termx/tui/domain/workspace"
)

func stripANSIRuntimeView(view string) string {
	return xansi.Strip(view)
}

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
	deps := runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
		TerminalSize: func(io.Reader, io.Writer) protocol.Size {
			return protocol.Size{Cols: 120, Rows: 40}
		},
	}

	err := runWithDependencies(&stubStartupClient{}, Config{DefaultShell: "/bin/zsh"}, nil, io.Discard, deps)
	if err != nil {
		t.Fatalf("expected runtime orchestration to succeed, got %v", err)
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
	if runner.calls != 1 {
		t.Fatalf("expected program runner to run once, got %d", runner.calls)
	}
	if runner.view == "" {
		t.Fatalf("expected renderer to produce non-empty view")
	}
	if bootstrapperStopCalls != 1 {
		t.Fatalf("expected bootstrap session stop on program exit, got %d", bootstrapperStopCalls)
	}
}

func TestRunUsesShellOnlyRendererByDefault(t *testing.T) {
	planner := &stubRunPlanner{plan: StartupPlan{State: connectedRunAppState()}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: connectedRunAppState()}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "$"}, {Content: " "}, {Content: "p"}, {Content: "w"}, {Content: "d"}},
							},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{}

	err := runWithDependencies(&stubStartupClient{}, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
	})
	if err != nil {
		t.Fatalf("expected runtime orchestration to succeed, got %v", err)
	}
	stripped := stripANSIRuntimeView(runner.view)
	if !strings.Contains(stripped, "termx") || !strings.Contains(stripped, "workspace main") {
		t.Fatalf("expected default run renderer to keep screen shell, got:\n%s", runner.view)
	}
	if !strings.Contains(stripped, "state running") || !strings.Contains(stripped, "Terminal") || !strings.Contains(stripped, "Screen") || !strings.Contains(stripped, "$ pwd") {
		t.Fatalf("expected default run renderer to keep terminal context in screen shell, got:\n%s", runner.view)
	}
	if !strings.Contains(stripped, "Screen") || !strings.Contains(stripped, "rows 1/1  •  live") || !strings.Contains(stripped, "│ $ pwd") {
		t.Fatalf("expected default run renderer to keep a framed screen block, got:\n%s", runner.view)
	}
	if strings.Contains(runner.view, "wireframe_view:") || strings.Contains(runner.view, "chrome_header:") {
		t.Fatalf("expected default run renderer to hide debug sections, got:\n%s", runner.view)
	}
}

func TestRunCanEnableDebugRendererSections(t *testing.T) {
	planner := &stubRunPlanner{plan: StartupPlan{State: connectedRunAppState()}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: connectedRunAppState()}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "$"}, {Content: " "}, {Content: "p"}, {Content: "w"}, {Content: "d"}},
							},
						},
					},
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
	})
	if err != nil {
		t.Fatalf("expected runtime orchestration with debug renderer to succeed, got %v", err)
	}
	if !strings.Contains(runner.view, "wireframe_view:") || !strings.Contains(runner.view, "chrome_header:") {
		t.Fatalf("expected debug renderer to expose debug sections, got:\n%s", runner.view)
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

func TestRunReturnsProgramRunnerError(t *testing.T) {
	planner := &stubRunPlanner{plan: StartupPlan{State: connectedRunAppState()}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: connectedRunAppState()}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{err: errRuntimeRunBoom}

	err := runWithDependencies(&stubStartupClient{}, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if !errors.Is(err, errRuntimeRunBoom) {
		t.Fatalf("expected program runner error, got %v", err)
	}
}

func TestE2ERunScenarioRendersSnapshotAndForwardsActivePaneInput(t *testing.T) {
	client := &stubRunClient{}
	planner := &stubRunPlanner{plan: StartupPlan{State: connectedRunAppState()}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: connectedRunAppState()}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Channel:    21,
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{
									{Content: "h"},
									{Content: "i"},
								},
							},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			if view := model.View(); !strings.Contains(view, "screen_shell:") || !strings.Contains(view, "termx workbench | 78x24 | overlay none") || !strings.Contains(view, "Workspace main | Tab shell") || !strings.Contains(view, "Workbench tiled | focus tiled | mode none | overlay none") || !strings.Contains(view, "Active pane term-1 | pane pane-1 | terminal term-1 | slot connected") || !strings.Contains(view, "#> term-1 [owner] [tiled]") || !strings.Contains(view, "hi") || !strings.Contains(view, "#term-1 running owner") {
				t.Fatalf("expected runtime view to include snapshot content, got:\n%s", view)
			}
			_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
			if cmd == nil {
				t.Fatal("expected key input to produce runtime command")
			}
			if msg := cmd(); msg != nil {
				_, _ = model.Update(msg)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.inputs) != 1 {
		t.Fatalf("expected one forwarded input call, got %d", len(client.inputs))
	}
	if client.inputs[0].channel != 21 || string(client.inputs[0].data) != "a" {
		t.Fatalf("unexpected forwarded input payload: %+v", client.inputs[0])
	}
}

func TestE2ERunScenarioWindowResizeResizesActiveTerminal(t *testing.T) {
	client := &stubRunClient{}
	planner := &stubRunPlanner{plan: StartupPlan{State: connectedRunAppState()}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: connectedRunAppState()}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Channel:    21,
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Size:       protocol.Size{Cols: 80, Rows: 24},
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{
									{Content: "h"},
									{Content: "i"},
								},
							},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			_, cmd := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			if cmd == nil {
				t.Fatal("expected runtime resize command")
			}
			if msg := cmd(); msg != nil {
				_, _ = model.Update(msg)
			}
			if len(client.resizeCalls) != 1 {
				t.Fatalf("expected one runtime resize call, got %d", len(client.resizeCalls))
			}
			if client.resizeCalls[0].channel != 21 || client.resizeCalls[0].cols != 120 || client.resizeCalls[0].rows != 40 {
				t.Fatalf("unexpected runtime resize payload: %+v", client.resizeCalls[0])
			}
			if view := model.View(); !strings.Contains(view, "runtime_size: 120x40") {
				t.Fatalf("expected runtime view to expose resized size, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioFollowerPaneWindowResizeDoesNotResizeSharedTerminal(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithFollowerActivePane()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Channel:    21,
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Size:       protocol.Size{Cols: 80, Rows: 24},
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{
									{Content: "h"},
									{Content: "i"},
								},
							},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			_, cmd := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			if cmd != nil {
				if msg := cmd(); msg != nil {
					t.Fatalf("expected follower resize to produce no message, got %#v", msg)
				}
			}
			if len(client.resizeCalls) != 0 {
				t.Fatalf("expected follower pane to skip runtime resize call, got %+v", client.resizeCalls)
			}
			if view := model.View(); !strings.Contains(view, "connection_role: follower") || strings.Contains(view, "runtime_size: 120x40") {
				t.Fatalf("expected follower pane to keep old runtime size, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected follower resize scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioFollowerPaneAcquireOwnerEnablesResize(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithFollowerActivePane()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Channel:    21,
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Size:       protocol.Size{Cols: 80, Rows: 24},
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{
									{Content: "h"},
									{Content: "i"},
								},
							},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyRunes, Runes: []rune("a")},
				{Type: tea.KeyEsc},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "connection_role: owner") {
				t.Fatalf("expected acquire owner flow to promote active pane, got:\n%s", view)
			}
			nextModel, cmd := current.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			current = nextModel.(*btui.Model)
			if cmd == nil {
				t.Fatal("expected acquired owner resize command")
			}
			if msg := cmd(); msg != nil {
				nextModel, _ = current.Update(msg)
				current = nextModel.(*btui.Model)
			}
			if len(client.resizeCalls) != 1 {
				t.Fatalf("expected one runtime resize call after owner acquire, got %d", len(client.resizeCalls))
			}
			if client.resizeCalls[0].channel != 21 || client.resizeCalls[0].cols != 120 || client.resizeCalls[0].rows != 40 {
				t.Fatalf("unexpected runtime resize payload after owner acquire: %+v", client.resizeCalls[0])
			}
			if view := current.View(); !strings.Contains(view, "runtime_size: 120x40") || !strings.Contains(view, "connection_role: owner") {
				t.Fatalf("expected acquired owner resize flow to expose updated runtime size, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected acquire owner resize scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioActivePaneCoreViewVisible(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithActiveTerminalMetadata()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Channel:    21,
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{
									{Content: "$"},
									{Content: " "},
									{Content: "p"},
									{Content: "w"},
									{Content: "d"},
								},
								{
									{Content: "/"},
									{Content: "t"},
									{Content: "m"},
									{Content: "p"},
								},
							},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			if view := model.View(); !strings.Contains(view, "termx workbench | 78x24 | overlay none") || !strings.Contains(view, "Workspace main | Tab shell") || !strings.Contains(view, "Overview tabs 1 | panes 1 | terminals 1 | floating 0") || !strings.Contains(view, "Tabs [shell]") || !strings.Contains(view, "Workbench tiled | focus tiled | mode none | overlay none") || !strings.Contains(view, "Active pane api-dev | pane pane-1 | terminal term-1 | slot connected") || !strings.Contains(view, "Location main / shell / tiled / pane-1 | focus tiled | active api-dev") || !strings.Contains(view, "Notice none") || !strings.Contains(view, "Keys Ctrl-p pane | Ctrl-t tab | Ctrl-w ws | Ctrl-o float | ? help") || !strings.Contains(view, "chrome_header:") || !strings.Contains(view, "header_bar: ws=main | tab=shell | pane=pane-1 | slot=connected | overlay=none | focus=tiled") || !strings.Contains(view, "tab_path_bar: path=main/shell/tiled:pane-1 | target=api-dev") || !strings.Contains(view, "tab_layer_bar: tiled_root=pane-1 | floating_top=<none> | floating_total=0") || !strings.Contains(view, "chrome_body:") || !strings.Contains(view, "body_bar: terminal=term-1:running | screen=preview:2/2 | overlay=none") || !strings.Contains(view, "terminal_bar: id=term-1 | title=api-dev | state=running | role=owner") || !strings.Contains(view, "screen_bar: state=preview | rows=2/2") || !strings.Contains(view, "overlay_bar: kind=none") || !strings.Contains(view, "chrome_footer:") || !strings.Contains(view, "footer_bar: notices=0 | overlay=none") || !strings.Contains(view, "title: api-dev") || !strings.Contains(view, "tab_layer: tiled") || !strings.Contains(view, "pane_kind: tiled") || !strings.Contains(view, "terminal_state: running") || !strings.Contains(view, "screen:") || !strings.Contains(view, "$ pwd") || !strings.Contains(view, "/tmp") {
				t.Fatalf("expected runtime view to expose active pane core fields, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioConnectedPaneWithoutSnapshotKeepsScreenPlaceholder(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithActiveTerminalMetadata()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			if view := model.View(); !strings.Contains(view, "chrome_footer:") || !strings.Contains(view, "body_bar: terminal=term-1:running | screen=unavailable | overlay=none") || !strings.Contains(view, "terminal_bar: id=term-1 | title=api-dev | state=running | role=owner") || !strings.Contains(view, "screen_bar: state=unavailable") || !strings.Contains(view, "overlay_bar: kind=none") || !strings.Contains(view, "footer_bar: notices=0 | overlay=none") || !strings.Contains(view, "screen: <unavailable>") || !strings.Contains(view, "terminal: term-1") {
				t.Fatalf("expected runtime view without snapshot to keep stable screen placeholder and active terminal metadata, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioDefaultShellOnlyEmptyPaneShowsStatusAndActions(t *testing.T) {
	client := &stubRunClient{}
	initial := buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty)
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			stripped := stripANSIRuntimeView(view)
			if !strings.Contains(stripped, "No terminal connected yet.") || !strings.Contains(stripped, "Press n to start one, or a to connect an existing terminal.") {
				t.Fatalf("expected shell-only empty pane to expose status and actions, got:\n%s", view)
			}
			if strings.Contains(view, "wireframe_view:") || strings.Contains(view, "chrome_header:") {
				t.Fatalf("expected shell-only empty pane runtime view to keep debug sections hidden, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
	})
	if err != nil {
		t.Fatalf("expected empty pane shell-only scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioDefaultShellOnlyExitedPaneShowsStatusAndActions(t *testing.T) {
	client := &stubRunClient{}
	initial := connectedRunAppState()
	ws := initial.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	pane := tab.Panes[types.PaneID("pane-1")]
	exitCode := 7
	pane.SlotState = types.PaneSlotExited
	pane.LastExitCode = &exitCode
	tab.Panes[types.PaneID("pane-1")] = pane
	ws.Tabs[types.TabID("tab-1")] = tab
	initial.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	terminal := initial.Domain.Terminals[types.TerminalID("term-1")]
	terminal.State = types.TerminalRunStateExited
	terminal.ExitCode = &exitCode
	initial.Domain.Terminals[types.TerminalID("term-1")] = terminal
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			stripped := stripANSIRuntimeView(view)
			if !strings.Contains(stripped, "Terminal program exited.") || !strings.Contains(stripped, "history retained  exit 7") || !strings.Contains(stripped, "Press r to restart, or a to connect another terminal.") {
				t.Fatalf("expected shell-only exited pane to expose status and actions, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
	})
	if err != nil {
		t.Fatalf("expected exited pane shell-only scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioDefaultModernSplitWorkbenchRendersPaneCanvas(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithSplitPaneTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "$"}, {Content: " "}, {Content: "n"}, {Content: "p"}, {Content: "m"}, {Content: " "}, {Content: "r"}, {Content: "u"}, {Content: "n"}, {Content: " "}, {Content: "d"}, {Content: "e"}, {Content: "v"}},
								{{Content: "r"}, {Content: "e"}, {Content: "a"}, {Content: "d"}, {Content: "y"}},
							},
						},
					},
				},
				types.TerminalID("term-2"): {
					TerminalID: types.TerminalID("term-2"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-2",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: ">"}, {Content: " "}, {Content: "t"}, {Content: "s"}, {Content: "c"}, {Content: " "}, {Content: "-"}, {Content: "w"}},
								{{Content: "o"}, {Content: "k"}},
							},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			stripped := stripANSIRuntimeView(view)
			if !strings.Contains(stripped, "Split view") || !strings.Contains(stripped, "api-dev") || !strings.Contains(stripped, "build-log") {
				t.Fatalf("expected default modern split view to expose multi-pane canvas, got:\n%s", view)
			}
			if !strings.Contains(stripped, "live input  •  Ctrl-p  •  pick") || !strings.Contains(stripped, "standby pane  •  Ctrl-p pane") {
				t.Fatalf("expected default modern split view to expose pane footer hints, got:\n%s", view)
			}
			if !strings.Contains(stripped, "rows 2/2  •  live") || !strings.Contains(stripped, "rows 2/2  •  standby") || !strings.Contains(stripped, "│ $ npm run dev") || !strings.Contains(stripped, "│ > tsc -w") {
				t.Fatalf("expected default modern split view to expose framed screen blocks, got:\n%s", view)
			}
			if !strings.Contains(stripped, "$ npm run dev") || !strings.Contains(stripped, "> tsc -w") {
				t.Fatalf("expected default modern split view to expose both pane previews, got:\n%s", view)
			}
			if strings.Contains(stripped, "Pane map") || strings.Contains(view, "wireframe_view:") {
				t.Fatalf("expected default modern split view without legacy summary/debug sections, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
	})
	if err != nil {
		t.Fatalf("expected default modern split scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioDefaultModernFloatingWorkbenchRendersWindowDeck(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithFloatingOverviewTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "a"}, {Content: "p"}, {Content: "i"}, {Content: " "}, {Content: "r"}, {Content: "e"}, {Content: "a"}, {Content: "d"}, {Content: "y"}},
							},
						},
					},
				},
				types.TerminalID("term-2"): {
					TerminalID: types.TerminalID("term-2"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-2",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "b"}, {Content: "u"}, {Content: "i"}, {Content: "l"}, {Content: "d"}, {Content: " "}, {Content: "o"}, {Content: "k"}},
							},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			stripped := stripANSIRuntimeView(view)
			if !strings.Contains(stripped, "Floating workbench") || !strings.Contains(stripped, "Window deck") {
				t.Fatalf("expected default modern floating view to expose window deck, got:\n%s", view)
			}
			if !strings.Contains(stripped, "api-dev • owner • floating • active") || !strings.Contains(stripped, "Geometry") || !strings.Contains(stripped, "z 1/2") || !strings.Contains(stripped, "z 2/2") {
				t.Fatalf("expected default modern floating view to expose title bars and geometry depth, got:\n%s", view)
			}
			if !strings.Contains(stripped, "top float-2") || !strings.Contains(stripped, "live window  •  deck  •  Ctrl-o") || !strings.Contains(stripped, "standby window  •  deck") {
				t.Fatalf("expected default modern floating view to expose depth summary and footer hints, got:\n%s", view)
			}
			if !strings.Contains(stripped, "rows 1/1  •  live") || !strings.Contains(stripped, "│ api ready") {
				t.Fatalf("expected default modern floating view to expose framed active screen block, got:\n%s", view)
			}
			if !strings.Contains(stripped, "api ready") || !strings.Contains(stripped, "build ok") || !strings.Contains(stripped, "rect 10,8  30x12") {
				t.Fatalf("expected default modern floating view to expose preview and geometry, got:\n%s", view)
			}
			if strings.Contains(stripped, "Window stack") || strings.Contains(view, "wireframe_view:") {
				t.Fatalf("expected default modern floating view without legacy stack/debug sections, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
	})
	if err != nil {
		t.Fatalf("expected default modern floating scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioDefaultModernTerminalManagerOverlayRendersStructuredModal(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	manager := terminalmanagerdomain.NewState(initial.Domain, initial.UI.Focus)
	initial.UI.Overlay = types.OverlayState{
		Kind:        types.OverlayTerminalManager,
		Data:        manager,
		ReturnFocus: initial.UI.Focus,
	}
	initial.UI.Focus.Layer = types.FocusLayerOverlay
	initial.UI.Focus.OverlayTarget = types.OverlayTerminalManager
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			stripped := stripANSIRuntimeView(view)
			if !strings.Contains(stripped, "Terminal Manager") || !strings.Contains(stripped, "Selection") || !strings.Contains(stripped, "Visible terminals") || !strings.Contains(stripped, "Detail panel") || !strings.Contains(stripped, "Action bar") {
				t.Fatalf("expected default modern terminal manager modal structure, got:\n%s", view)
			}
			if !strings.Contains(stripped, "Backdrop workbench") || !strings.Contains(stripped, "background api-dev • owner • tiled") || !strings.Contains(stripped, "overlay active • terminal_manager") {
				t.Fatalf("expected default modern terminal manager modal to retain backdrop context, got:\n%s", view)
			}
			if !strings.Contains(stripped, "[terminal] api-dev") || !strings.Contains(stripped, "[create] + new terminal") || !strings.Contains(stripped, "running  •  visible") {
				t.Fatalf("expected default modern terminal manager rows, got:\n%s", view)
			}
			if !strings.Contains(stripped, "Detail") || !strings.Contains(stripped, "cmd npm run dev") || !strings.Contains(stripped, "owner pane:pane-1") {
				t.Fatalf("expected default modern terminal manager detail, got:\n%s", view)
			}
			if strings.Contains(view, "wireframe_view:") {
				t.Fatalf("expected default modern terminal manager without debug sections, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
	})
	if err != nil {
		t.Fatalf("expected default modern terminal manager scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioDefaultModernWorkspacePickerOverlayRendersStructuredModal(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerTarget()
	picker := workspacedomain.NewPickerState(initial.Domain)
	picker.AppendQuery("ops")
	picker.ExpandSelected()
	initial.UI.Overlay = types.OverlayState{
		Kind:        types.OverlayWorkspacePicker,
		Data:        picker,
		ReturnFocus: initial.UI.Focus,
	}
	initial.UI.Focus.Layer = types.FocusLayerOverlay
	initial.UI.Focus.OverlayTarget = types.OverlayWorkspacePicker
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			stripped := stripANSIRuntimeView(view)
			if !strings.Contains(stripped, "Workspace Picker") || !strings.Contains(stripped, "Selection") || !strings.Contains(stripped, "Tree panel") || !strings.Contains(stripped, "Target panel") || !strings.Contains(stripped, "Action bar") {
				t.Fatalf("expected default modern workspace picker modal structure, got:\n%s", view)
			}
			if !strings.Contains(stripped, "Target") || !strings.Contains(stripped, "workspace ops  (ws-2)") || !strings.Contains(stripped, "[-] [workspace] ops") || !strings.Contains(stripped, "[tab] logs") || !strings.Contains(stripped, "[pane] unconnected pane") {
				t.Fatalf("expected default modern workspace picker tree rows, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
	})
	if err != nil {
		t.Fatalf("expected default modern workspace picker scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioDefaultModernLayoutResolveOverlayRendersStructuredModal(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithLayoutResolveTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			stripped := stripANSIRuntimeView(view)
			if !strings.Contains(stripped, "Layout Resolve") || !strings.Contains(stripped, "Target") || !strings.Contains(stripped, "Choices") || !strings.Contains(stripped, "Actions") {
				t.Fatalf("expected default modern layout resolve modal structure, got:\n%s", view)
			}
			if !strings.Contains(stripped, "Selection") || !strings.Contains(stripped, "connect existing") || !strings.Contains(stripped, "create new") {
				t.Fatalf("expected default modern layout resolve actions, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
	})
	if err != nil {
		t.Fatalf("expected default modern layout resolve scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioDefaultModernPromptOverlayRendersStructuredModal(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	initial.UI.Overlay = types.OverlayState{
		Kind: types.OverlayPrompt,
		Data: &promptdomain.State{
			Kind:       promptdomain.KindEditTerminalMetadata,
			Title:      "edit terminal metadata",
			TerminalID: types.TerminalID("term-1"),
			Fields: []promptdomain.Field{
				{Key: "name", Label: "Name", Value: "api-dev"},
				{Key: "tags", Label: "Tags", Value: "env=dev"},
			},
			Active: 0,
		},
		ReturnFocus: initial.UI.Focus,
	}
	initial.UI.Focus.Layer = types.FocusLayerPrompt
	initial.UI.Focus.OverlayTarget = types.OverlayPrompt
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			stripped := stripANSIRuntimeView(view)
			if !strings.Contains(stripped, "Prompt") || !strings.Contains(stripped, "Fields") || !strings.Contains(stripped, "Actions") {
				t.Fatalf("expected default modern prompt modal structure, got:\n%s", view)
			}
			if !strings.Contains(stripped, "Context") || !strings.Contains(stripped, "terminal term-1") || !strings.Contains(stripped, "Name: api-dev") || !strings.Contains(stripped, "Tags: env=dev") {
				t.Fatalf("expected default modern prompt fields, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
	})
	if err != nil {
		t.Fatalf("expected default modern prompt scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioDefaultModernTerminalPickerOverlayRendersStructuredModal(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	picker := terminalpickerdomain.NewState(initial.Domain, initial.UI.Focus)
	picker.AppendQuery("build")
	initial.UI.Overlay = types.OverlayState{
		Kind:        types.OverlayTerminalPicker,
		Data:        picker,
		ReturnFocus: initial.UI.Focus,
	}
	initial.UI.Focus.Layer = types.FocusLayerOverlay
	initial.UI.Focus.OverlayTarget = types.OverlayTerminalPicker
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			stripped := stripANSIRuntimeView(view)
			if !strings.Contains(stripped, "Terminal Picker") || !strings.Contains(stripped, "Selection") || !strings.Contains(stripped, "Detail panel") || !strings.Contains(stripped, "Results panel") || !strings.Contains(stripped, "Action bar") {
				t.Fatalf("expected default modern terminal picker modal structure, got:\n%s", view)
			}
			if !strings.Contains(stripped, "[terminal] build-log") || !strings.Contains(stripped, "running  •  hidden") || !strings.Contains(stripped, "cmd tail -f build.log") || !strings.Contains(stripped, "tags group=build") {
				t.Fatalf("expected default modern terminal picker detail, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
	})
	if err != nil {
		t.Fatalf("expected default modern terminal picker scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTrailingBlankSnapshotKeepsMeaningfulPreviewVisible(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithActiveTerminalMetadata()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	rows := make([][]protocol.Cell, 0, 12)
	rows = append(rows,
		[]protocol.Cell{{Content: "$"}, {Content: " "}, {Content: "p"}, {Content: "w"}, {Content: "d"}},
		[]protocol.Cell{{Content: "/"}, {Content: "t"}, {Content: "m"}, {Content: "p"}},
		[]protocol.Cell{{Content: "r"}, {Content: "e"}, {Content: "a"}, {Content: "d"}, {Content: "y"}},
	)
	for i := 0; i < 9; i++ {
		rows = append(rows, []protocol.Cell{})
	}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Size:       protocol.Size{Cols: 120, Rows: 40},
						Screen:     protocol.ScreenData{Cells: rows},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			if !strings.Contains(view, "$ pwd") || !strings.Contains(view, "/tmp") || !strings.Contains(view, "ready") {
				t.Fatalf("expected trailing blank snapshot to keep meaningful rows visible, got:\n%s", view)
			}
			if !strings.Contains(view, "body_bar: terminal=term-1:running | screen=preview:3/12 | overlay=none") || !strings.Contains(view, "screen_bar: state=preview | rows=3/12") {
				t.Fatalf("expected runtime view to expose meaningful preview row counts, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected trailing blank snapshot scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioQuestionMarkOpensAndClosesHelpOverlay(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithActiveTerminalMetadata()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			nextModel, cmd := current.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "termx workbench | 78x24 | overlay help") || !strings.Contains(view, "Workspace main | tabs 1 | panes 1 | terminals 1 | floating 0 | Tabs [shell]") || !strings.Contains(view, "Workbench tiled | focus overlay | mode picker | overlay help") || !strings.Contains(view, "MASK[dimmed 78x24]") || !strings.Contains(view, "OVERLAY[help]") || !strings.Contains(view, "RETURN[tiled:ws-1/tab-1/pane-1]") || !strings.Contains(view, "# DIALOG[help]") || !strings.Contains(view, "TITLE[help]") || !strings.Contains(view, "FOOTER[esc close]") || !strings.Contains(view, "ACTIONS[esc close]") || !strings.Contains(view, "overlay_bar: kind=help") || !strings.Contains(view, "help_most_used: Ctrl-p pane | Ctrl-t tab | Ctrl-w workspace | Ctrl-f picker | Ctrl-o floating | Ctrl-g global") || !strings.Contains(view, "shortcut_bar: Esc close | ? help") {
				t.Fatalf("expected question mark to open help overlay, got:\n%s", view)
			}
			nextModel, cmd = current.Update(tea.KeyMsg{Type: tea.KeyEsc})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "termx workbench | 78x24 | overlay none") || !strings.Contains(view, "Workbench tiled | focus tiled | mode none | overlay none") || !strings.Contains(view, "overlay_bar: kind=none") || strings.Contains(view, "help_most_used:") || strings.Contains(view, "MASK[") || strings.Contains(view, "DIALOG[help]") || !strings.Contains(view, "screen_shell:") || !strings.Contains(view, "#> api-dev [owner] [tiled]") || !strings.Contains(view, "Keys Ctrl-p pane | Ctrl-t tab | Ctrl-w ws | Ctrl-o float | ? help") || !strings.Contains(view, "shortcut_bar: Ctrl-p pane | Ctrl-t tab | Ctrl-w ws | Ctrl-o float | Ctrl-f pick | Ctrl-g global | ? help") {
				t.Fatalf("expected esc to close help overlay and restore default shortcuts, got:\n%s", view)
			}
			if current.State().UI.Focus.Layer != types.FocusLayerTiled || current.State().UI.Overlay.Kind != types.OverlayNone {
				t.Fatalf("expected help close to restore main focus, got focus=%+v overlay=%+v", current.State().UI.Focus, current.State().UI.Overlay)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected help overlay runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioLayoutResolveEscClearsShellDialogAndMask(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithLayoutResolveTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			if view := current.View(); !strings.Contains(view, "MASK[dimmed 78x24]") || !strings.Contains(view, "OVERLAY[layout_resolve]") || !strings.Contains(view, "RETURN[tiled:ws-1/tab-1/pane-1]") || !strings.Contains(view, "# DIALOG[layout_resolve]") || !strings.Contains(view, "TITLE[layout_resolve]") || !strings.Contains(view, "LIST[resolve]") || !strings.Contains(view, "DETAIL[target]") || !strings.Contains(view, "F:pane-1 connect_existing") || !strings.Contains(view, "D:pane-1") || !strings.Contains(view, "FOOTER[enter confirm esc close]") {
				t.Fatalf("expected initial layout resolve shell dialog, got:\n%s", view)
			}
			nextModel, cmd := current.Update(tea.KeyMsg{Type: tea.KeyEsc})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay_bar: kind=none") || strings.Contains(view, "MASK[") || strings.Contains(view, "DIALOG[layout_resolve]") || !strings.Contains(view, "waiting pane [waiting] [tiled]") || !strings.Contains(view, "waiting for connect") || !strings.Contains(view, "n new | a connect") {
				t.Fatalf("expected esc to clear layout resolve shell dialog and restore pane shell, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected layout resolve shell-close runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerEscClearsShellDialogAndMask(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "MASK[dimmed 78x24]") || !strings.Contains(view, "OVERLAY[terminal_manager]") || !strings.Contains(view, "DIALOG[terminal_manager]") || !strings.Contains(view, "TITLE[terminal_manager]") || !strings.Contains(view, "FOOTER[enter here esc close]") {
				t.Fatalf("expected initial terminal manager shell dialog, got:\n%s", view)
			}
			nextModel, cmd := current.Update(tea.KeyMsg{Type: tea.KeyEsc})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || strings.Contains(view, "MASK[") || strings.Contains(view, "DIALOG[terminal_manager]") || !strings.Contains(view, "api-dev [owner] [tiled]") || !strings.Contains(view, "<screen unavailable>") || !strings.Contains(view, "term-1 running owner") {
				t.Fatalf("expected esc to clear terminal manager shell dialog and restore main pane shell, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected terminal manager shell-close runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioRendersWireframeWorkbench(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithActiveTerminalMetadata()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "$"}, {Content: " "}, {Content: "p"}, {Content: "w"}, {Content: "d"}},
								{{Content: "/"}, {Content: "t"}, {Content: "m"}, {Content: "p"}},
							},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			if !strings.Contains(view, "wireframe_view:") || !strings.Contains(view, "DEBUG[main/shell]") || !strings.Contains(view, "viewport=78x24") || !strings.Contains(view, "overlay=none") || !strings.Contains(view, "WORKBENCH[single]") || !strings.Contains(view, "ACTIVE[api-dev] ROLE[owner] STATE[running]") || !strings.Contains(view, "TERM[term-1] STATE[running]") {
				t.Fatalf("expected runtime view to expose wireframe workbench, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected wireframe runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioHeaderAndFooterExposeWorkspaceTabsAndFocus(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTwoTabTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			if !strings.Contains(view, "workspace_bar: [main]") || !strings.Contains(view, "workspace_summary: tabs=2 | panes=2 | terminals=2 | floating=0") || !strings.Contains(view, "tab_strip: [shell] | logs") || !strings.Contains(view, "pane_bar: title=api-dev | role=owner | kind=tiled") || !strings.Contains(view, "focus_bar: target=api-dev | layer=tiled | role=owner") {
				t.Fatalf("expected runtime view to expose workspace/tabs/focus chrome, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected workspace/tab chrome runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioSplitTabShowsTiledOutline(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithSplitPaneTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{
									{Content: "$"},
									{Content: " "},
									{Content: "n"},
									{Content: "p"},
									{Content: "m"},
									{Content: " "},
									{Content: "r"},
									{Content: "u"},
									{Content: "n"},
									{Content: " "},
									{Content: "d"},
									{Content: "e"},
									{Content: "v"},
								},
								{
									{Content: "r"},
									{Content: "e"},
									{Content: "a"},
									{Content: "d"},
									{Content: "y"},
									{Content: " "},
									{Content: "o"},
									{Content: "n"},
									{Content: " "},
									{Content: ":"},
									{Content: "3"},
									{Content: "0"},
									{Content: "0"},
									{Content: "0"},
								},
							},
						},
					},
				},
				types.TerminalID("term-2"): {
					TerminalID: types.TerminalID("term-2"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-2",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{
									{Content: ">"},
									{Content: " "},
									{Content: "t"},
									{Content: "s"},
									{Content: "c"},
									{Content: " "},
									{Content: "-"},
									{Content: "w"},
								},
								{
									{Content: "F"},
									{Content: "o"},
									{Content: "u"},
									{Content: "n"},
									{Content: "d"},
									{Content: " "},
									{Content: "0"},
									{Content: " "},
									{Content: "e"},
									{Content: "r"},
									{Content: "r"},
									{Content: "o"},
									{Content: "r"},
									{Content: "s"},
								},
							},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			if !strings.Contains(view, "tiled_outline_bar: active=pane-1 | total=2") || !strings.Contains(view, "tiled_layout: root=vertical | depth=2 | leaves=2 | ratio=0.50") || !strings.Contains(view, "tiled_outline:") || !strings.Contains(view, "> [tiled] api-dev | role=owner | state=running | preview=ready on :3000") || !strings.Contains(view, "  [tiled] build-log | role=owner | state=running | preview=Found 0 errors") {
				t.Fatalf("expected runtime view to expose tiled layout and pane previews for split tab, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected split tab runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioSplitTabShowsWireframeWorkbench(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithSplitPaneTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Size:       protocol.Size{Cols: 120, Rows: 40},
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "$"}, {Content: " "}, {Content: "n"}, {Content: "p"}, {Content: "m"}},
								{{Content: "r"}, {Content: "e"}, {Content: "a"}, {Content: "d"}, {Content: "y"}},
							},
						},
					},
				},
				types.TerminalID("term-2"): {
					TerminalID: types.TerminalID("term-2"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-2",
						Size:       protocol.Size{Cols: 120, Rows: 40},
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: ">"}, {Content: " "}, {Content: "t"}, {Content: "s"}, {Content: "c"}},
								{{Content: "o"}, {Content: "k"}},
							},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			if !strings.Contains(view, "wireframe_view:") || !strings.Contains(view, "SPLIT SHELL[vertical 50/50]") || !strings.Contains(view, "LAYOUT[split] root=vertical ratio=50/50 leaves=2") || !strings.Contains(view, "TILED CANVAS[96x12 panes=2]") || !strings.Contains(view, "#> api-dev [owner] [tiled]") || !strings.Contains(view, "|  build-log [owner] [tiled]") || !strings.Contains(view, "$ npm") || !strings.Contains(view, "ready") || !strings.Contains(view, "#term-1 running owner") || !strings.Contains(view, "> tsc") || !strings.Contains(view, "ok") || !strings.Contains(view, "|term-2 running owner") || !strings.Contains(view, "DEBUG[main/shell]") || !strings.Contains(view, "viewport=96x40") || !strings.Contains(view, "WORKBENCH[split]") || !strings.Contains(view, "SPLIT[vertical]") || !strings.Contains(view, "RATIO[0.50]") || !strings.Contains(view, "LEAVES[2]") || !strings.Contains(view, "ACTIVE[api-dev] ROLE[owner] STATE[running]") || !strings.Contains(view, "PANE[build-log] ROLE[owner] STATE[running]") {
				t.Fatalf("expected runtime view to expose split wireframe workbench, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected split wireframe runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioNestedSplitShowsTiledTree(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithNestedSplitPaneTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "r"}, {Content: "e"}, {Content: "a"}, {Content: "d"}, {Content: "y"}}},
						},
					},
				},
				types.TerminalID("term-2"): {
					TerminalID: types.TerminalID("term-2"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-2",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "b"}, {Content: "u"}, {Content: "i"}, {Content: "l"}, {Content: "d"}}},
						},
					},
				},
				types.TerminalID("term-3"): {
					TerminalID: types.TerminalID("term-3"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-3",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "w"}, {Content: "a"}, {Content: "t"}, {Content: "c"}, {Content: "h"}}},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			if !strings.Contains(view, "tiled_tree:") || !strings.Contains(view, "split horizontal ratio=0.60") || !strings.Contains(view, "|- > [tiled] api-dev | role=owner | state=running | preview=ready") || !strings.Contains(view, "\\- split vertical ratio=0.50") || !strings.Contains(view, "   |- [tiled] watcher | role=owner | state=running | preview=watch") || !strings.Contains(view, "   \\- [tiled] build-log | role=owner | state=running | preview=build") {
				t.Fatalf("expected runtime view to expose nested tiled tree, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected nested split runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioFloatingLayerShowsOutline(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithFloatingOverviewTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "a"}, {Content: "p"}, {Content: "i"}, {Content: " "}, {Content: "r"}, {Content: "e"}, {Content: "a"}, {Content: "d"}, {Content: "y"}},
							},
						},
					},
				},
				types.TerminalID("term-2"): {
					TerminalID: types.TerminalID("term-2"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-2",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "b"}, {Content: "u"}, {Content: "i"}, {Content: "l"}, {Content: "d"}, {Content: " "}, {Content: "o"}, {Content: "k"}},
							},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			if !strings.Contains(view, "Workbench floating | focus floating | mode none | overlay none") || !strings.Contains(view, "FLOAT SHELL[2]") || !strings.Contains(view, "FLOAT CANVAS[78x12 windows=2]") || !strings.Contains(view, "STACK[windows] total=2") || !strings.Contains(view, "FOCUS[float-1] api-dev") || !strings.Contains(view, "WINDOW LIST[2]") || !strings.Contains(view, "> [float-1] api-dev | owner | 10,8 30x12 | running | api ready") || !strings.Contains(view, "  [float-2] build-log | owner | 45,14 28x10 | running | build ok") || strings.Contains(view, "WINDOW CARD[float-1]") || strings.Contains(view, "WINDOW CARD[float-2]") || !strings.Contains(view, "#> api-dev [owner] [floating]") || !strings.Contains(view, "|term-2 running owner") || !strings.Contains(view, "api ready") || !strings.Contains(view, "build ok") || !strings.Contains(view, "#term-1 running owner") || !strings.Contains(view, "tab_path_bar: path=main/shell/floating:float-1 | target=api-dev") || !strings.Contains(view, "tab_layer_bar: tiled_root=<none> | floating_top=float-2 | floating_total=2") || !strings.Contains(view, "floating_outline_bar: active=float-1 | total=2 | top=float-2") || !strings.Contains(view, "floating_outline:") || !strings.Contains(view, "> [floating] api-dev | role=owner | rect=10,8 30x12 | state=running | preview=api ready") || !strings.Contains(view, "  [floating] build-log | role=owner | rect=45,14 28x10 | state=running | preview=build ok") {
				t.Fatalf("expected runtime view to expose floating outline, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected floating outline runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioOverlayShowsWireframeDialog(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	manager := terminalmanagerdomain.NewState(initial.Domain, initial.UI.Focus)
	initial.UI.Overlay = types.OverlayState{
		Kind: types.OverlayTerminalManager,
		Data: manager,
	}
	initial.UI.Focus.Layer = types.FocusLayerOverlay
	initial.UI.Focus.OverlayTarget = types.OverlayTerminalManager
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			if !strings.Contains(view, "DEBUG_OVERLAY[terminal_manager]") || !strings.Contains(view, "focus=overlay") || !strings.Contains(view, "ROWS[7] SELECTED[term-1]") || !strings.Contains(view, "> [terminal] api-dev") {
				t.Fatalf("expected runtime view to expose terminal manager wireframe dialog, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected overlay wireframe runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioWorkspacePickerShowsWireframeTree(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerTarget()
	picker := workspacedomain.NewPickerState(initial.Domain)
	picker.AppendQuery("ops")
	picker.ExpandSelected()
	initial.UI.Overlay = types.OverlayState{
		Kind: types.OverlayWorkspacePicker,
		Data: picker,
	}
	initial.UI.Focus.Layer = types.FocusLayerOverlay
	initial.UI.Focus.OverlayTarget = types.OverlayWorkspacePicker

	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			if !strings.Contains(view, "DEBUG_OVERLAY[workspace_picker]") || !strings.Contains(view, "focus=overlay") || !strings.Contains(view, "ROWS[6] QUERY[ops] SELECTED[ws-2]") || !strings.Contains(view, "TARGET[workspace] LABEL[ops] DEPTH[0]") || !strings.Contains(view, "  [tab] logs") || !strings.Contains(view, "    [pane] unconnected pane") {
				t.Fatalf("expected runtime view to expose workspace picker wireframe tree, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected workspace picker wireframe scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioLayoutResolveShowsWireframeDialog(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithLayoutResolveTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			if !strings.Contains(view, "# DIALOG[layout_resolve]") || !strings.Contains(view, "LIST[resolve]") || !strings.Contains(view, "DETAIL[target]") || !strings.Contains(view, "RETURN TO[tiled:ws-1/tab-1/pane-1]") || !strings.Contains(view, "ACTIONS[enter confirm esc close]") || !strings.Contains(view, "DEBUG_OVERLAY[layout_resolve]") || !strings.Contains(view, "focus=overlay") || !strings.Contains(view, "ROWS[3] PANE[pane-1] ROLE[backend-dev]") || !strings.Contains(view, "HINT[env=dev service=api]") || !strings.Contains(view, "> [connect_existing] connect existing") {
				t.Fatalf("expected runtime view to expose layout resolve wireframe dialog, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected layout resolve wireframe scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioNestedSplitShowsWireframeLayoutTree(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithNestedSplitPaneTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "r"}, {Content: "e"}, {Content: "a"}, {Content: "d"}, {Content: "y"}}},
						},
					},
				},
				types.TerminalID("term-2"): {
					TerminalID: types.TerminalID("term-2"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-2",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "b"}, {Content: "u"}, {Content: "i"}, {Content: "l"}, {Content: "d"}}},
						},
					},
				},
				types.TerminalID("term-3"): {
					TerminalID: types.TerminalID("term-3"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-3",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "w"}, {Content: "a"}, {Content: "t"}, {Content: "c"}, {Content: "h"}}},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			if !strings.Contains(view, "DEBUG[main/shell]") || !strings.Contains(view, "WORKBENCH[split]") || !strings.Contains(view, "SPLIT[horizontal]") || !strings.Contains(view, "RATIO[0.60]") || !strings.Contains(view, "LEAVES[3]") || !strings.Contains(view, "ACTIVE[api-dev] ROLE[owner] STATE[running]") || !strings.Contains(view, "PANE[watcher] ROLE[owner] STATE[running]") || !strings.Contains(view, "PANE[build-log] ROLE[owner] STATE[running]") {
				t.Fatalf("expected runtime view to expose nested split wireframe tree, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected nested split wireframe scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioLayoutResolveShowsWireframeBackdropAndReturnFocus(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithLayoutResolveTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			if !strings.Contains(view, "DEBUG_OVERLAY[layout_resolve]") || !strings.Contains(view, "center=10/58") || !strings.Contains(view, "RETURN[tiled:ws-1/tab-1/pane-1]") {
				t.Fatalf("expected runtime view to expose overlay backdrop/return-focus summary, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected overlay backdrop runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioMixedSlotShowsWireframeWorkbench(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithMixedPaneSlots()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "a"}, {Content: "p"}, {Content: "i"}, {Content: " "}, {Content: "u"}, {Content: "p"}},
							},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			if !strings.Contains(view, "TILED CANVAS[78x12 panes=3]") || !strings.Contains(view, "waiting pane [waiting] [tiled]") || !strings.Contains(view, "api-dev [owner] [tiled]") || !strings.Contains(view, "deploy-log [exited] [tiled]") || !strings.Contains(view, "waiting for connect") || !strings.Contains(view, "n new | a connect") || !strings.Contains(view, "process exited") || !strings.Contains(view, "history retained") || !strings.Contains(view, "FLOATING WINDOWS[1]") || !strings.Contains(view, "WINDOW LIST[1]") || !strings.Contains(view, "  [float-empty] unconnected pane | empty | 60,2 20x8 | no terminal connected") || !strings.Contains(view, "WORKBENCH[split]") || !strings.Contains(view, "PANE[waiting pane] SLOT[waiting]") || !strings.Contains(view, "PANE[deploy-log] ROLE[owner] STATE[exited]") || !strings.Contains(view, "FLOATING[1]") || !strings.Contains(view, "FLOAT[float-empty] unconnected pane empty 60,2 20x8") || strings.Contains(view, "CARD[pane-3] deploy-log [owner]") || strings.Contains(view, "WINDOW CARD[float-empty]") {
				t.Fatalf("expected runtime view to expose mixed slot wireframe workbench, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected mixed slot wireframe scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTabSummaryShowsMixedPaneSlots(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithMixedPaneSlots()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{
								{{Content: "a"}, {Content: "p"}, {Content: "i"}, {Content: " "}, {Content: "u"}, {Content: "p"}},
							},
						},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			if !strings.Contains(view, "tab_summary: tiled=3 | floating=1 | connected=1 | waiting=1 | exited=1 | empty=1 | active_layer=tiled") || !strings.Contains(view, "|- [tiled] waiting pane | slot=waiting | detail=layout pending") || !strings.Contains(view, "\\- [tiled] deploy-log | slot=exited | exit=7 | detail=history retained | state=exited") || !strings.Contains(view, "  [floating] unconnected pane | slot=empty | detail=terminal missing | rect=60,2 20x8") {
				t.Fatalf("expected runtime view to expose tab summary and mixed pane slots, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected mixed pane slot runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioLongSummaryLinesStayCompacted(t *testing.T) {
	client := &stubRunClient{}
	initial := connectedRunAppState()
	workspace := initial.Domain.Workspaces[initial.Domain.ActiveWorkspaceID]
	tab := workspace.Tabs[workspace.ActiveTabID]
	pane := tab.Panes[tab.ActivePaneID]
	workspace.Name = "workspace-with-an-extremely-long-name-that-should-not-let-runtime-status-lines-grow-without-bound"
	tab.Name = "tab-with-an-extremely-long-name-that-should-not-let-runtime-status-lines-grow-without-bound"
	tab.Panes[pane.ID] = pane
	workspace.Tabs[tab.ID] = tab
	initial.Domain.Workspaces[workspace.ID] = workspace
	initial.Domain.Terminals[pane.TerminalID] = types.TerminalRef{
		ID:      pane.TerminalID,
		Name:    "terminal-with-a-very-long-title-that-should-not-let-runtime-terminal-summary-lines-grow-without-bound",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	picker := terminalpickerdomain.NewState(initial.Domain, initial.UI.Focus)
	picker.AppendQuery("query-with-a-very-long-value-that-should-not-let-runtime-overlay-summary-lines-grow-without-bound")
	initial.UI.Overlay = types.OverlayState{
		Kind: types.OverlayTerminalPicker,
		Data: picker,
	}
	initial.UI.Focus.Layer = types.FocusLayerOverlay

	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			statusSummary := findLineWithPrefix(view, "workspace:")
			if statusSummary == "" || len(statusSummary) > runtimeSummaryMaxWidth || !strings.Contains(statusSummary, "slot: connected") {
				t.Fatalf("expected compacted runtime status summary, got:\n%s", view)
			}
			terminalSummary := findLineWithPrefix(view, "terminal_bar:")
			if terminalSummary == "" || len(terminalSummary) > runtimeSummaryMaxWidth || !strings.Contains(terminalSummary, "terminal_bar: id=term-1") || !strings.Contains(terminalSummary, "grow-without-bound") {
				t.Fatalf("expected compacted runtime terminal summary, got:\n%s", view)
			}
			overlaySummary := findLineWithPrefix(view, "overlay_bar:")
			if overlaySummary == "" || len(overlaySummary) > runtimeSummaryMaxWidth || !strings.Contains(overlaySummary, "terminal_picker_row_count: 1") {
				t.Fatalf("expected compacted runtime overlay summary, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioLongDetailLinesStayCompacted(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	terminal := initial.Domain.Terminals[types.TerminalID("term-2")]
	terminal.Command = []string{
		"tail",
		"-f",
		"build-log-with-a-very-long-name-that-should-not-let-runtime-detail-lines-grow-without-bound",
		"--profile=build-pipeline-with-a-very-long-profile-name-that-keeps-the-runtime-detail-line-growing",
		"--region=us-east-1-development-cluster",
	}
	initial.Domain.Terminals[types.TerminalID("term-2")] = terminal
	manager := terminalmanagerdomain.NewState(initial.Domain, initial.UI.Focus)
	manager.MoveSelection(1)
	initial.UI.Overlay = types.OverlayState{
		Kind: types.OverlayTerminalManager,
		Data: manager,
	}
	initial.UI.Focus.Layer = types.FocusLayerOverlay

	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			view := model.View()
			selectedCommand := findLineWithPrefix(view, "terminal_manager_selected_command:")
			if selectedCommand == "" || len(selectedCommand) > runtimeDetailMaxWidth || !strings.Contains(selectedCommand, "terminal_manager_selected_owner:") {
				t.Fatalf("expected compacted runtime selected command line, got:\n%s", view)
			}
			detailCommand := findLineWithPrefix(view, "detail_connected_panes:")
			if detailCommand == "" || len(detailCommand) > runtimeDetailMaxWidth || !strings.Contains(detailCommand, "detail_command: tail -f") {
				t.Fatalf("expected compacted runtime detail command line, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioStreamOutputTriggersViewRefresh(t *testing.T) {
	stream := make(chan protocol.StreamFrame, 1)
	client := &stubRunClient{}
	planner := &stubRunPlanner{plan: StartupPlan{State: connectedRunAppState()}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: connectedRunAppState()}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Channel:    21,
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Size:       protocol.Size{Cols: 4, Rows: 1},
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "h"}, {Content: "i"}}},
						},
						Cursor: protocol.CursorState{Row: 0, Col: 2, Visible: true},
					},
					Stream: stream,
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			initCmd := model.Init()
			if initCmd == nil {
				t.Fatal("expected runtime init command")
			}
			stream <- protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("!")}
			msg := initCmd()
			if msg == nil {
				t.Fatal("expected runtime stream message")
			}
			_, nextCmd := model.Update(msg)
			if nextCmd == nil {
				t.Fatal("expected follow-up listen command")
			}
			if view := model.View(); !strings.Contains(view, "hi!") {
				t.Fatalf("expected runtime view to include streamed content, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioClosedFrameFeedsReducerAndMarksPaneExited(t *testing.T) {
	stream := make(chan protocol.StreamFrame, 1)
	client := &stubRunClient{}
	planner := &stubRunPlanner{plan: StartupPlan{State: connectedRunAppState()}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: connectedRunAppState()}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Channel:    21,
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Size:       protocol.Size{Cols: 4, Rows: 1},
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "h"}, {Content: "i"}}},
						},
						Cursor: protocol.CursorState{Row: 0, Col: 2, Visible: true},
					},
					Stream: stream,
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			initCmd := model.Init()
			if initCmd == nil {
				t.Fatal("expected runtime init command")
			}
			stream <- protocol.StreamFrame{Type: protocol.TypeClosed, Payload: protocol.EncodeClosedPayload(7)}
			msg := initCmd()
			if msg == nil {
				t.Fatal("expected runtime closed message")
			}
			_, cmd := model.Update(msg)
			for _, nextMsg := range runCmdMessages(cmd) {
				_, _ = model.Update(nextMsg)
			}
			if view := model.View(); !strings.Contains(view, "slot: exited") {
				t.Fatalf("expected runtime view to mark pane exited, got:\n%s", view)
			}
			if view := model.View(); !strings.Contains(view, "terminal_state: exited") || !strings.Contains(view, "terminal_exit_code: 7") || !strings.Contains(view, "pane_exit_code: 7") || !strings.Contains(view, "runtime_state: exited") || !strings.Contains(view, "runtime_exit_code: 7") {
				t.Fatalf("expected runtime view to expose exited terminal state, got:\n%s", view)
			}
			if terminal := model.State().Domain.Terminals[types.TerminalID("term-1")]; terminal.ExitCode == nil || *terminal.ExitCode != 7 {
				t.Fatalf("expected reducer to retain exit code, got %+v", terminal)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioSyncLostShowsRuntimeStatusAndRefreshesSnapshot(t *testing.T) {
	stream := make(chan protocol.StreamFrame, 1)
	client := &stubRunClient{
		snapshots: map[string]*protocol.Snapshot{
			"term-1": {
				TerminalID: "term-1",
				Size:       protocol.Size{Cols: 6, Rows: 1},
				Screen: protocol.ScreenData{
					Cells: [][]protocol.Cell{{{Content: "n"}, {Content: "e"}, {Content: "w"}}},
				},
				Cursor: protocol.CursorState{Row: 0, Col: 3, Visible: true},
			},
		},
	}
	initial := connectedRunAppState()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Channel:    21,
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Size:       protocol.Size{Cols: 4, Rows: 1},
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "o"}, {Content: "l"}, {Content: "d"}}},
						},
						Cursor: protocol.CursorState{Row: 0, Col: 3, Visible: true},
					},
					Stream: stream,
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			initCmd := model.Init()
			if initCmd == nil {
				t.Fatal("expected runtime init command")
			}
			stream <- protocol.StreamFrame{Type: protocol.TypeSyncLost, Payload: protocol.EncodeSyncLostPayload(32)}
			msg := initCmd()
			if msg == nil {
				t.Fatal("expected runtime sync-lost message")
			}
			_, cmd := model.Update(msg)
			if view := model.View(); !strings.Contains(view, "runtime_sync_lost: 32") {
				t.Fatalf("expected runtime view to expose pending sync-lost status, got:\n%s", view)
			}
			for _, nextMsg := range runCmdMessages(cmd) {
				_, _ = model.Update(nextMsg)
			}
			if view := model.View(); !strings.Contains(view, "screen:") || !strings.Contains(view, "new") || strings.Contains(view, "runtime_sync_lost: 32") {
				t.Fatalf("expected runtime view to refresh snapshot and clear sync-lost status, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioStateChangedStoppedFeedsReducerAndClearsPane(t *testing.T) {
	events := make(chan protocol.Event, 1)
	client := &stubRunClient{}
	planner := &stubRunPlanner{plan: StartupPlan{State: connectedRunAppState()}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: connectedRunAppState()}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			EventStream: events,
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Channel:    21,
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Size:       protocol.Size{Cols: 4, Rows: 1},
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "h"}, {Content: "i"}}},
						},
						Cursor: protocol.CursorState{Row: 0, Col: 2, Visible: true},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			initCmd := model.Init()
			if initCmd == nil {
				t.Fatal("expected runtime init command")
			}
			events <- protocol.Event{
				Type:       protocol.EventTerminalStateChanged,
				TerminalID: "term-1",
				StateChanged: &protocol.TerminalStateChangedData{
					OldState: "running",
					NewState: "stopped",
				},
			}
			msg := initCmd()
			if msg == nil {
				t.Fatal("expected runtime state-changed message")
			}
			_, cmd := model.Update(msg)
			for _, nextMsg := range runCmdMessages(cmd) {
				_, _ = model.Update(nextMsg)
			}
			if view := model.View(); !strings.Contains(view, "slot: empty") {
				t.Fatalf("expected runtime view to clear stopped pane, got:\n%s", view)
			}
			pane := model.State().Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
			if pane.TerminalID != "" {
				t.Fatalf("expected reducer to clear pane terminal, got %+v", pane)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioResizedEventUpdatesRuntimeSizeInView(t *testing.T) {
	events := make(chan protocol.Event, 1)
	client := &stubRunClient{}
	initial := connectedRunAppState()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			EventStream: events,
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Channel:    21,
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Size:       protocol.Size{Cols: 80, Rows: 24},
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "o"}, {Content: "k"}}},
						},
						Cursor: protocol.CursorState{Row: 0, Col: 2, Visible: true},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			initCmd := model.Init()
			if initCmd == nil {
				t.Fatal("expected runtime init command")
			}
			events <- protocol.Event{
				Type:       protocol.EventTerminalResized,
				TerminalID: "term-1",
				Resized: &protocol.TerminalResizedData{
					OldSize: protocol.Size{Cols: 80, Rows: 24},
					NewSize: protocol.Size{Cols: 120, Rows: 40},
				},
			}
			msg := initCmd()
			if msg == nil {
				t.Fatal("expected runtime resized message")
			}
			_, cmd := model.Update(msg)
			if cmd == nil {
				t.Fatal("expected resized event to keep runtime listener active")
			}
			if view := model.View(); !strings.Contains(view, "runtime_size: 120x40") {
				t.Fatalf("expected runtime view to expose resized size, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioCreatedEventRegistersDetachedTerminal(t *testing.T) {
	events := make(chan protocol.Event, 1)
	client := &stubRunClient{}
	initial := buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty)
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			EventStream: events,
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			initCmd := model.Init()
			if initCmd == nil {
				t.Fatal("expected runtime init command")
			}
			events <- protocol.Event{
				Type:       protocol.EventTerminalCreated,
				TerminalID: "term-2",
				Created: &protocol.TerminalCreatedData{
					Name:    "build-log",
					Command: []string{"tail", "-f", "build.log"},
					Size:    protocol.Size{Cols: 120, Rows: 40},
				},
			}
			msg := initCmd()
			if msg == nil {
				t.Fatal("expected runtime created message")
			}
			_, cmd := model.Update(msg)
			for _, nextMsg := range runCmdMessages(cmd) {
				_, _ = model.Update(nextMsg)
			}
			terminal, ok := model.State().Domain.Terminals[types.TerminalID("term-2")]
			if !ok {
				t.Fatal("expected runtime created terminal to be registered")
			}
			if terminal.Name != "build-log" || terminal.State != types.TerminalRunStateRunning {
				t.Fatalf("unexpected created terminal state: %+v", terminal)
			}
			if terminal.Visible {
				t.Fatalf("expected detached created terminal to remain non-visible, got %+v", terminal)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioRemovedReasonVisibleInView(t *testing.T) {
	events := make(chan protocol.Event, 1)
	client := &stubRunClient{}
	initial := connectedRunAppState()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{EventStream: events},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			initCmd := model.Init()
			if initCmd == nil {
				t.Fatal("expected runtime init command")
			}
			events <- protocol.Event{
				Type:       protocol.EventTerminalRemoved,
				TerminalID: "term-1",
				Removed:    &protocol.TerminalRemovedData{Reason: "server_shutdown"},
			}
			msg := initCmd()
			if msg == nil {
				t.Fatal("expected runtime removed message")
			}
			_, cmd := model.Update(msg)
			if view := model.View(); !strings.Contains(view, "runtime_removed: server_shutdown") {
				t.Fatalf("expected runtime view to expose removed reason before reducer clears pane, got:\n%s", view)
			}
			for _, nextMsg := range runCmdMessages(cmd) {
				_, _ = model.Update(nextMsg)
			}
			if view := model.View(); !strings.Contains(view, "slot: empty") || strings.Contains(view, "terminal: term-1") {
				t.Fatalf("expected removed feedback to clear active pane after reason was exposed, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioReadErrorVisibleInView(t *testing.T) {
	events := make(chan protocol.Event, 1)
	client := &stubRunClient{}
	initial := connectedRunAppState()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{EventStream: events},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			initCmd := model.Init()
			if initCmd == nil {
				t.Fatal("expected runtime init command")
			}
			events <- protocol.Event{
				Type:       protocol.EventTerminalReadError,
				TerminalID: "term-1",
				ReadError:  &protocol.TerminalReadErrorData{Error: "pty read failed"},
			}
			msg := initCmd()
			if msg == nil {
				t.Fatal("expected runtime read-error message")
			}
			_, cmd := model.Update(msg)
			for _, nextMsg := range runCmdMessages(cmd) {
				_, _ = model.Update(nextMsg)
			}
			if view := model.View(); !strings.Contains(view, "runtime_read_error: pty read failed") || !strings.Contains(view, "notice_bar: total=1 | showing=1 | last=error | notices:") || !strings.Contains(view, "notice_group_bar: error=1") || !strings.Contains(view, "notices:") {
				t.Fatalf("expected runtime view to expose read error status and notice, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioCollaboratorsRevokedBlocksSubsequentInput(t *testing.T) {
	events := make(chan protocol.Event, 1)
	client := &stubRunClient{}
	planner := &stubRunPlanner{plan: StartupPlan{State: connectedRunAppState()}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: connectedRunAppState()}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			EventStream: events,
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Channel:    21,
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Size:       protocol.Size{Cols: 80, Rows: 24},
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "h"}, {Content: "i"}}},
						},
						Cursor: protocol.CursorState{Row: 0, Col: 2, Visible: true},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			initCmd := model.Init()
			if initCmd == nil {
				t.Fatal("expected runtime init command")
			}
			events <- protocol.Event{
				Type:                 protocol.EventCollaboratorsRevoked,
				TerminalID:           "term-1",
				CollaboratorsRevoked: &protocol.CollaboratorsRevokedData{},
			}
			msg := initCmd()
			if msg == nil {
				t.Fatal("expected runtime revoke message")
			}
			_, cmd := model.Update(msg)
			for _, nextMsg := range runCmdMessages(cmd) {
				_, _ = model.Update(nextMsg)
			}
			if view := model.View(); !strings.Contains(view, "runtime_access: observer_only") {
				t.Fatalf("expected runtime view to show observer-only status, got:\n%s", view)
			}
			_, inputCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
			if inputCmd == nil {
				t.Fatal("expected blocked input to produce notice command")
			}
			feedback, ok := inputCmd().(btui.FeedbackMsg)
			if !ok || len(feedback.Notices) != 1 {
				t.Fatalf("expected blocked input notice, got %#v", inputCmd())
			}
			if len(client.inputs) != 0 {
				t.Fatalf("expected no forwarded input after revoke, got %+v", client.inputs)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioBlockedInputNoticeAppearsInView(t *testing.T) {
	events := make(chan protocol.Event, 1)
	client := &stubRunClient{}
	planner := &stubRunPlanner{plan: StartupPlan{State: connectedRunAppState()}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: connectedRunAppState()}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{
			EventStream: events,
			Terminals: map[types.TerminalID]TerminalRuntimeSession{
				types.TerminalID("term-1"): {
					TerminalID: types.TerminalID("term-1"),
					Channel:    21,
					Snapshot: &protocol.Snapshot{
						TerminalID: "term-1",
						Size:       protocol.Size{Cols: 80, Rows: 24},
						Screen: protocol.ScreenData{
							Cells: [][]protocol.Cell{{{Content: "h"}, {Content: "i"}}},
						},
						Cursor: protocol.CursorState{Row: 0, Col: 2, Visible: true},
					},
				},
			},
		},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			initCmd := model.Init()
			if initCmd == nil {
				t.Fatal("expected runtime init command")
			}
			events <- protocol.Event{
				Type:                 protocol.EventCollaboratorsRevoked,
				TerminalID:           "term-1",
				CollaboratorsRevoked: &protocol.CollaboratorsRevokedData{},
			}
			msg := initCmd()
			if msg == nil {
				t.Fatal("expected runtime revoke message")
			}
			_, cmd := model.Update(msg)
			for _, nextMsg := range runCmdMessages(cmd) {
				_, _ = model.Update(nextMsg)
			}
			_, inputCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
			if inputCmd == nil {
				t.Fatal("expected blocked input to produce notice command")
			}
			feedback := inputCmd()
			if feedback == nil {
				t.Fatal("expected blocked input feedback message")
			}
			_, _ = model.Update(feedback)
			if view := model.View(); !strings.Contains(view, "notices:") || !strings.Contains(view, "observer-only") {
				t.Fatalf("expected runtime view to show blocked input notice, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioRepeatedNoticeAppearsAggregatedInView(t *testing.T) {
	client := &stubRunClient{}
	initial := connectedRunAppState()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			feedback := btui.FeedbackMsg{
				Notices: []btui.Notice{{
					Level: btui.NoticeLevelError,
					Text:  "terminal switched to observer-only mode",
				}},
			}
			_, _ = model.Update(feedback)
			_, _ = model.Update(feedback)
			if view := model.View(); !strings.Contains(stripANSIRuntimeView(view), "terminal switched to observer-only mode (x2)") || !strings.Contains(view, "notice_bar: total=1 | showing=1 | last=error | notices:") || !strings.Contains(view, "notice_group_bar: error=1") || !strings.Contains(view, "notices:") || !strings.Contains(view, "[error] terminal switched to observer-only mode (x2)") {
				t.Fatalf("expected runtime view to aggregate repeated notices, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioCtrlWOpensWorkspacePickerInView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			if view := model.View(); strings.Contains(view, "workspace_picker_rows:") {
				t.Fatalf("expected picker to be closed initially, got:\n%s", view)
			}
			nextModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
			current := nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay=workspace_picker") || !strings.Contains(view, "DIALOG[workspace_picker]") || !strings.Contains(view, "TREE[workspace]") || !strings.Contains(view, "TARGET[node]") || !strings.Contains(view, "F:q= sel=ws-1/tab-1/pane-1") || !strings.Contains(view, "rows=5") || !strings.Contains(view, "D:ws-1/tab-1/pane-1") || !strings.Contains(view, "kind=pane depth=2") || !strings.Contains(view, "label=unconnected pane") || !strings.Contains(view, "workspace_picker_rows:") || !strings.Contains(view, ">     [pane] unconnected pane") || !strings.Contains(view, "workspace_picker_selected: ws-1/tab-1/pane-1") || !strings.Contains(view, "workspace_picker_selected_kind: pane") || !strings.Contains(view, "workspace_picker_selected_label: unconnected pane") || !strings.Contains(view, "workspace_picker_selected_expanded: false") || !strings.Contains(view, "workspace_picker_selected_match: false") || !strings.Contains(view, "workspace_picker_selected_depth: 2") || !strings.Contains(view, "workspace_picker_row_count: 5") || !strings.Contains(view, "[workspace] ops") {
				t.Fatalf("expected ctrl-w to open picker in view, got:\n%s", view)
			}
			if view := current.View(); !strings.Contains(view, "focus_layer: overlay") || !strings.Contains(view, "focus_overlay_target: workspace_picker") || !strings.Contains(view, "tab_layer: tiled") {
				t.Fatalf("expected ctrl-w to expose overlay focus and preserved tab layer in view, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioWorkspacePickerBackspaceUpdatesQuery(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlW},
				{Type: tea.KeyRunes, Runes: []rune("ops")},
				{Type: tea.KeyBackspace},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "workspace_picker_query: op") || !strings.Contains(view, "workspace_picker_selected: ws-2") || !strings.Contains(view, "workspace_picker_selected_kind: workspace") || !strings.Contains(view, "workspace_picker_selected_label: ops") || !strings.Contains(view, "workspace_picker_row_count: 5") || !strings.Contains(view, "> [workspace] ops") {
				t.Fatalf("expected workspace picker backspace flow to update query and preserve match, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioWorkspacePickerSubmitJumpsToWorkspace(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlW},
				{Type: tea.KeyRunes, Runes: []rune("ops")},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "workspace: ops") || !strings.Contains(view, "tab: logs") || !strings.Contains(view, "pane: pane-2") || !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || strings.Contains(view, "workspace_picker_rows:") {
				t.Fatalf("expected workspace picker submit flow to jump and close overlay, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioWorkspacePickerMouseClickOnSelectedRowSubmits(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlW},
				{Type: tea.KeyRunes, Runes: []rune("ops")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "|          #|>> [workspace] ops")
			if clickY < 0 {
				t.Fatalf("expected workspace picker preview to expose selected ops row, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "workspace: ops") || !strings.Contains(view, "tab: logs") || !strings.Contains(view, "pane: pane-2") || !strings.Contains(view, "overlay: none") || strings.Contains(view, "workspace_picker_rows:") {
				t.Fatalf("expected workspace picker mouse click submit to jump and close overlay, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected workspace picker mouse click submit scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioWorkspacePickerMouseClickOnCreateRowOpensPrompt(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlW},
				{Type: tea.KeyRunes, Runes: []rune("ops")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [create] + create workspace")
			if clickY < 0 {
				t.Fatalf("expected workspace picker preview to expose create row, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: prompt") || !strings.Contains(view, "prompt_title: create workspace") || !strings.Contains(view, "focus_layer: prompt") || !strings.Contains(view, "focus_overlay_target: prompt") {
				t.Fatalf("expected workspace picker mouse click create row to open prompt, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected workspace picker mouse click create-row scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioWorkspacePickerSubmitAutoAcquiresOwnerOnEnter(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerAutoAcquireTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlW},
				{Type: tea.KeyRunes, Runes: []rune("ops")},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "workspace: ops") || !strings.Contains(view, "tab: logs") || !strings.Contains(view, "pane: pane-2") || !strings.Contains(view, "connection_role: owner") || !strings.Contains(view, "connected_panes: 2") {
				t.Fatalf("expected workspace jump auto-acquire to promote target pane owner, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected workspace auto-acquire scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioWorkspacePickerExpandShowsChildren(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlW},
				{Type: tea.KeyRunes, Runes: []rune("ops")},
				{Type: tea.KeyRight},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "workspace_picker_bar: selected=ws-2 | kind=workspace | depth=0") || !strings.Contains(view, "workspace_picker_query: ops") || !strings.Contains(view, "workspace_picker_selected: ws-2") || !strings.Contains(view, "workspace_picker_selected_kind: workspace") || !strings.Contains(view, "workspace_picker_selected_expanded: true") || !strings.Contains(view, "workspace_picker_selected_match: true") || !strings.Contains(view, "workspace_picker_selected_depth: 0") || !strings.Contains(view, "workspace_picker_row_count: 6") || !strings.Contains(view, "> [workspace] ops") || !strings.Contains(view, "  [tab] logs") {
				t.Fatalf("expected workspace picker expand flow to reveal children, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioPaneModeMovesFocusToAdjacentPane(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithSplitPaneTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlP},
				{Type: tea.KeyRunes, Runes: []rune("l")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "tab: shell") || !strings.Contains(view, "pane: pane-2") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "terminal: term-2") || !strings.Contains(view, "title: build-log") || strings.Contains(view, "mode:") {
				t.Fatalf("expected pane mode to move focus to adjacent pane-2, got:\n%s", view)
			}
			tab := current.State().Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")]
			if tab.ActivePaneID != types.PaneID("pane-2") {
				t.Fatalf("expected active pane to switch to pane-2, got %+v", tab)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected pane mode runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTabModeMovesFocusToAdjacentTab(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTwoTabTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlT},
				{Type: tea.KeyRunes, Runes: []rune("l")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "tab: logs") || !strings.Contains(view, "pane: pane-2") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "terminal: term-2") || !strings.Contains(view, "title: build-log") || strings.Contains(view, "mode:") {
				t.Fatalf("expected tab mode to move focus to adjacent tab-2, got:\n%s", view)
			}
			workspace := current.State().Domain.Workspaces[types.WorkspaceID("ws-1")]
			if workspace.ActiveTabID != types.TabID("tab-2") {
				t.Fatalf("expected active tab to switch to tab-2, got %+v", workspace.ActiveTabID)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected tab mode runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioGlobalSplitOpensLayoutResolveOnNewPane(t *testing.T) {
	client := &stubRunClient{}
	initial := connectedRunAppState()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("s")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "pane: pane-2") || !strings.Contains(view, "slot: waiting") || !strings.Contains(view, "overlay: layout_resolve") || !strings.Contains(view, "focus_overlay_target: layout_resolve") || !strings.Contains(view, "mode: picker") || !strings.Contains(view, "layout_resolve_bar: pane=pane-2") {
				t.Fatalf("expected global split flow to open layout resolve on new pane, got:\n%s", view)
			}
			tab := current.State().Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")]
			if tab.ActivePaneID != types.PaneID("pane-2") {
				t.Fatalf("expected split flow to activate pane-2, got %+v", tab)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected global split runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTabCreateOpensLayoutResolveOnNewTab(t *testing.T) {
	client := &stubRunClient{}
	initial := connectedRunAppState()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlT},
				{Type: tea.KeyRunes, Runes: []rune("n")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "tab: tab-2") || !strings.Contains(view, "pane: ws-1-tab-2-pane-1") || !strings.Contains(view, "slot: waiting") || !strings.Contains(view, "overlay: layout_resolve") || !strings.Contains(view, "focus_overlay_target: layout_resolve") || !strings.Contains(view, "layout_resolve_bar: pane=ws-1-tab-2-pane-1") {
				t.Fatalf("expected tab create flow to open layout resolve on new tab, got:\n%s", view)
			}
			workspace := current.State().Domain.Workspaces[types.WorkspaceID("ws-1")]
			if workspace.ActiveTabID != types.TabID("tab-2") {
				t.Fatalf("expected tab create flow to activate tab-2, got %+v", workspace.ActiveTabID)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected tab create runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTabCreateThenCreateTerminalConnectsNewTabPane(t *testing.T) {
	client := &stubRunClient{}
	initial := connectedRunAppState()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlT},
				{Type: tea.KeyRunes, Runes: []rune("n")},
				{Type: tea.KeyDown},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "tab: tab-2") || !strings.Contains(view, "pane: ws-1-tab-2-pane-1") || !strings.Contains(view, "slot: connected") || !strings.Contains(view, "terminal: term-created-1") || strings.Contains(view, "layout_resolve_rows:") {
				t.Fatalf("expected tab create-new flow to connect created terminal into new tab pane, got:\n%s", view)
			}
			tab := current.State().Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-2")]
			if tab.ActivePaneID != types.PaneID("ws-1-tab-2-pane-1") {
				t.Fatalf("expected new tab pane to stay active after create connect, got %+v", tab)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected tab create-new runtime scenario to succeed, got %v", err)
	}
	if len(client.createCalls) != 1 {
		t.Fatalf("expected one create call from tab create-new flow, got %d", len(client.createCalls))
	}
}

func TestE2ERunScenarioEmptyPaneNStartsAndConnectsTerminal(t *testing.T) {
	client := &stubRunClient{}
	initial := buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty)
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			nextModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
			current := nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "slot: connected") || !strings.Contains(view, "terminal: term-created-1") || !strings.Contains(view, "title: ws-1-tab-1-pane-1") {
				t.Fatalf("expected empty pane n to create and connect terminal, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected empty pane start-new scenario to succeed, got %v", err)
	}
	if len(client.createCalls) != 1 {
		t.Fatalf("expected one create call from empty pane n, got %d", len(client.createCalls))
	}
}

func TestE2ERunScenarioEmptyPaneAOpensTerminalPicker(t *testing.T) {
	client := &stubRunClient{}
	initial := buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty)
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			nextModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
			current := nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_picker") || !strings.Contains(view, "focus_overlay_target: terminal_picker") || !strings.Contains(view, "mode: picker") {
				t.Fatalf("expected empty pane a to open terminal picker, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected empty pane open-picker scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioRestartProgramExitedTerminal(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithExitedPaneTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			nextModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
			current := nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "slot: connected") || !strings.Contains(view, "terminal: term-created-1") || !strings.Contains(view, "title: deploy-log") || strings.Contains(view, "pane_slot_detail: terminal program exited") {
				t.Fatalf("expected restart to reconnect exited pane with new terminal, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected exited-pane restart scenario to succeed, got %v", err)
	}
	if len(client.createCalls) != 1 {
		t.Fatalf("expected one create call from exited-pane restart, got %d", len(client.createCalls))
	}
	if client.createCalls[0].name != "deploy-log" {
		t.Fatalf("expected restart to reuse terminal name, got %+v", client.createCalls[0])
	}
	if len(client.createCalls[0].command) != 3 || client.createCalls[0].command[0] != "npm" || client.createCalls[0].command[2] != "deploy" {
		t.Fatalf("expected restart to reuse terminal command, got %+v", client.createCalls[0])
	}
}

func TestE2ERunScenarioFloatingModeMovesFocusToAdjacentFloatingPane(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTwoFloatingTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlO},
				{Type: tea.KeyRunes, Runes: []rune("l")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "pane: float-2") || !strings.Contains(view, "focus_layer: floating") || !strings.Contains(view, "tab_layer: floating") || !strings.Contains(view, "terminal: term-2") || !strings.Contains(view, "title: build-log") || strings.Contains(view, "mode:") {
				t.Fatalf("expected floating mode to move focus to adjacent float-2, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected floating mode runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioFloatingModeCreateOpensLayoutResolve(t *testing.T) {
	client := &stubRunClient{}
	initial := connectedRunAppState()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlO},
				{Type: tea.KeyRunes, Runes: []rune("n")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "pane: float-1") || !strings.Contains(view, "pane_kind: floating") || !strings.Contains(view, "slot: waiting") || !strings.Contains(view, "overlay: layout_resolve") || !strings.Contains(view, "layout_resolve_bar: pane=float-1") {
				t.Fatalf("expected floating create flow to open layout resolve on float-1, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected floating create runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioFloatingModeCreateThenCreateTerminalConnectsFloatingPane(t *testing.T) {
	client := &stubRunClient{}
	initial := connectedRunAppState()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlO},
				{Type: tea.KeyRunes, Runes: []rune("n")},
				{Type: tea.KeyDown},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "pane: float-1") || !strings.Contains(view, "focus_layer: floating") || !strings.Contains(view, "tab_layer: floating") || !strings.Contains(view, "slot: connected") || !strings.Contains(view, "terminal: term-created-1") || strings.Contains(view, "layout_resolve_rows:") {
				t.Fatalf("expected floating create-new flow to connect created terminal into float-1, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected floating create-new runtime scenario to succeed, got %v", err)
	}
	if len(client.createCalls) != 1 {
		t.Fatalf("expected one create call from floating create-new flow, got %d", len(client.createCalls))
	}
}

func TestE2ERunScenarioFloatingModeMoveUpdatesRectInView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithFloatingPositionedPane()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlO},
				{Type: tea.KeyRunes, Runes: []rune("j")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "pane: float-1") || !strings.Contains(view, "focus_layer: floating") || !strings.Contains(view, "pane_rect: x=10 | y=10 | w=30 | h=12") || strings.Contains(view, "mode:") {
				t.Fatalf("expected floating move flow to update rect in view, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected floating move runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioFloatingModeCenterRecentersPaneInView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithFloatingPositionedPane()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlO},
				{Type: tea.KeyRunes, Runes: []rune("c")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "pane: float-1") || !strings.Contains(view, "pane_rect: x=45 | y=14 | w=30 | h=12") || strings.Contains(view, "mode:") {
				t.Fatalf("expected floating center flow to recenter pane in view, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected floating center runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioFloatingModeResizeUpdatesRectInView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithFloatingPositionedPane()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlO},
				{Type: tea.KeyRunes, Runes: []rune("L")},
				{Type: tea.KeyCtrlO},
				{Type: tea.KeyRunes, Runes: []rune("J")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "pane: float-1") || !strings.Contains(view, "pane_rect: x=10 | y=8 | w=32 | h=14") || strings.Contains(view, "mode:") {
				t.Fatalf("expected floating resize flow to update rect in view, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected floating resize runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioFloatingModeAdjustsZOrderAndUpdatesStackInView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTwoFloatingTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlO},
				{Type: tea.KeyRunes, Runes: []rune("]")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "pane: float-1") || !strings.Contains(view, "floating_stack: float-2 > float-1") || strings.Contains(view, "mode:") {
				t.Fatalf("expected floating z-order flow to update stack in view, got:\n%s", view)
			}
			tab := current.State().Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")]
			if !slices.Equal(tab.FloatingOrder, []types.PaneID{types.PaneID("float-2"), types.PaneID("float-1")}) {
				t.Fatalf("expected floating z-order to update tab stack, got %+v", tab.FloatingOrder)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected floating z-order runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioFloatingModeCloseRemovesPaneAndFallsBackToRemainingFloating(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTwoFloatingTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlO},
				{Type: tea.KeyRunes, Runes: []rune("x")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "pane: float-2") || !strings.Contains(view, "focus_layer: floating") || !strings.Contains(view, "floating_stack: float-2") || strings.Contains(view, "mode:") {
				t.Fatalf("expected floating close flow to remove pane and fall back to remaining floating pane, got:\n%s", view)
			}
			tab := current.State().Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")]
			if _, ok := tab.Panes[types.PaneID("float-1")]; ok {
				t.Fatalf("expected float-1 to be removed from pane set")
			}
			if !slices.Equal(tab.FloatingOrder, []types.PaneID{types.PaneID("float-2")}) {
				t.Fatalf("expected floating close to shrink stack, got %+v", tab.FloatingOrder)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected floating close runtime scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioWorkspacePickerCollapseHidesChildren(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlW},
				{Type: tea.KeyRunes, Runes: []rune("ops")},
				{Type: tea.KeyRight},
				{Type: tea.KeyLeft},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "workspace_picker_query: ops") || !strings.Contains(view, "workspace_picker_selected: ws-2") || !strings.Contains(view, "workspace_picker_selected_kind: workspace") || !strings.Contains(view, "workspace_picker_selected_expanded: false") || !strings.Contains(view, "workspace_picker_row_count: 5") || !strings.Contains(view, "> [workspace] ops") || strings.Contains(view, "  [tab] logs") {
				t.Fatalf("expected workspace picker collapse flow to hide children, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioWorkspacePickerCreateRowOpensPrompt(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlW},
				{Type: tea.KeyUp},
				{Type: tea.KeyUp},
				{Type: tea.KeyUp},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "prompt_bar: kind=create_workspace | active=draft") || !strings.Contains(view, "overlay: prompt") || !strings.Contains(view, "focus_layer: prompt") || !strings.Contains(view, "focus_overlay_target: prompt") || !strings.Contains(view, "prompt_title: create workspace") || !strings.Contains(view, "prompt_kind: create_workspace") || !strings.Contains(view, "prompt_active_field: draft") || !strings.Contains(view, "prompt_fields:") || !strings.Contains(view, "> [draft] ") {
				t.Fatalf("expected workspace picker create-row flow to open prompt, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioCreateWorkspacePromptSubmitCreatesWorkspace(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlW},
				{Type: tea.KeyUp},
				{Type: tea.KeyUp},
				{Type: tea.KeyUp},
				{Type: tea.KeyEnter},
				{Type: tea.KeyRunes, Runes: []rune("ops-cente")},
				{Type: tea.KeyBackspace},
				{Type: tea.KeyRunes, Runes: []rune("er")},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "workspace: ops-center") || !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || strings.Contains(view, "prompt_title: create workspace") {
				t.Fatalf("expected create workspace prompt submit flow to create workspace and close prompt, got:\n%s", view)
			}
			if current.State().Domain.ActiveWorkspaceID == types.WorkspaceID("ws-1") {
				t.Fatalf("expected active workspace to switch after prompt submit, got %+v", current.State().Domain.ActiveWorkspaceID)
			}
			workspace := current.State().Domain.Workspaces[current.State().Domain.ActiveWorkspaceID]
			if workspace.Name != "ops-center" {
				t.Fatalf("expected workspace created from prompt draft, got %+v", workspace)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioCreateWorkspacePromptMouseClickOnSubmitCreatesWorkspace(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlW},
				{Type: tea.KeyUp},
				{Type: tea.KeyUp},
				{Type: tea.KeyUp},
				{Type: tea.KeyEnter},
				{Type: tea.KeyRunes, Runes: []rune("ops-cente")},
				{Type: tea.KeyBackspace},
				{Type: tea.KeyRunes, Runes: []rune("er")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [submit] submit")
			if clickY < 0 {
				t.Fatalf("expected create workspace prompt to expose submit action, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "workspace: ops-center") || !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || strings.Contains(view, "prompt_title: create workspace") {
				t.Fatalf("expected create workspace prompt mouse submit flow to create workspace and close prompt, got:\n%s", view)
			}
			if current.State().Domain.ActiveWorkspaceID == types.WorkspaceID("ws-1") {
				t.Fatalf("expected active workspace to switch after prompt mouse submit, got %+v", current.State().Domain.ActiveWorkspaceID)
			}
			workspace := current.State().Domain.Workspaces[current.State().Domain.ActiveWorkspaceID]
			if workspace.Name != "ops-center" {
				t.Fatalf("expected workspace created from prompt draft, got %+v", workspace)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioCreateWorkspacePromptEscCancels(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlW},
				{Type: tea.KeyUp},
				{Type: tea.KeyUp},
				{Type: tea.KeyUp},
				{Type: tea.KeyEnter},
				{Type: tea.KeyEsc},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "workspace: main") || strings.Contains(view, "prompt_title: create workspace") || strings.Contains(view, "focus_overlay_target:") {
				t.Fatalf("expected create workspace prompt esc flow to cancel and restore pane, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioCreateWorkspacePromptMouseClickOnCancelCancels(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlW},
				{Type: tea.KeyUp},
				{Type: tea.KeyUp},
				{Type: tea.KeyUp},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [cancel] cancel")
			if clickY < 0 {
				t.Fatalf("expected create workspace prompt to expose cancel action, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "workspace: main") || strings.Contains(view, "prompt_title: create workspace") || strings.Contains(view, "focus_overlay_target:") {
				t.Fatalf("expected create workspace prompt mouse cancel flow to restore pane, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioWorkspacePickerEscClosesOverlay(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithWorkspacePickerTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlW},
				{Type: tea.KeyEsc},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || strings.Contains(view, "workspace_picker_rows:") || strings.Contains(view, "focus_overlay_target:") {
				t.Fatalf("expected workspace picker esc flow to close overlay, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioCtrlGTOpensTerminalManagerInView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
			} {
				nextModel, cmd := model.Update(key)
				model = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = model.Update(msg)
						model = nextModel.(*btui.Model)
					}
				}
			}
			if view := model.View(); !strings.Contains(view, "terminal_manager_bar: selected=term-1 | section=VISIBLE | kind=terminal") || !strings.Contains(view, "overlay: terminal_manager") || !strings.Contains(view, "DIALOG[terminal_manager]") || !strings.Contains(view, "LIST[terminals]") || !strings.Contains(view, "DETAIL[terminal]") || !strings.Contains(view, "F:VISIBLE api-dev") || !strings.Contains(view, "rows=7 sel=term-1 q=") || !strings.Contains(view, ">> [terminal] api-dev") || !strings.Contains(view, "D:api-dev term-1") || !strings.Contains(view, "state=running vis=visible") || !strings.Contains(view, "owner=pane:pane-1") || !strings.Contains(view, "conn=1 loc=1") || !strings.Contains(view, "BODY[command]") || !strings.Contains(view, "npm run dev") || !strings.Contains(view, "focus_layer: overlay") || !strings.Contains(view, "focus_overlay_target: terminal_manager") || !strings.Contains(view, "terminal_manager_query: ") || !strings.Contains(view, "terminal_manager_selected: term-1") || !strings.Contains(view, "terminal_manager_selected_label: api-dev") || !strings.Contains(view, "terminal_manager_selected_kind: terminal") || !strings.Contains(view, "terminal_manager_selected_section: VISIBLE") || !strings.Contains(view, "terminal_manager_selected_state: running") || !strings.Contains(view, "terminal_manager_selected_visible: true") || !strings.Contains(view, "terminal_manager_selected_visibility: visible") || !strings.Contains(view, "terminal_manager_selected_connected_panes: 1") || !strings.Contains(view, "terminal_manager_selected_location_count: 1") || !strings.Contains(view, "terminal_manager_selected_command: npm run dev") || !strings.Contains(view, "terminal_manager_selected_owner: pane:pane-1") || !strings.Contains(view, "terminal_manager_row_count: 7") || !strings.Contains(view, "terminal_manager_rows:") || !strings.Contains(view, "terminal_manager_detail: api-dev") || !strings.Contains(view, "detail_terminal: term-1") || !strings.Contains(view, "detail_state: running") || !strings.Contains(view, "detail_visible: true") || !strings.Contains(view, "detail_visibility: visible") || !strings.Contains(view, "detail_connected_panes: 1") || !strings.Contains(view, "detail_location_count: 1") || !strings.Contains(view, "detail_command: npm run dev") || !strings.Contains(view, "detail_owner: pane:pane-1") || !strings.Contains(view, "detail_locations:") || !strings.Contains(view, "detail_locations_rendered: 1") || !strings.Contains(view, "  [location] main/shell/pane:pane-1") {
				t.Fatalf("expected ctrl-g t flow to render terminal manager, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerMoveShowsSelectedTags(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
			} {
				nextModel, cmd := model.Update(key)
				model = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = model.Update(msg)
						model = nextModel.(*btui.Model)
					}
				}
			}
			if view := model.View(); !strings.Contains(view, "terminal_manager_query: ") || !strings.Contains(view, "terminal_manager_selected: term-2") || !strings.Contains(view, "terminal_manager_selected_label: build-log") || !strings.Contains(view, "terminal_manager_selected_kind: terminal") || !strings.Contains(view, "terminal_manager_selected_section: PARKED") || !strings.Contains(view, "terminal_manager_selected_state: running") || !strings.Contains(view, "terminal_manager_selected_visible: false") || !strings.Contains(view, "terminal_manager_selected_visibility: hidden") || !strings.Contains(view, "terminal_manager_selected_connected_panes: 0") || !strings.Contains(view, "terminal_manager_selected_location_count: 0") || !strings.Contains(view, "terminal_manager_selected_command: tail -f build.log") || !strings.Contains(view, "terminal_manager_selected_owner: ") || !strings.Contains(view, "terminal_manager_selected_tags: group=build") || !strings.Contains(view, "terminal_manager_row_count: 7") || !strings.Contains(view, "terminal_manager_rows:") || !strings.Contains(view, "> [terminal] build-log") || !strings.Contains(view, "terminal_manager_detail: build-log") || !strings.Contains(view, "detail_terminal: term-2") || !strings.Contains(view, "detail_state: running") || !strings.Contains(view, "detail_visible: false") || !strings.Contains(view, "detail_visibility: hidden") || !strings.Contains(view, "detail_connected_panes: 0") || !strings.Contains(view, "detail_location_count: 0") || !strings.Contains(view, "detail_command: tail -f build.log") || !strings.Contains(view, "detail_owner: ") || !strings.Contains(view, "detail_tags: group=build") {
				t.Fatalf("expected terminal manager move flow to render selected tags, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerMouseWheelMovesSelection(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
			} {
				nextModel, cmd := model.Update(key)
				model = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = model.Update(msg)
						model = nextModel.(*btui.Model)
					}
				}
			}

			nextModel, cmd := model.Update(tea.MouseMsg{
				Button: tea.MouseButtonWheelDown,
				Action: tea.MouseActionPress,
			})
			model = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = model.Update(msg)
					model = nextModel.(*btui.Model)
				}
			}

			if view := model.View(); !strings.Contains(view, "terminal_manager_selected: term-2") || !strings.Contains(view, "terminal_manager_selected_tags: group=build") || !strings.Contains(view, "> [terminal] build-log") {
				t.Fatalf("expected terminal manager mouse wheel flow to move selection, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerMouseClickSelectsVisibleTerminal(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
			} {
				nextModel, cmd := model.Update(key)
				model = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = model.Update(msg)
						model = nextModel.(*btui.Model)
					}
				}
			}

			clickY := findLineIndexWithPrefix(model.View(), "  [terminal] api-dev")
			if clickY < 0 {
				t.Fatalf("expected terminal manager preview to expose api-dev row, got:\n%s", model.View())
			}

			nextModel, cmd := model.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			model = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = model.Update(msg)
					model = nextModel.(*btui.Model)
				}
			}

			if view := model.View(); !strings.Contains(view, "terminal_manager_selected: term-1") || !strings.Contains(view, "terminal_manager_selected_label: api-dev") || !strings.Contains(view, "> [terminal] api-dev") {
				t.Fatalf("expected terminal manager mouse click flow to select visible terminal, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerSearchUpdatesView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyRunes, Runes: []rune("ops")},
			} {
				nextModel, cmd := model.Update(key)
				model = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = model.Update(msg)
						model = nextModel.(*btui.Model)
					}
				}
			}
			if view := model.View(); !strings.Contains(view, "terminal_manager_query: ops") || !strings.Contains(view, "terminal_manager_selected: term-3") || !strings.Contains(view, "terminal_manager_selected_label: ops-watch") || !strings.Contains(view, "terminal_manager_selected_kind: terminal") || !strings.Contains(view, "terminal_manager_selected_section: PARKED") || !strings.Contains(view, "terminal_manager_selected_state: running") || !strings.Contains(view, "terminal_manager_selected_visible: false") || !strings.Contains(view, "terminal_manager_selected_visibility: hidden") || !strings.Contains(view, "terminal_manager_selected_connected_panes: 0") || !strings.Contains(view, "terminal_manager_selected_location_count: 0") || !strings.Contains(view, "terminal_manager_selected_command: journalctl -f") || !strings.Contains(view, "terminal_manager_selected_owner: ") || !strings.Contains(view, "terminal_manager_selected_tags: team=ops") || !strings.Contains(view, "terminal_manager_row_count: 4") || !strings.Contains(view, "terminal_manager_rows:") || !strings.Contains(view, "> [terminal] ops-watch") || !strings.Contains(view, "terminal_manager_detail: ops-watch") || !strings.Contains(view, "detail_terminal: term-3") || !strings.Contains(view, "detail_state: running") || !strings.Contains(view, "detail_visible: false") || !strings.Contains(view, "detail_visibility: hidden") || !strings.Contains(view, "detail_connected_panes: 0") || !strings.Contains(view, "detail_location_count: 0") || !strings.Contains(view, "detail_command: journalctl -f") || !strings.Contains(view, "detail_owner: ") || !strings.Contains(view, "detail_tags: team=ops") {
				t.Fatalf("expected terminal manager search flow to render filtered selection, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerBackspaceUpdatesQuery(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyRunes, Runes: []rune("ops")},
				{Type: tea.KeyBackspace},
			} {
				nextModel, cmd := model.Update(key)
				model = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = model.Update(msg)
						model = nextModel.(*btui.Model)
					}
				}
			}
			if view := model.View(); !strings.Contains(view, "terminal_manager_query: op") || !strings.Contains(view, "terminal_manager_selected: term-3") || !strings.Contains(view, "terminal_manager_selected_label: ops-watch") || !strings.Contains(view, "terminal_manager_row_count: 4") || !strings.Contains(view, "> [terminal] ops-watch") {
				t.Fatalf("expected terminal manager backspace flow to update query and preserve match, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerEditOpensPromptInView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyRunes, Runes: []rune("e")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: prompt") || !strings.Contains(view, "DIALOG[prompt]") || !strings.Contains(view, "FIELDS[prompt]") || !strings.Contains(view, "ACTIVE[field]") || !strings.Contains(view, "count=2 f=name") || !strings.Contains(view, "active=name") || !strings.Contains(view, ">> [name] Name: api-dev") || !strings.Contains(view, "D:name") || !strings.Contains(view, "label=Name") || !strings.Contains(view, "terminal=term-1") || !strings.Contains(view, "BODY[actions]") || !strings.Contains(view, "submit | cancel") || !strings.Contains(view, "prompt_title: edit terminal metadata") || !strings.Contains(view, "prompt_kind: edit_terminal_metadata") || !strings.Contains(view, "prompt_terminal: term-1") || !strings.Contains(view, "prompt_active_field: name") || !strings.Contains(view, "prompt_active_label: Name") || !strings.Contains(view, "prompt_active_value: api-dev") || !strings.Contains(view, "prompt_active_index: 0") || !strings.Contains(view, "prompt_field_count: 2") || !strings.Contains(view, "prompt_fields:") || !strings.Contains(view, "  [tags] Tags: ") {
				t.Fatalf("expected terminal manager edit flow to render prompt, got:\n%s", view)
			}
			if view := current.View(); !strings.Contains(view, "focus_layer: prompt") || !strings.Contains(view, "focus_overlay_target: prompt") {
				t.Fatalf("expected prompt flow to expose focus state in view, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioFollowerTerminalManagerEditShowsOwnerNotice(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithFollowerActivePane()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyRunes, Runes: []rune("e")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_manager") || !strings.Contains(view, "notices:") || !strings.Contains(view, "acquire owner first") || strings.Contains(view, "prompt_title: edit terminal metadata") {
				t.Fatalf("expected follower edit metadata to stay in manager and show owner notice, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected follower metadata gating scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioFollowerAcquireOwnerThenEditOpensPrompt(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithFollowerActivePane()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyRunes, Runes: []rune("a")},
				{Type: tea.KeyRunes, Runes: []rune("e")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: prompt") || !strings.Contains(view, "prompt_title: edit terminal metadata") || !strings.Contains(view, "prompt_terminal: term-1") || !strings.Contains(view, "focus_layer: prompt") {
				t.Fatalf("expected acquire-owner then edit flow to open metadata prompt, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected acquire owner then edit scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioMetadataPromptTabShowsTagsFieldInView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
				{Type: tea.KeyRunes, Runes: []rune("e")},
				{Type: tea.KeyTab},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: prompt") || !strings.Contains(view, "prompt_terminal: term-2") || !strings.Contains(view, "prompt_active_field: tags") || !strings.Contains(view, "prompt_active_label: Tags") || !strings.Contains(view, "prompt_active_value: group=build") || !strings.Contains(view, "prompt_active_index: 1") || !strings.Contains(view, "  [name] Name: build-log") || !strings.Contains(view, "> [tags] Tags: group=build") {
				t.Fatalf("expected metadata prompt tab flow to focus tags field in view, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioMetadataPromptMouseClickShowsTagsFieldInView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
				{Type: tea.KeyRunes, Runes: []rune("e")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [tags] Tags: group=build")
			if clickY < 0 {
				t.Fatalf("expected metadata prompt to expose tags field row, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: prompt") || !strings.Contains(view, "prompt_terminal: term-2") || !strings.Contains(view, "prompt_active_field: tags") || !strings.Contains(view, "prompt_active_label: Tags") || !strings.Contains(view, "prompt_active_value: group=build") || !strings.Contains(view, "prompt_active_index: 1") || !strings.Contains(view, "  [name] Name: build-log") || !strings.Contains(view, "> [tags] Tags: group=build") {
				t.Fatalf("expected metadata prompt mouse click flow to focus tags field in view, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected metadata prompt mouse click scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioMetadataPromptTabSubmitUpdatesTerminal(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
				{Type: tea.KeyRunes, Runes: []rune("e")},
				{Type: tea.KeyRunes, Runes: []rune("-v2")},
				{Type: tea.KeyTab},
				{Type: tea.KeyRunes, Runes: []rune(",env=prod")},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || strings.Contains(view, "prompt_title: edit terminal metadata") {
				t.Fatalf("expected metadata prompt submit flow to close prompt, got:\n%s", view)
			}
			terminal := current.State().Domain.Terminals[types.TerminalID("term-2")]
			if terminal.Name != "build-log-v2" {
				t.Fatalf("expected metadata prompt to update terminal name, got %+v", terminal)
			}
			if terminal.Tags["group"] != "build" || terminal.Tags["env"] != "prod" {
				t.Fatalf("expected metadata prompt to update terminal tags, got %+v", terminal.Tags)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.metadataCalls) != 1 {
		t.Fatalf("expected one metadata call, got %d", len(client.metadataCalls))
	}
	if client.metadataCalls[0].terminalID != "term-2" || client.metadataCalls[0].name != "build-log-v2" {
		t.Fatalf("unexpected metadata call payload: %+v", client.metadataCalls[0])
	}
	if client.metadataCalls[0].tags["group"] != "build" || client.metadataCalls[0].tags["env"] != "prod" {
		t.Fatalf("unexpected metadata call tags: %+v", client.metadataCalls[0].tags)
	}
}

func TestE2ERunScenarioMetadataPromptMouseClickOnSubmitUpdatesTerminal(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
				{Type: tea.KeyRunes, Runes: []rune("e")},
				{Type: tea.KeyRunes, Runes: []rune("-v2")},
				{Type: tea.KeyTab},
				{Type: tea.KeyRunes, Runes: []rune(",env=prod")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [submit] submit")
			if clickY < 0 {
				t.Fatalf("expected metadata prompt to expose submit action, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || strings.Contains(view, "prompt_title: edit terminal metadata") {
				t.Fatalf("expected metadata prompt mouse submit flow to close prompt, got:\n%s", view)
			}
			terminal := current.State().Domain.Terminals[types.TerminalID("term-2")]
			if terminal.Name != "build-log-v2" {
				t.Fatalf("expected metadata prompt to update terminal name, got %+v", terminal)
			}
			if terminal.Tags["group"] != "build" || terminal.Tags["env"] != "prod" {
				t.Fatalf("expected metadata prompt to update terminal tags, got %+v", terminal.Tags)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.metadataCalls) != 1 {
		t.Fatalf("expected one metadata call, got %d", len(client.metadataCalls))
	}
	if client.metadataCalls[0].terminalID != "term-2" || client.metadataCalls[0].name != "build-log-v2" {
		t.Fatalf("unexpected metadata call payload: %+v", client.metadataCalls[0])
	}
	if client.metadataCalls[0].tags["group"] != "build" || client.metadataCalls[0].tags["env"] != "prod" {
		t.Fatalf("unexpected metadata call tags: %+v", client.metadataCalls[0].tags)
	}
}

func TestE2ERunScenarioMetadataPromptMouseClickOnCancelClosesPrompt(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
				{Type: tea.KeyRunes, Runes: []rune("e")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [cancel] cancel")
			if clickY < 0 {
				t.Fatalf("expected metadata prompt to expose cancel action, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || strings.Contains(view, "prompt_title: edit terminal metadata") || strings.Contains(view, "focus_overlay_target:") {
				t.Fatalf("expected metadata prompt mouse cancel flow to close prompt, got:\n%s", view)
			}
			if len(client.metadataCalls) != 0 {
				t.Fatalf("expected mouse cancel to avoid metadata call, got %d", len(client.metadataCalls))
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioMetadataPromptSubmitFailureShowsNoticeInView(t *testing.T) {
	client := &stubRunClient{metadataErr: errRuntimeEffectBoom}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
				{Type: tea.KeyRunes, Runes: []rune("e")},
				{Type: tea.KeyRunes, Runes: []rune("-v2")},
				{Type: tea.KeyTab},
				{Type: tea.KeyRunes, Runes: []rune(",env=prod")},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: prompt") || !strings.Contains(view, "prompt_title: edit terminal metadata") || !strings.Contains(view, "notices:") || !strings.Contains(view, "runtime effect boom") {
				t.Fatalf("expected metadata failure to surface notice in runtime view, got:\n%s", view)
			}
			terminal := current.State().Domain.Terminals[types.TerminalID("term-2")]
			if terminal.Name != "build-log" || terminal.Tags["group"] != "build" || len(terminal.Tags) != 1 {
				t.Fatalf("expected metadata failure to keep terminal unchanged, got %+v", terminal)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.metadataCalls) != 1 {
		t.Fatalf("expected one metadata call despite failure, got %d", len(client.metadataCalls))
	}
}

func TestE2ERunScenarioTerminalManagerStopClosesOverlayAndClearsPane(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyRunes, Runes: []rune("k")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "slot: empty") || strings.Contains(view, "terminal_manager_rows:") {
				t.Fatalf("expected terminal manager stop flow to close overlay and clear pane, got:\n%s", view)
			}
			pane := current.State().Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
			if pane.TerminalID != "" || pane.SlotState != types.PaneSlotEmpty {
				t.Fatalf("expected stop flow to clear active pane terminal, got %+v", pane)
			}
			terminal := current.State().Domain.Terminals[types.TerminalID("term-1")]
			if terminal.State != types.TerminalRunStateStopped {
				t.Fatalf("expected stop flow to mark terminal stopped, got %+v", terminal)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.killCalls) != 1 || client.killCalls[0] != "term-1" {
		t.Fatalf("expected one kill call for term-1, got %+v", client.killCalls)
	}
}

func TestE2ERunScenarioFollowerTerminalManagerStopShowsOwnerNotice(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithFollowerActivePane()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyRunes, Runes: []rune("k")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_manager") || !strings.Contains(view, "notices:") || !strings.Contains(view, "stop terminal requires owner; acquire owner first") {
				t.Fatalf("expected follower stop to stay in manager and show owner notice, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected follower stop gating scenario to succeed, got %v", err)
	}
	if len(client.killCalls) != 0 {
		t.Fatalf("expected no kill call without owner, got %+v", client.killCalls)
	}
}

func TestE2ERunScenarioFollowerAcquireOwnerThenStopSharedTerminal(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithFollowerActivePane()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyRunes, Runes: []rune("a")},
				{Type: tea.KeyRunes, Runes: []rune("k")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "slot: empty") || strings.Contains(view, "terminal_manager_rows:") {
				t.Fatalf("expected acquire owner then stop flow to close overlay and clear active pane, got:\n%s", view)
			}
			state := current.State()
			if _, ok := state.Domain.Connections[types.TerminalID("term-1")]; ok {
				t.Fatalf("expected shared terminal connection to be removed after stop")
			}
			ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
			tab := ws.Tabs[types.TabID("tab-1")]
			if tab.Panes[types.PaneID("pane-1")].SlotState != types.PaneSlotEmpty || tab.Panes[types.PaneID("pane-2")].SlotState != types.PaneSlotEmpty {
				t.Fatalf("expected all connected panes to become empty after stop, got pane-1=%+v pane-2=%+v", tab.Panes[types.PaneID("pane-1")], tab.Panes[types.PaneID("pane-2")])
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected acquire owner then stop scenario to succeed, got %v", err)
	}
	if len(client.killCalls) != 1 || client.killCalls[0] != "term-1" {
		t.Fatalf("expected one kill call for shared terminal after owner acquire, got %+v", client.killCalls)
	}
}

func TestE2ERunScenarioTerminalManagerStopFailureShowsNoticeInView(t *testing.T) {
	client := &stubRunClient{killErr: errRuntimeEffectBoom}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyRunes, Runes: []rune("k")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_manager") || !strings.Contains(view, "terminal_manager_rows:") || !strings.Contains(view, "notices:") || !strings.Contains(view, "runtime effect boom") {
				t.Fatalf("expected failed stop to keep terminal manager open and surface notice, got:\n%s", view)
			}
			pane := current.State().Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
			if pane.TerminalID != types.TerminalID("term-1") || pane.SlotState != types.PaneSlotConnected {
				t.Fatalf("expected failed stop to keep pane connected, got %+v", pane)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.killCalls) != 1 || client.killCalls[0] != "term-1" {
		t.Fatalf("expected one failed kill call for term-1, got %+v", client.killCalls)
	}
}

func TestE2ERunScenarioTerminalManagerConnectHereClosesOverlayAndSwitchesPane(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "terminal: term-2") || !strings.Contains(view, "title: build-log") || !strings.Contains(view, "terminal_command: tail -f build.log") || !strings.Contains(view, "terminal_visibility: true") || strings.Contains(view, "terminal_manager_rows:") {
				t.Fatalf("expected terminal manager connect-here flow to close overlay and switch pane terminal, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerMouseClickOnSelectedRowConnectsHere(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "> [terminal] build-log")
			if clickY < 0 {
				t.Fatalf("expected terminal manager preview to expose selected build-log row, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "terminal: term-2") || !strings.Contains(view, "title: build-log") || strings.Contains(view, "terminal_manager_rows:") {
				t.Fatalf("expected terminal manager mouse click submit to connect here, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected terminal manager mouse click submit scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerConnectInNewTabCreatesTab(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
				{Type: tea.KeyRunes, Runes: []rune("t")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "tab: tab-2") || !strings.Contains(view, "terminal: term-2") || !strings.Contains(view, "title: build-log") || !strings.Contains(view, "terminal_visibility: true") || strings.Contains(view, "terminal_manager_rows:") {
				t.Fatalf("expected terminal manager new-tab flow to create and focus new tab, got:\n%s", view)
			}
			workspace := current.State().Domain.Workspaces[types.WorkspaceID("ws-1")]
			if workspace.ActiveTabID != types.TabID("tab-2") {
				t.Fatalf("expected runtime flow to switch active tab, got %+v", workspace.ActiveTabID)
			}
			tab := workspace.Tabs[types.TabID("tab-2")]
			if tab.ActivePaneID != types.PaneID("ws-1-tab-2-pane-1") || tab.ActiveLayer != types.FocusLayerTiled {
				t.Fatalf("expected runtime flow to create active pane in new tab, got %+v", tab)
			}
			pane := tab.Panes[types.PaneID("ws-1-tab-2-pane-1")]
			if pane.Kind != types.PaneKindTiled || pane.TerminalID != types.TerminalID("term-2") || pane.SlotState != types.PaneSlotConnected {
				t.Fatalf("expected runtime flow to connect term-2 in new tab pane, got %+v", pane)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.newTabCalls) != 1 {
		t.Fatalf("expected one new-tab call, got %d", len(client.newTabCalls))
	}
	if client.newTabCalls[0].workspaceID != types.WorkspaceID("ws-1") || client.newTabCalls[0].terminalID != types.TerminalID("term-2") {
		t.Fatalf("unexpected new-tab call payload: %+v", client.newTabCalls[0])
	}
}

func TestE2ERunScenarioTerminalManagerConnectInNewTabFailureShowsNoticeInView(t *testing.T) {
	client := &stubRunClient{newTabErr: errRuntimeEffectBoom}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
				{Type: tea.KeyRunes, Runes: []rune("t")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_manager") || !strings.Contains(view, "terminal_manager_rows:") || !strings.Contains(view, "notices:") || !strings.Contains(view, "runtime effect boom") {
				t.Fatalf("expected new-tab failure to surface notice in runtime view, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.newTabCalls) != 1 {
		t.Fatalf("expected one failed new-tab call, got %d", len(client.newTabCalls))
	}
}

func TestE2ERunScenarioTerminalManagerConnectInFloatingPaneCreatesFloatingPane(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
				{Type: tea.KeyRunes, Runes: []rune("o")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: floating") || !strings.Contains(view, "tab_layer: floating") || !strings.Contains(view, "pane_kind: floating") || !strings.Contains(view, "terminal: term-2") || !strings.Contains(view, "title: build-log") || !strings.Contains(view, "terminal_visibility: true") || strings.Contains(view, "terminal_manager_rows:") {
				t.Fatalf("expected terminal manager floating flow to create focused floating pane, got:\n%s", view)
			}
			tab := current.State().Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")]
			if tab.ActivePaneID != types.PaneID("float-1") || tab.ActiveLayer != types.FocusLayerFloating {
				t.Fatalf("expected active tab to switch to new floating pane, got %+v", tab)
			}
			pane := tab.Panes[types.PaneID("float-1")]
			if pane.Kind != types.PaneKindFloating || pane.TerminalID != types.TerminalID("term-2") || pane.SlotState != types.PaneSlotConnected {
				t.Fatalf("expected new connected floating pane, got %+v", pane)
			}
			conn := current.State().Domain.Connections[types.TerminalID("term-2")]
			if len(conn.ConnectedPaneIDs) != 1 || conn.ConnectedPaneIDs[0] != types.PaneID("float-1") || conn.OwnerPaneID != types.PaneID("float-1") {
				t.Fatalf("expected term-2 connection to target floating pane, got %+v", conn)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.floatingCalls) != 1 {
		t.Fatalf("expected one floating-pane call, got %d", len(client.floatingCalls))
	}
	if client.floatingCalls[0].workspaceID != types.WorkspaceID("ws-1") || client.floatingCalls[0].tabID != types.TabID("tab-1") || client.floatingCalls[0].terminalID != types.TerminalID("term-2") {
		t.Fatalf("unexpected floating-pane call payload: %+v", client.floatingCalls[0])
	}
}

func TestE2ERunScenarioTerminalManagerConnectInFloatingPaneFailureShowsNoticeInView(t *testing.T) {
	client := &stubRunClient{floatingErr: errRuntimeEffectBoom}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
				{Type: tea.KeyRunes, Runes: []rune("o")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_manager") || !strings.Contains(view, "terminal_manager_rows:") || !strings.Contains(view, "notices:") || !strings.Contains(view, "runtime effect boom") {
				t.Fatalf("expected floating-pane failure to surface notice in runtime view, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.floatingCalls) != 1 {
		t.Fatalf("expected one failed floating-pane call, got %d", len(client.floatingCalls))
	}
}

func TestE2ERunScenarioTerminalManagerJumpToConnectedPaneSwitchesWorkspaceAndFocus(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerJumpTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
				{Type: tea.KeyRunes, Runes: []rune("j")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "workspace: ops") || !strings.Contains(view, "tab: logs") || !strings.Contains(view, "pane: pane-remote") || !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "terminal: term-2") || !strings.Contains(view, "title: build-log") || strings.Contains(view, "terminal_manager_rows:") {
				t.Fatalf("expected terminal manager jump flow to switch to connected pane, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerJumpWithoutConnectedPaneShowsNoticeInView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
				{Type: tea.KeyRunes, Runes: []rune("j")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_manager") || !strings.Contains(view, "terminal_manager_rows:") || !strings.Contains(view, "notices:") || !strings.Contains(view, "selected terminal has no connected pane") {
				t.Fatalf("expected terminal manager jump failure to keep overlay and show notice, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerMouseClickOnFloatingActionCreatesFloatingPane(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [floating] open in floating pane")
			if clickY < 0 {
				t.Fatalf("expected terminal manager to expose floating action row, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: floating") || !strings.Contains(view, "pane_kind: floating") || !strings.Contains(view, "terminal: term-2") || !strings.Contains(view, "terminal_visibility: true") || strings.Contains(view, "terminal_manager_rows:") {
				t.Fatalf("expected floating mouse action to create focused floating pane, got:\n%s", view)
			}
			tab := current.State().Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")]
			if tab.ActivePaneID != types.PaneID("float-1") || tab.ActiveLayer != types.FocusLayerFloating {
				t.Fatalf("expected mouse floating action to switch to new floating pane, got %+v", tab)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.floatingCalls) != 1 {
		t.Fatalf("expected one floating-pane call, got %d", len(client.floatingCalls))
	}
	if client.floatingCalls[0].workspaceID != types.WorkspaceID("ws-1") || client.floatingCalls[0].tabID != types.TabID("tab-1") || client.floatingCalls[0].terminalID != types.TerminalID("term-2") {
		t.Fatalf("unexpected floating-pane call payload: %+v", client.floatingCalls[0])
	}
}

func TestE2ERunScenarioTerminalManagerMouseClickOnJumpActionSwitchesToConnectedPane(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerJumpTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [jump] jump to connected pane")
			if clickY < 0 {
				t.Fatalf("expected terminal manager to expose jump action row, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "workspace: ops") || !strings.Contains(view, "tab: logs") || !strings.Contains(view, "pane: pane-remote") || !strings.Contains(view, "overlay: none") || !strings.Contains(view, "terminal: term-2") || strings.Contains(view, "terminal_manager_actions:") {
				t.Fatalf("expected terminal manager jump mouse action to switch focus, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerMouseClickOnDetailLocationJumpsToExactPane(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerLocationTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [location] ops/logs/float:float-ops")
			if clickY < 0 {
				t.Fatalf("expected terminal manager detail to expose exact location row, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "workspace: ops") || !strings.Contains(view, "tab: logs") || !strings.Contains(view, "pane: float-ops") || !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: floating") || !strings.Contains(view, "tab_layer: floating") || !strings.Contains(view, "terminal: term-1") || strings.Contains(view, "terminal_manager_rows:") {
				t.Fatalf("expected detail location click to jump to exact floating pane, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerEscClosesOverlay(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyEsc},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || strings.Contains(view, "terminal_manager_rows:") || strings.Contains(view, "focus_overlay_target:") || strings.Contains(view, "mode:") {
				t.Fatalf("expected terminal manager esc flow to close overlay, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerCreateRowSubmitClosesOverlay(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			nextModel, cmd := current.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyUp},
			} {
				nextModel, cmd = current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_manager") || !strings.Contains(view, "focus_layer: overlay") || !strings.Contains(view, "focus_overlay_target: terminal_manager") || !strings.Contains(view, "terminal_manager_query: ") || !strings.Contains(view, "terminal_manager_row_count: 7") || !strings.Contains(view, "terminal_manager_rows:") || !strings.Contains(view, "> [create] + new terminal") || strings.Contains(view, "terminal_manager_detail:") {
				t.Fatalf("expected create row selection in terminal manager view, got:\n%s", view)
			}
			nextModel, cmd = current.Update(tea.KeyMsg{Type: tea.KeyEnter})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "slot: connected") || !strings.Contains(view, "terminal: term-created-1") || strings.Contains(view, "terminal_manager_rows:") {
				t.Fatalf("expected create row submit to close overlay, got:\n%s", view)
			}
			if created, ok := current.State().Domain.Terminals[types.TerminalID("term-created-1")]; !ok || created.Name == "" || created.State != types.TerminalRunStateRunning || !created.Visible {
				t.Fatalf("expected create row success to connect visible terminal, got %+v ok=%v", created, ok)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.createCalls) != 1 {
		t.Fatalf("expected one create call, got %d", len(client.createCalls))
	}
	if client.createCalls[0].name == "" || len(client.createCalls[0].command) == 0 {
		t.Fatalf("expected create call to include default name and command, got %+v", client.createCalls[0])
	}
}

func TestE2ERunScenarioTerminalManagerMouseClickOnCreateRowClosesOverlay(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [create] + new terminal")
			if clickY < 0 {
				t.Fatalf("expected terminal manager preview to expose create row, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "slot: connected") || !strings.Contains(view, "terminal: term-created-1") || strings.Contains(view, "terminal_manager_rows:") {
				t.Fatalf("expected terminal manager mouse click create row to close overlay, got:\n%s", view)
			}
			if created, ok := current.State().Domain.Terminals[types.TerminalID("term-created-1")]; !ok || created.Name == "" || created.State != types.TerminalRunStateRunning || !created.Visible {
				t.Fatalf("expected manager mouse click create success to connect visible terminal, got %+v ok=%v", created, ok)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected terminal manager mouse click create-row scenario to succeed, got %v", err)
	}
	if len(client.createCalls) != 1 {
		t.Fatalf("expected one create call, got %d", len(client.createCalls))
	}
}

func TestE2ERunScenarioTerminalManagerMouseClickOnCreateRowFailureShowsNoticeInView(t *testing.T) {
	client := &stubRunClient{createErr: errRuntimeEffectBoom}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [create] + new terminal")
			if clickY < 0 {
				t.Fatalf("expected terminal manager preview to expose create row, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_manager") || !strings.Contains(view, "terminal_manager_rows:") || !strings.Contains(view, "notices:") || !strings.Contains(view, "runtime effect boom") {
				t.Fatalf("expected terminal manager mouse click create failure to surface notice, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected terminal manager mouse click create failure scenario to succeed, got %v", err)
	}
	if len(client.createCalls) != 1 {
		t.Fatalf("expected one failed create call, got %d", len(client.createCalls))
	}
}

func TestE2ERunScenarioTerminalManagerMouseClickOnNewTabActionCreatesTab(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [new_tab] open in new tab")
			if clickY < 0 {
				t.Fatalf("expected terminal manager actions to expose new-tab row, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "tab: tab-2") || !strings.Contains(view, "terminal: term-2") || !strings.Contains(view, "terminal_visibility: true") || strings.Contains(view, "terminal_manager_actions:") {
				t.Fatalf("expected terminal manager new-tab mouse action to create focused tab, got:\n%s", view)
			}
			workspace := current.State().Domain.Workspaces[types.WorkspaceID("ws-1")]
			if workspace.ActiveTabID != types.TabID("tab-2") {
				t.Fatalf("expected mouse new-tab action to switch active tab, got %+v", workspace.ActiveTabID)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected terminal manager new-tab mouse action scenario to succeed, got %v", err)
	}
	if len(client.newTabCalls) != 1 {
		t.Fatalf("expected one new-tab call, got %d", len(client.newTabCalls))
	}
}

func TestE2ERunScenarioTerminalManagerMouseClickOnEditActionOpensPrompt(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyDown},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [edit] edit metadata")
			if clickY < 0 {
				t.Fatalf("expected terminal manager actions to expose edit row, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: prompt") || !strings.Contains(view, "prompt_title: edit terminal metadata") || !strings.Contains(view, "prompt_terminal: term-2") {
				t.Fatalf("expected terminal manager edit mouse action to open prompt, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected terminal manager edit mouse action scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerMouseClickOnStopActionClosesOverlay(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [stop] stop terminal")
			if clickY < 0 {
				t.Fatalf("expected terminal manager actions to expose stop row, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "slot: empty") || strings.Contains(view, "terminal_manager_actions:") {
				t.Fatalf("expected terminal manager stop mouse action to close overlay, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected terminal manager stop mouse action scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioFollowerMouseClickOnAcquireOwnerActionKeepsManagerOpen(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithFollowerActivePane()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [acquire_owner] acquire owner")
			if clickY < 0 {
				t.Fatalf("expected terminal manager actions to expose acquire-owner row, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_manager") || !strings.Contains(view, "terminal_manager_selected_owner: pane:pane-2") || !strings.Contains(view, "detail_owner: pane:pane-2") {
				t.Fatalf("expected acquire-owner mouse action to keep manager open and refresh owner, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected acquire-owner mouse action scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalManagerCreateRowFailureShowsNoticeInView(t *testing.T) {
	client := &stubRunClient{createErr: errRuntimeEffectBoom}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
				{Type: tea.KeyUp},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_manager") || !strings.Contains(view, "terminal_manager_rows:") || !strings.Contains(view, "notices:") || !strings.Contains(view, "runtime effect boom") {
				t.Fatalf("expected manager create failure to surface notice in runtime view, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.createCalls) != 1 {
		t.Fatalf("expected one failed create call, got %d", len(client.createCalls))
	}
}

func TestE2ERunScenarioTerminalManagerRefreshesAfterRuntimeRemoval(t *testing.T) {
	events := make(chan protocol.Event, 1)
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{
		sessions: RuntimeSessions{EventStream: events},
	}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			current := model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			initCmd := current.Init()
			if initCmd == nil {
				t.Fatal("expected runtime init command")
			}
			events <- protocol.Event{
				Type:       protocol.EventTerminalRemoved,
				TerminalID: "term-1",
				Removed:    &protocol.TerminalRemovedData{Reason: "server_shutdown"},
			}
			msg := initCmd()
			if msg == nil {
				t.Fatal("expected runtime removed event message")
			}
			nextModel, cmd := current.Update(msg)
			current = nextModel.(*btui.Model)
			if cmd == nil {
				t.Fatal("expected feedback command from removed event")
			}
			for _, nextMsg := range runCmdMessages(cmd) {
				nextModel, _ = current.Update(nextMsg)
				current = nextModel.(*btui.Model)
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_manager") || !strings.Contains(view, "terminal_manager_selected: term-2") || !strings.Contains(view, "terminal_manager_detail: build-log") || !strings.Contains(view, "detail_terminal: term-2") || !strings.Contains(view, "terminal_manager_row_count: 5") || strings.Contains(view, "detail_terminal: term-1") {
				t.Fatalf("expected runtime removal to refresh terminal manager projection, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected terminal manager refresh scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioCtrlFOpensTerminalPickerInView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlF},
				{Type: tea.KeyRunes, Runes: []rune("ops")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "terminal_picker_bar: query=ops | selected=term-3 | kind=terminal") || !strings.Contains(view, "overlay: terminal_picker") || !strings.Contains(view, "focus_layer: overlay") || !strings.Contains(view, "focus_overlay_target: terminal_picker") || !strings.Contains(view, "terminal_picker_rows:") || !strings.Contains(view, "terminal_picker_query: ops") || !strings.Contains(view, "terminal_picker_selected: term-3") || !strings.Contains(view, "terminal_picker_selected_label: ops-watch") || !strings.Contains(view, "terminal_picker_selected_kind: terminal") || !strings.Contains(view, "terminal_picker_selected_state: running") || !strings.Contains(view, "terminal_picker_selected_command: journalctl -f") || !strings.Contains(view, "terminal_picker_selected_visible: false") || !strings.Contains(view, "terminal_picker_selected_tags: team=ops") || !strings.Contains(view, "terminal_picker_selected_connected_panes: 0") || !strings.Contains(view, "terminal_picker_row_count: 2") || !strings.Contains(view, "# DIALOG[terminal_picker]") || !strings.Contains(view, "LIST[picker]") || !strings.Contains(view, "DETAIL[target]") || !strings.Contains(view, "F:q=ops term-3") || !strings.Contains(view, "D:ops-watch term-3") || !strings.Contains(view, "ops-watch") {
				t.Fatalf("expected ctrl-f flow to render terminal picker, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalPickerBackspaceUpdatesQuery(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlF},
				{Type: tea.KeyRunes, Runes: []rune("ops")},
				{Type: tea.KeyBackspace},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "terminal_picker_query: op") || !strings.Contains(view, "terminal_picker_selected: term-3") || !strings.Contains(view, "terminal_picker_selected_label: ops-watch") || !strings.Contains(view, "terminal_picker_row_count: 2") || !strings.Contains(view, "> [terminal] ops-watch") {
				t.Fatalf("expected terminal picker backspace flow to update query and preserve match, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalPickerMissingQueryShowsCreateRow(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlF},
				{Type: tea.KeyRunes, Runes: []rune("missing")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_picker") || !strings.Contains(view, "focus_layer: overlay") || !strings.Contains(view, "focus_overlay_target: terminal_picker") || !strings.Contains(view, "terminal_picker_query: missing") || !strings.Contains(view, "terminal_picker_row_count: 1") || !strings.Contains(view, "terminal_picker_rows:") || !strings.Contains(view, "> [create] + new terminal") || strings.Contains(view, "terminal_picker_selected:") {
				t.Fatalf("expected terminal picker missing-query flow to show create row, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalPickerCreateRowSubmitClosesOverlay(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlF},
				{Type: tea.KeyRunes, Runes: []rune("missing")},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "slot: connected") || !strings.Contains(view, "terminal: term-created-1") || strings.Contains(view, "terminal_picker_rows:") {
				t.Fatalf("expected terminal picker create-row submit to close overlay, got:\n%s", view)
			}
			if created, ok := current.State().Domain.Terminals[types.TerminalID("term-created-1")]; !ok || created.Name == "" || created.State != types.TerminalRunStateRunning || !created.Visible {
				t.Fatalf("expected picker create success to connect visible terminal, got %+v ok=%v", created, ok)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.createCalls) != 1 {
		t.Fatalf("expected one create call, got %d", len(client.createCalls))
	}
	if client.createCalls[0].name == "" || len(client.createCalls[0].command) == 0 {
		t.Fatalf("expected create call to include default name and command, got %+v", client.createCalls[0])
	}
}

func TestE2ERunScenarioTerminalPickerMouseClickOnCreateRowClosesOverlay(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlF},
				{Type: tea.KeyRunes, Runes: []rune("ops")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "  [create] + new terminal")
			if clickY < 0 {
				t.Fatalf("expected terminal picker preview to expose create row, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "slot: connected") || !strings.Contains(view, "terminal: term-created-1") || strings.Contains(view, "terminal_picker_rows:") {
				t.Fatalf("expected terminal picker mouse click create row to close overlay, got:\n%s", view)
			}
			if created, ok := current.State().Domain.Terminals[types.TerminalID("term-created-1")]; !ok || created.Name == "" || created.State != types.TerminalRunStateRunning || !created.Visible {
				t.Fatalf("expected picker mouse click create success to connect visible terminal, got %+v ok=%v", created, ok)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected terminal picker mouse click create-row scenario to succeed, got %v", err)
	}
	if len(client.createCalls) != 1 {
		t.Fatalf("expected one create call, got %d", len(client.createCalls))
	}
}

func TestE2ERunScenarioTerminalPickerMouseClickOnSelectedRowSubmits(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlF},
				{Type: tea.KeyRunes, Runes: []rune("ops")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			clickY := findLineIndexWithPrefix(current.View(), "> [terminal] ops-watch")
			if clickY < 0 {
				t.Fatalf("expected terminal picker preview to expose selected ops-watch row, got:\n%s", current.View())
			}
			nextModel, cmd := current.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "terminal: term-3") || !strings.Contains(view, "title: ops-watch") || strings.Contains(view, "terminal_picker_rows:") {
				t.Fatalf("expected terminal picker mouse click submit to connect selected terminal, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected terminal picker mouse click submit scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalPickerCreateRowFailureShowsNoticeInView(t *testing.T) {
	client := &stubRunClient{createErr: errRuntimeEffectBoom}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlF},
				{Type: tea.KeyRunes, Runes: []rune("missing")},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_picker") || !strings.Contains(view, "terminal_picker_rows:") || !strings.Contains(view, "notices:") || !strings.Contains(view, "runtime effect boom") {
				t.Fatalf("expected picker create failure to surface notice in runtime view, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.createCalls) != 1 {
		t.Fatalf("expected one failed create call, got %d", len(client.createCalls))
	}
}

func TestE2ERunScenarioTerminalPickerEscClosesOverlay(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlF},
				{Type: tea.KeyEsc},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || strings.Contains(view, "terminal_picker_rows:") || strings.Contains(view, "focus_overlay_target:") {
				t.Fatalf("expected terminal picker esc flow to close overlay, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioTerminalPickerSubmitConnectsSelectedTerminal(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlF},
				{Type: tea.KeyRunes, Runes: []rune("ops")},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "terminal: term-3") || !strings.Contains(view, "title: ops-watch") || !strings.Contains(view, "terminal_command: journalctl -f") || strings.Contains(view, "terminal_picker_rows:") {
				t.Fatalf("expected terminal picker submit flow to connect selected terminal, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioLayoutResolveMoveUpdatesView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithLayoutResolveTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			if view := model.View(); !strings.Contains(view, "layout_resolve_bar: pane=pane-1 | role=backend-dev | selected=connect_existing") || !strings.Contains(view, "> [connect_existing] connect existing") || !strings.Contains(view, "layout_resolve_role: backend-dev") || !strings.Contains(view, "layout_resolve_hint: env=dev service=api") || !strings.Contains(view, "focus_layer: overlay") || !strings.Contains(view, "focus_overlay_target: layout_resolve") || !strings.Contains(view, "mode: picker") || !strings.Contains(view, "LIST[resolve]") || !strings.Contains(view, "DETAIL[target]") || !strings.Contains(view, "F:pane-1 connect_existing") || !strings.Contains(view, "BODY[action]") {
				t.Fatalf("expected initial resolve selection in view, got:\n%s", view)
			}
			nextModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyDown})
			current := nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "layout_resolve_pane: pane-1") || !strings.Contains(view, "layout_resolve_selected: create_new") || !strings.Contains(view, "layout_resolve_selected_label: create new") || !strings.Contains(view, "layout_resolve_row_count: 3") || !strings.Contains(view, "> [create_new] create new") {
				t.Fatalf("expected down key to move resolve selection in view, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioLayoutResolveSubmitConnectExistingOpensTerminalPicker(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithLayoutResolveTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			nextModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			current := nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_picker") || !strings.Contains(view, "focus_layer: overlay") || !strings.Contains(view, "focus_overlay_target: terminal_picker") || !strings.Contains(view, "terminal_picker_row_count: 1") || !strings.Contains(view, "terminal_picker_rows:") || !strings.Contains(view, "> [create] + new terminal") {
				t.Fatalf("expected layout resolve connect-existing flow to open terminal picker, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioLayoutResolveMouseClickOnSelectedRowSubmits(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithLayoutResolveTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			clickY := findLineIndexWithPrefix(model.View(), "> [connect_existing] connect existing")
			if clickY < 0 {
				t.Fatalf("expected layout resolve preview to expose selected row, got:\n%s", model.View())
			}
			nextModel, cmd := model.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current := nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: terminal_picker") || !strings.Contains(view, "focus_layer: overlay") || !strings.Contains(view, "focus_overlay_target: terminal_picker") || !strings.Contains(view, "terminal_picker_rows:") {
				t.Fatalf("expected layout resolve mouse click submit to open terminal picker, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected layout resolve mouse click submit scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioLayoutResolveMouseClickOnCreateNewClosesOverlay(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithLayoutResolveTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			clickY := findLineIndexWithPrefix(model.View(), "  [create_new] create new")
			if clickY < 0 {
				t.Fatalf("expected layout resolve preview to expose create-new row, got:\n%s", model.View())
			}
			nextModel, cmd := model.Update(tea.MouseMsg{
				Button: tea.MouseButtonLeft,
				Action: tea.MouseActionPress,
				Y:      clickY,
			})
			current := nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "slot: connected") || !strings.Contains(view, "terminal: term-created-1") || strings.Contains(view, "layout_resolve_rows:") {
				t.Fatalf("expected layout resolve mouse click create-new to close overlay, got:\n%s", view)
			}
			if created, ok := current.State().Domain.Terminals[types.TerminalID("term-created-1")]; !ok || created.Name == "" || created.State != types.TerminalRunStateRunning || !created.Visible {
				t.Fatalf("expected layout resolve mouse click create success to connect visible terminal, got %+v ok=%v", created, ok)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected layout resolve mouse click create-new scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioLayoutResolveCreateNewClosesOverlay(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithLayoutResolveTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyDown},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "slot: connected") || !strings.Contains(view, "terminal: term-created-1") || strings.Contains(view, "layout_resolve_rows:") || strings.Contains(view, "focus_overlay_target:") || strings.Contains(view, "mode:") {
				t.Fatalf("expected layout resolve create-new flow to close overlay, got:\n%s", view)
			}
			if created, ok := current.State().Domain.Terminals[types.TerminalID("term-created-1")]; !ok || created.Name == "" || created.State != types.TerminalRunStateRunning || !created.Visible {
				t.Fatalf("expected layout resolve create success to connect visible terminal, got %+v ok=%v", created, ok)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioLayoutResolveCreateNewFailureShowsNoticeInView(t *testing.T) {
	client := &stubRunClient{createErr: errRuntimeEffectBoom}
	initial := runtimeStateWithLayoutResolveTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyDown},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: layout_resolve") || !strings.Contains(view, "layout_resolve_rows:") || !strings.Contains(view, "notices:") || !strings.Contains(view, "runtime effect boom") {
				t.Fatalf("expected layout resolve create failure to keep overlay and surface notice, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
	if len(client.createCalls) != 1 {
		t.Fatalf("expected one failed create call from layout resolve, got %d", len(client.createCalls))
	}
}

func TestE2ERunScenarioLayoutResolveSkipClosesOverlay(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithLayoutResolveTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyDown},
				{Type: tea.KeyDown},
				{Type: tea.KeyEnter},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "slot: waiting") || strings.Contains(view, "layout_resolve_rows:") || strings.Contains(view, "focus_overlay_target:") || strings.Contains(view, "mode:") {
				t.Fatalf("expected layout resolve skip flow to close overlay, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioLayoutResolveEscClosesOverlay(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithLayoutResolveTarget()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			nextModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
			current := nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "overlay: none") || !strings.Contains(view, "focus_layer: tiled") || !strings.Contains(view, "slot: waiting") || strings.Contains(view, "layout_resolve_rows:") || strings.Contains(view, "focus_overlay_target:") || strings.Contains(view, "mode:") {
				t.Fatalf("expected layout resolve esc flow to close overlay, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioCtrlGShowsGlobalModeInView(t *testing.T) {
	client := &stubRunClient{}
	initial := buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty)
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			if view := model.View(); strings.Contains(view, "mode: global") {
				t.Fatalf("expected initial view without global mode, got:\n%s", view)
			}
			nextModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
			current := nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "mode: global") || !strings.Contains(view, "mode_sticky: false") {
				t.Fatalf("expected ctrl-g to show global mode in view, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{PrefixTimeout: 3 * time.Second}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioCtrlGEscClearsGlobalModeInView(t *testing.T) {
	client := &stubRunClient{}
	initial := buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty)
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyEsc},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); strings.Contains(view, "mode: global") {
				t.Fatalf("expected esc to clear global mode in view, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioModeTimeoutClearsGlobalModeInView(t *testing.T) {
	client := &stubRunClient{}
	initial := buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty)
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			nextModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
			current := nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); !strings.Contains(view, "mode: global") {
				t.Fatalf("expected ctrl-g to activate global mode before timeout, got:\n%s", view)
			}
			deadline := current.State().UI.Mode.DeadlineAt
			if deadline == nil {
				t.Fatalf("expected global mode deadline to be set")
			}
			nextModel, cmd = current.Update(btui.FeedbackMsg{
				Intents: []intent.Intent{intent.ModeTimedOutIntent{Now: deadline.Add(time.Second)}},
			})
			current = nextModel.(*btui.Model)
			if cmd != nil {
				if msg := cmd(); msg != nil {
					nextModel, _ = current.Update(msg)
					current = nextModel.(*btui.Model)
				}
			}
			if view := current.View(); strings.Contains(view, "mode: global") {
				t.Fatalf("expected timeout to clear global mode in view, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioCtrlGTReplacesGlobalModeWithPickerMode(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithTerminalManagerTargets()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			var current *btui.Model = model
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyCtrlG},
				{Type: tea.KeyRunes, Runes: []rune("t")},
			} {
				nextModel, cmd := current.Update(key)
				current = nextModel.(*btui.Model)
				if cmd != nil {
					if msg := cmd(); msg != nil {
						nextModel, _ = current.Update(msg)
						current = nextModel.(*btui.Model)
					}
				}
			}
			if view := current.View(); !strings.Contains(view, "mode: picker") || strings.Contains(view, "mode: global") || !strings.Contains(view, "overlay: terminal_manager") {
				t.Fatalf("expected ctrl-g t to replace global mode with picker mode, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioFollowerConnectionRoleVisibleInView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithFollowerPaneConnection()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			if view := model.View(); !strings.Contains(view, "connection_role: follower") || !strings.Contains(view, "connected_panes: 2") {
				t.Fatalf("expected runtime view to expose shared connection state, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioOwnerConnectionRoleVisibleInView(t *testing.T) {
	client := &stubRunClient{}
	initial := connectedRunAppState()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			if view := model.View(); !strings.Contains(view, "connection_role: owner") || !strings.Contains(view, "connected_panes: 1") {
				t.Fatalf("expected runtime view to expose owner connection state, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioActiveTerminalMetadataVisibleInView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithActiveTerminalMetadata()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			if view := model.View(); !strings.Contains(view, "terminal_command: npm run dev") || !strings.Contains(view, "terminal_tags: env=dev,service=api") || !strings.Contains(view, "terminal_visibility: true") {
				t.Fatalf("expected runtime view to expose active terminal metadata, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

func TestE2ERunScenarioFloatingPaneKindVisibleInView(t *testing.T) {
	client := &stubRunClient{}
	initial := runtimeStateWithFloatingActivePane()
	planner := &stubRunPlanner{plan: StartupPlan{State: initial}}
	executor := &stubRunTaskExecutor{plan: StartupPlan{State: initial}}
	bootstrapper := &stubRunSessionBootstrapper{}
	runner := &stubProgramRunner{
		run: func(model *btui.Model) error {
			if view := model.View(); !strings.Contains(view, "pane_kind: floating") || !strings.Contains(view, "focus_layer: floating") || !strings.Contains(view, "tab_layer: floating") {
				t.Fatalf("expected runtime view to expose floating pane state, got:\n%s", view)
			}
			return nil
		},
	}

	err := runWithDependencies(client, Config{}, nil, io.Discard, runtimeDependencies{
		Planner:          planner,
		TaskExecutor:     executor,
		SessionBootstrap: bootstrapper,
		ProgramRunner:    runner,
		Renderer:         runtimeRenderer{},
	})
	if err != nil {
		t.Fatalf("expected run scenario to succeed, got %v", err)
	}
}

var (
	errRuntimeRunBoom     = errors.New("run boom")
	errRuntimeEffectBoom  = errors.New("runtime effect boom")
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

type stubProgramRunner struct {
	err   error
	calls int
	view  string
	run   func(model *btui.Model) error
}

func (r *stubProgramRunner) Run(model *btui.Model, _ io.Reader, _ io.Writer) error {
	r.calls++
	r.view = model.View()
	if r.run != nil {
		if err := r.run(model); err != nil {
			return err
		}
	}
	if r.err != nil {
		return r.err
	}
	return nil
}

type stubRunClient struct {
	inputs        []runtimeInputCall
	snapshots     map[string]*protocol.Snapshot
	snapshotErr   error
	createCalls   []runtimeCreateCall
	metadataCalls []runtimeMetadataCall
	killCalls     []string
	newTabCalls   []runtimeNewTabCall
	floatingCalls []runtimeFloatingCall
	resizeCalls   []runtimeResizeCall
	createErr     error
	metadataErr   error
	killErr       error
	newTabErr     error
	floatingErr   error
	resizeErr     error
}

type runtimeCreateCall struct {
	command []string
	name    string
	size    protocol.Size
}

type runtimeMetadataCall struct {
	terminalID string
	name       string
	tags       map[string]string
}

type runtimeNewTabCall struct {
	workspaceID types.WorkspaceID
	terminalID  types.TerminalID
}

type runtimeFloatingCall struct {
	workspaceID types.WorkspaceID
	tabID       types.TabID
	terminalID  types.TerminalID
}

type runtimeResizeCall struct {
	channel uint16
	cols    uint16
	rows    uint16
}

func (c *stubRunClient) Close() error { return nil }

func (c *stubRunClient) Create(_ context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	c.createCalls = append(c.createCalls, runtimeCreateCall{
		command: append([]string(nil), command...),
		name:    name,
		size:    size,
	})
	if c.createErr != nil {
		return nil, c.createErr
	}
	return &protocol.CreateResult{
		TerminalID: fmt.Sprintf("term-created-%d", len(c.createCalls)),
		State:      string(types.TerminalRunStateRunning),
	}, nil
}

func (c *stubRunClient) SetTags(context.Context, string, map[string]string) error { return nil }

func (c *stubRunClient) SetMetadata(_ context.Context, terminalID string, name string, tags map[string]string) error {
	cloned := make(map[string]string, len(tags))
	for key, value := range tags {
		cloned[key] = value
	}
	c.metadataCalls = append(c.metadataCalls, runtimeMetadataCall{
		terminalID: terminalID,
		name:       name,
		tags:       cloned,
	})
	if c.metadataErr != nil {
		return c.metadataErr
	}
	return nil
}

func (c *stubRunClient) List(context.Context) (*protocol.ListResult, error) { return nil, nil }

func (c *stubRunClient) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	ch := make(chan protocol.Event)
	close(ch)
	return ch, nil
}

func (c *stubRunClient) Attach(context.Context, string, string) (*protocol.AttachResult, error) {
	return nil, nil
}

func (c *stubRunClient) Snapshot(_ context.Context, terminalID string, _, _ int) (*protocol.Snapshot, error) {
	if c.snapshotErr != nil {
		return nil, c.snapshotErr
	}
	return cloneSnapshot(c.snapshots[terminalID]), nil
}

func (c *stubRunClient) Input(_ context.Context, channel uint16, data []byte) error {
	c.inputs = append(c.inputs, runtimeInputCall{
		channel: channel,
		data:    append([]byte(nil), data...),
	})
	return nil
}

func (c *stubRunClient) Resize(_ context.Context, channel uint16, cols, rows uint16) error {
	c.resizeCalls = append(c.resizeCalls, runtimeResizeCall{
		channel: channel,
		cols:    cols,
		rows:    rows,
	})
	if c.resizeErr != nil {
		return c.resizeErr
	}
	return nil
}

func (c *stubRunClient) Stream(uint16) (<-chan protocol.StreamFrame, func()) {
	ch := make(chan protocol.StreamFrame)
	close(ch)
	return ch, func() {}
}

func (c *stubRunClient) Kill(_ context.Context, terminalID string) error {
	c.killCalls = append(c.killCalls, terminalID)
	if c.killErr != nil {
		return c.killErr
	}
	return nil
}

func (c *stubRunClient) ConnectTerminalInNewTab(workspaceID types.WorkspaceID, terminalID types.TerminalID) error {
	c.newTabCalls = append(c.newTabCalls, runtimeNewTabCall{
		workspaceID: workspaceID,
		terminalID:  terminalID,
	})
	if c.newTabErr != nil {
		return c.newTabErr
	}
	return nil
}

func (c *stubRunClient) ConnectTerminalInFloatingPane(workspaceID types.WorkspaceID, tabID types.TabID, terminalID types.TerminalID) error {
	c.floatingCalls = append(c.floatingCalls, runtimeFloatingCall{
		workspaceID: workspaceID,
		tabID:       tabID,
		terminalID:  terminalID,
	})
	if c.floatingErr != nil {
		return c.floatingErr
	}
	return nil
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

func runtimeStateWithFollowerActivePane() types.AppState {
	state := connectedRunAppState()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]

	follower := types.PaneState{
		ID:         types.PaneID("pane-2"),
		Kind:       types.PaneKindTiled,
		Rect:       types.Rect{X: 40, Y: 0, W: 40, H: 24},
		TerminalID: types.TerminalID("term-1"),
		SlotState:  types.PaneSlotConnected,
	}
	tab.Panes[follower.ID] = follower
	tab.ActivePaneID = follower.ID
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.UI.Focus.PaneID = follower.ID

	conn := state.Domain.Connections[types.TerminalID("term-1")]
	conn.ConnectedPaneIDs = []types.PaneID{types.PaneID("pane-1"), types.PaneID("pane-2")}
	conn.OwnerPaneID = types.PaneID("pane-1")
	state.Domain.Connections[types.TerminalID("term-1")] = conn
	return state
}

func runtimeStateWithWorkspacePickerTarget() types.AppState {
	state := buildSinglePaneAppState("main", "shell", types.PaneSlotEmpty)
	state.Domain.WorkspaceOrder = append(state.Domain.WorkspaceOrder, types.WorkspaceID("ws-2"))
	state.Domain.Workspaces[types.WorkspaceID("ws-2")] = types.WorkspaceState{
		ID:          types.WorkspaceID("ws-2"),
		Name:        "ops",
		ActiveTabID: types.TabID("tab-2"),
		TabOrder:    []types.TabID{types.TabID("tab-2")},
		Tabs: map[types.TabID]types.TabState{
			types.TabID("tab-2"): {
				ID:           types.TabID("tab-2"),
				Name:         "logs",
				ActivePaneID: types.PaneID("pane-2"),
				ActiveLayer:  types.FocusLayerTiled,
				Panes: map[types.PaneID]types.PaneState{
					types.PaneID("pane-2"): {
						ID:        types.PaneID("pane-2"),
						Kind:      types.PaneKindTiled,
						SlotState: types.PaneSlotEmpty,
					},
				},
			},
		},
	}
	return state
}

func runtimeStateWithExitedPaneTarget() types.AppState {
	state := connectedRunAppState()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	pane := tab.Panes[types.PaneID("pane-1")]
	exitCode := 7
	pane.SlotState = types.PaneSlotExited
	pane.LastExitCode = &exitCode
	tab.Panes[types.PaneID("pane-1")] = pane
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:       types.TerminalID("term-1"),
		Name:     "deploy-log",
		Command:  []string{"npm", "run", "deploy"},
		State:    types.TerminalRunStateExited,
		ExitCode: &exitCode,
		Visible:  true,
	}
	return state
}

func runtimeStateWithSplitPaneTargets() types.AppState {
	state := buildSinglePaneAppState("main", "shell", types.PaneSlotConnected)
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	tab.Panes[types.PaneID("pane-1")] = types.PaneState{
		ID:         types.PaneID("pane-1"),
		Kind:       types.PaneKindTiled,
		SlotState:  types.PaneSlotConnected,
		TerminalID: types.TerminalID("term-1"),
	}
	tab.Panes[types.PaneID("pane-2")] = types.PaneState{
		ID:         types.PaneID("pane-2"),
		Kind:       types.PaneKindTiled,
		SlotState:  types.PaneSlotConnected,
		TerminalID: types.TerminalID("term-2"),
	}
	tab.RootSplit = &types.SplitNode{
		Direction: types.SplitDirectionVertical,
		Ratio:     0.5,
		First:     &types.SplitNode{PaneID: types.PaneID("pane-1")},
		Second:    &types.SplitNode{PaneID: types.PaneID("pane-2")},
	}
	tab.ActivePaneID = types.PaneID("pane-1")
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "api-dev",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	state.Domain.Terminals[types.TerminalID("term-2")] = types.TerminalRef{
		ID:      types.TerminalID("term-2"),
		Name:    "build-log",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	state.Domain.Connections[types.TerminalID("term-2")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-2"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-2")},
		OwnerPaneID:      types.PaneID("pane-2"),
	}
	return state
}

func runtimeStateWithNestedSplitPaneTargets() types.AppState {
	state := runtimeStateWithSplitPaneTargets()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	tab.Panes[types.PaneID("pane-3")] = types.PaneState{
		ID:         types.PaneID("pane-3"),
		Kind:       types.PaneKindTiled,
		SlotState:  types.PaneSlotConnected,
		TerminalID: types.TerminalID("term-3"),
	}
	tab.RootSplit = &types.SplitNode{
		Direction: types.SplitDirectionHorizontal,
		Ratio:     0.6,
		First:     &types.SplitNode{PaneID: types.PaneID("pane-1")},
		Second: &types.SplitNode{
			Direction: types.SplitDirectionVertical,
			Ratio:     0.5,
			First:     &types.SplitNode{PaneID: types.PaneID("pane-3")},
			Second:    &types.SplitNode{PaneID: types.PaneID("pane-2")},
		},
	}
	tab.ActivePaneID = types.PaneID("pane-1")
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.Domain.Terminals[types.TerminalID("term-3")] = types.TerminalRef{
		ID:      types.TerminalID("term-3"),
		Name:    "watcher",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	state.Domain.Connections[types.TerminalID("term-3")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-3"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-3")},
		OwnerPaneID:      types.PaneID("pane-3"),
	}
	return state
}

func runtimeStateWithTwoTabTargets() types.AppState {
	state := buildSinglePaneAppState("main", "shell", types.PaneSlotConnected)
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	ws.TabOrder = []types.TabID{types.TabID("tab-1"), types.TabID("tab-2")}
	ws.ActiveTabID = types.TabID("tab-1")
	ws.Tabs[types.TabID("tab-1")] = types.TabState{
		ID:           types.TabID("tab-1"),
		Name:         "shell",
		ActivePaneID: types.PaneID("pane-1"),
		ActiveLayer:  types.FocusLayerTiled,
		Panes: map[types.PaneID]types.PaneState{
			types.PaneID("pane-1"): {
				ID:         types.PaneID("pane-1"),
				Kind:       types.PaneKindTiled,
				SlotState:  types.PaneSlotConnected,
				TerminalID: types.TerminalID("term-1"),
			},
		},
		RootSplit: &types.SplitNode{PaneID: types.PaneID("pane-1")},
	}
	ws.Tabs[types.TabID("tab-2")] = types.TabState{
		ID:           types.TabID("tab-2"),
		Name:         "logs",
		ActivePaneID: types.PaneID("pane-2"),
		ActiveLayer:  types.FocusLayerTiled,
		Panes: map[types.PaneID]types.PaneState{
			types.PaneID("pane-2"): {
				ID:         types.PaneID("pane-2"),
				Kind:       types.PaneKindTiled,
				SlotState:  types.PaneSlotConnected,
				TerminalID: types.TerminalID("term-2"),
			},
		},
		RootSplit: &types.SplitNode{PaneID: types.PaneID("pane-2")},
	}
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "api-dev",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	state.Domain.Terminals[types.TerminalID("term-2")] = types.TerminalRef{
		ID:      types.TerminalID("term-2"),
		Name:    "build-log",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	state.Domain.Connections[types.TerminalID("term-2")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-2"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-2")},
		OwnerPaneID:      types.PaneID("pane-2"),
	}
	return state
}

func runtimeStateWithWorkspacePickerAutoAcquireTarget() types.AppState {
	state := buildSinglePaneAppState("main", "shell", types.PaneSlotConnected)
	ws1 := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab1 := ws1.Tabs[types.TabID("tab-1")]
	pane1 := tab1.Panes[types.PaneID("pane-1")]
	pane1.TerminalID = types.TerminalID("term-1")
	tab1.Panes[types.PaneID("pane-1")] = pane1
	ws1.Tabs[types.TabID("tab-1")] = tab1
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws1

	state.Domain.WorkspaceOrder = append(state.Domain.WorkspaceOrder, types.WorkspaceID("ws-2"))
	state.Domain.Workspaces[types.WorkspaceID("ws-2")] = types.WorkspaceState{
		ID:          types.WorkspaceID("ws-2"),
		Name:        "ops",
		ActiveTabID: types.TabID("tab-2"),
		TabOrder:    []types.TabID{types.TabID("tab-2")},
		Tabs: map[types.TabID]types.TabState{
			types.TabID("tab-2"): {
				ID:               types.TabID("tab-2"),
				Name:             "logs",
				ActivePaneID:     types.PaneID("pane-2"),
				ActiveLayer:      types.FocusLayerTiled,
				AutoAcquireOwner: true,
				Panes: map[types.PaneID]types.PaneState{
					types.PaneID("pane-2"): {
						ID:         types.PaneID("pane-2"),
						Kind:       types.PaneKindTiled,
						SlotState:  types.PaneSlotConnected,
						TerminalID: types.TerminalID("term-1"),
					},
				},
			},
		},
	}
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "shared-dev",
		State:   types.TerminalRunStateRunning,
		Command: []string{"npm", "run", "dev"},
		Visible: true,
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:        types.TerminalID("term-1"),
		ConnectedPaneIDs:  []types.PaneID{types.PaneID("pane-1"), types.PaneID("pane-2")},
		OwnerPaneID:       types.PaneID("pane-1"),
		AutoAcquirePolicy: types.AutoAcquireTabEnter,
	}
	return state
}

func runtimeStateWithTerminalManagerTargets() types.AppState {
	state := buildSinglePaneAppState("main", "shell", types.PaneSlotConnected)
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	pane := tab.Panes[types.PaneID("pane-1")]
	pane.TerminalID = types.TerminalID("term-1")
	tab.Panes[types.PaneID("pane-1")] = pane
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "api-dev",
		State:   types.TerminalRunStateRunning,
		Command: []string{"npm", "run", "dev"},
		Visible: true,
	}
	state.Domain.Terminals[types.TerminalID("term-2")] = types.TerminalRef{
		ID:      types.TerminalID("term-2"),
		Name:    "build-log",
		State:   types.TerminalRunStateRunning,
		Command: []string{"tail", "-f", "build.log"},
		Tags:    map[string]string{"group": "build"},
	}
	state.Domain.Terminals[types.TerminalID("term-3")] = types.TerminalRef{
		ID:      types.TerminalID("term-3"),
		Name:    "ops-watch",
		State:   types.TerminalRunStateRunning,
		Command: []string{"journalctl", "-f"},
		Tags:    map[string]string{"team": "ops"},
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	return state
}

func runtimeStateWithTerminalManagerJumpTargets() types.AppState {
	state := runtimeStateWithTerminalManagerTargets()
	state.Domain.WorkspaceOrder = append(state.Domain.WorkspaceOrder, types.WorkspaceID("ws-2"))
	state.Domain.Workspaces[types.WorkspaceID("ws-2")] = types.WorkspaceState{
		ID:          types.WorkspaceID("ws-2"),
		Name:        "ops",
		ActiveTabID: types.TabID("tab-2"),
		TabOrder:    []types.TabID{types.TabID("tab-2")},
		Tabs: map[types.TabID]types.TabState{
			types.TabID("tab-2"): {
				ID:           types.TabID("tab-2"),
				Name:         "logs",
				ActivePaneID: types.PaneID("pane-remote"),
				ActiveLayer:  types.FocusLayerTiled,
				Panes: map[types.PaneID]types.PaneState{
					types.PaneID("pane-remote"): {
						ID:         types.PaneID("pane-remote"),
						Kind:       types.PaneKindTiled,
						SlotState:  types.PaneSlotConnected,
						TerminalID: types.TerminalID("term-2"),
					},
				},
				RootSplit: &types.SplitNode{PaneID: types.PaneID("pane-remote")},
			},
		},
	}
	terminal := state.Domain.Terminals[types.TerminalID("term-2")]
	terminal.Visible = true
	state.Domain.Terminals[types.TerminalID("term-2")] = terminal
	state.Domain.Connections[types.TerminalID("term-2")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-2"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-remote")},
		OwnerPaneID:      types.PaneID("pane-remote"),
	}
	return state
}

func runtimeStateWithTerminalManagerLocationTargets() types.AppState {
	state := runtimeStateWithTerminalManagerTargets()
	state.Domain.WorkspaceOrder = append(state.Domain.WorkspaceOrder, types.WorkspaceID("ws-2"))
	state.Domain.Workspaces[types.WorkspaceID("ws-2")] = types.WorkspaceState{
		ID:          types.WorkspaceID("ws-2"),
		Name:        "ops",
		ActiveTabID: types.TabID("tab-2"),
		TabOrder:    []types.TabID{types.TabID("tab-2")},
		Tabs: map[types.TabID]types.TabState{
			types.TabID("tab-2"): {
				ID:           types.TabID("tab-2"),
				Name:         "logs",
				ActivePaneID: types.PaneID("float-ops"),
				ActiveLayer:  types.FocusLayerFloating,
				FloatingOrder: []types.PaneID{
					types.PaneID("float-ops"),
				},
				Panes: map[types.PaneID]types.PaneState{
					types.PaneID("float-ops"): {
						ID:         types.PaneID("float-ops"),
						Kind:       types.PaneKindFloating,
						SlotState:  types.PaneSlotConnected,
						TerminalID: types.TerminalID("term-1"),
					},
				},
			},
		},
	}
	terminal := state.Domain.Terminals[types.TerminalID("term-1")]
	terminal.Visible = true
	state.Domain.Terminals[types.TerminalID("term-1")] = terminal
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1"), types.PaneID("float-ops")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	return state
}

func runtimeStateWithLayoutResolveTarget() types.AppState {
	state := buildSinglePaneAppState("main", "shell", types.PaneSlotWaiting)
	state.UI.Overlay = types.OverlayState{
		Kind:        types.OverlayLayoutResolve,
		Data:        layoutresolvedomain.NewState(types.PaneID("pane-1"), "backend-dev", "env=dev service=api"),
		ReturnFocus: state.UI.Focus,
	}
	state.UI.Focus.Layer = types.FocusLayerOverlay
	state.UI.Focus.OverlayTarget = types.OverlayLayoutResolve
	state.UI.Mode = types.ModeState{Active: types.ModePicker}
	return state
}

func runtimeStateWithActiveTerminalMetadata() types.AppState {
	state := connectedRunAppState()
	terminal := state.Domain.Terminals[types.TerminalID("term-1")]
	terminal.Name = "api-dev"
	terminal.Command = []string{"npm", "run", "dev"}
	terminal.Tags = map[string]string{"service": "api", "env": "dev"}
	terminal.Visible = true
	state.Domain.Terminals[types.TerminalID("term-1")] = terminal
	return state
}

func runtimeStateWithFloatingActivePane() types.AppState {
	state := buildSinglePaneAppState("main", "shell", types.PaneSlotConnected)
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	delete(tab.Panes, types.PaneID("pane-1"))
	tab.Panes[types.PaneID("pane-float")] = types.PaneState{
		ID:         types.PaneID("pane-float"),
		Kind:       types.PaneKindFloating,
		SlotState:  types.PaneSlotConnected,
		TerminalID: types.TerminalID("term-1"),
	}
	tab.FloatingOrder = []types.PaneID{types.PaneID("pane-float")}
	tab.ActivePaneID = types.PaneID("pane-float")
	tab.ActiveLayer = types.FocusLayerFloating
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.UI.Focus.Layer = types.FocusLayerFloating
	state.UI.Focus.PaneID = types.PaneID("pane-float")
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:    types.TerminalID("term-1"),
		Name:  "float-dev",
		State: types.TerminalRunStateRunning,
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-float")},
		OwnerPaneID:      types.PaneID("pane-float"),
	}
	return state
}

func runtimeStateWithTwoFloatingTargets() types.AppState {
	state := connectedRunAppState()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	delete(tab.Panes, types.PaneID("pane-1"))
	tab.Panes[types.PaneID("float-1")] = types.PaneState{
		ID:         types.PaneID("float-1"),
		Kind:       types.PaneKindFloating,
		SlotState:  types.PaneSlotConnected,
		TerminalID: types.TerminalID("term-1"),
	}
	tab.Panes[types.PaneID("float-2")] = types.PaneState{
		ID:         types.PaneID("float-2"),
		Kind:       types.PaneKindFloating,
		SlotState:  types.PaneSlotConnected,
		TerminalID: types.TerminalID("term-2"),
	}
	tab.FloatingOrder = []types.PaneID{types.PaneID("float-1"), types.PaneID("float-2")}
	tab.ActivePaneID = types.PaneID("float-1")
	tab.ActiveLayer = types.FocusLayerFloating
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.UI.Focus = types.FocusState{
		Layer:       types.FocusLayerFloating,
		WorkspaceID: types.WorkspaceID("ws-1"),
		TabID:       types.TabID("tab-1"),
		PaneID:      types.PaneID("float-1"),
	}
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "api-dev",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	state.Domain.Terminals[types.TerminalID("term-2")] = types.TerminalRef{
		ID:      types.TerminalID("term-2"),
		Name:    "build-log",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("float-1")},
		OwnerPaneID:      types.PaneID("float-1"),
	}
	state.Domain.Connections[types.TerminalID("term-2")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-2"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("float-2")},
		OwnerPaneID:      types.PaneID("float-2"),
	}
	return state
}

func runtimeStateWithFloatingOverviewTargets() types.AppState {
	state := runtimeStateWithTwoFloatingTargets()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	pane1 := tab.Panes[types.PaneID("float-1")]
	pane1.Rect = types.Rect{X: 10, Y: 8, W: 30, H: 12}
	tab.Panes[types.PaneID("float-1")] = pane1
	pane2 := tab.Panes[types.PaneID("float-2")]
	pane2.Rect = types.Rect{X: 45, Y: 14, W: 28, H: 10}
	tab.Panes[types.PaneID("float-2")] = pane2
	tab.ActivePaneID = types.PaneID("float-1")
	tab.ActiveLayer = types.FocusLayerFloating
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.UI.Focus = types.FocusState{
		Layer:       types.FocusLayerFloating,
		WorkspaceID: types.WorkspaceID("ws-1"),
		TabID:       types.TabID("tab-1"),
		PaneID:      types.PaneID("float-1"),
	}
	return state
}

func runtimeStateWithMixedPaneSlots() types.AppState {
	state := buildSinglePaneAppState("main", "shell", types.PaneSlotConnected)
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	exitCode := 7

	tab.Panes[types.PaneID("pane-1")] = types.PaneState{
		ID:         types.PaneID("pane-1"),
		Kind:       types.PaneKindTiled,
		SlotState:  types.PaneSlotConnected,
		TerminalID: types.TerminalID("term-1"),
	}
	tab.Panes[types.PaneID("pane-2")] = types.PaneState{
		ID:        types.PaneID("pane-2"),
		Kind:      types.PaneKindTiled,
		SlotState: types.PaneSlotWaiting,
	}
	tab.Panes[types.PaneID("pane-3")] = types.PaneState{
		ID:           types.PaneID("pane-3"),
		Kind:         types.PaneKindTiled,
		SlotState:    types.PaneSlotExited,
		TerminalID:   types.TerminalID("term-3"),
		LastExitCode: &exitCode,
	}
	tab.Panes[types.PaneID("float-empty")] = types.PaneState{
		ID:        types.PaneID("float-empty"),
		Kind:      types.PaneKindFloating,
		SlotState: types.PaneSlotEmpty,
		Rect:      types.Rect{X: 60, Y: 2, W: 20, H: 8},
	}
	tab.RootSplit = &types.SplitNode{
		Direction: types.SplitDirectionHorizontal,
		Ratio:     0.5,
		First:     &types.SplitNode{PaneID: types.PaneID("pane-1")},
		Second: &types.SplitNode{
			Direction: types.SplitDirectionVertical,
			Ratio:     0.5,
			First:     &types.SplitNode{PaneID: types.PaneID("pane-2")},
			Second:    &types.SplitNode{PaneID: types.PaneID("pane-3")},
		},
	}
	tab.FloatingOrder = []types.PaneID{types.PaneID("float-empty")}
	tab.ActivePaneID = types.PaneID("pane-1")
	tab.ActiveLayer = types.FocusLayerTiled
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.UI.Focus = types.FocusState{
		Layer:       types.FocusLayerTiled,
		WorkspaceID: types.WorkspaceID("ws-1"),
		TabID:       types.TabID("tab-1"),
		PaneID:      types.PaneID("pane-1"),
	}
	state.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "api-dev",
		State:   types.TerminalRunStateRunning,
		Visible: true,
	}
	state.Domain.Terminals[types.TerminalID("term-3")] = types.TerminalRef{
		ID:       types.TerminalID("term-3"),
		Name:     "deploy-log",
		State:    types.TerminalRunStateExited,
		ExitCode: &exitCode,
		Visible:  true,
	}
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	state.Domain.Connections[types.TerminalID("term-3")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-3"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-3")},
		OwnerPaneID:      types.PaneID("pane-3"),
	}
	return state
}

func runtimeStateWithFloatingPositionedPane() types.AppState {
	state := runtimeStateWithTwoFloatingTargets()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	tab.FloatingOrder = []types.PaneID{types.PaneID("float-1")}
	delete(tab.Panes, types.PaneID("float-2"))
	pane := tab.Panes[types.PaneID("float-1")]
	pane.Rect = types.Rect{X: 10, Y: 8, W: 30, H: 12}
	tab.Panes[types.PaneID("float-1")] = pane
	tab.ActivePaneID = types.PaneID("float-1")
	tab.ActiveLayer = types.FocusLayerFloating
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	delete(state.Domain.Terminals, types.TerminalID("term-2"))
	delete(state.Domain.Connections, types.TerminalID("term-2"))
	return state
}

func runtimeStateWithFollowerPaneConnection() types.AppState {
	state := connectedRunAppState()
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	tab.Panes[types.PaneID("pane-2")] = types.PaneState{
		ID:         types.PaneID("pane-2"),
		Kind:       types.PaneKindTiled,
		SlotState:  types.PaneSlotConnected,
		TerminalID: types.TerminalID("term-1"),
	}
	tab.ActivePaneID = types.PaneID("pane-2")
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1"), types.PaneID("pane-2")},
		OwnerPaneID:      types.PaneID("pane-1"),
	}
	state.UI.Focus.PaneID = types.PaneID("pane-2")
	return state
}
