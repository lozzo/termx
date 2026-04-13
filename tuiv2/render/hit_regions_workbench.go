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
	HitRegionTabRename        HitRegionKind = "tab-rename"
	HitRegionTabKill          HitRegionKind = "tab-kill"
	HitRegionWorkspacePrev    HitRegionKind = "workspace-prev"
	HitRegionWorkspaceNext    HitRegionKind = "workspace-next"
	HitRegionWorkspaceCreate  HitRegionKind = "workspace-create"
	HitRegionWorkspaceRename  HitRegionKind = "workspace-rename"
	HitRegionWorkspaceDelete  HitRegionKind = "workspace-delete"
	HitRegionFloatingOverview HitRegionKind = "floating-overview"
)

type tabBarActionSpec struct {
	Kind   HitRegionKind
	Label  string
	Action input.SemanticAction
	Active bool
}

type tabBarPalette struct {
	barBG       string
	workspaceFG string
	workspaceBG string
	activeFG    string
	activeBG    string
	inactiveFG  string
	inactiveBG  string
	createFG    string
	createBG    string
	accent      string
	danger      string
	actionFG    string
	actionBG    string
	actionOnFG  string
	actionOnBG  string
}

func buildTabBarLayoutVM(vm RenderVM) tabBarLayout {
	layout := tabBarLayout{
		fallbackLabel: "[tuiv2]",
		rightText:     tabBarRightTextVM(vm),
		palette:       tabBarPaletteForVM(vm),
	}
	if vm.Workbench == nil {
		return layout
	}

	layout.fallbackLabel = ""
	workspaceName := strings.TrimSpace(vm.Workbench.WorkspaceName)
	if workspaceName == "" {
		workspaceName = "workspace"
	}
	layout.workspaceLabel = workspaceName
	layout.createLabel = "+"

	maxLeftWidth := vm.TermSize.Width - xansi.StringWidth(layout.rightText)
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
	layout.tabs = make([]tabBarItemLayout, 0, len(vm.Workbench.Tabs))
	for i, tab := range vm.Workbench.Tabs {
		name := strings.TrimSpace(tab.Name)
		if name == "" {
			name = fmt.Sprintf("tab %d", i+1)
		}
		active := i == vm.Workbench.ActiveTab
		switchWidth := xansi.StringWidth(renderTabSwitchToken(i+1, name, active, layout.palette))
		closeWidth := xansi.StringWidth(renderTabCloseToken(active, layout.palette))
		totalWidth := sepWidth + switchWidth + closeWidth
		if x+totalWidth > maxLeftWidth {
			break
		}

		x += sepWidth
		item := tabBarItemLayout{
			Label:    name,
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
	for _, spec := range tabBarActionSpecsVM(vm) {
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

func buildTabBarLayout(state VisibleRenderState) tabBarLayout {
	layout := tabBarLayout{
		fallbackLabel: "[tuiv2]",
		rightText:     tabBarRightText(state),
		palette:       tabBarPaletteForState(state),
	}
	if state.Workbench == nil {
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
		active := i == state.Workbench.ActiveTab
		switchWidth := xansi.StringWidth(renderTabSwitchToken(i+1, name, active, layout.palette))
		closeWidth := xansi.StringWidth(renderTabCloseToken(active, layout.palette))
		totalWidth := sepWidth + switchWidth + closeWidth
		if x+totalWidth > maxLeftWidth {
			break
		}

		x += sepWidth
		item := tabBarItemLayout{
			Label:    name,
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

func tabBarActionSpecsVM(vm RenderVM) []tabBarActionSpec {
	return nil
}

func TabBarHitRegions(vm RenderVM) []HitRegion {
	layout := buildTabBarLayoutVM(vm)
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

func StatusBarHitRegions(vm RenderVM) []HitRegion {
	if vm.TermSize.Width <= 0 || vm.TermSize.Height <= 0 {
		return nil
	}
	tokens := statusBarRightTokensVM(vm)
	if len(tokens) == 0 {
		return nil
	}
	labelWidths := make([]int, 0, len(tokens))
	totalWidth := 0
	for _, token := range tokens {
		if strings.TrimSpace(token.Label) == "" {
			continue
		}
		w := xansi.StringWidth(token.Label)
		labelWidths = append(labelWidths, w)
		totalWidth += w
	}
	if totalWidth == 0 {
		return nil
	}
	totalWidth += maxInt(0, len(labelWidths)-1)
	x := maxInt(0, vm.TermSize.Width-totalWidth)
	regions := make([]HitRegion, 0, len(tokens))
	widthIndex := 0
	for _, token := range tokens {
		if strings.TrimSpace(token.Label) == "" {
			continue
		}
		w := labelWidths[widthIndex]
		widthIndex++
		if token.Action.Kind != "" {
			regions = append(regions, HitRegion{
				Kind:   token.Kind,
				Rect:   workbench.Rect{X: x, Y: vm.TermSize.Height - 1, W: w, H: 1},
				Action: token.Action,
			})
		}
		x += w + 1
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
		builder.WriteString(renderTabSeparatorWithBG(layout.palette.barBG))
		builder.WriteString(renderTabSwitchToken(tab.TabIndex+1, tab.Label, tab.Active, layout.palette))
		builder.WriteString(renderTabCloseToken(tab.Active, layout.palette))
	}
	if layout.createRect.W > 0 {
		builder.WriteString(renderTabSeparatorWithBG(layout.palette.barBG))
		builder.WriteString(renderTabCreateToken(layout.createLabel, layout.palette))
	}
	for _, slot := range layout.actions {
		builder.WriteString(renderTabSeparatorWithBG(layout.palette.barBG))
		builder.WriteString(renderTopBarActionTokenWithPalette(slot.Label, slot.Active, layout.palette))
	}
	return builder.String()
}

func tabBarRightText(state VisibleRenderState) string {
	theme := uiThemeForState(state)
	var rightParts []string
	if state.Error != "" {
		rightParts = append(rightParts, statusPartErrorStyle(theme).Render(state.Error))
	} else if state.Notice != "" {
		rightParts = append(rightParts, statusPartNoticeStyle(theme).Render(state.Notice))
	}
	return strings.Join(rightParts, "  ")
}

func tabBarRightTextVM(vm RenderVM) string {
	theme := uiThemeForRuntime(vm.Runtime)
	var rightParts []string
	if vm.Status.Error != "" {
		rightParts = append(rightParts, statusPartErrorStyle(theme).Render(vm.Status.Error))
	} else if vm.Status.Notice != "" {
		rightParts = append(rightParts, statusPartNoticeStyle(theme).Render(vm.Status.Notice))
	}
	return strings.Join(rightParts, "  ")
}

func renderWorkspaceToken(label string, palette tabBarPalette) string {
	return workspaceLabelStyle(defaultUITheme()).
		Foreground(lipgloss.Color(palette.workspaceFG)).
		Background(lipgloss.Color(palette.workspaceBG)).
		Render(label)
}

func renderTabSeparator() string {
	return renderTabSeparatorWithBG("")
}

func renderTabSeparatorWithBG(bg string) string {
	if strings.TrimSpace(bg) == "" {
		bg = defaultUITheme().chromeBG
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(bg)).Render(" ")
}

func renderTabSwitchToken(index int, label string, active bool, palette tabBarPalette) string {
	indexText := strconv.Itoa(index)
	if active {
		indexStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(palette.accent)).
			Background(lipgloss.Color(palette.activeBG))
		return lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(palette.accent)).
			Background(lipgloss.Color(palette.activeBG)).
			Render("▎") +
			indexStyle.Render(" "+indexText+" ") +
			tabActiveStyle(defaultUITheme()).
				Foreground(lipgloss.Color(palette.activeFG)).
				Background(lipgloss.Color(palette.activeBG)).
				Render(label+" ")
	}
	indexColor := mixHex(palette.inactiveFG, palette.accent, 0.22)
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(indexColor)).
		Background(lipgloss.Color(palette.inactiveBG)).
		Render(" "+indexText+" ") +
		tabInactiveStyle(defaultUITheme()).
			Foreground(lipgloss.Color(palette.inactiveFG)).
			Background(lipgloss.Color(palette.inactiveBG)).
			Render(label+" ")
}

func renderTabCloseToken(active bool, palette tabBarPalette) string {
	if active {
		return tabCloseStyle(defaultUITheme(), true).
			Foreground(lipgloss.Color(palette.danger)).
			Background(lipgloss.Color(palette.activeBG)).
			Render(" " + paneCloseIcon() + " ")
	}
	return tabCloseStyle(defaultUITheme(), false).
		Foreground(lipgloss.Color(palette.inactiveFG)).
		Background(lipgloss.Color(palette.inactiveBG)).
		Render(" " + paneCloseIcon() + " ")
}

func renderTabCreateToken(label string, palette tabBarPalette) string {
	return tabCreateStyle(defaultUITheme()).
		Foreground(lipgloss.Color(palette.createFG)).
		Background(lipgloss.Color(palette.createBG)).
		Render("[" + label + "]")
}

func renderTopBarActionToken(label string, active bool) string {
	return renderTopBarActionTokenWithPalette(label, active, tabBarPalette{})
}

func renderTopBarActionTokenWithPalette(label string, active bool, palette tabBarPalette) string {
	if active {
		bg := palette.actionOnBG
		fg := palette.actionOnFG
		if bg == "" {
			theme := defaultUITheme()
			bg = theme.tabActionOnBG
			fg = theme.tabActionOnFG
		}
		return tabActionActiveStyle(defaultUITheme()).
			Foreground(lipgloss.Color(fg)).
			Background(lipgloss.Color(bg)).
			Render(label)
	}
	bg := palette.actionBG
	fg := palette.actionFG
	if bg == "" {
		theme := defaultUITheme()
		bg = theme.tabActionBG
		fg = theme.tabActionFG
	}
	return tabActionStyle(defaultUITheme()).
		Foreground(lipgloss.Color(fg)).
		Background(lipgloss.Color(bg)).
		Render(label)
}

func tabBarPaletteForState(state VisibleRenderState) tabBarPalette {
	theme := uiThemeForState(state)
	return tabBarPalette{
		barBG:       theme.chromeBG,
		workspaceFG: theme.tabWorkspaceFG,
		workspaceBG: theme.tabWorkspaceBG,
		activeFG:    theme.tabActiveFG,
		activeBG:    theme.tabActiveBG,
		inactiveFG:  theme.tabInactiveFG,
		inactiveBG:  theme.tabInactiveBG,
		createFG:    theme.tabCreateFG,
		createBG:    theme.tabCreateBG,
		accent:      theme.chromeAccent,
		danger:      theme.danger,
		actionFG:    theme.tabActionFG,
		actionBG:    theme.tabActionBG,
		actionOnFG:  theme.tabActionOnFG,
		actionOnBG:  theme.tabActionOnBG,
	}
}

func tabBarPaletteForVM(vm RenderVM) tabBarPalette {
	theme := uiThemeForRuntime(vm.Runtime)
	return tabBarPalette{
		barBG:       theme.chromeBG,
		workspaceFG: theme.tabWorkspaceFG,
		workspaceBG: theme.tabWorkspaceBG,
		activeFG:    theme.tabActiveFG,
		activeBG:    theme.tabActiveBG,
		inactiveFG:  theme.tabInactiveFG,
		inactiveBG:  theme.tabInactiveBG,
		createFG:    theme.tabCreateFG,
		createBG:    theme.tabCreateBG,
		accent:      theme.chromeAccent,
		danger:      theme.danger,
		actionFG:    theme.tabActionFG,
		actionBG:    theme.tabActionBG,
		actionOnFG:  theme.tabActionOnFG,
		actionOnBG:  theme.tabActionOnBG,
	}
}
