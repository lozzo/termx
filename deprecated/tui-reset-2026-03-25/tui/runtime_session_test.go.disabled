package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/domain/types"
)

var errRuntimeSessionBoom = errors.New("runtime session boom")

func TestRuntimeSessionBootstrapperAttachesConnectedTerminalsOnce(t *testing.T) {
	bootstrapper := NewRuntimeSessionBootstrapper()
	state := connectedSessionAppState()
	client := &stubRuntimeSessionClient{
		attachResults: map[string]*protocol.AttachResult{
			"term-1": {Mode: "collaborator", Channel: 11},
			"term-2": {Mode: "collaborator", Channel: 12},
		},
		snapshots: map[string]*protocol.Snapshot{
			"term-1": {TerminalID: "term-1"},
			"term-2": {TerminalID: "term-2"},
		},
	}

	sessions, err := bootstrapper.Bootstrap(context.Background(), client, state)
	if err != nil {
		t.Fatalf("expected runtime bootstrap to succeed, got %v", err)
	}
	if client.eventsCalls != 1 {
		t.Fatalf("expected one global events subscription, got %d", client.eventsCalls)
	}
	if len(client.attachCalls) != 2 {
		t.Fatalf("expected attach per connected terminal, got %d", len(client.attachCalls))
	}
	if len(sessions.Terminals) != 2 {
		t.Fatalf("expected two terminal sessions, got %d", len(sessions.Terminals))
	}
	if sessions.Terminals[types.TerminalID("term-1")].Channel != 11 {
		t.Fatalf("unexpected term-1 session payload: %+v", sessions.Terminals[types.TerminalID("term-1")])
	}
	if sessions.Terminals[types.TerminalID("term-2")].Snapshot == nil || sessions.Terminals[types.TerminalID("term-2")].Snapshot.TerminalID != "term-2" {
		t.Fatalf("expected term-2 snapshot to be retained, got %+v", sessions.Terminals[types.TerminalID("term-2")])
	}
}

func TestRuntimeSessionBootstrapperSharedTerminalAttachesOnlyOnce(t *testing.T) {
	bootstrapper := NewRuntimeSessionBootstrapper()
	state := sharedConnectedSessionAppState()
	client := &stubRuntimeSessionClient{
		attachResults: map[string]*protocol.AttachResult{
			"term-1": {Mode: "collaborator", Channel: 7},
		},
		snapshots: map[string]*protocol.Snapshot{
			"term-1": {TerminalID: "term-1"},
		},
	}

	sessions, err := bootstrapper.Bootstrap(context.Background(), client, state)
	if err != nil {
		t.Fatalf("expected shared terminal runtime bootstrap to succeed, got %v", err)
	}
	if len(client.attachCalls) != 1 {
		t.Fatalf("expected shared terminal to attach once, got %d", len(client.attachCalls))
	}
	if len(sessions.Terminals) != 1 {
		t.Fatalf("expected one deduplicated session, got %d", len(sessions.Terminals))
	}
}

func TestRuntimeSessionBootstrapperAttachFailureStopsPreviousStreams(t *testing.T) {
	bootstrapper := NewRuntimeSessionBootstrapper()
	state := connectedSessionAppState()
	client := &stubRuntimeSessionClient{
		attachResults: map[string]*protocol.AttachResult{
			"term-1": {Mode: "collaborator", Channel: 11},
		},
		snapshots: map[string]*protocol.Snapshot{
			"term-1": {TerminalID: "term-1"},
		},
		attachErrByID: map[string]error{
			"term-2": errRuntimeSessionBoom,
		},
	}

	_, err := bootstrapper.Bootstrap(context.Background(), client, state)
	if err == nil {
		t.Fatal("expected bootstrap failure when attach fails")
	}
	if client.stopCalls != 1 {
		t.Fatalf("expected previous stream stop on attach failure, got %d", client.stopCalls)
	}
}

func TestE2ERuntimeSessionScenarioRestorePlanBootstrapsAttachAndEvents(t *testing.T) {
	store := stubWorkspaceStore{
		domain: types.DomainState{
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
		},
	}
	planner := NewStartupPlannerWithStores(nil, store)
	bootstrapper := NewRuntimeSessionBootstrapper()
	client := &stubRuntimeSessionClient{
		attachResults: map[string]*protocol.AttachResult{
			"term-1": {Mode: "collaborator", Channel: 9},
		},
		snapshots: map[string]*protocol.Snapshot{
			"term-1": {TerminalID: "term-1", Timestamp: time.Date(2026, 3, 23, 11, 0, 0, 0, time.UTC)},
		},
	}

	plan, err := planner.Plan(context.Background(), Config{
		WorkspaceStatePath: "/tmp/workspace-state.json",
	})
	if err != nil {
		t.Fatalf("expected restore plan to succeed, got %v", err)
	}
	sessions, err := bootstrapper.Bootstrap(context.Background(), client, plan.State)
	if err != nil {
		t.Fatalf("expected runtime session bootstrap to succeed, got %v", err)
	}
	if sessions.EventStream == nil {
		t.Fatal("expected runtime bootstrap to retain event stream")
	}
	session, ok := sessions.Terminals[types.TerminalID("term-1")]
	if !ok {
		t.Fatalf("expected restored terminal session, got %+v", sessions.Terminals)
	}
	if session.Channel != 9 || session.Snapshot == nil || session.Snapshot.TerminalID != "term-1" {
		t.Fatalf("unexpected runtime session payload: %+v", session)
	}
}

