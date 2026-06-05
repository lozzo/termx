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
	if !linesContain(result.Lines(), "main") || !linesContain(result.Lines(), "shell 🚀") || !linesContain(result.Lines(), paneChromeRunningGlyph()) || !linesContain(result.Lines(), "⇄2") || !linesContain(result.Lines(), paneChromeCloseActionText()) || !linesContain(result.Lines(), "你好 output") {
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
	assertColumnGlyphs(t, vertical.Lines(), 20, 0, 12, "│┬┴┼")
	assertColumnGlyphs(t, vertical.Lines(), 39, 0, 12, "│┐┘┤")
	if !linesContain(horizontal.Lines(), "logs") || !linesContain(horizontal.Lines(), paneChromeRunningGlyph()) || !linesContain(horizontal.Lines(), "⇄2") || !linesContain(horizontal.Lines(), "├") {
		t.Fatalf("expected horizontal split chrome/separator, got %#v", horizontal.Lines())
	}
	assertAllRowsWidth(t, horizontal.Lines(), 40)
	assertAllRowsWidth(t, vertical.Lines(), 40)
}

func TestFrameworkRendersSplitLineTopBoundaryWithChromeOverlay(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 42, H: 8}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell 🚀",
			Presentation: PanelPresentationSplitLine,
			Active:       true,
			Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("你好 output")}},
		}}},
	}})
	lines := result.Lines()

	if got := SliceCells(lines[0], 0, 1); got != "┌" {
		t.Fatalf("split-line top boundary should keep top-left corner, got %q frame=%#v", got, lines)
	}
	if got := SliceCells(lines[0], 41, 42); got != "┐" {
		t.Fatalf("split-line top boundary should keep top-right corner, got %q frame=%#v", got, lines)
	}
	if !strings.Contains(lines[0], " shell 🚀 ") || !strings.Contains(lines[0], " "+paneChromeRunningGlyph()) || !strings.Contains(lines[0], "⇄2") || !strings.Contains(lines[0], "─"+paneChromeCloseActionText()) {
		t.Fatalf("split-line title/state/action slots should keep the remaining top border, got %#v", lines[0])
	}
	if !linesContain(lines, "你好 output") {
		t.Fatalf("expected split-line content, got %#v", lines)
	}
	assertAllRowsWidth(t, lines, 42)
}

func TestFrameworkRendersSplitLineAsSharedOuterFrame(t *testing.T) {
	panels := []PanelVM{
		{ID: "left", Title: "shell", Presentation: PanelPresentationSplitLine, Content: ContentVM{Kind: ContentPlaceholder, Lines: []Line{NewLine("left body")}}},
		{ID: "right", Title: "logs", Presentation: PanelPresentationSplitLine, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("right body")}}},
	}
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 48, H: 10}, Panels: panels, Split: SplitVM{Direction: SplitVertical, Children: []SplitVM{{PaneID: "left"}, {PaneID: "right"}}}},
	}})
	lines := result.Lines()

	if SliceCells(lines[0], 0, 1) != "┌" || SliceCells(lines[0], 24, 25) != "┬" || SliceCells(lines[0], 47, 48) != "┐" {
		t.Fatalf("split-line top should compose shared outer frame and divider, got %#v", lines[0])
	}
	if SliceCells(lines[9], 0, 1) != "└" || SliceCells(lines[9], 24, 25) != "┴" || SliceCells(lines[9], 47, 48) != "┘" {
		t.Fatalf("split-line bottom should compose shared outer frame and divider, got %#v", lines[9])
	}
	assertColumnGlyphs(t, lines, 0, 1, 9, "│")
	assertColumnGlyphs(t, lines, 24, 1, 9, "│")
	assertColumnGlyphs(t, lines, 47, 1, 9, "│")
	if !strings.Contains(lines[1], "│left body") || !strings.Contains(lines[1], "│right body") {
		t.Fatalf("split-line content must stay inside frame and divider, got %#v", lines)
	}
	assertAllRowsWidth(t, lines, 48)
}

