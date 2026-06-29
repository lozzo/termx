package termxcorev2

import "testing"

func TestModuleName(t *testing.T) {
	if ModuleName != "termx-core-v2" {
		t.Fatalf("unexpected module name %q", ModuleName)
	}
}
