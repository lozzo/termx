package tui

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	localvterm "github.com/lozzow/termx/vterm"
	"github.com/rivo/uniseg"
)

type drawStyle struct {
	FG        string
	BG        string
	Bold      bool
	Italic    bool
	Underline bool
	Blink     bool
	Reverse   bool
	Strike    bool
}

type drawCell struct {
	Content      string
	Width        int
	Style        drawStyle
	Continuation bool
}

type composedCanvas struct {
	width  int
	height int
	cells  [][]drawCell
}

func newComposedCanvas(width, height int) *composedCanvas {
	c := &composedCanvas{
		width:  width,
		height: height,
		cells:  make([][]drawCell, height),
	}
	for y := 0; y < height; y++ {
		row := make([]drawCell, width)
		for x := 0; x < width; x++ {
			row[x] = blankDrawCell()
		}
		c.cells[y] = row
	}
	return c
}

func (m *Model) renderTabComposite(tab *Tab, width, height int) string {
	canvas := newComposedCanvas(width, height)
	rootRect := Rect{X: 0, Y: 0, W: width, H: height}
	rects := tab.Root.Rects(rootRect)
	if tab.ZoomedPaneID != "" {
		if _, ok := rects[tab.ZoomedPaneID]; ok {
			rects = map[string]Rect{tab.ZoomedPaneID: rootRect}
		}
	}
	for paneID, rect := range rects {
		pane := tab.Panes[paneID]
		canvas.drawPane(rect, pane, paneID == tab.ActivePaneID)
	}
	return canvas.String()
}

func blankDrawCell() drawCell {
	return drawCell{Content: " ", Width: 1}
}

func (c *composedCanvas) set(x, y int, cell drawCell) {
	if x < 0 || y < 0 || x >= c.width || y >= c.height {
		return
	}
	if cell.Width <= 0 {
		cell.Width = max(1, xansi.StringWidth(cell.Content))
	}
	if cell.Content == "" {
		cell.Content = " "
		cell.Width = 1
	}
	cell.Continuation = false
	c.cells[y][x] = cell
	for i := 1; i < cell.Width && x+i < c.width; i++ {
		c.cells[y][x+i] = drawCell{Continuation: true}
	}
}

func (c *composedCanvas) drawPane(rect Rect, pane *Pane, active bool) {
	if rect.W < 2 || rect.H < 2 {
		return
	}
	border := drawStyle{FG: "#475569"}
	titleStyle := drawStyle{FG: "#cbd5e1", BG: "#111827", Bold: true}
	if active {
		border = drawStyle{FG: "#22c55e", Bold: true}
		titleStyle = drawStyle{FG: "#ecfccb", BG: "#111827", Bold: true}
	}

	c.drawRectBorder(rect, paneTitle(pane), border, titleStyle)

	contentRect := Rect{X: rect.X + 1, Y: rect.Y + 1, W: rect.W - 2, H: rect.H - 2}
	c.fill(contentRect, blankDrawCell())
	c.drawGrid(contentRect, paneCells(pane))
	if active {
		c.drawCursor(contentRect, pane)
	}
}

func (c *composedCanvas) drawRectBorder(rect Rect, title string, borderStyle, titleStyle drawStyle) {
	for x := rect.X; x < rect.X+rect.W; x++ {
		c.set(x, rect.Y, drawCell{Content: "─", Width: 1, Style: borderStyle})
		c.set(x, rect.Y+rect.H-1, drawCell{Content: "─", Width: 1, Style: borderStyle})
	}
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		c.set(rect.X, y, drawCell{Content: "│", Width: 1, Style: borderStyle})
		c.set(rect.X+rect.W-1, y, drawCell{Content: "│", Width: 1, Style: borderStyle})
	}
	c.set(rect.X, rect.Y, drawCell{Content: "┌", Width: 1, Style: borderStyle})
	c.set(rect.X+rect.W-1, rect.Y, drawCell{Content: "┐", Width: 1, Style: borderStyle})
	c.set(rect.X, rect.Y+rect.H-1, drawCell{Content: "└", Width: 1, Style: borderStyle})
	c.set(rect.X+rect.W-1, rect.Y+rect.H-1, drawCell{Content: "┘", Width: 1, Style: borderStyle})

	if rect.W <= 2 {
		return
	}
	title = truncateTextToWidth(" "+title+" ", rect.W-2)
	for x, cell := range stringToDrawCells(title, titleStyle) {
		if x >= rect.W-2 {
			break
		}
		if cell.Continuation {
			continue
		}
		c.set(rect.X+1+x, rect.Y, cell)
	}
}

