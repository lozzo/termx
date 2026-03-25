package layout

import (
	"testing"

	"github.com/lozzow/termx/tui/domain/types"
)

func TestNodeSplitAndRects(t *testing.T) {
	root := NewLeaf(types.PaneID("left"))
	if ok := root.Split(types.PaneID("left"), types.SplitDirectionVertical, types.PaneID("right")); !ok {
		t.Fatalf("expected split to succeed")
	}

	rects := root.Rects(types.Rect{X: 0, Y: 0, W: 100, H: 20})
	left := rects[types.PaneID("left")]
	right := rects[types.PaneID("right")]

	if left.W != 50 || right.W != 50 {
		t.Fatalf("expected equal split widths, got left=%+v right=%+v", left, right)
	}
	if right.X != 50 {
		t.Fatalf("expected right pane x=50, got %+v", right)
	}
}

func TestNodeRectsKeepsOddSpanSplitCompatibility(t *testing.T) {
	root := NewLeaf(types.PaneID("left"))
	if ok := root.Split(types.PaneID("left"), types.SplitDirectionVertical, types.PaneID("right")); !ok {
		t.Fatalf("expected split to succeed")
	}

	rects := root.Rects(types.Rect{X: 0, Y: 0, W: 5, H: 3})
	left := rects[types.PaneID("left")]
	right := rects[types.PaneID("right")]

	if left.W != 2 || right.W != 3 {
		t.Fatalf("expected odd span to keep floor split 2/3, got left=%+v right=%+v", left, right)
	}
	if right.X != 2 {
		t.Fatalf("expected right pane x=2, got %+v", right)
	}
}

func TestNodeRemoveCollapsesParent(t *testing.T) {
	root := NewLeaf(types.PaneID("root"))
	root.Split(types.PaneID("root"), types.SplitDirectionVertical, types.PaneID("new"))

	next := root.Remove(types.PaneID("root"))
	if next == nil || !next.IsLeaf() || next.PaneID != types.PaneID("new") {
		t.Fatalf("expected tree to collapse to remaining leaf, got %#v", next)
	}
}

func TestNodeAdjacentFindsNeighborByDirection(t *testing.T) {
	root := NewLeaf(types.PaneID("left"))
	root.Split(types.PaneID("left"), types.SplitDirectionVertical, types.PaneID("right"))

	rects := root.Rects(types.Rect{X: 0, Y: 0, W: 80, H: 20})
	got := root.Adjacent(types.PaneID("left"), types.DirectionRight, rects)
	if got != types.PaneID("right") {
		t.Fatalf("expected right neighbor, got %q", got)
	}
}

func TestNodeAdjustPaneBoundaryMovesSplitRatioWithinBounds(t *testing.T) {
	root := NewLeaf(types.PaneID("pane-1"))
	root.Split(types.PaneID("pane-1"), types.SplitDirectionVertical, types.PaneID("pane-2"))

	ok := root.AdjustPaneBoundary(
		types.PaneID("pane-1"),
		types.DirectionRight,
		5,
		10,
		types.Rect{W: 80, H: 24},
	)
	if !ok {
		t.Fatal("expected boundary adjust to succeed")
	}
	if root.Ratio <= 0.5 {
		t.Fatalf("expected ratio to move right, got %v", root.Ratio)
	}
}

func TestNodeSwapWithNeighborSwapsLeafOrder(t *testing.T) {
	root := NewLeaf(types.PaneID("pane-1"))
	root.Split(types.PaneID("pane-1"), types.SplitDirectionVertical, types.PaneID("pane-2"))

	if !root.SwapWithNeighbor(types.PaneID("pane-1"), 1) {
		t.Fatal("expected swap to succeed")
	}

	ids := root.LeafIDs()
	if len(ids) != 2 {
		t.Fatalf("expected two leaves, got %v", ids)
	}
	if ids[0] != types.PaneID("pane-2") || ids[1] != types.PaneID("pane-1") {
		t.Fatalf("expected swapped order, got %v", ids)
	}
}
