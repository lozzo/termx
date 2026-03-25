package canvas

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tui/domain/types"
	"github.com/rivo/uniseg"
)

type DrawStyle struct {
	FG        string
	BG        string
	Bold      bool
	Italic    bool
	Underline bool
	Blink     bool
	Reverse   bool
	Strike    bool
}

type Cell struct {
	Content string
	Width   int
	Style   DrawStyle
}

type Canvas struct {
	width  int
	height int
	cells  [][]Cell
}

func New(width, height int) *Canvas {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}

	canvas := &Canvas{
		width:  width,
		height: height,
		cells:  make([][]Cell, height),
	}
	for y := 0; y < height; y++ {
		row := make([]Cell, width)
		for x := 0; x < width; x++ {
			row[x] = BlankCell()
		}
		canvas.cells[y] = row
	}
	return canvas
}

func BlankCell() Cell {
	return Cell{Content: " ", Width: 1}
}

func (c *Canvas) Fill(rect types.Rect, cell Cell) {
	if c == nil {
		return
	}
	rect, ok := c.clipRect(rect)
	if !ok {
		return
	}

	for y := rect.Y; y < rect.Y+rect.H; y++ {
		for x := rect.X; x < rect.X+rect.W; x++ {
			c.Set(x, y, cell)
		}
	}
}

func (c *Canvas) Set(x, y int, cell Cell) {
	if c == nil || x < 0 || y < 0 || x >= c.width || y >= c.height {
		return
	}

	cell = normalizeCell(cell)
	if x+cell.Width > c.width {
		return
	}

	// 先清掉当前位置上可能残留的宽字符占位，避免新内容被旧 continuation 污染。
	c.clearFootprint(y, x)
	maxX := x + cell.Width
	for i := x + 1; i < maxX; i++ {
		c.clearFootprint(y, i)
	}

	c.cells[y][x] = cell
	for i := 1; i < cell.Width && x+i < c.width; i++ {
		c.cells[y][x+i] = continuationCell()
	}
}

func (c *Canvas) DrawText(rect types.Rect, x, y int, text string, style DrawStyle) {
	if c == nil {
		return
	}
	rect, ok := c.clipRect(rect)
	if !ok {
		return
	}

	lines := strings.Split(text, "\n")
	for lineIndex, line := range lines {
		targetY := y + lineIndex
		if targetY < rect.Y || targetY >= rect.Y+rect.H {
			continue
		}
		if targetY < 0 || targetY >= c.height {
			continue
		}

		cursorX := x
		gr := uniseg.NewGraphemes(line)
		for gr.Next() {
			cluster := gr.Str()
			width := xansi.StringWidth(cluster)
			if width <= 0 {
				width = 1
			}

			if cursorX >= rect.X+rect.W {
				break
			}
			if cursorX >= rect.X && cursorX+width <= rect.X+rect.W {
				c.Set(cursorX, targetY, Cell{
					Content: cluster,
					Width:   width,
					Style:   style,
				})
			}
			cursorX += width
		}
	}
}

func (c *Canvas) Lines() []string {
	if c == nil {
		return nil
	}

	lines := make([]string, c.height)
	for y := 0; y < c.height; y++ {
		var row strings.Builder
		for x := 0; x < c.width; x++ {
			cell := c.cells[y][x]
			if isContinuationCell(cell) {
				continue
			}
			content := cell.Content
			if content == "" {
				content = " "
			}
			row.WriteString(content)
		}
		lines[y] = row.String()
	}
	return lines
}

func (c *Canvas) clipRect(rect types.Rect) (types.Rect, bool) {
	if rect.W <= 0 || rect.H <= 0 || c.width <= 0 || c.height <= 0 {
		return types.Rect{}, false
	}
	if rect.X < 0 {
		rect.W += rect.X
		rect.X = 0
	}
	if rect.Y < 0 {
		rect.H += rect.Y
		rect.Y = 0
	}
	if rect.X >= c.width || rect.Y >= c.height {
		return types.Rect{}, false
	}
	if rect.X+rect.W > c.width {
		rect.W = c.width - rect.X
	}
	if rect.Y+rect.H > c.height {
		rect.H = c.height - rect.Y
	}
	if rect.W <= 0 || rect.H <= 0 {
		return types.Rect{}, false
	}
	return rect, true
}

func (c *Canvas) clearFootprint(y, x int) {
	cell := c.cells[y][x]
	if isContinuationCell(cell) {
		start := x
		for start > 0 && isContinuationCell(c.cells[y][start]) {
			start--
		}
		c.blankSpan(y, start, c.cells[y][start].Width)
		return
	}
	c.blankSpan(y, x, cell.Width)
}

func (c *Canvas) blankSpan(y, start, width int) {
	if width <= 0 {
		width = 1
	}
	for i := 0; i < width && start+i < c.width; i++ {
		c.cells[y][start+i] = BlankCell()
	}
}

func normalizeCell(cell Cell) Cell {
	if cell.Content == "" {
		cell.Content = " "
		cell.Width = 1
	}
	actualWidth := xansi.StringWidth(cell.Content)
	if actualWidth > cell.Width {
		cell.Width = actualWidth
	}
	if cell.Width <= 0 {
		cell.Width = 1
	}
	return cell
}

func continuationCell() Cell {
	return Cell{}
}

func isContinuationCell(cell Cell) bool {
	return cell.Content == "" && cell.Width == 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
