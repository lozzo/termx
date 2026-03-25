package runtime

import (
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
)

func TestUpdateLoopConvertsDaemonEventsIntoMessages(t *testing.T) {
	events := make(chan protocol.Event, 1)
	loop := NewUpdateLoop(events)

	events <- protocol.Event{
		Type:       protocol.EventTerminalRemoved,
		TerminalID: "term-1",
		Timestamp:  time.Unix(1, 0),
	}

	msg, ok := loop.Next()
	if !ok {
		t.Fatal("expected one converted message")
	}
	if msg.Event.TerminalID != "term-1" || msg.Event.Type != protocol.EventTerminalRemoved {
		t.Fatalf("unexpected update message: %#v", msg)
	}
}
