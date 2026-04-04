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

	chromeBG := mixHex(hostBG, hostFG, 0.08)
	chromeAltBG := mixHex(hostBG, hostFG, 0.12)
	panelBG := mixHex(hostBG, hostFG, 0.06)
	panelAltBG := mixHex(hostBG, hostFG, 0.1)
	panelStrong := mixHex(hostBG, hostFG, 0.14)
	panelText := contrastTextColor(panelBG)
	panelMuted := mixHex(panelText, panelBG, 0.4)
	panelBorder := mixHex(hostBG, hostFG, 0.34)
	panelBorder2 := mixHex(hostBG, hostFG, 0.22)
	fieldBG := mixHex(hostBG, hostFG, 0.1)
	fieldText := contrastTextColor(fieldBG)
	selectedBG := mixHex(hostBG, hostFG, 0.16)
	selectedText := contrastTextColor(selectedBG)
	createBG := mixHex(hostBG, hostFG, 0.18)
	createText := contrastTextColor(createBG)
	metaBG := mixHex(hostBG, hostFG, 0.11)
	metaText := contrastTextColor(metaBG)
	errorBG := mixHex(hostBG, hostFG, 0.2)
	errorFG := contrastTextColor(errorBG)
	noticeBG := mixHex(hostBG, hostFG, 0.16)
	noticeFG := contrastTextColor(noticeBG)
	footerKeyBG := mixHex(hostBG, hostFG, 0.14)
	footerKeyFG := contrastTextColor(footerKeyBG)
	footerTextBG := mixHex(hostBG, hostFG, 0.1)
	footerTextFG := contrastTextColor(footerTextBG)
	tabWorkspaceBG := mixHex(hostBG, hostFG, 0.14)
	tabWorkspaceFG := contrastTextColor(tabWorkspaceBG)
	tabActiveBG := hostBG
	tabActiveFG := contrastTextColor(tabActiveBG)
	tabInactiveBG := mixHex(hostBG, hostFG, 0.18)
	tabInactiveFG := mixHex(tabActiveFG, tabInactiveBG, 0.56)
	tabCreateBG := tabActiveBG
	tabCreateFG := tabActiveFG
	tabActionBG := mixHex(hostBG, hostFG, 0.12)
	tabActionFG := contrastTextColor(tabActionBG)
	tabActionOnBG := mixHex(hostBG, hostFG, 0.16)
	tabActionOnFG := contrastTextColor(tabActionOnBG)

	return uiTheme{
		hostBG: hostBG,
		hostFG: hostFG,

		chromeBG:     chromeBG,
		chromeAltBG:  chromeAltBG,
		chromeText:   contrastTextColor(chromeBG),
		chromeMuted:  mixHex(contrastTextColor(chromeBG), chromeBG, 0.46),
		chromeAccent: mixHex(hostBG, hostFG, 0.58),

		panelBG:      panelBG,
		panelAltBG:   panelAltBG,
		panelStrong:  panelStrong,
		panelText:    panelText,
		panelMuted:   panelMuted,
		panelBorder:  panelBorder,
		panelBorder2: panelBorder2,

		fieldBG:     fieldBG,
		fieldText:   fieldText,
		fieldAccent: mixHex(hostBG, hostFG, 0.78),

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

		success: mixHex(hostBG, hostFG, 0.3),
		warning: mixHex(hostBG, hostFG, 0.42),
		danger:  mixHex(hostBG, hostFG, 0.24),
		info:    mixHex(hostBG, hostFG, 0.5),

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
		Foreground(lipgloss.Color(theme.hostFG)).
		Background(lipgloss.Color(theme.hostBG))
}

func terminalPickerTitleStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.hostFG)).
		Background(lipgloss.Color(theme.panelBG)).
		Padding(0, 1).
		Bold(true)
}

func modalBorderTitleStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.hostFG)).
		Background(lipgloss.Color(theme.panelBG)).
		Bold(true)
}

func pickerBorderStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(mixHex(theme.hostBG, theme.hostFG, 0.42))).
		Background(lipgloss.Color(mixHex(theme.hostBG, theme.hostFG, 0.06)))
}

func pickerFooterStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelMuted)).
		Background(lipgloss.Color(mixHex(theme.hostBG, theme.hostFG, 0.09)))
}

func pickerLineStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.hostFG)).
		Background(lipgloss.Color(mixHex(theme.hostBG, theme.hostFG, 0.06)))
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

func overlayCardFillStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.hostFG)).
		Background(lipgloss.Color(mixHex(theme.hostBG, theme.hostFG, 0.06)))
}

func promptFieldMarkerStyle(theme uiTheme, _ bool) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelMuted))
}

func promptFieldLabelStyle(theme uiTheme, _ bool) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelMuted)).
		Bold(true)
}

func promptFieldValueStyle(theme uiTheme, active bool) lipgloss.Style {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.hostFG))
	if active {
		style = style.Underline(true)
	}
	return style
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
