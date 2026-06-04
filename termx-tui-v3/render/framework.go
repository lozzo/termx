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
	width = maxInt(width, minFrameWidth)
	height = maxInt(height, minFrameHeight)
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

func (c *canvas) drawHLine(x int, y int, width int, left string, fill string, right string) {
	if width <= 0 {
		return
	}
	switch {
	case width == 1:
		c.writeText(x, y, 1, fill)
	case width == 2:
		c.writeText(x, y, 2, left+right)
	default:
		c.writeText(x, y, width, left+strings.Repeat(fill, width-2)+right)
	}
}

func (c *canvas) drawVLine(x int, y int, height int, fill string) {
	for row := 0; row < height; row++ {
		c.writeText(x, y+row, 1, fill)
	}
}

func (c *canvas) lines() []Line {
	lines := make([]Line, len(c.rows))
	for i, row := range c.rows {
		lines[i] = NewLine(row)
	}
	return lines
}

type panelLayout struct {
	Panel       PanelVM
	Rect        Rect
	ContentRect Rect
}

func (renderer Renderer) renderFramework(vm RenderVM) RenderResult {
	width, height := inferViewport(vm)
	c := newCanvas(width, height)
	shell := vm.Shell
	if len(shell.Layout.Panels) == 0 && len(vm.Lines) > 0 {
		shell = RenderVM{Shell: shell, Lines: vm.Lines, Status: vm.Status}.withFallbackShell()
	}

	body := Rect{X: 0, Y: 0, W: c.width, H: c.height}
	if shell.Header.Visible && c.height > 0 {
		renderHeader(c, shell.Header)
		body.Y++
		body.H--
	}
	if shell.Footer.Visible && body.H > 0 {
		body.H--
		renderFooter(c, shell.Footer, body.Y+body.H)
	}
	if body.H < 1 {
		body.H = 1
	}
	finalCursor := shell.Cursor

	panelLayouts := layoutPanels(shell.Layout, body)
	hitRegions := make([]HitRegion, 0)
	layers := make([]Layer, 0)
	for _, layout := range panelLayouts {
		switch layout.Panel.Presentation {
		case PanelPresentationSplitLine:
			renderSplitPanel(c, layout)
		default:
			renderCardPanel(c, layout)
		}
		contentResult := renderContent(c, layout.Panel.Content, layout.ContentRect)
		hitRegions = append(hitRegions, translateHitRegions(layout.Panel.Content.HitRegions, layout.ContentRect)...)
		layers = append(layers, Layer{Kind: LayerPanel, Rect: layout.Rect, Lines: contentResult})
	}

	overlayLayer := renderOverlay(c, shell.Overlay)
	if overlayLayer.Rect.W > 0 && overlayLayer.Rect.H > 0 {
		layers = append(layers, overlayLayer)
		hitRegions = append(hitRegions, HitRegion{Kind: HitRegionOverlay, Rect: overlayLayer.Rect})
		if shell.Overlay.Opaque {
			finalCursor = shell.Overlay.Content.Cursor
		}
	}
	toastLayers := renderToasts(c, shell.Toasts)
	for _, layer := range toastLayers {
		layers = append(layers, layer)
		hitRegions = append(hitRegions, HitRegion{Kind: HitRegionToast, Rect: layer.Rect})
	}

	lines := c.lines()
	return RenderResult{
		Content:    lines,
		Cursor:     finalCursor,
		HitRegions: hitRegions,
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

func inferViewport(vm RenderVM) (int, int) {
	width := vm.Shell.Layout.Viewport.W
	height := vm.Shell.Layout.Viewport.H
	if width <= 0 {
		width = maxInt(defaultWidth, maxLineWidth(lineVMsFromStrings(vm.Lines)))
	}
	if height <= 0 {
		height = defaultHeight
		if len(vm.Lines) > 0 {
			height = maxInt(minFrameHeight, len(vm.Lines)+4)
		}
	}
	return width, height
}

func renderHeader(c *canvas, header HeaderVM) {
	title := header.Title
	if title == "" {
		title = "termx"
	}
	text := " " + title
	if header.Notice != "" {
		text += "  " + header.Notice
	}
	c.writeText(0, 0, c.width, text)
}

func renderFooter(c *canvas, footer FooterVM, y int) {
	mode := footer.Mode
	if mode == "" {
		mode = "live"
	}
	text := " " + mode
	if footer.Hint != "" {
		text += "  " + footer.Hint
	}
	c.writeText(0, y, c.width, text)
}

func layoutPanels(layout LayoutVM, body Rect) []panelLayout {
	if body.W <= 0 || body.H <= 0 || len(layout.Panels) == 0 {
		return nil
	}
	rects := splitPanelRects(layout, body)
	out := make([]panelLayout, len(layout.Panels))
	for i, panel := range layout.Panels {
		rect := rects[panel.ID]
		if rect.W == 0 || rect.H == 0 {
			rect = body
		}
		contentRect := rect
		if panel.Presentation == PanelPresentationCard {
			contentRect = Rect{X: rect.X + 1, Y: rect.Y + 1, W: maxInt(0, rect.W-2), H: maxInt(0, rect.H-2)}
		} else {
			contentRect = Rect{X: rect.X, Y: rect.Y + 1, W: rect.W, H: maxInt(0, rect.H-1)}
		}
		out[i] = panelLayout{Panel: panel, Rect: rect, ContentRect: contentRect}
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

func renderCardPanel(c *canvas, layout panelLayout) {
	rect := layout.Rect
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	title := panelTitle(layout.Panel)
	c.drawHLine(rect.X, rect.Y, rect.W, "+", "-", "+")
	if rect.H > 1 {
		c.drawHLine(rect.X, rect.Y+rect.H-1, rect.W, "+", "-", "+")
		c.drawVLine(rect.X, rect.Y+1, rect.H-2, "|")
		c.drawVLine(rect.X+rect.W-1, rect.Y+1, rect.H-2, "|")
	}
	if rect.W > 4 {
		c.writeText(rect.X+2, rect.Y, rect.W-4, " "+title+" ")
	}
}

func renderSplitPanel(c *canvas, layout panelLayout) {
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
		c.drawVLine(rect.X, rect.Y, rect.H, "|")
	}
	if rect.Y > 0 {
		c.drawHLine(rect.X, rect.Y, rect.W, "+", "-", "+")
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

func renderOverlay(c *canvas, overlay OverlayVM) Layer {
	if overlay.Kind == OverlayNone || overlay.Content.Kind == "" {
		return Layer{}
	}
	width := minInt(c.width-4, 48)
	height := minInt(c.height-4, 8)
	if width < 16 || height < 4 {
		width = maxInt(8, c.width)
		height = maxInt(3, minInt(c.height, 4))
	}
	rect := Rect{X: maxInt(0, (c.width-width)/2), Y: maxInt(0, (c.height-height)/2), W: minInt(width, c.width), H: minInt(height, c.height)}
	c.drawHLine(rect.X, rect.Y, rect.W, "+", "-", "+")
	c.drawHLine(rect.X, rect.Y+rect.H-1, rect.W, "+", "-", "+")
	c.drawVLine(rect.X, rect.Y+1, rect.H-2, "|")
	c.drawVLine(rect.X+rect.W-1, rect.Y+1, rect.H-2, "|")
	title := string(overlay.Kind)
	c.writeText(rect.X+2, rect.Y, maxInt(0, rect.W-4), " "+title+" ")
	contentLines := renderContent(c, overlay.Content, Rect{X: rect.X + 1, Y: rect.Y + 1, W: maxInt(0, rect.W-2), H: maxInt(0, rect.H-2)})
	return Layer{Kind: LayerOverlay, Rect: rect, Lines: contentLines}
}

func renderToasts(c *canvas, toasts []ToastVM) []Layer {
	if len(toasts) == 0 {
		return nil
	}
	layers := make([]Layer, 0, len(toasts))
	y := 1
	for i := len(toasts) - 1; i >= 0 && y < c.height; i-- {
		toast := toasts[i]
		width := minInt(c.width, 36)
		if c.width < 40 {
			width = c.width
		}
		height := 3
		rect := Rect{X: maxInt(0, c.width-width), Y: y, W: width, H: minInt(height, c.height-y)}
		if rect.H <= 0 {
			break
		}
		text := "[" + string(toast.Severity) + "] " + toast.Title
		if toast.Pending {
			text += " ..."
		}
		if c.width >= 40 && toast.Body != "" {
			text += " " + toast.Body
		}
		c.drawHLine(rect.X, rect.Y, rect.W, "+", "-", "+")
		if rect.H > 1 {
			c.writeText(rect.X, rect.Y+1, rect.W, "|"+FitText(text, maxInt(0, rect.W-2))+"|")
		}
		if rect.H > 2 {
			c.drawHLine(rect.X, rect.Y+2, rect.W, "+", "-", "+")
		}
		layers = append(layers, Layer{Kind: LayerToast, Rect: rect})
		y += rect.H
	}
	return layers
}

func translateHitRegions(regions []HitRegion, origin Rect) []HitRegion {
	if len(regions) == 0 {
		return nil
	}
	out := make([]HitRegion, len(regions))
	for i, region := range regions {
		out[i] = region
		out[i].Rect.X += origin.X
		out[i].Rect.Y += origin.Y
	}
	return out
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
