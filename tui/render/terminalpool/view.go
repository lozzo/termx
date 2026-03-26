package terminalpool

import (
	"strings"

	"github.com/lozzow/termx/tui/core/types"
	"github.com/lozzow/termx/tui/render/canvas"
	"github.com/lozzow/termx/tui/render/projection"
)

func Render(screen projection.Screen, width, height int) string {
	rows := 8 + len(screen.Pool.Visible) + len(screen.Pool.Parked) + len(screen.Pool.Exited)
	c := canvas.New(width, rows)
	row := 0
	c.WriteLine(row, "terminal pool")
	row++
	c.WriteLine(row, "visible")
	row++
	for _, item := range screen.Pool.Visible {
		c.WriteLine(row, renderItem(item, screen.Pool.SelectedTerminalID))
		row++
	}
	c.WriteLine(row, "parked")
	row++
	for _, item := range screen.Pool.Parked {
		c.WriteLine(row, renderItem(item, screen.Pool.SelectedTerminalID))
		row++
	}
	c.WriteLine(row, "exited")
	row++
	for _, item := range screen.Pool.Exited {
		c.WriteLine(row, renderItem(item, screen.Pool.SelectedTerminalID))
		row++
	}
	return strings.TrimRight(c.String(), "\n")
}

func renderItem(item projection.PoolItem, selected types.TerminalID) string {
	prefix := " "
	if item.ID == selected {
		prefix = ">"
	}
	return prefix + " " + item.Name + " [" + item.State + "]"
}
