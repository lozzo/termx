package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/domain/types"
)

func TestStartupPlannerAttachLaunchBuildsAttachTask(t *testing.T) {
	planner := NewStartupPlanner(nil)

	plan, err := planner.Plan(context.Background(), Config{
		AttachID:  "term-9",
		Workspace: "main",
	})
	if err != nil {
		t.Fatalf("expected attach startup plan to succeed, got %v", err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected one attach task, got %d", len(plan.Tasks))
	}
	task, ok := plan.Tasks[0].(AttachTerminalTask)
	if !ok {
		t.Fatalf("expected attach terminal task, got %T", plan.Tasks[0])
	}
	if task.PaneID != types.PaneID("pane-1") || task.TerminalID != types.TerminalID("term-9") {
		t.Fatalf("unexpected attach task payload: %+v", task)
	}
}

func TestStartupTaskExecutorCreateTerminalCreatesAndConnectsPane(t *testing.T) {
	executor := NewStartupTaskExecutor()
	plan := defaultStartupPlan(Config{
		DefaultShell: "/bin/zsh",
		Workspace:    "main",
	})
	client := &stubStartupClient{
		createResult: &protocol.CreateResult{
			TerminalID: "term-1",
			State:      "running",
		},
	}

	bootstrapped, err := executor.Execute(context.Background(), client, protocol.Size{Cols: 120, Rows: 40}, plan)
	if err != nil {
		t.Fatalf("expected startup execution to succeed, got %v", err)
	}
	if len(bootstrapped.Tasks) != 0 {
		t.Fatalf("expected executed tasks to be cleared, got %d", len(bootstrapped.Tasks))
	}
	if len(client.created) != 1 {
		t.Fatalf("expected one create call, got %d", len(client.created))
	}
	pane := bootstrapped.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.SlotState != types.PaneSlotConnected || pane.TerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected pane to become connected after startup create, got %+v", pane)
	}
	terminal := bootstrapped.State.Domain.Terminals[types.TerminalID("term-1")]
	if terminal.Name != "ws-1-tab-1-pane-1" || len(terminal.Command) != 1 || terminal.Command[0] != "/bin/zsh" {
		t.Fatalf("expected terminal metadata to be retained after startup create, got %+v", terminal)
	}
}

func TestStartupTaskExecutorAttachTaskConnectsRequestedTerminal(t *testing.T) {
	executor := NewStartupTaskExecutor()
	plan, err := NewStartupPlanner(nil).Plan(context.Background(), Config{
		AttachID: "term-9",
	})
	if err != nil {
		t.Fatalf("expected attach startup plan to succeed, got %v", err)
	}
	client := &stubStartupClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{
					ID:      "term-9",
					Name:    "api-dev",
					Command: []string{"npm", "run", "dev"},
					Tags:    map[string]string{"service": "api"},
					State:   "running",
				},
			},
		},
	}

	bootstrapped, err := executor.Execute(context.Background(), client, protocol.Size{}, plan)
	if err != nil {
		t.Fatalf("expected attach startup execution to succeed, got %v", err)
	}
	if client.listCalls != 1 {
		t.Fatalf("expected one list call for attach bootstrap, got %d", client.listCalls)
	}
	pane := bootstrapped.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.SlotState != types.PaneSlotConnected || pane.TerminalID != types.TerminalID("term-9") {
		t.Fatalf("expected pane to connect requested terminal, got %+v", pane)
	}
	terminal := bootstrapped.State.Domain.Terminals[types.TerminalID("term-9")]
	if terminal.Name != "api-dev" || terminal.State != types.TerminalRunStateRunning {
		t.Fatalf("expected terminal metadata to be copied from list result, got %+v", terminal)
	}
}

func TestStartupTaskExecutorAttachMissingTerminalReturnsError(t *testing.T) {
	executor := NewStartupTaskExecutor()
	plan, err := NewStartupPlanner(nil).Plan(context.Background(), Config{
		AttachID: "term-missing",
	})
	if err != nil {
		t.Fatalf("expected attach startup plan to succeed, got %v", err)
	}
	client := &stubStartupClient{
		listResult: &protocol.ListResult{},
	}

	_, err = executor.Execute(context.Background(), client, protocol.Size{}, plan)
	if err == nil {
		t.Fatal("expected missing attach terminal to return error")
	}
}

func TestE2EStartupBootstrapScenarioDefaultLaunchCreatesWorkingPane(t *testing.T) {
	planner := NewStartupPlanner(nil)
	executor := NewStartupTaskExecutor()
	client := &stubStartupClient{
		createResult: &protocol.CreateResult{
			TerminalID: "term-1",
			State:      "running",
		},
	}

	plan, err := planner.Plan(context.Background(), Config{
		DefaultShell: "/bin/zsh",
		Workspace:    "main",
	})
	if err != nil {
		t.Fatalf("expected startup plan to succeed, got %v", err)
	}
	bootstrapped, err := executor.Execute(context.Background(), client, protocol.Size{Cols: 100, Rows: 30}, plan)
	if err != nil {
		t.Fatalf("expected startup bootstrap to succeed, got %v", err)
	}

	if bootstrapped.State.UI.Overlay.Kind != types.OverlayNone {
		t.Fatalf("expected default launch to avoid overlay, got %q", bootstrapped.State.UI.Overlay.Kind)
	}
	pane := bootstrapped.State.Domain.Workspaces[types.WorkspaceID("ws-1")].Tabs[types.TabID("tab-1")].Panes[types.PaneID("pane-1")]
	if pane.SlotState != types.PaneSlotConnected || pane.TerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected default launch to end in connected pane, got %+v", pane)
	}
}

type stubStartupClient struct {
	createResult *protocol.CreateResult
	createErr    error
	listResult   *protocol.ListResult
	listErr      error
	created      []protocol.CreateParams
	listCalls    int
}

func (c *stubStartupClient) Close() error { return nil }

func (c *stubStartupClient) Create(_ context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	c.created = append(c.created, protocol.CreateParams{
		Command: command,
		Name:    name,
		Size:    size,
	})
	if c.createErr != nil {
		return nil, c.createErr
	}
	if c.createResult != nil {
		return c.createResult, nil
	}
	return nil, errors.New("missing create result")
}

func (c *stubStartupClient) SetTags(context.Context, string, map[string]string) error { return nil }
func (c *stubStartupClient) SetMetadata(context.Context, string, string, map[string]string) error {
	return nil
}

func (c *stubStartupClient) List(context.Context) (*protocol.ListResult, error) {
	c.listCalls++
	if c.listErr != nil {
		return nil, c.listErr
	}
	if c.listResult != nil {
		return c.listResult, nil
	}
	return &protocol.ListResult{}, nil
}

func (c *stubStartupClient) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	ch := make(chan protocol.Event)
	close(ch)
	return ch, nil
}

func (c *stubStartupClient) Attach(context.Context, string, string) (*protocol.AttachResult, error) {
	return nil, nil
}

func (c *stubStartupClient) Snapshot(context.Context, string, int, int) (*protocol.Snapshot, error) {
	return nil, nil
}

func (c *stubStartupClient) Input(context.Context, uint16, []byte) error          { return nil }
func (c *stubStartupClient) Resize(context.Context, uint16, uint16, uint16) error { return nil }
func (c *stubStartupClient) Stream(uint16) (<-chan protocol.StreamFrame, func()) {
	ch := make(chan protocol.StreamFrame)
	close(ch)
	return ch, func() {}
}
func (c *stubStartupClient) Kill(context.Context, string) error { return nil }
