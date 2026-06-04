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
	if !linesContain(result.Lines(), "main") || !linesContain(result.Lines(), "shell 🚀") || !linesContain(result.Lines(), "[x]") || !linesContain(result.Lines(), "你好 output") {
		t.Fatalf("expected shell, panel title and content, got %#v", result.Lines())
	}
	if !linesContain(result.Lines(), "┌") || !linesContain(result.Lines(), "┐") || !linesContain(result.Lines(), "└") || !linesContain(result.Lines(), "┘") {
		t.Fatalf("expected square Unicode pane chrome, got %#v", result.Lines())
	}
	assertAllRowsWidth(t, result.Lines(), 40)
}

func TestFrameworkRendersContinuousCardPaneVerticalBorders(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 24, H: 8}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}},
		}}},
	}})

	lines := result.Lines()
	assertColumnGlyphs(t, lines, 0, 1, 7, "│")
	assertColumnGlyphs(t, lines, 23, 1, 7, "│")
	assertAllRowsWidth(t, lines, 24)
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
	assertColumnGlyphs(t, vertical.Lines(), 20, 0, 12, "│")
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

func TestFrameworkStylesActiveAndInactivePaneChromeDifferently(t *testing.T) {
	panels := []PanelVM{
		{ID: "pane-1", Title: "shell 🚀", Presentation: PanelPresentationCard, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("left")}}},
		{ID: "pane-2", Title: "logs 世界", Presentation: PanelPresentationCard, Active: false, Content: ContentVM{Kind: ContentPlaceholder, Lines: []Line{NewLine("right")}}},
	}
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{
			Viewport: Rect{W: 54, H: 10},
			Panels:   panels,
			Split:    SplitVM{Direction: SplitVertical, Children: []SplitVM{{PaneID: "pane-1"}, {PaneID: "pane-2"}}},
		},
	}})
	frame := result.Frame()

	if !styledLinesContain(frame.StyledLines, "┌", StyleAccent) || !styledLinesContain(frame.StyledLines, "┐", StyleAccent) {
		t.Fatalf("active card pane border should use accent style, got %#v", frame.StyledLines)
	}
	if !styledLinesContain(frame.StyledLines, "┌", StyleMuted) || !styledLinesContain(frame.StyledLines, "┐", StyleMuted) {
		t.Fatalf("inactive card pane border should use muted style, got %#v", frame.StyledLines)
	}
	if !linesContain(frame.ANSILines, "\x1b[1;38;2;88;213;201m") || !linesContain(frame.ANSILines, "\x1b[38;2;111;119;113m") {
		t.Fatalf("pane chrome should output active accent and inactive muted SGR, got %#v", frame.ANSILines)
	}
	assertAllRowsWidth(t, frame.Lines, 54)
	if right := SliceCells(frame.Lines[1], 53, 54); right != "┐" && right != "│" {
		t.Fatalf("wide title should not break right pane border, got %#v", frame.Lines)
	}
}

func TestFrameworkRendersStyledTopAndBottomBars(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Header: HeaderVM{Visible: true, Title: "main", Notice: "ok"},
		Footer: FooterVM{Visible: true, Mode: "live", Hint: "term-1"},
		Layout: LayoutVM{Viewport: Rect{W: 42, H: 10}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}},
		}}},
	}})
	frame := result.Frame()

	if !strings.Contains(frame.Lines[0], "ws:main") || !strings.Contains(frame.Lines[0], "tab:1") || !strings.Contains(frame.Lines[0], "⊕") || !strings.Contains(frame.Lines[0], "notice:ok") {
		t.Fatalf("top bar should contain workspace/tab/create/notice tokens, got %#v", frame.Lines[0])
	}
	if !strings.Contains(frame.Lines[len(frame.Lines)-1], "mode:live") || !strings.Contains(frame.Lines[len(frame.Lines)-1], "hint:term-1") || !strings.Contains(frame.Lines[len(frame.Lines)-1], "status:ready") {
		t.Fatalf("bottom bar should contain mode/hint/status tokens, got %#v", frame.Lines[len(frame.Lines)-1])
	}
	if !styledLinesContain(frame.StyledLines[:1], "w", StyleStatus) || !styledLinesContain(frame.StyledLines[len(frame.StyledLines)-1:], "m", StyleStatus) {
		t.Fatalf("top/bottom bar cells should use status style, got %#v", frame.StyledLines)
	}
	if !strings.Contains(frame.ANSILines[0], "\x1b[48;2;24;50;74m") || !strings.Contains(frame.ANSILines[len(frame.ANSILines)-1], "\x1b[48;2;24;50;74m") {
		t.Fatalf("top/bottom bars should output status background SGR, got %#v", frame.ANSILines)
	}
	assertAllRowsWidth(t, frame.Lines, 42)
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

