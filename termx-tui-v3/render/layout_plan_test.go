package render

import "testing"

func TestMeasureLayoutPlansBodyPanelOverlayAndToastRects(t *testing.T) {
	shell := ShellVM{
		Header: HeaderVM{Visible: true, Title: "main"},
		Footer: FooterVM{Visible: true, Mode: "live"},
		Layout: LayoutVM{
			Panels: []PanelVM{{
				ID:           "pane-1",
				Title:        "shell",
				Presentation: PanelPresentationCard,
				Active:       true,
			}},
		},
		Overlay: OverlayVM{Kind: OverlayTerminalPicker, Content: ContentVM{Kind: ContentTerminalPicker}},
		Toasts:  []ToastVM{{ID: "toast-1", Title: "notice"}},
	}

	plan := MeasureLayout(shell, Rect{W: 50, H: 14})
	if plan.Viewport != (Rect{W: 50, H: 14}) {
		t.Fatalf("unexpected viewport plan %#v", plan.Viewport)
	}
	if plan.Header != (Rect{X: 0, Y: 0, W: 50, H: 1}) {
		t.Fatalf("unexpected header rect %#v", plan.Header)
	}
	if plan.Footer != (Rect{X: 0, Y: 13, W: 50, H: 1}) {
		t.Fatalf("unexpected footer rect %#v", plan.Footer)
	}
	if plan.Body != (Rect{X: 0, Y: 1, W: 50, H: 12}) {
		t.Fatalf("unexpected body rect %#v", plan.Body)
	}
	if len(plan.Panels) != 1 || plan.Panels[0].Rect != plan.Body {
		t.Fatalf("expected panel to occupy body, got %#v", plan.Panels)
	}
	if want := (Rect{X: 1, Y: 2, W: 48, H: 10}); plan.Panels[0].ContentRect != want {
		t.Fatalf("unexpected card content rect got=%#v want=%#v", plan.Panels[0].ContentRect, want)
	}
	if plan.Overlay.W == 0 || plan.Overlay.H == 0 {
		t.Fatalf("expected overlay rect, got %#v", plan.Overlay)
	}
	if plan.OverlayContentRect.W > plan.Overlay.W || plan.OverlayContentRect.H > plan.Overlay.H {
		t.Fatalf("overlay content must stay inside overlay rect overlay=%#v content=%#v", plan.Overlay, plan.OverlayContentRect)
	}
	if len(plan.Toasts) != 1 || plan.Toasts[0].Y != 1 || plan.Toasts[0].W == 0 {
		t.Fatalf("unexpected toast rects %#v", plan.Toasts)
	}
}

func TestMeasureLayoutUsesKnownNarrowViewportExactly(t *testing.T) {
	shell := ShellVM{
		Header: HeaderVM{Visible: true, Title: "main"},
		Footer: FooterVM{Visible: true, Mode: "live"},
		Layout: LayoutVM{Panels: []PanelVM{{
			ID:           "pane-1",
			Presentation: PanelPresentationCard,
			Active:       true,
		}}},
	}

	plan := MeasureLayout(shell, Rect{W: 12, H: 6})
	if plan.Viewport != (Rect{W: 12, H: 6}) {
		t.Fatalf("known viewport must not be expanded, got %#v", plan.Viewport)
	}
	if plan.Body != (Rect{X: 0, Y: 1, W: 12, H: 4}) {
		t.Fatalf("unexpected narrow body rect %#v", plan.Body)
	}
	if got, want := plan.Panels[0].ContentRect, (Rect{X: 1, Y: 2, W: 10, H: 2}); got != want {
		t.Fatalf("unexpected narrow content rect got=%#v want=%#v", got, want)
	}
}

func TestMeasureLayoutSplitsPanelsFromPurePlan(t *testing.T) {
	panels := []PanelVM{
		{ID: "left", Presentation: PanelPresentationSplitLine},
		{ID: "right", Presentation: PanelPresentationSplitLine, Active: true},
	}
	shell := ShellVM{
		Layout: LayoutVM{
			Panels: panels,
			Split:  SplitVM{Direction: SplitVertical, Children: []SplitVM{{PaneID: "left"}, {PaneID: "right"}}},
		},
	}

	plan := MeasureLayout(shell, Rect{W: 41, H: 10})
	if len(plan.Panels) != 2 {
		t.Fatalf("expected two panel plans, got %#v", plan.Panels)
	}
	if got, want := plan.Panels[0].Rect, (Rect{X: 0, Y: 0, W: 20, H: 10}); got != want {
		t.Fatalf("unexpected left split rect got=%#v want=%#v", got, want)
	}
	if got, want := plan.Panels[1].Rect, (Rect{X: 20, Y: 0, W: 21, H: 10}); got != want {
		t.Fatalf("unexpected right split rect got=%#v want=%#v", got, want)
	}
	if got, want := plan.Panels[1].ContentRect, (Rect{X: 21, Y: 1, W: 20, H: 9}); got != want {
		t.Fatalf("unexpected split content rect got=%#v want=%#v", got, want)
	}
}

