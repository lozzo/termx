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
	for _, tc := range []struct {
		name string
		got  lipgloss.Style
		want interface {
			RGBA() (r, g, bl, alpha uint32)
		}
	}{
		{name: "footer", got: pickerFooterStyle(theme), want: lipgloss.Color(theme.footerTextBG)},
		{name: "line", got: pickerLineStyle(theme), want: lipgloss.Color(overlayCardBG(theme))},
		{name: "selected-line", got: pickerSelectedLineStyle(theme), want: lipgloss.Color(theme.selectedBG)},
		{name: "create-line", got: pickerCreateRowStyle(theme), want: lipgloss.Color(theme.createBG)},
		{name: "marker", got: promptFieldMarkerStyle(theme, true), want: lipgloss.Color(theme.fieldBG)},
		{name: "label", got: promptFieldLabelStyle(theme, true), want: lipgloss.Color(theme.fieldBG)},
		{name: "active-value", got: promptFieldValueStyle(theme, true), want: lipgloss.Color(theme.fieldBG)},
		{name: "inactive-value", got: promptFieldValueStyle(theme, false), want: lipgloss.Color(theme.fieldBG)},
		{name: "section-title", got: overlaySectionTitleStyle(theme), want: lipgloss.Color(overlayCardBG(theme))},
		{name: "help-key", got: overlayHelpKeyStyle(theme), want: lipgloss.Color(overlayCardBG(theme))},
		{name: "help-action", got: overlayHelpActionStyle(theme), want: lipgloss.Color(overlayCardBG(theme))},
		{name: "footer-key", got: overlayFooterKeyStyle(theme), want: lipgloss.Color(theme.footerKeyBG)},
		{name: "footer-text", got: overlayFooterTextStyle(theme), want: lipgloss.Color(theme.footerTextBG)},
		{name: "footer-plain", got: overlayFooterPlainStyle(theme), want: lipgloss.Color(theme.footerPlainBG)},
	} {
		if !sameColor(tc.got.GetBackground(), tc.want) {
			t.Fatalf("%s background = %#v, want %#v", tc.name, tc.got.GetBackground(), tc.want)
		}
	}
}

func TestStatusBarInlineStylesUseChromeBackground(t *testing.T) {
	theme := defaultUITheme()
	want := lipgloss.Color(theme.chromeBG)
	for _, tc := range []struct {
		name string
		got  lipgloss.Style
	}{
		{name: "hint-key", got: statusHintKeyStyle(theme)},
		{name: "hint-text", got: statusHintTextStyle(theme)},
		{name: "meta", got: statusMetaStyle(theme)},
		{name: "default", got: statusPartDefaultStyle(theme)},
		{name: "error", got: statusPartErrorStyle(theme)},
		{name: "notice", got: statusPartNoticeStyle(theme)},
	} {
		if !sameColor(tc.got.GetBackground(), want) {
			t.Fatalf("%s background = %#v, want %#v", tc.name, tc.got.GetBackground(), want)
		}
	}
}

func TestRenderOverlaySearchLineFillsEditableWidth(t *testing.T) {
	theme := defaultUITheme()
	line := renderOverlaySearchLine(theme, "", 0, false, 40)
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
		{Label: "submit"},
		{Label: "cancel"},
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
	line := renderOverlaySpan(style, "submit", 40)
	if got := xansi.StringWidth(line); got != 40 {
		t.Fatalf("overlay span width = %d, want 40", got)
	}
}

func TestDarkThemeAccentTokensStayHostDerivedWithoutHostPalette(t *testing.T) {
	theme := uiThemeFromHostColors("#080b14", "#e5e7eb", nil)

	if theme.chromeBG != "#080b14" || theme.panelBG != "#080b14" {
		t.Fatalf("expected chrome and panel backgrounds to stay host-native, got chrome=%q panel=%q", theme.chromeBG, theme.panelBG)
	}
	if got := contrastRatio(theme.chromeAccent, theme.chromeBG); got < 3.0 {
		t.Fatalf("accent contrast = %.2f, want >= 3.00", got)
	}
	if theme.chromeAccent == ensureContrast("#8b5cf6", theme.chromeBG, 3.2) {
		t.Fatalf("expected chrome accent fallback to derive from host colors before using the fixed accent fallback, got %q", theme.chromeAccent)
	}
	if got := contrastRatio(theme.panelBorder2, theme.panelBG); got < 1.05 {
		t.Fatalf("muted border contrast = %.2f, want >= 1.05", got)
	}
	if theme.footerTextFG != theme.hintTextFG {
		t.Fatalf("expected overlay/footer hint text styles to share the same muted fg, got footer=%q hint=%q", theme.footerTextFG, theme.hintTextFG)
	}
}

