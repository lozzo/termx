package chrome

import (
	"strings"

	"github.com/lozzow/termx/tui/render/canvas"
)

func Frame(title, meta string, width int, body []string) string {
	if width < 4 {
		width = 4
	}
	header := "┌─ " + title
	if meta != "" {
		header += "─" + meta
	}
	if len(header)+1 > width {
		width = len(header) + 1
	}
	header += strings.Repeat("─", max(1, width-len(header)-1))
	header = canvas.PadRight(header, width-1) + "┐"

	lines := make([]string, 0, len(body)+2)
	lines = append(lines, header)
	for _, line := range body {
		lines = append(lines, "│"+canvas.PadRight(line, width-2)+"│")
	}
	lines = append(lines, "└"+strings.Repeat("─", width-2)+"┘")
	return strings.Join(lines, "\n")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
