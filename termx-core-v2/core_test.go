package termxcorev2

import "testing"

func TestModuleName(t *testing.T) {
	if ModuleName != "termx-core-v2" {
		t.Fatalf("unexpected module name %q", ModuleName)
	}
}

func TestSmokeHistoryWindow(t *testing.T) {
	window, err := SmokeHistoryWindow()
	if err != nil {
		t.Fatalf("smoke history window: %v", err)
	}
	if len(window.Rows) != 1 || window.Rows[0].Text != "termx-core-v2" {
		t.Fatalf("unexpected smoke window %#v", window)
	}
}
