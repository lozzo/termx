package render

import (
	xansi "github.com/charmbracelet/x/ansi"
	"strings"
)

type canvas struct {
	width  int
	height int
	rows   [][]canvasCell
}

type canvasCell struct {
	text         string
	width        int
	style        StyleToken
	ansiStyle    ANSICellStyle
	linkURL      string
	linkParams   string
	owner        string
	layer        LayerKind
	continuation bool
	safe         bool
}

func newCanvas(width int, height int) *canvas {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	rows := make([][]canvasCell, height)
	for i := range rows {
		rows[i] = make([]canvasCell, width)
	}
	return &canvas{width: width, height: height, rows: rows}
}

func (c *canvas) writeText(x int, y int, width int, text string) {
	c.writeTextStyled(x, y, width, text, "", "", LayerBase)
}

func (c *canvas) writeTextStyled(x int, y int, width int, text string, style StyleToken, owner string, layer LayerKind) {
	fitted := FitText(text, width)
	c.writeLine(x, y, width, Line{Cells: []Cell{{Text: fitted, Width: DisplayWidth(fitted), Style: style, Safe: true}}}, owner, layer)
}

func (c *canvas) overlayTextStyled(x int, y int, maxWidth int, text string, style StyleToken, owner string, layer LayerKind) {
	if y < 0 || y >= c.height || maxWidth <= 0 || x >= c.width {
		return
	}
	if x < 0 {
		text = SliceCells(text, -x, DisplayWidth(text))
		maxWidth += x
		x = 0
	}
	maxWidth = minInt(maxWidth, c.width-x)
	text = TruncateCells(text, maxWidth)
	if text == "" {
		return
	}
	cursor := x
	for _, segment := range cellSegments(text, style, owner, layer) {
		if cursor+segment.width > x+maxWidth {
			break
		}
		c.writeSegment(cursor, y, segment)
		cursor += segment.width
	}
}

func (c *canvas) writeLine(x int, y int, width int, line Line, owner string, layer LayerKind) {
	if y < 0 || y >= c.height || width <= 0 || x >= c.width {
		return
	}
	if x < 0 {
		width += x
		x = 0
	}
	width = minInt(width, c.width-x)
	if width <= 0 {
		return
	}
	c.clearCellRange(y, x, width)
	cursor := x
	remaining := width
	fillStyle := lineFillStyle(line)
	for _, cell := range line.Cells {
		if remaining <= 0 {
			break
		}
		text := xansi.Strip(SafeLine(cell.Text))
		if text == "" {
			continue
		}
		for len(text) > 0 && remaining > 0 {
			cluster, clusterWidth := xansi.FirstGraphemeCluster(text, xansi.GraphemeWidth)
			if cluster == "" {
				break
			}
			text = text[len(cluster):]
			if clusterWidth <= 0 {
				continue
			}
			if clusterWidth > remaining {
				break
			}
			c.writeSegmentNoClear(cursor, y, canvasSegment{
				text:       cluster,
				width:      clusterWidth,
				style:      cell.Style,
				ansiStyle:  cell.ANSIStyle,
				linkURL:    cell.LinkURL,
				linkParams: cell.LinkParams,
				owner:      owner,
				layer:      layer,
				safe:       cell.Safe,
			})
			cursor += clusterWidth
			remaining -= clusterWidth
		}
	}
	for remaining > 0 {
		c.writeSegmentNoClear(cursor, y, canvasSegment{
			text:  " ",
			width: 1,
			style: fillStyle,
			owner: owner,
			layer: layer,
			safe:  true,
		})
		cursor++
		remaining--
	}
}

func (c *canvas) writeBoxCell(x int, y int, glyph string) {
	c.writeTextStyled(x, y, 1, glyph, StyleMuted, "chrome", LayerChrome)
}

func (c *canvas) writeStyledBoxCell(x int, y int, glyph string, style StyleToken, owner string, layer LayerKind) {
	c.writeTextStyled(x, y, 1, glyph, style, owner, layer)
}

func (c *canvas) mergeBoxCell(x int, y int, connections uint8) {
	c.mergeStyledBoxCell(x, y, connections, StyleMuted, "chrome", LayerChrome)
}

func (c *canvas) mergeStyledBoxCell(x int, y int, connections uint8, style StyleToken, owner string, layer LayerKind) {
	if x < 0 || y < 0 || x >= c.width || y >= c.height {
		return
	}
	existing := c.cellText(x, y)
	if existingConnections, ok := boxConnectionsForGlyph(existing); ok {
		connections |= existingConnections
	}
	glyph, ok := boxGlyphForConnections(connections)
	if !ok {
		return
	}
	c.writeStyledBoxCell(x, y, glyph, style, owner, layer)
}

