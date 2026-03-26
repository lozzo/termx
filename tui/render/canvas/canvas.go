package canvas

import "strings"

type Canvas struct {
	width  int
	height int
	cells  [][]rune
}

func New(width, height int) *Canvas {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	c := &Canvas{
		width:  width,
		height: height,
		cells:  make([][]rune, height),
	}
	for y := 0; y < height; y++ {
		row := make([]rune, width)
		for x := 0; x < width; x++ {
			row[x] = ' '
		}
		c.cells[y] = row
	}
	return c
}

func (c *Canvas) WriteLine(row int, value string) {
	if c == nil {
		return
	}
	c.WriteText(0, row, value)
}

func (c *Canvas) WriteText(x, y int, value string) {
	if c == nil || y < 0 || y >= c.height || x >= c.width {
		return
	}
	if x < 0 {
		runes := []rune(value)
		if -x >= len(runes) {
			return
		}
		value = string(runes[-x:])
		x = 0
	}
	col := x
	for _, r := range value {
		if col >= c.width {
			break
		}
		c.cells[y][col] = r
		col++
	}
}

func (c *Canvas) WriteBlock(x, y, width, height int, lines []string) {
	if c == nil || width <= 0 || height <= 0 {
		return
	}
	for row := 0; row < height && row < len(lines); row++ {
		c.WriteText(x, y+row, truncate(lines[row], width))
	}
}

func (c *Canvas) FillRect(x, y, width, height int, r rune) {
	if c == nil || width <= 0 || height <= 0 {
		return
	}
	startX := max(0, x)
	startY := max(0, y)
	endX := min(c.width, x+width)
	endY := min(c.height, y+height)
	if startX >= endX || startY >= endY {
		return
	}
	for row := startY; row < endY; row++ {
		for col := startX; col < endX; col++ {
			c.cells[row][col] = r
		}
	}
}

func (c *Canvas) DrawBox(x, y, width, height int, title string) {
	if c == nil || width < 2 || height < 2 {
		return
	}
	startX := max(0, x)
	startY := max(0, y)
	endX := min(c.width, x+width)
	endY := min(c.height, y+height)
	if startX >= endX || startY >= endY || endX-startX < 2 || endY-startY < 2 {
		return
	}
	for col := startX + 1; col < endX-1; col++ {
		c.cells[startY][col] = '─'
		c.cells[endY-1][col] = '─'
	}
	for row := startY + 1; row < endY-1; row++ {
		c.cells[row][startX] = '│'
		c.cells[row][endX-1] = '│'
	}
	c.cells[startY][startX] = '┌'
	c.cells[startY][endX-1] = '┐'
	c.cells[endY-1][startX] = '└'
	c.cells[endY-1][endX-1] = '┘'
	if title != "" && endX-startX > 4 {
		label := " " + truncate(title, endX-startX-4) + " "
		c.WriteText(startX+2, startY, label)
	}
}

func (c *Canvas) String() string {
	if c == nil {
		return ""
	}
	lines := make([]string, 0, c.height)
	for _, row := range c.cells {
		lines = append(lines, strings.TrimRight(string(row), " "))
	}
	return strings.Join(lines, "\n")
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return string(runes[:1])
	}
	return string(runes[:width-1]) + "…"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
