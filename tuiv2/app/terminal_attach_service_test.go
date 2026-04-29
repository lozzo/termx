package app

import (
	"os"
	"testing"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/bootstrap"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestTerminalAttachServiceRestartAndAttachCmdRestartsThenAttaches(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 7, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-1": {
				TerminalID: "term-1",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "exited"
	terminal.ExitCode = intPtr(23)

	service := model.terminalAttachService()
	drainCmd(t, model, service.restartAndAttachCmd("pane-1", "term-1"), 20)

	if len(client.restartCalls) != 1 || client.restartCalls[0] != "term-1" {
		t.Fatalf("expected restart for term-1, got %#v", client.restartCalls)
	}
	if len(client.attachCalls) != 1 || client.attachCalls[0].terminalID != "term-1" {
		t.Fatalf("expected attach after restart for term-1, got %#v", client.attachCalls)
	}
	if model.isPaneAttachPending("pane-1") {
		t.Fatal("expected pending attach cleared after restart attach")
	}
	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "term-1" {
		t.Fatalf("expected pane-1 attached to term-1, got %#v", pane)
	}
}

func TestTerminalAttachServiceCreateAndAttachCmdCreatesAndAttachesReplacement(t *testing.T) {
	client := &recordingBridgeClient{
		createResult: &protocol.CreateResult{TerminalID: "term-new", State: "running"},
		attachResult: &protocol.AttachResult{Channel: 9, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-new": {
				TerminalID: "term-new",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "k", Width: 1}}}},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client})
	service := model.terminalAttachService()

	cmd := service.createAndAttachCmd("pane-1", modal.CreateTargetReplace, protocol.CreateParams{
		Name:    "demo",
		Command: []string{"/bin/sh"},
		Tags:    map[string]string{"role": "dev"},
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	drainCmd(t, model, cmd, 20)

	if len(client.createCalls) != 1 {
		t.Fatalf("expected one create call, got %#v", client.createCalls)
	}
	if len(client.attachCalls) != 1 || client.attachCalls[0].terminalID != "term-new" {
		t.Fatalf("expected attach of created terminal, got %#v", client.attachCalls)
	}
	if model.isPaneAttachPending("pane-1") {
		t.Fatal("expected pending attach cleared after create attach")
	}
	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "term-new" {
		t.Fatalf("expected pane-1 attached to created terminal, got %#v", pane)
	}
	terminal := model.runtime.Registry().Get("term-new")
	if terminal == nil {
		t.Fatal("expected created terminal runtime")
	}
	if terminal.Name != "demo" || terminal.Tags["role"] != "dev" {
		t.Fatalf("expected runtime metadata primed from create params, got %#v", terminal)
	}
}

func TestTerminalAttachServiceCreateAndAttachCmdSplitTargetClearsOriginalPendingAttach(t *testing.T) {
	client := &recordingBridgeClient{
		createResult: &protocol.CreateResult{TerminalID: "term-new", State: "running"},
		attachResult: &protocol.AttachResult{Channel: 5, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-new": {
				TerminalID: "term-new",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client})
	service := model.terminalAttachService()

	drainCmd(t, model, service.createAndAttachCmd("pane-1", modal.CreateTargetSplit, protocol.CreateParams{
		Name:    "split-demo",
		Command: []string{"/bin/sh"},
		Size:    protocol.Size{Cols: 80, Rows: 24},
	}), 20)

	tab := model.workbench.CurrentTab()
	if tab == nil || len(tab.Panes) != 2 {
		t.Fatalf("expected split to create a second pane, got %#v", tab)
	}
	var newPaneID string
	for paneID := range tab.Panes {
		if paneID != "pane-1" {
			newPaneID = paneID
			break
		}
	}
	if newPaneID == "" {
		t.Fatal("expected generated pane for split attach")
	}
	if model.isPaneAttachPending("pane-1") {
		t.Fatal("expected original pane pending attach cleared after split create flow")
	}
	if model.isPaneAttachPending(newPaneID) {
		t.Fatal("expected new pane pending attach cleared after split create flow")
	}
	if pane := tab.Panes[newPaneID]; pane == nil || pane.TerminalID != "term-new" {
		t.Fatalf("expected new pane attached to created terminal, got %#v", pane)
	}
}

func TestTerminalAttachServiceAttachCmdResizesHiddenTabPane(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 11, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-hidden": {
				TerminalID: "term-hidden",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
	}
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{
			{
				ID:           "tab-1",
				Name:         "1",
				ActivePaneID: "pane-1",
				Panes: map[string]*workbench.PaneState{
					"pane-1": {ID: "pane-1"},
				},
				Root: workbench.NewLeaf("pane-1"),
			},
			{
				ID:           "tab-2",
				Name:         "2",
				ActivePaneID: "pane-2",
				Panes: map[string]*workbench.PaneState{
					"pane-2": {ID: "pane-2"},
				},
				Root: workbench.NewLeaf("pane-2"),
			},
		},
	})
	model := New(shared.Config{}, wb, runtime.New(client))
	model.width = 120
	model.height = 40
	service := model.terminalAttachService()

	drainCmd(t, model, service.attachCmd("tab-2", "pane-2", "term-hidden"), 20)

	tab := wb.CurrentWorkspace().Tabs[1]
	if pane := tab.Panes["pane-2"]; pane == nil || pane.TerminalID != "term-hidden" {
		t.Fatalf("expected hidden-tab pane bound to term-hidden, got %#v", pane)
	}
	if got := len(client.resizes); got != 1 {
		t.Fatalf("expected one resize for hidden tab attach, got %#v", client.resizes)
	}
}