func (c *composedCanvas) drawGrid(rect Rect, grid [][]drawCell) {
	for y := 0; y < rect.H && y < len(grid); y++ {
		row := grid[y]
		for x, cell := range row {
			if x >= rect.W {
				break
			}
			if cell.Continuation {
				continue
			}
			c.set(rect.X+x, rect.Y+y, cell)
		}
	}
}

func (c *composedCanvas) drawCursor(rect Rect, pane *Pane) {
	cursor := paneCursor(pane)
	if !cursor.Visible || cursor.Row < 0 || cursor.Col < 0 {
		return
	}
	if cursor.Row >= rect.H || cursor.Col >= rect.W {
		return
	}

	x := rect.X + cursor.Col
	y := rect.Y + cursor.Row
	cell := c.cells[y][x]
	if cell.Continuation {
		for x > rect.X && c.cells[y][x].Continuation {
			x--
		}
		cell = c.cells[y][x]
	}
	if cell.Content == "" || cell.Continuation {
		cell = blankDrawCell()
	}

	cell.Style.Reverse = !cell.Style.Reverse
	switch cursor.Shape {
	case localvterm.CursorUnderline:
		cell.Style.Underline = true
	case localvterm.CursorBar:
		cell.Style.Bold = true
	}
	c.set(x, y, cell)
}

func (c *composedCanvas) drawText(rect Rect, lines []string, style drawStyle) {
	for y := 0; y < rect.H && y < len(lines); y++ {
		cells := stringToDrawCells(truncateTextToWidth(lines[y], rect.W), style)
		for x, cell := range cells {
			if x >= rect.W {
				break
			}
			if cell.Continuation {
				continue
			}
			c.set(rect.X+x, rect.Y+y, cell)
		}
	}
}

func (c *composedCanvas) fill(rect Rect, cell drawCell) {
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		for x := rect.X; x < rect.X+rect.W; x++ {
			c.set(x, y, cell)
		}
	}
}

func (c *composedCanvas) String() string {
	var out strings.Builder
	current := drawStyle{}
	for y := 0; y < c.height; y++ {
		if y > 0 {
			out.WriteByte('\n')
		}
		for x := 0; x < c.width; x++ {
			cell := c.cells[y][x]
			if cell.Continuation {
				continue
			}
			content := cell.Content
			if content == "" {
				content = " "
			}
			if current != cell.Style {
				out.WriteString(styleDiffANSI(current, cell.Style))
				current = cell.Style
			}
			out.WriteString(content)
		}
		out.WriteString("\x1b[0m")
		current = drawStyle{}
	}
	return out.String()
}

func paneCells(pane *Pane) [][]drawCell {
	if pane == nil {
		return nil
	}
	if !pane.renderDirty && pane.cellCache != nil {
		return pane.cellCache
	}

	var grid [][]drawCell
	switch {
	case pane.live:
		grid = convertVTermGrid(pane.VTerm.ScreenContent().Cells)
	case snapshotHasVisibleContent(pane.Snapshot):
		grid = convertProtocolGrid(pane.Snapshot.Screen.Cells)
	default:
		grid = textLinesToGrid(welcomePaneLines(pane))
	}

	pane.cellCache = grid
	pane.renderDirty = false
	return grid
}

func paneCursor(pane *Pane) localvterm.CursorState {
	if pane == nil {
		return localvterm.CursorState{}
	}
	if pane.live && pane.VTerm != nil {
		return pane.VTerm.CursorState()
	}
	if pane.Snapshot != nil {
		return localvterm.CursorState{
			Row:     pane.Snapshot.Cursor.Row,
			Col:     pane.Snapshot.Cursor.Col,
			Visible: pane.Snapshot.Cursor.Visible,
			Shape:   localvterm.CursorShape(pane.Snapshot.Cursor.Shape),
			Blink:   pane.Snapshot.Cursor.Blink,
		}
	}
	return localvterm.CursorState{}
}

