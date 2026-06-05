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
	if got, want := paneChromeCompactActionText(), "["+glyphs.Zoom+"]─["+glyphs.Close+"]"; got != want {
		t.Fatalf("compact action text got=%q want=%q", got, want)
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
	if got := paneChromeCloseActionText(); got != "[❌]" {
		t.Fatalf("close glyph override got=%q", got)
	}
	if got := paneChromeCompactActionText(); got != "[🔎]─[❌]" {
		t.Fatalf("compact action override got=%q", got)
	}
	if got := paneChromeActionClusterText(); got != "["+DefaultPaneChromeGlyphs().SplitHorizontal+"]─[↕]─[🔎]─[❌]" {
		t.Fatalf("partial override should keep defaults for empty fields, got=%q", got)
	}
	if DisplayWidth(paneChromeCloseActionText()) != 4 {
		t.Fatalf("emoji override width should be measured with display cells, got %d", DisplayWidth(paneChromeCloseActionText()))
	}
}
