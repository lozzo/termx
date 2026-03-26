package workbench

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/tui/render/canvas"
	"github.com/lozzow/termx/tui/render/projection"
)

func Render(screen projection.Screen, width, height int) string {
	rows := 2
	for _, pane := range screen.Panes {
		rows += 2
		rows += strings.Count(pane.Body, "\n") + 1
	}
	c := canvas.New(width, rows)
	row := 0
	c.WriteLine(row, fmt.Sprintf("termx [%s] workbench panes=%d", screen.WorkspaceName, len(screen.Panes)))
	row++
	c.WriteLine(row, "keys: c connect  p pool  d disconnect  r reconnect  ? help")
	row++
	for index, pane := range screen.Panes {
		prefix := " "
		if index == 0 {
			prefix = ">"
		}
		c.WriteLine(row, prefix+" "+pane.Title+" ["+pane.Status+"]")
		row++
		for _, line := range strings.Split(pane.Body, "\n") {
			c.WriteLine(row, line)
			row++
		}
		c.WriteLine(row, "")
		row++
	}
	return c.String()
}
