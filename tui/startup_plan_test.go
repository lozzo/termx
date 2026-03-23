package tui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app/reducer"
	"github.com/lozzow/termx/tui/bt"
	"github.com/lozzow/termx/tui/domain/layoutresolve"
	"github.com/lozzow/termx/tui/domain/types"
)

func TestStartupPlannerDefaultLaunchBuildsWorkspaceAndCreateTask(t *testing.T) {
	planner := NewStartupPlanner(nil)

	plan, err := planner.Plan(context.Background(), Config{
		DefaultShell: "/bin/zsh",
		Workspace:    "main",
	})
	if err != nil {
		t.Fatalf("expected default startup to succeed, got %v", err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected one startup task, got %d", len(plan.Tasks))
	}
	task, ok := plan.Tasks[0].(CreateTerminalTask)
	if !ok {
		t.Fatalf("expected create terminal task, got %T", plan.Tasks[0])
	}
	if task.PaneID != types.PaneID("pane-1") {
		t.Fatalf("expected task target pane-1, got %+v", task)
	}
	if len(task.Command) != 1 || task.Command[0] != "/bin/zsh" {
		t.Fatalf("expected default shell command, got %+v", task.Command)
	}
	if task.Name != "ws-1-tab-1-pane-1" {
		t.Fatalf("expected stable startup terminal name, got %+v", task)
	}

	if plan.State.Domain.ActiveWorkspaceID != types.WorkspaceID("ws-1") {
		t.Fatalf("expected active workspace ws-1, got %q", plan.State.Domain.ActiveWorkspaceID)
	}
	workspace := plan.State.Domain.Workspaces[types.WorkspaceID("ws-1")]
	if workspace.Name != "main" {
		t.Fatalf("expected workspace name main, got %+v", workspace)
	}
	tab := workspace.Tabs[types.TabID("tab-1")]
	if tab.Name != "shell" {
		t.Fatalf("expected default tab name shell, got %+v", tab)
	}
	pane := tab.Panes[types.PaneID("pane-1")]
	if pane.SlotState != types.PaneSlotEmpty || pane.TerminalID != "" {
		t.Fatalf("expected startup pane to remain empty before boot task executes, got %+v", pane)
	}
}

func TestStartupPlannerLayoutFileBuildsWaitingPaneAndResolveOverlay(t *testing.T) {
	planner := NewStartupPlanner(stubLayoutLoader{
		docs: map[string]string{
			"demo": "workspace: project-api\ntab: dev\nslot:\n  role: backend-dev\n  hint: env=dev service=api\n",
		},
	})

	plan, err := planner.Plan(context.Background(), Config{
		StartupLayout: "demo",
		Workspace:     "main",
	})
	if err != nil {
		t.Fatalf("expected layout startup to succeed, got %v", err)
	}
	if len(plan.Tasks) != 0 {
		t.Fatalf("expected layout startup to defer decision to resolve overlay, got %d tasks", len(plan.Tasks))
	}
	workspace := plan.State.Domain.Workspaces[types.WorkspaceID("ws-1")]
	if workspace.Name != "project-api" {
		t.Fatalf("expected workspace name from layout, got %+v", workspace)
	}
	tab := workspace.Tabs[types.TabID("tab-1")]
	if tab.Name != "dev" {
		t.Fatalf("expected tab name from layout, got %+v", tab)
	}
	pane := tab.Panes[types.PaneID("pane-1")]
	if pane.SlotState != types.PaneSlotWaiting || pane.TerminalID != "" {
		t.Fatalf("expected layout startup waiting pane, got %+v", pane)
	}
	if plan.State.UI.Overlay.Kind != types.OverlayLayoutResolve {
		t.Fatalf("expected resolve overlay, got %q", plan.State.UI.Overlay.Kind)
	}
	resolveState, ok := plan.State.UI.Overlay.Data.(*layoutresolve.State)
	if !ok {
		t.Fatalf("expected resolve overlay data, got %T", plan.State.UI.Overlay.Data)
	}
	if resolveState.Role != "backend-dev" || resolveState.Hint != "env=dev service=api" {
		t.Fatalf("unexpected resolve state payload: %+v", resolveState)
	}
}

func TestStartupPlannerLayoutFailureDegradesToDefaultWorkspace(t *testing.T) {
	planner := NewStartupPlanner(stubLayoutLoader{
		errs: map[string]error{
			"missing": errors.New("missing layout"),
		},
	})

	plan, err := planner.Plan(context.Background(), Config{
		StartupLayout:     "missing",
		StartupAutoLayout: true,
		DefaultShell:      "/bin/zsh",
		Workspace:         "main",
	})
	if err != nil {
		t.Fatalf("expected missing layout to degrade, got %v", err)
	}
	if len(plan.Warnings) != 1 {
		t.Fatalf("expected one startup warning, got %+v", plan.Warnings)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected degraded default startup task, got %d", len(plan.Tasks))
	}
	if plan.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected degraded startup to avoid broken overlay, got %q", plan.State.UI.Overlay.Kind)
	}
}

