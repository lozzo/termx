package render

import (
	"strings"
	"testing"
)

func TestFrameworkRendersCardPanelShellAndContent(t *testing.T) {
	vm := RenderVM{Shell: ShellVM{
		Header: HeaderVM{Visible: true, Title: "main"},
		Footer: FooterVM{Visible: true, Mode: "live", Hint: "term-1"},
		Layout: LayoutVM{
			Viewport: Rect{W: 40, H: 12},
			Panels: []PanelVM{{
				ID:           "pane-1",
				Title:        "shell 🚀",
				Presentation: PanelPresentationCard,
				Active:       true,
				Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("你好 output")}},
			}},
		},
	}}

	result := NewRenderer(DefaultTheme()).RenderResult(vm)
	assertFrameSize(t, result, 40, 12)
	if !linesContain(result.Lines(), "main") || !linesContain(result.Lines(), "shell 🚀 active") || !linesContain(result.Lines(), "你好 output") {
		t.Fatalf("expected shell, panel title and content, got %#v", result.Lines())
	}
	assertAllRowsWidth(t, result.Lines(), 40)
}

func TestFrameworkUsesKnownViewportExactly(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Header: HeaderVM{Visible: true, Title: "narrow"},
		Footer: FooterVM{Visible: true, Mode: "live"},
		Layout: LayoutVM{
			Viewport: Rect{W: 12, H: 6},
			Panels: []PanelVM{{
				ID:           "pane-1",
				Title:        "shell",
				Presentation: PanelPresentationCard,
				Active:       true,
				Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}},
			}},
		},
	}})

	assertFrameSize(t, result, 12, 6)
	assertAllRowsWidth(t, result.Lines(), 12)
}

func TestFrameworkRendersSplitLineHorizontalAndVertical(t *testing.T) {
	panels := []PanelVM{
		{ID: "pane-1", Title: "shell", Presentation: PanelPresentationSplitLine, Active: false, Content: ContentVM{Kind: ContentPlaceholder, Lines: []Line{NewLine("left")}}},
		{ID: "pane-2", Title: "logs", Presentation: PanelPresentationSplitLine, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("right")}}},
	}
	horizontal := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 40, H: 12}, Panels: panels, Split: SplitVM{Direction: SplitHorizontal, Children: []SplitVM{{PaneID: "pane-1"}, {PaneID: "pane-2"}}}},
	}})
	vertical := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 40, H: 12}, Panels: panels, Split: SplitVM{Direction: SplitVertical, Children: []SplitVM{{PaneID: "pane-1"}, {PaneID: "pane-2"}}}},
	}})

	if !linesContain(horizontal.Lines(), "right") || !linesContain(vertical.Lines(), "right") {
		t.Fatalf("expected active panel content in both split modes")
	}
	if !frameHasRowPrefix(vertical.Lines(), 20, "│") {
		t.Fatalf("expected Unicode vertical split line near midpoint, got %#v", vertical.Lines())
	}
	if !linesContain(horizontal.Lines(), "─ logs active") {
		t.Fatalf("expected horizontal split chrome/separator, got %#v", horizontal.Lines())
	}
	assertAllRowsWidth(t, horizontal.Lines(), 40)
	assertAllRowsWidth(t, vertical.Lines(), 40)
}

func TestFrameworkUsesUnicodeChromeAndNoDefaultASCIIBorders(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Header: HeaderVM{Visible: true, Title: "main"},
		Footer: FooterVM{Visible: true, Mode: "live"},
		Layout: LayoutVM{Viewport: Rect{W: 42, H: 12}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell 🚀",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("你好 e\u0301 output")}},
		}}},
		Overlay: OverlayVM{Kind: OverlayTerminalPicker, Content: ContentVM{Kind: ContentTerminalPicker, Lines: []Line{NewLine("picker 世界")}}},
		Toasts:  []ToastVM{{ID: "toast-1", Severity: ToastInfo, Title: "notice 🚀", Body: "世界"}},
	}})

	lines := result.Lines()
	if !linesContain(lines, "╭") || !linesContain(lines, "╮") || !linesContain(lines, "╰") || !linesContain(lines, "╯") {
		t.Fatalf("expected rounded Unicode card/overlay/toast chrome, got %#v", lines)
	}
	for _, line := range lines {
		if strings.ContainsAny(line, "+|") {
			t.Fatalf("default UI chrome must not contain ASCII + or |, got %q", line)
		}
	}
	assertAllRowsWidth(t, lines, 42)
}

