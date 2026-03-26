package workbench

import (
	"strings"

	"github.com/lozzow/termx/tui/render/canvas"
	"github.com/lozzow/termx/tui/render/projection"
)

func Render(screen projection.Screen, width, height int) string {
	rows := 1
	for _, pane := range screen.Panes {
		rows += 2
		rows += strings.Count(pane.Body, "\n") + 1
	}
	c := canvas.New(width, rows)
	row := 0
	c.WriteLine(row, "termx ["+screen.WorkspaceName+"]")
	row++
	for _, pane := range screen.Panes {
		c.WriteLine(row, pane.Title+" ["+pane.Status+"]")
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
