package overlay

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/tui/app"
)

func Render(model app.Model, width, height int) string {
	_ = width
	_ = height

	active := model.Overlay.Active()
	switch active.Kind {
	case app.OverlayConnectDialog:
		return renderConnectDialog(active)
	case app.OverlayHelp:
		return renderHelpOverlay(active)
	default:
		return ""
	}
}

func renderConnectDialog(active app.OverlayState) string {
	if active.Connect == nil {
		return ""
	}
	lines := []string{
		"Connect Pane",
		fmt.Sprintf("target: %s", active.Connect.Target),
		fmt.Sprintf("destination: %s", active.Connect.Destination),
	}
	if active.Connect.Query != "" {
		lines = append(lines, fmt.Sprintf("query: %s", active.Connect.Query))
	}
	for index, item := range active.Connect.Items {
		prefix := "  "
		if index == 0 {
			prefix = "> "
		}
		line := strings.TrimSpace(strings.Join([]string{item.Name, item.StateSummary, item.OwnerSummary}, "   "))
		lines = append(lines, prefix+line)
	}
	lines = append(lines, "Enter confirm  •  Esc cancel")
	return strings.Join(lines, "\n")
}

func renderHelpOverlay(active app.OverlayState) string {
	if active.Help == nil {
		return ""
	}
	lines := []string{"Help"}
	for _, section := range active.Help.Sections {
		lines = append(lines, section.Title)
		lines = append(lines, "  "+strings.Join(section.Items, "   "))
	}
	lines = append(lines, "Esc close")
	return strings.Join(lines, "\n")
}
