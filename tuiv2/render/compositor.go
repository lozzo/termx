package render

import (
	"fmt"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
	"github.com/rivo/uniseg"
)

const rowDirtyChunkWidth = 32

type drawStyle struct {
	FG        string
	BG        string
	Bold      bool
	Italic    bool
	Underline bool
	Reverse   bool
}

type drawCell struct {
	Content             string
	Width               int
	Style               drawStyle
	Continuation        bool
	TerminalContent     bool
	HostWidthStabilizer bool
	// Marks the synthetic second column we materialize for FE0F ambiguous emoji.
	// It participates in overlap clearing like a wide-cell continuation, but it
	// still serializes through the raw+ECH path instead of being skipped.
	AmbiguousCompensation bool
}

type composedCanvas struct {
	width       int
	height      int
	cells       [][]drawCell
	rowCache    []string
	rowDirty    []bool
	rowDirtyMin []int
	rowDirtyMax []int
	rowChunks   [][]string
	fullCache   string
	fullDirty   bool

	hostEmojiVS16Mode shared.AmbiguousEmojiVariationSelectorMode

	cursorPlaced  bool
	cursorVisible bool
	cursorX       int
	cursorY       int
	cursorOffsetX int
	cursorOffsetY int
	cursorShape   string
	cursorBlink   bool

	syntheticCursorBlink     bool
	syntheticCursorVisibleFn func(protocol.CursorState) bool
}

func (c *composedCanvas) shiftRowsUp(shift int) {
	if c == nil || shift <= 0 || shift >= c.height {
		return
	}
	droppedCells := append([][]drawCell(nil), c.cells[:shift]...)
	droppedCache := append([]string(nil), c.rowCache[:shift]...)
	droppedDirty := append([]bool(nil), c.rowDirty[:shift]...)
	droppedMin := append([]int(nil), c.rowDirtyMin[:shift]...)
	droppedMax := append([]int(nil), c.rowDirtyMax[:shift]...)
	droppedChunks := append([][]string(nil), c.rowChunks[:shift]...)

	copy(c.cells, c.cells[shift:])
	copy(c.rowCache, c.rowCache[shift:])
	copy(c.rowDirty, c.rowDirty[shift:])
	copy(c.rowDirtyMin, c.rowDirtyMin[shift:])
	copy(c.rowDirtyMax, c.rowDirtyMax[shift:])
	copy(c.rowChunks, c.rowChunks[shift:])

	for i := 0; i < shift; i++ {
		target := c.height - shift + i
		c.cells[target] = droppedCells[i]
		c.rowCache[target] = droppedCache[i]
		c.rowDirty[target] = droppedDirty[i]
		c.rowDirtyMin[target] = droppedMin[i]
		c.rowDirtyMax[target] = droppedMax[i]
		c.rowChunks[target] = droppedChunks[i]
		c.resetRowToBlank(target)
	}
	c.fullCache = ""
	c.fullDirty = true
}

func (c *composedCanvas) shiftRowsDown(shift int) {
	if c == nil || shift <= 0 || shift >= c.height {
		return
	}
	droppedCells := append([][]drawCell(nil), c.cells[c.height-shift:]...)
	droppedCache := append([]string(nil), c.rowCache[c.height-shift:]...)
	droppedDirty := append([]bool(nil), c.rowDirty[c.height-shift:]...)
	droppedMin := append([]int(nil), c.rowDirtyMin[c.height-shift:]...)
	droppedMax := append([]int(nil), c.rowDirtyMax[c.height-shift:]...)
	droppedChunks := append([][]string(nil), c.rowChunks[c.height-shift:]...)

	copy(c.cells[shift:], c.cells[:c.height-shift])
	copy(c.rowCache[shift:], c.rowCache[:c.height-shift])
	copy(c.rowDirty[shift:], c.rowDirty[:c.height-shift])
	copy(c.rowDirtyMin[shift:], c.rowDirtyMin[:c.height-shift])
	copy(c.rowDirtyMax[shift:], c.rowDirtyMax[:c.height-shift])
	copy(c.rowChunks[shift:], c.rowChunks[:c.height-shift])

	for i := 0; i < shift; i++ {
		c.cells[i] = droppedCells[i]
		c.rowCache[i] = droppedCache[i]
		c.rowDirty[i] = droppedDirty[i]
		c.rowDirtyMin[i] = droppedMin[i]
		c.rowDirtyMax[i] = droppedMax[i]
		c.rowChunks[i] = droppedChunks[i]
		c.resetRowToBlank(i)
	}
	c.fullCache = ""
	c.fullDirty = true
}

