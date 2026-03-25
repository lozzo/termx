package app

import "testing"

func TestRootModelCanSwitchBetweenWorkbenchAndTerminalPoolScreens(t *testing.T) {
	model := NewModel()
	if model.Screen != ScreenWorkbench {
		t.Fatalf("expected initial screen %q, got %q", ScreenWorkbench, model.Screen)
	}
	if model.FocusTarget != FocusWorkbench {
		t.Fatalf("expected initial focus %q, got %q", FocusWorkbench, model.FocusTarget)
	}
	if model.Overlay.HasActive() {
		t.Fatalf("expected no active overlay, got %#v", model.Overlay)
	}

	model = model.SwitchScreen(ScreenTerminalPool)
	if model.Screen != ScreenTerminalPool {
		t.Fatalf("expected switched screen %q, got %q", ScreenTerminalPool, model.Screen)
	}
	if model.FocusTarget != FocusTerminalPool {
		t.Fatalf("expected switched focus %q, got %q", FocusTerminalPool, model.FocusTarget)
	}

	model = model.SwitchScreen(ScreenWorkbench)
	if model.Screen != ScreenWorkbench {
		t.Fatalf("expected switched back screen %q, got %q", ScreenWorkbench, model.Screen)
	}
	if model.FocusTarget != FocusWorkbench {
		t.Fatalf("expected switched back focus %q, got %q", FocusWorkbench, model.FocusTarget)
	}
}

func TestOpeningOverlayKeepsWorkbenchFocusTarget(t *testing.T) {
	model := NewModel()
	model = model.Apply(IntentOpenHelp)
	if model.FocusTarget != FocusWorkbench {
		t.Fatalf("expected overlay to keep workbench focus target, got %q", model.FocusTarget)
	}
	if !model.Overlay.HasActive() {
		t.Fatal("expected help overlay to be active")
	}
}
