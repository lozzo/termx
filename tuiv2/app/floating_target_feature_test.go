package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

func TestFeatureCreateFloatingPanePickerSubmitTargetsNewFloatingPane(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult:       &protocol.AttachResult{Channel: 9, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{},
	}
	model := setupModel(t, modelOpts{client: client, attachTerminal: "term-2"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCreateFloatingPane})

	tab := model.workbench.CurrentTab()
	if tab == nil || len(tab.Floating) != 1 {
		t.Fatalf("expected one floating pane after create, got %#v", tab)
	}
	floatingPaneID := tab.Floating[0].PaneID
	model.modalHost.Picker = &modal.PickerState{
		Selected: 0,
		Items: []modal.PickerItem{
			{TerminalID: "term-2", Name: "logs"},
		},
	}

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if got := tab.Panes[floatingPaneID].TerminalID; got != "term-2" {
		t.Fatalf("expected new floating pane %q to attach term-2, got %q", floatingPaneID, got)
	}
	if got := tab.Panes["pane-1"].TerminalID; got != "term-1" {
		t.Fatalf("expected original pane to stay attached to term-1, got %q", got)
	}
}
