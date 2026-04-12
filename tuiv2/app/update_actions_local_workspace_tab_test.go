package app

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestHandleWorkspaceAndTabLocalActionModeGuards(t *testing.T) {
	model := setupModel(t, modelOpts{})

	if handled, cmd := model.handleWorkspaceAndTabLocalAction(input.SemanticAction{Kind: input.ActionCreateWorkspace}); handled || cmd != nil {
		t.Fatalf("expected create-workspace guard to reject outside workspace mode, got handled=%v cmd=%#v", handled, cmd)
	}
	if handled, cmd := model.handleWorkspaceAndTabLocalAction(input.SemanticAction{Kind: input.ActionRenameTab}); handled || cmd != nil {
		t.Fatalf("expected rename-tab guard to reject outside tab mode, got handled=%v cmd=%#v", handled, cmd)
	}
}

func TestHandleWorkspaceAndTabLocalActionCreateWorkspaceOpensPrompt(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.input.SetMode(input.ModeState{Kind: input.ModeWorkspace})

	handled, cmd := model.handleWorkspaceAndTabLocalAction(input.SemanticAction{Kind: input.ActionCreateWorkspace})
	if !handled || cmd != nil {
		t.Fatalf("expected create-workspace helper to handle synchronously, got handled=%v cmd=%#v", handled, cmd)
	}
	if model.modalHost == nil || model.modalHost.Prompt == nil {
		t.Fatal("expected workspace creation prompt")
	}
	if model.modalHost.Prompt.Kind != "rename-workspace" || model.modalHost.Prompt.Original != "" {
		t.Fatalf("expected create-workspace prompt state, got %#v", model.modalHost.Prompt)
	}
}

func TestHandleWorkspaceAndTabLocalActionNextWorkspaceSwitchesAndReturnsSave(t *testing.T) {
	model := setupModel(t, modelOpts{statePath: t.TempDir() + "/workspace-state.json"})
	if err := model.workbench.CreateWorkspace("dev"); err != nil {
		t.Fatalf("create workspace dev: %v", err)
	}
	if !model.workbench.SwitchWorkspace("main") {
		t.Fatal("expected to switch back to main")
	}
	model.input.SetMode(input.ModeState{Kind: input.ModeWorkspace})

	handled, cmd := model.handleWorkspaceAndTabLocalAction(input.SemanticAction{Kind: input.ActionNextWorkspace})
	if !handled || cmd == nil {
		t.Fatalf("expected next-workspace helper to handle and return save cmd, got handled=%v cmd=%#v", handled, cmd)
	}
	if got := model.workbench.CurrentWorkspaceName(); got != "dev" {
		t.Fatalf("expected workspace switched to dev, got %q", got)
	}
	if got := model.input.Mode().Kind; got != input.ModeNormal {
		t.Fatalf("expected mode reset to normal after workspace switch, got %q", got)
	}
}

func TestHandleWorkspaceAndTabLocalActionKillTabReturnsCommand(t *testing.T) {
	model := setupModel(t, modelOpts{
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{
					{
						ID:           "tab-1",
						Name:         "tab 1",
						ActivePaneID: "pane-1",
						Panes: map[string]*workbench.PaneState{
							"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
						},
						Root: workbench.NewLeaf("pane-1"),
					},
					{
						ID:           "tab-2",
						Name:         "tab 2",
						ActivePaneID: "pane-2",
						Panes: map[string]*workbench.PaneState{
							"pane-2": {ID: "pane-2", Title: "logs", TerminalID: "term-2"},
						},
						Root: workbench.NewLeaf("pane-2"),
					},
				},
			},
		},
	})
	model.input.SetMode(input.ModeState{Kind: input.ModeTab})

	handled, cmd := model.handleWorkspaceAndTabLocalAction(input.SemanticAction{Kind: input.ActionKillTab})
	if !handled || cmd == nil {
		t.Fatalf("expected kill-tab helper to return command, got handled=%v cmd=%#v", handled, cmd)
	}
}

func TestHandleWorkspaceAndTabLocalActionRenameTabOpensPrompt(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.input.SetMode(input.ModeState{Kind: input.ModeTab})

	handled, cmd := model.handleWorkspaceAndTabLocalAction(input.SemanticAction{Kind: input.ActionRenameTab})
	if !handled || cmd != nil {
		t.Fatalf("expected rename-tab helper to handle synchronously, got handled=%v cmd=%#v", handled, cmd)
	}
	if model.modalHost == nil || model.modalHost.Prompt == nil {
		t.Fatal("expected rename-tab prompt")
	}
	if model.modalHost.Prompt.Kind != "rename-tab" {
		t.Fatalf("expected rename-tab prompt kind, got %#v", model.modalHost.Prompt)
	}
}
