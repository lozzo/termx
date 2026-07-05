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

func TestPaneChromeActionTemplatesAllowGroupCapsuleAndEmptySides(t *testing.T) {
	ResetPaneChromeGlyphs()
	defer ResetPaneChromeGlyphs()

	SetPaneChromeGlyphs(PaneChromeGlyphs{
		ActionLeft:          " ",
		ActionLeftSet:       true,
		ActionRight:         " ",
		ActionRightSet:      true,
		ActionSeparator:     "",
		ActionSeparatorSet:  true,
		ActionGroupLeft:     "[fg:#8ffcff][fg:#071112;bg:#8ffcff;font:bold]",
		ActionGroupLeftSet:  true,
		ActionGroupRight:    "[reset][fg:#8ffcff][reset]",
		ActionGroupRightSet: true,
		Zoom:                "—",
		SplitVertical:       "□",
		SplitHorizontal:     "▭",
		Close:               "⤫",
	})

	got := paneChromeActionText(40)
	want := " —  □  ▭  ⤫ "
	if got != want {
		t.Fatalf("capsule action text got=%q want=%q", got, want)
	}
	if strings.Contains(got, "[fg:") || strings.Contains(got, "[reset]") {
		t.Fatalf("span tags must not render literally, got=%q", got)
	}

	rendered := paneChromeActionRenderedFromItems(paneChromeActionItems(40), StyleAccent)
	foundStyledGlyph := false
	for _, segment := range rendered.Segments {
		if strings.Contains(segment.text, "—") && segment.ansi.FG == "#071112" && segment.ansi.BG == "#8ffcff" && segment.ansi.Bold {
			foundStyledGlyph = true
			break
		}
	}
	if !foundStyledGlyph {
		t.Fatalf("expected action glyph to inherit configured ANSI style, segments=%#v", rendered.Segments)
	}
}

func TestPaneChromeActionTemplatesCanHideOneSide(t *testing.T) {
	ResetPaneChromeGlyphs()
	defer ResetPaneChromeGlyphs()

	SetPaneChromeGlyphs(PaneChromeGlyphs{
		ActionLeft:     "",
		ActionLeftSet:  true,
		ActionRight:    ")",
		ActionRightSet: true,
		Close:          "x",
	})
	if got, want := paneChromeActionText(8), "x)"; got != want {
		t.Fatalf("right-only action template got=%q want=%q", got, want)
	}
}
