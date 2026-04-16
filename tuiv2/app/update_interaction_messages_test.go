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

func TestCompactInteractionMessagesKeepsBoundaryOrderWhileDroppingInvalidatedContinuous(t *testing.T) {
	msgs := []tea.Msg{
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}},
		queuedMouseMsg{Seq: 1, Kind: "motion", Msg: tea.MouseMsg{X: 10, Y: 5, Action: tea.MouseActionMotion}},
		tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 1, Y: 1},
		queuedMouseMsg{Seq: 2, Kind: "motion", Msg: tea.MouseMsg{X: 30, Y: 12, Action: tea.MouseActionMotion}},
	}

	got := compactInteractionMessages(msgs)
	if len(got) != 3 {
		t.Fatalf("expected key + press + trailing motion after compaction, got %d (%#v)", len(got), got)
	}
	if _, ok := got[0].(tea.KeyMsg); !ok {
		t.Fatalf("expected first boundary key preserved, got %T", got[0])
	}
	if _, ok := got[1].(tea.MouseMsg); !ok {
		t.Fatalf("expected press boundary preserved, got %T", got[1])
	}
	if motion, ok := got[2].(queuedMouseMsg); !ok || motion.Seq != 2 {
		t.Fatalf("expected only trailing motion after last boundary, got %#v", got[2])
	}
}

func TestCompactInteractionMessagesKeepsLatestMotionOnly(t *testing.T) {
	msgs := []tea.Msg{
		queuedMouseMsg{Seq: 1, Kind: "motion", Msg: tea.MouseMsg{X: 10, Y: 5, Action: tea.MouseActionMotion}},
		queuedMouseMsg{Seq: 2, Kind: "motion", Msg: tea.MouseMsg{X: 20, Y: 7, Action: tea.MouseActionMotion}},
		queuedMouseMsg{Seq: 3, Kind: "motion", Msg: tea.MouseMsg{X: 30, Y: 9, Action: tea.MouseActionMotion}},
	}

	got := compactInteractionMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected one compacted motion, got %d (%#v)", len(got), got)
	}
	motion, ok := got[0].(queuedMouseMsg)
	if !ok || motion.Seq != 3 {
		t.Fatalf("expected latest motion seq 3, got %#v", got[0])
	}
}

func TestCompactInteractionMessagesAccumulatesWheelBursts(t *testing.T) {
	msgs := []tea.Msg{
		mouseWheelBurstMsg{Msg: tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress, X: 4, Y: 4}, Repeat: 2},
		mouseWheelBurstMsg{Msg: tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress, X: 4, Y: 4}, Repeat: 3},
	}

	got := compactInteractionMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected one compacted wheel burst, got %d (%#v)", len(got), got)
	}
	wheel, ok := got[0].(mouseWheelBurstMsg)
	if !ok || wheel.Repeat != 5 {
		t.Fatalf("expected accumulated wheel repeat 5, got %#v", got[0])
	}
}

func TestCompactInteractionMessagesDropsContinuousBeforeBoundaryKey(t *testing.T) {
	msgs := []tea.Msg{
		queuedMouseMsg{Seq: 1, Kind: "motion", Msg: tea.MouseMsg{X: 10, Y: 5, Action: tea.MouseActionMotion}},
		mouseWheelBurstMsg{Msg: tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress, X: 4, Y: 4}, Repeat: 2},
		tea.KeyMsg{Type: tea.KeyCtrlG},
	}

	got := compactInteractionMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected boundary key to invalidate prior continuous input, got %d (%#v)", len(got), got)
	}
	if key, ok := got[0].(tea.KeyMsg); !ok || key.Type != tea.KeyCtrlG {
		t.Fatalf("expected surviving boundary key, got %#v", got[0])
	}
}
