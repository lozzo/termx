package termx

import (
	"context"
	"testing"
	"time"
)

func TestEventBusFilters(t *testing.T) {
	bus := NewEventBus(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := bus.Subscribe(ctx, WithTerminalFilter("abc12345"), WithTypeFilter(EventTerminalCreated))

	bus.Publish(Event{Type: EventTerminalResized, TerminalID: "abc12345", Timestamp: time.Now()})
	bus.Publish(Event{Type: EventTerminalCreated, TerminalID: "other", Timestamp: time.Now()})
	want := Event{Type: EventTerminalCreated, TerminalID: "abc12345", Timestamp: time.Now()}
	bus.Publish(want)

	select {
	case got := <-ch:
		if got.Type != want.Type || got.TerminalID != want.TerminalID {
			t.Fatalf("unexpected event: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}
