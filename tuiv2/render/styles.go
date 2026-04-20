package render

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

type uiTheme struct {
	hostBG string
	hostFG string

	chromeBG     string
	chromeAltBG  string
	chromeText   string
	chromeMuted  string
	chromeAccent string

	panelBG      string
	panelAltBG   string
	panelStrong  string
	panelText    string
	panelMuted   string
	panelBorder  string
	panelBorder2 string

	fieldBG     string
	fieldText   string
	fieldAccent string

	selectedBG   string
	selectedText string
	createBG     string
	createText   string

	metaBG   string
	metaText string
	errorBG  string
	errorFG  string
	noticeBG string
	noticeFG string

	success string
	warning string
	danger  string
	info    string

	footerKeyBG   string
	footerKeyFG   string
	footerTextBG  string
	footerTextFG  string
	footerPlainBG string
	footerPlainFG string
	hintKeyBG     string
	hintKeyFG     string
	hintTextBG    string
	hintTextFG    string

	tabWorkspaceBG string
	tabWorkspaceFG string
	tabActiveBG    string
	tabActiveFG    string
	tabInactiveBG  string
	tabInactiveFG  string
	tabCreateBG    string
	tabCreateFG    string
	tabActionBG    string
	tabActionFG    string
	tabActionOnBG  string
	tabActionOnFG  string
}

func defaultUITheme() uiTheme {
	return uiThemeFromHostColors("", "", nil)
}

func uiThemeForState(state VisibleRenderState) uiTheme {
	if state.Runtime == nil {
		return defaultUITheme()
	}
	return uiThemeFromHostColors(state.Runtime.HostDefaultBG, state.Runtime.HostDefaultFG, state.Runtime.HostPalette)
}

func uiThemeForRuntime(runtimeState *VisibleRuntimeStateProxy) uiTheme {
	if runtimeState == nil {
		return defaultUITheme()
	}
	return uiThemeFromHostColors(runtimeState.HostDefaultBG, runtimeState.HostDefaultFG, runtimeState.HostPalette)
}

