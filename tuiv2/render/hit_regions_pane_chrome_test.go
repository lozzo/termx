package render

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestPaneChromeHitRegionsExposeStableTiledActions(t *testing.T) {
	pane := workbench.VisiblePane{
		ID:   "pane-1",
		Rect: workbench.Rect{X: 2, Y: 1, W: 64, H: 12},
	}

	regions := PaneChromeHitRegions(pane, nil, "")
	if len(regions) != 4 {
		t.Fatalf("expected 4 tiled pane chrome regions, got %#v", regions)
	}

	wantKinds := []HitRegionKind{
		HitRegionPaneZoom,
		HitRegionPaneSplitV,
		HitRegionPaneSplitH,
		HitRegionPaneClose,
	}
	wantActions := []input.ActionKind{
		input.ActionZoomPane,
		input.ActionSplitPane,
		input.ActionSplitPaneHorizontal,
		input.ActionClosePane,
	}
	for i, region := range regions {
		if region.Kind != wantKinds[i] {
			t.Fatalf("region[%d] kind=%q, want %q", i, region.Kind, wantKinds[i])
		}
		if region.Action.Kind != wantActions[i] {
			t.Fatalf("region[%d] action=%q, want %q", i, region.Action.Kind, wantActions[i])
		}
		if region.PaneID != pane.ID || region.Action.PaneID != pane.ID {
			t.Fatalf("region[%d] pane scope mismatch: %#v", i, region)
		}
		if region.Rect.Y != pane.Rect.Y || region.Rect.H != 1 {
			t.Fatalf("region[%d] rect=%+v, want top border row", i, region.Rect)
		}
		if i > 0 && region.Rect.X <= regions[i-1].Rect.X {
			t.Fatalf("expected left-to-right slot ordering, got %#v", regions)
		}
	}
}

func TestPaneChromeHitRegionsHideSplitActionsForFloatingPanes(t *testing.T) {
	pane := workbench.VisiblePane{
		ID:       "pane-1",
		Rect:     workbench.Rect{X: 4, Y: 3, W: 40, H: 10},
		Floating: true,
	}

	regions := PaneChromeHitRegions(pane, nil, "")
	if len(regions) != 4 {
		t.Fatalf("expected 4 floating pane chrome regions, got %#v", regions)
	}
	wantKinds := []HitRegionKind{
		HitRegionPaneCenterFloating,
		HitRegionPaneCollapseFloating,
		HitRegionPaneZoom,
		HitRegionPaneClose,
	}
	for i, want := range wantKinds {
		if regions[i].Kind != want {
			t.Fatalf("expected floating region[%d] kind=%q, got %#v", i, want, regions)
		}
	}
}

func TestPaneChromeHitRegionsKeepAttachedAndLayoutActionsHidden(t *testing.T) {
	pane := workbench.VisiblePane{
		ID:         "pane-1",
		TerminalID: "term-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 96, H: 20},
	}

	regions := PaneChromeHitRegions(pane, nil, "")
	wantKinds := []HitRegionKind{
		HitRegionPaneZoom,
		HitRegionPaneSplitV,
		HitRegionPaneSplitH,
		HitRegionPaneClose,
	}
	if len(regions) != len(wantKinds) {
		t.Fatalf("expected %d pane chrome regions, got %#v", len(wantKinds), regions)
	}
	for i, want := range wantKinds {
		if regions[i].Kind != want {
			t.Fatalf("expected region[%d] kind=%q, got %#v", i, want, regions)
		}
	}
}

func TestPaneChromeHitRegionsExposeSizeLockButtonWhenRuntimeKnowsTerminal(t *testing.T) {
	pane := workbench.VisiblePane{
		ID:         "pane-1",
		TerminalID: "term-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 96, H: 20},
	}
	runtimeState := &VisibleRuntimeStateProxy{
		Terminals: []runtime.VisibleTerminal{{
			TerminalID: "term-1",
			Name:       "shell",
			State:      "running",
		}},
	}

	regions := PaneChromeHitRegions(pane, runtimeState, "")
	found := false
	for _, region := range regions {
		if region.Kind != HitRegionPaneSizeLock {
			continue
		}
		found = true
		if region.Action.Kind != input.ActionToggleTerminalSizeLock {
			t.Fatalf("expected size lock action, got %#v", region)
		}
		if region.PaneID != pane.ID || region.Action.PaneID != pane.ID {
			t.Fatalf("expected pane scoping on size lock region, got %#v", region)
		}
		break
	}
	if !found {
		t.Fatalf("expected size lock hit region, got %#v", regions)
	}
}

func TestPaneChromeHitRegionsDoNotRepeatLayoutActionsOnNonClusterPane(t *testing.T) {
	pane := workbench.VisiblePane{
		ID:         "pane-1",
		TerminalID: "term-1",
		Rect:       workbench.Rect{X: 20, Y: 6, W: 96, H: 20},
	}

	regions := PaneChromeHitRegions(pane, nil, "")
	for _, region := range regions {
		if region.Kind == HitRegionPaneBalancePanes || region.Kind == HitRegionPaneCycleLayout {
			t.Fatalf("expected non-cluster pane to hide layout actions, got %#v", regions)
		}
	}
}

func TestPaneChromeHitRegionsClipByStableActionPrefix(t *testing.T) {
	pane := workbench.VisiblePane{
		ID:         "pane-1",
		TerminalID: "term-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 34, H: 8},
	}
	regions := PaneChromeHitRegions(pane, nil, "")
	if len(regions) == 0 {
		t.Fatalf("expected at least one clipped action region, got %#v", regions)
	}

	fullOrder := []HitRegionKind{
		HitRegionPaneZoom,
		HitRegionPaneSplitV,
		HitRegionPaneSplitH,
		HitRegionPaneClose,
	}
	if len(regions) > len(fullOrder) {
		t.Fatalf("unexpected action count %d for clipped region set", len(regions))
	}
	for i := range regions {
		if regions[i].Kind != fullOrder[i] {
			t.Fatalf("expected clipped regions to keep stable prefix order, got %#v", regions)
		}
	}
}

func TestPaneChromeHitRegionsOmitFramelessPane(t *testing.T) {
	pane := workbench.VisiblePane{
		ID:         "pane-1",
		TerminalID: "term-1",
		Rect:       workbench.Rect{X: 0, Y: 0, W: 80, H: 24},
		Frameless:  true,
	}

	regions := PaneChromeHitRegions(pane, nil, "")
	if len(regions) != 0 {
		t.Fatalf("expected frameless pane to omit chrome hit regions, got %#v", regions)
	}
}

func TestPaneTopBorderLabelsLayoutKeepsRoleSlotInNarrowPane(t *testing.T) {
	rect := workbench.Rect{X: 0, Y: 0, W: 34, H: 8}
	title := " shell "
	border := paneBorderInfo{StateLabel: "●", ShareLabel: "⇄2", RoleLabel: "◇ follow"}
	tokens := paneChromeActionTokensForFrame(rect, title, border, false)
	layout, ok := paneTopBorderLabelsLayout(rect, title, border, tokens)
	if !ok {
		t.Fatal("expected valid narrow pane border layout")
	}
	if layout.roleLabel == "" {
		t.Fatalf("expected role label to be preserved in narrow pane, got %#v", layout)
	}
	if len(layout.actionSlots) >= len(tokens) {
		t.Fatalf("expected narrow pane to clip lower-priority actions, got %#v", layout.actionSlots)
	}
}
