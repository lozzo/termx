package render

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type tabBarItemLayout struct {
	Label     string
	Rect      workbench.Rect
	CloseRect workbench.Rect
	Active    bool
	TabIndex  int
	TabID     string
}

type tabBarActionLayout struct {
	Kind   HitRegionKind
	Label  string
	Rect   workbench.Rect
	Action input.SemanticAction
	Active bool
}

type tabBarLayout struct {
	fallbackLabel  string
	rightText      string
	workspaceLabel string
	workspaceRect  workbench.Rect
	palette        tabBarPalette
	tabs           []tabBarItemLayout
	createLabel    string
	createRect     workbench.Rect
	actions        []tabBarActionLayout
}

const (
	tabBarCreateReserve = 6
	tabBarActionReserve = 6
)

const (
	HitRegionTabRename       HitRegionKind = "tab-rename"
	HitRegionTabKill         HitRegionKind = "tab-kill"
	HitRegionWorkspacePrev   HitRegionKind = "workspace-prev"
	HitRegionWorkspaceNext   HitRegionKind = "workspace-next"
	HitRegionWorkspaceCreate HitRegionKind = "workspace-create"
	HitRegionWorkspaceRename HitRegionKind = "workspace-rename"
	HitRegionWorkspaceDelete HitRegionKind = "workspace-delete"
)

type tabBarActionSpec struct {
	Kind   HitRegionKind
	Label  string
	Action input.SemanticAction
	Active bool
}

type tabBarPalette struct {
	workspaceFG string
	workspaceBG string
	activeFG    string
	activeBG    string
	inactiveFG  string
	inactiveBG  string
	createFG    string
	createBG    string
	accent      string
}

func buildTabBarLayout(state VisibleRenderState) tabBarLayout {
	layout := tabBarLayout{
		fallbackLabel: "[tuiv2]",
		rightText:     tabBarRightText(state),
		palette:       tabBarPaletteForState(state),
	}
	if state.Workbench == nil || len(state.Workbench.Tabs) == 0 {
		return layout
	}

	layout.fallbackLabel = ""
	workspaceName := strings.TrimSpace(state.Workbench.WorkspaceName)
	if workspaceName == "" {
		workspaceName = "workspace"
	}
	layout.workspaceLabel = workspaceName
	layout.createLabel = "+"

	maxLeftWidth := state.TermSize.Width - xansi.StringWidth(layout.rightText)
	if maxLeftWidth < 0 {
		maxLeftWidth = 0
	}

	workspaceWidth := xansi.StringWidth(renderWorkspaceToken(layout.workspaceLabel, layout.palette))
	if workspaceWidth > maxLeftWidth {
		return layout
	}
	layout.workspaceRect = workbench.Rect{X: 0, Y: 0, W: workspaceWidth, H: 1}

	x := workspaceWidth
	sepWidth := xansi.StringWidth(renderTabSeparator())
	layout.tabs = make([]tabBarItemLayout, 0, len(state.Workbench.Tabs))
	for i, tab := range state.Workbench.Tabs {
		name := strings.TrimSpace(tab.Name)
		if name == "" {
			name = fmt.Sprintf("tab %d", i+1)
		}
		label := name
		active := i == state.Workbench.ActiveTab
		switchWidth := xansi.StringWidth(renderTabSwitchToken(label, active, layout.palette))
		closeWidth := xansi.StringWidth(renderTabCloseToken(active, layout.palette))
		totalWidth := sepWidth + switchWidth + closeWidth
		if x+totalWidth > maxLeftWidth {
			break
		}

		x += sepWidth
		item := tabBarItemLayout{
			Label:    label,
			Rect:     workbench.Rect{X: x, Y: 0, W: switchWidth, H: 1},
			Active:   active,
			TabIndex: i,
			TabID:    tab.ID,
		}
		x += switchWidth
		item.CloseRect = workbench.Rect{X: x, Y: 0, W: closeWidth, H: 1}
		x += closeWidth
		layout.tabs = append(layout.tabs, item)
	}

	createWidth := xansi.StringWidth(renderTabCreateToken(layout.createLabel, layout.palette))
	if x+sepWidth+createWidth <= maxLeftWidth-tabBarCreateReserve {
		layout.createRect = workbench.Rect{
			X: x + sepWidth,
			Y: 0,
			W: createWidth,
			H: 1,
		}
		x = layout.createRect.X + createWidth
	}

	layout.actions = make([]tabBarActionLayout, 0, 8)
	for _, spec := range tabBarActionSpecs(state) {
		slotWidth := xansi.StringWidth(renderTopBarActionToken(spec.Label, spec.Active))
		if x+sepWidth+slotWidth > maxLeftWidth-tabBarActionReserve {
			break
		}
		rect := workbench.Rect{X: x + sepWidth, Y: 0, W: slotWidth, H: 1}
		layout.actions = append(layout.actions, tabBarActionLayout{
			Kind:   spec.Kind,
			Label:  spec.Label,
			Rect:   rect,
			Action: spec.Action,
			Active: spec.Active,
		})
		x = rect.X + rect.W
	}
	return layout
}

func tabBarActionSpecs(state VisibleRenderState) []tabBarActionSpec {
	return nil
}

