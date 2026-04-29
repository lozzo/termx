package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/input"
)

func TestTerminalPoolPageModeTokenRestoredAfterEditCancel(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", Command: []string{"bash"}, State: "running"},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client})

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlG})
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})

	if got := model.input.Mode(); got.Kind != input.ModeTerminalManager || got.RequestID != terminalPoolPageModeToken {
		t.Fatalf("expected terminal pool page mode token after open, got %#v", got)
	}

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlE})
	if got := model.input.Mode().Kind; got != input.ModePrompt {
		t.Fatalf("expected prompt mode after edit, got %q", got)
	}

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEsc})

	if got := model.input.Mode(); got.Kind != input.ModeTerminalManager || got.RequestID != terminalPoolPageModeToken {
		t.Fatalf("expected terminal pool page mode token after cancel, got %#v", got)
	}
	if model.terminalPage == nil {
		t.Fatal("expected terminal pool page to remain open after cancel")
	}
}

func TestTerminalPoolPageModeTokenRestoredAfterEditSave(t *testing.T) {
	client := &recordingBridgeClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-1", Name: "shell", Command: []string{"bash"}, State: "running", Tags: map[string]string{"role": "ops"}},
			},
		},
	}
	model := setupModel(t, modelOpts{client: client})

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlG})
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlE})

	if prompt := model.modalHost.Prompt; prompt == nil || prompt.Kind != "edit-terminal-name" {
		t.Fatalf("expected edit-terminal-name prompt, got %#v", prompt)
	}

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if prompt := model.modalHost.Prompt; prompt == nil || prompt.Kind != "edit-terminal-tags" {
		t.Fatalf("expected edit-terminal-tags prompt, got %#v", prompt)
	}

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(client.setMetadataCalls) != 1 {
		t.Fatalf("expected one metadata save, got %#v", client.setMetadataCalls)
	}
	if got := model.input.Mode(); got.Kind != input.ModeTerminalManager || got.RequestID != terminalPoolPageModeToken {
		t.Fatalf("expected terminal pool page mode token after save, got %#v", got)
	}
	if model.terminalPage == nil {
		t.Fatal("expected terminal pool page to remain open after save")
	}
}
