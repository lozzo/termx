package render

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
)

const (
	TopChromeRows    = 1
	BottomChromeRows = 1
)

func FrameBodyHeight(totalHeight int) int {
	return maxInt(1, totalHeight-TopChromeRows-BottomChromeRows)
}

func renderTabBar(state VisibleRenderState) string {
	theme := uiThemeForState(state)
	layout := buildTabBarLayout(state)
	return fillLine(renderTabBarLeft(layout), layout.rightText, state.TermSize.Width, theme.tabActiveBG)
}

func renderStatusBar(state VisibleRenderState) string {
	theme := uiThemeForState(state)
	width := state.TermSize.Width
	labels := currentStatusTexts(state)

	var leftParts []string
	if !suppressStatusHints(state) {
		mode := strings.TrimSpace(state.InputMode)
		if mode == "" || mode == "normal" {
			leftParts = append(leftParts, renderDesktopHint(theme, "Ctrl", theme.hintKeyFG))
			rootColors := rootStatusHintColors(theme)
			for i, label := range labels {
				if i >= len(rootColors) {
					break
				}
				leftParts = append(leftParts, renderStatusSep(theme))
				leftParts = append(leftParts, renderDesktopHint(theme, label, rootColors[i]))
			}
		} else {
			badge := renderModeBadge(theme, mode)
			if badge != "" {
				leftParts = append(leftParts, badge)
			}
			leftParts = append(leftParts, renderModeHints(theme, mode, labels)...)
		}
	}
	left := strings.Join(leftParts, "")

	right := renderStatusBarRight(theme, statusBarRightTokens(state))
	if right != "" && xansi.StringWidth(left)+1+xansi.StringWidth(right) > width {
		right = ""
	}

	return fillLine(left, right, width, theme.chromeBG)
}

type statusBarToken struct {
	Kind   HitRegionKind
	Label  string
	Action input.SemanticAction
}

func statusBarRightTokens(state VisibleRenderState) []statusBarToken {
	tokens := make([]statusBarToken, 0, 3)
	if state.Workbench != nil {
		tokens = append(tokens, statusBarToken{Label: "ws:" + state.Workbench.WorkspaceName})
		if label := floatingSummaryLabel(state.Workbench); label != "" {
			tokens = append(tokens, statusBarToken{
				Kind:   HitRegionFloatingOverview,
				Label:  label,
				Action: input.SemanticAction{Kind: input.ActionOpenFloatingOverview},
			})
		}
	}
	if state.Runtime != nil {
		tokens = append(tokens, statusBarToken{Label: fmt.Sprintf("terminals:%d", len(state.Runtime.Terminals))})
	}
	return tokens
}

func renderStatusBarRight(theme uiTheme, tokens []statusBarToken) string {
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if strings.TrimSpace(token.Label) == "" {
			continue
		}
		parts = append(parts, statusMetaTokenStyle(theme, token.Action.Kind != "").Render(token.Label))
	}
	return strings.Join(parts, " ")
}

func floatingSummaryLabel(visible *workbench.VisibleWorkbench) string {
	if visible == nil || visible.FloatingTotal == 0 {
		return ""
	}
	if visible.FloatingCollapsed > 0 {
		if visible.FloatingHidden > 0 {
			return fmt.Sprintf("float:%d collapsed:%d hidden:%d", visible.FloatingTotal, visible.FloatingCollapsed, visible.FloatingHidden)
		}
		return fmt.Sprintf("float:%d collapsed:%d", visible.FloatingTotal, visible.FloatingCollapsed)
	}
	if visible.FloatingHidden > 0 {
		return fmt.Sprintf("float:%d hidden:%d", visible.FloatingTotal, visible.FloatingHidden)
	}
	return fmt.Sprintf("float:%d", visible.FloatingTotal)
}

func suppressStatusHints(state VisibleRenderState) bool {
	return false
}

func renderStatusChip(theme uiTheme, label, bg, fg string) string {
	return statusChipStyle(theme).
		Foreground(lipgloss.Color(fg)).
		Background(lipgloss.Color(bg)).
		Render(label)
}

func renderStatusSep(theme uiTheme) string {
	return statusSeparatorStyle(theme).Render(" • ")
}

func renderDesktopHint(theme uiTheme, label, color string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	parts := strings.SplitN(label, " ", 2)
	key := parts[0]
	text := ""
	if len(parts) > 1 {
		text = parts[1]
	}
	// Use foreground-only coloring (no background chip). Colored backgrounds
	// are especially noticeable during full-frame redraws triggered by cursor
	// position changes, making the status bar appear to flash.
	keyStyle := statusHintKeyStyle(theme).
		Foreground(lipgloss.Color(color))
	if text == "" {
		return keyStyle.Render("[" + key + "]")
	}
	textStyle := statusHintTextStyle(theme)
	return keyStyle.Render("["+key+"]") + textStyle.Render(" "+text)
}

func renderModeBadge(theme uiTheme, mode string) string {
	label := strings.ToUpper(mode)
	color := modeAccentColor(theme, input.ModeKind(mode))
	return statusPartDefaultStyle(theme).
		Bold(true).
		Foreground(lipgloss.Color(color)).
		Render(label) + renderStatusSep(theme)
}

