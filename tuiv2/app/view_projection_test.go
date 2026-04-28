package app

import "testing"

func TestLocalViewProjectionPreservesPaneViewport(t *testing.T) {
	model := setupModel(t, modelOpts{})

	_ = model.runtime.SetPaneViewportOffset("pane-1", 4)
	proj := model.captureLocalViewProjection()
	_ = model.runtime.SetPaneViewportOffset("pane-1", 0)

	model.applyLocalViewProjection(proj)

	if got := model.runtime.PaneViewportOffset("pane-1"); got != 4 {
		t.Fatalf("expected local view projection to restore pane viewport 4, got %d", got)
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
