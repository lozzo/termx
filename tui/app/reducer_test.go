package app

import "testing"

func TestReducerCanSwitchBetweenWorkbenchAndTerminalPool(t *testing.T) {
	model := NewModel("main")

	model, effects := Reduce(model, IntentOpenTerminalPool)
	if len(effects) != 0 {
		t.Fatalf("expected no effects for screen switch, got %d", len(effects))
	}
	if model.Screen != ScreenTerminalPool {
		t.Fatalf("expected terminal pool screen, got %q", model.Screen)
	}

	model, effects = Reduce(model, IntentCloseScreen)
	if len(effects) != 0 {
		t.Fatalf("expected no effects when closing screen, got %d", len(effects))
	}
	if model.Screen != ScreenWorkbench {
		t.Fatalf("expected workbench screen after close, got %q", model.Screen)
	}
}