func TestMeasureLayoutUsesSplitSizeHints(t *testing.T) {
	panels := []PanelVM{
		{ID: "left", Presentation: PanelPresentationSplitLine},
		{ID: "right", Presentation: PanelPresentationSplitLine, Active: true},
	}
	shell := ShellVM{Layout: LayoutVM{
		Panels: panels,
		Split:  SplitVM{Direction: SplitVertical, Ratio: 0.25, Children: []SplitVM{{PaneID: "left"}, {PaneID: "right"}}},
	}}
	plan := MeasureLayout(shell, Rect{W: 40, H: 10})
	if got, want := plan.Panels[0].Rect.W, 10; got != want {
		t.Fatalf("ratio split should allocate left width got=%d want=%d", got, want)
	}

	shell.Layout.Split = SplitVM{Direction: SplitVertical, BiasCells: 5, Children: []SplitVM{{PaneID: "left"}, {PaneID: "right"}}}
	plan = MeasureLayout(shell, Rect{W: 40, H: 10})
	if got, want := plan.Panels[0].Rect.W, 25; got != want {
		t.Fatalf("bias split should adjust left width got=%d want=%d", got, want)
	}

	shell.Layout.Split = SplitVM{Direction: SplitVertical, FixedPaneID: "right", FixedCols: 12, Children: []SplitVM{{PaneID: "left"}, {PaneID: "right"}}}
	plan = MeasureLayout(shell, Rect{W: 40, H: 10})
	if got, want := plan.Panels[1].Rect.W, 12; got != want {
		t.Fatalf("fixed split should allocate right width got=%d want=%d", got, want)
	}
}

func TestMeasureLayoutHeaderFooterHideReclaimsBodyInPurePlan(t *testing.T) {
	shell := ShellVM{
		Header: HeaderVM{Visible: true},
		Footer: FooterVM{Visible: true},
		Layout: LayoutVM{Panels: []PanelVM{{
			ID:           "pane-1",
			Presentation: PanelPresentationCard,
			Active:       true,
		}}},
	}

	visible := MeasureLayout(shell, Rect{W: 30, H: 10})
	shell.Header.Visible = false
	shell.Footer.Visible = false
	hidden := MeasureLayout(shell, Rect{W: 30, H: 10})

	if hidden.Body.H <= visible.Body.H || hidden.Panels[0].ContentRect.H <= visible.Panels[0].ContentRect.H {
		t.Fatalf("expected hidden chrome to reclaim body visible=%#v hidden=%#v", visible, hidden)
	}
}

func TestMeasureLayoutProducesGlobalHitRegionsAndCursorRect(t *testing.T) {
	shell := ShellVM{
		Layout: LayoutVM{Panels: []PanelVM{{
			ID:           "pane-1",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content: ContentVM{
				Kind:       ContentCopyHistory,
				Cursor:     Cursor{Visible: true, Row: 1, Col: 2, Shape: CursorShapeBlock},
				HitRegions: []HitRegion{{Kind: HitRegionHistoryRow, Rect: Rect{X: 1, Y: 0, W: 5, H: 1}, LineID: 42}},
			},
		}}},
	}

	plan := MeasureLayout(shell, Rect{W: 30, H: 10})
	var contentRegion HitRegion
	for _, region := range plan.HitRegions {
		if region.Kind == HitRegionHistoryRow {
			contentRegion = region
			break
		}
	}
	if contentRegion.Kind != HitRegionHistoryRow {
		t.Fatalf("expected content hit region, got %#v", plan.HitRegions)
	}
	if got, want := contentRegion.Rect, (Rect{X: 2, Y: 1, W: 5, H: 1}); got != want {
		t.Fatalf("unexpected global hit region got=%#v want=%#v", got, want)
	}
	if !plan.Cursor.Visible || plan.Cursor.Row != 1 || plan.Cursor.Col != 2 {
		t.Fatalf("expected content-local cursor passthrough, got %#v", plan.Cursor)
	}
	if got, want := plan.CursorRect, (Rect{X: 3, Y: 2, W: 1, H: 1}); got != want {
		t.Fatalf("unexpected global cursor rect got=%#v want=%#v", got, want)
	}
}

