package canvas

import "strings"

type Canvas struct {
	lines []string
}

func New(width, height int) *Canvas {
	if height < 0 {
		height = 0
	}
	return &Canvas{lines: make([]string, height)}
}

func (c *Canvas) WriteLine(row int, value string) {
	if c == nil || row < 0 || row >= len(c.lines) {
		return
	}
	c.lines[row] = value
}

func (c *Canvas) String() string {
	if c == nil {
		return ""
	}
	return strings.Join(c.lines, "\n")
}
