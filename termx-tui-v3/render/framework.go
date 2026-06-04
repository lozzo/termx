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
	c.writeLine(x, y, width, Line{Cells: []Cell{{Text: text, Width: DisplayWidth(text), Style: style, Safe: true}}}, owner, layer)
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
	if len(shell.Layout.Panels) == 0 && len(vm.Lines) > 0 {
		shell = RenderVM{Shell: shell, Lines: vm.Lines, Status: vm.Status}.withFallbackShell()
	}
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

func (vm RenderVM) withFallbackShell() ShellVM {
	content := ContentVM{Kind: ContentTerminalLive, Lines: lineVMsFromStrings(vm.Lines), Status: vm.Status, HitRegions: vm.HitRegions}
	if len(content.Lines) == 0 {
		content.Lines = []Line{NewLine("live surface pending")}
	}
	return ShellVM{
		Header: HeaderVM{Visible: true, Title: "termx"},
		Footer: FooterVM{Visible: true, Mode: string(vm.Mode), Hint: vm.Status},
		Layout: LayoutVM{Panels: []PanelVM{{
			ID:           "pane",
			Title:        "pane",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content:      content,
		}}},
		Cursor: vm.Shell.Cursor,
	}
}

func renderHeader(c *canvas, header HeaderVM, rect Rect) {
	title := header.Title
	if title == "" {
		title = "termx"
	}
	text := " " + title
	if header.Notice != "" {
		text += "  " + header.Notice
	}
	c.writeText(rect.X, rect.Y, rect.W, text)
}

func renderFooter(c *canvas, footer FooterVM, rect Rect) {
	mode := footer.Mode
	if mode == "" {
		mode = "live"
	}
	text := " " + mode
	if footer.Hint != "" {
		text += "  " + footer.Hint
	}
	c.writeText(rect.X, rect.Y, rect.W, text)
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
		leftWidth := rect.W / 2
		rightWidth := rect.W - leftWidth
		assignSplitRects(first, Rect{X: rect.X, Y: rect.Y, W: leftWidth, H: rect.H}, out)
		assignSplitRects(second, Rect{X: rect.X + leftWidth, Y: rect.Y, W: rightWidth, H: rect.H}, out)
	default:
		topHeight := rect.H / 2
		bottomHeight := rect.H - topHeight
		assignSplitRects(first, Rect{X: rect.X, Y: rect.Y, W: rect.W, H: topHeight}, out)
		assignSplitRects(second, Rect{X: rect.X, Y: rect.Y + topHeight, W: rect.W, H: bottomHeight}, out)
	}
}

func renderCardPanel(c *canvas, layout PanelLayoutPlan) {
	rect := layout.Rect
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	title := panelTitle(layout.Panel)
	style := paneChromeStyle(layout.Panel)
	owner := "pane:" + layout.Panel.ID
	c.drawStyledBox(rect, squareBoxStyle, style, owner, LayerPanel)
	if rect.W > 4 {
		c.writeTextStyled(rect.X+2, rect.Y, rect.W-4, " "+title+" ", style, owner, LayerPanel)
	}
	renderPaneActionSlot(c, rect, layout.Panel, style, owner)
}

func renderSplitPanel(c *canvas, layout PanelLayoutPlan) {
	rect := layout.Rect
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	title := panelTitle(layout.Panel)
	style := paneChromeStyle(layout.Panel)
	owner := "pane:" + layout.Panel.ID
	chrome := paneChromeText(layout.Panel, title)
	c.writeTextStyled(rect.X, rect.Y, rect.W, chrome, style, owner, LayerPanel)
	if rect.X > 0 {
		c.drawStyledConnectedVLine(rect.X, rect.Y, rect.H, style, owner, LayerPanel)
	}
	if rect.Y > 0 {
		lineWidth := rect.W
		if rect.X+rect.W < c.width {
			lineWidth++
		}
		c.drawStyledConnectedHLine(rect.X, rect.Y, lineWidth, style, owner, LayerPanel)
		c.writeTextStyled(rect.X+1, rect.Y, maxInt(0, rect.W-2), chrome, style, owner, LayerPanel)
	}
	renderPaneActionSlot(c, rect, layout.Panel, style, owner)
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

func paneChromeText(panel PanelVM, title string) string {
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
	return " " + title + " " + state
}

func renderPaneActionSlot(c *canvas, rect Rect, panel PanelVM, style StyleToken, owner string) {
	if rect.W < 12 || rect.H <= 0 {
		return
	}
	token := "[·]"
	if panel.Active {
		token = "[x]"
	}
	c.writeTextStyled(rect.X+rect.W-4, rect.Y, 3, token, style, owner, LayerPanel)
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
	c.drawRoundedBox(rect)
	title := string(overlay.Kind)
	c.writeText(rect.X+2, rect.Y, maxInt(0, rect.W-4), " "+title+" ")
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
		c.drawRoundedBox(rect)
		if rect.H > 1 {
			c.writeText(rect.X+1, rect.Y+1, maxInt(0, rect.W-2), text)
		}
		layers = append(layers, Layer{Kind: LayerToast, Rect: rect})
	}
	return layers
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
