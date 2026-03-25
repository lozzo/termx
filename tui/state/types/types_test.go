package types

import "testing"

func TestSplitDirectionNormalizeFallsBackToVertical(t *testing.T) {
	if got := SplitDirection("unknown").Normalize(); got != SplitDirectionVertical {
		t.Fatalf("expected unknown direction to normalize to %q, got %q", SplitDirectionVertical, got)
	}
}

func TestRectEmptyDetectsNonPositiveArea(t *testing.T) {
	if !(Rect{W: 0, H: 3}).Empty() {
		t.Fatal("expected zero-width rect to be empty")
	}
	if !(Rect{W: 4, H: 0}).Empty() {
		t.Fatal("expected zero-height rect to be empty")
	}
	if (Rect{W: 4, H: 3}).Empty() {
		t.Fatal("expected positive rect to be non-empty")
	}
}