func TestFrameworkPreservesPaneChromeLineBetweenTitleAndAction(t *testing.T) {
	for _, presentation := range []PanelPresentation{PanelPresentationCard, PanelPresentationSplitLine} {
		result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
			Layout: LayoutVM{Viewport: Rect{W: 44, H: 8}, Panels: []PanelVM{{
				ID:           "pane-1",
				Title:        "shell",
				Presentation: presentation,
				Active:       true,
				Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}},
			}}},
		}})
		line := result.Lines()[0]
		actionCol := cellIndex(line, paneChromeCloseActionText())
		if actionCol < 0 {
			t.Fatalf("presentation=%s missing action slot in %#v", presentation, result.Lines())
		}
		if !strings.Contains(line, paneChromeCloseActionText()) || !strings.Contains(line, "⇄2") {
			t.Fatalf("presentation=%s should render target pane action/meta slots, got row=%q", presentation, line)
		}
		beforeAction := SliceCells(line, actionCol-2, actionCol)
		if !strings.Contains(beforeAction, "─") {
			t.Fatalf("presentation=%s should keep line segment before action slot, got row=%q beforeAction=%q", presentation, line, beforeAction)
		}
		if strings.Contains(SliceCells(line, actionCol-4, actionCol), "    ") {
			t.Fatalf("presentation=%s should not leave blank gap before action slot, got row=%q", presentation, line)
		}
		assertAllRowsWidth(t, result.Lines(), 44)
	}
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
	if !linesContain(lines, "┌") || !linesContain(lines, "┐") || !linesContain(lines, "└") || !linesContain(lines, "┘") || !linesContain(lines, "│") {
		t.Fatalf("expected square Unicode card/overlay/toast chrome, got %#v", lines)
	}
	for _, line := range lines {
		if strings.ContainsAny(line, "+|-") {
			t.Fatalf("default UI chrome must not contain ASCII + - or |, got %q", line)
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
	if !linesContain(frame.ANSILines, "\x1b[1;38;2;169;112;255m") || !linesContain(frame.ANSILines, "\x1b[38;2;119;113;127m") {
		t.Fatalf("pane chrome should output active accent and inactive muted SGR, got %#v", frame.ANSILines)
	}
	assertAllRowsWidth(t, frame.Lines, 54)
	if right := SliceCells(frame.Lines[1], 53, 54); right != "┐" && right != "│" {
		t.Fatalf("wide title should not break right pane border, got %#v", frame.Lines)
	}
}

func TestFrameworkRendersStyledTopAndBottomBars(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Header: HeaderVM{Visible: true, Workspace: "main", Tab: "1", ActivePane: "pane-1", TerminalSummary: "term:1", FloatingSummary: "float:0", Notice: "ok"},
		Footer: FooterVM{Visible: true, Mode: "live", Hint: "term-1", Actions: []string{"^P pane", "^R resize"}, ActiveTarget: "pane:shell term:term-1", GlobalSummary: "ws:main tabs:1 panes:1 float:0"},
		Layout: LayoutVM{Viewport: Rect{W: 120, H: 10}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}},
		}}},
	}})
	frame := result.Frame()

	if !strings.Contains(frame.Lines[0], " main ") || !strings.Contains(frame.Lines[0], "▎") || !strings.Contains(frame.Lines[0], "[＋]") || !strings.Contains(frame.Lines[0], "×") || !strings.Contains(frame.Lines[0], "! ok") {
		t.Fatalf("top bar should contain tuiv2-like workspace/tab/create/notice slots, got %#v", frame.Lines[0])
	}
	footer := frame.Lines[len(frame.Lines)-1]
	if !strings.Contains(footer, "LIVE") || !strings.Contains(footer, "[Ctrl] · [P] PANE") || !strings.Contains(footer, "[Ctrl] · [R] RESIZE") || !strings.Contains(footer, "● shell term:term-1") || !strings.Contains(footer, "ws main") || !strings.Contains(footer, "» term-1") {
		t.Fatalf("bottom bar should contain high-density mode/action/summary tokens, got %#v", footer)
	}
	if !styledLinesContainText(frame.StyledLines[:1], " main ", StyleStatusAccent) ||
		!styledLinesContainText(frame.StyledLines[:1], "[ ]", StyleStatusMuted) ||
		!styledLinesContainText(frame.StyledLines[:1], "! ok", StyleStatusWarning) ||
		!styledLinesContainText(frame.StyledLines[len(frame.StyledLines)-1:], "LIVE", StyleStatusAccent) ||
		!styledLinesContainText(frame.StyledLines[len(frame.StyledLines)-1:], "[Ctrl] · [P]", StyleStatusAccent) {
		t.Fatalf("top/bottom bar cells should use status token styles, got %#v", frame.StyledLines)
	}
	if !strings.Contains(frame.ANSILines[0], "\x1b[1;38;2;169;112;255m\x1b[48;2;8;8;13m") || !strings.Contains(frame.ANSILines[len(frame.ANSILines)-1], "\x1b[1;38;2;169;112;255m\x1b[48;2;8;8;13m") {
		t.Fatalf("top/bottom bars should output status background SGR, got %#v", frame.ANSILines)
	}
	assertAllRowsWidth(t, frame.Lines, 120)
}

