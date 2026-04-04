package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
)

var (
	tabBarBG = lipgloss.Color("#020617")

	workspaceLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#e2e8f0")).
				Background(lipgloss.Color("#0f172a")).
				Padding(0, 1)

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#94a3b8")).
				Background(tabBarBG)

	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Underline(true).
			Foreground(lipgloss.Color("#e2e8f0")).
			Background(tabBarBG)

	statusBarBG = lipgloss.Color("#020617")

	statusChipStyle = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1)

	statusSeparatorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#64748b")).
				Background(statusBarBG).
				Bold(true)

	statusPartDefaultStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#cbd5e1")).
				Background(statusBarBG)

	statusPartErrorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#fee2e2")).
				Background(lipgloss.Color("#7f1d1d")).
				Bold(true)

	statusPartNoticeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#e0f2fe")).
				Background(lipgloss.Color("#0f766e")).
				Bold(true).
				Padding(0, 1)
)

func renderTabBar(state VisibleRenderState) string {
	layout := buildTabBarLayout(state)
	return fillLine(renderTabBarLeft(layout), layout.rightText, state.TermSize.Width, tabBarBG)
}

func renderStatusBar(state VisibleRenderState) string {
	width := state.TermSize.Width
	labels := currentStatusTexts(state)

	// left: mode badge + shortcut hints
	var leftParts []string
	if !suppressStatusHints(state) {
		mode := strings.TrimSpace(state.InputMode)
		if mode == "" || mode == "normal" {
			leftParts = append(leftParts, renderStatusChip("Ctrl", "#020617", "#f8fafc"))
			rootColors := []string{"#86efac", "#fca5a5", "#93c5fd", "#fcd34d", "#fde047", "#c4b5fd", "#a7f3d0", "#67e8f9"}
			for i, label := range labels {
				if i >= len(rootColors) {
					break
				}
				leftParts = append(leftParts, renderStatusSep())
				leftParts = append(leftParts, renderStatusChip(label, rootColors[i], "#020617"))
			}
		} else {
			badge := renderModeBadge(mode)
			if badge != "" {
				leftParts = append(leftParts, badge)
			}
			leftParts = append(leftParts, renderModeHints(mode, labels)...)
		}
	}
	left := strings.Join(leftParts, "")

	// right: state summary
	var rightParts []string
	if state.Workbench != nil {
		rightParts = append(rightParts, "ws:"+state.Workbench.WorkspaceName)
	}
	if state.Runtime != nil {
		rightParts = append(rightParts, fmt.Sprintf("terminals:%d", len(state.Runtime.Terminals)))
	}
	right := statusPartDefaultStyle.Render(strings.Join(rightParts, "  "))

	return fillLine(left, right, width, statusBarBG)
}

func suppressStatusHints(state VisibleRenderState) bool {
	return false
}

func renderStatusChip(label, bg, fg string) string {
	return statusChipStyle.
		Foreground(lipgloss.Color(fg)).
		Background(lipgloss.Color(bg)).
		Render(label)
}

func renderStatusSep() string {
	return statusSeparatorStyle.Render(" \u25b8 ")
}

func renderModeBadge(mode string) string {
	label := strings.ToUpper(mode)
	bg := "#d1d5db"
	switch label {
	case "PANE":
		bg = "#86efac"
	case "RESIZE":
		bg = "#fca5a5"
	case "TAB":
		bg = "#93c5fd"
	case "WORKSPACE":
		bg = "#fcd34d"
	case "FLOATING":
		bg = "#fde047"
	case "DISPLAY":
		bg = "#c4b5fd"
	case "PICKER":
		bg = "#a7f3d0"
	case "PROMPT":
		bg = "#fdba74"
	case "HELP":
		bg = "#f9a8d4"
	case "WORKSPACE-PICKER":
		bg = "#fcd34d"
	case "GLOBAL":
		bg = "#67e8f9"
	case "TERMINAL-MANAGER":
		bg = "#67e8f9"
	}
	return renderStatusChip(label, bg, "#020617") + renderStatusSep()
}

