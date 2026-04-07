package render

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
	"github.com/rivo/uniseg"
)

type drawStyle struct {
	FG        string
	BG        string
	Bold      bool
	Italic    bool
	Underline bool
	Reverse   bool
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

	hostEmojiVS16Mode shared.AmbiguousEmojiVariationSelectorMode

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

var styleANSICache sync.Map

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
		fullDirty:         true,
		hostEmojiVS16Mode: shared.AmbiguousEmojiVariationSelectorRaw,
	}
	blankRow := cachedBlankFillRow(width)
	for y := 0; y < height; y++ {
		row := make([]drawCell, width)
		copy(row, blankRow)
		c.cells[y] = row
		c.rowDirty[y] = true
	}
	return c
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
	for start > 0 && c.cells[y][start].Continuation {
		start--
	}
	end := minInt(c.width, x+maxCellWidth(width))
	for i := start; i < end; i++ {
		if c.cells[y][i].Continuation {
			lead := i
			for lead > 0 && c.cells[y][lead].Continuation {
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
	c.rowDirty[y] = true
	c.fullDirty = true
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
		c.set(cursorX, y, drawCell{Content: content, Width: width, Style: style})
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
	}
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
	width := cell.Width
	continuation := cell.Content == "" && width == 0
	if width <= 0 {
		width = xansi.StringWidth(cell.Content)
	}
	if width <= 0 {
		width = 1
	}
	return drawCell{
		Content:      cell.Content,
		Width:        width,
		Style:        cellStyleFromSnapshot(cell),
		Continuation: continuation,
	}
}

func isAmbiguousEmojiVariationSelectorCluster(content string, width int) bool {
	if width != 2 || !strings.ContainsRune(content, '\uFE0F') {
		return false
	}
	if strings.ContainsRune(content, '\u200D') || strings.ContainsRune(content, '\u20E3') {
		return false
	}
	stripped := strings.ReplaceAll(content, "\uFE0F", "")
	return stripped != "" && xansi.StringWidth(stripped) == 1
}

func serializeCellContent(content string, width int, mode shared.AmbiguousEmojiVariationSelectorMode) string {
	if !isAmbiguousEmojiVariationSelectorCluster(content, width) {
		return content
	}
	switch mode {
	case shared.AmbiguousEmojiVariationSelectorAdvance:
		// Some host terminals render a FE0F grapheme like "♻️" but only advance
		// one column. Appending a cursor-forward keeps the visible emoji while
		// restoring the two-column footprint expected by the pane layout model.
		return content + xansi.CursorForward(1)
	case shared.AmbiguousEmojiVariationSelectorStrip:
		// Visible text-presentation plus a padding cell is the safe fallback when
		// we haven't yet proven the host terminal can keep the emoji width stable.
		return strings.ReplaceAll(content, "\uFE0F", "") + " "
	default:
		return content
	}
}

func serializeCellContentForDisplay(content string, width int, mode shared.AmbiguousEmojiVariationSelectorMode, nextCol int) string {
	if !isAmbiguousEmojiVariationSelectorCluster(content, width) {
		return content
	}
	switch mode {
	case shared.AmbiguousEmojiVariationSelectorStrip:
		return strings.ReplaceAll(content, "\uFE0F", "") + " "
	default:
		if nextCol <= 0 {
			return content
		}
		// Re-anchor the cursor to the next expected grid column after writing an
		// ambiguous FE0F grapheme. This keeps later cells and pane borders aligned
		// even if the host terminal advances the cursor by a different width.
		return content + xansi.CHA(nextCol)
	}
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
	for y := 0; y < c.height; y++ {
		if !c.rowDirty[y] && c.rowCache[y] != "" {
			continue
		}
		var row strings.Builder
		current := drawStyle{}
		// Each serialized row re-anchors itself at column 1 so any per-cell CHA
		// adjustments stay relative to the row grid rather than the host cursor's
		// previous position.
		row.WriteString(xansi.CHA(1))
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
			nextCol := 0
			if x+cell.Width < c.width {
				nextCol = x + cell.Width + 1
			}
			row.WriteString(serializeCellContentForDisplay(content, cell.Width, c.hostEmojiVS16Mode, nextCol))
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

func (c *composedCanvas) cursorANSI() string {
	if c == nil || !c.cursorVisible {
		return hideCursorANSI()
	}
	return cursorShapeANSI(c.cursorShape, c.cursorBlink) +
		fmt.Sprintf("\x1b[?25h\x1b[%d;%dH", c.cursorY+c.cursorOffsetY+1, c.cursorX+c.cursorOffsetX+1)
}

func hideCursorANSI() string {
	return "\x1b[?25l"
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
		c.cursorVisible = false
		return
	}
	c.cursorVisible = true
	c.cursorX = x
	c.cursorY = y
	c.cursorShape = shape
	c.cursorBlink = blink
}

func styleDiffANSI(from, to drawStyle) string {
	if from == to {
		return ""
	}
	return styleANSI(to)
}

func styleANSI(s drawStyle) string {
	if cached, ok := styleANSICache.Load(s); ok {
		return cached.(string)
	}
	var b strings.Builder
	b.WriteString("\x1b[0")
	if s == (drawStyle{}) {
		b.WriteByte('m')
		ansi := b.String()
		styleANSICache.Store(s, ansi)
		return ansi
	}
	if s.FG != "" {
		writeFGColor(&b, s.FG)
	}
	if s.BG != "" {
		writeBGColor(&b, s.BG)
	}
	if s.Bold {
		b.WriteString(";1")
	}
	if s.Italic {
		b.WriteString(";3")
	}
	if s.Underline {
		b.WriteString(";4")
	}
	if s.Reverse {
		b.WriteString(";7")
	}
	b.WriteByte('m')
	ansi := b.String()
	styleANSICache.Store(s, ansi)
	return ansi
}

// writeFGColor appends the ANSI foreground color sequence for the given color
// string. Supported formats: "ansi:N" (basic palette 0-15), "idx:N" (256-color
// index), "#rrggbb" (24-bit RGB).
func writeFGColor(b *strings.Builder, c string) {
	if n, ok := parseAnsiColor(c); ok {
		if n <= 7 {
			b.WriteString(";3")
			b.WriteString(itoa(n))
		} else {
			b.WriteString(";9")
			b.WriteString(itoa(n - 8))
		}
		return
	}
	if n, ok := parseIdxColor(c); ok {
		b.WriteString(";38;5;")
		b.WriteString(itoa(n))
		return
	}
	if rgb, ok := hexToRGB(c); ok {
		b.WriteString(";38;2;")
		b.WriteString(itoa(rgb[0]))
		b.WriteByte(';')
		b.WriteString(itoa(rgb[1]))
		b.WriteByte(';')
		b.WriteString(itoa(rgb[2]))
	}
}

// writeBGColor appends the ANSI background color sequence.
func writeBGColor(b *strings.Builder, c string) {
	if n, ok := parseAnsiColor(c); ok {
		if n <= 7 {
			b.WriteString(";4")
			b.WriteString(itoa(n))
		} else {
			b.WriteString(";10")
			b.WriteString(itoa(n - 8))
		}
		return
	}
	if n, ok := parseIdxColor(c); ok {
		b.WriteString(";48;5;")
		b.WriteString(itoa(n))
		return
	}
	if rgb, ok := hexToRGB(c); ok {
		b.WriteString(";48;2;")
		b.WriteString(itoa(rgb[0]))
		b.WriteByte(';')
		b.WriteString(itoa(rgb[1]))
		b.WriteByte(';')
		b.WriteString(itoa(rgb[2]))
	}
}

func parseAnsiColor(c string) (int, bool) {
	if !strings.HasPrefix(c, "ansi:") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(c, "ansi:"))
	if err != nil || n < 0 || n > 15 {
		return 0, false
	}
	return n, true
}

func parseIdxColor(c string) (int, bool) {
	if !strings.HasPrefix(c, "idx:") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(c, "idx:"))
	if err != nil || n < 0 || n > 255 {
		return 0, false
	}
	return n, true
}

func hexToRGB(hex string) ([3]int, bool) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return [3]int{}, false
	}
	r := hexByte(hex[0])<<4 | hexByte(hex[1])
	g := hexByte(hex[2])<<4 | hexByte(hex[3])
	b := hexByte(hex[4])<<4 | hexByte(hex[5])
	return [3]int{r, g, b}, true
}

func hexByte(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return 0
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
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
