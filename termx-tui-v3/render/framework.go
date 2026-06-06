package render

import (
	"strconv"
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
	ansiStyle    ANSICellStyle
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
				Text:      cell.text,
				Width:     width,
				Style:     cell.style,
				ANSIStyle: cell.ansiStyle,
				Safe:      cell.safe,
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
		text:      segment.text,
		width:     segment.width,
		style:     segment.style,
		ansiStyle: segment.ansiStyle,
		owner:     segment.owner,
		layer:     segment.layer,
		safe:      segment.safe,
	}
	for col := x + 1; col < x+segment.width; col++ {
		c.rows[y][col] = canvasCell{
			width:        0,
			style:        segment.style,
			ansiStyle:    segment.ansiStyle,
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
	text      string
	width     int
	style     StyleToken
	ansiStyle ANSICellStyle
	owner     string
	layer     LayerKind
	safe      bool
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
			segment.ansiStyle = cell.ANSIStyle
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
			Text:      segment.text,
			Width:     segment.width,
			Style:     segment.style,
			ANSIStyle: segment.ansiStyle,
			Safe:      segment.safe,
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

	toastLayers := renderToasts(c, shell.Toasts, plan.Toasts)
	for _, layer := range toastLayers {
		layers = append(layers, layer)
	}
	overlayLayer := renderOverlay(c, shell.Overlay, plan.Overlay, plan.OverlayContentRect)
	if overlayLayer.Rect.W > 0 && overlayLayer.Rect.H > 0 {
		layers = append(layers, overlayLayer)
	}

	lines := c.lines()
	return RenderResult{
		Content:    lines,
		Cursor:     plan.Cursor,
		CursorRect: plan.CursorRect,
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
		barText(" "+workspace+" ", StyleStatusAccent, 1),
	}
	left = append(left, headerTabSegments(tab)...)
	left = append(left, barSep(), barText(" ＋ ", StyleSuccess, 3))
	if active := compactHeaderMeta("pane", header.ActivePane); active != "" {
		left = append(left, barSep(), barText(" "+active+" ", StyleStatusMuted, 4))
	}
	right := []barSegment{}
	if header.Notice != "" {
		right = append(right, barText(" ! "+header.Notice+" ", StyleStatusWarning, 0))
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
	left := []barSegment{}
	if mode != "live" && mode != "normal" {
		left = append(left, barText(" "+strings.ToUpper(mode)+" ", StyleStatusAccent, 1), barSep())
	}
	if len(footer.Actions) > 0 {
		left = appendFooterActionSegments(left, footer.Actions, rect.W)
	}
	if len(left) == 0 {
		left = append(left, barText(" [Ctrl] ", StyleStatusAccent, 1))
	}
	right := footerMetadataSegments(footer, hintIsCritical)
	c.writeLine(rect.X, rect.Y, rect.W, composeBarLine(left, right, rect.W), "shell:footer", LayerChrome)
}

func footerMetadataSegments(footer FooterVM, hintIsCritical bool) []barSegment {
	right := []barSegment{}
	if target := compactActiveTarget(footer.ActiveTarget); target != "" {
		right = append(right, barText(" "+target+" ", StyleStatusAccent, 2))
	}
	if footer.GlobalSummary != "" {
		for _, token := range metadataTokens(compactGlobalSummary(footer.GlobalSummary)) {
			right = append(right, barText(" "+token+" ", StyleStatusMuted, 3))
		}
	}
	if footer.Hint != "" {
		style := StyleStatusMuted
		priority := 3
		if hintIsCritical {
			style = StyleStatusWarning
			priority = 0
		}
		right = append(right, barText(" "+footer.Hint+" ", style, priority))
	}
	return right
}

func headerTabSegments(tab string) []barSegment {
	fields := strings.Fields(tab)
	if len(fields) == 0 {
		fields = []string{"[main]"}
	}
	segments := make([]barSegment, 0, len(fields)*3)
	for index, field := range fields {
		active := strings.HasPrefix(field, "[") && strings.HasSuffix(field, "]")
		if len(fields) == 1 {
			active = true
		}
		label := strings.Trim(field, "[]")
		if label == "" {
			label = field
		}
		if active {
			segments = append(segments,
				barSep(),
				barText(" "+intLabel(index+1)+":"+label+" ", StyleStatus, 1),
				barText("× ", StyleStatusWarning, 2),
			)
			continue
		}
		segments = append(segments,
			barSep(),
			barText(" "+intLabel(index+1)+":"+label+" ", StyleStatusMuted, 2),
		)
	}
	return segments
}

func intLabel(value int) string {
	if value < 0 {
		value = 0
	}
	return strconv.Itoa(value)
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

func appendFooterActionSegments(segments []barSegment, actions []string, width int) []barSegment {
	limit := 58
	if width < 100 {
		limit = 34
	}
	if width >= 140 {
		limit = 78
	}
	used := 0
	for _, action := range selectFooterActions(compactFooterActions(actions), limit) {
		key, label := splitFooterAction(action)
		if key == "" {
			continue
		}
		textToken := strings.ToUpper(label)
		tokenWidth := DisplayWidth(" • ") + DisplayWidth(formatFooterKeyToken(key))
		if textToken != "" {
			tokenWidth += 1 + DisplayWidth(textToken)
		}
		if used > 0 && used+tokenWidth > limit {
			break
		}
		if used == 0 && tokenWidth > limit {
			break
		}
		if len(segments) > 0 {
			segments = append(segments, barText(" • ", StyleStatusMuted, 1))
		}
		segments = append(segments, barText(formatFooterKeyToken(key), footerActionKeyStyle(key, textToken), 1))
		if textToken != "" {
			segments = append(segments, barText(" "+textToken, StyleStatus, 1))
		}
		used += tokenWidth
	}
	return segments
}

func selectFooterActions(actions []string, limit int) []string {
	selected := make([]string, 0, len(actions))
	used := 0
	for _, action := range actions {
		tokenWidth := footerActionDisplayWidth(action)
		if tokenWidth <= 0 {
			continue
		}
		if len(selected) > 0 && used+tokenWidth > limit {
			break
		}
		if len(selected) == 0 && tokenWidth > limit {
			break
		}
		selected = append(selected, action)
		used += tokenWidth
	}
	tail := footerTailAction(actions)
	if tail == "" || containsStringValue(selected, tail) {
		return selected
	}
	tailWidth := footerActionDisplayWidth(tail)
	for len(selected) > 0 && used+tailWidth > limit {
		dropped := selected[len(selected)-1]
		selected = selected[:len(selected)-1]
		used -= footerActionDisplayWidth(dropped)
	}
	if tailWidth <= limit && used+tailWidth <= limit {
		selected = append(selected, tail)
	}
	return selected
}

func footerActionDisplayWidth(action string) int {
	key, label := splitFooterAction(action)
	if key == "" {
		return 0
	}
	width := DisplayWidth(" • ") + DisplayWidth(formatFooterKeyToken(key))
	if label != "" {
		width += 1 + DisplayWidth(strings.ToUpper(label))
	}
	return width
}

func formatFooterKeyToken(key string) string {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(key, "^") && len(key) > 1 {
		return "[Ctrl] • [" + strings.ToUpper(strings.TrimPrefix(key, "^")) + "]"
	}
	return "[" + key + "]"
}

func compactFooterActions(actions []string) []string {
	out := make([]string, 0, len(actions))
	for _, action := range actions {
		action = strings.TrimSpace(action)
		if action == "" {
			continue
		}
		switch action {
		case "^R size":
			action = "^R resize"
		case "^W ws":
			action = "^W workspace"
		case "^F pick":
			action = "^F picker"
		case "^G":
			action = "^G global"
		}
		out = append(out, action)
	}
	return out
}

func splitFooterAction(action string) (string, string) {
	parts := strings.Fields(strings.TrimSpace(action))
	if len(parts) == 0 {
		return "", ""
	}
	key := parts[0]
	if len(parts) == 1 {
		return key, ""
	}
	return key, strings.Join(parts[1:], " ")
}

func footerActionKeyStyle(key string, label string) StyleToken {
	upper := strings.ToUpper(key + " " + label)
	switch {
	case strings.Contains(upper, "X") || strings.Contains(upper, "CLOSE") || strings.Contains(upper, "KILL"):
		return StyleStatusWarning
	case strings.Contains(upper, "R") || strings.Contains(upper, "RESIZE") || strings.Contains(upper, "SIZE"):
		return StyleStatusWarning
	case strings.Contains(upper, "P") || strings.Contains(upper, "PANE") || strings.Contains(upper, "PICK"):
		return StyleStatusAccent
	case strings.Contains(upper, "T") || strings.Contains(upper, "TAB") || strings.Contains(upper, "TREE"):
		return StyleStatusAccent
	default:
		return StyleStatusAccent
	}
}

func compactActiveTarget(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimPrefix(value, "pane:")
	value = strings.ReplaceAll(value, " live", "")
	value = strings.ReplaceAll(value, " copy", "")
	return "● " + value
}

func compactHeaderMeta(label string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return label + ":" + value
}

func compactGlobalSummary(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return value
}

func metadataTokens(value string) []string {
	return strings.Fields(strings.TrimSpace(value))
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
	marker := paneChromeWaitingGlyph()
	if panel.Active {
		marker = paneChromeRunningGlyph()
	}
	if panel.Content.Pending {
		marker = "…"
	}
	if panel.Content.Error != "" {
		marker = paneChromeExitedGlyph()
	}
	if panel.Content.Empty {
		marker = paneChromeWaitingGlyph()
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
	for _, slot := range paneChromeTopSlots(rect, panel, style) {
		c.overlayTextStyled(slot.x, rect.Y, DisplayWidth(slot.text), slot.text, slot.style, owner, LayerPanel)
	}
}

type paneChromeTopSlot struct {
	x        int
	text     string
	style    StyleToken
	priority int
}

func paneChromeTopSlots(rect Rect, panel PanelVM, borderStyle StyleToken) []paneChromeTopSlot {
	innerLeft := rect.X + 2
	innerRight := rect.X + rect.W - 1
	if innerRight <= innerLeft {
		return nil
	}

	rightLimit := innerRight
	slots := make([]paneChromeTopSlot, 0, 1)

	titleWidth := maxInt(0, rightLimit-innerLeft)
	if titleWidth > 0 {
		title := paneChromeTitleText(panel, titleWidth)
		if strings.TrimSpace(title) != "" {
			slots = append(slots, paneChromeTopSlot{x: innerLeft, text: title, style: paneChromeTitleStyle(panel, borderStyle)})
		}
	}
	return slots
}

func paneChromeActionText(width int) string {
	items := paneChromeActionItems(width)
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.Text)
	}
	return strings.Join(parts, "─")
}

func paneChromeActionItems(width int) []paneChromeActionItem {
	// tiled pane 的 Nerd Font/action 槽位尚未完成产品设计，默认不提前渲染。
	return nil
}

func paneChromeActionItemsWidth(items []paneChromeActionItem) int {
	if len(items) == 0 {
		return 0
	}
	width := 0
	for i, item := range items {
		if i > 0 {
			width++
		}
		width += DisplayWidth(item.Text)
	}
	return width
}

type paneChromeActionItem struct {
	Text     string
	ActionID string
}

func paneChromeActionClusterText() string {
	return strings.Join([]string{
		paneChromeBracketToken(paneChromeSplitHorizontalGlyph()),
		paneChromeBracketToken(paneChromeSplitVerticalGlyph()),
		paneChromeBracketToken(paneChromeZoomGlyph()),
		paneChromeBracketToken(paneChromeCloseGlyph()),
	}, "─")
}

func paneChromeCompactActionText() string {
	return strings.Join([]string{
		paneChromeBracketToken(paneChromeZoomGlyph()),
		paneChromeBracketToken(paneChromeCloseGlyph()),
	}, "─")
}

func paneChromeBracketToken(glyph string) string {
	glyph = strings.TrimSpace(glyph)
	if glyph == "" {
		glyph = "?"
	}
	return "[" + glyph + "]"
}

func paneChromeTitleText(panel PanelVM, width int) string {
	if width <= 0 {
		return ""
	}
	title := strings.TrimSpace(panelTitle(panel))
	if title == "" {
		return ""
	}
	if width <= 2 {
		return TruncateCells(title, width)
	}
	return " " + TruncateCells(title, width-2) + " "
}

func paneChromeTitleStyle(panel PanelVM, borderStyle StyleToken) StyleToken {
	if panel.Active {
		return StyleAccent
	}
	return borderStyle
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
	c.drawStyledBox(rect, squareBoxStyle, style, owner, LayerFloating)
	title := floating.Title
	if title == "" {
		title = floating.ID
	}
	state := "float"
	if floating.Active {
		state = paneChromeRunningGlyph() + " float"
	}
	if floating.Collapsed {
		state = paneChromeFloatingCollapseGlyph() + " collapsed"
	}
	renderChromeCardTitle(c, rect, title, state, paneChromeCloseActionText(), style, owner, LayerFloating)
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
	c.drawStyledBox(rect, squareBoxStyle, StyleOverlay, owner, LayerOverlay)
	title := overlayTitle(overlay.Kind)
	renderChromeCardTitle(c, rect, title, overlayChromeState(overlay), "esc", StyleAccent, owner, LayerOverlay)
	contentLines := renderContent(c, overlay.Content, contentRect)
	return Layer{Kind: LayerOverlay, Rect: rect, Lines: contentLines}
}

func overlayTitle(kind OverlayKind) string {
	title := strings.TrimSpace(string(kind))
	if title == "" {
		return "overlay"
	}
	return strings.ReplaceAll(title, "-", " ")
}

func renderChromeCardTitle(c *canvas, rect Rect, title string, state string, action string, style StyleToken, owner string, layer LayerKind) {
	if rect.W < 4 || rect.H <= 0 {
		return
	}
	titleX := rect.X + 2
	actionWidth := DisplayWidth(action)
	actionX := rect.X + rect.W - actionWidth - 2
	if action != "" && rect.W >= actionWidth+8 {
		c.overlayTextStyled(actionX, rect.Y, actionWidth, action, style, owner, layer)
	}
	titleLimit := rect.X + rect.W - 3
	if action != "" && actionX > titleX {
		titleLimit = actionX - 1
	}
	stateText := ""
	if state != "" {
		stateText = " · " + state + " "
	}
	if stateText != "" && rect.W >= 34 {
		stateWidth := DisplayWidth(stateText)
		stateX := titleLimit - stateWidth
		if stateX > titleX+DisplayWidth(title)+2 {
			c.overlayTextStyled(stateX, rect.Y, stateWidth, stateText, style, owner, layer)
			titleLimit = stateX - 1
		}
	}
	if titleLimit > titleX {
		c.overlayTextStyled(titleX, rect.Y, titleLimit-titleX, " "+title+" ", style, owner, layer)
	}
}

func overlayChromeState(overlay OverlayVM) string {
	if overlay.Content.Pending {
		return "… pending"
	}
	if overlay.Content.Error != "" {
		return "× error"
	}
	if overlay.Content.Empty {
		return "○ empty"
	}
	return "● open"
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
		owner := "toast:" + toast.ID
		c.fillStyledRect(rect, StyleToast, owner, LayerToast)
		if rect.W >= 2 {
			for y := rect.Y; y < rect.Y+rect.H; y++ {
				c.writeTextStyled(rect.X, y, 1, "│", StyleToastAccent, owner, LayerToast)
				c.writeTextStyled(rect.X+rect.W-1, y, 1, "│", StyleToastAccent, owner, LayerToast)
			}
		}
		if rect.H > 0 && rect.W > 4 {
			textRect := Rect{X: rect.X + 2, Y: rect.Y + rect.H/2, W: maxInt(0, rect.W-4), H: 1}
			c.writeTextStyled(textRect.X, textRect.Y, textRect.W, centerToastText(toastMessageLine(toast), textRect.W), StyleToast, owner, LayerToast)
		}
		layers = append(layers, Layer{Kind: LayerToast, Rect: rect})
	}
	return layers
}

func toastTitleLine(toast ToastVM) string {
	title := toast.Title
	if title == "" {
		title = string(toast.Severity)
	}
	if toast.Pending {
		title += " ..."
	}
	return title
}

func toastMessageLine(toast ToastVM) string {
	title := strings.TrimSpace(toast.Title)
	if title == "" {
		title = strings.TrimSpace(toast.Body)
	}
	if title == "" {
		title = string(toast.Severity)
	}
	if toast.Pending {
		title += " ..."
	}
	return title
}

func centerToastText(text string, width int) string {
	if width <= 0 {
		return ""
	}
	text = TruncateCells(text, width)
	pad := width - DisplayWidth(text)
	if pad <= 0 {
		return text
	}
	left := pad / 2
	right := pad - left
	return strings.Repeat(" ", left) + text + strings.Repeat(" ", right)
}

func paneChromeCloseActionText() string {
	return paneChromeBracketToken(paneChromeCloseGlyph())
}

func paneChromeSplitHorizontalActionText() string {
	return paneChromeBracketToken(paneChromeSplitHorizontalGlyph())
}

func paneChromeSplitVerticalActionText() string {
	return paneChromeBracketToken(paneChromeSplitVerticalGlyph())
}

func toastBodyLine(toast ToastVM) string {
	if toast.Body == "" {
		return string(toast.Severity)
	}
	return string(toast.Severity) + "  " + toast.Body
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
