package layout

import (
	"testing"

	"github.com/lozzow/termx/tui/state/types"
)

func TestLayoutSplitRemoveAndRects(t *testing.T) {
	root := NewLeaf(types.PaneID("pane-1"))
	if ok := root.Split(types.PaneID("pane-1"), types.SplitDirectionVertical, types.PaneID("pane-2")); !ok {
		t.Fatal("expected split to succeed")
	}

	rects := root.Rects(types.Rect{X: 10, Y: 4, W: 11, H: 7})
	if got := rects[types.PaneID("pane-1")]; got != (types.Rect{X: 10, Y: 4, W: 5, H: 7}) {
		t.Fatalf("expected pane-1 rect %+v, got %+v", types.Rect{X: 10, Y: 4, W: 5, H: 7}, got)
	}
	if got := rects[types.PaneID("pane-2")]; got != (types.Rect{X: 15, Y: 4, W: 6, H: 7}) {
		t.Fatalf("expected pane-2 rect %+v, got %+v", types.Rect{X: 15, Y: 4, W: 6, H: 7}, got)
	}

	next := root.Remove(types.PaneID("pane-1"))
	if next == nil || !next.IsLeaf() || next.PaneID != types.PaneID("pane-2") {
		t.Fatalf("expected remove to collapse tree to pane-2, got %#v", next)
	}
}
