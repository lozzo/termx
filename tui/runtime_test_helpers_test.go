package tui

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/lozzow/termx/protocol"
	btui "github.com/lozzow/termx/tui/bt"
	"github.com/lozzow/termx/tui/domain/types"
)

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

type runtimeResizeCall struct {
	channel uint16
	cols    uint16
	rows    uint16
}

// connectedRunAppState 构造一个最小但真实的“pane 已连接 terminal”的运行时状态，
// 供输入、resize、运行编排等非渲染测试复用。
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

type stubRunClient struct {
	snapshots   map[string]*protocol.Snapshot
	snapshotErr error
}

func (c *stubRunClient) Close() error { return nil }

func (c *stubRunClient) Create(_ context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	return &protocol.CreateResult{
		TerminalID: fmt.Sprintf("term-created-%s", name),
		State:      string(types.TerminalRunStateRunning),
	}, nil
}

func (c *stubRunClient) SetTags(context.Context, string, map[string]string) error { return nil }

func (c *stubRunClient) SetMetadata(context.Context, string, string, map[string]string) error {
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

func (c *stubRunClient) Input(context.Context, uint16, []byte) error { return nil }

func (c *stubRunClient) Resize(context.Context, uint16, uint16, uint16) error { return nil }

func (c *stubRunClient) Stream(uint16) (<-chan protocol.StreamFrame, func()) {
	ch := make(chan protocol.StreamFrame)
	close(ch)
	return ch, func() {}
}

func (c *stubRunClient) Kill(context.Context, string) error { return nil }

func (c *stubRunClient) ConnectTerminalInNewTab(types.WorkspaceID, types.TerminalID) error {
	return nil
}

func (c *stubRunClient) ConnectTerminalInFloatingPane(types.WorkspaceID, types.TabID, types.TerminalID) error {
	return nil
}