func TestFrameworkComposesUnicodeSplitConnections(t *testing.T) {
	panels := []PanelVM{
		{ID: "left-top", Title: "lt", Presentation: PanelPresentationSplitLine, Content: ContentVM{Kind: ContentPlaceholder, Lines: []Line{NewLine("lt")}}},
		{ID: "left-bottom", Title: "lb", Presentation: PanelPresentationSplitLine, Content: ContentVM{Kind: ContentPlaceholder, Lines: []Line{NewLine("lb")}}},
		{ID: "right", Title: "right", Presentation: PanelPresentationSplitLine, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("right")}}},
	}
	split := SplitVM{Direction: SplitVertical, Children: []SplitVM{
		{Direction: SplitHorizontal, Children: []SplitVM{{PaneID: "left-top"}, {PaneID: "left-bottom"}}},
		{PaneID: "right"},
	}}

	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 40, H: 12}, Panels: panels, Split: split},
	}})

	if !linesContain(result.Lines(), "┼") {
		t.Fatalf("expected composed split intersection, got %#v", result.Lines())
	}
	assertAllRowsWidth(t, result.Lines(), 40)
}

func TestFrameworkHeaderFooterHideReclaimsBody(t *testing.T) {
	base := ShellVM{
		Header: HeaderVM{Visible: true, Title: "main"},
		Footer: FooterVM{Visible: true, Mode: "live"},
		Layout: LayoutVM{Viewport: Rect{W: 30, H: 10}, Panels: []PanelVM{{ID: "pane-1", Title: "pane", Presentation: PanelPresentationCard, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}}}}},
	}
	visible := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: base})
	base.Header.Visible = false
	base.Footer.Visible = false
	hidden := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: base})

	visiblePanel := firstLayer(t, visible, LayerPanel)
	hiddenPanel := firstLayer(t, hidden, LayerPanel)
	if hiddenPanel.Rect.H <= visiblePanel.Rect.H {
		t.Fatalf("expected hidden header/footer to reclaim body, visible=%#v hidden=%#v", visiblePanel.Rect, hiddenPanel.Rect)
	}
}

func TestFrameworkRendersToastAndTerminalPickerOverlay(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Header:  HeaderVM{Visible: true, Title: "main"},
		Footer:  FooterVM{Visible: true, Mode: "live"},
		Layout:  LayoutVM{Viewport: Rect{W: 50, H: 14}, Panels: []PanelVM{{ID: "pane-1", Title: "pane", Presentation: PanelPresentationCard, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}}}}},
		Overlay: OverlayVM{Kind: OverlayTerminalPicker, Content: ContentVM{Kind: ContentTerminalPicker, Lines: []Line{NewLine("picker pending")}, Pending: true}},
		Toasts:  []ToastVM{{ID: "toast-1", Severity: ToastWarning, Title: "warn 🚀", Body: "世界", Pending: true}},
	}})

	if !linesContain(result.Lines(), "picker pending") {
		t.Fatalf("expected terminal picker overlay, got %#v", result.Lines())
	}
	if !linesContain(result.Lines(), "[warning] warn 🚀 ... 世界") {
		t.Fatalf("expected toast, got %#v", result.Lines())
	}
	if firstLayer(t, result, LayerOverlay).Rect.W == 0 || firstLayer(t, result, LayerToast).Rect.W == 0 {
		t.Fatalf("expected overlay and toast layers, got %#v", result.Layers)
	}
	assertAllRowsWidth(t, result.Lines(), 50)
}

