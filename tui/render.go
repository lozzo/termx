package tui

import (
	"strconv"
	"strings"
	"sync"

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
	width     int
	height    int
	cells     [][]drawCell
	rowCache  []string
	rowDirty  []bool
	fullCache string
	fullDirty bool
}

type paneRenderEntry struct {
	PaneID   string
	Rect     Rect
	Title    string
	Meta     string
	Floating bool
}

var styleANSICache sync.Map
var borderTitleCellCache sync.Map
var blankFillRowCache = struct {
	mu   sync.RWMutex
	rows map[int][]drawCell
}{
	rows: make(map[int][]drawCell),
}

func newComposedCanvas(width, height int) *composedCanvas {
	c := &composedCanvas{
		width:     width,
		height:    height,
		cells:     make([][]drawCell, height),
		rowCache:  make([]string, height),
		rowDirty:  make([]bool, height),
		fullDirty: true,
	}
	for y := 0; y < height; y++ {
		row := make([]drawCell, width)
		for x := 0; x < width; x++ {
			row[x] = blankDrawCell()
		}
		c.cells[y] = row
		c.rowDirty[y] = true
	}
	return c
}

func (m *Model) renderTabComposite(tab *Tab, width, height int) string {
	entries := m.paneRenderEntries(tab, width, height)
	rects := make(map[string]Rect, len(entries))
	order := make([]string, 0, len(entries))
	for _, entry := range entries {
		rects[entry.PaneID] = entry.Rect
		order = append(order, entry.PaneID)
	}

	cache := tab.renderCache
	if cache == nil || cache.width != width || cache.height != height || cache.zoomedPaneID != tab.ZoomedPaneID || !sameOrder(cache.order, order) {
		canvas := newComposedCanvas(width, height)
		frameKeys := make(map[string]string, len(rects))
		for _, entry := range entries {
			pane := tab.Panes[entry.PaneID]
			active := entry.PaneID == tab.ActivePaneID
			frameKey := paneFrameKey(entry.Title, entry.Meta, active, entry.Floating, entry.Rect)
			canvas.drawPaneWithTitleFull(entry.Rect, pane, entry.Title, entry.Meta, active, entry.Floating)
			frameKeys[entry.PaneID] = frameKey
		}
		tab.renderCache = &tabRenderCache{
			canvas:       canvas,
			rects:        cloneRects(rects),
			order:        append([]string(nil), order...),
			frameKeys:    frameKeys,
			width:        width,
			height:       height,
			activePaneID: tab.ActivePaneID,
			zoomedPaneID: tab.ZoomedPaneID,
		}
		return canvas.String()
	}
	if !sameRects(cache.rects, rects) {
		if !floatingRectsChanged(cache.rects, rects, entries) {
			if damage, ok := incrementalRectDamage(cache.rects, rects); ok {
				cache.canvas.redrawDamage(entries, tab.Panes, tab.ActivePaneID, damage)
				cache.rects = cloneRects(rects)
				cache.activePaneID = tab.ActivePaneID
				return cache.canvas.String()
			}
		}
		canvas := newComposedCanvas(width, height)
		frameKeys := make(map[string]string, len(rects))
		for _, entry := range entries {
			pane := tab.Panes[entry.PaneID]
			active := entry.PaneID == tab.ActivePaneID
			frameKey := paneFrameKey(entry.Title, entry.Meta, active, entry.Floating, entry.Rect)
			canvas.drawPaneWithTitleFull(entry.Rect, pane, entry.Title, entry.Meta, active, entry.Floating)
			frameKeys[entry.PaneID] = frameKey
		}
		tab.renderCache = &tabRenderCache{
			canvas:       canvas,
			rects:        cloneRects(rects),
			order:        append([]string(nil), order...),
			frameKeys:    frameKeys,
			width:        width,
			height:       height,
			activePaneID: tab.ActivePaneID,
			zoomedPaneID: tab.ZoomedPaneID,
		}
		return canvas.String()
	}

	damage := make([]Rect, 0, len(entries))
	for _, entry := range entries {
		pane := tab.Panes[entry.PaneID]
		active := entry.PaneID == tab.ActivePaneID
		nextFrameKey := paneFrameKey(entry.Title, entry.Meta, active, entry.Floating, entry.Rect)
		overlapped := overlapsAnyRect(entry.Rect, damage)
		if cache.frameKeys[entry.PaneID] != nextFrameKey || overlapped {
			cache.canvas.drawPaneWithTitleFull(entry.Rect, pane, entry.Title, entry.Meta, active, entry.Floating)
			cache.frameKeys[entry.PaneID] = nextFrameKey
			damage = append(damage, entry.Rect)
			continue
		}
		if pane == nil || !pane.IsRenderDirty() {
			continue
		}
		cache.canvas.drawPaneBody(entry.Rect, pane, active)
		damage = append(damage, entry.Rect)
	}
	cache.activePaneID = tab.ActivePaneID
	return cache.canvas.String()
}

func (c *composedCanvas) redrawDamage(entries []paneRenderEntry, panes map[string]*Pane, activePaneID string, damage []Rect) {
	if c == nil || len(damage) == 0 {
		return
	}
	for _, rect := range damage {
		c.fill(rect, blankDrawCell())
	}
	for _, entry := range entries {
		if !overlapsAnyRect(entry.Rect, damage) {
			continue
		}
		c.drawPaneWithTitleFull(entry.Rect, panes[entry.PaneID], entry.Title, entry.Meta, entry.PaneID == activePaneID, entry.Floating)
	}
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
	if c.cells[y][x] != cell {
		c.rowDirty[y] = true
		c.fullDirty = true
	}
	c.cells[y][x] = cell
	for i := 1; i < cell.Width && x+i < c.width; i++ {
		cont := drawCell{Continuation: true}
		if c.cells[y][x+i] != cont {
			c.rowDirty[y] = true
			c.fullDirty = true
		}
		c.cells[y][x+i] = cont
	}
}

