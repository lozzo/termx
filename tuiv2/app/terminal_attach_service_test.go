package app

import (
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/modal"
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
