package render

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestEmptyPaneActionRegionsReturnsStableRowsAndActions(t *testing.T) {
	pane := workbench.VisiblePane{
		ID:   "pane-1",
		Rect: workbench.Rect{X: 4, Y: 2, W: 52, H: 12},
	}
	regions := EmptyPaneActionRegions(pane)
	if len(regions) != 4 {
		t.Fatalf("expected 4 action regions, got %#v", regions)
	}

	content := contentRectForPane(pane.Rect)
	wantKinds := []HitRegionKind{
		HitRegionEmptyPaneAttach,
		HitRegionEmptyPaneCreate,
		HitRegionEmptyPaneManager,
		HitRegionEmptyPaneClose,
	}
	wantActions := []input.ActionKind{
		input.ActionOpenPicker,
		input.ActionOpenPicker,
		input.ActionOpenTerminalManager,
		input.ActionClosePane,
	}
	for i, region := range regions {
		if region.Kind != wantKinds[i] {
			t.Fatalf("region[%d] kind=%q, want %q", i, region.Kind, wantKinds[i])
		}
		if region.Rect.X != content.X || region.Rect.W != content.W || region.Rect.H != 1 {
			t.Fatalf("region[%d] rect=%+v, want X=%d W=%d H=1", i, region.Rect, content.X, content.W)
		}
		if i > 0 && region.Rect.Y != regions[i-1].Rect.Y+1 {
			t.Fatalf("region rows must be consecutive: %#v", regions)
		}
		if region.Action.Kind != wantActions[i] {
			t.Fatalf("region[%d] action=%q, want %q", i, region.Action.Kind, wantActions[i])
		}
		if region.Action.PaneID != pane.ID {
			t.Fatalf("region[%d] action pane=%q, want %q", i, region.Action.PaneID, pane.ID)
		}
		if region.PaneID != pane.ID {
			t.Fatalf("region[%d] pane=%q, want %q", i, region.PaneID, pane.ID)
		}
	}
	if regions[0].Action.TargetID != pane.ID || regions[1].Action.TargetID != pane.ID {
		t.Fatalf("picker actions must scope target to pane, got %#v %#v", regions[0].Action, regions[1].Action)
	}
}

func TestEmptyPaneActionRegionsSkipsBoundPane(t *testing.T) {
	pane := workbench.VisiblePane{
		ID:         "pane-1",
		TerminalID: "term-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 40, H: 10},
	}
	regions := EmptyPaneActionRegions(pane)
	if len(regions) != 0 {
		t.Fatalf("expected no empty-pane regions for bound pane, got %#v", regions)
	}
}

func TestLayoutEmptyPaneActionsClampsToVisibleHeight(t *testing.T) {
	layout := layoutEmptyPaneActions(workbench.Rect{X: 2, Y: 3, W: 30, H: 2}, "pane-1")
	if len(layout) != 2 {
		t.Fatalf("expected 2 visible action rows, got %#v", layout)
	}
	if layout[0].spec.Kind != HitRegionEmptyPaneAttach || layout[1].spec.Kind != HitRegionEmptyPaneCreate {
		t.Fatalf("expected first actions to remain stable, got %#v", layout)
	}
}

func TestEmptyPaneActionRegionsUseFramelessPaneRect(t *testing.T) {
	pane := workbench.VisiblePane{
		ID:        "pane-1",
		Rect:      workbench.Rect{X: 0, Y: 0, W: 40, H: 8},
		Frameless: true,
	}

	regions := EmptyPaneActionRegions(pane)
	if len(regions) == 0 {
		t.Fatalf("expected frameless empty-pane regions, got %#v", regions)
	}
	if regions[0].Rect.Y != 1 {
		t.Fatalf("expected frameless actions to start inside the visible pane body without frame offset, got %#v", regions)
	}
	if regions[0].Rect.X != pane.Rect.X || regions[0].Rect.W != pane.Rect.W {
		t.Fatalf("expected frameless actions to use full pane width, got %#v", regions[0].Rect)
	}
}