func TestFrameworkPreservesStyledContentThroughMatrixAndANSIFrame(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 36, H: 10}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "pane",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{{Cells: []Cell{
				{Text: "accent", Width: 6, Style: StyleAccent, Safe: true},
				{Text: " plain", Width: 6, Safe: true},
			}}}},
		}}},
	}})
	frame := result.Frame()

	if !linesContain(frame.Lines, "accent plain") {
		t.Fatalf("plain snapshot should keep text without ANSI, got %#v", frame.Lines)
	}
	if !linesContain(frame.ANSILines, "\x1b[1;38;2;88;213;201m") || !linesContain(frame.ANSILines, ANSIReset) {
		t.Fatalf("ANSI frame should retain styled matrix cells and reset, got %#v", frame.ANSILines)
	}
	if !styledLinesContain(frame.StyledLines, "a", StyleAccent) {
		t.Fatalf("styled frame should retain StyleAccent cells, got %#v", frame.StyledLines)
	}
	assertAllRowsWidth(t, frame.Lines, 36)
}

func TestCanvasMatrixTracksOwnerLayerContinuationAndSafeFlag(t *testing.T) {
	c := newCanvas(6, 1)
	c.writeTextStyled(0, 0, 2, "你", StyleAccent, "pane-1", LayerPanel)

	if cell := c.rows[0][0]; cell.text != "你" || cell.width != 2 || cell.style != StyleAccent || cell.owner != "pane-1" || cell.layer != LayerPanel || !cell.safe {
		t.Fatalf("unexpected matrix anchor cell %#v", cell)
	}
	if cell := c.rows[0][1]; !cell.continuation || cell.owner != "pane-1" || cell.layer != LayerPanel {
		t.Fatalf("expected wide-cell continuation footprint, got %#v", cell)
	}
}

func TestCanvasMatrixClearsWideCellFootprintBeforeOverwrite(t *testing.T) {
	c := newCanvas(6, 1)
	c.writeText(0, 0, 6, "你你你")
	c.writeText(1, 0, 1, "x")

	line := c.lines()[0].PlainString()
	if got := DisplayWidth(line); got != 6 {
		t.Fatalf("matrix overwrite must keep row width 6, got %d line=%q cells=%#v", got, line, c.rows[0])
	}
	if !strings.HasPrefix(line, " x") {
		t.Fatalf("overwrite at continuation cell should clear old wide footprint, got %q cells=%#v", line, c.rows[0])
	}
}

func TestFrameworkStripsRawANSIInputBeforeMatrixLayout(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 30, H: 8}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "pane",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("\x1b[31mred 世界\x1b[0m")}},
		}}},
	}})

	lines := result.Lines()
	if !linesContain(lines, "red 世界") {
		t.Fatalf("plain matrix output should strip raw ANSI and keep text, got %#v", lines)
	}
	for _, line := range lines {
		if strings.Contains(line, "\x1b[") {
			t.Fatalf("plain matrix output must not contain raw ANSI, got %q", line)
		}
	}
	assertAllRowsWidth(t, lines, 30)
	if right := SliceCells(lines[1], 29, 30); right != "│" {
		t.Fatalf("right border should survive ANSI/wide content, got %#v", lines)
	}
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
	if !linesContain(result.Lines(), "[×]") {
		t.Fatalf("expected toast close action token, got %#v", result.Lines())
	}
	if firstLayer(t, result, LayerOverlay).Rect.W == 0 || firstLayer(t, result, LayerToast).Rect.W == 0 {
		t.Fatalf("expected overlay and toast layers, got %#v", result.Layers)
	}
	if !linesContain(result.ANSILines(), "\x1b[38;2;240;196;92m") || !linesContain(result.ANSILines(), "\x1b[48;2;17;22;20m") || !linesContain(result.ANSILines(), "\x1b[48;2;20;24;22m") {
		t.Fatalf("expected styled warning toast and overlay ANSI, got %#v", result.ANSILines())
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
	var contentRegion HitRegion
	for _, region := range result.HitRegions {
		if region.Kind == HitRegionHistoryRow && region.LineID == 42 {
			contentRegion = region
			break
		}
	}
	if contentRegion.Kind != HitRegionHistoryRow || contentRegion.Rect.Y == 0 {
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

func styledLinesContain(lines []Line, value string, style StyleToken) bool {
	for _, line := range lines {
		for _, cell := range line.Cells {
			if cell.Text == value && cell.Style == style {
				return true
			}
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

func assertColumnGlyphs(t *testing.T, lines []string, col int, startRow int, endRow int, allowed string) {
	t.Helper()
	for row := startRow; row < endRow; row++ {
		if row < 0 || row >= len(lines) {
			t.Fatalf("row %d out of frame bounds lines=%d", row, len(lines))
		}
		got := SliceCells(lines[row], col, col+1)
		if !strings.Contains(allowed, got) {
			t.Fatalf("expected continuous border glyph at row=%d col=%d got=%q allowed=%q frame=%#v", row, col, got, allowed, lines)
		}
	}
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
