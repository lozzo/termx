package render

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestOverlayContainerStylesUseSharedBackground(t *testing.T) {
	theme := defaultUITheme()
	want := lipgloss.Color(overlayCardBG(theme))

	for _, tc := range []struct {
		name string
		got  lipgloss.Style
	}{
		{name: "card-fill", got: overlayCardFillStyle(theme)},
		{name: "title", got: terminalPickerTitleStyle(theme)},
		{name: "border-title", got: modalBorderTitleStyle(theme)},
		{name: "border", got: pickerBorderStyle(theme)},
	} {
		if !sameColor(tc.got.GetBackground(), want) {
			t.Fatalf("%s background = %#v, want %#v", tc.name, tc.got.GetBackground(), want)
		}
	}
}

func TestOverlayInlineStylesUseSharedBackground(t *testing.T) {
	theme := defaultUITheme()
	want := lipgloss.Color(overlayCardBG(theme))
	for _, tc := range []struct {
		name string
		got  lipgloss.Style
	}{
		{name: "footer", got: pickerFooterStyle(theme)},
		{name: "line", got: pickerLineStyle(theme)},
		{name: "selected-line", got: pickerSelectedLineStyle(theme)},
		{name: "create-line", got: pickerCreateRowStyle(theme)},
		{name: "marker", got: promptFieldMarkerStyle(theme, true)},
		{name: "label", got: promptFieldLabelStyle(theme, true)},
		{name: "active-value", got: promptFieldValueStyle(theme, true)},
		{name: "inactive-value", got: promptFieldValueStyle(theme, false)},
		{name: "section-title", got: overlaySectionTitleStyle(theme)},
		{name: "help-key", got: overlayHelpKeyStyle(theme)},
		{name: "help-action", got: overlayHelpActionStyle(theme)},
		{name: "footer-key", got: overlayFooterKeyStyle(theme)},
		{name: "footer-text", got: overlayFooterTextStyle(theme)},
		{name: "footer-plain", got: overlayFooterPlainStyle(theme)},
	} {
		if !sameColor(tc.got.GetBackground(), want) {
			t.Fatalf("%s background = %#v, want %#v", tc.name, tc.got.GetBackground(), want)
		}
	}
}

func TestRenderOverlaySearchLineFillsEditableWidth(t *testing.T) {
	theme := defaultUITheme()
	line := renderOverlaySearchLine(theme, "", 40)
	if got := xansi.StringWidth(line); got != 40 {
		t.Fatalf("search line width = %d, want 40", got)
	}
}

func TestRenderOverlayFooterLineFillsRowWidth(t *testing.T) {
	theme := defaultUITheme()
	prompt := &modal.PromptState{Kind: "rename-tab", Value: "demo"}
	footer, _ := layoutOverlayFooterActionsWithTheme(theme, promptFooterActionSpecs(prompt), workbench.Rect{W: 40, H: 1})
	line := renderOverlayFooterLine(theme, footer, 40)
	if got := xansi.StringWidth(line); got != 40 {
		t.Fatalf("footer line width = %d, want 40", got)
	}
}

func TestLayoutOverlayFooterActionsStylesInterActionGap(t *testing.T) {
	theme := defaultUITheme()
	line, _ := layoutOverlayFooterActionsWithTheme(theme, []overlayFooterActionSpec{
		{Label: "[Enter] submit"},
		{Label: "[Esc] cancel"},
	}, workbench.Rect{W: 40, H: 1})
	wantGap := renderOverlaySpan(overlayFooterPlainStyle(theme), "", overlayFooterActionGap)
	if !strings.Contains(line, wantGap) {
		t.Fatalf("footer gap missing styled span:\n%q", line)
	}
}

func TestHelpOverlayLinesStyleInterActionGap(t *testing.T) {
	theme := defaultUITheme()
	lines := helpOverlayLines(theme, &modal.HelpState{
		Sections: []modal.HelpSection{{
			Title:    "Most Used",
			Bindings: []modal.HelpBinding{{Key: "Ctrl-P", Action: "pane mode"}},
		}},
	}, 40)
	wantGap := renderOverlaySpan(overlayHelpActionStyle(theme), "", 2)
	found := false
	for _, line := range lines {
		if strings.Contains(line, wantGap) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("help overlay lines missing styled gap: %#v", lines)
	}
}

func TestTerminalPoolFooterStylesGapAndIndent(t *testing.T) {
	theme := defaultUITheme()
	line, _ := layoutTerminalPoolFooterActionsWithTheme(theme, 80, 24)
	wantGap := renderOverlaySpan(overlayFooterPlainStyle(theme), "", terminalPoolFooterActionGap)
	wantIndent := renderOverlaySpan(overlayFooterPlainStyle(theme), "", terminalPoolListLeftX)
	if !strings.Contains(line, wantGap) {
		t.Fatalf("terminal pool footer missing styled gap:\n%q", line)
	}
	if !strings.Contains(line, wantIndent) {
		t.Fatalf("terminal pool footer missing styled indent:\n%q", line)
	}
}

func TestRenderOverlaySpanFillsRequestedWidth(t *testing.T) {
	style := overlayCardFillStyle(defaultUITheme())
	line := renderOverlaySpan(style, "[Enter] submit", 40)
	if got := xansi.StringWidth(line); got != 40 {
		t.Fatalf("overlay span width = %d, want 40", got)
	}
}

func sameColor(a, b interface{ RGBA() (r, g, bl, alpha uint32) }) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}
