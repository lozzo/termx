package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestActiveContentMouseTargetReturnsActivePaneContentRect(t *testing.T) {
	model := setupModel(t, modelOpts{})
	pane := firstVisiblePane(t, model)
	contentRect, ok := paneContentRectForVisible(pane)
	if !ok {
		t.Fatal("expected content rect")
	}
	paneID, gotRect, ok := model.activeContentMouseTarget(contentRect.X, screenYForBodyY(model, contentRect.Y))
	if !ok {
		t.Fatal("expected active content mouse target")
	}
	if paneID != pane.ID {
		t.Fatalf("expected pane target %q, got %q", pane.ID, paneID)
	}
	if gotRect != contentRect {
		t.Fatalf("expected content rect %#v, got %#v", contentRect, gotRect)
	}
}

func TestForwardTerminalMouseInputCmdEncodesActivePaneMousePress(t *testing.T) {
	model := setupModel(t, modelOpts{})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Modes:      protocol.TerminalModes{MouseTracking: true, MouseSGR: true},
	}
	pane := firstVisiblePane(t, model)
	contentRect, ok := paneContentRectForVisible(pane)
	if !ok {
		t.Fatal("expected content rect")
	}
	msg := tea.MouseMsg{
		X:      contentRect.X,
		Y:      screenYForBodyY(model, contentRect.Y),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}
	cmd := model.forwardTerminalMouseInputCmd(msg)
	if cmd == nil {
		t.Fatal("expected terminal mouse input command")
	}
	result, ok := cmd().(input.TerminalInput)
	if !ok {
		t.Fatalf("expected terminal input message, got %#v", cmd())
	}
	if result.PaneID != "pane-1" {
		t.Fatalf("expected pane-1 target, got %#v", result)
	}
	if len(result.Data) == 0 {
		t.Fatalf("expected encoded mouse input, got %#v", result)
	}
}

func TestHandlePromptInputMouseClickMovesCursorAndField(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: "prompt-1"}
	model.modalHost.Prompt = &modal.PromptState{
		Kind:  "create-terminal-form",
		Title: "Create Terminal",
		Fields: []modal.PromptField{
			{Key: "name", Label: "name", Value: "shell", Cursor: 5, Required: true},
			{Key: "command", Label: "command", Value: "/bin/sh", Cursor: 7},
		},
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: "prompt-1"})

	region := render.HitRegion{
		Kind:      render.HitRegionPromptInput,
		ItemIndex: 1,
		Rect:      workbench.Rect{X: 10, Y: 5, W: 20, H: 1},
	}
	_ = model.handlePromptInputMouseClick(region, 13)
	if model.modalHost.Prompt.ActiveField != 1 {
		t.Fatalf("expected prompt active field 1, got %d", model.modalHost.Prompt.ActiveField)
	}
	if got := model.modalHost.Prompt.Fields[1].Cursor; got != 3 {
		t.Fatalf("expected prompt cursor 3, got %d", got)
	}
}

func TestDispatchOverlayRegionActionPrefersModalHandler(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseReady, RequestID: "picker-1"}
	model.modalHost.Picker = &modal.PickerState{Items: []modal.PickerItem{{TerminalID: "term-1", Name: "shell"}}}
	model.modalHost.Picker.ApplyFilter()
	model.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "picker-1"})

	drainCmd(t, model, model.dispatchOverlayRegionAction(input.SemanticAction{Kind: input.ActionCancelMode}), 20)
	if model.modalHost.Session != nil {
		t.Fatalf("expected modal handler to close picker, got %#v", model.modalHost.Session)
	}
}