func (c *composedCanvas) drawPane(rect Rect, pane *Pane, active bool) {
	c.drawPaneWithTitle(rect, pane, paneTitle(pane), "", active, false)
}

func (c *composedCanvas) drawPaneWithTitle(rect Rect, pane *Pane, title string, meta string, active bool, floating bool) {
	if rect.W < 2 || rect.H < 2 {
		return
	}
	c.drawPaneFrameWithTitle(rect, title, meta, active, floating)
	c.drawPaneBody(rect, pane, active)
}

func (c *composedCanvas) drawPaneWithTitleFull(rect Rect, pane *Pane, title string, meta string, active bool, floating bool) {
	if rect.W < 2 || rect.H < 2 {
		return
	}
	c.drawPaneFrameWithTitle(rect, title, meta, active, floating)
	c.drawPaneBodyFull(rect, pane, active)
}

func (c *composedCanvas) drawPaneFrame(rect Rect, pane *Pane, active bool) {
	c.drawPaneFrameWithTitle(rect, paneTitle(pane), "", active, false)
}

func (c *composedCanvas) drawPaneFrameWithTitle(rect Rect, title string, meta string, active bool, floating bool) {
	if rect.W < 2 || rect.H < 2 {
		return
	}
	border := drawStyle{FG: "#d1d5db"}
	titleStyle := drawStyle{FG: "#e5e7eb", Bold: true}
	if active {
		border = drawStyle{FG: "#4ade80", Bold: true}
		titleStyle = drawStyle{FG: "#f0fdf4", Bold: true}
	} else if floating {
		titleStyle = drawStyle{FG: "#f3f4f6", Bold: true}
	}
	c.drawRectBorder(rect, title, meta, border, titleStyle)
}

func (c *composedCanvas) drawPaneBody(rect Rect, pane *Pane, active bool) {
	if rect.W < 2 || rect.H < 2 {
		return
	}
	contentRect := Rect{X: rect.X + 1, Y: rect.Y + 1, W: rect.W - 2, H: rect.H - 2}
	if pane == nil || (active && pane.live) || !c.drawPaneBodyDirtyRows(contentRect, pane) {
		c.fill(contentRect, blankDrawCell())
		if !c.drawPaneSource(contentRect, pane) {
			c.drawGrid(contentRect, paneCellsForViewport(pane, contentRect.W, contentRect.H))
			if pane != nil {
				pane.clearDirtyRegion()
			}
		}
	}
	if active {
		c.drawCursor(contentRect, pane, contentRect.W, contentRect.H)
	}
}

func (c *composedCanvas) drawPaneBodyFull(rect Rect, pane *Pane, active bool) {
	if rect.W < 2 || rect.H < 2 {
		return
	}
	contentRect := Rect{X: rect.X + 1, Y: rect.Y + 1, W: rect.W - 2, H: rect.H - 2}
	c.fill(contentRect, blankDrawCell())
	if !c.drawPaneSource(contentRect, pane) {
		c.drawGrid(contentRect, paneCellsForViewport(pane, contentRect.W, contentRect.H))
		if pane != nil {
			pane.clearDirtyRegion()
		}
	}
	if active {
		c.drawCursor(contentRect, pane, contentRect.W, contentRect.H)
	}
}

func (c *composedCanvas) drawPaneBodyDirtyRows(contentRect Rect, pane *Pane) bool {
	rowStart, rowEnd, rowsKnown := pane.DirtyRows()
	if pane == nil || !rowsKnown || !pane.IsRenderDirty() {
		return false
	}
	switch {
	case pane.live && pane.VTerm != nil:
		if pane.VTerm.IsAltScreen() {
			return false
		}
	case snapshotHasVisibleContent(pane.Snapshot):
	default:
		return false
	}
	offset := paneVisibleOffset(pane)
	start := max(rowStart-offset.Y, 0)
	end := min(rowEnd-offset.Y, contentRect.H-1)
	if start > end {
		pane.clearDirtyRegion()
		pane.ClearRenderDirty()
		return true
	}
	for row := start; row <= end; row++ {
		rowRect := Rect{X: contentRect.X, Y: contentRect.Y + row, W: contentRect.W, H: 1}
		viewColStart := 0
		colStart, colEnd, colsKnown := pane.DirtyCols()
		if colsKnown && rowStart == rowEnd && row == start {
			colStart = max(colStart-offset.X, 0)
			colEnd = min(colEnd-offset.X, contentRect.W-1)
			if colStart <= colEnd {
				viewColStart = colStart
				rowRect.X += colStart
				rowRect.W = colEnd - colStart + 1
			}
		}
		c.fill(rowRect, blankDrawCell())
		c.drawPaneSourceRow(rowRect, pane, row, viewColStart)
	}
	pane.ClearRenderDirty()
	pane.clearDirtyRegion()
	return true
}

