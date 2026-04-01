package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
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
	if state.Workbench == nil || len(state.Workbench.Tabs) == 0 {
		return fillLine("[tuiv2]", "", state.TermSize.Width, tabBarBG)
	}
	wsLabel := workspaceLabelStyle.Render("[" + state.Workbench.WorkspaceName + "]")

	items := make([]string, 0, len(state.Workbench.Tabs)+1)
	items = append(items, wsLabel)
	for i, tab := range state.Workbench.Tabs {
		name := strings.TrimSpace(tab.Name)
		if name == "" {
			name = fmt.Sprintf("tab %d", i+1)
		}
		labelText := fmt.Sprintf("%d:%s", i+1, name)
		if i == state.Workbench.ActiveTab {
			items = append(items, tabActiveStyle.Render("["+labelText+"]"))
		} else {
			items = append(items, tabInactiveStyle.Render(labelText))
		}
	}
	left := strings.Join(items, " ")

	var rightParts []string
	if state.Error != "" {
		rightParts = append(rightParts, statusPartErrorStyle.Padding(0, 1).Render(state.Error))
	} else if state.Notice != "" {
		rightParts = append(rightParts, statusPartNoticeStyle.Render(state.Notice))
	}
	right := strings.Join(rightParts, "  ")

	return fillLine(left, right, state.TermSize.Width, tabBarBG)
}

func renderStatusBar(state VisibleRenderState) string {
	width := state.TermSize.Width

	// left: mode badge + shortcut hints
	var leftParts []string
	mode := strings.TrimSpace(state.InputMode)
	if mode == "" || mode == "normal" {
		leftParts = append(leftParts, renderStatusChip("Ctrl", "#020617", "#f8fafc"))
		leftParts = append(leftParts, renderStatusSep())
		leftParts = append(leftParts, renderStatusChip("P PANE", "#86efac", "#020617"))
		leftParts = append(leftParts, renderStatusSep())
		leftParts = append(leftParts, renderStatusChip("R RESIZE", "#fca5a5", "#020617"))
		leftParts = append(leftParts, renderStatusSep())
		leftParts = append(leftParts, renderStatusChip("T TAB", "#93c5fd", "#020617"))
		leftParts = append(leftParts, renderStatusSep())
		leftParts = append(leftParts, renderStatusChip("W WORKSPACE", "#fcd34d", "#020617"))
		leftParts = append(leftParts, renderStatusSep())
		leftParts = append(leftParts, renderStatusChip("O FLOAT", "#fde047", "#020617"))
		leftParts = append(leftParts, renderStatusSep())
		leftParts = append(leftParts, renderStatusChip("V DISPLAY", "#c4b5fd", "#020617"))
		leftParts = append(leftParts, renderStatusSep())
		leftParts = append(leftParts, renderStatusChip("F PICKER", "#a7f3d0", "#020617"))
		leftParts = append(leftParts, renderStatusSep())
		leftParts = append(leftParts, renderStatusChip("G GLOBAL", "#67e8f9", "#020617"))
	} else {
		badge := renderModeBadge(mode)
		if badge != "" {
			leftParts = append(leftParts, badge)
		}
		leftParts = append(leftParts, renderModeHints(mode)...)
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
	case "GLOBAL":
		bg = "#67e8f9"
	case "TERMINAL-MANAGER":
		bg = "#67e8f9"
	}
	return renderStatusChip(label, bg, "#020617") + renderStatusSep()
}

func renderModeHints(mode string) []string {
	switch mode {
	case "pane":
		return []string{
			renderStatusChip("h/j/k/l FOCUS", "#86efac", "#020617"),
			renderStatusSep(),
			renderStatusChip("% VSPLIT", "#86efac", "#020617"),
			renderStatusSep(),
			renderStatusChip("\" HSPLIT", "#86efac", "#020617"),
			renderStatusSep(),
			renderStatusChip("z ZOOM", "#86efac", "#020617"),
			renderStatusSep(),
			renderStatusChip("w CLOSE", "#86efac", "#020617"),
			renderStatusSep(),
			renderStatusChip("Esc BACK", "#334155", "#f8fafc"),
		}
	case "resize":
		return []string{
			renderStatusChip("h/j/k/l RESIZE", "#fca5a5", "#020617"),
			renderStatusSep(),
			renderStatusChip("H/J/K/L RESIZE\u00d72", "#fca5a5", "#020617"),
			renderStatusSep(),
			renderStatusChip("= BALANCE", "#fca5a5", "#020617"),
			renderStatusSep(),
			renderStatusChip("Space LAYOUT", "#fca5a5", "#020617"),
			renderStatusSep(),
			renderStatusChip("Esc BACK", "#334155", "#f8fafc"),
		}
	case "tab":
		return []string{
			renderStatusChip("c NEW", "#93c5fd", "#020617"),
			renderStatusSep(),
			renderStatusChip("n/p NEXT/PREV", "#93c5fd", "#020617"),
			renderStatusSep(),
			renderStatusChip("w CLOSE", "#93c5fd", "#020617"),
			renderStatusSep(),
			renderStatusChip("Esc BACK", "#334155", "#f8fafc"),
		}
	case "workspace":
		return []string{
			renderStatusChip("Ctrl-F PICK", "#fcd34d", "#020617"),
			renderStatusSep(),
			renderStatusChip("N NEW", "#fcd34d", "#020617"),
			renderStatusSep(),
			renderStatusChip("D DELETE", "#fcd34d", "#020617"),
			renderStatusSep(),
			renderStatusChip("Esc BACK", "#334155", "#f8fafc"),
		}
	case "floating":
		return []string{
			renderStatusChip("N NEW FLOAT", "#fde047", "#020617"),
			renderStatusSep(),
			renderStatusChip("Esc BACK", "#334155", "#f8fafc"),
		}
	case "display":
		return []string{
			renderStatusChip("u/d SCROLL", "#c4b5fd", "#020617"),
			renderStatusSep(),
			renderStatusChip("z ZOOM", "#c4b5fd", "#020617"),
			renderStatusSep(),
			renderStatusChip("Esc BACK", "#334155", "#f8fafc"),
		}
	case "global":
		return []string{
			renderStatusChip("Ctrl-T MANAGER", "#67e8f9", "#020617"),
			renderStatusSep(),
			renderStatusChip("Ctrl-Q QUIT", "#67e8f9", "#020617"),
			renderStatusSep(),
			renderStatusChip("Esc BACK", "#334155", "#f8fafc"),
		}
	case "terminal-manager":
		return []string{
			renderStatusChip("↑/↓ MOVE", "#67e8f9", "#020617"),
			renderStatusSep(),
			renderStatusChip("Enter ATTACH", "#67e8f9", "#020617"),
			renderStatusSep(),
			renderStatusChip("Ctrl-K KILL", "#67e8f9", "#020617"),
			renderStatusSep(),
			renderStatusChip("Esc BACK", "#334155", "#f8fafc"),
		}
	default:
		return []string{renderStatusChip("Esc BACK", "#334155", "#f8fafc")}
	}
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
