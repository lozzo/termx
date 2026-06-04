package render

import "strings"

const (
	minFrameWidth  = 24
	minFrameHeight = 8
	defaultWidth   = 80
	defaultHeight  = 24
)

type canvas struct {
	width  int
	height int
	rows   []string
}

func newCanvas(width int, height int) *canvas {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	rows := make([]string, height)
	blank := strings.Repeat(" ", width)
	for i := range rows {
		rows[i] = blank
	}
	return &canvas{width: width, height: height, rows: rows}
}

func (c *canvas) writeText(x int, y int, width int, text string) {
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
	c.rows[y] = replaceCellRange(c.rows[y], x, width, FitText(text, width))
}

func (c *canvas) writeBoxCell(x int, y int, glyph string) {
	c.writeText(x, y, 1, glyph)
}

func (c *canvas) mergeBoxCell(x int, y int, connections uint8) {
	if x < 0 || y < 0 || x >= c.width || y >= c.height {
		return
	}
	existing := SliceCells(c.rows[y], x, x+1)
	if existingConnections, ok := boxConnectionsForGlyph(existing); ok {
		connections |= existingConnections
	}
	glyph, ok := boxGlyphForConnections(connections)
	if !ok {
		return
	}
	c.writeBoxCell(x, y, glyph)
}

func (c *canvas) drawRoundedBox(rect Rect) {
	c.drawBox(rect, roundedBoxStyle)
}

func (c *canvas) drawBox(rect Rect, style boxStyle) {
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	if rect.W == 1 {
		for y := 0; y < rect.H; y++ {
			c.writeBoxCell(rect.X, rect.Y+y, style.Vertical)
		}
		return
	}
	if rect.H == 1 {
		c.writeText(rect.X, rect.Y, rect.W, style.TopLeft+strings.Repeat(style.Horizontal, maxInt(0, rect.W-2))+style.TopRight)
		return
	}
	c.writeText(rect.X, rect.Y, rect.W, style.TopLeft+strings.Repeat(style.Horizontal, maxInt(0, rect.W-2))+style.TopRight)
	c.writeText(rect.X, rect.Y+rect.H-1, rect.W, style.BottomLeft+strings.Repeat(style.Horizontal, maxInt(0, rect.W-2))+style.BottomRight)
	for y := rect.Y + 1; y < rect.Y+rect.H-1; y++ {
		c.writeBoxCell(rect.X, y, style.Vertical)
		c.writeBoxCell(rect.X+rect.W-1, y, style.Vertical)
	}
}

func (c *canvas) drawConnectedHLine(x int, y int, width int) {
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
		c.mergeBoxCell(x+col, y, connections)
	}
}

func (c *canvas) drawConnectedVLine(x int, y int, height int) {
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
		c.mergeBoxCell(x, y+row, connections)
	}
}

func (c *canvas) lines() []Line {
	lines := make([]Line, len(c.rows))
	for i, row := range c.rows {
		lines[i] = NewLine(row)
	}
	return lines
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
	c.drawRoundedBox(rect)
	if rect.W > 4 {
		c.writeText(rect.X+2, rect.Y, rect.W-4, " "+title+" ")
	}
}

func renderSplitPanel(c *canvas, layout PanelLayoutPlan) {
	rect := layout.Rect
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	title := panelTitle(layout.Panel)
	chrome := " " + title
	if layout.Panel.Active {
		chrome += " *"
	}
	c.writeText(rect.X, rect.Y, rect.W, chrome)
	if rect.X > 0 {
		c.drawConnectedVLine(rect.X, rect.Y, rect.H)
	}
	if rect.Y > 0 {
		lineWidth := rect.W
		if rect.X+rect.W < c.width {
			lineWidth++
		}
		c.drawConnectedHLine(rect.X, rect.Y, lineWidth)
		c.writeText(rect.X+1, rect.Y, maxInt(0, rect.W-2), chrome)
	}
}

func panelTitle(panel PanelVM) string {
	title := panel.Title
	if title == "" {
		title = panel.ID
	}
	if panel.Active {
		title += " active"
	}
	return title
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
		text := ""
		if i < len(lines) {
			text = lines[i].String()
		}
		fitted := FitText(text, rect.W)
		c.writeText(rect.X, rect.Y+i, rect.W, fitted)
		rendered = append(rendered, NewLine(fitted))
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

func replaceCellRange(row string, x int, width int, value string) string {
	prefix := FitText(SliceCells(row, 0, x), x)
	suffix := SliceCells(row, x+width, DisplayWidth(row))
	return prefix + FitText(value, width) + suffix
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
