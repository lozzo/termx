package render

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type Theme struct {
	HostFG string
	HostBG string

	ChromeFG string
	ChromeBG string

	Accent string
	Muted  string

	Success string
	Warning string
	Danger  string
	Info    string

	PanelBorder      string
	MutedBorder      string
	ActivePaneBorder string
	InactivePane     string

	ToastBG   string
	OverlayBG string

	StatusFG string
	StatusBG string
}

func DefaultTheme() Theme {
	return Theme{
		HostFG: "#d7d7d0",
		HostBG: "#060807",

		ChromeFG: "#d7d7d0",
		ChromeBG: "#141816",

		Accent: "#58d5c9",
		Muted:  "#6f7771",

		Success: "#8adf7a",
		Warning: "#f0c45c",
		Danger:  "#ff6b6b",
		Info:    "#7ab8ff",

		PanelBorder:      "#9aa39c",
		MutedBorder:      "#4f5651",
		ActivePaneBorder: "#58d5c9",
		InactivePane:     "#6f7771",

		ToastBG:   "#141816",
		OverlayBG: "#111614",

		StatusFG: "#d9e7ff",
		StatusBG: "#18324a",
	}
}

func (theme Theme) WithFallback() Theme {
	defaults := DefaultTheme()
	if theme.HostFG == "" {
		theme.HostFG = defaults.HostFG
	}
	if theme.HostBG == "" {
		theme.HostBG = defaults.HostBG
	}
	if theme.ChromeFG == "" {
		theme.ChromeFG = defaults.ChromeFG
	}
	if theme.ChromeBG == "" {
		theme.ChromeBG = defaults.ChromeBG
	}
	if theme.Accent == "" {
		theme.Accent = defaults.Accent
	}
	if theme.Muted == "" {
		theme.Muted = defaults.Muted
	}
	if theme.Success == "" {
		theme.Success = defaults.Success
	}
	if theme.Warning == "" {
		theme.Warning = defaults.Warning
	}
	if theme.Danger == "" {
		theme.Danger = defaults.Danger
	}
	if theme.Info == "" {
		theme.Info = defaults.Info
	}
	if theme.PanelBorder == "" {
		theme.PanelBorder = defaults.PanelBorder
	}
	if theme.MutedBorder == "" {
		theme.MutedBorder = defaults.MutedBorder
	}
	if theme.ActivePaneBorder == "" {
		theme.ActivePaneBorder = defaults.ActivePaneBorder
	}
	if theme.InactivePane == "" {
		theme.InactivePane = defaults.InactivePane
	}
	if theme.ToastBG == "" {
		theme.ToastBG = defaults.ToastBG
	}
	if theme.OverlayBG == "" {
		theme.OverlayBG = defaults.OverlayBG
	}
	if theme.StatusFG == "" {
		theme.StatusFG = defaults.StatusFG
	}
	if theme.StatusBG == "" {
		theme.StatusBG = defaults.StatusBG
	}
	return theme
}

func StatusStyle(theme Theme) lipgloss.Style {
	theme = theme.WithFallback()
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.StatusFG)).
		Background(lipgloss.Color(theme.StatusBG))
}

func SafeLine(value string) string {
	return strings.ReplaceAll(value, "\n", " ")
}

func Width(value string) int {
	return DisplayWidth(value)
}

func Truncate(value string, width int) string {
	return TruncateCells(value, width)
}
