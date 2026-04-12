package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
)

func TestHandleInteractionMessageSemanticActionRoutesThroughLocalPath(t *testing.T) {
	model := setupModel(t, modelOpts{})

	cmd, handled := model.handleInteractionMessage(SemanticActionMsg{Action: input.SemanticAction{Kind: input.ActionEnterPaneMode}})
	if !handled || cmd == nil {
		t.Fatalf("expected SemanticActionMsg handled with follow-up cmd, got handled=%v cmd=%#v", handled, cmd)
	}
	if got := model.input.Mode().Kind; got != input.ModePane {
		t.Fatalf("expected pane mode entered, got %q", got)
	}
}

func TestHandleInteractionMessageTerminalInputRoutesToRuntimePath(t *testing.T) {
	client := &recordingBridgeClient{}
	model := setupModel(t, modelOpts{client: client})

	cmd, handled := model.handleInteractionMessage(TerminalInputMsg{
		Input: input.TerminalInput{PaneID: "pane-1", Data: []byte("abc")},
	})
	if !handled || cmd == nil {
		t.Fatalf("expected TerminalInputMsg handled with send command, got handled=%v cmd=%#v", handled, cmd)
	}
	drainCmd(t, model, cmd, 20)
	if len(client.inputCalls) != 1 || string(client.inputCalls[0].data) != "abc" {
		t.Fatalf("expected terminal input forwarded, got %#v", client.inputCalls)
	}
}

func TestHandleInteractionMessagePrefixTimeoutResetsStickyMode(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.setMode(input.ModeState{Kind: input.ModePane})
	model.prefixSeq = 7

	cmd, handled := model.handleInteractionMessage(prefixTimeoutMsg{seq: 7})
	if !handled || cmd != nil {
		t.Fatalf("expected prefix timeout handled synchronously, got handled=%v cmd=%#v", handled, cmd)
	}
	if got := model.input.Mode().Kind; got != input.ModeNormal {
		t.Fatalf("expected sticky mode reset to normal, got %q", got)
	}
}

func TestHandleInteractionMessageBatchDispatchesMessages(t *testing.T) {
	model := setupModel(t, modelOpts{})

	cmd, handled := model.handleInteractionMessage(interactionBatchMsg{
		Messages: []tea.Msg{
			SemanticActionMsg{Action: input.SemanticAction{Kind: input.ActionEnterPaneMode}},
		},
	})
	if !handled || cmd == nil {
		t.Fatalf("expected interaction batch handled with command, got handled=%v cmd=%#v", handled, cmd)
	}
	drainCmd(t, model, cmd, 20)
	if got := model.input.Mode().Kind; got != input.ModePane {
		t.Fatalf("expected interaction batch to apply semantic action, got %q", got)
	}
}

func TestHandleInteractionMessageFallsThroughForUnknownMsg(t *testing.T) {
	model := setupModel(t, modelOpts{})
	cmd, handled := model.handleInteractionMessage(struct{ X int }{X: 1})
	if handled || cmd != nil {
		t.Fatalf("expected unknown interaction msg to fall through, got handled=%v cmd=%#v", handled, cmd)
	}
}