func TestMeasureLayoutAddsPaneCommandHitRegionsBeforeContent(t *testing.T) {
	shell := ShellVM{
		Layout: LayoutVM{Panels: []PanelVM{{
			ID:           "pane-1",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content: ContentVM{
				HitRegions: []HitRegion{{Kind: HitRegionHistoryRow, Rect: Rect{W: 10, H: 1}, LineID: 7}},
			},
		}}},
	}

	plan := MeasureLayout(shell, Rect{W: 40, H: 10})
	if len(plan.HitRegions) < 5 {
		t.Fatalf("expected pane command and content hit regions, got %#v", plan.HitRegions)
	}
	if plan.HitRegions[0].Kind != HitRegionPaneAction || plan.HitRegions[0].PaneID != "pane-1" || plan.HitRegions[0].ActionID != "pane.close" {
		t.Fatalf("pane action region should be first, got %#v", plan.HitRegions)
	}
	if plan.HitRegions[1].Kind != HitRegionPaneResize || plan.HitRegions[1].ActionID != "pane.resize" {
		t.Fatalf("pane resize region should precede content, got %#v", plan.HitRegions)
	}
	if plan.HitRegions[2].Kind != HitRegionPaneChrome || plan.HitRegions[2].ActionID != "pane.focus" {
		t.Fatalf("pane chrome region should precede content, got %#v", plan.HitRegions)
	}
	historyIndex := hitRegionIndex(plan.HitRegions, HitRegionHistoryRow)
	paneContentIndex := hitRegionIndex(plan.HitRegions, HitRegionPaneContent)
	if historyIndex <= 2 {
		t.Fatalf("content hit region should remain after chrome regions, got %#v", plan.HitRegions)
	}
	if paneContentIndex <= historyIndex {
		t.Fatalf("broad pane content focus region must not cover specific content hits, got %#v", plan.HitRegions)
	}
}

func hitRegionIndex(regions []HitRegion, kind HitRegionKind) int {
	for i, region := range regions {
		if region.Kind == kind {
			return i
		}
	}
	return -1
}

func TestMeasureLayoutOpaqueOverlayOwnsHitRegionsAndCursorRect(t *testing.T) {
	shell := ShellVM{
		Layout: LayoutVM{Panels: []PanelVM{{
			ID:           "pane-1",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content: ContentVM{
				Kind:       ContentCopyHistory,
				HitRegions: []HitRegion{{Kind: HitRegionHistoryRow, Rect: Rect{W: 10, H: 1}, LineID: 7}},
			},
		}}},
		Overlay: OverlayVM{
			Kind:   OverlayPrompt,
			Opaque: true,
			Content: ContentVM{
				Kind:       ContentPrompt,
				Cursor:     Cursor{Visible: true, Row: 1, Col: 2, Shape: CursorShapeBar},
				HitRegions: []HitRegion{{Kind: HitRegionStatus, Rect: Rect{W: 4, H: 1}}},
			},
		},
		Toasts: []ToastVM{{ID: "toast-1", Title: "notice"}},
	}

	plan := MeasureLayout(shell, Rect{W: 40, H: 12})
	if !plan.Cursor.Visible || plan.Cursor.Shape != CursorShapeBar {
		t.Fatalf("expected overlay cursor ownership, got %#v", plan.Cursor)
	}
	if plan.CursorRect.X != plan.OverlayContentRect.X+2 || plan.CursorRect.Y != plan.OverlayContentRect.Y+1 {
		t.Fatalf("unexpected overlay cursor rect content=%#v cursor=%#v", plan.OverlayContentRect, plan.CursorRect)
	}
	for _, region := range plan.HitRegions {
		if region.LineID == 7 {
			t.Fatalf("opaque overlay must hide body hit regions, got %#v", plan.HitRegions)
		}
	}
	if len(plan.HitRegions) < 4 || plan.HitRegions[0].Kind != HitRegionToastClose || plan.HitRegions[1].Kind != HitRegionToast || plan.HitRegions[2].Kind != HitRegionStatus || plan.HitRegions[3].Kind != HitRegionOverlay {
		t.Fatalf("expected toast, overlay content, overlay hit priority, got %#v", plan.HitRegions)
	}
}
