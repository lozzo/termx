package render

import "testing"

func TestPaneChromeGlyphsDefaultToNerdFontAndRemainCellSafe(t *testing.T) {
	ResetPaneChromeGlyphs()
	defer ResetPaneChromeGlyphs()

	glyphs := DefaultPaneChromeGlyphs()
	if glyphs.Close != "\uea76" || glyphs.Zoom != "\ueb01" || glyphs.SplitVertical != "\ueb56" || glyphs.SplitHorizontal != "\ueb57" {
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

func TestPaneChromeActionTextHiddenUntilNerdFontDesignLands(t *testing.T) {
	ResetPaneChromeGlyphs()
	defer ResetPaneChromeGlyphs()

	for _, width := range []int{7, 8, 40, 120} {
		if got := paneChromeActionText(width); got != "" {
			t.Fatalf("tiled pane action text should stay hidden before Nerd Font design lands, width=%d got=%q", width, got)
		}
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
