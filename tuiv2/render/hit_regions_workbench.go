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

type tabBarSlotLayout struct {
	Slot ChromeSlotID
	Rect workbench.Rect
}

type statusBarLayout struct {
	LeftText    string
	RightText   string
	TokenRects  []workbench.Rect
	RightTokens []RenderStatusToken
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
	slotOrder      []ChromeSlotID
	slotRects      []tabBarSlotLayout
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

type tabBarActionLayout struct {
	Kind   HitRegionKind
	Label  string
	Rect   workbench.Rect
	Action input.SemanticAction
	Active bool
}

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
		slotOrder:     append([]ChromeSlotID(nil), normalizeUIChromeConfig(vm.Chrome).TabBar.Left...),
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
	layout.createLabel = "\uf067" // nf-fa-plus

	maxLeftWidth := vm.TermSize.Width - xansi.StringWidth(layout.rightText)
	if maxLeftWidth < 0 {
		maxLeftWidth = 0
	}

	x := 0
	sepWidth := xansi.StringWidth(renderTabSeparator())
	layout.tabs = make([]tabBarItemLayout, 0, len(vm.Workbench.Tabs))
	layout.actions = make([]tabBarActionLayout, 0, 8)
	layout.slotRects = nil
	for _, slot := range layout.slotOrder {
		slotStart := x
		slotPlaced := false
		switch slot {
		case SlotTabWorkspace:
			workspaceWidth := xansi.StringWidth(renderWorkspaceToken(layout.workspaceLabel, layout.palette))
			if workspaceWidth > maxLeftWidth || x+workspaceWidth > maxLeftWidth {
				if x == 0 {
					return layout
				}
				continue
			}
			layout.workspaceRect = workbench.Rect{X: x, Y: 0, W: workspaceWidth, H: 1}
			x += workspaceWidth
			slotPlaced = true
		case SlotTabTabs:
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
				slotPlaced = true
			}
		case SlotTabCreate:
			createWidth := xansi.StringWidth(renderTabCreateToken(layout.createLabel, layout.palette))
			if x+sepWidth+createWidth <= maxLeftWidth-tabBarCreateReserve {
				layout.createRect = workbench.Rect{X: x + sepWidth, Y: 0, W: createWidth, H: 1}
				x = layout.createRect.X + createWidth
				slotPlaced = true
			}
		case SlotTabActions:
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
				slotPlaced = true
			}
		}
		if slotPlaced {
			layout.slotRects = append(layout.slotRects, tabBarSlotLayout{Slot: slot, Rect: workbench.Rect{X: slotStart, Y: 0, W: x - slotStart, H: 1}})
		}
	}
	return layout
}