func TestFrameworkRendersFloatingLayerAboveTiledPane(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{
			Viewport: Rect{W: 64, H: 18},
			Panels: []PanelVM{{
				ID:           "pane-1",
				Title:        "shell",
				Presentation: PanelPresentationCard,
				Active:       true,
				Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("tiled background")}},
			}},
			Floating: []FloatingVM{{
				ID:      "float-1",
				Title:   "浮窗🚀",
				Rect:    Rect{X: 8, Y: 4, W: 36, H: 8},
				Z:       1,
				Active:  true,
				Content: ContentVM{Kind: ContentEmptyPane, Lines: []Line{NewLine("floating body 世界")}},
			}},
		},
	}})
	frame := result.Frame()

	if !linesContain(frame.Lines, "浮窗🚀") || !linesContain(frame.Lines, paneChromeRunningGlyph()+" float") || !linesContain(frame.Lines, "floating body 世界") {
		t.Fatalf("expected floating title/content, got %#v", frame.Lines)
	}
	if !styledLinesContain(frame.StyledLines, "┌", StyleAccent) || !styledLinesContain(frame.StyledLines, "┘", StyleAccent) {
		t.Fatalf("active floating border should use accent style, got %#v", frame.StyledLines)
	}
	layer := firstLayer(t, result, LayerFloating)
	if layer.Rect != (Rect{X: 8, Y: 4, W: 36, H: 8}) {
		t.Fatalf("unexpected floating layer rect %#v", layer)
	}
	assertAllRowsWidth(t, frame.Lines, 64)
}