func connectedSessionAppState() types.AppState {
	return types.AppState{
		Domain: types.DomainState{
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
									ID:         types.PaneID("pane-1"),
									Kind:       types.PaneKindTiled,
									SlotState:  types.PaneSlotConnected,
									TerminalID: types.TerminalID("term-1"),
								},
								types.PaneID("pane-2"): {
									ID:         types.PaneID("pane-2"),
									Kind:       types.PaneKindTiled,
									SlotState:  types.PaneSlotConnected,
									TerminalID: types.TerminalID("term-2"),
								},
							},
							RootSplit: &types.SplitNode{
								Direction: types.SplitDirectionHorizontal,
								First:     &types.SplitNode{PaneID: types.PaneID("pane-1")},
								Second:    &types.SplitNode{PaneID: types.PaneID("pane-2")},
							},
						},
					},
				},
			},
			Terminals: map[types.TerminalID]types.TerminalRef{
				types.TerminalID("term-1"): {ID: types.TerminalID("term-1"), State: types.TerminalRunStateRunning},
				types.TerminalID("term-2"): {ID: types.TerminalID("term-2"), State: types.TerminalRunStateRunning},
			},
			Connections: map[types.TerminalID]types.ConnectionState{
				types.TerminalID("term-1"): {
					TerminalID:       types.TerminalID("term-1"),
					ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1")},
				},
				types.TerminalID("term-2"): {
					TerminalID:       types.TerminalID("term-2"),
					ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-2")},
				},
			},
		},
	}
}

func sharedConnectedSessionAppState() types.AppState {
	state := connectedSessionAppState()
	delete(state.Domain.Terminals, types.TerminalID("term-2"))
	delete(state.Domain.Connections, types.TerminalID("term-2"))
	ws := state.Domain.Workspaces[types.WorkspaceID("ws-1")]
	tab := ws.Tabs[types.TabID("tab-1")]
	pane := tab.Panes[types.PaneID("pane-2")]
	pane.TerminalID = types.TerminalID("term-1")
	tab.Panes[types.PaneID("pane-2")] = pane
	ws.Tabs[types.TabID("tab-1")] = tab
	state.Domain.Workspaces[types.WorkspaceID("ws-1")] = ws
	state.Domain.Connections[types.TerminalID("term-1")] = types.ConnectionState{
		TerminalID:       types.TerminalID("term-1"),
		ConnectedPaneIDs: []types.PaneID{types.PaneID("pane-1"), types.PaneID("pane-2")},
	}
	return state
}

type stubRuntimeSessionClient struct {
	attachResults map[string]*protocol.AttachResult
	attachErrByID map[string]error
	snapshots     map[string]*protocol.Snapshot
	snapshotErr   error
	eventStream   chan protocol.Event
	attachCalls   []string
	eventsCalls   int
	stopCalls     int
}

func (c *stubRuntimeSessionClient) Close() error { return nil }

func (c *stubRuntimeSessionClient) Create(context.Context, []string, string, protocol.Size) (*protocol.CreateResult, error) {
	return nil, nil
}

func (c *stubRuntimeSessionClient) SetTags(context.Context, string, map[string]string) error {
	return nil
}

func (c *stubRuntimeSessionClient) SetMetadata(context.Context, string, string, map[string]string) error {
	return nil
}

func (c *stubRuntimeSessionClient) List(context.Context) (*protocol.ListResult, error) {
	return nil, nil
}

func (c *stubRuntimeSessionClient) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	c.eventsCalls++
	if c.eventStream == nil {
		c.eventStream = make(chan protocol.Event)
	}
	return c.eventStream, nil
}

func (c *stubRuntimeSessionClient) Attach(_ context.Context, terminalID string, _ string) (*protocol.AttachResult, error) {
	c.attachCalls = append(c.attachCalls, terminalID)
	if err := c.attachErrByID[terminalID]; err != nil {
		return nil, err
	}
	if result, ok := c.attachResults[terminalID]; ok {
		return result, nil
	}
	return nil, errRuntimeSessionBoom
}

func (c *stubRuntimeSessionClient) Snapshot(_ context.Context, terminalID string, _, _ int) (*protocol.Snapshot, error) {
	if c.snapshotErr != nil {
		return nil, c.snapshotErr
	}
	if snapshot, ok := c.snapshots[terminalID]; ok {
		return snapshot, nil
	}
	return nil, errRuntimeSessionBoom
}

func (c *stubRuntimeSessionClient) Input(context.Context, uint16, []byte) error { return nil }
func (c *stubRuntimeSessionClient) Resize(context.Context, uint16, uint16, uint16) error {
	return nil
}

func (c *stubRuntimeSessionClient) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	ch := make(chan protocol.StreamFrame)
	stop := func() {
		c.stopCalls++
		close(ch)
	}
	_ = channel
	return ch, stop
}

func (c *stubRuntimeSessionClient) Kill(context.Context, string) error { return nil }
