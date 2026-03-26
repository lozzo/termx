package overlay

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/tui/render/canvas"
	"github.com/lozzow/termx/tui/render/projection"
)

func Render(screen projection.Screen) string {
	switch screen.Overlay.Kind {
	case "connect-picker":
		return renderConnectPicker(screen.Overlay)
	case "help":
		return renderBox("help", []string{
			"workbench / pool shortcuts",
			"",
			"c connect picker",
			"p terminal pool",
			"d disconnect active pane",
			"r reconnect active pane",
			"esc close overlay",
		})
	case "prompt":
		title := strings.TrimSpace(screen.Overlay.Title)
		if title == "" {
			title = "prompt"
		}
		return renderBox(title, []string{
			"prompt input UI pending",
			"",
			"this frame reserves the final overlay position",
		})
	default:
		return ""
	}
}

func renderConnectPicker(overlay projection.Overlay) string {
	lines := []string{"j/k move  enter connect  esc close", ""}
	for _, item := range overlay.Items {
		prefix := "  "
		if item.ID == overlay.Selected {
			prefix = "> "
		}
		lines = append(lines, prefix+item.Name+" ["+item.State+"]")
	}
	if len(overlay.Items) == 0 {
		lines = append(lines, "no terminals available")
	}
	return renderBox("connect picker", lines)
}

func renderBox(title string, lines []string) string {
	contentWidth := longestLine(lines)
	if len(title) > contentWidth {
		contentWidth = len(title)
	}
	contentWidth = max(contentWidth, 28)
	width := contentWidth + 4
	height := len(lines) + 2
	c := canvas.New(width, height)
	c.DrawBox(0, 0, width, height, fmt.Sprintf(" %s ", title))
	c.WriteBlock(2, 1, contentWidth, len(lines), lines)
	return c.String()
}

func longestLine(lines []string) int {
	longest := 0
	for _, line := range lines {
		if len([]rune(line)) > longest {
			longest = len([]rune(line))
		}
	}
	return longest
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
