package tui

import "testing"

func TestLayoutSplitAndRects(t *testing.T) {
	root := NewLeaf("p1")
	root.Split("p1", SplitVertical, "p2")

	rects := root.Rects(Rect{X: 0, Y: 0, W: 100, H: 20})
	if len(rects) != 2 {
		t.Fatalf("expected 2 rects, got %d", len(rects))
	}
	if rects["p1"].W+rects["p2"].W != 100 {
		t.Fatalf("unexpected widths: %#v %#v", rects["p1"], rects["p2"])
	}

	root.Split("p2", SplitHorizontal, "p3")
	rects = root.Rects(Rect{X: 0, Y: 0, W: 100, H: 20})
	if rects["p3"].H == 0 || rects["p2"].H == 0 {
		t.Fatalf("expected non-zero heights: %#v %#v", rects["p2"], rects["p3"])
	}
}

func TestLayoutAdjacent(t *testing.T) {
	root := NewLeaf("p1")
	root.Split("p1", SplitVertical, "p2")
	root.Split("p2", SplitHorizontal, "p3")

	rects := root.Rects(Rect{X: 0, Y: 0, W: 120, H: 40})
	if got := root.Adjacent("p1", DirectionRight, rects); got != "p2" && got != "p3" {
		t.Fatalf("expected right neighbor, got %q", got)
	}
	if got := root.Adjacent("p3", DirectionUp, rects); got != "p2" {
		t.Fatalf("expected p2 above p3, got %q", got)
	}
}

func TestLayoutSwapWithNeighbor(t *testing.T) {
	root := NewLeaf("p1")
	root.Split("p1", SplitVertical, "p2")
	root.Split("p2", SplitHorizontal, "p3")

	if ok := root.SwapWithNeighbor("p3", -1); !ok {
		t.Fatal("expected swap with previous leaf to succeed")
	}

	got := root.LeafIDs()
	want := []string{"p1", "p3", "p2"}
	if len(got) != len(want) {
		t.Fatalf("expected %d leaves, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected leaf order %v, got %v", want, got)
		}
	}

	if ok := root.SwapWithNeighbor("p1", -1); ok {
		t.Fatal("expected swap before first leaf to fail")
	}
}