func TestSemanticColorsStayLegibleWithoutHostPalette(t *testing.T) {
	fixedFallback := map[string]func(string) string{
		"success":      func(bg string) string { return ensureContrast("#34d399", bg, 2.8) },
		"danger":       func(bg string) string { return ensureContrast("#f87171", bg, 2.8) },
		"warning":      func(bg string) string { return ensureContrast("#fbbf24", bg, 2.8) },
		"info":         func(bg string) string { return ensureContrast("#60a5fa", bg, 2.8) },
		"chromeAccent": func(bg string) string { return ensureContrast("#8b5cf6", bg, 3.2) },
	}
	for _, bg := range []string{"#000000", "#080b14", "#ffffff", "#f5f5f5"} {
		theme := uiThemeFromHostColors(bg, "", nil)
		for _, tc := range []struct {
			name  string
			color string
		}{
			{"success", theme.success},
			{"danger", theme.danger},
			{"warning", theme.warning},
			{"info", theme.info},
			{"chromeAccent", theme.chromeAccent},
		} {
			if cr := contrastRatio(tc.color, bg); cr < 2.5 {
				t.Errorf("bg=%s %s=%q contrast %.2f < 2.5, invisible", bg, tc.name, tc.color, cr)
			}
			if tc.color == fixedFallback[tc.name](bg) {
				t.Errorf("bg=%s %s=%q unexpectedly fell back to fixed semantic color", bg, tc.name, tc.color)
			}
		}
	}
}

func TestStatusBarChipColorsAreDistinguishable(t *testing.T) {
	theme := defaultUITheme()
	rootColors := []string{theme.success, theme.danger, theme.chromeAccent, theme.warning, theme.info}
	for i := 0; i < len(rootColors); i++ {
		for j := i + 1; j < len(rootColors); j++ {
			if rootColors[i] == rootColors[j] {
				t.Errorf("rootColors[%d]=%q and rootColors[%d]=%q are identical", i, rootColors[i], j, rootColors[j])
			}
		}
	}
}

func TestHostPaletteDrivesSemanticAccentTokens(t *testing.T) {
	palette := map[int]string{
		14: "#58e1ff",
		10: "#78f5b2",
		11: "#ffd666",
		9:  "#ff7b96",
	}
	theme := uiThemeFromHostColors("#09111f", "#dbeafe", palette)

	if got := contrastRatio(theme.chromeAccent, "#09111f"); got < 3.2 {
		t.Fatalf("chrome accent contrast = %.2f, want >= 3.20", got)
	}
	if theme.success == ensureContrast("#34d399", "#09111f", 3.0) {
		t.Fatalf("expected host palette success color to override fallback, got %q", theme.success)
	}
	if theme.success != ensureContrast("#78f5b2", "#09111f", 3.0) {
		t.Fatalf("expected success color to use host palette, got %q", theme.success)
	}
	if theme.chromeAccent != ensureContrast("#58e1ff", "#09111f", 3.2) {
		t.Fatalf("expected chrome accent to use host palette, got %q", theme.chromeAccent)
	}
}

func TestTopBarHierarchySeparatesCreateAndActionIntensity(t *testing.T) {
	theme := uiThemeFromHostColors("#0b1020", "#dbeafe", nil)

	createContrast := contrastRatio(theme.tabCreateFG, theme.chromeBG)
	actionContrast := contrastRatio(theme.tabActionFG, theme.chromeBG)
	actionOnContrast := contrastRatio(theme.tabActionOnFG, theme.chromeBG)

	if createContrast <= actionContrast {
		t.Fatalf("expected create token to stand above regular action, got create %.2f action %.2f", createContrast, actionContrast)
	}
	if actionOnContrast <= actionContrast {
		t.Fatalf("expected active action token to stand above regular action, got active %.2f action %.2f", actionOnContrast, actionContrast)
	}
}

func sameColor(a, b interface {
	RGBA() (r, g, bl, alpha uint32)
}) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}
