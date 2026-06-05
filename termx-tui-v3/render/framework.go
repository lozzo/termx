package render

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

const (
	minFrameWidth  = 24
	minFrameHeight = 8
	defaultWidth   = 80
	defaultHeight  = 24
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
		for col := range rows[i] {
			rows[i][col] = blankCanvasCell()
		}
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
	for _, segment := range cellSegmentsFromLine(line, width, owner, layer) {
		if remaining <= 0 {
			break
		}
		if segment.width <= 0 {
			continue
		}
		if segment.width > remaining {
			break
		}
		c.writeSegment(cursor, y, segment)
		cursor += segment.width
		remaining -= segment.width
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
			width := cell.width
			if width <= 0 {
				width = 1
			}
			cells = append(cells, Cell{
				Text:  cell.text,
				Width: width,
				Style: cell.style,
				Safe:  cell.safe,
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
	c.rows[y][x] = canvasCell{
		text:  segment.text,
		width: segment.width,
		style: segment.style,
		owner: segment.owner,
		layer: segment.layer,
		safe:  segment.safe,
	}
	for col := x + 1; col < x+segment.width; col++ {
		c.rows[y][col] = canvasCell{
			width:        0,
			style:        segment.style,
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

type canvasSegment struct {
	text  string
	width int
	style StyleToken
	owner string
	layer LayerKind
	safe  bool
}

func cellSegments(text string, style StyleToken, owner string, layer LayerKind) []canvasSegment {
	text = xansi.Strip(SafeLine(text))
	segments := make([]canvasSegment, 0, DisplayWidth(text))
	for len(text) > 0 {
		cluster, width := xansi.FirstGraphemeCluster(text, xansi.GraphemeWidth)
		if cluster == "" {
			break
		}
		if width < 0 {
			width = 0
		}
		if width > 0 {
			segments = append(segments, canvasSegment{
				text:  cluster,
				width: width,
				style: style,
				owner: owner,
				layer: layer,
				safe:  true,
			})
		}
		text = text[len(cluster):]
	}
	return segments
}

func cellSegmentsFromLine(line Line, width int, owner string, layer LayerKind) []canvasSegment {
	if width <= 0 {
		return nil
	}
	segments := make([]canvasSegment, 0, width)
	remaining := width
	for _, cell := range line.Cells {
		if remaining <= 0 {
			break
		}
		text := cell.Text
		if text == "" {
			continue
		}
		if DisplayWidth(text) > remaining {
			text = TruncateCells(text, remaining)
		}
		for _, segment := range cellSegments(text, cell.Style, owner, layer) {
			if segment.width > remaining {
				break
			}
			segment.safe = cell.Safe
			segments = append(segments, segment)
			remaining -= segment.width
		}
	}
	for remaining > 0 {
		segments = append(segments, canvasSegment{
			text:  " ",
			width: 1,
			owner: owner,
			layer: layer,
			safe:  true,
		})
		remaining--
	}
	return segments
}

func cellsFromSegments(segments []canvasSegment) []Cell {
	if len(segments) == 0 {
		return nil
	}
	cells := make([]Cell, len(segments))
	for i, segment := range segments {
		cells[i] = Cell{
			Text:  segment.text,
			Width: segment.width,
			Style: segment.style,
			Safe:  segment.safe,
		}
	}
	return cells
}

func blankCanvasCell() canvasCell {
	return canvasCell{text: " ", width: 1, safe: true}
}

func (renderer Renderer) renderFramework(vm RenderVM) RenderResult {
	shell := vm.Shell
	plan := MeasureLayout(shell, shell.Layout.Viewport)
	c := newCanvas(plan.Viewport.W, plan.Viewport.H)

	if plan.Header.W > 0 && plan.Header.H > 0 {
		renderHeader(c, shell.Header, plan.Header)
	}
	if plan.Footer.W > 0 && plan.Footer.H > 0 {
		renderFooter(c, shell.Footer, plan.Footer)
	}

	layers := make([]Layer, 0)
	for _, layout := range plan.Panels {
		switch layout.Panel.Presentation {
		case PanelPresentationSplitLine:
			renderSplitPanel(c, layout)
		default:
			renderCardPanel(c, layout)
		}
		contentResult := renderContent(c, layout.Panel.Content, layout.ContentRect)
		layers = append(layers, Layer{Kind: LayerPanel, Rect: layout.Rect, Lines: contentResult})
	}
	for _, floating := range plan.Floatings {
		layer := renderFloating(c, floating)
		if layer.Rect.W > 0 && layer.Rect.H > 0 {
			layers = append(layers, layer)
		}
	}

	overlayLayer := renderOverlay(c, shell.Overlay, plan.Overlay, plan.OverlayContentRect)
	if overlayLayer.Rect.W > 0 && overlayLayer.Rect.H > 0 {
		layers = append(layers, overlayLayer)
	}
	toastLayers := renderToasts(c, shell.Toasts, plan.Toasts)
	for _, layer := range toastLayers {
		layers = append(layers, layer)
	}

	lines := c.lines()
	return RenderResult{
		Content:    lines,
		Cursor:     plan.Cursor,
		HitRegions: plan.HitRegions,
		Metadata:   RenderMetadata{Width: c.width, Height: c.height},
		Layers:     layers,
		Theme:      renderer.Theme.WithFallback(),
	}
}

func renderHeader(c *canvas, header HeaderVM, rect Rect) {
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	workspace := header.Workspace
	if workspace == "" {
		workspace = header.Title
	}
	if workspace == "" {
		workspace = "termx"
	}
	tab := header.Tab
	if tab == "" {
		tab = "main"
	}
	left := []barSegment{
		barText(" ws:"+workspace+" ", StyleStatusAccent, 1),
		barSep(),
		barText(" tab:"+tab+" ", StyleStatus, 1),
		barText(" ⊕ ", StyleStatusAccent, 3),
	}
	if header.Notice != "" {
		left = append(left, barSep(), barText(" notice:"+header.Notice+" ", StyleStatusWarning, 2))
	}
	right := []barSegment{}
	if header.ActivePane != "" {
		right = append(right, barText(" active:"+header.ActivePane+" ", StyleStatus, 1))
	}
	if header.TerminalSummary != "" {
		right = append(right, barText(" "+header.TerminalSummary+" ", StyleStatusMuted, 2))
	}
	if header.FloatingSummary != "" {
		right = append(right, barText(" "+header.FloatingSummary+" ", StyleStatusMuted, 2))
	}
	c.writeLine(rect.X, rect.Y, rect.W, composeBarLine(left, right, rect.W), "shell:header", LayerChrome)
}

func renderFooter(c *canvas, footer FooterVM, rect Rect) {
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	mode := footer.Mode
	if mode == "" {
		mode = "live"
	}
	hintIsCritical := strings.HasPrefix(footer.Hint, "error:") || strings.HasPrefix(footer.Hint, "exited:")
	left := []barSegment{
		barText(" mode:"+mode+" ", StyleStatusAccent, 1),
	}
	left = appendBarSegment(left, "active:"+footer.ActiveTarget, StyleStatus, 1)
	if len(footer.Actions) > 0 {
		left = appendBarSegment(left, footerActionsLabel(footer.Actions, rect.W), StyleStatus, 1)
	}
	if footer.Hint != "" {
		style := StyleStatusMuted
		priority := 3
		if hintIsCritical {
			style = StyleStatusWarning
			priority = 0
		}
		left = appendBarSegment(left, "hint:"+footer.Hint, style, priority)
	}
	right := []barSegment{
		barText(" status:ready ", StyleStatusAccent, 3),
	}
	if footer.GlobalSummary != "" {
		right = append([]barSegment{barText(" "+footer.GlobalSummary+" ", StyleStatusMuted, 2)}, right...)
	}
	c.writeLine(rect.X, rect.Y, rect.W, composeBarLine(left, right, rect.W), "shell:footer", LayerChrome)
}

func appendFooterSegment(base string, segment string, width int) string {
	if segment == "" {
		return base
	}
	next := base + "  " + segment
	if width <= 0 || DisplayWidth(next) <= width {
		return next
	}
	return base
}

func footerActionsLabel(actions []string, width int) string {
	if len(actions) == 0 {
		return ""
	}
	limit := 56
	if width < 100 {
		limit = 34
	}
	if width >= 140 {
		limit = 72
	}
	kept := make([]string, 0, len(actions))
	for _, action := range actions {
		next := append(append([]string{}, kept...), action)
		if DisplayWidth("keys:"+strings.Join(next, " ")) > limit && len(kept) > 0 {
			break
		}
		kept = next
	}
	if len(kept) < len(actions) {
		tail := footerTailAction(actions)
		if !containsStringValue(kept, tail) {
			withTail := append(append([]string{}, kept...), tail)
			for len(withTail) > 1 && DisplayWidth("keys:"+strings.Join(withTail, " ")) > limit {
				withTail = append(withTail[:len(withTail)-2], withTail[len(withTail)-1])
			}
			kept = withTail
		}
	}
	return "keys:" + strings.Join(kept, " ")
}

func footerTailAction(actions []string) string {
	for i := len(actions) - 1; i >= 0; i-- {
		action := strings.TrimSpace(actions[i])
		if action != "" && !strings.HasPrefix(action, "esc") {
			return actions[i]
		}
	}
	if len(actions) == 0 {
		return ""
	}
	return actions[len(actions)-1]
}

func containsStringValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type barSegment struct {
	text     string
	style    StyleToken
	priority int
}

func barText(text string, style StyleToken, priority int) barSegment {
	return barSegment{text: text, style: style, priority: priority}
}

func barSep() barSegment {
	return barText("│", StyleStatusMuted, 1)
}

func appendBarSegment(segments []barSegment, text string, style StyleToken, priority int) []barSegment {
	if text == "" {
		return segments
	}
	return append(segments, barSep(), barText(" "+text+" ", style, priority))
}

func composeBarLine(left []barSegment, right []barSegment, width int) Line {
	if width <= 0 {
		return Line{}
	}
	left = trimBarSegments(left, width)
	right = trimBarSegments(right, width-barSegmentsWidth(left))
	total := barSegmentsWidth(left) + barSegmentsWidth(right)
	if total > width {
		right = trimBarSegments(right, width-barSegmentsWidth(left))
		total = barSegmentsWidth(left) + barSegmentsWidth(right)
	}
	spacer := width - total
	cells := make([]Cell, 0, len(left)+len(right)+1)
	cells = append(cells, cellsFromBarSegments(left)...)
	if spacer > 0 {
		cells = append(cells, Cell{Text: strings.Repeat(" ", spacer), Width: spacer, Style: StyleStatus, Safe: true})
	}
	cells = append(cells, cellsFromBarSegments(right)...)
	return Line{Cells: cells}
}

func trimBarSegments(segments []barSegment, width int) []barSegment {
	if width <= 0 {
		return nil
	}
	out := append([]barSegment(nil), segments...)
	for barSegmentsWidth(out) > width && len(out) > 0 {
		drop := lowestPriorityBarSegment(out)
		out = append(out[:drop], out[drop+1:]...)
	}
	if barSegmentsWidth(out) <= width {
		return cleanBarSegments(out)
	}
	return nil
}

func lowestPriorityBarSegment(segments []barSegment) int {
	index := len(segments) - 1
	for i, segment := range segments {
		if segment.priority > segments[index].priority {
			index = i
		}
	}
	return index
}

func barSegmentsWidth(segments []barSegment) int {
	width := 0
	for _, segment := range segments {
		width += DisplayWidth(segment.text)
	}
	return width
}

func cellsFromBarSegments(segments []barSegment) []Cell {
	cells := make([]Cell, 0, len(segments))
	for _, segment := range segments {
		if segment.text == "" {
			continue
		}
		style := segment.style
		if style == "" {
			style = StyleStatus
		}
		cells = append(cells, Cell{Text: segment.text, Width: DisplayWidth(segment.text), Style: style, Safe: true})
	}
	return cells
}

func cleanBarSegments(segments []barSegment) []barSegment {
	if len(segments) == 0 {
		return nil
	}
	out := make([]barSegment, 0, len(segments))
	for _, segment := range segments {
		isSep := segment.text == "│"
		if isSep && len(out) == 0 {
			continue
		}
		if isSep && len(out) > 0 && out[len(out)-1].text == "│" {
			continue
		}
		out = append(out, segment)
	}
	for len(out) > 0 && out[len(out)-1].text == "│" {
		out = out[:len(out)-1]
	}
	return out
}

func splitPanelRects(layout LayoutVM, body Rect) map[string]Rect {
	rects := make(map[string]Rect)
	if len(layout.Panels) == 1 || len(layout.Split.Children) == 0 {
		for _, panel := range layout.Panels {
			rects[panel.ID] = body
		}
		return rects
	}
	assignSplitRects(layout.Split, body, rects)
	for _, panel := range layout.Panels {
		if rects[panel.ID].W == 0 || rects[panel.ID].H == 0 {
			rects[panel.ID] = body
		}
	}
	return rects
}

func assignSplitRects(split SplitVM, rect Rect, out map[string]Rect) {
	if split.PaneID != "" || len(split.Children) == 0 {
		out[split.PaneID] = rect
		return
	}
	first := split.Children[0]
	second := split.Children[1]
	switch split.Direction {
	case SplitVertical:
		leftWidth := splitFirstExtent(split, rect.W)
		rightWidth := rect.W - leftWidth
		assignSplitRects(first, Rect{X: rect.X, Y: rect.Y, W: leftWidth, H: rect.H}, out)
		assignSplitRects(second, Rect{X: rect.X + leftWidth, Y: rect.Y, W: rightWidth, H: rect.H}, out)
	default:
		topHeight := splitFirstExtent(split, rect.H)
		bottomHeight := rect.H - topHeight
		assignSplitRects(first, Rect{X: rect.X, Y: rect.Y, W: rect.W, H: topHeight}, out)
		assignSplitRects(second, Rect{X: rect.X, Y: rect.Y + topHeight, W: rect.W, H: bottomHeight}, out)
	}
}

func splitFirstExtent(split SplitVM, total int) int {
	if total <= 1 {
		return total
	}
	// SplitVM 的 size hint 已由 state/reducer 投影完成；这里仅做纯 layout measurement。
	if split.FixedPaneID != "" {
		fixed := splitFixedExtent(split)
		if fixed > 0 {
			fixed = clampInt(fixed, 1, total-1)
			if splitContainsPane(split.Children[0], split.FixedPaneID) {
				return fixed
			}
			if splitContainsPane(split.Children[1], split.FixedPaneID) {
				return total - fixed
			}
		}
	}
	first := total / 2
	if split.Ratio > 0 && split.Ratio < 1 {
		first = int(float64(total) * split.Ratio)
	}
	first += split.BiasCells
	return clampInt(first, 1, total-1)
}

func splitFixedExtent(split SplitVM) int {
	if split.Direction == SplitVertical {
		return split.FixedCols
	}
	return split.FixedRows
}

func splitContainsPane(split SplitVM, paneID string) bool {
	if split.PaneID == paneID {
		return true
	}
	for _, child := range split.Children {
		if splitContainsPane(child, paneID) {
			return true
		}
	}
	return false
}

func clampInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func renderCardPanel(c *canvas, layout PanelLayoutPlan) {
	rect := layout.Rect
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	style := paneChromeStyle(layout.Panel)
	owner := "pane:" + layout.Panel.ID
	c.drawStyledPaneFrame(rect, style, owner, LayerPanel)
	renderPaneChromeSlots(c, rect, layout.Panel, style, owner)
}

func renderSplitPanel(c *canvas, layout PanelLayoutPlan) {
	rect := layout.Rect
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	style := paneChromeStyle(layout.Panel)
	owner := "pane:" + layout.Panel.ID
	body := layout.Body
	if body.W <= 0 || body.H <= 0 {
		body = rect
	}
	c.drawStyledSplitPaneChrome(rect, body, style, owner, LayerPanel)
	renderPaneChromeSlots(c, rect, layout.Panel, style, owner)
}

func panelTitle(panel PanelVM) string {
	title := panel.Title
	if title == "" {
		title = panel.ID
	}
	return title
}

func paneChromeStyle(panel PanelVM) StyleToken {
	if panel.Active {
		return StyleAccent
	}
	return StyleMuted
}

func paneChromeStateText(panel PanelVM) string {
	state := "idle"
	if panel.Active {
		state = "active"
	}
	if panel.Content.Pending {
		state = "pending"
	}
	if panel.Content.Error != "" {
		state = "error"
	}
	if panel.Content.Empty {
		state = "empty"
	}
	marker := "◦"
	if panel.Active {
		marker = "●"
	}
	if panel.Content.Pending {
		marker = "…"
	}
	if panel.Content.Error != "" {
		marker = "×"
	}
	if panel.Content.Empty {
		marker = "○"
	}
	return marker + " " + state
}

func (c *canvas) drawStyledSplitPaneChrome(rect Rect, body Rect, style StyleToken, owner string, layer LayerKind) {
	startX := rect.X
	endX := splitPaneBorderEndX(rect, body)
	topY := rect.Y
	bottomY := body.Y + body.H - 1
	c.drawStyledSplitHBorder(startX, endX, topY, body, style, owner, layer)
	if rect.Y+rect.H >= body.Y+body.H {
		c.drawStyledSplitHBorder(startX, endX, bottomY, body, style, owner, layer)
	}
	leftStartY, leftEndY := splitPaneVerticalRange(rect, body)
	c.drawStyledPaneVBorder(rect.X, leftStartY, leftEndY, style, owner, layer)
	if rect.X+rect.W >= body.X+body.W {
		c.drawStyledPaneVBorder(body.X+body.W-1, leftStartY, leftEndY, style, owner, layer)
	}
}

func splitPaneBorderEndX(rect Rect, body Rect) int {
	endX := rect.X + rect.W - 1
	if rect.X+rect.W < body.X+body.W {
		endX++
	}
	return minInt(endX, body.X+body.W-1)
}

func splitPaneVerticalRange(rect Rect, body Rect) (int, int) {
	startY := rect.Y + 1
	endY := rect.Y + rect.H - 1
	if rect.Y+rect.H >= body.Y+body.H {
		endY--
	}
	return startY, endY
}

func (c *canvas) drawStyledSplitHBorder(startX int, endX int, y int, body Rect, style StyleToken, owner string, layer LayerKind) {
	if startX > endX {
		return
	}
	for x := startX; x <= endX; x++ {
		connections := uint8(boxConnLeft | boxConnRight)
		if x == startX {
			connections = boxConnRight
			if y > body.Y {
				connections |= boxConnUp
			}
			if y < body.Y+body.H-1 {
				connections |= boxConnDown
			}
		} else if x == endX {
			connections = boxConnLeft
			if y > body.Y {
				connections |= boxConnUp
			}
			if y < body.Y+body.H-1 {
				connections |= boxConnDown
			}
		}
		c.mergeStyledBoxCell(x, y, connections, style, owner, layer)
	}
}

func renderPaneChromeSlots(c *canvas, rect Rect, panel PanelVM, style StyleToken, owner string) {
	if rect.W < 4 || rect.H <= 0 {
		return
	}
	actionX := paneActionRect(rect).X
	if actionX <= rect.X {
		actionX = rect.X + rect.W
	}
	titleLimit := minInt(actionX-1, rect.X+rect.W-2)
	stateText := " " + paneChromeStateText(panel) + " "
	stateWidth := DisplayWidth(stateText)
	if rect.W >= 28 && actionX-stateWidth-2 > rect.X+4 {
		stateX := actionX - stateWidth - 2
		c.overlayTextStyled(stateX, rect.Y, stateWidth, stateText, style, owner, LayerPanel)
		titleLimit = minInt(titleLimit, stateX-1)
	}
	titleX := rect.X + 2
	if titleLimit > titleX {
		c.overlayTextStyled(titleX, rect.Y, titleLimit-titleX, " "+panelTitle(panel)+" ", style, owner, LayerPanel)
	}
	renderPaneActionSlot(c, rect, panel, style, owner)
}

func renderPaneActionSlot(c *canvas, rect Rect, panel PanelVM, style StyleToken, owner string) {
	if rect.W < 12 || rect.H <= 0 {
		return
	}
	token := "[·]"
	if panel.Active {
		token = "[x]"
	}
	c.overlayTextStyled(rect.X+rect.W-4, rect.Y, 3, token, style, owner, LayerPanel)
}

func renderFloating(c *canvas, layout FloatingLayoutPlan) Layer {
	rect := layout.Rect
	if rect.W <= 0 || rect.H <= 0 {
		return Layer{}
	}
	floating := layout.Floating
	style := StyleMuted
	if floating.Active {
		style = StyleAccent
	}
	owner := "floating:" + floating.ID
	c.fillStyledRect(rect, StyleOverlay, owner, LayerFloating)
	c.drawStyledBox(rect, roundedBoxStyle, style, owner, LayerFloating)
	title := floating.Title
	if title == "" {
		title = floating.ID
	}
	state := "float"
	if floating.Active {
		state = "active"
	}
	if floating.Collapsed {
		state = "collapsed"
	}
	c.overlayTextStyled(rect.X+2, rect.Y, maxInt(0, rect.W-6), " "+title+" "+state+" ", style, owner, LayerFloating)
	if rect.W >= 12 {
		c.overlayTextStyled(rect.X+rect.W-4, rect.Y, 3, "[x]", style, owner, LayerFloating)
	}
	if rect.W >= 2 && rect.H >= 2 {
		c.overlayTextStyled(rect.X+rect.W-2, rect.Y+rect.H-1, 1, "◢", style, owner, LayerFloating)
	}
	var contentLines []Line
	if !floating.Collapsed {
		contentLines = renderContent(c, floating.Content, layout.ContentRect)
	}
	return Layer{Kind: LayerFloating, Rect: rect, Lines: contentLines}
}

func renderContent(c *canvas, content ContentVM, rect Rect) []Line {
	if rect.W <= 0 || rect.H <= 0 {
		return nil
	}
	lines := content.Lines
	if len(lines) == 0 {
		lines = []Line{NewLine(content.Status)}
	}
	rendered := make([]Line, 0, rect.H)
	for i := 0; i < rect.H; i++ {
		line := Line{}
		if i < len(lines) {
			line = lines[i]
		}
		c.writeLine(rect.X, rect.Y+i, rect.W, line, string(content.Kind), LayerPanel)
		rendered = append(rendered, Line{Cells: cellsFromSegments(cellSegmentsFromLine(line, rect.W, string(content.Kind), LayerPanel))})
	}
	return rendered
}

func renderOverlay(c *canvas, overlay OverlayVM, rect Rect, contentRect Rect) Layer {
	if overlay.Kind == OverlayNone || overlay.Content.Kind == "" || rect.W <= 0 || rect.H <= 0 {
		return Layer{}
	}
	owner := "overlay:" + string(overlay.Kind)
	c.fillStyledRect(rect, StyleOverlay, owner, LayerOverlay)
	c.drawStyledBox(rect, roundedBoxStyle, StyleOverlay, owner, LayerOverlay)
	title := string(overlay.Kind)
	c.writeTextStyled(rect.X+2, rect.Y, maxInt(0, rect.W-4), " "+title+" ", StyleAccent, owner, LayerOverlay)
	contentLines := renderContent(c, overlay.Content, contentRect)
	return Layer{Kind: LayerOverlay, Rect: rect, Lines: contentLines}
}

func renderToasts(c *canvas, toasts []ToastVM, rects []Rect) []Layer {
	if len(toasts) == 0 || len(rects) == 0 {
		return nil
	}
	layers := make([]Layer, 0, len(toasts))
	for i, rect := range rects {
		toastIndex := len(toasts) - 1 - i
		if toastIndex < 0 {
			break
		}
		if rect.H <= 0 {
			break
		}
		toast := toasts[toastIndex]
		text := "[" + string(toast.Severity) + "] " + toast.Title
		if toast.Pending {
			text += " ..."
		}
		if c.width >= 40 && toast.Body != "" {
			text += " " + toast.Body
		}
		owner := "toast:" + toast.ID
		c.fillStyledRect(rect, StyleToast, owner, LayerToast)
		c.drawStyledBox(rect, roundedBoxStyle, toastSeverityStyle(toast.Severity), owner, LayerToast)
		if rect.H > 1 {
			textWidth := minInt(DisplayWidth(text), maxInt(0, rect.W-5))
			c.writeTextStyled(rect.X+1, rect.Y+1, textWidth, text, toastSeverityStyle(toast.Severity), owner, LayerToast)
			if rect.W >= 8 {
				c.writeTextStyled(rect.X+rect.W-4, rect.Y+1, 3, "[×]", StyleMuted, owner, LayerToast)
			}
		}
		layers = append(layers, Layer{Kind: LayerToast, Rect: rect})
	}
	return layers
}

func toastSeverityStyle(severity ToastSeverity) StyleToken {
	switch severity {
	case ToastSuccess:
		return StyleSuccess
	case ToastWarning:
		return StyleWarning
	case ToastError:
		return StyleDanger
	default:
		return StyleInfo
	}
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