func renderModeHints(mode string, labels []string) []string {
	modeKind := input.ModeKind(mode)
	bg := "#d1d5db"
	switch modeKind {
	case input.ModePane:
		bg = "#86efac"
	case input.ModeResize:
		bg = "#fca5a5"
	case input.ModeTab:
		bg = "#93c5fd"
	case input.ModeWorkspace:
		bg = "#fcd34d"
	case input.ModeFloating:
		bg = "#fde047"
	case input.ModeDisplay:
		bg = "#c4b5fd"
	case input.ModePicker:
		bg = "#a7f3d0"
	case input.ModePrompt:
		bg = "#fdba74"
	case input.ModeHelp:
		bg = "#f9a8d4"
	case input.ModeWorkspacePicker:
		bg = "#fcd34d"
	case input.ModeGlobal, input.ModeTerminalManager:
		bg = "#67e8f9"
	}
	if len(labels) == 0 {
		return []string{renderStatusChip("Esc BACK", "#334155", "#f8fafc")}
	}
	out := make([]string, 0, len(labels)*2)
	for i, label := range labels {
		if i > 0 {
			out = append(out, renderStatusSep())
		}
		chipBG := bg
		fg := "#020617"
		if label == "Esc BACK" {
			chipBG = "#334155"
			fg = "#f8fafc"
		}
		out = append(out, renderStatusChip(label, chipBG, fg))
	}
	return out
}

type statusHintContext struct {
	activeTab        *workbench.VisibleTab
	activePane       *workbench.VisiblePane
	activeRole       string
	tabCount         int
	workspaceCount   int
	hasFloating      bool
	activeIsFloating bool
}

func currentStatusTexts(state VisibleRenderState) []string {
	mode := input.ModeKind(strings.TrimSpace(state.InputMode))
	if mode == "" {
		mode = input.ModeNormal
	}
	ctx := buildStatusHintContext(state)
	out := make([]string, 0, 8)
	seen := make(map[string]struct{})
	for _, doc := range input.DefaultBindingCatalog() {
		if doc.Mode != mode || strings.TrimSpace(doc.StatusText) == "" {
			continue
		}
		if !statusDocVisible(doc, mode, ctx) {
			continue
		}
		if _, ok := seen[doc.StatusText]; ok {
			continue
		}
		seen[doc.StatusText] = struct{}{}
		out = append(out, doc.StatusText)
	}
	return out
}

func buildStatusHintContext(state VisibleRenderState) statusHintContext {
	ctx := statusHintContext{}
	if state.Workbench == nil {
		return ctx
	}
	ctx.tabCount = len(state.Workbench.Tabs)
	ctx.workspaceCount = state.Workbench.WorkspaceCount
	ctx.hasFloating = len(state.Workbench.FloatingPanes) > 0
	if state.Workbench.ActiveTab < 0 || state.Workbench.ActiveTab >= len(state.Workbench.Tabs) {
		return ctx
	}
	ctx.activeTab = &state.Workbench.Tabs[state.Workbench.ActiveTab]
	activePaneID := strings.TrimSpace(ctx.activeTab.ActivePaneID)
	if activePaneID == "" {
		return ctx
	}
	for i := range state.Workbench.FloatingPanes {
		if state.Workbench.FloatingPanes[i].ID == activePaneID {
			ctx.activePane = &state.Workbench.FloatingPanes[i]
			ctx.activeIsFloating = true
			break
		}
	}
	if ctx.activePane == nil {
		for i := range ctx.activeTab.Panes {
			if ctx.activeTab.Panes[i].ID == activePaneID {
				ctx.activePane = &ctx.activeTab.Panes[i]
				break
			}
		}
	}
	if ctx.activePane != nil {
		ctx.activeRole = newRuntimeLookup(state.Runtime).paneRole(ctx.activePane.ID)
	}
	return ctx
}

