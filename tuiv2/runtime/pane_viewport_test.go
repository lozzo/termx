package runtime

import "testing"

func TestPaneViewportOffsetVisibleStateTracksBinding(t *testing.T) {
	rt := New(nil)

	if changed := rt.SetPaneViewportOffset("pane-2", 3); !changed {
		t.Fatal("expected initial viewport set to change runtime state")
	}
	if got := rt.PaneViewportOffset("pane-2"); got != 3 {
		t.Fatalf("expected pane viewport 3, got %d", got)
	}
	if next, changed := rt.AdjustPaneViewportOffset("pane-2", -2); !changed || next != 1 {
		t.Fatalf("expected viewport adjust to land on 1, next=%d changed=%v", next, changed)
	}

	visible := rt.Visible()
	if visible == nil {
		t.Fatal("expected visible runtime")
	}
	if len(visible.Bindings) != 1 {
		t.Fatalf("expected one visible binding, got %#v", visible.Bindings)
	}
	if got := visible.Bindings[0].ViewportOffset; got != 1 {
		t.Fatalf("expected visible binding viewport 1, got %d", got)
	}
}

func TestPaneContentOffsetVisibleStateTracksBinding(t *testing.T) {
	rt := New(nil)

	if changed := rt.SetPaneContentOffset("pane-2", -3, 4); !changed {
		t.Fatal("expected initial content offset set to change runtime state")
	}
	if gotX, gotY := rt.PaneContentOffset("pane-2"); gotX != -3 || gotY != 4 {
		t.Fatalf("expected pane content offset -3,4 got %d,%d", gotX, gotY)
	}
	if nextX, nextY, changed := rt.AdjustPaneContentOffset("pane-2", 2, -6); !changed || nextX != -1 || nextY != -2 {
		t.Fatalf("expected content offset adjust to land on -1,-2, next=%d,%d changed=%v", nextX, nextY, changed)
	}

	visible := rt.Visible()
	if visible == nil {
		t.Fatal("expected visible runtime")
	}
	if len(visible.Bindings) != 1 {
		t.Fatalf("expected one visible binding, got %#v", visible.Bindings)
	}
	if got := visible.Bindings[0].ContentOffsetX; got != -1 {
		t.Fatalf("expected visible binding content offset x -1, got %d", got)
	}
	if got := visible.Bindings[0].ContentOffsetY; got != -2 {
		t.Fatalf("expected visible binding content offset y -2, got %d", got)
	}
	if changed := rt.ResetPaneContentOffset("pane-2"); !changed {
		t.Fatal("expected reset content offset to change runtime state")
	}
	if gotX, gotY := rt.PaneContentOffset("pane-2"); gotX != 0 || gotY != 0 {
		t.Fatalf("expected reset pane content offset 0,0 got %d,%d", gotX, gotY)
	}
}