func TestFrameworkRendersModeSpecificFooterHints(t *testing.T) {
	cases := []struct {
		name string
		mode string
		want string
	}{
		{name: "pane", mode: "pane", want: "[v] SPLIT"},
		{name: "resize", mode: "resize", want: "[←/h]"},
		{name: "global", mode: "global", want: "[h] HEADER"},
		{name: "tab", mode: "tab", want: "[n] NEW"},
		{name: "workspace", mode: "workspace", want: "[t] TREE"},
		{name: "copy", mode: "copy", want: "[pgup] OLDER"},
		{name: "overlay", mode: "terminal-picker", want: "[attach]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame := NewRenderer(DefaultTheme()).Render(RenderVM{Shell: ShellVM{
				Header: HeaderVM{Visible: true, Workspace: "main"},
				Footer: FooterVM{Visible: true, Mode: tc.mode, Actions: footerActions(tc.mode), ActiveTarget: "pane:shell", GlobalSummary: "ws:main tabs:1 panes:1 float:0"},
				Layout: LayoutVM{Viewport: Rect{W: 96, H: 9}, Panels: []PanelVM{{
					ID:           "pane-1",
					Title:        "shell",
					Presentation: PanelPresentationCard,
					Active:       true,
					Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}},
				}}},
			}})
			footer := frame.Lines[len(frame.Lines)-1]
			if !strings.Contains(footer, strings.ToUpper(tc.mode)) || !strings.Contains(footer, tc.want) || !strings.Contains(footer, "● shell") {
				t.Fatalf("footer missing mode-specific product hints for %s: %#v", tc.mode, footer)
			}
			assertAllRowsWidth(t, frame.Lines, 96)
		})
	}
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

	lines := result.Lines()
	if SliceCells(lines[0], 20, 21) != "┬" || SliceCells(lines[6], 0, 1) != "├" || SliceCells(lines[6], 20, 21) != "┤" || SliceCells(lines[11], 20, 21) != "┴" {
		t.Fatalf("expected composed split divider joints, got %#v", lines)
	}
	if !linesContain(lines, paneChromeSplitHorizontalActionText()) || !linesContain(lines, paneChromeSplitVerticalActionText()) || !linesContain(lines, paneChromeCloseActionText()) {
		t.Fatalf("expected real split and close action tokens in split chrome, got %#v", lines)
	}
	assertAllRowsWidth(t, lines, 40)
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
				{Text: " dir", Width: 4, ANSIStyle: ANSICellStyle{FG: "ansi:4", Bold: true}, Safe: true},
			}}}},
		}}},
	}})
	frame := result.Frame()

	if !linesContain(frame.Lines, "accent plain dir") {
		t.Fatalf("plain snapshot should keep text without ANSI, got %#v", frame.Lines)
	}
	if !linesContain(frame.ANSILines, "\x1b[1;38;2;169;112;255m") || !linesContain(frame.ANSILines, ANSIReset) {
		t.Fatalf("ANSI frame should retain styled matrix cells and reset, got %#v", frame.ANSILines)
	}
	if !linesContain(frame.ANSILines, "\x1b[1;34md") || !linesContain(frame.ANSILines, "\x1b[1;34mr") || linesContain(frame.ANSILines, "38;2;122;184;255m") {
		t.Fatalf("terminal ANSI cell style should survive compositor as host palette code, got %#v", frame.ANSILines)
	}
	if !styledLinesContain(frame.StyledLines, "a", StyleAccent) {
		t.Fatalf("styled frame should retain StyleAccent cells, got %#v", frame.StyledLines)
	}
	if !styledLinesContainANSI(frame.StyledLines, " ", ANSICellStyle{FG: "ansi:4", Bold: true}) {
		t.Fatalf("styled frame should retain terminal ANSI cell style, got %#v", frame.StyledLines)
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
	if linesContain(result.Lines(), "warn 🚀") {
		t.Fatalf("overlay should stay in foreground above toast, got %#v", result.Lines())
	}
	if firstLayer(t, result, LayerOverlay).Rect.W == 0 || firstLayer(t, result, LayerToast).Rect.W == 0 {
		t.Fatalf("expected overlay and toast layers, got %#v", result.Layers)
	}
	if !linesContain(result.ANSILines(), "\x1b[48;2;20;18;27m") {
		t.Fatalf("expected styled overlay ANSI, got %#v", result.ANSILines())
	}
	assertAllRowsWidth(t, result.Lines(), 50)
}

func TestFrameworkToastDoesNotOverwritePaneTopChrome(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 64, H: 16}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}},
		}}},
		Toasts: []ToastVM{{ID: "toast-1", Severity: ToastInfo, Title: "pane.split", Body: "created"}},
	}})
	lines := result.Lines()

	if !strings.Contains(lines[0], "┌─ shell") || !strings.Contains(lines[0], " "+paneChromeRunningGlyph()) || !strings.Contains(lines[0], "⇄2") || !strings.Contains(lines[0], paneChromeCloseActionText()) {
		t.Fatalf("toast must not overwrite pane top chrome, got %#v", lines)
	}
	if strings.Contains(lines[0], "pane.split") {
		t.Fatalf("toast should start below pane top chrome, got %#v", lines)
	}
	if !linesContain(lines, "pane.split") || linesContain(lines, "created") || linesContain(lines[3:], paneChromeCloseActionText()) {
		t.Fatalf("expected centered no-action toast below chrome, got %#v", lines)
	}
	assertAllRowsWidth(t, lines, 64)
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

func styledLinesContainText(lines []Line, value string, style StyleToken) bool {
	for _, line := range lines {
		var styledText strings.Builder
		for _, cell := range line.Cells {
			if cell.Style == style {
				styledText.WriteString(cell.Text)
			}
		}
		if strings.Contains(styledText.String(), value) {
			return true
		}
	}
	return false
}

func styledLinesContainANSI(lines []Line, value string, style ANSICellStyle) bool {
	for _, line := range lines {
		for _, cell := range line.Cells {
			if cell.Text == value && cell.ANSIStyle == style && cell.Style == "" {
				return true
			}
		}
	}
	return false
}

func cellIndex(line string, needle string) int {
	for col := 0; col <= DisplayWidth(line)-DisplayWidth(needle); col++ {
		if SliceCells(line, col, col+DisplayWidth(needle)) == needle {
			return col
		}
	}
	return -1
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