func statusDocVisible(doc input.BindingDoc, mode input.ModeKind, ctx statusHintContext) bool {
	switch mode {
	case input.ModePane:
		switch doc.Binding.Action {
		case input.ActionDetachPane, input.ActionClosePaneKill:
			return ctx.activePaneConnected()
		case input.ActionBecomeOwner:
			return ctx.canBecomeOwner()
		case input.ActionFocusPaneLeft, input.ActionFocusPaneRight, input.ActionFocusPaneUp, input.ActionFocusPaneDown,
			input.ActionSplitPane, input.ActionSplitPaneHorizontal, input.ActionReconnectPane,
			input.ActionClosePane, input.ActionZoomPane:
			return ctx.activePane != nil
		}
	case input.ModeResize:
		switch doc.Binding.Action {
		case input.ActionBecomeOwner:
			return ctx.canBecomeOwner()
		case input.ActionResizePaneLeft, input.ActionResizePaneRight, input.ActionResizePaneUp, input.ActionResizePaneDown,
			input.ActionResizePaneLargeLeft, input.ActionResizePaneLargeRight, input.ActionResizePaneLargeUp, input.ActionResizePaneLargeDown,
			input.ActionBalancePanes, input.ActionCycleLayout:
			return ctx.activeTab != nil
		}
	case input.ModeTab:
		switch doc.Binding.Action {
		case input.ActionNextTab, input.ActionPrevTab, input.ActionJumpTab:
			return ctx.tabCount > 1
		case input.ActionRenameTab, input.ActionKillTab:
			return ctx.activeTab != nil
		case input.ActionCreateTab:
			return ctx.tabCount >= 0
		}
	case input.ModeWorkspace:
		switch doc.Binding.Action {
		case input.ActionNextWorkspace, input.ActionPrevWorkspace:
			return ctx.workspaceCount > 1
		case input.ActionOpenWorkspacePicker, input.ActionCreateWorkspace, input.ActionRenameWorkspace, input.ActionDeleteWorkspace:
			return ctx.workspaceCount >= 0
		}
	case input.ModeFloating:
		switch doc.Binding.Action {
		case input.ActionCreateFloatingPane:
			return ctx.tabCount >= 0
		case input.ActionFocusNextFloatingPane, input.ActionFocusPrevFloatingPane:
			return ctx.hasFloating
		case input.ActionMoveFloatingLeft, input.ActionMoveFloatingDown, input.ActionMoveFloatingUp, input.ActionMoveFloatingRight,
			input.ActionResizeFloatingLeft, input.ActionResizeFloatingDown, input.ActionResizeFloatingUp, input.ActionResizeFloatingRight,
			input.ActionCenterFloatingPane, input.ActionToggleFloatingVisibility, input.ActionCloseFloatingPane, input.ActionOpenPicker:
			return ctx.activeIsFloating
		case input.ActionBecomeOwner:
			return ctx.activeIsFloating && ctx.canBecomeOwner()
		}
	case input.ModeDisplay:
		switch doc.Binding.Action {
		case input.ActionScrollUp, input.ActionScrollDown:
			return ctx.activePaneConnected()
		case input.ActionZoomPane:
			return ctx.activePane != nil
		}
	}
	return true
}

func (c statusHintContext) activePaneConnected() bool {
	return c.activePane != nil && strings.TrimSpace(c.activePane.TerminalID) != ""
}

func (c statusHintContext) canBecomeOwner() bool {
	return c.activePaneConnected() && c.activeRole == "follower"
}

func fillLine(left, right string, width int, bg lipgloss.Color) string {
	if width <= 0 {
		return ""
	}
	filler := lipgloss.NewStyle().Background(bg)
	leftW := xansi.StringWidth(left)
	rightW := xansi.StringWidth(right)
	if leftW+rightW >= width {
		return forceWidthANSIOverlay(left+right, width)
	}
	return left + filler.Render(strings.Repeat(" ", width-leftW-rightW)) + right
}
