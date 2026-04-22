package render

import "testing"

func TestSetPaneChromeGlyphsOverridesSubset(t *testing.T) {
	ResetPaneChromeGlyphs()
	defer ResetPaneChromeGlyphs()

	SetPaneChromeGlyphs(PaneChromeGlyphs{
		Zoom:    "@",
		Running: "*",
	})

	if got := paneZoomIcon(); got != "@" {
		t.Fatalf("paneZoomIcon() = %q, want %q", got, "@")
	}
	if got := paneRunningIcon(); got != "*" {
		t.Fatalf("paneRunningIcon() = %q, want %q", got, "*")
	}
	if got := paneCloseIcon(); got != DefaultPaneChromeGlyphs().Close {
		t.Fatalf("paneCloseIcon() = %q, want default %q", got, DefaultPaneChromeGlyphs().Close)
	}
}

func TestDefaultPaneChromeSplitGlyphsMatchSplitDirections(t *testing.T) {
	glyphs := DefaultPaneChromeGlyphs()

	if got := glyphs.SplitVertical; got != "\ueb56" {
		t.Fatalf("SplitVertical = %q, want %q", got, "\ueb56")
	}
	if got := glyphs.SplitHorizontal; got != "\ueb57" {
		t.Fatalf("SplitHorizontal = %q, want %q", got, "\ueb57")
	}
}