func TestTerminalAttachServiceHandleAttachedMsgResetsLegacyTabScrollOffset(t *testing.T) {
	model := setupModel(t, modelOpts{})
	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	_ = model.workbench.SetTabScrollOffset(tab.ID, 4)

	service := model.terminalAttachService()
	if service == nil {
		t.Fatal("expected attach service")
	}
	service.handleAttachedMsg(orchestrator.TerminalAttachedMsg{
		TabID:      tab.ID,
		PaneID:     "pane-1",
		TerminalID: "term-1",
	})

	if got := model.workbench.CurrentTab().ScrollOffset; got != 0 {
		t.Fatalf("expected handleAttachedMsg to reset tab scroll offset, got %d", got)
	}
}

func TestTerminalAttachServiceAttachCmdRollsBackRuntimeOnLateFailure(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 0, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-new": {
				TerminalID: "term-new",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client})
	oldTerminal := model.runtime.Registry().GetOrCreate("term-1")
	oldTerminal.OwnerPaneID = "pane-1"
	oldTerminal.ControlPaneID = "pane-1"
	oldTerminal.BoundPaneIDs = []string{"pane-1"}
	oldTerminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "l", Width: 1}, {Content: "d", Width: 1}}}},
	}
	model.runtime.RefreshSnapshotFromVTerm("term-1")
	oldBinding := model.runtime.BindPane("pane-1")
	oldBinding.Channel = 7
	oldBinding.Connected = true
	oldBinding.Role = runtime.BindingRoleOwner

	service := model.terminalAttachService()
	drainCmd(t, model, service.attachCmd("", "pane-1", "term-new"), 20)

	if model.err == nil {
		t.Fatal("expected attach error after stream start failure")
	}
	if binding := model.runtime.Binding("pane-1"); binding == nil || binding.Channel != 7 || !binding.Connected || binding.Role != runtime.BindingRoleOwner {
		t.Fatalf("expected pane binding restored after late attach failure, got %#v", binding)
	}
	if oldTerminal.OwnerPaneID != "pane-1" || oldTerminal.ControlPaneID != "pane-1" || len(oldTerminal.BoundPaneIDs) != 1 || oldTerminal.BoundPaneIDs[0] != "pane-1" {
		t.Fatalf("expected old terminal control restored after late attach failure, got %#v", oldTerminal)
	}
	if oldTerminal.Snapshot == nil || oldTerminal.Snapshot.TerminalID != "term-1" {
		t.Fatalf("expected old terminal snapshot restored after late attach failure, got %#v", oldTerminal)
	}
	if newTerminal := model.runtime.Registry().Get("term-new"); newTerminal != nil && len(newTerminal.BoundPaneIDs) != 0 {
		t.Fatalf("expected failed target terminal to release pane binding, got %#v", newTerminal)
	}
	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "term-1" {
		t.Fatalf("expected workbench binding to remain on original terminal, got %#v", pane)
	}
}

func TestTerminalAttachServicePreparedSplitTargetRollsBackWorkbenchOnLateFailure(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 0, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-new": {
				TerminalID: "term-new",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client})
	originalTab := model.workbench.CurrentTab()
	if originalTab == nil {
		t.Fatal("expected current tab")
	}
	service := model.terminalAttachService()

	drainCmd(t, model, service.splitAndAttachCmd("pane-1", "term-new"), 20)

	tab := model.workbench.CurrentTab()
	if tab == nil || tab.ID != originalTab.ID {
		t.Fatalf("expected rollback to restore original tab, got %#v", tab)
	}
	if len(tab.Panes) != 1 || tab.Panes["pane-1"] == nil {
		t.Fatalf("expected rollback to remove prepared split pane, got %#v", tab.Panes)
	}
	if tab.ActivePaneID != "pane-1" {
		t.Fatalf("expected rollback to restore original focused pane, got %#v", tab)
	}
}

