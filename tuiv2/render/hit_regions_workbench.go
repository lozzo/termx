package render

import (
	"fmt"
	"strconv"
	"strings"

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
	tabs           []tabBarItemLayout
	createLabel    string
	createRect     workbench.Rect
	actions        []tabBarActionLayout
}

const (
	HitRegionTabRename         HitRegionKind = "tab-rename"
	HitRegionTabKill           HitRegionKind = "tab-kill"
	HitRegionWorkspacePrev     HitRegionKind = "workspace-prev"
	HitRegionWorkspaceNext     HitRegionKind = "workspace-next"
	HitRegionWorkspaceCreate   HitRegionKind = "workspace-create"
	HitRegionWorkspaceRename   HitRegionKind = "workspace-rename"
	HitRegionWorkspaceDelete   HitRegionKind = "workspace-delete"
)

type tabBarActionSpec struct {
	Kind   HitRegionKind
	Label  string
	Action input.SemanticAction
	Active bool
}

func buildTabBarLayout(state VisibleRenderState) tabBarLayout {
	layout := tabBarLayout{
		fallbackLabel: "[tuiv2]",
		rightText:     tabBarRightText(state),
	}
	if state.Workbench == nil || len(state.Workbench.Tabs) == 0 {
		return layout
	}

	layout.fallbackLabel = ""
	workspaceName := strings.TrimSpace(state.Workbench.WorkspaceName)
	if workspaceName == "" {
		workspaceName = "workspace"
	}
	layout.workspaceLabel = "[" + workspaceName + "]"
	layout.createLabel = "[+]"

	maxLeftWidth := state.TermSize.Width - xansi.StringWidth(layout.rightText)
	if maxLeftWidth < 0 {
		maxLeftWidth = 0
	}

	workspaceWidth := xansi.StringWidth(renderWorkspaceToken(layout.workspaceLabel))
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
		label := fmt.Sprintf("[%d:%s]", i+1, name)
		active := i == state.Workbench.ActiveTab
		switchWidth := xansi.StringWidth(renderTabSwitchToken(label, active))
		closeWidth := xansi.StringWidth(renderTabCloseToken(active))
		totalWidth := sepWidth + switchWidth + sepWidth + closeWidth
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
		x += sepWidth
		item.CloseRect = workbench.Rect{X: x, Y: 0, W: closeWidth, H: 1}
		x += closeWidth
		layout.tabs = append(layout.tabs, item)
	}

	createWidth := xansi.StringWidth(renderTabCreateToken(layout.createLabel))
	if x+sepWidth+createWidth <= maxLeftWidth {
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
		if x+sepWidth+slotWidth > maxLeftWidth {
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
	if state.Workbench == nil {
		return nil
	}
	specs := make([]tabBarActionSpec, 0, 7)
	activeTabID := ""
	if state.Workbench.ActiveTab >= 0 && state.Workbench.ActiveTab < len(state.Workbench.Tabs) {
		activeTabID = state.Workbench.Tabs[state.Workbench.ActiveTab].ID
	}
	if activeTabID != "" {
		specs = append(specs,
			tabBarActionSpec{
				Kind:   HitRegionTabRename,
				Label:  "[tr]",
				Active: true,
				Action: input.SemanticAction{Kind: input.ActionRenameTab, TabID: activeTabID},
			},
			tabBarActionSpec{
				Kind:   HitRegionTabKill,
				Label:  "[tx]",
				Active: true,
				Action: input.SemanticAction{Kind: input.ActionKillTab, TabID: activeTabID},
			},
		)
	}
	specs = append(specs,
		tabBarActionSpec{
			Kind:   HitRegionWorkspacePrev,
			Label:  "[w<]",
			Action: input.SemanticAction{Kind: input.ActionPrevWorkspace},
		},
		tabBarActionSpec{
			Kind:   HitRegionWorkspaceNext,
			Label:  "[w>]",
			Action: input.SemanticAction{Kind: input.ActionNextWorkspace},
		},
		tabBarActionSpec{
			Kind:   HitRegionWorkspaceCreate,
			Label:  "[w+]",
			Action: input.SemanticAction{Kind: input.ActionCreateWorkspace},
		},
		tabBarActionSpec{
			Kind:   HitRegionWorkspaceRename,
			Label:  "[wr]",
			Action: input.SemanticAction{Kind: input.ActionRenameWorkspace},
		},
		tabBarActionSpec{
			Kind:   HitRegionWorkspaceDelete,
			Label:  "[wx]",
			Action: input.SemanticAction{Kind: input.ActionDeleteWorkspace},
		},
	)
	return specs
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
	builder.WriteString(renderWorkspaceToken(layout.workspaceLabel))
	for _, tab := range layout.tabs {
		builder.WriteString(renderTabSeparator())
		builder.WriteString(renderTabSwitchToken(tab.Label, tab.Active))
		builder.WriteString(renderTabSeparator())
		builder.WriteString(renderTabCloseToken(tab.Active))
	}
	if layout.createRect.W > 0 {
		builder.WriteString(renderTabSeparator())
		builder.WriteString(renderTabCreateToken(layout.createLabel))
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

func renderWorkspaceToken(label string) string {
	return workspaceLabelStyle.Render(label)
}

func renderTabSeparator() string {
	return tabInactiveStyle.Render(" ")
}

func renderTabSwitchToken(label string, active bool) string {
	if active {
		return tabActiveStyle.Render(label)
	}
	return tabInactiveStyle.Render(label)
}

func renderTabCloseToken(active bool) string {
	if active {
		return tabActiveStyle.Render("[x]")
	}
	return tabInactiveStyle.Render("[x]")
}

func renderTabCreateToken(label string) string {
	return tabInactiveStyle.Render(label)
}

func renderTopBarActionToken(label string, active bool) string {
	if active {
		return tabActiveStyle.Render(label)
	}
	return tabInactiveStyle.Render(label)
}
