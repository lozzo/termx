package workbench

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/tui/render/canvas"
	"github.com/lozzow/termx/tui/render/projection"
)

const (
	minWorkbenchWidth  = 48
	minWorkbenchHeight = 12
)

func Render(screen projection.Screen, width, height int) string {
	width = max(width, minWorkbenchWidth)
	height = max(height, minWorkbenchHeight)
	c := canvas.New(width, height)

	c.DrawBox(0, 0, width, height, fmt.Sprintf(" termx [%s] workbench ", screen.WorkspaceName))
	c.WriteText(2, 1, fmt.Sprintf("layout: %d pane(s)  active: %s", len(screen.Panes), activePaneTitle(screen.Panes)))
	c.WriteText(2, height-2, "c connect  p pool  d disconnect  r reconnect  ? help")

	bodyTop := 3
	bodyHeight := max(1, height-6)
	if len(screen.Panes) == 0 {
		c.DrawBox(2, bodyTop, width-4, bodyHeight, " empty workbench ")
		c.WriteText(4, bodyTop+2, "no panes available")
		return c.String()
	}

	paneRects := stackedPaneRects(len(screen.Panes), 2, bodyTop, width-4, bodyHeight)
	for index, pane := range screen.Panes {
		rect := paneRects[index]
		title := fmt.Sprintf(" %s %s ", activeMarker(index), pane.Title)
		c.DrawBox(rect.x, rect.y, rect.w, rect.h, title)
		c.WriteText(rect.x+2, rect.y+1, "state: "+paneStateLabel(pane.Status))
		if pane.Status == "unconnected" {
			c.WriteText(rect.x+2, rect.y+2, "actions: connect / create / pool")
		}
		bodyOffset := 2
		if pane.Status == "unconnected" {
			bodyOffset = 3
		}
		bodyLines := fitBodyLines(pane.Body, rect.w-4, rect.h-(bodyOffset+1))
		c.WriteBlock(rect.x+2, rect.y+bodyOffset, rect.w-4, rect.h-(bodyOffset+1), bodyLines)
	}
	return c.String()
}

type rect struct {
	x int
	y int
	w int
	h int
}

func stackedPaneRects(count, x, y, width, height int) []rect {
	if count <= 0 {
		return nil
	}
	rects := make([]rect, 0, count)
	remainingY := y
	remainingH := height
	for i := 0; i < count; i++ {
		left := count - i
		h := remainingH / left
		if h < 5 {
			h = 5
		}
		if i == count-1 || h > remainingH {
			h = remainingH
		}
		rects = append(rects, rect{x: x, y: remainingY, w: width, h: h})
		remainingY += h
		remainingH -= h
	}
	return rects
}

func fitBodyLines(body string, width, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	lines := strings.Split(body, "\n")
	out := make([]string, 0, height)
	for _, line := range lines {
		if len(out) >= height {
			break
		}
		if line == "" {
			out = append(out, "")
			continue
		}
		runes := []rune(line)
		for len(runes) > 0 && len(out) < height {
			if len(runes) <= width {
				out = append(out, string(runes))
				break
			}
			out = append(out, string(runes[:width]))
			runes = runes[width:]
		}
	}
	return out
}

func paneStateLabel(status string) string {
	switch status {
	case "live":
		return "live / connected"
	case "exited":
		return "exited / scrollback"
	case "unconnected":
		return "unconnected / waiting"
	default:
		return status
	}
}

func activePaneTitle(panes []projection.Pane) string {
	if len(panes) == 0 {
		return "-"
	}
	return panes[0].Title
}

func activeMarker(index int) string {
	if index == 0 {
		return "ACTIVE"
	}
	return "pane"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
