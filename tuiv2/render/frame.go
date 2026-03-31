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
		leftParts = append(leftParts, renderStatusChip("p PANE", "#86efac", "#020617"))
		leftParts = append(leftParts, renderStatusSep())
		leftParts = append(leftParts, renderStatusChip("r RESIZE", "#fca5a5", "#020617"))
		leftParts = append(leftParts, renderStatusSep())
		leftParts = append(leftParts, renderStatusChip("t TAB", "#93c5fd", "#020617"))
		leftParts = append(leftParts, renderStatusSep())
		leftParts = append(leftParts, renderStatusChip("w WORKSPACE", "#fcd34d", "#020617"))
		leftParts = append(leftParts, renderStatusSep())
		leftParts = append(leftParts, renderStatusChip("o FLOAT", "#fde047", "#020617"))
		leftParts = append(leftParts, renderStatusSep())
		leftParts = append(leftParts, renderStatusChip("v DISPLAY", "#c4b5fd", "#020617"))
		leftParts = append(leftParts, renderStatusSep())
		leftParts = append(leftParts, renderStatusChip("f PICKER", "#a7f3d0", "#020617"))
		leftParts = append(leftParts, renderStatusSep())
		leftParts = append(leftParts, renderStatusChip("g GLOBAL", "#67e8f9", "#020617"))
	} else {
		badge := renderModeBadge(mode)
		if badge != "" {
			leftParts = append(leftParts, badge)
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
	case "FLOAT":
		bg = "#fde047"
	case "PICKER":
		bg = "#a7f3d0"
	case "GLOBAL":
		bg = "#67e8f9"
	}
	return renderStatusChip(label, bg, "#020617") + renderStatusSep()
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