func TabBarHitRegions(state VisibleRenderState) []HitRegion {
	layout := buildTabBarLayout(state)
	regions := make([]HitRegion, 0, len(layout.tabs)*2+2+len(layout.actions))
	if layout.workspaceRect.W > 0 {
		regions = append(regions, HitRegion{
			Kind:   HitRegionWorkspaceLabel,
			Rect:   layout.workspaceRect,
			Action: input.SemanticAction{Kind: input.ActionOpenWorkspacePicker},
		})
	}
	for _, tab := range layout.tabs {
		regions = append(regions, HitRegion{
			Kind:     HitRegionTabSwitch,
			Rect:     tab.Rect,
			TabIndex: tab.TabIndex,
			Action: input.SemanticAction{
				Kind:  input.ActionJumpTab,
				TabID: tab.TabID,
				Text:  strconv.Itoa(tab.TabIndex + 1),
			},
		})
		regions = append(regions, HitRegion{
			Kind:     HitRegionTabClose,
			Rect:     tab.CloseRect,
			TabIndex: tab.TabIndex,
			Action: input.SemanticAction{
				Kind:  input.ActionCloseTab,
				TabID: tab.TabID,
			},
		})
	}
	if layout.createRect.W > 0 {
		regions = append(regions, HitRegion{
			Kind: HitRegionTabCreate,
			Rect: layout.createRect,
			Action: input.SemanticAction{
				Kind: input.ActionCreateTab,
			},
		})
	}
	for _, slot := range layout.actions {
		regions = append(regions, HitRegion{
			Kind:   slot.Kind,
			Rect:   slot.Rect,
			Action: slot.Action,
		})
	}
	return regions
}

func renderTabBarLeft(layout tabBarLayout) string {
	if layout.fallbackLabel != "" {
		return layout.fallbackLabel
	}
	if layout.workspaceRect.W <= 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString(renderWorkspaceToken(layout.workspaceLabel, layout.palette))
	for _, tab := range layout.tabs {
		builder.WriteString(renderTabSeparator())
		builder.WriteString(renderTabSwitchToken(tab.Label, tab.Active, layout.palette))
		builder.WriteString(renderTabCloseToken(tab.Active, layout.palette))
	}
	if layout.createRect.W > 0 {
		builder.WriteString(renderTabSeparator())
		builder.WriteString(renderTabCreateToken(layout.createLabel, layout.palette))
	}
	for _, slot := range layout.actions {
		builder.WriteString(renderTabSeparator())
		builder.WriteString(renderTopBarActionToken(slot.Label, slot.Active))
	}
	return builder.String()
}

func tabBarRightText(state VisibleRenderState) string {
	var rightParts []string
	if state.Error != "" {
		rightParts = append(rightParts, statusPartErrorStyle.Padding(0, 1).Render(state.Error))
	} else if state.Notice != "" {
		rightParts = append(rightParts, statusPartNoticeStyle.Render(state.Notice))
	}
	return strings.Join(rightParts, "  ")
}

func renderWorkspaceToken(label string, palette tabBarPalette) string {
	return workspaceLabelStyle.
		Foreground(lipgloss.Color(palette.workspaceFG)).
		Background(lipgloss.Color(palette.workspaceBG)).
		Render(label)
}

func renderTabSeparator() string {
	return lipgloss.NewStyle().Background(tabBarBG).Render(" ")
}

func renderTabSwitchToken(label string, active bool, palette tabBarPalette) string {
	if active {
		return lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(palette.accent)).
			Background(lipgloss.Color(palette.activeBG)).
			Render("▎") +
			tabActiveStyle.
				Foreground(lipgloss.Color(palette.activeFG)).
				Background(lipgloss.Color(palette.activeBG)).
				Underline(false).
				Render(" "+label+" ")
	}
	return tabInactiveStyle.
		Foreground(lipgloss.Color(palette.inactiveFG)).
		Background(lipgloss.Color(palette.inactiveBG)).
		Render("  " + label + " ")
}

func renderTabCloseToken(active bool, palette tabBarPalette) string {
	if active {
		return tabCloseActiveStyle.
			Foreground(lipgloss.Color(palette.activeFG)).
			Background(lipgloss.Color(palette.activeBG)).
			Underline(false).
			Render("   ")
	}
	return tabCloseStyle.
		Foreground(lipgloss.Color(palette.inactiveFG)).
		Background(lipgloss.Color(palette.inactiveBG)).
		Render("   ")
}

func renderTabCreateToken(label string, palette tabBarPalette) string {
	return tabCreateStyle.
		Foreground(lipgloss.Color(palette.createFG)).
		Background(lipgloss.Color(palette.createBG)).
		Render(" " + label + " ")
}

func renderTopBarActionToken(label string, active bool) string {
	if active {
		return tabActionActiveStyle.Render(label)
	}
	return tabActionStyle.Render(label)
}

func tabBarPaletteForState(state VisibleRenderState) tabBarPalette {
	activeBG := ""
	if state.Runtime != nil {
		activeBG = strings.TrimSpace(state.Runtime.HostDefaultBG)
	}
	if !isHexColor(activeBG) {
		activeBG = "#000000"
	}
	return tabBarPalette{
		workspaceFG: contrastTextColor("#182033"),
		workspaceBG: "#182033",
		activeFG:    contrastTextColor(activeBG),
		activeBG:    activeBG,
		inactiveFG:  mixHex(contrastTextColor(activeBG), activeBG, 0.62),
		inactiveBG:  mixHex(activeBG, "#64748b", 0.34),
		createFG:    "#ecfeff",
		createBG:    mixHex(activeBG, "#0f766e", 0.82),
		accent:      "#8b5cf6",
	}
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