func TestTerminalAttachServicePreparedTargetFailureDoesNotPersistPhantomState(t *testing.T) {
	statePath := t.TempDir() + "/workspace-state.json"
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 0, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-new": {
				TerminalID: "term-new",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client, statePath: statePath})

	drainCmd(t, model, model.createTabAndAttachTerminalCmd("term-new"), 20)

	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected failed prepared attach not to persist temporary state, err=%v", err)
	}
	ws := model.workbench.CurrentWorkspace()
	if ws == nil || len(ws.Tabs) != 1 {
		t.Fatalf("expected rollback to remove prepared tab after failure, got %#v", ws)
	}
}

func TestTerminalAttachServiceOrdinaryAttachRollsBackOriginalFocusOnLateFailure(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 0, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-new": {
				TerminalID: "term-new",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
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
						"pane-1": {ID: "pane-1", TerminalID: "term-old", Title: "old"},
						"pane-2": {ID: "pane-2", Title: "empty"},
					},
					Root: &workbench.LayoutNode{
						Direction: workbench.SplitVertical,
						Ratio:     0.5,
						First:     workbench.NewLeaf("pane-1"),
						Second:    workbench.NewLeaf("pane-2"),
					},
				}},
			},
		},
	})
	service := model.terminalAttachService()

	drainCmd(t, model, service.attachCmd("tab-1", "pane-2", "term-new"), 20)

	tab := model.workbench.CurrentTab()
	if tab == nil || tab.ActivePaneID != "pane-1" {
		t.Fatalf("expected rollback to restore original active pane, got %#v", tab)
	}
}

func TestTerminalAttachServiceLateFailureRestoresSharedTerminalFollowerTitles(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 0, Mode: "collaborator"},
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:    "term-shared",
			Name:  "shared-new",
			State: "running",
		}}},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-shared": {
				TerminalID: "term-shared",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
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
						"pane-1": {ID: "pane-1", Title: "shared-old", TerminalID: "term-shared"},
						"pane-2": {ID: "pane-2", Title: "empty"},
					},
					Root: &workbench.LayoutNode{
						Direction: workbench.SplitVertical,
						Ratio:     0.5,
						First:     workbench.NewLeaf("pane-1"),
						Second:    workbench.NewLeaf("pane-2"),
					},
				}},
			},
		},
	})
	service := model.terminalAttachService()

	drainCmd(t, model, service.attachCmd("tab-1", "pane-2", "term-shared"), 20)

	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if pane := tab.Panes["pane-1"]; pane == nil || pane.Title != "shared-old" {
		t.Fatalf("expected rollback to restore follower pane title, got %#v", pane)
	}
}

func TestTerminalAttachServiceReattachRestoredCmdClearsBindingAndPendingOnFailure(t *testing.T) {
	client := &recordingBridgeClient{attachErr: teaErr("terminal not found"), snapshotByTerminal: map[string]*protocol.Snapshot{}}
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
						"pane-1": {ID: "pane-1", TerminalID: "term-missing"},
					},
					Root: workbench.NewLeaf("pane-1"),
				}},
			},
		},
	})
	service := model.terminalAttachService()

	cmd := service.reattachRestoredCmd(bootstrap.PaneReattachHint{
		TabID:      "tab-1",
		PaneID:     "pane-1",
		TerminalID: "term-missing",
	})
	if cmd == nil {
		t.Fatal("expected restore reattach cmd")
	}
	msg := cmd()
	if _, ok := msg.(reattachFailedMsg); !ok {
		t.Fatalf("expected reattachFailedMsg, got %#v", msg)
	}
	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "" {
		t.Fatalf("expected restored binding cleared after failure, got %#v", pane)
	}
	if model.isPaneAttachPending("pane-1") {
		t.Fatal("expected pending attach cleared after failed restore reattach")
	}
}

func TestTerminalAttachServiceReattachRestoredCmdSucceedsAndClearsPending(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 17, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-restore": {
				TerminalID: "term-restore",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "k", Width: 1}}}},
			},
		},
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
						"pane-1": {ID: "pane-1", TerminalID: "term-restore"},
					},
					Root: workbench.NewLeaf("pane-1"),
				}},
			},
		},
	})
	service := model.terminalAttachService()

	cmd := service.reattachRestoredCmd(bootstrap.PaneReattachHint{
		TabID:      "tab-1",
		PaneID:     "pane-1",
		TerminalID: "term-restore",
	})
	if cmd == nil {
		t.Fatal("expected restore reattach cmd")
	}
	msg := cmd()
	applyTestMsg(t, model, msg, "restore reattach success")

	if len(client.attachCalls) != 1 || client.attachCalls[0].terminalID != "term-restore" {
		t.Fatalf("expected one successful reattach for term-restore, got %#v", client.attachCalls)
	}
	if model.isPaneAttachPending("pane-1") {
		t.Fatal("expected pending attach cleared after successful restore reattach")
	}
	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "term-restore" {
		t.Fatalf("expected restored pane to stay bound to term-restore, got %#v", pane)
	}
}
