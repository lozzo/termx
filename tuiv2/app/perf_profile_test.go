package app

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestPerfProfileLayoutKindLabelsPureLeftRightSplitAsSideBySide(t *testing.T) {
	visible := &workbench.VisibleWorkbench{
		ActiveTab: 0,
		Tabs: []workbench.VisibleTab{{
			ID: "tab-1",
			Panes: []workbench.VisiblePane{
				{ID: "left", Rect: workbench.Rect{X: 0, Y: 0, W: 60, H: 20}},
				{ID: "right", Rect: workbench.Rect{X: 60, Y: 0, W: 60, H: 20}},
			},
		}},
	}

	if got := perfProfileLayoutKind(visible); got != "side-by-side" {
		t.Fatalf("expected pure left/right split to be side-by-side, got %q", got)
	}
}

func TestPerfProfileLayoutKindKeepsMixedTiledLayoutAsMixed(t *testing.T) {
	visible := &workbench.VisibleWorkbench{
		ActiveTab: 0,
		Tabs: []workbench.VisibleTab{{
			ID: "tab-1",
			Panes: []workbench.VisiblePane{
				{ID: "left", Rect: workbench.Rect{X: 0, Y: 0, W: 60, H: 20}},
				{ID: "top-right", Rect: workbench.Rect{X: 60, Y: 0, W: 60, H: 10}},
				{ID: "bottom-right", Rect: workbench.Rect{X: 60, Y: 10, W: 60, H: 10}},
			},
		}},
	}

	if got := perfProfileLayoutKind(visible); got != "mixed" {
		t.Fatalf("expected mixed tiled layout to stay mixed, got %q", got)
	}
}
