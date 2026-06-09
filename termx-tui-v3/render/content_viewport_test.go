package render

import (
	"strings"
	"testing"
)

func TestContentViewportKeepsTerminalBlankCellsAsSpaces(t *testing.T) {
	result := RenderContentViewport(ContentRenderRequest{
		Rect: Rect{W: 20, H: 5},
		Content: ContentVM{
			Kind: ContentTerminalLive,
			Lines: []Line{
				NewLine("123456789012345678901234"),
				NewLine("ok"),
			},
			Extent: ContentExtent{Known: true, Cols: 20, Rows: 5},
		},
	})

	if got, want := plainContentViewportLines(result.Lines), []string{
		"12345678901234567890",
		"ok                  ",
		"                    ",
		"                    ",
		"                    ",
	}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ordinary terminal blanks must stay spaces\n got=%#v\nwant=%#v", got, want)
	}
	if !result.Overflow.Right || result.Overflow.Bottom {
		t.Fatalf("expected only right overflow from over-wide input line, got %#v", result.Overflow)
	}
	assertContentViewportLineWidths(t, result.Lines, 20)
}

func TestContentViewportMarksOnlyAreaOutsideTerminalExtentWithDots(t *testing.T) {
	result := RenderContentViewport(ContentRenderRequest{
		Rect: Rect{W: 10, H: 6},
		Content: ContentVM{
			Kind: ContentTerminalLive,
			Lines: []Line{
				NewLine("abcde"),
				NewLine("fghij"),
				NewLine("klmno"),
			},
			Extent: ContentExtent{Known: true, Cols: 5, Rows: 3},
		},
	})

	if got, want := plainContentViewportLines(result.Lines), []string{
		"abcde·····",
		"fghij·····",
		"klmno·····",
		"··········",
		"··········",
		"··········",
	}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("extent outside cells should be weak dots\n got=%#v\nwant=%#v", got, want)
	}
	if result.Overflow != (ContentOverflow{}) {
		t.Fatalf("small extent should not report overflow, got %#v", result.Overflow)
	}
	assertContentViewportLineWidths(t, result.Lines, 10)
}

func TestContentViewportSupportsOffsetTerminalExtent(t *testing.T) {
	result := RenderContentViewport(ContentRenderRequest{
		Rect: Rect{W: 4, H: 3},
		Content: ContentVM{
			Kind:   ContentTerminalLive,
			Lines:  []Line{NewLine("hi")},
			Extent: ContentExtent{Known: true, X: 1, Y: 1, Cols: 2, Rows: 1},
		},
	})

	if got, want := plainContentViewportLines(result.Lines), []string{
		"····",
		"·hi·",
		"····",
	}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("offset extent should move the dot region with terminal area\n got=%#v\nwant=%#v", got, want)
	}
	if result.Overflow != (ContentOverflow{}) {
		t.Fatalf("offset extent inside rect should not overflow, got %#v", result.Overflow)
	}
}

func TestContentViewportReturnsOverflowHintsWithoutWritingMarkers(t *testing.T) {
	result := RenderContentViewport(ContentRenderRequest{
		Rect: Rect{W: 10, H: 5},
		Content: ContentVM{
			Kind: ContentTerminalLive,
			Lines: []Line{
				NewLine("abcdefghijk"),
				NewLine("row 2"),
				NewLine("row 3"),
				NewLine("row 4"),
				NewLine("row 5"),
				NewLine("row 6"),
			},
			Extent: ContentExtent{Known: true, Cols: 12, Rows: 8},
		},
	})

	if !result.Overflow.Right || !result.Overflow.Bottom {
		t.Fatalf("expected right and bottom overflow hints, got %#v", result.Overflow)
	}
	if rendered := strings.Join(plainContentViewportLines(result.Lines), "\n"); strings.Contains(rendered, ">") || strings.Contains(rendered, "v") {
		t.Fatalf("overflow markers belong to chrome, not content cells: %q", rendered)
	}
	if got := len(result.Lines); got != 5 {
		t.Fatalf("content viewport must return rect height lines, got %d", got)
	}
	assertContentViewportLineWidths(t, result.Lines, 10)
}

