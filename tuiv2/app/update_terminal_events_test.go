package app

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/modal"
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
		Type:       protocol.EventTerminalReadError,
		TerminalID: "term-1",
	}})
	if !handled || cmd != nil {
		t.Fatalf("expected unknown terminal event handled without cmd, got handled=%v cmd=%#v", handled, cmd)
	}
}

func TestHandleTerminalEventMessageMetadataChangedRefreshesInventory(t *testing.T) {
	exitCode := 9
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:        "term-1",
			Name:      "renamed-shell",
			Command:   []string{"zsh", "-l"},
			Tags:      map[string]string{"termx.environment": "prod"},
			State:     "exited",
			ExitCode:  &exitCode,
			CreatedAt: time.Unix(123, 0),
		}}},
	}
	model := setupModel(t, modelOpts{client: client})
	model.openTerminalPool()
	model.terminalPage.Items = []modal.PickerItem{{TerminalID: "term-1", Name: "old-shell", State: "running"}}
	model.terminalPage.ApplyFilter()
	model.modalHost.Picker = &modal.PickerState{
		Items:    []modal.PickerItem{{TerminalID: "term-1", Name: "old-shell", State: "running"}, {CreateNew: true, Name: "new terminal"}},
		Filtered: []modal.PickerItem{{TerminalID: "term-1", Name: "old-shell", State: "running"}, {CreateNew: true, Name: "new terminal"}},
	}

	_, cmd := model.Update(terminalEventMsg{Event: protocol.Event{
		Type:       protocol.EventTerminalMetadataChanged,
		TerminalID: "term-1",
	}})
	drainCmd(t, model, cmd, 10)

	if client.listCalls != 1 {
		t.Fatalf("expected metadata event to refresh inventory once, got %d", client.listCalls)
	}
	terminal := model.runtime.Registry().Get("term-1")
	if terminal == nil || terminal.Name != "renamed-shell" || terminal.State != "exited" || terminal.ExitCode == nil || *terminal.ExitCode != exitCode {
		t.Fatalf("expected runtime terminal patched from inventory, got %#v", terminal)
	}
	if got := terminal.Tags["termx.environment"]; got != "prod" {
		t.Fatalf("expected runtime tags patched, got %#v", terminal.Tags)
	}
	if pane := model.workbench.CurrentTab().Panes["pane-1"]; pane == nil || pane.Title != "renamed-shell" {
		t.Fatalf("expected bound pane title patched, got %#v", pane)
	}
	managerItem := findPickerItemByTerminalID(model.terminalPage.Items, "term-1")
	if managerItem == nil || managerItem.Name != "renamed-shell" || managerItem.TerminalState != "exited" || managerItem.ExitCode == nil || *managerItem.ExitCode != exitCode {
		t.Fatalf("expected terminal manager item patched, got %#v", managerItem)
	}
	pickerItem := findPickerItemByTerminalID(model.modalHost.Picker.Items, "term-1")
	if pickerItem == nil || pickerItem.Name != "renamed-shell" || pickerItem.State != "exited" || pickerItem.ExitCode == nil || *pickerItem.ExitCode != exitCode {
		t.Fatalf("expected picker item patched, got %#v", pickerItem)
	}
}

func TestHandleTerminalEventMessageCreatedAddsTerminalToOpenLists(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{
			ID:      "term-1",
			Name:    "shell",
			Command: []string{"sh"},
			State:   "running",
		}, {
			ID:      "term-2",
			Name:    "logs",
			Command: []string{"tail", "-f", "app.log"},
			State:   "running",
		}}},
	}
	model := setupModel(t, modelOpts{client: client})
	model.openTerminalPool()
	model.terminalPage.Items = []modal.PickerItem{{TerminalID: "term-1", Name: "shell", State: "running"}}
	model.terminalPage.ApplyFilter()
	model.modalHost.Picker = &modal.PickerState{
		Items:    []modal.PickerItem{{TerminalID: "term-1", Name: "shell", State: "running"}, {CreateNew: true, Name: "new terminal"}},
		Filtered: []modal.PickerItem{{TerminalID: "term-1", Name: "shell", State: "running"}, {CreateNew: true, Name: "new terminal"}},
	}

	_, cmd := model.Update(terminalEventMsg{Event: protocol.Event{
		Type:       protocol.EventTerminalCreated,
		TerminalID: "term-2",
	}})
	drainCmd(t, model, cmd, 10)

	if got := model.runtime.Registry().Get("term-2"); got == nil || got.Name != "logs" || got.State != "running" {
		t.Fatalf("expected created terminal patched into runtime, got %#v", got)
	}
	if item := findPickerItemByTerminalID(model.terminalPage.Items, "term-2"); item == nil || item.Name != "logs" {
		t.Fatalf("expected created terminal in manager items, got %#v", model.terminalPage.Items)
	}
	if item := findPickerItemByTerminalID(model.modalHost.Picker.Items, "term-2"); item == nil || item.Name != "logs" {
		t.Fatalf("expected created terminal in picker items, got %#v", model.modalHost.Picker.Items)
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

func findPickerItemByTerminalID(items []modal.PickerItem, terminalID string) *modal.PickerItem {
	for index := range items {
		if items[index].TerminalID == terminalID {
			return &items[index]
		}
	}
	return nil
}
