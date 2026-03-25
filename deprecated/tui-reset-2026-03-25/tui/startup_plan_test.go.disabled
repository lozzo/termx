package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
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

func TestStartupPlannerRestoreStoreBuildsStateWithoutStartupTasks(t *testing.T) {
	store := stubWorkspaceStore{
		domain: types.DomainState{
			ActiveWorkspaceID: types.WorkspaceID("ws-restore"),
			WorkspaceOrder:    []types.WorkspaceID{types.WorkspaceID("ws-restore")},
			Workspaces: map[types.WorkspaceID]types.WorkspaceState{
				types.WorkspaceID("ws-restore"): {
					ID:          types.WorkspaceID("ws-restore"),
					Name:        "project-api",
					ActiveTabID: types.TabID("tab-dev"),
					TabOrder:    []types.TabID{types.TabID("tab-dev")},
					Tabs: map[types.TabID]types.TabState{
						types.TabID("tab-dev"): {
							ID:           types.TabID("tab-dev"),
							Name:         "dev",
							ActivePaneID: types.PaneID("pane-api"),
							ActiveLayer:  types.FocusLayerTiled,
							Panes: map[types.PaneID]types.PaneState{
								types.PaneID("pane-api"): {
									ID:         types.PaneID("pane-api"),
									Kind:       types.PaneKindTiled,
									SlotState:  types.PaneSlotConnected,
									TerminalID: types.TerminalID("term-api"),
								},
							},
							RootSplit: &types.SplitNode{PaneID: types.PaneID("pane-api")},
						},
					},
				},
			},
			Terminals: map[types.TerminalID]types.TerminalRef{
				types.TerminalID("term-api"): {
					ID:      types.TerminalID("term-api"),
					Name:    "api-dev",
					Command: []string{"npm", "run", "dev"},
					State:   types.TerminalRunStateRunning,
				},
			},
			Connections: map[types.TerminalID]types.ConnectionState{
				types.TerminalID("term-api"): {
					TerminalID:       types.TerminalID("term-api"),
					ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-api")},
					OwnerPaneID:      types.PaneID("pane-api"),
				},
			},
		},
	}
	planner := NewStartupPlannerWithStores(nil, store)

	plan, err := planner.Plan(context.Background(), Config{
		WorkspaceStatePath: "/tmp/workspace-state.json",
	})
	if err != nil {
		t.Fatalf("expected restore plan to succeed, got %v", err)
	}
	if len(plan.Tasks) != 0 {
		t.Fatalf("expected restored startup to need no bootstrap tasks, got %d", len(plan.Tasks))
	}
	if plan.State.Domain.ActiveWorkspaceID != types.WorkspaceID("ws-restore") {
		t.Fatalf("expected restored workspace to become active, got %q", plan.State.Domain.ActiveWorkspaceID)
	}
	if plan.State.UI.Focus.WorkspaceID != types.WorkspaceID("ws-restore") || plan.State.UI.Focus.PaneID != types.PaneID("pane-api") {
		t.Fatalf("expected focus to be derived from restored active pane, got %+v", plan.State.UI.Focus)
	}
}