func TestContentViewportPreservesANSICellStyleAndWideTruncation(t *testing.T) {
	result := RenderContentViewport(ContentRenderRequest{
		Rect: Rect{W: 5, H: 2},
		Content: ContentVM{
			Kind: ContentTerminalLive,
			Lines: []Line{{Cells: []Cell{
				{Text: "你", Width: 2, ANSIStyle: ANSICellStyle{FG: "ansi:2", Bold: true}, Safe: true},
				{Text: "好", Width: 2, ANSIStyle: ANSICellStyle{FG: "ansi:3"}, Safe: true},
				{Text: "w", Width: 1, ANSIStyle: ANSICellStyle{FG: "ansi:4"}, Safe: true},
				{Text: "orld", Width: 4, ANSIStyle: ANSICellStyle{FG: "ansi:5"}, Safe: true},
			}}},
			Extent: ContentExtent{Known: true, Cols: 5, Rows: 2},
		},
	})

	if got := result.Lines[0].PlainString(); got != "你好w" {
		t.Fatalf("wide cells should truncate at cell boundary, got %q", got)
	}
	if got := result.Lines[1].PlainString(); got != "     " {
		t.Fatalf("terminal-internal empty row should be spaces, got %q", got)
	}
	if !contentViewportLineHasANSI(result.Lines[0], "你", ANSICellStyle{FG: "ansi:2", Bold: true}) ||
		!contentViewportLineHasANSI(result.Lines[0], "w", ANSICellStyle{FG: "ansi:4"}) {
		t.Fatalf("ANSI cell style should survive viewport clipping, got %#v", result.Lines[0])
	}
	if strings.Contains(result.Lines[0].ANSIString(DefaultTheme()), "\x1b[38;2") {
		t.Fatalf("terminal ANSI cells must not be remapped to theme tokens, got %q", result.Lines[0].ANSIString(DefaultTheme()))
	}
}

func TestFrameworkRendersContentViewportDotsAndStoresOverflow(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 16, H: 8}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "pane",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content: ContentVM{
				Kind:   ContentTerminalLive,
				Lines:  []Line{NewLine("abcdef")},
				Extent: ContentExtent{Known: true, Cols: 3, Rows: 1},
			},
		}}},
	}})

	layer := firstLayer(t, result, LayerPanel)
	if got, want := plainContentViewportLines(layer.Lines), []string{
		"abc···········",
		"··············",
		"··············",
		"··············",
		"··············",
		"··············",
	}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("panel layer should keep content viewport projection\n got=%#v\nwant=%#v", got, want)
	}
	if !layer.ContentOverflow.Right {
		t.Fatalf("panel layer should expose content overflow for chrome, got %#v", layer.ContentOverflow)
	}
	lines := result.Lines()
	if got := SliceCells(lines[4], 15, 16); got != ">" {
		t.Fatalf("right overflow marker should be drawn on pane chrome, got %q frame=%#v", got, lines)
	}
	if strings.Contains(strings.Join(plainContentViewportLines(layer.Lines), "\n"), ">") {
		t.Fatalf("right overflow marker must not be written into content layer, got %#v", layer.Lines)
	}
}

func TestFrameworkRendersContentBottomOverflowOnPaneChrome(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 16, H: 8}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "pane",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content: ContentVM{
				Kind: ContentTerminalLive,
				Lines: []Line{
					NewLine("row 1"),
					NewLine("row 2"),
					NewLine("row 3"),
				},
				Extent: ContentExtent{Known: true, Cols: 14, Rows: 8},
			},
		}}},
	}})

	layer := firstLayer(t, result, LayerPanel)
	if !layer.ContentOverflow.Bottom {
		t.Fatalf("panel layer should expose bottom overflow, got %#v", layer.ContentOverflow)
	}
	lines := result.Lines()
	if got := SliceCells(lines[7], 8, 9); got != "v" {
		t.Fatalf("bottom overflow marker should be drawn on pane chrome, got %q frame=%#v", got, lines)
	}
	if strings.Contains(strings.Join(plainContentViewportLines(layer.Lines), "\n"), "v") {
		t.Fatalf("bottom overflow marker must not be written into content layer, got %#v", layer.Lines)
	}
}

func TestRenderContentWritesRequestedOwnerLayer(t *testing.T) {
	c := newCanvas(12, 3)
	result := renderContent(c, ContentVM{
		Kind:   ContentTerminalLive,
		Lines:  []Line{NewLine("float")},
		Extent: ContentExtent{Known: true, Cols: 8, Rows: 2},
	}, Rect{X: 1, Y: 1, W: 8, H: 2}, "floating:float-1:content", LayerFloating)

	if got := result.Lines[0].PlainString(); got != "float   " {
		t.Fatalf("expected content viewport output, got %q", got)
	}
	cell := c.rows[1][1]
	if cell.text != "f" || cell.owner != "floating:float-1:content" || cell.layer != LayerFloating {
		t.Fatalf("content writer must preserve requested owner/layer, got %#v", cell)
	}
}

func plainContentViewportLines(lines []Line) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = line.PlainString()
	}
	return out
}

func assertContentViewportLineWidths(t *testing.T, lines []Line, width int) {
	t.Helper()
	for i, line := range lines {
		if got := line.Width(); got != width {
			t.Fatalf("line %d width=%d want=%d line=%#v plain=%q", i, got, width, line, line.PlainString())
		}
	}
}

func contentViewportLineHasANSI(line Line, text string, style ANSICellStyle) bool {
	for _, cell := range line.Cells {
		if cell.Text == text && cell.ANSIStyle == style {
			return true
		}
	}
	return false
}
