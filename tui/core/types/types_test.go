package types

import "testing"

func TestRectRightAndBottom(t *testing.T) {
	rect := Rect{X: 3, Y: 4, W: 10, H: 6}
	if rect.Right() != 13 {
		t.Fatalf("expected right edge 13, got %d", rect.Right())
	}
	if rect.Bottom() != 10 {
		t.Fatalf("expected bottom edge 10, got %d", rect.Bottom())
	}
}
