package history

import "testing"

func TestLogicalLineContractSmoke(t *testing.T) {
	line := LogicalLine{ID: 1, Seal: SealStateOpen, Dirty: true}
	if line.ID == 0 {
		t.Fatal("expected stable non-zero logical line id")
	}
	if line.Seal != SealStateOpen {
		t.Fatalf("unexpected seal state %q", line.Seal)
	}
	if !line.Dirty {
		t.Fatal("expected dirty line")
	}
}
