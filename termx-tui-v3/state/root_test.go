package state

import "testing"

func TestRootAdvance(t *testing.T) {
	next := (Root{}).Advance()
	if next.Generation != 1 {
		t.Fatalf("expected generation 1, got %d", next.Generation)
	}
}
