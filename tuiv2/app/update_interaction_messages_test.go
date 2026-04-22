package app

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
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

func TestHandleInteractionMessageTerminalInputSentWithoutPendingDoesNotForceInvalidate(t *testing.T) {
	model := setupModel(t, modelOpts{})

	cmd, handled := model.handleInteractionMessage(terminalInputSentMsg{
		paneID:     "pane-1",
		terminalID: "term-1",
	})
	if !handled {
		t.Fatal("expected terminalInputSentMsg handled")
	}
	if cmd != nil {
		t.Fatalf("expected no follow-up invalidate when queue is empty, got %#v", cmd)
	}
}

func TestHandleInteractionMessageTerminalInputSentContinuousReschedulesWheelTail(t *testing.T) {
	model := setupModel(t, modelOpts{})

	originalTailDelay := terminalWheelTailDispatchDelay
	terminalWheelTailDispatchDelay = time.Millisecond
	defer func() { terminalWheelTailDispatchDelay = originalTailDelay }()

	model.terminalInputs.Enqueue(input.TerminalInput{
		Kind:           input.TerminalInputWheel,
		PaneID:         "pane-1",
		Data:           []byte("up"),
		WheelDirection: 1,
	})
	model.terminalInputSending = true

	cmd, handled := model.handleInteractionMessage(terminalInputSentMsg{
		paneID:     "pane-1",
		terminalID: "term-1",
		continuous: true,
	})
	if !handled {
		t.Fatal("expected terminalInputSentMsg handled")
	}
	if cmd == nil {
		t.Fatal("expected wheel tail dispatch command")
	}
	if model.terminalInputSending {
		t.Fatal("expected wheel tail pacing to release sending flag before next tick")
	}
	msg := cmd()
	if _, ok := msg.(terminalWheelDispatchMsg); !ok {
		t.Fatalf("expected wheel tail dispatch tick, got %T", msg)
	}
}

func TestHandleInteractionMessageDropsSupersededWheelBurst(t *testing.T) {
	model := setupModel(t, modelOpts{})
	noteQueuedMouseWheel(2)

	cmd, handled := model.handleInteractionMessage(mouseWheelBurstMsg{
		Seq:      1,
		QueuedAt: time.Now(),
		Msg:      tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress, X: 4, Y: 4},
		Repeat:   1,
	})
	if !handled {
		t.Fatal("expected mouseWheelBurstMsg handled")
	}
	if cmd != nil {
		t.Fatalf("expected superseded wheel burst dropped, got cmd %#v", cmd)
	}
}

func TestHandleInteractionMessageDropsLaggedWheelBurst(t *testing.T) {
	model := setupModel(t, modelOpts{})

	originalThreshold := staleMouseWheelThreshold
	originalRemoteThreshold := remoteStaleMouseWheelThreshold
	staleMouseWheelThreshold = 10 * time.Millisecond
	remoteStaleMouseWheelThreshold = 10 * time.Millisecond
	defer func() {
		staleMouseWheelThreshold = originalThreshold
		remoteStaleMouseWheelThreshold = originalRemoteThreshold
	}()

	cmd, handled := model.handleInteractionMessage(mouseWheelBurstMsg{
		Seq:      1,
		QueuedAt: time.Now().Add(-50 * time.Millisecond),
		Msg:      tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress, X: 4, Y: 4},
		Repeat:   1,
	})
	if !handled {
		t.Fatal("expected mouseWheelBurstMsg handled")
	}
	if cmd != nil {
		t.Fatalf("expected lagged wheel burst dropped, got cmd %#v", cmd)
	}
}

func TestHandleInteractionMessageTerminalInputSentStillSchedulesSharedResync(t *testing.T) {
	model := setupModel(t, modelOpts{})
	terminal := model.runtime.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected runtime terminal")
	}
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "x", Width: 1}}}},
		Modes:      protocol.TerminalModes{AlternateScreen: true},
	}

	originalDelay := sharedTerminalSnapshotResyncDelay
	sharedTerminalSnapshotResyncDelay = 0
	defer func() { sharedTerminalSnapshotResyncDelay = originalDelay }()

	sent := make(chan tea.Msg, 1)
	model.send = func(msg tea.Msg) {
		sent <- msg
	}

	cmd, handled := model.handleInteractionMessage(terminalInputSentMsg{
		paneID:     "pane-1",
		terminalID: "term-1",
	})
	if !handled {
		t.Fatal("expected terminalInputSentMsg handled")
	}
	if cmd != nil {
		t.Fatalf("expected shared resync to be scheduled asynchronously without immediate invalidate, got %#v", cmd)
	}

	select {
	case msg := <-sent:
		if _, ok := msg.(sharedTerminalSnapshotResyncMsg); !ok {
			t.Fatalf("expected sharedTerminalSnapshotResyncMsg, got %T", msg)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for shared terminal resync scheduling")
	}
}
