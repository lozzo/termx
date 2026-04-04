package render

import (
	"fmt"
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
	return uiThemeFromHostColors("", "")
}

func uiThemeForState(state VisibleRenderState) uiTheme {
	if state.Runtime == nil {
		return defaultUITheme()
	}
	return uiThemeFromHostColors(state.Runtime.HostDefaultBG, state.Runtime.HostDefaultFG)
}

func uiThemeForRuntime(runtimeState *VisibleRuntimeStateProxy) uiTheme {
	if runtimeState == nil {
		return defaultUITheme()
	}
	return uiThemeFromHostColors(runtimeState.HostDefaultBG, runtimeState.HostDefaultFG)
}

func uiThemeFromHostColors(hostBG, hostFG string) uiTheme {
	if !isHexColor(strings.TrimSpace(hostBG)) {
		hostBG = "#000000"
	}
	if !isHexColor(strings.TrimSpace(hostFG)) {
		hostFG = contrastTextColor(hostBG)
	}

	chromeBG := mixHex(hostBG, "#0f172a", 0.76)
	chromeAltBG := mixHex(hostBG, "#111827", 0.88)
	panelBG := mixHex(hostBG, "#0b1324", 0.9)
	panelAltBG := mixHex(hostBG, "#101a2e", 0.84)
	panelStrong := mixHex(hostBG, "#172338", 0.84)
	panelText := contrastTextColor(panelBG)
	panelMuted := mixHex(panelText, panelBG, 0.38)
	panelBorder := mixHex(hostBG, panelText, 0.28)
	panelBorder2 := mixHex(hostBG, panelText, 0.18)
	fieldBG := mixHex(hostBG, "#101a2e", 0.8)
	fieldText := contrastTextColor(fieldBG)
	selectedBG := mixHex(hostBG, "#1d4ed8", 0.7)
	selectedText := contrastTextColor(selectedBG)
	createBG := mixHex(hostBG, "#14532d", 0.78)
	createText := contrastTextColor(createBG)
	metaBG := mixHex(hostBG, "#121c2e", 0.84)
	metaText := contrastTextColor(metaBG)
	errorBG := mixHex(hostBG, "#7f1d1d", 0.92)
	errorFG := contrastTextColor(errorBG)
	noticeBG := mixHex(hostBG, "#0f766e", 0.86)
	noticeFG := contrastTextColor(noticeBG)
	footerKeyBG := mixHex(hostBG, "#223554", 0.84)
	footerKeyFG := contrastTextColor(footerKeyBG)
	footerTextBG := mixHex(hostBG, "#101a2e", 0.9)
	footerTextFG := contrastTextColor(footerTextBG)
	tabWorkspaceBG := mixHex(hostBG, "#182033", 0.9)
	tabWorkspaceFG := contrastTextColor(tabWorkspaceBG)
	tabActiveBG := hostBG
	tabActiveFG := contrastTextColor(tabActiveBG)
	tabInactiveBG := mixHex(hostBG, "#64748b", 0.32)
	tabInactiveFG := mixHex(tabActiveFG, tabInactiveBG, 0.56)
	tabCreateBG := mixHex(hostBG, "#0f766e", 0.84)
	tabCreateFG := contrastTextColor(tabCreateBG)
	tabActionBG := mixHex(hostBG, "#111827", 0.84)
	tabActionFG := contrastTextColor(tabActionBG)
	tabActionOnBG := mixHex(hostBG, "#1f2937", 0.82)
	tabActionOnFG := contrastTextColor(tabActionOnBG)

	return uiTheme{
		hostBG: hostBG,
		hostFG: hostFG,

		chromeBG:     chromeBG,
		chromeAltBG:  chromeAltBG,
		chromeText:   contrastTextColor(chromeBG),
		chromeMuted:  mixHex(contrastTextColor(chromeBG), chromeBG, 0.42),
		chromeAccent: mixHex("#8b5cf6", hostBG, 0.16),

		panelBG:      panelBG,
		panelAltBG:   panelAltBG,
		panelStrong:  panelStrong,
		panelText:    panelText,
		panelMuted:   panelMuted,
		panelBorder:  panelBorder,
		panelBorder2: panelBorder2,

		fieldBG:     fieldBG,
		fieldText:   fieldText,
		fieldAccent: mixHex("#67e8f9", hostBG, 0.18),

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

		success: mixHex("#4ade80", hostBG, 0.1),
		warning: mixHex("#fbbf24", hostBG, 0.12),
		danger:  mixHex("#f87171", hostBG, 0.12),
		info:    mixHex("#67e8f9", hostBG, 0.14),

		footerKeyBG:   footerKeyBG,
		footerKeyFG:   footerKeyFG,
		footerTextBG:  footerTextBG,
		footerTextFG:  footerTextFG,
		footerPlainBG: footerTextBG,
		footerPlainFG: footerTextFG,
		hintKeyBG:     footerKeyBG,
		hintKeyFG:     footerKeyFG,
		hintTextBG:    footerTextBG,
		hintTextFG:    footerTextFG,

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

func backgroundStyle(bg string) lipgloss.Style {
	return lipgloss.NewStyle().Background(lipgloss.Color(bg))
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

func statusChipStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Padding(0, 1).
		Background(lipgloss.Color(theme.chromeBG))
}

func statusHintKeyStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(theme.hintKeyFG)).
		Background(lipgloss.Color(theme.hintKeyBG))
}

func statusHintTextStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.hintTextFG)).
		Background(lipgloss.Color(theme.hintTextBG))
}

func statusSeparatorStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.chromeMuted)).
		Background(lipgloss.Color(theme.chromeBG))
}

func statusPartDefaultStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.chromeText)).
		Background(lipgloss.Color(theme.chromeBG)).
		Padding(0, 1)
}

func statusPartErrorStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.errorFG)).
		Background(lipgloss.Color(theme.errorBG)).
		Bold(true).
		Padding(0, 1)
}

func statusPartNoticeStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.noticeFG)).
		Background(lipgloss.Color(theme.noticeBG)).
		Bold(true).
		Padding(0, 1)
}

func statusMetaStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.metaText)).
		Background(lipgloss.Color(theme.metaBG)).
		Padding(0, 1)
}

func terminalPickerBodyStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelText)).
		Background(lipgloss.Color(theme.hostBG))
}

func terminalPickerTitleStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelText)).
		Background(lipgloss.Color(theme.panelStrong)).
		Padding(0, 1).
		Bold(true)
}

func terminalPickerQueryStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.fieldText)).
		Background(lipgloss.Color(theme.fieldBG))
}

func pickerBorderStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelBorder)).
		Background(lipgloss.Color(theme.panelBG))
}

func pickerFooterStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelMuted)).
		Background(lipgloss.Color(theme.panelAltBG))
}

func pickerLineStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelText)).
		Background(lipgloss.Color(theme.panelBG))
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

func overlayFieldPrefixStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.fieldAccent)).
		Background(lipgloss.Color(theme.fieldBG)).
		Bold(true)
}

func overlayFieldValueStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.fieldText)).
		Background(lipgloss.Color(theme.fieldBG))
}

func overlayCardFillStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelText)).
		Background(lipgloss.Color(theme.panelBG))
}

func overlaySectionTitleStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.warning)).
		Background(lipgloss.Color(theme.panelBG)).
		Bold(true)
}

func overlayHelpKeyStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.success)).
		Background(lipgloss.Color(theme.panelBG)).
		Bold(true)
}

func overlayHelpActionStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelText)).
		Background(lipgloss.Color(theme.panelBG))
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