func (c *composedCanvas) shiftRectRowsUp(rect workbench.Rect, shift int) {
	if c == nil || shift <= 0 {
		return
	}
	rect, ok := clipRectToViewport(rect, c.width, c.height)
	if !ok || shift >= rect.H {
		return
	}
	for y := rect.Y; y < rect.Y+rect.H-shift; y++ {
		copy(c.cells[y][rect.X:rect.X+rect.W], c.cells[y+shift][rect.X:rect.X+rect.W])
		c.markRowDirtyRange(y, rect.X, rect.X+rect.W-1)
	}
	fillRect(c, workbench.Rect{X: rect.X, Y: rect.Y + rect.H - shift, W: rect.W, H: shift}, blankDrawCell())
	c.fullCache = ""
	c.fullDirty = true
}

func (c *composedCanvas) shiftRectRowsDown(rect workbench.Rect, shift int) {
	if c == nil || shift <= 0 {
		return
	}
	rect, ok := clipRectToViewport(rect, c.width, c.height)
	if !ok || shift >= rect.H {
		return
	}
	for y := rect.Y + rect.H - 1; y >= rect.Y+shift; y-- {
		copy(c.cells[y][rect.X:rect.X+rect.W], c.cells[y-shift][rect.X:rect.X+rect.W])
		c.markRowDirtyRange(y, rect.X, rect.X+rect.W-1)
	}
	fillRect(c, workbench.Rect{X: rect.X, Y: rect.Y, W: rect.W, H: shift}, blankDrawCell())
	c.fullCache = ""
	c.fullDirty = true
}

func (c *composedCanvas) resetRowToBlank(y int) {
	if c == nil || y < 0 || y >= c.height {
		return
	}
	blankRow := cachedBlankFillRow(c.width)
	copy(c.cells[y], blankRow)
	c.rowCache[y] = ""
	if c.rowChunks[y] != nil {
		clear(c.rowChunks[y])
	}
	c.rowDirty[y] = true
	c.rowDirtyMin[y] = 0
	c.rowDirtyMax[y] = c.width - 1
}

func blankDrawCell() drawCell {
	return drawCell{Content: " ", Width: 1}
}

func newComposedCanvas(width, height int) *composedCanvas {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	c := &composedCanvas{
		width:             width,
		height:            height,
		cells:             make([][]drawCell, height),
		rowCache:          make([]string, height),
		rowDirty:          make([]bool, height),
		rowDirtyMin:       make([]int, height),
		rowDirtyMax:       make([]int, height),
		rowChunks:         make([][]string, height),
		fullDirty:         true,
		hostEmojiVS16Mode: shared.AmbiguousEmojiVariationSelectorRaw,
	}
	blankRow := cachedBlankFillRow(width)
	for y := 0; y < height; y++ {
		row := make([]drawCell, width)
		copy(row, blankRow)
		c.cells[y] = row
		c.markRowDirtyRange(y, 0, width-1)
	}
	return c
}

func (c *composedCanvas) resetToBlank() {
	if c == nil || c.width <= 0 || c.height <= 0 {
		return
	}
	blankRow := cachedBlankFillRow(c.width)
	for y := 0; y < c.height; y++ {
		copy(c.cells[y], blankRow)
		c.rowCache[y] = ""
		if c.rowChunks[y] != nil {
			clear(c.rowChunks[y])
		}
		c.rowDirty[y] = true
		c.rowDirtyMin[y] = 0
		c.rowDirtyMax[y] = c.width - 1
	}
	c.fullCache = ""
	c.fullDirty = true
	c.syntheticCursorBlink = false
	c.clearCursor()
}