func rowToANSI(row []drawCell) string {
	var b strings.Builder
	current := drawStyle{}
	for _, cell := range row {
		if cell.Continuation {
			continue
		}
		if current != cell.Style {
			b.WriteString(styleDiffANSI(current, cell.Style))
			current = cell.Style
		}
		content := cell.Content
		if content == "" {
			content = " "
		}
		b.WriteString(content)
	}
	if current != (drawStyle{}) {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

func snapshotHasVisibleContent(snap *protocol.Snapshot) bool {
	if snap == nil {
		return false
	}
	for _, row := range snap.Screen.Cells {
		for _, cell := range row {
			if cell.Content != "" || cell.Style != (protocol.CellStyle{}) {
				return true
			}
		}
	}
	return false
}

func convertVTermGrid(rows [][]localvterm.Cell) [][]drawCell {
	out := make([][]drawCell, len(rows))
	for y, row := range rows {
		out[y] = make([]drawCell, len(row))
		for x, cell := range row {
			continuation := cell.Content == "" && cell.Width == 0
			out[y][x] = drawCell{
				Content:      cell.Content,
				Width:        max(1, cell.Width),
				Continuation: continuation,
				Style: drawStyle{
					FG:        cell.Style.FG,
					BG:        cell.Style.BG,
					Bold:      cell.Style.Bold,
					Italic:    cell.Style.Italic,
					Underline: cell.Style.Underline,
					Blink:     cell.Style.Blink,
					Reverse:   cell.Style.Reverse,
					Strike:    cell.Style.Strikethrough,
				},
			}
		}
	}
	return out
}

func convertProtocolGrid(rows [][]protocol.Cell) [][]drawCell {
	out := make([][]drawCell, len(rows))
	for y, row := range rows {
		out[y] = make([]drawCell, len(row))
		for x, cell := range row {
			width := cell.Width
			continuation := cell.Content == "" && width == 0
			if width == 0 {
				width = max(1, xansi.StringWidth(cell.Content))
			}
			out[y][x] = drawCell{
				Content:      cell.Content,
				Width:        max(1, width),
				Continuation: continuation,
				Style: drawStyle{
					FG:        cell.Style.FG,
					BG:        cell.Style.BG,
					Bold:      cell.Style.Bold,
					Italic:    cell.Style.Italic,
					Underline: cell.Style.Underline,
					Blink:     cell.Style.Blink,
					Reverse:   cell.Style.Reverse,
					Strike:    cell.Style.Strikethrough,
				},
			}
		}
	}
	return out
}

func textLinesToGrid(lines []string) [][]drawCell {
	out := make([][]drawCell, len(lines))
	for y, line := range lines {
		out[y] = stringToDrawCells(line, drawStyle{})
	}
	return out
}

type textCluster struct {
	Content string
	Width   int
}

func splitTextClusters(s string) []textCluster {
	graphemes := uniseg.NewGraphemes(s)
	out := make([]textCluster, 0, len(s))
	lastBase := -1
	for graphemes.Next() {
		cluster := graphemes.Str()
		width := xansi.StringWidth(cluster)
		if width <= 0 {
			if lastBase >= 0 {
				out[lastBase].Content += cluster
				continue
			}
			width = 1
		}
		out = append(out, textCluster{Content: cluster, Width: width})
		lastBase = len(out) - 1
	}
	return out
}

func stringToDrawCells(s string, style drawStyle) []drawCell {
	clusters := splitTextClusters(s)
	cells := make([]drawCell, 0, len(clusters))
	for _, cluster := range clusters {
		cells = append(cells, drawCell{
			Content: cluster.Content,
			Width:   cluster.Width,
			Style:   style,
		})
		for i := 1; i < cluster.Width; i++ {
			cells = append(cells, drawCell{Continuation: true})
		}
	}
	return cells
}

func truncateTextToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}

	var out strings.Builder
	used := 0
	for _, cluster := range splitTextClusters(s) {
		if used+cluster.Width > width {
			break
		}
		out.WriteString(cluster.Content)
		used += cluster.Width
	}
	return out.String()
}

func paneTitle(pane *Pane) string {
	if pane == nil {
		return "pane"
	}
	return coalesce(pane.Title, pane.TerminalID, pane.ID)
}

func welcomePaneLines(pane *Pane) []string {
	title := paneTitle(pane)
	return []string{
		"termx interactive pane",
		"",
		"connected terminal: " + title,
		"",
		"type normally to send input to the shell",
		"press Ctrl-a then % to split vertically",
		"press Ctrl-a then \" to split horizontally",
		"press Ctrl-a then c to open a new tab",
		"press Ctrl-a then ? for the full cheat sheet",
	}
}

func styleDiffANSI(from, to drawStyle) string {
	if from == to {
		return ""
	}
	if to == (drawStyle{}) {
		return "\x1b[0m"
	}
	codes := make([]string, 0, 12)
	codes = append(codes, "0")
	if to.FG != "" {
		if rgb, ok := hexToRGB(to.FG); ok {
			codes = append(codes, "38", "2", itoa(rgb[0]), itoa(rgb[1]), itoa(rgb[2]))
		}
	}
	if to.BG != "" {
		if rgb, ok := hexToRGB(to.BG); ok {
			codes = append(codes, "48", "2", itoa(rgb[0]), itoa(rgb[1]), itoa(rgb[2]))
		}
	}
	if to.Bold {
		codes = append(codes, "1")
	}
	if to.Italic {
		codes = append(codes, "3")
	}
	if to.Underline {
		codes = append(codes, "4")
	}
	if to.Blink {
		codes = append(codes, "5")
	}
	if to.Reverse {
		codes = append(codes, "7")
	}
	if to.Strike {
		codes = append(codes, "9")
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
