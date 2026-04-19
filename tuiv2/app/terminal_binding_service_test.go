package app

import (
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestTerminalBindingServiceBindSelectionUpdatesWorkbenchAndRuntime(t *testing.T) {
	model := setupModel(t, modelOpts{
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:           "tab-1",
					Name:         "tab 1",
					ActivePaneID: "pane-1",
					Panes: map[string]*workbench.PaneState{
						"pane-1": {ID: "pane-1", Title: "old", TerminalID: "term-old"},
					},
					Root: workbench.NewLeaf("pane-1"),
				}},
			},
		},
	})
	oldTerminal := model.runtime.Registry().GetOrCreate("term-old")
	oldTerminal.OwnerPaneID = "pane-1"
	oldTerminal.BoundPaneIDs = []string{"pane-1"}
	oldBinding := model.runtime.BindPane("pane-1")
	oldBinding.Channel = 1
	oldBinding.Connected = true
	oldBinding.Role = runtime.BindingRoleOwner

	service := model.terminalBindingService()
	result, err := service.bindSelection("", "pane-1", modal.PickerItem{
		TerminalID:    "term-new",
		Name:          "parked-shell",
		State:         "parked",
		TerminalState: "parked",
		CommandArgs:   []string{"bash", "-lc", "htop"},
		Tags:          map[string]string{"env": "dev"},
	})
	if err != nil {
		t.Fatalf("bind selection: %v", err)
	}

	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	pane := tab.Panes["pane-1"]
	if pane == nil || pane.TerminalID != "term-new" {
		t.Fatalf("expected pane-1 rebound to term-new, got %#v", pane)
	}
	if tab.ActivePaneID != "pane-1" {
		t.Fatalf("expected pane-1 focused after bind, got %q", tab.ActivePaneID)
	}
	newTerminal := model.runtime.Registry().Get("term-new")
	if newTerminal == nil {
		t.Fatal("expected new terminal runtime")
	}
	if newTerminal.Name != "parked-shell" {
		t.Fatalf("expected runtime name updated, got %#v", newTerminal)
	}
	if len(newTerminal.Command) != 3 || newTerminal.Command[2] != "htop" {
		t.Fatalf("expected command copied, got %#v", newTerminal.Command)
	}
	if newTerminal.Tags["env"] != "dev" {
		t.Fatalf("expected tags copied, got %#v", newTerminal.Tags)
	}
	if newTerminal.OwnerPaneID != "pane-1" || newTerminal.ControlPaneID != "" || newTerminal.RequiresExplicitOwner {
		t.Fatalf("expected reference binding ownership metadata applied, got %#v", newTerminal)
	}
	if len(newTerminal.BoundPaneIDs) != 1 || newTerminal.BoundPaneIDs[0] != "pane-1" {
		t.Fatalf("expected pane-1 as sole bound pane, got %#v", newTerminal.BoundPaneIDs)
	}
	if got := model.runtime.Binding("pane-1"); got != nil {
		t.Fatalf("expected live runtime binding cleared for reference bind, got %#v", got)
	}
	if len(oldTerminal.BoundPaneIDs) != 0 || oldTerminal.OwnerPaneID != "" {
		t.Fatalf("expected old terminal detached from pane-1, got %#v", oldTerminal)
	}
	if result.tabID != "tab-1" || result.paneID != "pane-1" || result.terminalID != "term-new" {
		t.Fatalf("unexpected bind result: %#v", result)
	}
	if result.loadSnapshotAfter {
		t.Fatalf("parked terminal should not request snapshot load, got %#v", result)
	}
}

func TestTerminalBindingServiceBindSelectionCmdLoadsSnapshotForExitedTerminal(t *testing.T) {
	client := &recordingBridgeClient{
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-exited": {
				TerminalID: "term-exited",
				Size:       protocol.Size{Cols: 90, Rows: 30},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client})
	service := model.terminalBindingService()

	cmd := service.bindSelectionCmd("", "pane-1", modal.PickerItem{
		TerminalID:    "term-exited",
		Name:          "exited-shell",
		State:         "exited",
		TerminalState: "exited",
		ExitCode:      intPtr(23),
	})
	drainCmd(t, model, cmd, 20)

	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "term-exited" {
		t.Fatalf("expected active pane rebound to exited terminal, got %#v", pane)
	}
	terminal := model.runtime.Registry().Get("term-exited")
	if terminal == nil || terminal.Snapshot == nil {
		t.Fatalf("expected exited terminal snapshot loaded, got %#v", terminal)
	}
	if terminal.ExitCode == nil || *terminal.ExitCode != 23 {
		t.Fatalf("expected exit code preserved, got %#v", terminal)
	}
}

func TestTerminalBindingServiceBindSelectionPreservesExistingCommandWhenPickerItemIsSparse(t *testing.T) {
	model := setupModel(t, modelOpts{})
	terminal := model.runtime.Registry().GetOrCreate("term-keep")
	terminal.Command = []string{"bash", "-lc", "htop"}

	service := model.terminalBindingService()
	_, err := service.bindSelection("", "pane-1", modal.PickerItem{
		TerminalID:    "term-keep",
		Name:          "parked-shell",
		State:         "parked",
		TerminalState: "parked",
		CommandArgs:   nil,
	})
	if err != nil {
		t.Fatalf("bind selection: %v", err)
	}

	if len(terminal.Command) != 3 || terminal.Command[2] != "htop" {
		t.Fatalf("expected sparse picker item to preserve existing command metadata, got %#v", terminal.Command)
	}
}

func intPtr(v int) *int { return &v }
