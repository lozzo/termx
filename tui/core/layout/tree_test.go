package layout

import (
	"testing"

	"github.com/lozzow/termx/tui/core/types"
)

func TestLayoutSplitAndProjectRects(t *testing.T) {
	root := NewLeaf(types.PaneID("pane-1"))
	root, ok := root.Split(types.PaneID("pane-1"), types.SplitVertical, types.PaneID("pane-2"))
	if !ok {
		t.Fatal("expected split success")
	}

	rects := root.Project(types.Rect{X: 0, Y: 0, W: 120, H: 40})
	if len(rects) != 2 {
		t.Fatalf("expected 2 rects, got %d", len(rects))
	}
	if got := rects[types.PaneID("pane-1")]; got.W != 60 || got.H != 40 {
		t.Fatalf("expected pane-1 to get left half, got %#v", got)
	}
	if got := rects[types.PaneID("pane-2")]; got.X != 60 || got.W != 60 || got.H != 40 {
		t.Fatalf("expected pane-2 to get right half, got %#v", got)
	}
}

func TestLayoutRemoveCollapsesBranch(t *testing.T) {
	root := NewLeaf(types.PaneID("pane-1"))
	root, ok := root.Split(types.PaneID("pane-1"), types.SplitHorizontal, types.PaneID("pane-2"))
	if !ok {
		t.Fatal("expected split success")
	}

	root = root.Remove(types.PaneID("pane-2"))
	if root == nil {
		t.Fatal("expected root to keep remaining pane")
	}
	if !root.IsLeaf() {
		t.Fatal("expected collapsed root to be leaf")
	}
	if root.PaneID != types.PaneID("pane-1") {
		t.Fatalf("expected remaining pane-1, got %q", root.PaneID)
	}
}