func buildTabBarLayout(state VisibleRenderState) tabBarLayout {
	layout := tabBarLayout{
		fallbackLabel: "[tuiv2]",
		rightText:     tabBarRightText(state),
		palette:       tabBarPaletteForState(state),
		slotOrder:     append([]ChromeSlotID(nil), normalizeUIChromeConfig(state.Chrome).TabBar.Left...),
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
	layout.createLabel = "\uf067" // nf-fa-plus

	maxLeftWidth := state.TermSize.Width - xansi.StringWidth(layout.rightText)
	if maxLeftWidth < 0 {
		maxLeftWidth = 0
	}

	x := 0
	sepWidth := xansi.StringWidth(renderTabSeparator())
	layout.tabs = make([]tabBarItemLayout, 0, len(state.Workbench.Tabs))
	layout.actions = make([]tabBarActionLayout, 0, 8)
	layout.slotRects = nil
	for _, slot := range layout.slotOrder {
		slotStart := x
		slotPlaced := false
		switch slot {
		case SlotTabWorkspace:
			workspaceWidth := xansi.StringWidth(renderWorkspaceToken(layout.workspaceLabel, layout.palette))
			if workspaceWidth > maxLeftWidth || x+workspaceWidth > maxLeftWidth {
				if x == 0 {
					return layout
				}
				continue
			}
			layout.workspaceRect = workbench.Rect{X: x, Y: 0, W: workspaceWidth, H: 1}
			x += workspaceWidth
			slotPlaced = true
		case SlotTabTabs:
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
				slotPlaced = true
			}
		case SlotTabCreate:
			createWidth := xansi.StringWidth(renderTabCreateToken(layout.createLabel, layout.palette))
			if x+sepWidth+createWidth <= maxLeftWidth-tabBarCreateReserve {
				layout.createRect = workbench.Rect{X: x + sepWidth, Y: 0, W: createWidth, H: 1}
				x = layout.createRect.X + createWidth
				slotPlaced = true
			}
		case SlotTabActions:
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
				slotPlaced = true
			}
		}
		if slotPlaced {
			layout.slotRects = append(layout.slotRects, tabBarSlotLayout{Slot: slot, Rect: workbench.Rect{X: slotStart, Y: 0, W: x - slotStart, H: 1}})
		}
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
	layout := buildStatusBarLayoutVM(vm)
	if len(layout.RightTokens) == 0 || len(layout.TokenRects) == 0 {
		return nil
	}
	regions := make([]HitRegion, 0, len(layout.RightTokens))
	rectIndex := 0
	for _, token := range layout.RightTokens {
		if strings.TrimSpace(token.Label) == "" {
			continue
		}
		if rectIndex >= len(layout.TokenRects) {
			break
		}
		rect := layout.TokenRects[rectIndex]
		rectIndex++
		if token.Action.Kind != "" {
			rect.Y = vm.TermSize.Height - 1
			regions = append(regions, HitRegion{
				Kind:   token.Kind,
				Rect:   rect,
				Action: token.Action,
			})
		}
	}
	return regions
}

func buildStatusBarLayoutVM(vm RenderVM) statusBarLayout {
	theme := uiThemeForVM(vm)
	width := vm.TermSize.Width
	cfg := normalizeUIChromeConfig(vm.Chrome)
	labels := currentStatusTextsVM(vm)
	leftClusters := make(map[ChromeSlotID]string, 2)
	if !suppressStatusHintsVM(vm) {
		mode := strings.TrimSpace(vm.Status.InputMode)
		if mode == "" || mode == "normal" {
			var parts []string
			parts = append(parts, renderDesktopHint(theme, "Ctrl", theme.hintKeyFG))
			rootColors := rootStatusHintColors(theme)
			for i, label := range labels {
				if i >= len(rootColors) {
					break
				}
				parts = append(parts, renderStatusSep(theme))
				parts = append(parts, renderDesktopHint(theme, label, rootColors[i]))
			}
			leftClusters[SlotStatusHints] = strings.Join(parts, "")
		} else {
			leftClusters[SlotStatusMode] = renderModeBadge(theme, mode)
			leftClusters[SlotStatusHints] = strings.Join(renderModeHints(theme, mode, labels), "")
		}
	}
	var leftParts []string
	for _, slot := range cfg.StatusBar.Left {
		if part := leftClusters[slot]; strings.TrimSpace(part) != "" {
			leftParts = append(leftParts, part)
		}
	}
	left := strings.Join(leftParts, "")

	tokens := statusBarRightTokensVM(vm)
	rightText := ""
	rightTokens := make([]RenderStatusToken, 0, len(tokens))
	if chromeSlotEnabled(cfg.StatusBar.Right, SlotStatusTokens) {
		rightText = renderStatusBarRight(theme, tokens)
		rightTokens = append(rightTokens, tokens...)
	}
	if rightText != "" && xansi.StringWidth(left)+1+xansi.StringWidth(rightText) > width {
		rightText = ""
		rightTokens = nil
	}
	layout := statusBarLayout{LeftText: left, RightText: rightText, RightTokens: rightTokens}
	layout.TokenRects = statusBarTokenRects(width, rightTokens)
	return layout
}

