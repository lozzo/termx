package render

import (
	"strings"
	"testing"
)

func TestPaneChromeGlyphsDefaultToWireframeUnicodeAndRemainCellSafe(t *testing.T) {
	ResetPaneChromeGlyphs()
	defer ResetPaneChromeGlyphs()

	glyphs := DefaultPaneChromeGlyphs()
	if glyphs.Close != "" || glyphs.Zoom != "" || glyphs.SplitVertical != "" || glyphs.SplitHorizontal != "" {
		t.Fatalf("unexpected default Nerd Font glyphs: %#v", glyphs)
	}
	for name, glyph := range map[string]string{
		"close": glyphs.Close,
		"zoom":  glyphs.Zoom,
		"split": glyphs.SplitVertical,
		"run":   glyphs.Running,
	} {
		if DisplayWidth(glyph) != 1 {
			t.Fatalf("%s glyph must be one terminal cell, got width=%d glyph=%q", name, DisplayWidth(glyph), glyph)
		}
	}
}

func TestPaneChromeActionTextShowsWiredSplitAndCloseActions(t *testing.T) {
	ResetPaneChromeGlyphs()
	defer ResetPaneChromeGlyphs()

	glyphs := DefaultPaneChromeGlyphs()
	got := paneChromeActionText(40)
	want := "[" + glyphs.Zoom + "]─[" + glyphs.SplitVertical + "]─[" + glyphs.SplitHorizontal + "]─[" + glyphs.Close + "]"
	if got != want {
		t.Fatalf("wired action text got=%q want=%q", got, want)
	}
	if got := paneChromeActionText(8); got != "["+glyphs.Close+"]" {
		t.Fatalf("narrow pane should degrade to close-only action, got=%q", got)
	}
	if got := paneChromeActionText(7); got != "" {
		t.Fatalf("too-narrow pane should hide action text, got=%q", got)
	}
}

func TestFloatingChromeActionTextKeepsBracketedPaneChromeWithoutSplit(t *testing.T) {
	ResetPaneChromeGlyphs()
	defer ResetPaneChromeGlyphs()

	got := paneChromeActionTextFromItems(floatingChromeActionItems(30))
	want := "[]─[]─[" + paneChromeZoomGlyph() + "]─[" + paneChromeCloseGlyph() + "]"
	if got != want {
		t.Fatalf("floating actions got=%q want=%q", got, want)
	}
	if got := paneChromeActionTextFromItems(floatingChromeActionItems(8)); got != "["+paneChromeCloseGlyph()+"]" {
		t.Fatalf("narrow floating should degrade to close-only action, got=%q", got)
	}
	if got := paneChromeActionTextFromItems(floatingChromeActionItems(7)); got != "" {
		t.Fatalf("too-narrow floating should hide action text, got=%q", got)
	}
}

func TestSetPaneChromeGlyphsAllowsUTF8Overrides(t *testing.T) {
	ResetPaneChromeGlyphs()
	defer ResetPaneChromeGlyphs()

	SetPaneChromeGlyphs(PaneChromeGlyphs{
		Close:         "❌",
		Zoom:          "🔎",
		SplitVertical: "↕",
	})
	if paneChromeCloseGlyph() != "❌" || paneChromeZoomGlyph() != "🔎" || paneChromeSplitVerticalGlyph() != "↕" {
		t.Fatalf("glyph override did not apply, got close=%q zoom=%q splitVertical=%q", paneChromeCloseGlyph(), paneChromeZoomGlyph(), paneChromeSplitVerticalGlyph())
	}
	if DisplayWidth(paneChromeCloseGlyph()) != 2 {
		t.Fatalf("emoji override width should be measured with display cells, got %d", DisplayWidth(paneChromeCloseGlyph()))
	}
}

func TestANSIStringReanchorsAfterAmbiguousEmojiBeforeBorder(t *testing.T) {
	line := Line{Cells: []Cell{
		NewCell("♻️"),
		NewCell("│"),
	}}

	got := line.ANSIString(DefaultTheme())
	if !strings.Contains(got, "♻️\x1b[3G│") {
		t.Fatalf("ambiguous emoji should re-anchor before following border, got %q", got)
	}
}
