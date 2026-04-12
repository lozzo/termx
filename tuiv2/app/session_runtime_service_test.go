package app

import (
	"context"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestSessionRuntimeServiceReleaseLeaseCmdRemovesLeaseAndReappliesLocalOwner(t *testing.T) {
	client := &recordingBridgeClient{snapshotByTerminal: map[string]*protocol.Snapshot{}}
	model := setupModel(t, modelOpts{client: client})
	model.sessionID = "session-main"
	model.sessionViewID = "view-local"
	model.sessionLeases = map[string]protocol.LeaseInfo{
		"term-1": {TerminalID: "term-1", SessionID: "session-main", ViewID: "view-remote", PaneID: "pane-remote"},
	}

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.BoundPaneIDs = []string{"pane-1"}
	binding := model.runtime.BindPane("pane-1")
	binding.Channel = 1
	binding.Connected = true
	model.runtime.ApplySessionLeases(model.sessionViewID, model.currentSessionLeases())

	service := model.sessionRuntimeService()
	drainCmd(t, model, service.releaseLeaseCmd("term-1"), 20)

	if len(client.releaseLeaseCalls) != 1 {
		t.Fatalf("expected one release lease call, got %#v", client.releaseLeaseCalls)
	}
	if model.sessionLeases != nil {
		t.Fatalf("expected session lease map cleared, got %#v", model.sessionLeases)
	}
	if terminal.OwnerPaneID != "" || terminal.ControlPaneID != "" || !terminal.RequiresExplicitOwner {
		t.Fatalf("expected lease release to leave terminal awaiting explicit owner, got %#v", terminal)
	}
}

func TestSessionRuntimeServiceReconcileRuntimeUnbindsRemovedAndAttachesNewBindings(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 7, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client})
	oldTerminal := model.runtime.Registry().GetOrCreate("term-old")
	oldTerminal.BoundPaneIDs = []string{"pane-1"}
	oldBinding := model.runtime.BindPane("pane-1")
	oldBinding.Channel = 1
	oldBinding.Connected = true

	service := model.sessionRuntimeService()
	service.reconcileRuntime(context.Background(),
		map[string]string{"pane-1": "term-old"},
		map[string]string{"pane-2": "term-new"},
	)

	if got := model.runtime.Binding("pane-1"); got != nil {
		t.Fatalf("expected pane-1 unbound after reconcile, got %#v", got)
	}
	if got := oldTerminal.BoundPaneIDs; len(got) != 0 {
		t.Fatalf("expected old terminal bindings cleared, got %#v", got)
	}
	if len(client.attachCalls) != 1 || client.attachCalls[0].terminalID != "term-new" {
		t.Fatalf("expected attach for new binding, got %#v", client.attachCalls)
	}
	if binding := model.runtime.Binding("pane-2"); binding == nil || !binding.Connected {
		t.Fatalf("expected pane-2 attached and connected, got %#v", binding)
	}
}

func TestSessionRuntimeServiceReconcileRuntimeSkipsUnchangedConnectedBinding(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 7, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{
		client: client,
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:           "tab-1",
					Name:         "tab 1",
					ActivePaneID: "pane-1",
					Panes: map[string]*workbench.PaneState{
						"pane-1": {ID: "pane-1", TerminalID: "term-1"},
					},
					Root: workbench.NewLeaf("pane-1"),
				}},
			},
		},
	})
	binding := model.runtime.BindPane("pane-1")
	binding.Channel = 1
	binding.Connected = true
	model.runtime.Registry().GetOrCreate("term-1").BoundPaneIDs = []string{"pane-1"}

	service := model.sessionRuntimeService()
	service.reconcileRuntime(context.Background(),
		map[string]string{"pane-1": "term-1"},
		map[string]string{"pane-1": "term-1"},
	)

	if len(client.attachCalls) != 0 {
		t.Fatalf("expected unchanged connected binding to skip attach, got %#v", client.attachCalls)
	}
}

func TestSessionRuntimeServiceReconcileRuntimeReattachesDisconnectedBinding(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 7, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{
		client: client,
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:           "tab-1",
					Name:         "tab 1",
					ActivePaneID: "pane-1",
					Panes: map[string]*workbench.PaneState{
						"pane-1": {ID: "pane-1", TerminalID: "term-1"},
					},
					Root: workbench.NewLeaf("pane-1"),
				}},
			},
		},
	})
	binding := model.runtime.BindPane("pane-1")
	binding.Channel = 1
	binding.Connected = false
	model.runtime.Registry().GetOrCreate("term-1").BoundPaneIDs = []string{"pane-1"}

	service := model.sessionRuntimeService()
	service.reconcileRuntime(context.Background(),
		map[string]string{"pane-1": "term-1"},
		map[string]string{"pane-1": "term-1"},
	)

	if len(client.attachCalls) != 1 || client.attachCalls[0].terminalID != "term-1" {
		t.Fatalf("expected disconnected binding to reattach term-1, got %#v", client.attachCalls)
	}
	if got := model.runtime.Binding("pane-1"); got == nil || !got.Connected {
		t.Fatalf("expected pane-1 binding reconnected after reconcile, got %#v", got)
	}
}