func (c *canvas) drawRoundedBox(rect Rect) {
	c.drawBox(rect, roundedBoxStyle)
}

func (c *canvas) fillStyledRect(rect Rect, style StyleToken, owner string, layer LayerKind) {
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		c.writeTextStyled(rect.X, y, rect.W, "", style, owner, layer)
	}
}

func (c *canvas) drawBox(rect Rect, style boxStyle) {
	c.drawStyledBox(rect, style, StyleMuted, "chrome", LayerChrome)
}

func (c *canvas) drawStyledBox(rect Rect, style boxStyle, token StyleToken, owner string, layer LayerKind) {
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	if rect.W == 1 {
		for y := 0; y < rect.H; y++ {
			c.writeStyledBoxCell(rect.X, rect.Y+y, style.Vertical, token, owner, layer)
		}
		return
	}
	if rect.H == 1 {
		c.writeTextStyled(rect.X, rect.Y, rect.W, style.TopLeft+strings.Repeat(style.Horizontal, maxInt(0, rect.W-2))+style.TopRight, token, owner, layer)
		return
	}
	c.writeTextStyled(rect.X, rect.Y, rect.W, style.TopLeft+strings.Repeat(style.Horizontal, maxInt(0, rect.W-2))+style.TopRight, token, owner, layer)
	c.writeTextStyled(rect.X, rect.Y+rect.H-1, rect.W, style.BottomLeft+strings.Repeat(style.Horizontal, maxInt(0, rect.W-2))+style.BottomRight, token, owner, layer)
	for y := rect.Y + 1; y < rect.Y+rect.H-1; y++ {
		c.writeStyledBoxCell(rect.X, y, style.Vertical, token, owner, layer)
		c.writeStyledBoxCell(rect.X+rect.W-1, y, style.Vertical, token, owner, layer)
	}
}

func (c *canvas) drawStyledPaneFrame(rect Rect, style StyleToken, owner string, layer LayerKind) {
	if rect.W < 2 || rect.H < 2 {
		return
	}
	// 参考 tuiv2 的 pane frame 语义：按连接位逐格绘制边框，再让标题槽位覆盖顶边内部。
	c.drawStyledPaneHBorder(rect.X, rect.X+rect.W-1, rect.Y, style, owner, layer, true)
	c.drawStyledPaneHBorder(rect.X, rect.X+rect.W-1, rect.Y+rect.H-1, style, owner, layer, false)
	c.drawStyledPaneVBorder(rect.X, rect.Y+1, rect.Y+rect.H-2, style, owner, layer)
	c.drawStyledPaneVBorder(rect.X+rect.W-1, rect.Y+1, rect.Y+rect.H-2, style, owner, layer)
}

func (c *canvas) drawStyledPaneHBorder(startX int, endX int, y int, style StyleToken, owner string, layer LayerKind, top bool) {
	if startX > endX {
		return
	}
	for x := startX; x <= endX; x++ {
		connections := uint8(boxConnLeft | boxConnRight)
		if x == startX {
			connections = boxConnRight
			if top {
				connections |= boxConnDown
			} else {
				connections |= boxConnUp
			}
		} else if x == endX {
			connections = boxConnLeft
			if top {
				connections |= boxConnDown
			} else {
				connections |= boxConnUp
			}
		}
		c.mergeStyledBoxCell(x, y, connections, style, owner, layer)
	}
}

func (c *canvas) drawStyledPaneVBorder(x int, startY int, endY int, style StyleToken, owner string, layer LayerKind) {
	if startY > endY {
		return
	}
	for y := startY; y <= endY; y++ {
		c.mergeStyledBoxCell(x, y, boxConnUp|boxConnDown, style, owner, layer)
	}
}

func (c *canvas) drawConnectedHLine(x int, y int, width int) {
	c.drawStyledConnectedHLine(x, y, width, StyleMuted, "chrome", LayerChrome)
}

func (c *canvas) drawStyledConnectedHLine(x int, y int, width int, style StyleToken, owner string, layer LayerKind) {
	if width <= 0 {
		return
	}
	for col := 0; col < width; col++ {
		connections := uint8(boxConnLeft | boxConnRight)
		if col == 0 {
			connections = boxConnRight
		}
		if col == width-1 {
			connections = boxConnLeft
		}
		if width == 1 {
			connections = boxConnLeft | boxConnRight
		}
		c.mergeStyledBoxCell(x+col, y, connections, style, owner, layer)
	}
}

