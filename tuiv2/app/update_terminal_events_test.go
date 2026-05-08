package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestHandleTerminalEventMessageResizeReloadsSnapshotWhenStreamIdle(t *testing.T) {
	client := &recordingBridgeClient{
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-1": {TerminalID: "term-1", Size: protocol.Size{Cols: 88, Rows: 26}},
		},
	}
	model := setupModel(t, modelOpts{client: client})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "running"

	cmd, handled := model.handleTerminalEventMessage(terminalEventMsg{Event: protocol.Event{
		Type:       protocol.EventTerminalResized,
		TerminalID: "term-1",
	}})
	if !handled || cmd == nil {
		t.Fatalf("expected terminal resize event handled with reload cmd, got handled=%v cmd=%#v", handled, cmd)
	}
	msg := cmd()
	if _, ok := msg.(orchestrator.SnapshotLoadedMsg); !ok {
		t.Fatalf("expected snapshot reload message, got %#v", msg)
	}
}

func TestHandleTerminalEventMessageResizeSkipsReloadWhenStreamActive(t *testing.T) {
	model := setupModel(t, modelOpts{})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Stream.Active = true

	cmd, handled := model.handleTerminalEventMessage(terminalEventMsg{Event: protocol.Event{
		Type:       protocol.EventTerminalResized,
		TerminalID: "term-1",
	}})
	if !handled || cmd != nil {
		t.Fatalf("expected active-stream resize event handled without reload cmd, got handled=%v cmd=%#v", handled, cmd)
	}
}

func TestHandleTerminalEventMessageResizeIgnoresMissingTerminal(t *testing.T) {
	model := setupModel(t, modelOpts{})

	cmd, handled := model.handleTerminalEventMessage(terminalEventMsg{Event: protocol.Event{
		Type:       protocol.EventTerminalResized,
		TerminalID: "term-missing",
	}})
	if !handled || cmd != nil {
		t.Fatalf("expected missing-terminal resize event handled without cmd, got handled=%v cmd=%#v", handled, cmd)
	}
}

func TestHandleTerminalEventMessageUnknownEventFallsThroughAsHandled(t *testing.T) {
	model := setupModel(t, modelOpts{})

	cmd, handled := model.handleTerminalEventMessage(terminalEventMsg{Event: protocol.Event{
		Type:       protocol.EventTerminalCreated,
		TerminalID: "term-1",
	}})
	if !handled || cmd != nil {
		t.Fatalf("expected unknown terminal event handled without cmd, got handled=%v cmd=%#v", handled, cmd)
	}
}

func TestHandleTerminalEventMessageRemovedClearsLocalState(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.runtime.Registry().Get("term-1").BoundPaneIDs = []string{"pane-1"}

	cmd, handled := model.handleTerminalEventMessage(terminalEventMsg{Event: protocol.Event{
		Type:       protocol.EventTerminalRemoved,
		TerminalID: "term-1",
	}})
	if !handled || cmd != nil {
		t.Fatalf("expected removed event handled without cmd, got handled=%v cmd=%#v", handled, cmd)
	}
	if pane := model.workbench.CurrentTab().Panes["pane-1"]; pane == nil || pane.TerminalID != "" {
		t.Fatalf("expected pane terminal binding cleared, got %#v", pane)
	}
	if got := model.runtime.Registry().Get("term-1"); got != nil {
		t.Fatalf("expected runtime terminal removed, got %#v", got)
	}
	if got := model.runtime.Binding("pane-1"); got != nil {
		t.Fatalf("expected runtime pane binding removed, got %#v", got)
	}
}

func TestHandleTerminalEventMessageRemovedClearsAllWorkspaceBindings(t *testing.T) {
	model := setupModel(t, modelOpts{workspaces: map[string]*workbench.WorkspaceState{
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
		"ops": {
			Name:      "ops",
			ActiveTab: 0,
			Tabs: []*workbench.TabState{{
				ID:           "tab-2",
				Name:         "tab 2",
				ActivePaneID: "pane-2",
				Panes: map[string]*workbench.PaneState{
					"pane-2": {ID: "pane-2", TerminalID: "term-1"},
				},
				Root: workbench.NewLeaf("pane-2"),
			}},
		},
	}})
	model.runtime.Registry().GetOrCreate("term-1").BoundPaneIDs = []string{"pane-1", "pane-2"}
	model.runtime.BindPane("pane-1")
	model.runtime.BindPane("pane-2")

	_, handled := model.handleTerminalEventMessage(terminalEventMsg{Event: protocol.Event{
		Type:       protocol.EventTerminalRemoved,
		TerminalID: "term-1",
	}})
	if !handled {
		t.Fatal("expected removed event handled")
	}
	for _, wsName := range model.workbench.ListWorkspaces() {
		ws := model.workbench.WorkspaceByName(wsName)
		for _, tab := range ws.Tabs {
			for _, pane := range tab.Panes {
				if pane.TerminalID != "" {
					t.Fatalf("expected all bindings cleared, got pane %#v", pane)
				}
			}
		}
	}
	if got := model.runtime.Binding("pane-1"); got != nil {
		t.Fatalf("expected pane-1 runtime binding removed, got %#v", got)
	}
	if got := model.runtime.Binding("pane-2"); got != nil {
		t.Fatalf("expected pane-2 runtime binding removed, got %#v", got)
	}
}

func TestHandleTerminalEventMessageFallsThroughForNonTerminalEventMsg(t *testing.T) {
	model := setupModel(t, modelOpts{})
	cmd, handled := model.handleTerminalEventMessage(tea.WindowSizeMsg{Width: 80, Height: 24})
	if handled || cmd != nil {
		t.Fatalf("expected non-terminal event msg to fall through, got handled=%v cmd=%#v", handled, cmd)
	}
}
