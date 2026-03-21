package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
)

func TestPrefixSOpensWorkspacePickerAndCreatesWorkspace(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", Workspace: "main"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.workspacePicker == nil {
		t.Fatal("expected workspace picker to open")
	}
	if model.workspacePicker.Title != "Choose Workspace" {
		t.Fatalf("expected workspace picker title, got %q", model.workspacePicker.Title)
	}
	if len(model.workspacePicker.Filtered) != 2 {
		t.Fatalf("expected create row plus current workspace, got %d items", len(model.workspacePicker.Filtered))
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, nextCmd := model.Update(next)
			if nextCmd != nil {
				if follow := mustRunCmd(t, nextCmd); follow != nil {
					_, _ = model.Update(follow)
				}
			}
		}
	}

	if model.workspacePicker != nil {
		t.Fatal("expected workspace picker to close after selection")
	}
	if model.workspace.Name != "workspace-2" {
		t.Fatalf("expected new workspace name workspace-2, got %q", model.workspace.Name)
	}
	if model.terminalPicker == nil {
		t.Fatal("expected empty new workspace to bootstrap terminal chooser when reusable terminals already exist")
	}
	if model.terminalPicker.Title != "Choose Terminal" {
		t.Fatalf("expected bootstrap chooser title, got %q", model.terminalPicker.Title)
	}
	tab := model.currentTab()
	if tab == nil {
		t.Fatal("expected current tab in new workspace")
	}
	if len(tab.Panes) != 0 {
		t.Fatalf("expected new workspace to remain empty until chooser selection, got %d panes", len(tab.Panes))
	}
}

func TestWorkspacePickerSwitchRestoresPreviousWorkspaceLayout(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", Workspace: "main"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	createSplitPaneViaPicker(t, model, SplitVertical)
	mainTab := model.currentTab()
	if mainTab == nil || len(mainTab.Panes) != 2 {
		t.Fatalf("expected two panes in main workspace, got %#v", mainTab)
	}
	originalPaneIDs := append([]string(nil), mainTab.Root.LeafIDs()...)
	originalAttached := len(client.attachedIDs)

	openWorkspacePickerAndSelect(t, model, "")
	if model.workspace.Name != "workspace-2" {
		t.Fatalf("expected switched to new workspace, got %q", model.workspace.Name)
	}

	openWorkspacePickerAndSelect(t, model, "main")
	if model.workspace.Name != "main" {
		t.Fatalf("expected switched back to main workspace, got %q", model.workspace.Name)
	}
	tab := model.currentTab()
	if tab == nil || len(tab.Panes) != 2 {
		t.Fatalf("expected restored main workspace panes, got %#v", tab)
	}
	if got := tab.Root.LeafIDs(); len(got) != len(originalPaneIDs) {
		t.Fatalf("expected restored root leaf count %d, got %v", len(originalPaneIDs), got)
	}
	if len(client.attachedIDs) < originalAttached+2 {
		t.Fatalf("expected switching back to reattach both panes, got %v", client.attachedIDs)
	}
}

func TestCreateWorkspaceBootstrapsCenteredTerminalChooserWhenReusableTerminalsExist(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", Workspace: "main"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	cmd = model.createWorkspaceCmd("workspace-2")
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if model.workspace.Name != "workspace-2" {
		t.Fatalf("expected switched workspace, got %q", model.workspace.Name)
	}
	if model.terminalPicker == nil {
		t.Fatal("expected empty workspace bootstrap to open terminal chooser when terminals already exist")
	}
	if model.terminalPicker.Title != "Choose Terminal" {
		t.Fatalf("expected bootstrap chooser title, got %q", model.terminalPicker.Title)
	}
}

func TestSameTerminalCanAppearInDifferentWorkspaces(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{
				ID:      "shared-001",
				Name:    "shared",
				Command: []string{"tail", "-f", "worker.log"},
				State:   "running",
			},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", Workspace: "main"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if pane := model.currentTab().Panes[model.currentTab().ActivePaneID]; pane == nil || pane.TerminalID != "shared-001" {
		t.Fatalf("expected main workspace to attach shared terminal, got %#v", pane)
	}

	openWorkspacePickerAndSelect(t, model, "")
	if model.workspace.Name != "workspace-2" {
		t.Fatalf("expected second workspace, got %q", model.workspace.Name)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if pane := model.currentTab().Panes[model.currentTab().ActivePaneID]; pane == nil || pane.TerminalID != "shared-001" {
		t.Fatalf("expected second workspace to attach shared terminal, got %#v", pane)
	}

	openWorkspacePickerAndSelect(t, model, "main")
	if pane := model.currentTab().Panes[model.currentTab().ActivePaneID]; pane == nil || pane.TerminalID != "shared-001" {
		t.Fatalf("expected main workspace to keep shared terminal, got %#v", pane)
	}
}

func openWorkspacePickerAndSelect(t *testing.T, model *Model, query string) {
	t.Helper()

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	msg := mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.workspacePicker == nil {
		t.Fatal("expected workspace picker to open")
	}
	if query != "" {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(query)})
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}
}
