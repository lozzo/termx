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

func TestRouteCtrlFAndCtrlVToUIIntents(t *testing.T) {
	ctrlF := Route(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "\x06", Ctrl: true}, false)
	if ctrlF.Kind != IntentOpenTerminalPicker {
		t.Fatalf("expected terminal picker intent, got %#v", ctrlF)
	}
	ctrlV := Route(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "\x16", Ctrl: true}, false)
	if ctrlV.Kind != IntentEnterCopyMode {
		t.Fatalf("expected display/copy intent, got %#v", ctrlV)
	}
	namedCtrlF := Route(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "f", Ctrl: true}, false)
	if namedCtrlF.Kind != IntentOpenTerminalPicker {
		t.Fatalf("expected named ctrl-f terminal picker intent, got %#v", namedCtrlF)
	}
}

func TestRouteInteractionModePrefixesAndModeKeys(t *testing.T) {
	ctrlP := Route(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "\x10", Ctrl: true}, false)
	if ctrlP.Kind != IntentSetInteractionMode || ctrlP.Mode != InteractionModePane {
		t.Fatalf("expected pane mode intent, got %#v", ctrlP)
	}
	paneSplit := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "v"}, false, InteractionModePane)
	if paneSplit.Kind != IntentPaneCommand || paneSplit.Command != "pane split-right" {
		t.Fatalf("expected split-right pane command, got %#v", paneSplit)
	}
	resizeRight := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyRight}, false, InteractionModeResize)
	if resizeRight.Kind != IntentPaneCommand || resizeRight.Command != "pane resize right delta=2" {
		t.Fatalf("expected resize-right pane command, got %#v", resizeRight)
	}
	globalFooter := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyChar, Char: "f"}, false, InteractionModeGlobal)
	if globalFooter.Kind != IntentShellAction || globalFooter.Action != ShellActionToggleFooter {
		t.Fatalf("expected global footer action, got %#v", globalFooter)
	}
	esc := RouteWithMode(InputEvent{Kind: EventKindKey, Key: KeyEsc}, false, InteractionModePane)
	if esc.Kind != IntentExitInteraction {
		t.Fatalf("expected interaction exit, got %#v", esc)
	}
}