func (c *composedCanvas) blit(src *composedCanvas, dstX, dstY int) {
	if c == nil || src == nil || src.width <= 0 || src.height <= 0 {
		return
	}
	for y := 0; y < src.height; y++ {
		targetY := dstY + y
		if targetY < 0 || targetY >= c.height {
			continue
		}
		startX := dstX
		srcStartX := 0
		width := src.width
		if startX < 0 {
			srcStartX = -startX
			width -= srcStartX
			startX = 0
		}
		if width <= 0 || startX >= c.width {
			continue
		}
		if startX+width > c.width {
			width = c.width - startX
		}
		copy(c.cells[targetY][startX:startX+width], src.cells[y][srcStartX:srcStartX+width])
		c.markRowDirtyRange(targetY, startX, startX+width-1)
	}
	c.fullDirty = true
}

func (c *composedCanvas) set(x, y int, cell drawCell) {
	if x < 0 || y < 0 || x >= c.width || y >= c.height {
		return
	}
	if cell.Width <= 0 {
		cell.Width = 1
	}
	if cell.Content == "" {
		cell.Content = " "
		cell.Width = 1
	}
	cell.Continuation = false
	c.clearOverlappingCellFootprints(x, y, cell.Width)
	c.writeCell(x, y, cell)
	for i := 1; i < cell.Width && x+i < c.width; i++ {
		c.writeCell(x+i, y, drawCell{Continuation: true})
	}
}

func (c *composedCanvas) clearOverlappingCellFootprints(x, y, width int) {
	if c == nil || y < 0 || y >= c.height || x < 0 || x >= c.width {
		return
	}
	start := x
	for start > 0 && c.cellBelongsToWideFootprint(start, y) {
		start--
	}
	end := minInt(c.width, x+maxCellWidth(width))
	for i := start; i < end; i++ {
		if c.cellBelongsToWideFootprint(i, y) {
			lead := i
			for lead > 0 && c.cellBelongsToWideFootprint(lead, y) {
				lead--
			}
			if lead < start {
				start = lead
				i = start - 1
				continue
			}
			if span := c.cellFootprintWidth(lead, y); lead+span > end {
				end = minInt(c.width, lead+span)
			}
			continue
		}
		if span := c.cellFootprintWidth(i, y); i+span > end {
			end = minInt(c.width, i+span)
		}
	}
	for i := start; i < end; i++ {
		c.writeCell(i, y, blankDrawCell())
	}
}

func (c *composedCanvas) cellBelongsToWideFootprint(x, y int) bool {
	if c == nil || y < 0 || y >= c.height || x < 0 || x >= c.width {
		return false
	}
	cell := c.cells[y][x]
	return cell.Continuation || cell.AmbiguousCompensation
}

func (c *composedCanvas) cellFootprintWidth(x, y int) int {
	if c == nil || y < 0 || y >= c.height || x < 0 || x >= c.width {
		return 1
	}
	cell := c.cells[y][x]
	width := cell.Width
	if width <= 0 && cell.Content != "" {
		width = xansi.StringWidth(cell.Content)
	}
	return maxCellWidth(width)
}

func (c *composedCanvas) writeCell(x, y int, cell drawCell) {
	if c == nil || x < 0 || y < 0 || x >= c.width || y >= c.height {
		return
	}
	if c.cells[y][x] == cell {
		return
	}
	c.cells[y][x] = cell
	c.markRowDirtyRange(y, x, x)
}

