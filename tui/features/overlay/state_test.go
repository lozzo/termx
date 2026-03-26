package overlay

import "testing"

func TestOpenConnectPickerReplacesActiveOverlay(t *testing.T) {
	state := State{}
	state = state.OpenConnectPicker()
	if state.Active.Kind != KindConnectPicker {
		t.Fatalf("expected connect picker, got %q", state.Active.Kind)
	}

	state = state.Clear()
	if state.Active.Kind != "" {
		t.Fatalf("expected cleared overlay, got %q", state.Active.Kind)
	}
}