func (c *composedCanvas) drawPaneSource(rect Rect, pane *Pane) bool {
	if pane == nil {
		return false
	}
	offset := paneVisibleOffset(pane)
	switch {
	case pane.live && pane.VTerm != nil:
		contentW, contentH := pane.VTerm.Size()
		c.drawSourceRegion(rect, offset, contentW, contentH, func(x, y int) drawCell {
			return normalizePaneDrawCellStyle(pane, drawCellFromVTermCell(pane.VTerm.CellAt(x, y)))
		})
		pane.ClearRenderDirty()
		return true
	case snapshotHasVisibleContent(pane.Snapshot):
		rows := pane.Snapshot.Screen.Cells
		c.drawSourceRegion(rect, offset, 0, len(rows), func(x, y int) drawCell {
			row := rows[y]
			if x < 0 || x >= len(row) {
				return drawCell{}
			}
			return normalizePaneDrawCellStyle(pane, drawCellFromProtocolCell(row[x]))
		})
		pane.ClearRenderDirty()
		return true
	default:
		return false
	}
}

func (c *composedCanvas) drawPaneSourceRow(rect Rect, pane *Pane, viewRow, viewColStart int) bool {
	if pane == nil {
		return false
	}
	offset := paneVisibleOffset(pane)
	rowOffset := Point{X: offset.X + viewColStart, Y: offset.Y + viewRow}
	switch {
	case pane.live && pane.VTerm != nil:
		contentW, contentH := pane.VTerm.Size()
		c.drawSourceRegion(rect, rowOffset, contentW, contentH, func(x, y int) drawCell {
			return normalizePaneDrawCellStyle(pane, drawCellFromVTermCell(pane.VTerm.CellAt(x, y)))
		})
		return true
	case snapshotHasVisibleContent(pane.Snapshot):
		rows := pane.Snapshot.Screen.Cells
		c.drawSourceRegion(rect, rowOffset, 0, len(rows), func(x, y int) drawCell {
			row := rows[y]
			if x < 0 || x >= len(row) {
				return drawCell{}
			}
			return normalizePaneDrawCellStyle(pane, drawCellFromProtocolCell(row[x]))
		})
		return true
	default:
		return false
	}
}

func (c *composedCanvas) drawRectBorder(rect Rect, title string, meta string, borderStyle, titleStyle drawStyle) {
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
	c.drawBorderTopLabels(rect, title, meta, titleStyle)
}

func (c *composedCanvas) drawBorderTopLabels(rect Rect, title string, meta string, style drawStyle) {
	innerW := rect.W - 2
	if innerW <= 0 {
		return
	}
	title = strings.TrimSpace(title)
	meta = strings.TrimSpace(meta)
	if title == "" && meta == "" {
		return
	}

	leftX := rect.X + 2
	rightLimit := rect.X + rect.W - 2
	available := max(0, rightLimit-leftX)
	titleCells := borderTitleCells(" "+title+" ", available, style)
	metaCells := borderTitleCells(" "+meta+" ", available, style)
	titleWidth := drawCellsWidth(titleCells)
	metaWidth := drawCellsWidth(metaCells)
	if titleWidth > 0 && metaWidth > 0 && titleWidth+metaWidth >= available {
		remainingMetaWidth := available - titleWidth - 1
		if remainingMetaWidth < 6 {
			metaCells = nil
			metaWidth = 0
		} else {
			metaCells = borderTitleCells(" "+meta+" ", remainingMetaWidth, style)
			metaWidth = drawCellsWidth(metaCells)
		}
	}

	if len(titleCells) > 0 {
		for x, cell := range titleCells {
			if cell.Continuation {
				continue
			}
			drawX := leftX + x
			if drawX >= rightLimit {
				break
			}
			c.set(drawX, rect.Y, cell)
		}
	}
	if len(metaCells) == 0 {
		return
	}
	metaStart := rect.X + rect.W - 2 - metaWidth
	if metaStart <= leftX {
		available := max(1, metaStart-leftX-1)
		titleCells = borderTitleCells(" "+title+" ", available, style)
		if len(titleCells) > 0 {
			for x, cell := range titleCells {
				if cell.Continuation {
					continue
				}
				drawX := leftX + x
				if drawX >= metaStart-1 {
					break
				}
				c.set(drawX, rect.Y, cell)
			}
		}
	}
	for x, cell := range metaCells {
		if cell.Continuation {
			continue
		}
		drawX := metaStart + x
		if drawX >= rightLimit {
			break
		}
		c.set(drawX, rect.Y, cell)
	}
}

type borderTitleCacheKey struct {
	Text  string
	Width int
	Style drawStyle
}

func borderTitleCells(text string, maxWidth int, style drawStyle) []drawCell {
	if maxWidth <= 0 {
		return nil
	}
	key := borderTitleCacheKey{
		Text:  truncateTextToWidth(text, maxWidth),
		Width: maxWidth,
		Style: style,
	}
	if cached, ok := borderTitleCellCache.Load(key); ok {
		return cached.([]drawCell)
	}
	cells := stringToDrawCells(key.Text, style)
	borderTitleCellCache.Store(key, cells)
	return cells
}

