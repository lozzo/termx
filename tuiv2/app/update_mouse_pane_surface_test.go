package app

import (
	"testing"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestHandleOwnerActionClickArmsConfirmation(t *testing.T) {
	model := setupModel(t, modelOpts{})

	cmd := model.handleOwnerActionClick("pane-1")
	if model.ownerConfirmPaneID != "pane-1" {
		t.Fatalf("expected owner confirm pane armed, got %q", model.ownerConfirmPaneID)
	}
	if model.ownerSeq == 0 {
		t.Fatal("expected owner sequence incremented")
	}
	if cmd == nil {
		t.Fatal("expected confirmation clear command")
	}
	if _, ok := cmd().(clearOwnerConfirmMsg); !ok {
		t.Fatalf("expected clearOwnerConfirmMsg, got %#v", cmd())
	}
}

func TestHandlePaneChromeRegionOwnerDelegatesToOwnerAction(t *testing.T) {
	model := setupModel(t, modelOpts{})

	cmd := model.handlePaneChromeRegion(render.HitRegion{Kind: render.HitRegionPaneOwner, PaneID: "pane-1"})
	if model.ownerConfirmPaneID != "pane-1" {
		t.Fatalf("expected pane owner click to arm confirmation, got %q", model.ownerConfirmPaneID)
	}
	if cmd == nil {
		t.Fatal("expected owner action command")
	}
}

func TestHandleEmptyPaneClickCreateOpensPrompt(t *testing.T) {
	model := setupModel(t, modelOpts{
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:           "tab-1",
					Name:         "tab 1",
					ActivePaneID: "pane-1",
					Panes:        map[string]*workbench.PaneState{"pane-1": {ID: "pane-1"}},
					Root:         workbench.NewLeaf("pane-1"),
				}},
			},
		},
	})
	pane := firstVisiblePane(t, model)
	var target render.HitRegion
	found := false
	for _, region := range render.EmptyPaneActionRegions(pane) {
		if region.Kind == render.HitRegionEmptyPaneCreate {
			target = region
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected empty-pane create region")
	}

	cmd := model.handleEmptyPaneClick(pane, target.Rect.X, target.Rect.Y)
	if cmd != nil {
		drainCmd(t, model, cmd, 20)
	}
	if model.modalHost == nil || model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "create-terminal-form" {
		t.Fatalf("expected create-terminal prompt, got %#v", model.modalHost)
	}
}

func TestHandleEmptyPaneClickManagerOpensTerminalPool(t *testing.T) {
	client := &recordingBridgeClient{listResult: &protocol.ListResult{}}
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
					Panes:        map[string]*workbench.PaneState{"pane-1": {ID: "pane-1"}},
					Root:         workbench.NewLeaf("pane-1"),
				}},
			},
		},
	})
	pane := firstVisiblePane(t, model)
	var target render.HitRegion
	found := false
	for _, region := range render.EmptyPaneActionRegions(pane) {
		if region.Kind == render.HitRegionEmptyPaneManager {
			target = region
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected empty-pane manager region")
	}

	drainCmd(t, model, model.handleEmptyPaneClick(pane, target.Rect.X, target.Rect.Y), 20)
	if model.terminalPage == nil {
		t.Fatal("expected terminal pool page to open")
	}
	if client.listCalls == 0 {
		t.Fatal("expected terminal pool open to load items")
	}
}

func TestHandleExitedPaneClickRestartUsesRestartFlow(t *testing.T) {
	client := &recordingBridgeClient{
		attachResult: &protocol.AttachResult{Channel: 7, Mode: "collaborator"},
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-1": {TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}},
		},
	}
	model := setupModel(t, modelOpts{client: client})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	exitCode := 23
	terminal.State = "exited"
	terminal.ExitCode = &exitCode
	pane := firstVisiblePane(t, model)
	var target render.HitRegion
	found := false
	for _, region := range render.ExitedPaneRecoveryRegions(pane, model.runtime.Visible()) {
		if region.Kind == render.HitRegionExitedPaneRestart {
			target = region
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected exited-pane restart region")
	}

	drainCmd(t, model, model.handleExitedPaneClick(pane, target.Rect.X, target.Rect.Y), 20)
	if len(client.restartCalls) != 1 || client.restartCalls[0] != "term-1" {
		t.Fatalf("expected restart flow for term-1, got %#v", client.restartCalls)
	}
}

func TestOpenTerminalManagerMouseOpensPoolAndLoadsItems(t *testing.T) {
	client := &recordingBridgeClient{listResult: &protocol.ListResult{}}
	model := setupModel(t, modelOpts{client: client})

	drainCmd(t, model, model.openTerminalManagerMouse(), 20)
	if model.terminalPage == nil {
		t.Fatal("expected terminal pool page state")
	}
	if client.listCalls == 0 {
		t.Fatal("expected terminal manager mouse open to load items")
	}
}

func firstVisiblePane(t *testing.T, model *Model) workbench.VisiblePane {
	t.Helper()
	visible := model.workbench.VisibleWithSize(model.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) || len(visible.Tabs[visible.ActiveTab].Panes) == 0 {
		t.Fatalf("expected visible pane, got %#v", visible)
	}
	return visible.Tabs[visible.ActiveTab].Panes[0]
}
