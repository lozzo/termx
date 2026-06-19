package render

import (
	"strings"
)

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
	primitive := PaneChromePrimitive(layout.Panel, rect, style)
	c.drawStyledPaneFrame(rect, style, primitive.Owner, primitive.Layer)
	renderPaneChromePrimitive(c, primitive, layout.Panel)
}

func renderSplitPanel(c *canvas, layout PanelLayoutPlan) {
	rect := layout.Rect
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	style := paneChromeStyle(layout.Panel)
	body := layout.Body
	if body.W <= 0 || body.H <= 0 {
		body = rect
	}
	primitive := PaneChromePrimitive(layout.Panel, rect, style)
	c.drawStyledSplitPaneChrome(rect, body, style, primitive.Owner, primitive.Layer)
	renderPaneChromePrimitive(c, primitive, layout.Panel)
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

func renderPaneChromePrimitive(c *canvas, primitive ChromePrimitive, panel PanelVM) {
	if primitive.Rect.W < 4 || primitive.Rect.H <= 0 {
		return
	}
	for _, slot := range paneChromeTopSlots(primitive.Rect, panel, primitive.Style) {
		c.overlayTextStyled(slot.x, primitive.Rect.Y, slot.paintWidth(), slot.text, slot.style, primitive.Owner, primitive.Layer)
	}
}

type paneChromeTopSlot struct {
	x    int
	text string
	// 中文说明：layoutWidth 只用于保留右侧状态槽锚点；绘制仍按 text 宽度，避免空白覆盖顶边框。
	layoutWidth int
	style       StyleToken
	priority    int
	actionID    string
}

func (slot paneChromeTopSlot) paintWidth() int {
	return DisplayWidth(slot.text)
}

func (slot paneChromeTopSlot) advanceWidth() int {
	if slot.layoutWidth > 0 {
		return slot.layoutWidth
	}
	return slot.paintWidth()
}

func paneChromeTopSlots(rect Rect, panel PanelVM, borderStyle StyleToken) []paneChromeTopSlot {
	innerLeft := rect.X + 2
	innerRight := rect.X + rect.W - 1
	if innerRight <= innerLeft {
		return nil
	}

	rightLimit := innerRight
	actionItems := visiblePaneChromeActionItems(panel, rect.W)
	actions := paneChromeActionTextFromItems(actionItems)
	actionWidth := DisplayWidth(actions)
	slots := make([]paneChromeTopSlot, 0, 2)
	if actionWidth > 0 {
		actionX := innerRight - actionWidth - 1
		if actionX >= innerLeft {
			slots = append(slots, paneChromeTopSlot{x: actionX, text: actions, style: paneChromeActionClusterStyle(actionItems, paneChromeActionStyle(panel, borderStyle))})
			rightLimit = actionX - 1
		}
	}

	leftWidth := maxInt(0, rightLimit-innerLeft)
	if leftWidth > 0 {
		x := innerLeft
		for _, slot := range paneChromeLabelSlots(panel, borderStyle, leftWidth) {
			slot.x = x
			slots = append(slots, slot)
			x += slot.advanceWidth()
		}
	}
	return slots
}

func paneChromeLabelSlots(panel PanelVM, borderStyle StyleToken, width int) []paneChromeTopSlot {
	if width <= 0 {
		return nil
	}
	if panel.Chrome.Terminal.TerminalID != "" {
		return paneChromeTerminalLabelSlots(panel, borderStyle, width)
	}
	optionals := paneChromeOptionalLabelSlots(panel, borderStyle)
	for len(optionals) > 0 && paneChromeSlotsWidth(optionals)+paneChromeMinimumTitleWidth(panel) > width {
		optionals = optionals[:len(optionals)-1]
	}
	optionalWidth := paneChromeSlotsWidth(optionals)
	titleWidth := maxInt(0, width-optionalWidth)
	title := paneChromeTitleText(panel, titleWidth)
	slots := make([]paneChromeTopSlot, 0, 1+len(optionals))
	if strings.TrimSpace(title) != "" {
		slots = append(slots, paneChromeTopSlot{text: title, style: paneChromeTitleStyle(panel, borderStyle), priority: 0})
	}
	return append(slots, optionals...)
}

func paneChromeOptionalLabelSlots(panel PanelVM, borderStyle StyleToken) []paneChromeTopSlot {
	out := make([]paneChromeTopSlot, 0, 1+len(panel.Chrome.Meta))
	if state := paneChromeSlotText(panel.Chrome.State); state != "" {
		out = append(out, paneChromeTopSlot{text: " " + state + " ", style: paneChromeSlotStyle(panel.Chrome.State, borderStyle), priority: 2})
	}
	for _, meta := range panel.Chrome.Meta {
		if text := paneChromeSlotText(meta); text != "" {
			out = append(out, paneChromeTopSlot{text: " " + text + " ", style: paneChromeSlotStyle(meta, borderStyle), priority: 3})
		}
	}
	return out
}

func paneChromeSlotsWidth(slots []paneChromeTopSlot) int {
	width := 0
	for _, slot := range slots {
		width += slot.advanceWidth()
	}
	return width
}

func paneChromeMinimumTitleWidth(panel PanelVM) int {
	if strings.TrimSpace(paneChromeTitleSource(panel)) == "" {
		return 0
	}
	return 3
}

func paneChromeActionText(width int) string {
	items := paneChromeActionItems(width)
	return paneChromeActionTextFromItems(items)
}

func paneChromeActionTextFromItems(items []paneChromeActionItem) string {
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
	if width < 8 {
		return nil
	}
	items := paneChromeActionItemsFromSpecs(ActionPaneClose)
	full := paneChromeActionItemsFromSpecs(ActionPaneZoom, ActionPaneSplitRight, ActionPaneSplitDown, ActionPaneClose)
	if paneChromeActionItemsWidth(full) <= maxInt(0, width-6) {
		return full
	}
	return items
}

func visiblePaneChromeActionItems(panel PanelVM, width int) []paneChromeActionItem {
	actions := paneChromeActionItemsFromVM(panel.Chrome.Actions)
	if len(actions) == 0 {
		actions = paneChromeActionItems(width)
	}
	return fitPaneChromeActionItems(actions, width)
}

func paneChromeActionItemsFromVM(actions []ChromeActionVM) []paneChromeActionItem {
	out := make([]paneChromeActionItem, 0, len(actions))
	for _, action := range actions {
		text := strings.TrimSpace(action.Text)
		if text == "" || action.ActionID == "" {
			continue
		}
		if !strings.HasPrefix(text, "[") {
			text = paneChromeBracketToken(text)
		}
		out = append(out, paneChromeActionItem{
			Text:     text,
			ActionID: action.ActionID,
			Style:    action.Style,
		})
	}
	return out
}

func fitPaneChromeActionItems(actions []paneChromeActionItem, width int) []paneChromeActionItem {
	if width < 8 || len(actions) == 0 {
		return nil
	}
	if paneChromeActionItemsWidth(actions) <= maxInt(0, width-6) {
		return actions
	}
	for i := len(actions) - 1; i >= 0; i-- {
		if actions[i].ActionID == ActionPaneClose.String() {
			if DisplayWidth(actions[i].Text) <= maxInt(0, width-5) {
				return []paneChromeActionItem{actions[i]}
			}
			return nil
		}
	}
	return nil
}

func paneChromeActionItemsWidth(items []paneChromeActionItem) int {
	if len(items) == 0 {
		return 0
	}
	width := 0
	for i, item := range items {
		if i > 0 {
			width += 1
		}
		width += DisplayWidth(item.Text)
	}
	return width
}

type paneChromeActionItem struct {
	Text     string
	ActionID string
	Style    StyleToken
}

func paneChromeActionItemsFromSpecs(ids ...ActionID) []paneChromeActionItem {
	out := make([]paneChromeActionItem, 0, len(ids))
	for _, id := range ids {
		spec, ok := ActionSpecByID(id)
		if !ok || spec.ChromeGlyph == "" {
			continue
		}
		out = append(out, paneChromeActionItem{
			Text:     paneChromeBracketToken(spec.ChromeGlyph),
			ActionID: spec.ID.String(),
		})
	}
	return out
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
	title := strings.TrimSpace(paneChromeTitleSource(panel))
	if title == "" {
		return ""
	}
	if width <= 2 {
		return TruncateCells(title, width)
	}
	return " " + TruncateCells(title, width-2) + " "
}

func paneChromeTitleStyle(panel PanelVM, borderStyle StyleToken) StyleToken {
	if panel.Chrome.Title.Style != "" {
		return panel.Chrome.Title.Style
	}
	if panel.Active {
		return StyleAccent
	}
	return borderStyle
}

func paneChromeTitleSource(panel PanelVM) string {
	if text := strings.TrimSpace(panel.Chrome.Title.Text); text != "" {
		return text
	}
	return panelTitle(panel)
}

func paneChromeSlotText(slot ChromeSlotVM) string {
	return strings.TrimSpace(slot.Text)
}

func paneChromeSlotStyle(slot ChromeSlotVM, fallback StyleToken) StyleToken {
	if slot.Style != "" {
		return slot.Style
	}
	return fallback
}

func paneChromeActionStyle(panel PanelVM, borderStyle StyleToken) StyleToken {
	if panel.Active {
		return StyleAccent
	}
	return borderStyle
}

func paneChromeActionClusterStyle(items []paneChromeActionItem, fallback StyleToken) StyleToken {
	for _, item := range items {
		if item.Style != "" {
			return item.Style
		}
	}
	return fallback
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
	primitive := FloatingChromePrimitive(floating, rect, style)
	owner := primitive.Owner
	if floating.Chrome.FillOverlay {
		c.fillStyledRect(rect, StyleOverlay, owner, LayerFloating)
	}
	c.drawStyledBox(rect, squareBoxStyle, style, owner, LayerFloating)
	renderFloatingTerminalChrome(c, primitive)
	renderFloatingChromeActions(c, primitive)
	if rect.W >= 2 && rect.H >= 2 && floating.Chrome.ShowResizeHandle {
		c.overlayTextStyled(rect.X+rect.W-2, rect.Y+rect.H-1, 1, "v", style, owner, LayerFloating)
	}
	var contentResult ContentRenderResult
	if !floating.Collapsed {
		contentResult = renderContent(c, floating.Content, layout.ContentRect, owner+":content", LayerFloating)
		renderFloatingContentOverflowMarkers(c, layout, contentResult.Overflow)
	}
	return Layer{Kind: LayerFloating, Rect: rect, Lines: contentResult.Lines, ContentOverflow: contentResult.Overflow}
}

func renderFloatingTerminalChrome(c *canvas, primitive ChromePrimitive) {
	for _, slot := range primitive.LabelSlots {
		if slot.Rect.W <= 0 || strings.TrimSpace(slot.Text) == "" {
			continue
		}
		c.overlayTextStyled(slot.Rect.X, primitive.Rect.Y, slot.Rect.W, slot.Text, slot.Style, primitive.Owner, primitive.Layer)
	}
}

func renderFloatingChromeActions(c *canvas, primitive ChromePrimitive) {
	controlSlots := floatingChromeControlSlots(primitive.ActionSlots)
	text := chromeBracketActionClusterText(controlSlots)
	width := DisplayWidth(text)
	if width <= 0 {
		return
	}
	if len(controlSlots) == 0 {
		return
	}
	actionX := controlSlots[0].Rect.X
	if actionX < primitive.Rect.X+2 {
		return
	}
	c.overlayTextStyled(actionX, primitive.Rect.Y, width, text, primitive.Style, primitive.Owner, primitive.Layer)
}

func floatingChromeControlSlots(slots []ChromeSlot) []ChromeSlot {
	out := make([]ChromeSlot, 0, len(slots))
	for _, slot := range slots {
		switch slot.ActionID {
		case ActionResizeLayoutLock.String(), ActionTerminalTakeResizeOwner.String():
			continue
		default:
			out = append(out, slot)
		}
	}
	return out
}

func chromeSlotClusterText(slots []ChromeSlot) string {
	if len(slots) == 0 {
		return ""
	}
	parts := make([]string, 0, len(slots))
	for _, slot := range slots {
		parts = append(parts, slot.Text)
	}
	return strings.Join(parts, "─")
}

func chromeBracketActionClusterText(slots []ChromeSlot) string {
	filtered := make([]ChromeSlot, 0, len(slots))
	for _, slot := range slots {
		if strings.HasPrefix(strings.TrimSpace(slot.Text), "[") {
			filtered = append(filtered, slot)
		}
	}
	return chromeSlotClusterText(filtered)
}

// floating 没有分屏动作；右上角只复用 pane chrome 的括号化可用动作。
func floatingChromeActionItems(width int) []paneChromeActionItem {
	if width < 8 {
		return nil
	}
	items := paneChromeActionItemsFromSpecs(ActionFloatingCenter, ActionFloatingCollapse, ActionPaneZoom, ActionFloatingClose)
	if paneChromeActionItemsWidth(items) <= maxInt(0, width-6) {
		return items
	}
	closeOnly := paneChromeActionItemsFromSpecs(ActionFloatingClose)
	if paneChromeActionItemsWidth(closeOnly) <= maxInt(0, width-5) {
		return closeOnly
	}
	return nil
}

func renderContent(c *canvas, content ContentVM, rect Rect, owner string, layer LayerKind) ContentRenderResult {
	if rect.W <= 0 || rect.H <= 0 {
		return ContentRenderResult{}
	}
	result := RenderContentViewport(ContentRenderRequest{Rect: rect, Content: content})
	for i, line := range result.Lines {
		c.writeLine(rect.X, rect.Y+i, rect.W, line, owner, layer)
	}
	renderWorkbenchNavigatorSnapshotContent(c, content, rect, owner, layer)
	return result
}