func drawCellsWidth(cells []drawCell) int {
	width := 0
	for _, cell := range cells {
		if cell.Continuation {
			continue
		}
		width += max(1, cell.Width)
	}
	return width
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

func (c *composedCanvas) drawVTermRegion(rect Rect, vt *localvterm.VTerm, offset Point) {
	if vt == nil {
		return
	}
	contentW, contentH := vt.Size()
	c.drawSourceRegion(rect, offset, contentW, contentH, func(x, y int) drawCell {
		return drawCellFromVTermCell(vt.CellAt(x, y))
	})
}

func (c *composedCanvas) drawProtocolRegion(rect Rect, rows [][]protocol.Cell, offset Point) {
	contentH := len(rows)
	c.drawSourceRegion(rect, offset, 0, contentH, func(x, y int) drawCell {
		row := rows[y]
		if x < 0 || x >= len(row) {
			return drawCell{}
		}
		return drawCellFromProtocolCell(row[x])
	})
}

func (c *composedCanvas) drawSourceRegion(rect Rect, offset Point, contentW, contentH int, cellAt func(x, y int) drawCell) {
	for y := 0; y < rect.H; y++ {
		srcY := offset.Y + y
		if srcY < 0 || srcY >= contentH {
			continue
		}
		for x := 0; x < rect.W; x++ {
			srcX := offset.X + x
			if contentW > 0 && (srcX < 0 || srcX >= contentW) {
				continue
			}
			cell := cellAt(srcX, srcY)
			if cell.Continuation {
				continue
			}
			if cell.Content == "" {
				if cell.Style == (drawStyle{}) {
					continue
				}
				cell = blankDrawCell()
			}
			if cell.Width <= 0 {
				cell.Width = max(1, xansi.StringWidth(cell.Content))
			}
			if x+cell.Width > rect.W {
				continue
			}
			c.set(rect.X+x, rect.Y+y, cell)
		}
	}
}

func (c *composedCanvas) drawCursor(rect Rect, pane *Pane, viewW, viewH int) {
	cursor := paneCursorForViewport(pane, viewW, viewH)
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

func (c *composedCanvas) drawPaneCursorOnly(rect Rect, pane *Pane, active bool) {
	if rect.W < 2 || rect.H < 2 || pane == nil {
		return
	}
	contentRect := Rect{X: rect.X + 1, Y: rect.Y + 1, W: rect.W - 2, H: rect.H - 2}
	cursor := paneCursorForViewport(pane, contentRect.W, contentRect.H)
	if !cursor.Visible || cursor.Row < 0 || cursor.Col < 0 || cursor.Row >= contentRect.H || cursor.Col >= contentRect.W {
		return
	}
	cell, startCol, ok := paneVisibleCell(pane, cursor.Col, cursor.Row, contentRect.W, contentRect.H)
	if !ok {
		cell = blankDrawCell()
		startCol = cursor.Col
	}
	if active {
		cell.Style.Reverse = !cell.Style.Reverse
		switch cursor.Shape {
		case localvterm.CursorUnderline:
			cell.Style.Underline = true
		case localvterm.CursorBar:
			cell.Style.Bold = true
		}
	}
	c.set(contentRect.X+startCol, contentRect.Y+cursor.Row, cell)
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
	if rect.W <= 0 || rect.H <= 0 {
		return
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
		return
	}
	rect.W = min(rect.W, c.width-rect.X)
	rect.H = min(rect.H, c.height-rect.Y)
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	if cell == blankDrawCell() {
		row := cachedBlankFillRow(rect.W)
		for y := rect.Y; y < rect.Y+rect.H; y++ {
			copy(c.cells[y][rect.X:rect.X+rect.W], row)
			c.rowDirty[y] = true
		}
		c.fullDirty = true
		return
	}
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		for x := rect.X; x < rect.X+rect.W; x++ {
			c.set(x, y, cell)
		}
	}
}

func cachedBlankFillRow(width int) []drawCell {
	if width <= 0 {
		return nil
	}
	blankFillRowCache.mu.RLock()
	row := blankFillRowCache.rows[width]
	blankFillRowCache.mu.RUnlock()
	if row != nil {
		return row
	}
	row = make([]drawCell, width)
	row[0] = blankDrawCell()
	for filled := 1; filled < width; filled *= 2 {
		copy(row[filled:], row[:min(filled, width-filled)])
	}
	blankFillRowCache.mu.Lock()
	if cached := blankFillRowCache.rows[width]; cached != nil {
		row = cached
	} else {
		blankFillRowCache.rows[width] = row
	}
	blankFillRowCache.mu.Unlock()
	return row
}

func (c *composedCanvas) blit(src *composedCanvas, dstX, dstY int) {
	if c == nil || src == nil {
		return
	}
	for y := 0; y < src.height; y++ {
		targetY := dstY + y
		if targetY < 0 || targetY >= c.height {
			continue
		}
		startX := max(0, dstX)
		endX := min(c.width, dstX+src.width)
		if startX >= endX {
			continue
		}
		srcStart := startX - dstX
		srcEnd := srcStart + (endX - startX)
		copy(c.cells[targetY][startX:endX], src.cells[y][srcStart:srcEnd])
		c.rowDirty[targetY] = true
		c.fullDirty = true
	}
}

func (c *composedCanvas) String() string {
	if !c.fullDirty && c.fullCache != "" {
		return c.fullCache
	}
	for y := 0; y < c.height; y++ {
		if !c.rowDirty[y] && c.rowCache[y] != "" {
			continue
		}
		var row strings.Builder
		current := drawStyle{}
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
				row.WriteString(styleDiffANSI(current, cell.Style))
				current = cell.Style
			}
			row.WriteString(content)
		}
		row.WriteString("\x1b[0m")
		c.rowCache[y] = row.String()
		c.rowDirty[y] = false
	}

	var out strings.Builder
	for y := 0; y < c.height; y++ {
		if y > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(c.rowCache[y])
	}
	c.fullCache = out.String()
	c.fullDirty = false
	return c.fullCache
}