func TestStartupPlannerRestoreStoreFailureDegradesToDefaultWorkspace(t *testing.T) {
	planner := NewStartupPlannerWithStores(nil, stubWorkspaceStore{
		err: errors.New("decode failed"),
	})

	plan, err := planner.Plan(context.Background(), Config{
		WorkspaceStatePath: "/tmp/workspace-state.json",
		DefaultShell:       "/bin/zsh",
		Workspace:          "main",
	})
	if err != nil {
		t.Fatalf("expected restore failure to degrade, got %v", err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected degraded startup to rebuild default create task, got %d", len(plan.Tasks))
	}
	if len(plan.Warnings) != 1 {
		t.Fatalf("expected restore degradation warning, got %+v", plan.Warnings)
	}
}

func TestStartupPlannerMissingRestoreStoreFallsBackWithoutWarning(t *testing.T) {
	planner := NewStartupPlannerWithStores(nil, stubWorkspaceStore{
		err: os.ErrNotExist,
	})

	plan, err := planner.Plan(context.Background(), Config{
		WorkspaceStatePath: "/tmp/workspace-state.json",
		DefaultShell:       "/bin/zsh",
	})
	if err != nil {
		t.Fatalf("expected missing restore state to fall back, got %v", err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected fallback default startup task, got %d", len(plan.Tasks))
	}
	if len(plan.Warnings) != 0 {
		t.Fatalf("expected missing restore state to stay quiet, got %+v", plan.Warnings)
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

func TestWorkspaceStoreFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace-state.json")
	store := fileWorkspaceStore{}
	want := types.DomainState{
		ActiveWorkspaceID: types.WorkspaceID("ws-1"),
		WorkspaceOrder:    []types.WorkspaceID{types.WorkspaceID("ws-1")},
		Workspaces: map[types.WorkspaceID]types.WorkspaceState{
			types.WorkspaceID("ws-1"): {
				ID:          types.WorkspaceID("ws-1"),
				Name:        "main",
				ActiveTabID: types.TabID("tab-1"),
				TabOrder:    []types.TabID{types.TabID("tab-1")},
				Tabs: map[types.TabID]types.TabState{
					types.TabID("tab-1"): {
						ID:           types.TabID("tab-1"),
						Name:         "shell",
						ActivePaneID: types.PaneID("pane-1"),
						ActiveLayer:  types.FocusLayerTiled,
						Panes: map[types.PaneID]types.PaneState{
							types.PaneID("pane-1"): {
								ID:        types.PaneID("pane-1"),
								Kind:      types.PaneKindTiled,
								SlotState: types.PaneSlotEmpty,
							},
						},
						RootSplit: &types.SplitNode{PaneID: types.PaneID("pane-1")},
					},
				},
			},
		},
		Terminals:   map[types.TerminalID]types.TerminalRef{},
		Connections: map[types.TerminalID]types.ConnectionState{},
	}
	content, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal restore state: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write restore state: %v", err)
	}

	got, err := store.LoadWorkspace(context.Background(), path)
	if err != nil {
		t.Fatalf("expected workspace store to load file, got %v", err)
	}
	if got.ActiveWorkspaceID != want.ActiveWorkspaceID || got.Workspaces[types.WorkspaceID("ws-1")].Name != "main" {
		t.Fatalf("unexpected loaded workspace state: %+v", got)
	}
}

func TestWorkspaceStoreSaveWorkspaceWritesRoundTripFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace-state.json")
	store := fileWorkspaceStore{}
	want := types.DomainState{
		ActiveWorkspaceID: types.WorkspaceID("ws-save"),
		WorkspaceOrder:    []types.WorkspaceID{types.WorkspaceID("ws-save")},
		Workspaces: map[types.WorkspaceID]types.WorkspaceState{
			types.WorkspaceID("ws-save"): {
				ID:          types.WorkspaceID("ws-save"),
				Name:        "saved",
				ActiveTabID: types.TabID("tab-1"),
				TabOrder:    []types.TabID{types.TabID("tab-1")},
				Tabs: map[types.TabID]types.TabState{
					types.TabID("tab-1"): {
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
					},
				},
			},
		},
		Terminals: map[types.TerminalID]types.TerminalRef{
			types.TerminalID("term-1"): {
				ID:      types.TerminalID("term-1"),
				Name:    "api-dev",
				Command: []string{"npm", "run", "dev"},
				State:   types.TerminalRunStateRunning,
			},
		},
		Connections: map[types.TerminalID]types.ConnectionState{
			types.TerminalID("term-1"): {
				TerminalID:       types.TerminalID("term-1"),
				ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1")},
				OwnerPaneID:      types.PaneID("pane-1"),
			},
		},
	}

	if err := store.SaveWorkspace(context.Background(), path, want); err != nil {
		t.Fatalf("expected save workspace to succeed, got %v", err)
	}
	got, err := store.LoadWorkspace(context.Background(), path)
	if err != nil {
		t.Fatalf("expected saved workspace to load, got %v", err)
	}
	if got.ActiveWorkspaceID != want.ActiveWorkspaceID || got.Workspaces[types.WorkspaceID("ws-save")].Name != "saved" {
		t.Fatalf("unexpected saved workspace round-trip: %+v", got)
	}
}

func TestE2ERestoreSaveAndReloadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace-state.json")
	store := fileWorkspaceStore{}
	planner := NewStartupPlannerWithStores(nil, store)
	executor := NewStartupTaskExecutor()
	client := &stubStartupClient{
		createResult: &protocol.CreateResult{
			TerminalID: "term-1",
			State:      "running",
		},
	}

	plan, err := planner.Plan(context.Background(), Config{
		DefaultShell:       "/bin/zsh",
		Workspace:          "main",
		WorkspaceStatePath: path,
	})
	if err != nil {
		t.Fatalf("expected initial plan to succeed, got %v", err)
	}
	bootstrapped, err := executor.Execute(context.Background(), client, protocol.Size{Cols: 100, Rows: 30}, plan)
	if err != nil {
		t.Fatalf("expected startup execution to succeed, got %v", err)
	}
	if err := store.SaveWorkspace(context.Background(), path, bootstrapped.State.Domain); err != nil {
		t.Fatalf("expected save after bootstrap to succeed, got %v", err)
	}

	restored, err := planner.Plan(context.Background(), Config{
		WorkspaceStatePath: path,
	})
	if err != nil {
		t.Fatalf("expected restore plan to succeed, got %v", err)
	}
	if len(restored.Tasks) != 0 {
		t.Fatalf("expected restored plan to avoid bootstrap tasks, got %d", len(restored.Tasks))
	}
	pane := restored.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.SlotState != types.PaneSlotConnected || pane.TerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected restored state to keep connected pane, got %+v", pane)
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

type stubWorkspaceStore struct {
	domain types.DomainState
	err    error
}

func (s stubWorkspaceStore) LoadWorkspace(context.Context, string) (types.DomainState, error) {
	if s.err != nil {
		return types.DomainState{}, s.err
	}
	return s.domain, nil
}

func (s stubWorkspaceStore) SaveWorkspace(context.Context, string, types.DomainState) error {
	return s.err
}
