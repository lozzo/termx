package app

import "testing"

func TestLocalViewProjectionPreservesMultiplePaneOffsets(t *testing.T) {
	model := setupSplitCopyModeModel(t)

	_ = model.runtime.SetPaneViewportOffset("pane-1", 4)
	_ = model.runtime.SetPaneContentOffset("pane-1", 3, 2)
	_ = model.runtime.SetPaneViewportOffset("pane-2", 11)
	_ = model.runtime.SetPaneContentOffset("pane-2", -5, 7)

	proj := model.captureLocalViewProjection()

	_ = model.runtime.SetPaneViewportOffset("pane-1", 0)
	_ = model.runtime.SetPaneContentOffset("pane-1", 0, 0)
	_ = model.runtime.SetPaneViewportOffset("pane-2", 0)
	_ = model.runtime.SetPaneContentOffset("pane-2", 0, 0)

	model.applyLocalViewProjection(proj)

	if got := model.runtime.PaneViewportOffset("pane-1"); got != 4 {
		t.Fatalf("expected pane-1 viewport restored to 4, got %d", got)
	}
	if gotX, gotY := model.runtime.PaneContentOffset("pane-1"); gotX != 3 || gotY != 2 {
		t.Fatalf("expected pane-1 content offset restored to 3,2 got %d,%d", gotX, gotY)
	}
	if got := model.runtime.PaneViewportOffset("pane-2"); got != 11 {
		t.Fatalf("expected pane-2 viewport restored to 11, got %d", got)
	}
	if gotX, gotY := model.runtime.PaneContentOffset("pane-2"); gotX != -5 || gotY != 7 {
		t.Fatalf("expected pane-2 content offset restored to -5,7 got %d,%d", gotX, gotY)
	}
}
