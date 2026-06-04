package input

import "testing"

func TestInputEventKind(t *testing.T) {
	event := InputEvent{Kind: EventKindKey}
	if event.Kind != EventKindKey {
		t.Fatalf("unexpected input event kind %q", event.Kind)
	}
}

func TestRoutePageUpEntersOrRequestsCopyMode(t *testing.T) {
	event := InputEvent{Kind: EventKindKey, Key: KeyPageUp}
	if intent := Route(event, false); intent.Kind != IntentEnterCopyMode {
		t.Fatalf("expected enter copy mode, got %#v", intent)
	}
	if intent := Route(event, true); intent.Kind != IntentRequestOlder {
		t.Fatalf("expected older request, got %#v", intent)
	}
}

func TestRouteWheelUpEntersOrRequestsCopyMode(t *testing.T) {
	event := InputEvent{Kind: EventKindMouse, Mouse: MouseWheelUp}
	if intent := Route(event, false); intent.Kind != IntentEnterCopyMode {
		t.Fatalf("expected enter copy mode, got %#v", intent)
	}
	if intent := Route(event, true); intent.Kind != IntentRequestOlder {
		t.Fatalf("expected older request, got %#v", intent)
	}
}

func TestRouteTerminalInputAndCopyModeSelection(t *testing.T) {
	key := Route(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "x"}, false)
	if key.Kind != IntentTerminalInput || string(key.Bytes) != "x" {
		t.Fatalf("unexpected terminal input intent %#v", key)
	}
	enter := Route(InputEvent{Kind: EventKindKey, Key: KeyEnter}, false)
	if enter.Kind != IntentTerminalInput || string(enter.Bytes) != "\r" {
		t.Fatalf("unexpected enter intent %#v", enter)
	}
	esc := Route(InputEvent{Kind: EventKindKey, Key: KeyEsc}, false)
	if esc.Kind != IntentTerminalInput || string(esc.Bytes) != "\x1b" {
		t.Fatalf("unexpected esc intent %#v", esc)
	}

	mouse := Route(InputEvent{Kind: EventKindMouse, Mouse: MouseLeft, Row: 2, Col: 3}, true)
	if mouse.Kind != IntentMouseSelect || mouse.Event.Row != 2 || mouse.Event.Col != 3 {
		t.Fatalf("unexpected mouse selection intent %#v", mouse)
	}
}