func buildStatusBarLayout(state VisibleRenderState) statusBarLayout {
	theme := uiThemeForState(state)
	width := state.TermSize.Width
	cfg := normalizeUIChromeConfig(state.Chrome)
	labels := currentStatusTexts(state)
	leftClusters := make(map[ChromeSlotID]string, 2)
	if !suppressStatusHints(state) {
		mode := strings.TrimSpace(state.InputMode)
		if mode == "" || mode == "normal" {
			var parts []string
			parts = append(parts, renderDesktopHint(theme, "Ctrl", theme.hintKeyFG))
			rootColors := rootStatusHintColors(theme)
			for i, label := range labels {
				if i >= len(rootColors) {
					break
				}
				parts = append(parts, renderStatusSep(theme))
				parts = append(parts, renderDesktopHint(theme, label, rootColors[i]))
			}
			leftClusters[SlotStatusHints] = strings.Join(parts, "")
		} else {
			leftClusters[SlotStatusMode] = renderModeBadge(theme, mode)
			leftClusters[SlotStatusHints] = strings.Join(renderModeHints(theme, mode, labels), "")
		}
	}
	var leftParts []string
	for _, slot := range cfg.StatusBar.Left {
		if part := leftClusters[slot]; strings.TrimSpace(part) != "" {
			leftParts = append(leftParts, part)
		}
	}
	left := strings.Join(leftParts, "")

	tokens := statusBarRightTokens(state)
	rightText := ""
	rightTokens := make([]RenderStatusToken, 0, len(tokens))
	if chromeSlotEnabled(cfg.StatusBar.Right, SlotStatusTokens) {
		rightText = renderStatusBarRight(theme, tokens)
		rightTokens = append(rightTokens, tokens...)
	}
	if rightText != "" && xansi.StringWidth(left)+1+xansi.StringWidth(rightText) > width {
		rightText = ""
		rightTokens = nil
	}
	layout := statusBarLayout{LeftText: left, RightText: rightText, RightTokens: rightTokens}
	layout.TokenRects = statusBarTokenRects(width, rightTokens)
	return layout
}

func statusBarTokenRects(width int, tokens []RenderStatusToken) []workbench.Rect {
	if len(tokens) == 0 {
		return nil
	}
	labelWidths := make([]int, 0, len(tokens))
	totalWidth := 0
	visibleCount := 0
	for _, token := range tokens {
		if strings.TrimSpace(token.Label) == "" {
			continue
		}
		w := xansi.StringWidth(token.Label)
		labelWidths = append(labelWidths, w)
		totalWidth += w
		visibleCount++
	}
	if totalWidth == 0 {
		return nil
	}
	totalWidth += maxInt(0, visibleCount-1)
	x := maxInt(0, width-totalWidth)
	rects := make([]workbench.Rect, 0, visibleCount)
	widthIndex := 0
	for _, token := range tokens {
		if strings.TrimSpace(token.Label) == "" {
			continue
		}
		w := labelWidths[widthIndex]
		widthIndex++
		rects = append(rects, workbench.Rect{X: x, Y: 0, W: w, H: 1})
		x += w + 1
	}
	return rects
}

func renderTabBarLeft(layout tabBarLayout) string {
	if layout.fallbackLabel != "" {
		return layout.fallbackLabel
	}

	var builder strings.Builder
	for _, slot := range layout.slotOrder {
		switch slot {
		case SlotTabWorkspace:
			if layout.workspaceRect.W > 0 {
				builder.WriteString(renderWorkspaceToken(layout.workspaceLabel, layout.palette))
			}
		case SlotTabTabs:
			for _, tab := range layout.tabs {
				builder.WriteString(renderTabSeparatorWithBG(layout.palette.barBG))
				builder.WriteString(renderTabSwitchToken(tab.TabIndex+1, tab.Label, tab.Active, layout.palette))
				builder.WriteString(renderTabCloseToken(tab.Active, layout.palette))
			}
		case SlotTabCreate:
			if layout.createRect.W > 0 {
				builder.WriteString(renderTabSeparatorWithBG(layout.palette.barBG))
				builder.WriteString(renderTabCreateToken(layout.createLabel, layout.palette))
			}
		case SlotTabActions:
			for _, action := range layout.actions {
				builder.WriteString(renderTabSeparatorWithBG(layout.palette.barBG))
				builder.WriteString(renderTopBarActionTokenWithPalette(action.Label, action.Active, layout.palette))
			}
		}
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
	theme := uiThemeForVM(vm)
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
		Render("\uf120 " + label) // nf-fa-terminal icon prefix
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
		Render(" " + label + " ")
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
	theme := uiThemeForVM(vm)
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
