package render

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type Theme struct {
	StatusFG string
	StatusBG string
}

func DefaultTheme() Theme {
	return Theme{
		StatusFG: "#d9e7ff",
		StatusBG: "#18324a",
	}
}

func StatusStyle(theme Theme) lipgloss.Style {
	if theme == (Theme{}) {
		theme = DefaultTheme()
	}
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
