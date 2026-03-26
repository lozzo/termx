package overlay

import (
	"strings"

	"github.com/lozzow/termx/tui/render/projection"
)

func Render(screen projection.Screen) string {
	switch screen.Overlay.Kind {
	case "connect-picker":
		return renderConnectPicker(screen.Overlay)
	case "help":
		return "overlay: help"
	case "prompt":
		return "overlay: prompt"
	default:
		return ""
	}
}

func renderConnectPicker(overlay projection.Overlay) string {
	var lines []string
	lines = append(lines, "overlay: connect picker")
	lines = append(lines, "j/k move  enter connect  esc close")
	for _, item := range overlay.Items {
		prefix := " "
		if item.ID == overlay.Selected {
			prefix = ">"
		}
		lines = append(lines, prefix+" "+item.Name+" ["+item.State+"]")
	}
	return strings.Join(lines, "\n")
}
