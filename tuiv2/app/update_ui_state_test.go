package app

import (
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
)

func TestHandleUIStateMessagePickerItemsLoadedPopulatesPickerAndMarksReady(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseLoading, RequestID: "req-1", Loading: true}

	items := []modal.PickerItem{{TerminalID: "term-1", Name: "shell"}, {CreateNew: true, Name: "new terminal"}}
	cmd, handled := model.handleUIStateMessage(pickerItemsLoadedMsg{Items: items})
	if !handled || cmd != nil {
		t.Fatalf("expected pickerItemsLoadedMsg handled synchronously, got handled=%v cmd=%#v", handled, cmd)
	}
	if model.modalHost.Picker == nil || len(model.modalHost.Picker.Items) != 2 {
		t.Fatalf("expected picker items populated, got %#v", model.modalHost.Picker)
	}
	if model.modalHost.Session == nil || model.modalHost.Session.Phase != modal.ModalPhaseReady || model.modalHost.Session.Loading {
		t.Fatalf("expected modal session marked ready, got %#v", model.modalHost.Session)
	}
}

func TestHandleUIStateMessageTerminalAttachedUsesAttachServiceFlow(t *testing.T) {
	client := &recordingBridgeClient{}
	model := setupModel(t, modelOpts{client: client})
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePicker, Phase: modal.ModalPhaseReady, RequestID: "req-1"}
	model.modalHost.Picker = &modal.PickerState{Items: []modal.PickerItem{{TerminalID: "term-1", Name: "shell"}}}
	model.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "req-1"})
	model.pendingPaneAttaches["pane-1"] = "term-1"
	if pane := model.workbench.ActivePane(); pane != nil {
		pane.TerminalID = "term-1"
	}
	binding := model.runtime.BindPane("pane-1")
	binding.Channel = 7
	binding.Connected = true
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Channel = 7
	terminal.State = "running"
	terminal.BoundPaneIDs = []string{"pane-1"}
	terminal.OwnerPaneID = "pane-1"
	terminal.Snapshot = &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}}

	cmd, handled := model.handleUIStateMessage(orchestrator.TerminalAttachedMsg{PaneID: "pane-1", TerminalID: "term-1", Channel: 7})
	if !handled || cmd == nil {
		t.Fatalf("expected TerminalAttachedMsg handled with follow-up cmd, got handled=%v cmd=%#v", handled, cmd)
	}
	if model.modalHost.Session != nil {
		t.Fatalf("expected picker modal closed immediately, got %#v", model.modalHost.Session)
	}
	if model.isPaneAttachPending("pane-1") {
		t.Fatal("expected pending attach cleared before follow-up cmd")
	}
	drainCmd(t, model, cmd, 20)
	if len(client.resizes) != 1 || client.resizes[0].channel != 7 {
		t.Fatalf("expected attached follow-up to resize pane, got %#v", client.resizes)
	}
}

func TestHandleUIStateMessageHostEmojiProbeWritesControlSequence(t *testing.T) {
	model := setupModel(t, modelOpts{})
	writer := &recordingControlWriter{}
	model.cursorOut = writer
	model.hostEmojiProbePending = true

	cmd, handled := model.handleUIStateMessage(hostEmojiProbeMsg{Attempt: 1})
	if !handled {
		t.Fatal("expected hostEmojiProbeMsg to be handled")
	}
	if len(writer.controls) != 1 || writer.controls[0] != hostEmojiVariationProbeSequence {
		t.Fatalf("expected probe control sequence written, got %#v", writer.controls)
	}
	if !model.hostEmojiProbePending {
		t.Fatal("expected probe to remain pending after first attempt")
	}
	if cmd == nil {
		t.Fatal("expected follow-up probe tick command")
	}
	if _, ok := cmd().(hostEmojiProbeMsg); !ok {
		t.Fatalf("expected follow-up hostEmojiProbeMsg, got %#v", cmd())
	}
}

func TestHandleUIStateMessageHostCursorPositionAppliesProbeMode(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.hostEmojiProbePending = true

	cmd, handled := model.handleUIStateMessage(hostCursorPositionMsg{X: 1, Y: 0})
	if !handled || cmd != nil {
		t.Fatalf("expected host cursor position handled synchronously, got handled=%v cmd=%#v", handled, cmd)
	}
	if model.hostEmojiProbePending {
		t.Fatal("expected host emoji probe to complete")
	}
	if got := model.runtime.Visible(); got == nil || got.HostEmojiVS16Mode == "" {
		t.Fatalf("expected runtime visible state updated, got %#v", got)
	}
}