func paneFrameKey(title string, meta string, active bool, floating bool, rect Rect) string {
	if title == "" && meta == "" {
		if active {
			if floating {
				return "active:floating:nil:" + itoa(rect.W) + ":" + itoa(rect.H)
			}
			return "active:nil:" + itoa(rect.W) + ":" + itoa(rect.H)
		}
		if floating {
			return "inactive:floating:nil:" + itoa(rect.W) + ":" + itoa(rect.H)
		}
		return "inactive:nil:" + itoa(rect.W) + ":" + itoa(rect.H)
	}
	state := "inactive"
	if active {
		state = "active"
	}
	layer := "tiled"
	if floating {
		layer = "floating"
	}
	return state + ":" + layer + ":" + title + ":" + meta + ":" + itoa(rect.W) + ":" + itoa(rect.H)
}

func sameRects(a, b map[string]Rect) bool {
	if len(a) != len(b) {
		return false
	}
	for key, rect := range a {
		if other, ok := b[key]; !ok || other != rect {
			return false
		}
	}
	return true
}

func cloneRects(src map[string]Rect) map[string]Rect {
	out := make(map[string]Rect, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func incrementalRectDamage(previous, next map[string]Rect) ([]Rect, bool) {
	if len(previous) != len(next) {
		return nil, false
	}
	damage := make([]Rect, 0, len(previous)*2)
	for paneID, prev := range previous {
		current, ok := next[paneID]
		if !ok {
			return nil, false
		}
		if prev == current {
			continue
		}
		damage = append(damage, prev, current)
	}
	for paneID := range next {
		if _, ok := previous[paneID]; !ok {
			return nil, false
		}
	}
	return damage, true
}

func floatingRectsChanged(previous, next map[string]Rect, entries []paneRenderEntry) bool {
	for _, entry := range entries {
		if !entry.Floating {
			continue
		}
		prev, ok := previous[entry.PaneID]
		if !ok {
			return true
		}
		if current, ok := next[entry.PaneID]; !ok || prev != current {
			return true
		}
	}
	return false
}

func sameOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func overlapsAnyRect(rect Rect, others []Rect) bool {
	for _, other := range others {
		if rectsOverlap(rect, other) {
			return true
		}
	}
	return false
}

func rectsOverlap(a, b Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func (m *Model) paneRenderEntries(tab *Tab, width, height int) []paneRenderEntry {
	if tab == nil {
		return nil
	}
	rootRect := Rect{X: 0, Y: 0, W: width, H: height}
	if paneID, rect, ok := m.zoomedPaneRect(tab, rootRect); ok {
		pane := tab.Panes[paneID]
		return []paneRenderEntry{{
			PaneID:   paneID,
			Rect:     rect,
			Title:    m.paneFrameTitle(tab, paneID, pane),
			Meta:     m.paneFrameMeta(tab, paneID, pane, isFloatingPane(tab, paneID)),
			Floating: false,
		}}
	}
	entries := make([]paneRenderEntry, 0, len(tab.Panes))
	if tab.Root != nil {
		tilingRects := tab.Root.Rects(rootRect)
		tilingIDs := tab.Root.LeafIDs()
		for _, paneID := range tilingIDs {
			rect, ok := tilingRects[paneID]
			if !ok {
				continue
			}
			entries = append(entries, paneRenderEntry{
				PaneID:   paneID,
				Rect:     rect,
				Title:    m.paneFrameTitle(tab, paneID, tab.Panes[paneID]),
				Meta:     m.paneFrameMeta(tab, paneID, tab.Panes[paneID], false),
				Floating: false,
			})
		}
	}
	for _, floating := range m.visibleFloatingPanes(tab) {
		entries = append(entries, paneRenderEntry{
			PaneID:   floating.PaneID,
			Rect:     floating.Rect,
			Title:    m.paneFrameTitle(tab, floating.PaneID, tab.Panes[floating.PaneID]),
			Meta:     m.paneFrameMeta(tab, floating.PaneID, tab.Panes[floating.PaneID], true),
			Floating: true,
		})
	}
	return entries
}

func paneCells(pane *Pane) [][]drawCell {
	if pane == nil {
		return nil
	}
	if !pane.IsRenderDirty() && pane.CellCache() != nil {
		return pane.CellCache()
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
	grid = normalizePaneGridStyle(pane, grid)

	pane.SetCellCache(grid)
	pane.ClearRenderDirty()
	return grid
}

func paneCellsForViewport(pane *Pane, viewW, viewH int) [][]drawCell {
	if pane == nil {
		return nil
	}
	if pane.Mode != ViewportModeFixed {
		return paneCells(pane)
	}
	viewW = max(1, viewW)
	viewH = max(1, viewH)
	if pane.viewportCache != nil &&
		pane.viewportOffset == pane.Offset &&
		pane.viewportWidth == viewW &&
		pane.viewportHeight == viewH &&
		pane.viewportVersion == pane.cellVersion {
		return pane.viewportCache
	}

	if grid, ok := fixedViewportGridFromSource(pane, viewW, viewH); ok {
		grid = normalizePaneGridStyle(pane, grid)
		pane.viewportCache = grid
		pane.viewportOffset = pane.Offset
		pane.viewportWidth = viewW
		pane.viewportHeight = viewH
		pane.viewportVersion = pane.cellVersion
		pane.ClearRenderDirty()
		return pane.viewportCache
	}

	grid := paneCells(pane)
	pane.viewportCache = cropDrawGrid(grid, pane.Offset, viewW, viewH)
	pane.viewportOffset = pane.Offset
	pane.viewportWidth = viewW
	pane.viewportHeight = viewH
	pane.viewportVersion = pane.cellVersion
	return pane.viewportCache
}

func paneVisibleOffset(pane *Pane) Point {
	if pane == nil || pane.Mode != ViewportModeFixed {
		return Point{}
	}
	return pane.Offset
}

func paneVisibleCell(pane *Pane, viewX, viewY, viewW, viewH int) (drawCell, int, bool) {
	if pane == nil {
		return drawCell{}, 0, false
	}
	offset := paneVisibleOffset(pane)
	srcX := offset.X + viewX
	srcY := offset.Y + viewY

	switch {
	case pane.live && pane.VTerm != nil:
		contentW, contentH := pane.VTerm.Size()
		if srcX < 0 || srcY < 0 || srcX >= contentW || srcY >= contentH {
			return drawCell{}, 0, false
		}
		for srcX > 0 {
			cell := drawCellFromVTermCell(pane.VTerm.CellAt(srcX, srcY))
			if !cell.Continuation {
				break
			}
			srcX--
		}
		cell := drawCellFromVTermCell(pane.VTerm.CellAt(srcX, srcY))
		startCol := srcX - offset.X
		if cell.Continuation || startCol < 0 || startCol >= viewW || startCol+cell.Width > viewW {
			return drawCell{}, 0, false
		}
		return normalizePaneDrawCellStyle(pane, cell), startCol, true
	case snapshotHasVisibleContent(pane.Snapshot):
		if srcY < 0 || srcY >= len(pane.Snapshot.Screen.Cells) {
			return drawCell{}, 0, false
		}
		row := pane.Snapshot.Screen.Cells[srcY]
		if srcX < 0 || srcX >= len(row) {
			return drawCell{}, 0, false
		}
		for srcX > 0 {
			cell := drawCellFromProtocolCell(row[srcX])
			if !cell.Continuation {
				break
			}
			srcX--
		}
		cell := drawCellFromProtocolCell(row[srcX])
		startCol := srcX - offset.X
		if cell.Continuation || startCol < 0 || startCol >= viewW || startCol+cell.Width > viewW {
			return drawCell{}, 0, false
		}
		return normalizePaneDrawCellStyle(pane, cell), startCol, true
	default:
		grid := paneCellsForViewport(pane, viewW, viewH)
		if viewY < 0 || viewY >= len(grid) || viewX < 0 || viewX >= len(grid[viewY]) {
			return drawCell{}, 0, false
		}
		startCol := viewX
		for startCol > 0 && grid[viewY][startCol].Continuation {
			startCol--
		}
		cell := grid[viewY][startCol]
		if cell.Continuation || startCol+cell.Width > viewW {
			return drawCell{}, 0, false
		}
		return cell, startCol, true
	}
}

func normalizePaneGridStyle(pane *Pane, grid [][]drawCell) [][]drawCell {
	if pane == nil || paneTerminalState(pane) != "exited" || len(grid) == 0 {
		return grid
	}
	for y := range grid {
		for x := range grid[y] {
			grid[y][x] = normalizePaneDrawCellStyle(pane, grid[y][x])
		}
	}
	return grid
}

func normalizePaneDrawCellStyle(pane *Pane, cell drawCell) drawCell {
	if cell.Continuation {
		return cell
	}
	if pane == nil {
		return cell
	}
	if paneTerminalState(pane) != "exited" {
		return cell
	}
	if strings.TrimSpace(cell.Content) == "" {
		cell.Style = drawStyle{}
		return cell
	}
	cell.Style = drawStyle{FG: "#e2e8f0"}
	return cell
}

func fixedViewportGridFromSource(pane *Pane, viewW, viewH int) ([][]drawCell, bool) {
	switch {
	case pane == nil:
		return nil, false
	case pane.live && pane.VTerm != nil:
		return convertVTermViewport(pane.VTerm, pane.Offset, viewW, viewH), true
	case snapshotHasVisibleContent(pane.Snapshot):
		return convertProtocolViewport(pane.Snapshot.Screen.Cells, pane.Offset, viewW, viewH), true
	default:
		return nil, false
	}
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

func paneCursorForViewport(pane *Pane, viewW, viewH int) localvterm.CursorState {
	cursor := paneCursor(pane)
	if pane == nil || pane.Mode != ViewportModeFixed {
		return cursor
	}
	cursor.Row -= pane.Offset.Y
	cursor.Col -= pane.Offset.X
	if cursor.Row < 0 || cursor.Col < 0 || cursor.Row >= viewH || cursor.Col >= viewW {
		cursor.Visible = false
	}
	return cursor
}

func cropDrawGrid(grid [][]drawCell, offset Point, width, height int) [][]drawCell {
	if width <= 0 || height <= 0 {
		return nil
	}

	out := blankDrawGrid(width, height)
	for y := 0; y < height; y++ {
		row := out[y]

		srcY := offset.Y + y
		if srcY < 0 || srcY >= len(grid) {
			continue
		}

		srcRow := grid[srcY]
		for x := 0; x < width; x++ {
			srcX := offset.X + x
			if srcX < 0 || srcX >= len(srcRow) {
				continue
			}

			cell := srcRow[srcX]
			if cell.Continuation {
				continue
			}
			if cell.Content == "" {
				cell = blankDrawCell()
			}
			if cell.Width <= 0 {
				cell.Width = max(1, xansi.StringWidth(cell.Content))
			}
			if x+cell.Width > width {
				continue
			}

			row[x] = cell
			for i := 1; i < cell.Width && x+i < width; i++ {
				row[x+i] = drawCell{Continuation: true}
			}
		}
	}
	return out
}

func blankDrawGrid(width, height int) [][]drawCell {
	out := make([][]drawCell, height)
	for y := range out {
		row := make([]drawCell, width)
		for x := range row {
			row[x] = blankDrawCell()
		}
		out[y] = row
	}
	return out
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
			out[y][x] = drawCellFromVTermCell(cell)
		}
	}
	return out
}

func convertProtocolGrid(rows [][]protocol.Cell) [][]drawCell {
	out := make([][]drawCell, len(rows))
	for y, row := range rows {
		out[y] = make([]drawCell, len(row))
		for x, cell := range row {
			out[y][x] = drawCellFromProtocolCell(cell)
		}
	}
	return out
}

func convertVTermViewport(vt *localvterm.VTerm, offset Point, width, height int) [][]drawCell {
	out := blankDrawGrid(width, height)
	if vt == nil {
		return out
	}
	contentW, contentH := vt.Size()
	for y := 0; y < height; y++ {
		srcY := offset.Y + y
		if srcY < 0 || srcY >= contentH {
			continue
		}
		fillViewportRow(out[y], width, offset.X, contentW, func(srcX int) drawCell {
			return drawCellFromVTermCell(vt.CellAt(srcX, srcY))
		})
	}
	return out
}

func convertProtocolViewport(rows [][]protocol.Cell, offset Point, width, height int) [][]drawCell {
	out := blankDrawGrid(width, height)
	for y := 0; y < height; y++ {
		srcY := offset.Y + y
		if srcY < 0 || srcY >= len(rows) {
			continue
		}
		row := rows[srcY]
		fillViewportRow(out[y], width, offset.X, len(row), func(srcX int) drawCell {
			return drawCellFromProtocolCell(row[srcX])
		})
	}
	return out
}

func fillViewportRow(dst []drawCell, viewW, offsetX, contentW int, cellAt func(srcX int) drawCell) {
	for x := 0; x < viewW; x++ {
		srcX := offsetX + x
		if srcX < 0 || srcX >= contentW {
			continue
		}
		cell := cellAt(srcX)
		if cell.Continuation {
			continue
		}
		if cell.Content == "" {
			cell = blankDrawCell()
		}
		if cell.Width <= 0 {
			cell.Width = max(1, xansi.StringWidth(cell.Content))
		}
		if x+cell.Width > viewW {
			continue
		}
		dst[x] = cell
		for i := 1; i < cell.Width && x+i < viewW; i++ {
			dst[x+i] = drawCell{Continuation: true}
		}
	}
}

func drawCellFromVTermCell(cell localvterm.Cell) drawCell {
	continuation := cell.Content == "" && cell.Width == 0
	return drawCell{
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

func drawCellFromProtocolCell(cell protocol.Cell) drawCell {
	width := cell.Width
	continuation := cell.Content == "" && width == 0
	if width == 0 {
		width = max(1, xansi.StringWidth(cell.Content))
	}
	return drawCell{
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
	switch paneTerminalState(pane) {
	case "waiting":
		return "waiting pane" + paneTitleBadges(pane)
	case "unbound":
		return "saved pane" + paneTitleBadges(pane)
	case "killed":
		return "saved pane" + paneTitleBadges(pane)
	case "exited":
		title := paneDisplayTitle(pane) + paneTitleBadges(pane)
		if pane.ExitCode != nil {
			return "[exited code=" + itoa(*pane.ExitCode) + "] " + title
		}
		return "[exited] " + title
	default:
		return paneDisplayTitle(pane) + paneTitleBadges(pane)
	}
}

func paneDisplayTitle(pane *Pane) string {
	if pane == nil {
		return "pane"
	}
	if terminal := paneTerminal(pane); terminal != nil {
		name := strings.TrimSpace(terminal.Name)
		if name != "" {
			return name
		}
		if command := firstCommandWord(terminal.Command); command != "" || strings.TrimSpace(terminal.ID) != "" {
			return paneTitleForCommand(name, command, terminal.ID)
		}
	}
	return coalesce(strings.TrimSpace(pane.Name), strings.TrimSpace(pane.Title), strings.TrimSpace(pane.TerminalID), strings.TrimSpace(pane.ID), "terminal")
}

func paneTitleBadges(pane *Pane) string {
	if pane == nil {
		return ""
	}
	badges := make([]string, 0, 2)
	if paneAccessMode(pane) == "observer" {
		badges = append(badges, "[obs]")
	}
	if pane.Readonly {
		badges = append(badges, "[ro]")
	}
	if len(badges) == 0 {
		return ""
	}
	return " " + strings.Join(badges, " ")
}

func (m *Model) paneFrameTitle(tab *Tab, paneID string, pane *Pane) string {
	if pane == nil {
		return "pane"
	}
	title := paneDisplayTitle(pane)
	z, total, ok := floatingPaneOrder(tab, paneID)
	if !ok {
		return title
	}
	if total > 1 {
		return title + " [floating z:" + itoa(z) + "]"
	}
	return title + " [floating]"
}

func (m *Model) paneFrameMeta(tab *Tab, paneID string, pane *Pane, floating bool) string {
	if pane == nil {
		return ""
	}
	visible := m.visibleWorkbenchState()
	workspace := visible.Workspace
	parts := make([]string, 0, 8)
	switch paneTerminalState(pane) {
	case "waiting":
		parts = append(parts, m.icons.token("waiting", m.icons.Waiting))
	case "unbound":
		parts = append(parts, m.icons.token("saved", m.icons.Unbound))
	case "killed":
		parts = append(parts, m.icons.token("killed", m.icons.Killed))
	case "exited":
		code := ""
		if pane.ExitCode != nil {
			code = itoa(*pane.ExitCode)
		} else {
			code = "done"
		}
		parts = append(parts, m.icons.pairToken("exit", m.icons.Exited, code))
	default:
		parts = append(parts, m.icons.token("live", m.icons.Running))
	}
	workspaceTabs := []*Tab(nil)
	if workspace != nil {
		workspaceTabs = workspace.Tabs
	}
	if connection := paneConnectionStatus(workspaceTabs, pane); connection != "" {
		if connection == "owner" {
			parts = append(parts, m.icons.token("owner", m.icons.Owner))
		} else {
			parts = append(parts, m.icons.token("follower", m.icons.Follower))
		}
	}
	if count := terminalBindingCount(workspaceTabs, pane.TerminalID); count > 1 {
		parts = append(parts, m.icons.countToken("share", m.icons.Shared, count))
	}
	if paneAccessMode(pane) == "observer" {
		parts = append(parts, m.icons.token("obs", m.icons.Observer))
	}
	if pane.Readonly {
		parts = append(parts, m.icons.token("ro", m.icons.Readonly))
	}
	if pane.Pin {
		parts = append(parts, m.icons.token("pin", m.icons.Pinned))
	}
	if strings.TrimSpace(pane.Tags["termx.size_lock"]) == "warn" {
		parts = append(parts, m.icons.token("lock", m.icons.LockWarn))
	}
	return strings.Join(parts, " ")
}

func welcomePaneLines(pane *Pane) []string {
	title := paneTitle(pane)
	switch paneTerminalState(pane) {
	case "waiting":
		return []string{
			"waiting for terminal",
			"",
			"pane: " + title,
			"",
			"load-layout can create or attach a matching terminal later",
		}
	case "unbound":
		return []string{
			"saved pane",
			"",
			"No terminal in this pane",
			"",
			"Enter start new terminal",
			"Ctrl-f bring running terminal here",
			"Ctrl-g then t open terminal manager",
		}
	case "killed":
		return []string{
			"terminal was killed",
			"",
			"pane: " + title,
			"",
			"press Ctrl-p then x to close this pane",
			"or press Ctrl-f to attach another terminal",
		}
	case "exited":
		code := ""
		if pane != nil && pane.ExitCode != nil {
			code = " (exit code " + itoa(*pane.ExitCode) + ")"
		}
		return []string{
			"terminal exited" + code,
			"",
			"pane: " + title,
			"",
			"scrollback remains readable",
			"press r to restart in this pane",
			"press Ctrl-f to attach another terminal",
		}
	}
	return []string{
		"termx interactive pane",
		"",
		"connected terminal: " + title,
		"",
		"type normally to send input to the shell",
		"press Ctrl-p then % to split vertically",
		"press Ctrl-p then \" to split horizontally",
		"press Ctrl-t then c to open a new tab",
		"press Ctrl-g then ? for the full shortcut sheet",
	}
}

func styleDiffANSI(from, to drawStyle) string {
	if from == to {
		return ""
	}
	return styleANSI(to)
}

func styleANSI(to drawStyle) string {
	if cached, ok := styleANSICache.Load(to); ok {
		return cached.(string)
	}
	var b strings.Builder
	b.WriteString("\x1b[0")
	if to == (drawStyle{}) {
		b.WriteByte('m')
		ansi := b.String()
		styleANSICache.Store(to, ansi)
		return ansi
	}
	if to.FG != "" {
		if seq := ansiColorSequence(to.FG, true); seq != "" {
			b.WriteString(seq)
		} else if rgb, ok := hexToRGB(to.FG); ok {
			b.WriteString(";38;2;")
			b.WriteString(itoa(rgb[0]))
			b.WriteByte(';')
			b.WriteString(itoa(rgb[1]))
			b.WriteByte(';')
			b.WriteString(itoa(rgb[2]))
		}
	}
	if to.BG != "" {
		if seq := ansiColorSequence(to.BG, false); seq != "" {
			b.WriteString(seq)
		} else if rgb, ok := hexToRGB(to.BG); ok {
			b.WriteString(";48;2;")
			b.WriteString(itoa(rgb[0]))
			b.WriteByte(';')
			b.WriteString(itoa(rgb[1]))
			b.WriteByte(';')
			b.WriteString(itoa(rgb[2]))
		}
	}
	if to.Bold {
		b.WriteString(";1")
	}
	if to.Italic {
		b.WriteString(";3")
	}
	if to.Underline {
		b.WriteString(";4")
	}
	if to.Blink {
		b.WriteString(";5")
	}
	if to.Reverse {
		b.WriteString(";7")
	}
	if to.Strike {
		b.WriteString(";9")
	}
	b.WriteByte('m')
	ansi := b.String()
	styleANSICache.Store(to, ansi)
	return ansi
}

func ansiColorSequence(value string, foreground bool) string {
	value = strings.TrimSpace(value)
	switch {
	case strings.HasPrefix(value, "ansi:"):
		index, err := strconv.Atoi(strings.TrimPrefix(value, "ansi:"))
		if err != nil || index < 0 || index > 15 {
			return ""
		}
		switch {
		case index < 8 && foreground:
			return ";3" + itoa(index)
		case index < 8:
			return ";4" + itoa(index)
		case foreground:
			return ";9" + itoa(index-8)
		default:
			return ";10" + itoa(index-8)
		}
	case strings.HasPrefix(value, "idx:"):
		index, err := strconv.Atoi(strings.TrimPrefix(value, "idx:"))
		if err != nil || index < 0 || index > 255 {
			return ""
		}
		if foreground {
			return ";38;5;" + strconv.Itoa(index)
		}
		return ";48;5;" + strconv.Itoa(index)
	default:
		return ""
	}
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