func (c *canvas) drawConnectedVLine(x int, y int, height int) {
	c.drawStyledConnectedVLine(x, y, height, StyleMuted, "chrome", LayerChrome)
}

func (c *canvas) drawStyledConnectedVLine(x int, y int, height int, style StyleToken, owner string, layer LayerKind) {
	if height <= 0 {
		return
	}
	for row := 0; row < height; row++ {
		connections := uint8(boxConnUp | boxConnDown)
		if row == 0 {
			connections = boxConnDown
		}
		if row == height-1 {
			connections = boxConnUp
		}
		if height == 1 {
			connections = boxConnUp | boxConnDown
		}
		c.mergeStyledBoxCell(x, y+row, connections, style, owner, layer)
	}
}

func (c *canvas) lines() []Line {
	lines := make([]Line, len(c.rows))
	for i, row := range c.rows {
		cells := make([]Cell, 0, len(row))
		for _, cell := range row {
			if cell.continuation {
				continue
			}
			if cell.text == "" && cell.width == 0 {
				cells = append(cells, Cell{Text: " ", Width: 1, Safe: true})
				continue
			}
			width := cell.width
			if width <= 0 {
				width = 1
			}
			cells = append(cells, Cell{
				Text:       cell.text,
				Width:      width,
				Style:      cell.style,
				ANSIStyle:  cell.ansiStyle,
				LinkURL:    cell.linkURL,
				LinkParams: cell.linkParams,
				Safe:       cell.safe,
			})
		}
		lines[i] = Line{Cells: cells}
	}
	return lines
}

func (c *canvas) clearCellRange(y int, x int, width int) {
	for col := x; col < x+width && col < c.width; col++ {
		c.clearCell(y, col)
	}
}

func (c *canvas) clearCell(y int, x int) {
	if y < 0 || y >= c.height || x < 0 || x >= c.width {
		return
	}
	if c.rows[y][x].continuation {
		for col := x - 1; col >= 0; col-- {
			if !c.rows[y][col].continuation {
				c.clearCellFootprint(y, col)
				break
			}
		}
	}
	c.clearCellFootprint(y, x)
}

func (c *canvas) clearCellFootprint(y int, x int) {
	cell := c.rows[y][x]
	width := maxInt(1, cell.width)
	for col := x; col < x+width && col < c.width; col++ {
		c.rows[y][col] = blankCanvasCell()
	}
}

func (c *canvas) writeSegment(x int, y int, segment canvasSegment) {
	if y < 0 || y >= c.height || x < 0 || x >= c.width || segment.width <= 0 {
		return
	}
	if x+segment.width > c.width {
		return
	}
	c.clearCellRange(y, x, segment.width)
	c.writeSegmentNoClear(x, y, segment)
}

func (c *canvas) writeSegmentNoClear(x int, y int, segment canvasSegment) {
	if y < 0 || y >= c.height || x < 0 || x >= c.width || segment.width <= 0 {
		return
	}
	if x+segment.width > c.width {
		return
	}
	c.rows[y][x] = canvasCell{
		text:       segment.text,
		width:      segment.width,
		style:      segment.style,
		ansiStyle:  segment.ansiStyle,
		linkURL:    segment.linkURL,
		linkParams: segment.linkParams,
		owner:      segment.owner,
		layer:      segment.layer,
		safe:       segment.safe,
	}
	for col := x + 1; col < x+segment.width; col++ {
		c.rows[y][col] = canvasCell{
			width:        0,
			style:        segment.style,
			ansiStyle:    segment.ansiStyle,
			linkURL:      segment.linkURL,
			linkParams:   segment.linkParams,
			owner:        segment.owner,
			layer:        segment.layer,
			continuation: true,
			safe:         segment.safe,
		}
	}
}

func (c *canvas) cellText(x int, y int) string {
	if y < 0 || y >= c.height || x < 0 || x >= c.width {
		return ""
	}
	if c.rows[y][x].continuation {
		for col := x - 1; col >= 0; col-- {
			if !c.rows[y][col].continuation {
				return c.rows[y][col].text
			}
		}
		return ""
	}
	return c.rows[y][x].text
}

func lineFillStyle(line Line) StyleToken {
	return ""
}

func blankCanvasCell() canvasCell {
	return canvasCell{}
}
