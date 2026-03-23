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
