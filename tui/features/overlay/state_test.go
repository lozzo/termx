package overlay

import "testing"

func TestOverlayOpenAndClear(t *testing.T) {
	state := State{}
	state = state.OpenConnectPicker()
	if state.Active.Kind != KindConnectPicker || state.Active.Title != "connect" {
		t.Fatalf("expected connect picker overlay, got %#v", state.Active)
	}

	state = state.OpenHelp()
	if state.Active.Kind != KindHelp || state.Active.Title != "help" {
		t.Fatalf("expected help overlay, got %#v", state.Active)
	}

	state = state.OpenPrompt("remove terminal")
	if state.Active.Kind != KindPrompt || state.Active.Title != "remove terminal" {
		t.Fatalf("expected prompt overlay, got %#v", state.Active)
	}

	state = state.Clear()
	if state.Active.Kind != "" {
		t.Fatalf("expected cleared overlay, got %q", state.Active.Kind)
	}
}