func renderModeHints(theme uiTheme, mode string, labels []string) []string {
	modeKind := input.ModeKind(mode)
	color := modeAccentColor(theme, modeKind)
	if len(labels) == 0 {
		return []string{renderDesktopHint(theme, "Esc BACK", theme.panelMuted)}
	}
	out := make([]string, 0, len(labels)*2)
	for i, label := range labels {
		if i > 0 {
			out = append(out, renderStatusSep(theme))
		}
		fg := color
		if label == "Esc BACK" {
			fg = theme.panelMuted
		}
		out = append(out, renderDesktopHint(theme, label, fg))
	}
	return out
}

func modeAccentColor(theme uiTheme, mode input.ModeKind) string {
	switch mode {
	case input.ModePane:
		return theme.success
	case input.ModeResize:
		return theme.danger
	case input.ModeTab:
		return theme.chromeAccent
	case input.ModeWorkspace, input.ModeWorkspacePicker:
		return theme.warning
	case input.ModeFloating:
		return theme.warning
	case input.ModeFloatingOverview:
		return theme.warning
	case input.ModeDisplay:
		return theme.hintKeyFG
	case input.ModePicker:
		return theme.success
	case input.ModePrompt:
		return theme.chromeAccent
	case input.ModeHelp:
		return theme.info
	case input.ModeGlobal, input.ModeTerminalManager:
		return theme.hintKeyFG
	default:
		return theme.chromeText
	}
}

func rootStatusHintColors(theme uiTheme) []string {
	// Display/global are the only root shortcuts that fan into the most
	// saturated info accent. On Terminal.app that accent is the most prone to
	// looking like footer shimmer during redraws, so keep those roots on the
	// base hint accent while preserving the other semantic group colors.
	return []string{
		theme.success,
		theme.danger,
		theme.chromeAccent,
		theme.warning,
		theme.warning,
		theme.hintKeyFG,
		theme.success,
		theme.hintKeyFG,
	}
}

type statusHintContext struct {
	activeTab        *workbench.VisibleTab
	activePane       *workbench.VisiblePane
	activeRole       string
	tabCount         int
	workspaceCount   int
	hasFloating      bool
	activeIsFloating bool
	state            *VisibleRenderState
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
	ctx := statusHintContext{state: &state}
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
		case input.ActionRestartTerminal:
			return ctx.activePaneExited()
		case input.ActionBecomeOwner:
			return ctx.canBecomeOwner()
		case input.ActionToggleTerminalSizeLock:
			return ctx.activePaneConnected()
		case input.ActionReconnectPane:
			return ctx.activePane != nil && !ctx.activePaneExited()
		case input.ActionFocusPaneLeft, input.ActionFocusPaneRight, input.ActionFocusPaneUp, input.ActionFocusPaneDown,
			input.ActionSplitPane, input.ActionSplitPaneHorizontal,
			input.ActionClosePane, input.ActionZoomPane:
			return ctx.activePane != nil
		}
	case input.ModeResize:
		switch doc.Binding.Action {
		case input.ActionBecomeOwner:
			return ctx.canBecomeOwner()
		case input.ActionToggleTerminalSizeLock:
			return ctx.activePaneConnected()
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
		case input.ActionOpenFloatingOverview, input.ActionSummonFloatingPane,
			input.ActionExpandAllFloatingPanes, input.ActionCollapseAllFloatingPanes:
			return ctx.hasFloating
		case input.ActionMoveFloatingLeft, input.ActionMoveFloatingDown, input.ActionMoveFloatingUp, input.ActionMoveFloatingRight,
			input.ActionResizeFloatingLeft, input.ActionResizeFloatingDown, input.ActionResizeFloatingUp, input.ActionResizeFloatingRight,
			input.ActionCenterFloatingPane, input.ActionToggleFloatingVisibility, input.ActionCloseFloatingPane, input.ActionOpenPicker,
			input.ActionCollapseFloatingPane, input.ActionAutoFitFloatingPane, input.ActionToggleFloatingAutoFit:
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

func (c statusHintContext) activePaneExited() bool {
	if !c.activePaneConnected() || c.state == nil {
		return false
	}
	lookup := newRuntimeLookup(c.state.Runtime)
	terminal := lookup.terminal(c.activePane.TerminalID)
	return terminal != nil && terminal.State == "exited"
}

func (c statusHintContext) canBecomeOwner() bool {
	return c.activePaneConnected() && c.activeRole == "follower"
}

func fillLine(left, right string, width int, bg string) string {
	if width <= 0 {
		return ""
	}
	leftW := xansi.StringWidth(left)
	rightW := xansi.StringWidth(right)
	gap := 0
	if strings.TrimSpace(left) != "" && strings.TrimSpace(right) != "" {
		gap = 1
	}
	if rightW >= width {
		return xansi.CHA(1) + forceWidthANSIOverlay(right, width)
	}
	maxLeftW := maxInt(0, width-rightW-gap)
	if leftW > maxLeftW {
		left = xansi.Truncate(left, maxLeftW, "")
		leftW = xansi.StringWidth(left)
	}
	fillW := maxInt(0, width-leftW-rightW-gap)

	// Build the filler with direct ANSI instead of lipgloss to avoid
	// lipgloss reset/re-set artifacts that can eat background colors
	// on some terminals.
	var b strings.Builder
	b.WriteString(xansi.CHA(1))
	b.WriteString(left)
	if fillW+gap > 0 {
		b.WriteString(styleANSI(drawStyle{BG: bg}))
		b.WriteString(strings.Repeat(" ", fillW+gap))
		b.WriteString("\x1b[0m")
	}
	b.WriteString(right)
	// Erase to end of line to prevent stale characters from previous
	// wider renders lingering on the right.
	b.WriteString("\x1b[K")
	return b.String()
}
