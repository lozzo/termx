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