func uiThemeFromHostColors(hostBG, hostFG string, hostPalette map[int]string) uiTheme {
	hostBG, hostFG = normalizeHostColors(hostBG, hostFG)

	chromeBG := hostBG
	chromeAltBG := mixHex(hostBG, hostFG, 0.08)
	panelBG := hostBG
	panelAltBG := mixHex(hostBG, hostFG, 0.06)
	panelStrong := mixHex(hostBG, hostFG, 0.11)
	panelText := hostFG
	accent := resolveSemanticColor(
		hostBG,
		hostPalette,
		[]int{13, 5, 12, 14, 6, 4},
		hostDerivedToken(hostBG, hostFG, 0.74, 3.2),
		ensureContrast("#c084fc", hostBG, 3.2),
		3.2,
	)
	panelMuted := ensureContrast(mixHex(hostBG, hostFG, 0.46), panelBG, 2.0)
	success := resolveSemanticColor(hostBG, hostPalette, []int{10, 2}, hostDerivedToken(hostBG, hostFG, 0.66, 2.8), ensureContrast("#34d399", hostBG, 2.8), 2.8)
	warning := resolveSemanticColor(hostBG, hostPalette, []int{11, 3}, hostDerivedToken(hostBG, hostFG, 0.58, 2.8), ensureContrast("#fbbf24", hostBG, 2.8), 2.8)
	danger := resolveSemanticColor(hostBG, hostPalette, []int{9, 1}, hostDerivedToken(hostBG, hostFG, 0.82, 2.8), ensureContrast("#f87171", hostBG, 2.8), 2.8)
	info := resolveSemanticColor(hostBG, hostPalette, []int{14, 6}, hostDerivedToken(hostBG, hostFG, 0.50, 2.8), ensureContrast("#60a5fa", hostBG, 2.8), 2.8)
	panelBorder := ensureContrast(mixHex(hostBG, hostFG, 0.26), panelBG, 1.24)
	panelBorder2 := ensureContrast(mixHex(hostBG, hostFG, 0.42), panelBG, 1.6)
	fieldBG := mixHex(panelAltBG, accent, 0.08)
	fieldText := ensureContrast(panelText, fieldBG, 4.0)
	fieldAccent := ensureContrast(accent, fieldBG, 3.0)
	selectedBG := mixHex(panelAltBG, accent, 0.16)
	selectedText := ensureContrast(mixHex(panelText, accent, 0.18), selectedBG, 4.2)
	createBG := mixHex(panelAltBG, success, 0.18)
	createText := ensureContrast(panelText, createBG, 4.0)
	metaBG := hostBG
	metaText := panelMuted
	errorBG := hostBG
	errorFG := danger
	noticeBG := hostBG
	noticeFG := info
	hintKeyBG := chromeBG
	hintKeyFG := ensureContrast(accent, chromeBG, 4.0)
	hintTextBG := chromeBG
	hintTextFG := panelMuted
	footerKeyBG := mixHex(panelAltBG, accent, 0.18)
	footerKeyFG := ensureContrast(contrastTextColor(footerKeyBG), footerKeyBG, 4.0)
	footerTextBG := panelStrong
	footerTextFG := ensureContrast(panelMuted, footerTextBG, 2.4)
	tabWorkspaceBG := mixHex(chromeAltBG, accent, 0.24)
	tabWorkspaceFG := ensureContrast(mixHex(panelText, accent, 0.30), tabWorkspaceBG, 3.5)
	tabActiveBG := mixHex(panelAltBG, accent, 0.14)
	tabActiveFG := ensureContrast(panelText, tabActiveBG, 4.0)
	tabInactiveBG := chromeBG
	tabInactiveFG := panelMuted
	tabCreateBG := mixHex(chromeAltBG, success, 0.18)
	tabCreateFG := ensureContrast(panelText, tabCreateBG, 4.0)
	tabActionBG := chromeAltBG
	tabActionFG := ensureContrast(panelMuted, tabActionBG, 2.4)
	tabActionOnBG := mixHex(chromeAltBG, accent, 0.18)
	tabActionOnFG := ensureContrast(panelText, tabActionOnBG, 4.0)
	chromeText := hostFG

	return uiTheme{
		hostBG: hostBG,
		hostFG: hostFG,

		chromeBG:     chromeBG,
		chromeAltBG:  chromeAltBG,
		chromeText:   chromeText,
		chromeMuted:  panelMuted,
		chromeAccent: accent,

		panelBG:      panelBG,
		panelAltBG:   panelAltBG,
		panelStrong:  panelStrong,
		panelText:    panelText,
		panelMuted:   panelMuted,
		panelBorder:  panelBorder,
		panelBorder2: panelBorder2,

		fieldBG:     fieldBG,
		fieldText:   fieldText,
		fieldAccent: fieldAccent,

		selectedBG:   selectedBG,
		selectedText: selectedText,
		createBG:     createBG,
		createText:   createText,

		metaBG:   metaBG,
		metaText: metaText,
		errorBG:  errorBG,
		errorFG:  errorFG,
		noticeBG: noticeBG,
		noticeFG: noticeFG,

		success: success,
		warning: warning,
		danger:  danger,
		info:    info,

		footerKeyBG:   footerKeyBG,
		footerKeyFG:   footerKeyFG,
		footerTextBG:  footerTextBG,
		footerTextFG:  footerTextFG,
		footerPlainBG: footerTextBG,
		footerPlainFG: footerTextFG,
		hintKeyBG:     hintKeyBG,
		hintKeyFG:     hintKeyFG,
		hintTextBG:    hintTextBG,
		hintTextFG:    hintTextFG,

		tabWorkspaceBG: tabWorkspaceBG,
		tabWorkspaceFG: tabWorkspaceFG,
		tabActiveBG:    tabActiveBG,
		tabActiveFG:    tabActiveFG,
		tabInactiveBG:  tabInactiveBG,
		tabInactiveFG:  tabInactiveFG,
		tabCreateBG:    tabCreateBG,
		tabCreateFG:    tabCreateFG,
		tabActionBG:    tabActionBG,
		tabActionFG:    tabActionFG,
		tabActionOnBG:  tabActionOnBG,
		tabActionOnFG:  tabActionOnFG,
	}
}

func workspaceLabelStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(theme.tabWorkspaceFG)).
		Background(lipgloss.Color(theme.tabWorkspaceBG)).
		Padding(0, 1)
}

func tabInactiveStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.tabInactiveFG)).
		Background(lipgloss.Color(theme.tabInactiveBG))
}

func tabActiveStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(theme.tabActiveFG)).
		Background(lipgloss.Color(theme.tabActiveBG))
}

func tabCreateStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(theme.tabCreateFG)).
		Background(lipgloss.Color(theme.tabCreateBG))
}

func tabActionStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.tabActionFG)).
		Background(lipgloss.Color(theme.tabActionBG))
}

func tabActionActiveStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(theme.tabActionOnFG)).
		Background(lipgloss.Color(theme.tabActionOnBG))
}

func tabCloseStyle(theme uiTheme, active bool) lipgloss.Style {
	bg := theme.tabInactiveBG
	fg := theme.tabInactiveFG
	if active {
		bg = theme.tabActiveBG
		fg = theme.tabActiveFG
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fg)).
		Background(lipgloss.Color(bg))
}

func statusHintKeyStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(theme.hintKeyFG)).
		Background(lipgloss.Color(theme.chromeBG))
}

func statusHintTextStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.hintTextFG)).
		Background(lipgloss.Color(theme.chromeBG))
}

func statusSeparatorStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.chromeMuted)).
		Background(lipgloss.Color(theme.chromeBG))
}

func statusPartDefaultStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.chromeText)).
		Background(lipgloss.Color(theme.chromeBG))
}

func statusPartErrorStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.errorFG)).
		Background(lipgloss.Color(theme.chromeBG)).
		Bold(true)
}

func statusPartNoticeStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.noticeFG)).
		Background(lipgloss.Color(theme.chromeBG)).
		Bold(true)
}

func statusMetaStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.metaText)).
		Background(lipgloss.Color(theme.chromeBG))
}

func statusMetaTokenStyle(theme uiTheme, interactive bool) lipgloss.Style {
	style := lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(lipgloss.Color(theme.metaText)).
		Background(lipgloss.Color(theme.chromeAltBG))
	if interactive {
		style = style.
			Bold(true).
			Foreground(lipgloss.Color(theme.chromeAccent))
	}
	return style
}

func terminalPickerBodyStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.hostFG)).
		Background(lipgloss.Color(theme.hostBG))
}

func terminalPickerTitleStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.hostFG)).
		Background(lipgloss.Color(overlayCardBG(theme))).
		Padding(0, 1).
		Bold(true)
}

func modalBorderTitleStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.hostFG)).
		Background(lipgloss.Color(overlayCardBG(theme))).
		Bold(true)
}

func pickerBorderStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ensureContrast(mixHex(theme.panelText, theme.chromeAccent, 0.12), overlayCardBG(theme), 1.35))).
		Background(lipgloss.Color(overlayCardBG(theme)))
}

func pickerFooterStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.footerTextFG)).
		Background(lipgloss.Color(theme.footerTextBG))
}

func pickerLineStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.hostFG)).
		Background(lipgloss.Color(overlayCardBG(theme)))
}

func pickerSelectedLineStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.selectedText)).
		Background(lipgloss.Color(theme.selectedBG)).
		Bold(true)
}

func pickerCreateRowStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.createText)).
		Background(lipgloss.Color(theme.createBG)).
		Bold(true)
}

func overlayCardBG(theme uiTheme) string {
	return theme.panelAltBG
}

func overlayCardFillStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelText)).
		Background(lipgloss.Color(overlayCardBG(theme)))
}

func promptFieldMarkerStyle(theme uiTheme, _ bool) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.fieldAccent)).
		Background(lipgloss.Color(theme.fieldBG))
}

func promptFieldLabelStyle(theme uiTheme, active bool) lipgloss.Style {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.fieldAccent)).
		Background(lipgloss.Color(theme.fieldBG))
	if active {
		style = style.Bold(true)
	}
	return style
}

func promptFieldValueStyle(theme uiTheme, active bool) lipgloss.Style {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.fieldText)).
		Background(lipgloss.Color(theme.fieldBG))
	if active {
		style = style.Bold(true)
	}
	return style
}

func overlaySectionTitleStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.warning)).
		Background(lipgloss.Color(overlayCardBG(theme))).
		Bold(true)
}

func overlayHelpKeyStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.footerKeyFG)).
		Background(lipgloss.Color(overlayCardBG(theme))).
		Bold(true)
}

func overlayHelpActionStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelText)).
		Background(lipgloss.Color(overlayCardBG(theme)))
}

func overlayFooterKeyStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.footerKeyFG)).
		Background(lipgloss.Color(theme.footerKeyBG)).
		Bold(true)
}

func overlayFooterTextStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.footerTextFG)).
		Background(lipgloss.Color(theme.footerTextBG))
}

func overlayFooterPlainStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.footerPlainFG)).
		Background(lipgloss.Color(theme.footerPlainBG))
}

func normalizeHostColors(hostBG, hostFG string) (string, string) {
	hostBG = strings.TrimSpace(hostBG)
	hostFG = strings.TrimSpace(hostFG)
	bgValid := isHexColor(hostBG)
	fgValid := isHexColor(hostFG)
	switch {
	case bgValid && fgValid:
		return hostBG, hostFG
	case bgValid:
		return hostBG, contrastTextColor(hostBG)
	case fgValid:
		derivedBG := mixHex(hostFG, chromeTextColor(hostFG), 0.92)
		return derivedBG, ensureContrast(hostFG, derivedBG, 4.5)
	default:
		hostBG = "#000000"
		return hostBG, contrastTextColor(hostBG)
	}
}

func hostDerivedToken(bg, fg string, ratio, minContrast float64) string {
	return ensureContrast(mixHex(bg, fg, ratio), bg, minContrast)
}

func isHexColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, ch := range value[1:] {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}

func contrastTextColor(bg string) string {
	return chromeTextColor(bg)
}

func chromeTextColor(bg string) string {
	r, g, b, ok := parseHexColor(bg)
	if !ok {
		return "#f8fafc"
	}
	luminance := 0.2126*float64(r)/255 + 0.7152*float64(g)/255 + 0.0722*float64(b)/255
	if luminance > 0.55 {
		return "#0f172a"
	}
	return "#f8fafc"
}

func mixHex(a, b string, ratio float64) string {
	ar, ag, ab, okA := parseHexColor(a)
	br, bg, bb, okB := parseHexColor(b)
	if !okA {
		return b
	}
	if !okB {
		return a
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	mix := func(x, y uint8) uint8 {
		return uint8(float64(x)*(1-ratio) + float64(y)*ratio)
	}
	return fmt.Sprintf("#%02x%02x%02x", mix(ar, br), mix(ag, bg), mix(ab, bb))
}

func resolveSemanticColor(bg string, palette map[int]string, indexes []int, derived string, fallback string, minContrast float64) string {
	for _, index := range indexes {
		if candidate, ok := palette[index]; ok && isHexColor(strings.TrimSpace(candidate)) {
			return ensureContrast(candidate, bg, minContrast)
		}
	}
	if isHexColor(strings.TrimSpace(derived)) {
		return ensureContrast(derived, bg, minContrast)
	}
	return ensureContrast(fallback, bg, minContrast)
}

func ensureContrast(fg, bg string, minContrast float64) string {
	if !isHexColor(strings.TrimSpace(fg)) || !isHexColor(strings.TrimSpace(bg)) {
		return fg
	}
	if contrastRatio(fg, bg) >= minContrast {
		return fg
	}
	target := chromeTextColor(bg)
	best := fg
	for i := 1; i <= 8; i++ {
		candidate := mixHex(fg, target, float64(i)/8)
		best = candidate
		if contrastRatio(candidate, bg) >= minContrast {
			return candidate
		}
	}
	return best
}

func contrastRatio(a, b string) float64 {
	al := relativeLuminance(a)
	bl := relativeLuminance(b)
	if al < 0 || bl < 0 {
		return 0
	}
	if al < bl {
		al, bl = bl, al
	}
	return (al + 0.05) / (bl + 0.05)
}

func relativeLuminance(value string) float64 {
	r, g, b, ok := parseHexColor(value)
	if !ok {
		return -1
	}
	toLinear := func(channel uint8) float64 {
		v := float64(channel) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	lr := toLinear(r)
	lg := toLinear(g)
	lb := toLinear(b)
	return 0.2126*lr + 0.7152*lg + 0.0722*lb
}

func parseHexColor(value string) (uint8, uint8, uint8, bool) {
	if !isHexColor(value) {
		return 0, 0, 0, false
	}
	rv, err := strconv.ParseUint(value[1:3], 16, 8)
	if err != nil {
		return 0, 0, 0, false
	}
	gv, err := strconv.ParseUint(value[3:5], 16, 8)
	if err != nil {
		return 0, 0, 0, false
	}
	bv, err := strconv.ParseUint(value[5:7], 16, 8)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(rv), uint8(gv), uint8(bv), true
}