func TestFrameworkToastDoesNotChangeBodyLayout(t *testing.T) {
	shell := ShellVM{
		Header: HeaderVM{Visible: true, Title: "main"},
		Footer: FooterVM{Visible: true, Mode: "live"},
		Layout: LayoutVM{Viewport: Rect{W: 50, H: 14}, Panels: []PanelVM{{ID: "pane-1", Title: "pane", Presentation: PanelPresentationCard, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}}}}},
	}
	withoutToast := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: shell})
	shell.Toasts = []ToastVM{{ID: "toast-1", Severity: ToastInfo, Title: "notice"}}
	withToast := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: shell})

	if firstLayer(t, withoutToast, LayerPanel).Rect != firstLayer(t, withToast, LayerPanel).Rect {
		t.Fatalf("toast must not change panel layout without=%#v with=%#v", firstLayer(t, withoutToast, LayerPanel).Rect, firstLayer(t, withToast, LayerPanel).Rect)
	}
}

func TestFrameworkTranslatesContentHitRegionsAndCursor(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Cursor: Cursor{Visible: true, Row: 1, Col: 2, Shape: CursorShapeBlock},
		Layout: LayoutVM{Viewport: Rect{W: 30, H: 10}, Panels: []PanelVM{{ID: "pane-1", Title: "pane", Presentation: PanelPresentationCard, Active: true, Content: ContentVM{
			Kind:       ContentCopyHistory,
			Lines:      []Line{NewLine("row")},
			HitRegions: []HitRegion{{Kind: HitRegionHistoryRow, Rect: Rect{Y: 0, W: 10, H: 1}, LineID: 42}},
		}}}},
	}})

	if !result.Cursor.Visible || result.Cursor.Row != 1 || result.Cursor.Col != 2 {
		t.Fatalf("expected cursor passthrough, got %#v", result.Cursor)
	}
	if len(result.HitRegions) != 1 || result.HitRegions[0].LineID != 42 || result.HitRegions[0].Rect.Y == 0 {
		t.Fatalf("expected translated content hit region, got %#v", result.HitRegions)
	}
}

func TestFrameworkOpaqueOverlayOwnsCursor(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Cursor: Cursor{Visible: true, Row: 1, Col: 2, Shape: CursorShapeBlock},
		Layout: LayoutVM{Viewport: Rect{W: 40, H: 12}, Panels: []PanelVM{{ID: "pane-1", Title: "pane", Presentation: PanelPresentationCard, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}}}}},
		Overlay: OverlayVM{
			Kind:   OverlayPrompt,
			Opaque: true,
			Content: ContentVM{
				Kind:   ContentPrompt,
				Lines:  []Line{NewLine("prompt")},
				Cursor: Cursor{Visible: true, Row: 3, Col: 4, Shape: CursorShapeBar},
			},
		},
	}})

	if !result.Cursor.Visible || result.Cursor.Row != 3 || result.Cursor.Col != 4 || result.Cursor.Shape != CursorShapeBar {
		t.Fatalf("expected opaque overlay cursor, got %#v", result.Cursor)
	}
}

func assertFrameSize(t *testing.T, result RenderResult, width int, height int) {
	t.Helper()
	if result.Metadata.Width != width || result.Metadata.Height != height || len(result.Content) != height {
		t.Fatalf("unexpected result size metadata=%#v lines=%d", result.Metadata, len(result.Content))
	}
}

func assertAllRowsWidth(t *testing.T, lines []string, width int) {
	t.Helper()
	for i, line := range lines {
		if got := DisplayWidth(line); got != width {
			t.Fatalf("row %d width=%d want=%d line=%q", i, got, width, line)
		}
	}
}

func linesContain(lines []string, value string) bool {
	for _, line := range lines {
		if strings.Contains(line, value) {
			return true
		}
	}
	return false
}

func frameHasRowPrefix(lines []string, col int, prefix string) bool {
	for _, line := range lines {
		if strings.HasPrefix(SliceCells(line, col, col+DisplayWidth(prefix)), prefix) {
			return true
		}
	}
	return false
}

func firstLayer(t *testing.T, result RenderResult, kind LayerKind) Layer {
	t.Helper()
	for _, layer := range result.Layers {
		if layer.Kind == kind {
			return layer
		}
	}
	t.Fatalf("missing layer %s in %#v", kind, result.Layers)
	return Layer{}
}
