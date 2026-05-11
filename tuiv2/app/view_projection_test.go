package app

import "testing"

func TestLocalViewProjectionPreservesPaneViewport(t *testing.T) {
	model := setupModel(t, modelOpts{})

	_ = model.runtime.SetPaneViewportOffset("pane-1", 4)
	_ = model.runtime.SetPaneContentOffset("pane-1", 3, 2)
	proj := model.captureLocalViewProjection()
	_ = model.runtime.SetPaneViewportOffset("pane-1", 0)
	_ = model.runtime.SetPaneContentOffset("pane-1", 0, 0)

	model.applyLocalViewProjection(proj)

	if got := model.runtime.PaneViewportOffset("pane-1"); got != 4 {
		t.Fatalf("expected local view projection to restore pane viewport 4, got %d", got)
	}
	if gotX, gotY := model.runtime.PaneContentOffset("pane-1"); gotX != 3 || gotY != 2 {
		t.Fatalf("expected local view projection to restore pane content offset 3,2 got %d,%d", gotX, gotY)
	}
}

func TestLocalViewProjectionNilModelCompatibility(t *testing.T) {
	var model *Model

	proj := model.captureLocalViewProjection()
	if proj.WorkspaceName != "" || proj.ActiveTabID != "" || proj.FocusedPaneID != "" {
		t.Fatalf("expected zero projection for nil model, got %#v", proj)
	}

	model.applyLocalViewProjection(localViewProjection{
		WorkspaceName: "main",
		ActiveTabID:   "tab-1",
		FocusedPaneID: "pane-1",
	})
}
