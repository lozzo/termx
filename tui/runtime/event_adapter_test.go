package runtime

import (
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/core/types"
)

func TestEventAdapterMapsExitedAndRemovedEvents(t *testing.T) {
	adapter := EventAdapter{}

	msg := adapter.Normalize(protocol.Event{
		Type:       protocol.EventTerminalStateChanged,
		TerminalID: "term-1",
		StateChanged: &protocol.TerminalStateChangedData{
			NewState: "exited",
		},
	})
	exited, ok := msg.(app.MessageTerminalExited)
	if !ok {
		t.Fatalf("expected MessageTerminalExited, got %T", msg)
	}
	if exited.TerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected exited terminal term-1, got %q", exited.TerminalID)
	}

	msg = adapter.Normalize(protocol.Event{
		Type:       protocol.EventTerminalRemoved,
		TerminalID: "term-2",
		Removed:    &protocol.TerminalRemovedData{Reason: "removed"},
	})
	removed, ok := msg.(app.MessageTerminalRemoved)
	if !ok {
		t.Fatalf("expected MessageTerminalRemoved, got %T", msg)
	}
	if removed.TerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected removed terminal term-2, got %q", removed.TerminalID)
	}
}
