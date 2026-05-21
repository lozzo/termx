package app

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/input"
)

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

func TestCopyModeSelectionIgnoresObserverWidthRewrapOfCanonicalRows(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"alpha", "bravo", "charlie"}, []string{"live0"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot.Size.Cols = 80
	terminal.Snapshot.ScrollbackWrapped = []bool{true, false, false}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	model.width = 12
	model.height = 6
	model.copyMode.Mark = &copyModePoint{Row: 0, Col: 0}
	model.copyMode.Cursor = copyModePoint{Row: 2, Col: 6}

	text, ok := model.copyModeSelectedText()
	if !ok {
		t.Fatal("expected selection text")
	}
	if text != "alphabravo\ncharlie" {
		t.Fatalf("expected canonical wrapped rows to survive observer narrowing, got %q", text)
	}
}

func TestCopyModeExitResetsPaneViewport(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"s0", "s1", "s2", "s3", "s4"}, []string{"live0", "live1", "live2", "live3"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	if got := model.runtime.PaneViewportOffset("pane-1"); got <= 0 {
		t.Fatalf("expected copy mode to project a non-zero pane viewport before exit, got %d", got)
	}

	model.setMode(input.ModeState{Kind: input.ModeNormal})

	if got := model.runtime.PaneViewportOffset("pane-1"); got != 0 {
		t.Fatalf("expected copy-mode exit to reset pane viewport to 0, got %d", got)
	}
	if model.copyMode.PaneID != "" {
		t.Fatalf("expected active copy-mode state cleared on exit, got %#v", model.copyMode)
	}
	if _, ok := model.copyModeStateForPane("pane-1"); ok {
		t.Fatal("expected pane-1 copy-mode state removed on exit")
	}
}
