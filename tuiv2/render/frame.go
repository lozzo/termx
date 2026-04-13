package render

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/workbench"
)

const (
	TopChromeRows    = 1
	BottomChromeRows = 1
)

func FrameBodyHeight(totalHeight int) int {
	return maxInt(1, totalHeight-TopChromeRows-BottomChromeRows)
}

func renderTabBarVM(vm RenderVM) string {
	theme := uiThemeForRuntime(vm.Runtime)
	layout := buildTabBarLayoutVM(vm)
	return fillLine(renderTabBarLeft(layout), layout.rightText, vm.TermSize.Width, theme.tabActiveBG)
}

func renderTabBar(state VisibleRenderState) string {
	theme := uiThemeForState(state)
	layout := buildTabBarLayout(state)
	return fillLine(renderTabBarLeft(layout), layout.rightText, state.TermSize.Width, theme.tabActiveBG)
}

func renderStatusBarVM(vm RenderVM) string {
	theme := uiThemeForRuntime(vm.Runtime)
	width := vm.TermSize.Width
	labels := currentStatusTextsVM(vm)

	var leftParts []string
	if !suppressStatusHintsVM(vm) {
		mode := strings.TrimSpace(vm.Status.InputMode)
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

	right := renderStatusBarRight(theme, statusBarRightTokensVM(vm))
	if right != "" && xansi.StringWidth(left)+1+xansi.StringWidth(right) > width {
		right = ""
	}

	return fillLine(left, right, width, theme.chromeBG)
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

func statusBarRightTokens(state VisibleRenderState) []RenderStatusToken {
	tokens := make([]RenderStatusToken, 0, 8)
	if state.Overlay.Kind == VisibleOverlayWorkspacePicker && state.Overlay.WorkspacePicker != nil {
		tokens = append(tokens, workspacePickerStatusBarRightTokens(state.Overlay.WorkspacePicker.SelectedItem())...)
	}
	if state.Workbench != nil {
		tokens = append(tokens, RenderStatusToken{Label: "ws:" + state.Workbench.WorkspaceName})
		if label := floatingSummaryLabel(state.Workbench); label != "" {
			tokens = append(tokens, RenderStatusToken{
				Kind:   HitRegionFloatingOverview,
				Label:  label,
				Action: input.SemanticAction{Kind: input.ActionOpenFloatingOverview},
			})
		}
	}
	if state.Runtime != nil {
		tokens = append(tokens, RenderStatusToken{Label: fmt.Sprintf("terminals:%d", len(state.Runtime.Terminals))})
	}
	return tokens
}

func statusBarRightTokensVM(vm RenderVM) []RenderStatusToken {
	if len(vm.Status.RightTokens) == 0 {
		return nil
	}
	return append([]RenderStatusToken(nil), vm.Status.RightTokens...)
}

func workspacePickerStatusBarRightTokens(item *modal.WorkspacePickerItem) []RenderStatusToken {
	if item == nil {
		return nil
	}
	switch {
	case item.CreateNew:
		label := "sel:new-workspace"
		if name := strings.TrimSpace(item.CreateName); name != "" {
			label = "sel:new:" + name
		}
		return []RenderStatusToken{{Label: label}}
	case strings.TrimSpace(item.PaneID) != "":
		tokens := []RenderStatusToken{{Label: "sel:pane:" + strings.TrimSpace(item.Name)}}
		if state := strings.TrimSpace(item.State); state != "" {
			tokens = append(tokens, RenderStatusToken{Label: state})
		}
		if role := strings.TrimSpace(item.Role); role != "" {
			tokens = append(tokens, RenderStatusToken{Label: role})
		}
		if item.Floating {
			tokens = append(tokens, RenderStatusToken{Label: "floating"})
		}
		return tokens
	case strings.TrimSpace(item.TabID) != "":
		tokens := []RenderStatusToken{{Label: "sel:tab:" + strings.TrimSpace(item.Name)}}
		if item.PaneCount > 0 {
			tokens = append(tokens, RenderStatusToken{Label: fmt.Sprintf("panes:%d", item.PaneCount)})
		}
		if item.FloatingCount > 0 {
			tokens = append(tokens, RenderStatusToken{Label: fmt.Sprintf("float:%d", item.FloatingCount)})
		}
		return tokens
	default:
		tokens := []RenderStatusToken{{Label: "sel:ws:" + strings.TrimSpace(item.Name)}}
		if item.TabCount > 0 {
			tokens = append(tokens, RenderStatusToken{Label: fmt.Sprintf("tabs:%d", item.TabCount)})
		}
		if item.PaneCount > 0 {
			tokens = append(tokens, RenderStatusToken{Label: fmt.Sprintf("panes:%d", item.PaneCount)})
		}
		if item.FloatingCount > 0 {
			tokens = append(tokens, RenderStatusToken{Label: fmt.Sprintf("float:%d", item.FloatingCount)})
		}
		return tokens
	}
}

func renderStatusBarRight(theme uiTheme, tokens []RenderStatusToken) string {
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

func suppressStatusHintsVM(vm RenderVM) bool {
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

func currentStatusTexts(state VisibleRenderState) []string {
	return append([]string(nil), state.StatusHints...)
}

func currentStatusTextsVM(vm RenderVM) []string {
	return append([]string(nil), vm.Status.Hints...)
}

func statusBarRightTokenSignature(tokens []RenderStatusToken) string {
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if strings.TrimSpace(token.Label) == "" {
			continue
		}
		part := string(token.Kind) + "|" + token.Label + "|" + string(token.Action.Kind)
		if token.Action.Kind == "" {
			part += "|plain"
		} else {
			part += "|action"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "\x1f")
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