func TestStartupPlannerLayoutFailureReturnsErrorWhenAutoLayoutDisabled(t *testing.T) {
	planner := NewStartupPlanner(stubLayoutLoader{
		errs: map[string]error{
			"broken": errors.New("broken layout"),
		},
	})

	_, err := planner.Plan(context.Background(), Config{
		StartupLayout:     "broken",
		StartupAutoLayout: false,
	})
	if err == nil {
		t.Fatal("expected startup layout failure to return error when auto layout is disabled")
	}
}

func TestE2EStartupPlanScenarioLaunchFromLayoutFile(t *testing.T) {
	planner := NewStartupPlanner(stubLayoutLoader{
		docs: map[string]string{
			"demo": "workspace: project-api\ntab: dev\nslot:\n  role: backend-dev\n  hint: env=dev service=api\n",
		},
	})

	plan, err := planner.Plan(context.Background(), Config{
		StartupLayout: "demo",
	})
	if err != nil {
		t.Fatalf("expected startup plan to succeed, got %v", err)
	}

	plan.State.Domain.Terminals[types.TerminalID("term-1")] = types.TerminalRef{
		ID:      types.TerminalID("term-1"),
		Name:    "api-dev",
		State:   types.TerminalRunStateRunning,
		Command: []string{"npm", "run", "dev"},
		Tags:    map[string]string{"service": "api"},
	}
	plan.State.Domain.Terminals[types.TerminalID("term-2")] = types.TerminalRef{
		ID:      types.TerminalID("term-2"),
		Name:    "ops-watch",
		State:   types.TerminalRunStateRunning,
		Command: []string{"journalctl", "-f"},
		Tags:    map[string]string{"team": "ops"},
	}

	model := bt.NewModel(bt.ModelConfig{
		InitialState:  plan.State,
		Mapper:        bt.NewIntentMapper(bt.Config{}),
		Reducer:       reducer.New(),
		EffectHandler: bt.NoopEffectHandler{},
		Renderer:      bt.StaticRenderer{},
	})

	current := model
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyEnter},
		{Type: tea.KeyRunes, Runes: []rune("ops")},
		{Type: tea.KeyEnter},
	} {
		next, _ := current.Update(key)
		current = next.(*bt.Model)
	}

	state := current.State()
	if state.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected resolve flow to close overlay, got %q", state.UI.Overlay.Kind)
	}
	pane := state.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.SlotState != types.PaneSlotConnected || pane.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected layout startup flow to connect selected terminal, got %+v", pane)
	}
}

type stubLayoutLoader struct {
	docs map[string]string
	errs map[string]error
}

func (l stubLayoutLoader) LoadLayout(_ context.Context, ref string) ([]byte, error) {
	if err, ok := l.errs[ref]; ok {
		return nil, err
	}
	if doc, ok := l.docs[ref]; ok {
		return []byte(doc), nil
	}
	return nil, errors.New("layout not found")
}
