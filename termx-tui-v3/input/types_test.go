package input

import "testing"

func TestInputEventKind(t *testing.T) {
	event := InputEvent{Kind: EventKindKey}
	if event.Kind != EventKindKey {
		t.Fatalf("unexpected input event kind %q", event.Kind)
	}
}
