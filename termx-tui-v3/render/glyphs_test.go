package render

import (
	"strings"
	"testing"
)

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

func TestPaneChromeActionTextOnlyShowsWiredSplitAndCloseActions(t *testing.T) {
	ResetPaneChromeGlyphs()
	defer ResetPaneChromeGlyphs()

	glyphs := DefaultPaneChromeGlyphs()
	got := paneChromeActionText(40)
	want := "[" + glyphs.SplitHorizontal + "]─[" + glyphs.SplitVertical + "]─[" + glyphs.Close + "]"
	if got != want {
		t.Fatalf("wired action text got=%q want=%q", got, want)
	}
	if strings.Contains(got, glyphs.Zoom) {
		t.Fatalf("zoom action is not wired yet and must stay hidden, got=%q", got)
	}
	if got := paneChromeActionText(8); got != "["+glyphs.Close+"]" {
		t.Fatalf("narrow pane should degrade to close-only action, got=%q", got)
	}
	if got := paneChromeActionText(7); got != "" {
		t.Fatalf("too-narrow pane should hide action text, got=%q", got)
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
