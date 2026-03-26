package terminalpool

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/tui/core/types"
	"github.com/lozzow/termx/tui/render/canvas"
	"github.com/lozzow/termx/tui/render/projection"
)

const (
	minPoolWidth  = 72
	minPoolHeight = 18
)

func Render(screen projection.Screen, width, height int) string {
	width = max(width, minPoolWidth)
	height = max(height, minPoolHeight)
	c := canvas.New(width, height)
	c.DrawBox(0, 0, width, height, " termx terminal pool ")
	c.WriteText(2, 1, "three-column wireframe: list / preview / details")
	c.WriteText(2, height-2, "j/k move  x kill  X remove  esc back  ? help")

	bodyTop := 3
	bodyHeight := height - 6
	leftW := max(24, width/3)
	middleW := max(20, width/3)
	rightW := width - 4 - leftW - middleW
	if rightW < 18 {
		rightW = 18
		middleW = max(20, width-4-leftW-rightW)
	}
	leftX := 2
	middleX := leftX + leftW
	rightX := middleX + middleW

	c.DrawBox(leftX, bodyTop, leftW, bodyHeight, " terminals ")
	c.DrawBox(middleX, bodyTop, middleW, bodyHeight, " preview ")
	c.DrawBox(rightX, bodyTop, rightW, bodyHeight, " details ")

	listLines := buildListLines(screen.Pool)
	c.WriteBlock(leftX+2, bodyTop+1, leftW-4, bodyHeight-2, listLines)

	selected, group := selectedItem(screen.Pool)
	previewLines := buildPreviewLines(selected, group, middleW-4, bodyHeight-2)
	c.WriteBlock(middleX+2, bodyTop+1, middleW-4, bodyHeight-2, previewLines)

	detailLines := buildDetailLines(selected, group)
	c.WriteBlock(rightX+2, bodyTop+1, rightW-4, bodyHeight-2, detailLines)

	return strings.TrimRight(c.String(), "\n")
}

func buildListLines(pool projection.TerminalPool) []string {
	var lines []string
	lines = append(lines, sectionLines("visible", pool.Visible, pool.SelectedTerminalID)...)
	lines = append(lines, "")
	lines = append(lines, sectionLines("parked", pool.Parked, pool.SelectedTerminalID)...)
	lines = append(lines, "")
	lines = append(lines, sectionLines("exited", pool.Exited, pool.SelectedTerminalID)...)
	return lines
}

func sectionLines(title string, items []projection.PoolItem, selected types.TerminalID) []string {
	lines := []string{fmt.Sprintf("%s (%d)", title, len(items))}
	if len(items) == 0 {
		return append(lines, "  - empty")
	}
	for _, item := range items {
		lines = append(lines, renderItem(item, selected))
	}
	return lines
}

func buildPreviewLines(item projection.PoolItem, group string, width, height int) []string {
	if height <= 0 {
		return nil
	}
	if item.ID == "" {
		return []string{"no terminal selected"}
	}
	lines := []string{
		"selected terminal",
		"",
		"name: " + item.Name,
		"state: " + item.State,
		"group: " + group,
		"",
		"snapshot preview pending",
		"use this panel for last screen / summary",
	}
	if len(lines) > height {
		return lines[:height]
	}
	return lines
}

func buildDetailLines(item projection.PoolItem, group string) []string {
	if item.ID == "" {
		return []string{"terminal id: -", "state: -", "actions: kill / remove"}
	}
	return []string{
		"terminal id: " + string(item.ID),
		"name: " + item.Name,
		"state: " + item.State,
		"bucket: " + group,
		"",
		"actions",
		"- enter connect from picker",
		"- x kill selected terminal",
		"- X remove exited terminal",
	}
}

func selectedItem(pool projection.TerminalPool) (projection.PoolItem, string) {
	for _, item := range pool.Visible {
		if item.ID == pool.SelectedTerminalID {
			return item, "visible"
		}
	}
	for _, item := range pool.Parked {
		if item.ID == pool.SelectedTerminalID {
			return item, "parked"
		}
	}
	for _, item := range pool.Exited {
		if item.ID == pool.SelectedTerminalID {
			return item, "exited"
		}
	}
	return projection.PoolItem{}, ""
}

func renderItem(item projection.PoolItem, selected types.TerminalID) string {
	prefix := "  "
	if item.ID == selected {
		prefix = "> "
	}
	return prefix + item.Name + " [" + item.State + "]"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