func (c *composedCanvas) drawText(x, y int, text string, style drawStyle) {
	cursorX := x
	for _, cluster := range splitTextClusters(text) {
		if cursorX >= c.width {
			break
		}
		content := cluster.Content
		width := cluster.Width
		if cursorX+width > c.width {
			break
		}
		cell := drawCell{Content: content, Width: width, Style: style}
		c.set(cursorX, y, cell)
		c.materializeRawAmbiguousContinuation(cursorX, y, cell)
		cursorX += width
	}
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

func (c *composedCanvas) drawSnapshotInRect(rect workbench.Rect, snapshot *protocol.Snapshot) {
	if c == nil || snapshot == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	for y := 0; y < rect.H && y < len(snapshot.Screen.Cells); y++ {
		c.drawProtocolRowInRect(rect, rect.Y+y, snapshot.Screen.Cells[y])
	}
}

func (c *composedCanvas) drawProtocolRowInRect(rect workbench.Rect, targetY int, row []protocol.Cell) {
	if c == nil || rect.W <= 0 || targetY < 0 || targetY >= c.height {
		return
	}
	limit := minInt(rect.W, len(row))
	for x := 0; x < limit; x++ {
		cell := drawCellFromProtocolCell(row[x])
		if cell.Continuation {
			continue
		}
		if x+cell.Width > rect.W {
			continue
		}
		if cell.Content == "" {
			cell.Content = " "
			cell.Width = 1
		}
		targetX := rect.X + x
		if targetX < 0 || targetX >= c.width {
			continue
		}
		c.set(targetX, targetY, cell)
		c.materializeRawAmbiguousContinuation(targetX, targetY, cell)
	}
}

func (c *composedCanvas) materializeRawAmbiguousContinuation(x, y int, cell drawCell) {
	if c == nil {
		return
	}
	if !isAmbiguousEmojiVariationSelectorCluster(cell.Content, cell.Width) {
		return
	}
	contX := x + 1
	if contX >= c.width {
		return
	}
	c.writeCell(contX, y, drawCell{
		Content:               " ",
		Width:                 1,
		Style:                 cell.Style,
		AmbiguousCompensation: true,
	})
}

func cellStyleFromSnapshot(cell protocol.Cell) drawStyle {
	return drawStyle{
		FG:        cell.Style.FG,
		BG:        cell.Style.BG,
		Bold:      cell.Style.Bold,
		Italic:    cell.Style.Italic,
		Underline: cell.Style.Underline,
		Reverse:   cell.Style.Reverse,
	}
}

func drawCellFromProtocolCell(cell protocol.Cell) drawCell {
	hostWidthStabilizer := cell.Content != "" &&
		(cell.Width == 0 || (cell.Width == 1 && shared.IsEastAsianAmbiguousWidthCluster(cell.Content) && !shared.IsStableNarrowTerminalSymbol(cell.Content)))
	width := cell.Width
	continuation := cell.Content == "" && width == 0
	if width <= 0 {
		width = xansi.StringWidth(cell.Content)
	}
	if width <= 0 {
		width = 1
	}
	return drawCell{
		Content:             cell.Content,
		Width:               width,
		Style:               cellStyleFromSnapshot(cell),
		Continuation:        continuation,
		TerminalContent:     true,
		HostWidthStabilizer: hostWidthStabilizer,
	}
}

func isAmbiguousEmojiVariationSelectorCluster(content string, width int) bool {
	return shared.IsAmbiguousEmojiVariationSelectorCluster(content, width)
}

func shouldReanchorAfterTerminalAmbiguousWidthCell(cell drawCell) bool {
	return cell.TerminalContent && cell.HostWidthStabilizer
}

// 中文说明：这里只保留“原样输出 cell 内容”这个最小职责。FE0F 歧义 emoji
// 是否需要消失，不在序列化阶段猜测，而是交给后续覆盖写入去清掉整个
// footprint。

func serializeCellContentForDisplay(content string, width int, mode shared.AmbiguousEmojiVariationSelectorMode, nextCol int) string {
	// 保留 mode/nextCol 形参是为了不牵动调用点；新策略下它们已不再影响输出。
	_ = mode
	_ = nextCol
	return content
}

func (c *composedCanvas) isRawAmbiguousContinuationSpace(x, y int) bool {
	if c == nil {
		return false
	}
	if y < 0 || y >= c.height || x <= 0 || x >= c.width {
		return false
	}
	cell := c.cells[y][x]
	if cell.AmbiguousCompensation {
		return true
	}
	if cell.Continuation || cell.Width != 1 || cell.Content != " " {
		return false
	}
	prev := c.cells[y][x-1]
	if prev.Continuation {
		return false
	}
	return isAmbiguousEmojiVariationSelectorCluster(prev.Content, prev.Width)
}

// drawSnapshot draws a snapshot starting at (0,0).
func (c *composedCanvas) drawSnapshot(snapshot *protocol.Snapshot) {
	if c == nil || snapshot == nil {
		return
	}
	c.drawSnapshotInRect(workbench.Rect{X: 0, Y: 0, W: c.width, H: c.height}, snapshot)
}

// drawSnapshotRect draws a snapshot within a specific rect (legacy compat).
func (c *composedCanvas) drawSnapshotRect(rect workbench.Rect, snapshot *protocol.Snapshot) {
	c.drawSnapshotInRect(rect, snapshot)
}

func (c *composedCanvas) String() string {
	if c == nil {
		return ""
	}
	return c.contentString() + c.cursorANSI()
}

func (c *composedCanvas) contentString() string {
	if c == nil {
		return ""
	}
	if !c.fullDirty && c.fullCache != "" {
		return c.fullCache
	}
	c.ensureRowCache()
	var out strings.Builder
	totalLen := maxInt(0, c.height-1)
	for y := 0; y < c.height; y++ {
		totalLen += len(c.rowCache[y])
	}
	out.Grow(totalLen)
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

func (c *composedCanvas) cachedContentLines() []string {
	if c == nil {
		return nil
	}
	finish := perftrace.Measure("render.body.lines_export")
	defer func() {
		total := 0
		for _, line := range c.rowCache {
			total += len(line)
		}
		finish(total)
	}()
	c.ensureRowCache()
	lines := make([]string, c.height)
	copy(lines, c.rowCache)
	return lines
}

func (c *composedCanvas) contentLines() []string {
	if c == nil {
		return nil
	}
	lines := make([]string, c.height)
	for y := 0; y < c.height; y++ {
		lines[y] = c.serializeRowRangeCompressed(y, 0, c.width-1)
	}
	return lines
}

func (c *composedCanvas) embeddedContentLines() []string {
	if c == nil {
		return nil
	}
	lines := make([]string, c.height)
	for y := 0; y < c.height; y++ {
		var row strings.Builder
		current := drawStyle{}
		for x := 0; x < c.width; x++ {
			cell := c.cells[y][x]
			if cell.Continuation {
				continue
			}
			if c.isRawAmbiguousContinuationSpace(x, y) {
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
			row.WriteString(serializeCellContentForDisplay(content, cell.Width, c.hostEmojiVS16Mode, 0))
		}
		if current != (drawStyle{}) {
			row.WriteString(styleANSI(drawStyle{}))
		}
		lines[y] = row.String()
	}
	return lines
}

func (c *composedCanvas) ensureRowCache() {
	if c == nil {
		return
	}
	for y := 0; y < c.height; y++ {
		dirtyStart, dirtyEnd, dirty := c.rowDirtyRange(y)
		if !dirty && c.rowCache[y] != "" {
			continue
		}
		if !dirty {
			dirtyStart = 0
			dirtyEnd = c.width - 1
		}

		// Fast path: entire row is dirty — serialize in one pass, skip chunk overhead.
		if dirtyStart == 0 && dirtyEnd >= c.width-1 {
			if c.rowChunks[y] == nil {
				c.rowChunks[y] = make([]string, c.rowChunkCount())
			}
			for chunk := range c.rowChunks[y] {
				startX, endX := c.rowChunkBounds(chunk)
				c.rowChunks[y][chunk] = c.serializeRowRange(y, startX, endX)
			}
			// Keep the exported row representation stable across both initial full
			// paints and later partial updates. Mixing full-row strings with
			// chunk-stitched strings caused scroll diff misses; mixing them also
			// forced partial updates to reserialize whole rows.
			c.rowCache[y] = c.buildRowFromChunks(y)
			c.clearRowDirty(y)
			continue
		}

		// Partial dirty: use chunk system.
		startChunk := dirtyStart / rowDirtyChunkWidth
		endChunk := dirtyEnd / rowDirtyChunkWidth
		if c.rowChunks[y] == nil {
			c.rowChunks[y] = make([]string, c.rowChunkCount())
			startChunk = 0
			endChunk = len(c.rowChunks[y]) - 1
		}
		for chunk := startChunk; chunk <= endChunk; chunk++ {
			startX, endX := c.rowChunkBounds(chunk)
			c.rowChunks[y][chunk] = c.serializeRowRange(y, startX, endX)
		}
		// Keep full-row and partial-row exports on the same chunk-stitched
		// representation. Mixing full-row strings with chunk-built strings broke
		// vertical scroll detection even when the visible rows matched exactly.
		c.rowCache[y] = c.buildRowFromChunks(y)
		c.clearRowDirty(y)
	}
}

func (c *composedCanvas) compressibleBlankRun(y, startX int) int {
	return c.compressibleBlankRunInRange(y, startX, c.width-1)
}

func (c *composedCanvas) compressibleBlankRunInRange(y, startX, endX int) int {
	if c == nil || y < 0 || y >= c.height || startX < 0 || startX >= c.width {
		return 0
	}
	if endX < startX {
		return 0
	}
	if endX >= c.width {
		endX = c.width - 1
	}
	first := c.cells[y][startX]
	if first.Continuation || first.AmbiguousCompensation || first.Width != 1 {
		return 0
	}
	if first.Content != "" && first.Content != " " {
		return 0
	}
	run := 0
	for x := startX; x <= endX; x++ {
		cell := c.cells[y][x]
		if cell.Continuation || cell.AmbiguousCompensation || cell.Width != 1 {
			break
		}
		if cell.Style != first.Style {
			break
		}
		if cell.Content != "" && cell.Content != " " {
			break
		}
		run++
	}
	return run
}

func (c *composedCanvas) markRowDirtyRange(y, startX, endX int) {
	if c == nil || y < 0 || y >= c.height || c.width <= 0 {
		return
	}
	if startX > endX {
		startX, endX = endX, startX
	}
	if endX < 0 || startX >= c.width {
		return
	}
	if startX < 0 {
		startX = 0
	}
	if endX >= c.width {
		endX = c.width - 1
	}
	c.rowDirty[y] = true
	if c.rowDirtyMin[y] < 0 || startX < c.rowDirtyMin[y] {
		c.rowDirtyMin[y] = startX
	}
	if c.rowDirtyMax[y] < 0 || endX > c.rowDirtyMax[y] {
		c.rowDirtyMax[y] = endX
	}
	c.fullDirty = true
}

func (c *composedCanvas) rowDirtyRange(y int) (int, int, bool) {
	if c == nil || y < 0 || y >= c.height {
		return 0, 0, false
	}
	start := c.rowDirtyMin[y]
	end := c.rowDirtyMax[y]
	if !c.rowDirty[y] || start < 0 || end < start {
		return 0, 0, false
	}
	return start, end, true
}

func (c *composedCanvas) clearRowDirty(y int) {
	if c == nil || y < 0 || y >= c.height {
		return
	}
	c.rowDirty[y] = false
	c.rowDirtyMin[y] = -1
	c.rowDirtyMax[y] = -1
}

func (c *composedCanvas) rowChunkCount() int {
	if c == nil || c.width <= 0 {
		return 0
	}
	return (c.width + rowDirtyChunkWidth - 1) / rowDirtyChunkWidth
}

func (c *composedCanvas) rowChunkBounds(chunk int) (int, int) {
	start := chunk * rowDirtyChunkWidth
	end := minInt(c.width-1, start+rowDirtyChunkWidth-1)
	return start, end
}

func (c *composedCanvas) buildRowFromChunks(y int) string {
	if c == nil || y < 0 || y >= c.height {
		return ""
	}
	var row strings.Builder
	rowHint := len(c.rowCache[y])
	if rowHint <= 0 {
		rowHint = c.width + 16
	}
	row.Grow(rowHint)
	for _, chunk := range c.rowChunks[y] {
		row.WriteString(chunk)
	}
	row.WriteString("\x1b[0m\x1b[K")
	return row.String()
}

func (c *composedCanvas) serializeRowRange(y, startX, endX int) string {
	return c.serializeRowRangeWithBlankMode(y, startX, endX, false)
}

func (c *composedCanvas) serializeRowRangeCompressed(y, startX, endX int) string {
	return c.serializeRowRangeWithBlankMode(y, startX, endX, true)
}

func (c *composedCanvas) serializeRowRangeWithBlankMode(y, startX, endX int, compressBlanks bool) string {
	if c == nil || y < 0 || y >= c.height || startX < 0 || endX < startX || startX >= c.width {
		return ""
	}
	if endX >= c.width {
		endX = c.width - 1
	}
	var row strings.Builder
	rowHint := (endX - startX + 1) * 8
	if rowHint < 32 {
		rowHint = 32
	}
	row.Grow(rowHint)
	current := drawStyle{}
	needsReanchor := true
	for x := startX; x <= endX; x++ {
		cell := c.cells[y][x]
		if cell.Continuation {
			continue
		}
		if blankRun := c.compressibleBlankRunInRange(y, x, endX); blankRun >= 5 {
			if needsReanchor {
				// Full rows already restart at column 1 after CRLF. Skip CHA(1)
				// so the writer does not have to strip it again later.
				if x > 0 {
					writeCHAANSI(&row, x+1)
				}
				needsReanchor = false
			}
			if current != cell.Style {
				row.WriteString(styleDiffANSI(current, cell.Style))
				current = cell.Style
			}
			if compressBlanks {
				writeECHANSI(&row, blankRun)
				needsReanchor = true
			} else {
				row.WriteString(cachedBlankString(blankRun))
			}
			x += blankRun - 1
			continue
		}
		content := cell.Content
		if content == "" {
			content = " "
		}
		if c.isRawAmbiguousContinuationSpace(x, y) {
			// Keep FE0F compensation cells explicitly erasable across hosts with
			// different ambiguous-width behavior.
			if needsReanchor {
				if x > 0 {
					writeCHAANSI(&row, x+1)
				}
				needsReanchor = false
			}
			writeECHANSI(&row, 1)
			needsReanchor = true
			continue
		}
		if needsReanchor {
			if x > 0 {
				writeCHAANSI(&row, x+1)
			}
			needsReanchor = false
		}
		if current != cell.Style {
			row.WriteString(styleDiffANSI(current, cell.Style))
			current = cell.Style
		}
		nextCol := 0
		if x+cell.Width <= endX {
			nextCol = x + cell.Width + 1
		}
		row.WriteString(serializeCellContentForDisplay(content, cell.Width, c.hostEmojiVS16Mode, nextCol))
		if shouldReanchorAfterTerminalAmbiguousWidthCell(cell) {
			needsReanchor = true
		}
	}
	if current != (drawStyle{}) {
		row.WriteString(styleANSI(drawStyle{}))
	}
	return row.String()
}

func (c *composedCanvas) cursorANSI() string {
	if c == nil || !c.cursorPlaced {
		return hideCursorANSI()
	}
	if !c.cursorVisible {
		return hostHiddenCursorANSI(c.cursorX+c.cursorOffsetX, c.cursorY+c.cursorOffsetY, c.cursorShape, c.cursorBlink)
	}
	return hostCursorANSI(c.cursorX+c.cursorOffsetX, c.cursorY+c.cursorOffsetY, c.cursorShape, c.cursorBlink)
}

func hideCursorANSI() string {
	return "\x1b[?25l"
}

func hostCursorANSI(x, y int, shape string, blink bool) string {
	if x < 0 || y < 0 {
		return hideCursorANSI()
	}
	return fmt.Sprintf("\x1b[%d;%dH", y+1, x+1) + cursorShapeANSI(shape, blink) + "\x1b[?25h"
}

func hostHiddenCursorANSI(x, y int, shape string, blink bool) string {
	if x < 0 || y < 0 {
		return hideCursorANSI()
	}
	// Keep the host cursor parked at the in-pane insertion point so IME/preedit
	// UIs anchor locally, but leave it hidden to avoid host-side bleed.
	// Match zellij's hidden-cursor path: hide first, then move the hidden host
	// cursor into place. Hidden anchors do not need an explicit DECSCUSR shape.
	_ = shape
	_ = blink
	return "\x1b[?25l" + fmt.Sprintf("\x1b[%d;%dH", y+1, x+1)
}

func cursorShapeANSI(shape string, blink bool) string {
	code := 0
	switch shape {
	case "underline":
		if blink {
			code = 3
		} else {
			code = 4
		}
	case "bar":
		if blink {
			code = 5
		} else {
			code = 6
		}
	default:
		if blink {
			code = 1
		} else {
			code = 2
		}
	}
	return fmt.Sprintf("\x1b[%d q", code)
}

func (c *composedCanvas) setCursor(x, y int, shape string, blink bool) {
	if c == nil || x < 0 || y < 0 || x >= c.width || y >= c.height {
		c.clearCursor()
		return
	}
	c.cursorPlaced = true
	c.cursorVisible = true
	c.cursorX = x
	c.cursorY = y
	c.cursorShape = shape
	c.cursorBlink = blink
}

func (c *composedCanvas) setHiddenCursor(x, y int, shape string, blink bool) {
	if c == nil || x < 0 || y < 0 || x >= c.width || y >= c.height {
		c.clearCursor()
		return
	}
	c.cursorPlaced = true
	c.cursorVisible = false
	c.cursorX = x
	c.cursorY = y
	c.cursorShape = shape
	c.cursorBlink = blink
}

func (c *composedCanvas) clearCursor() {
	if c == nil {
		return
	}
	c.cursorPlaced = false
	c.cursorVisible = false
	c.cursorX = 0
	c.cursorY = 0
	c.cursorShape = ""
	c.cursorBlink = false
}

func maxCellWidth(width int) int {
	if width <= 0 {
		return 1
	}
	return width
}

// renderPaneSnapshot is kept for backward compatibility with tests.
func renderPaneSnapshot(rect workbench.Rect, pane workbench.VisiblePane, runtimeState *VisibleRuntimeStateProxy) []string {
	if rect.W <= 0 || rect.H <= 0 {
		return nil
	}
	canvas := newComposedCanvas(rect.W, rect.H)
	if runtimeState != nil {
		for _, terminal := range runtimeState.Terminals {
			if terminal.TerminalID == pane.TerminalID {
				if terminal.Snapshot != nil {
					canvas.drawSnapshotInRect(workbench.Rect{X: 0, Y: 0, W: rect.W, H: rect.H}, terminal.Snapshot)
					// Return raw content without ANSI for snapshot tests
					lines := make([]string, 0, canvas.height)
					for _, row := range canvas.cells {
						var b strings.Builder
						for _, cell := range row {
							if cell.Continuation {
								continue
							}
							b.WriteString(cell.Content)
						}
						lines = append(lines, strings.TrimRight(b.String(), " "))
					}
					return lines
				}
				break
			}
		}
	}
	return strings.Split(canvas.rawString(), "\n")
}

// rawString returns the canvas content without ANSI styling.
func (c *composedCanvas) rawString() string {
	if c == nil {
		return ""
	}
	lines := make([]string, 0, c.height)
	for _, row := range c.cells {
		var b strings.Builder
		for _, cell := range row {
			if cell.Continuation {
				continue
			}
			content := cell.Content
			if content == "" {
				content = " "
			}
			b.WriteString(content)
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	return strings.Join(lines, "\n")
}
