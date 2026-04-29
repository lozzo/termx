package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/orchestrator"
)

func TestHandleTerminalEventMessageResizeReloadsSnapshotWhenStreamIdle(t *testing.T) {
	client := &recordingBridgeClient{
		snapshotByTerminal: map[string]*protocol.Snapshot{
			"term-1": {TerminalID: "term-1", Size: protocol.Size{Cols: 88, Rows: 26}},
		},
	}
	model := setupModel(t, modelOpts{client: client})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.State = "running"

	cmd, handled := model.handleTerminalEventMessage(terminalEventMsg{Event: protocol.Event{
		Type:       protocol.EventTerminalResized,
		TerminalID: "term-1",
	}})
	if !handled || cmd == nil {
		t.Fatalf("expected terminal resize event handled with reload cmd, got handled=%v cmd=%#v", handled, cmd)
	}
	msg := cmd()
	if _, ok := msg.(orchestrator.SnapshotLoadedMsg); !ok {
		t.Fatalf("expected snapshot reload message, got %#v", msg)
	}
}

func TestHandleTerminalEventMessageResizeSkipsReloadWhenStreamActive(t *testing.T) {
	model := setupModel(t, modelOpts{})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Stream.Active = true

	cmd, handled := model.handleTerminalEventMessage(terminalEventMsg{Event: protocol.Event{
		Type:       protocol.EventTerminalResized,
		TerminalID: "term-1",
	}})
	if !handled || cmd != nil {
		t.Fatalf("expected active-stream resize event handled without reload cmd, got handled=%v cmd=%#v", handled, cmd)
	}
}

func TestHandleTerminalEventMessageResizeIgnoresMissingTerminal(t *testing.T) {
	model := setupModel(t, modelOpts{})

	cmd, handled := model.handleTerminalEventMessage(terminalEventMsg{Event: protocol.Event{
		Type:       protocol.EventTerminalResized,
		TerminalID: "term-missing",
	}})
	if !handled || cmd != nil {
		t.Fatalf("expected missing-terminal resize event handled without cmd, got handled=%v cmd=%#v", handled, cmd)
	}
}

func TestHandleTerminalEventMessageUnknownEventFallsThroughAsHandled(t *testing.T) {
	model := setupModel(t, modelOpts{})

	cmd, handled := model.handleTerminalEventMessage(terminalEventMsg{Event: protocol.Event{
		Type:       protocol.EventTerminalCreated,
		TerminalID: "term-1",
	}})
	if !handled || cmd != nil {
		t.Fatalf("expected unknown terminal event handled without cmd, got handled=%v cmd=%#v", handled, cmd)
	}
}

func TestHandleTerminalEventMessageFallsThroughForNonTerminalEventMsg(t *testing.T) {
	model := setupModel(t, modelOpts{})
	cmd, handled := model.handleTerminalEventMessage(tea.WindowSizeMsg{Width: 80, Height: 24})
	if handled || cmd != nil {
		t.Fatalf("expected non-terminal event msg to fall through, got handled=%v cmd=%#v", handled, cmd)
	}
}
