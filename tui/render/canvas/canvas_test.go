package canvas

import "testing"

func TestPadRightPreservesWidth(t *testing.T) {
	if got := PadRight("abc", 5); got != "abc  " {
		t.Fatalf("expected padded string, got %q", got)
	}
}
