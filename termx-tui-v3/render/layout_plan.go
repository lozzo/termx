package render

type LayoutPlan struct {
	Viewport           Rect
	Header             Rect
	Footer             Rect
	Body               Rect
	Panels             []PanelLayoutPlan
	Floatings          []FloatingLayoutPlan
	Overlay            Rect
	OverlayContentRect Rect
	Toasts             []Rect
	HitRegions         []HitRegion
	Cursor             Cursor
	CursorRect         Rect
}

type PanelLayoutPlan struct {
	Panel       PanelVM
	Rect        Rect
	Body        Rect
	ContentRect Rect
}

type FloatingLayoutPlan struct {
	Floating    FloatingVM
	Rect        Rect
	ContentRect Rect
}

func MeasureLayout(shell ShellVM, viewport Rect) LayoutPlan {
	viewport.W = normalizeViewportDimension(viewport.W, defaultWidth)
	viewport.H = normalizeViewportDimension(viewport.H, defaultHeight)
	viewport.X = 0
	viewport.Y = 0

	body := viewport
	plan := LayoutPlan{Viewport: viewport}
	if shellFrameVisible(shell) && body.W > 2 {
		body.X = 1
		body.W = maxInt(0, body.W-2)
	}
	if shell.Header.Visible && body.H > 0 {
		headerH := shellBandHeight(viewport.H)
		headerH = minInt(headerH, body.H)
		plan.Header = Rect{X: 0, Y: 0, W: viewport.W, H: headerH}
		body.Y += headerH
		body.H -= headerH
	}
	if shell.Footer.Visible && body.H > 0 {
		footerH := shellBandHeight(viewport.H)
		footerH = minInt(footerH, body.H)
		body.H -= footerH
		plan.Footer = Rect{X: 0, Y: body.Y + body.H, W: viewport.W, H: footerH}
	}
	plan.Body = body
	plan.Panels = measurePanels(shell.Layout, body)
	plan.Floatings = measureFloatings(shell.Layout.Floating, viewport)
	plan.Overlay = measureOverlay(shell.Overlay, viewport)
	plan.OverlayContentRect = measureOverlayContentRect(plan.Overlay)
	plan.Toasts = measureToasts(shell.Toasts, viewport)
	plan.Cursor, plan.CursorRect = measureCursor(shell, plan)
	plan.HitRegions = measureHitRegions(shell, plan)
	return plan
}

func shellFrameVisible(shell ShellVM) bool {
	return shell.Header.Visible && shell.Footer.Visible
}

func shellBandHeight(viewportHeight int) int {
	if viewportHeight >= 8 {
		return 2
	}
	return 1
}

func normalizeViewportDimension(value int, fallback int) int {
	if value <= 0 {
		value = fallback
	}
	return maxInt(value, 1)
}

func measurePanels(layout LayoutVM, body Rect) []PanelLayoutPlan {
	if body.W <= 0 || body.H <= 0 || len(layout.Panels) == 0 {
		return nil
	}
	rects := splitPanelRects(layout, body)
	out := make([]PanelLayoutPlan, len(layout.Panels))
	for i, panel := range layout.Panels {
		rect := rects[panel.ID]
		if rect.W == 0 || rect.H == 0 {
			rect = body
		}
		contentRect := measurePanelContentRect(panel, rect, body)
		out[i] = PanelLayoutPlan{Panel: panel, Rect: rect, Body: body, ContentRect: contentRect}
	}
	return out
}

func measurePanelContentRect(panel PanelVM, rect Rect, body Rect) Rect {
	if panel.Presentation == PanelPresentationCard {
		return Rect{X: rect.X + 1, Y: rect.Y + 1, W: maxInt(0, rect.W-2), H: maxInt(0, rect.H-2)}
	}
	if body.W <= 0 || body.H <= 0 {
		body = rect
	}
	content := Rect{X: rect.X + 1, Y: rect.Y + 1, W: maxInt(0, rect.W-1), H: maxInt(0, rect.H-1)}
	if rect.X+rect.W >= body.X+body.W {
		content.W = maxInt(0, content.W-1)
	}
	if rect.Y+rect.H >= body.Y+body.H {
		content.H = maxInt(0, content.H-1)
	}
	return content
}

func measureFloatings(floatings []FloatingVM, viewport Rect) []FloatingLayoutPlan {
	if len(floatings) == 0 {
		return nil
	}
	sorted := append([]FloatingVM(nil), floatings...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1].Z > sorted[j].Z; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	out := make([]FloatingLayoutPlan, 0, len(sorted))
	for _, floating := range sorted {
		rect := intersectRect(floating.Rect, viewport)
		if rect.W <= 0 || rect.H <= 0 {
			continue
		}
		contentRect := Rect{X: rect.X + 1, Y: rect.Y + 1, W: maxInt(0, rect.W-2), H: maxInt(0, rect.H-2)}
		if floating.Collapsed {
			contentRect = Rect{}
		}
		out = append(out, FloatingLayoutPlan{Floating: floating, Rect: rect, ContentRect: contentRect})
	}
	return out
}

func measureOverlay(overlay OverlayVM, viewport Rect) Rect {
	if overlay.Kind == OverlayNone || overlay.Content.Kind == "" {
		return Rect{}
	}
	if overlay.Content.Kind == ContentTerminalPicker || overlay.Content.Kind == ContentTerminalPool || overlay.Content.Kind == ContentWorkbenchTree || overlay.Content.Kind == ContentHelp {
		return measurePageOverlay(viewport)
	}
	width := minInt(maxInt(54, viewport.W*3/5), viewport.W-8)
	height := minInt(maxInt(10, viewport.H/3), viewport.H-4)
	if width < 16 || height < 4 {
		width = maxInt(8, viewport.W)
		height = maxInt(3, minInt(viewport.H, 4))
	}
	return Rect{
		X: maxInt(0, (viewport.W-width)/2),
		Y: maxInt(0, (viewport.H-height)/2),
		W: minInt(width, viewport.W),
		H: minInt(height, viewport.H),
	}
}

func measurePageOverlay(viewport Rect) Rect {
	width := minInt(maxInt(76, viewport.W-12), 132)
	height := minInt(maxInt(18, viewport.H-8), viewport.H-6)
	if width < 40 {
		width = maxInt(8, viewport.W)
	}
	if height < 10 {
		height = maxInt(3, minInt(viewport.H, 12))
	}
	width = minInt(width, viewport.W)
	height = minInt(height, viewport.H)
	return Rect{
		X: maxInt(0, (viewport.W-width)/2),
		Y: maxInt(0, (viewport.H-height)/2),
		W: width,
		H: height,
	}
}

func measureOverlayContentRect(rect Rect) Rect {
	if rect.W <= 0 || rect.H <= 0 {
		return Rect{}
	}
	if rect.W >= 48 && rect.H >= 10 {
		return Rect{X: rect.X + 4, Y: rect.Y + 3, W: maxInt(0, rect.W-8), H: maxInt(0, rect.H-5)}
	}
	if rect.W >= 28 && rect.H >= 6 {
		return Rect{X: rect.X + 2, Y: rect.Y + 2, W: maxInt(0, rect.W-4), H: maxInt(0, rect.H-3)}
	}
	return Rect{X: rect.X + 1, Y: rect.Y + 1, W: maxInt(0, rect.W-2), H: maxInt(0, rect.H-2)}
}

func measureToasts(toasts []ToastVM, viewport Rect) []Rect {
	if len(toasts) == 0 {
		return nil
	}
	rects := make([]Rect, 0, len(toasts))
	y := 3
	bottomLimit := maxInt(0, viewport.H-1)
	for i := len(toasts) - 1; i >= 0 && y < bottomLimit; i-- {
		width := minInt(maxInt(42, viewport.W/3), 56)
		width = minInt(width, viewport.W-4)
		if viewport.W < 40 {
			width = viewport.W
		}
		height := minInt(5, bottomLimit-y)
		if height < 4 {
			break
		}
		rect := Rect{X: maxInt(0, viewport.W-width-2), Y: y, W: width, H: height}
		rects = append(rects, rect)
		y += rect.H + 1
	}
	return rects
}

func measureHitRegions(shell ShellVM, plan LayoutPlan) []HitRegion {
	regions := make([]HitRegion, 0)
	// 命中区域按前景到背景排序，后续鼠标分发可以直接取第一个匹配项。
	for _, rect := range plan.Toasts {
		// toast 不再绘制 close token；这里只保留遮挡命中，避免鼠标穿透到底层 pane/overlay。
		regions = appendRegion(regions, HitRegion{Kind: HitRegionToast, Rect: rect}, plan.Viewport)
	}
	if plan.Overlay.W > 0 && plan.Overlay.H > 0 {
		regions = appendTranslatedRegions(regions, shell.Overlay.Content.HitRegions, plan.OverlayContentRect, plan.Viewport)
		regions = appendRegion(regions, HitRegion{Kind: HitRegionOverlay, Rect: plan.Overlay}, plan.Viewport)
	}
	if shell.Overlay.Opaque {
		return regions
	}
	regions = appendHeaderHitRegions(regions, shell.Header, plan.Header, plan.Viewport)
	for i := len(plan.Floatings) - 1; i >= 0; i-- {
		regions = appendFloatingHitRegions(regions, plan.Floatings[i], plan.Viewport)
	}
	for _, panel := range plan.Panels {
		regions = appendPaneActionRegions(regions, panel.Rect, panel.Panel.ID, plan.Viewport)
	}
	regions = appendSplitResizeHitRegions(regions, shell.Layout.Split, plan.Body, plan.Viewport, rootSplitPath)
	for _, panel := range plan.Panels {
		regions = appendPanelChromeHitRegions(regions, panel, false, plan.Viewport)
		regions = appendTranslatedRegions(regions, panel.Panel.Content.HitRegions, panel.ContentRect, plan.Viewport)
		regions = appendPanelContentHitRegion(regions, panel, plan.Viewport)
	}
	return regions
}

func appendHeaderHitRegions(out []HitRegion, header HeaderVM, rect Rect, viewport Rect) []HitRegion {
	if rect.W <= 0 || rect.H <= 0 || !header.Visible {
		return out
	}
	x := rect.X + 1
	for _, segment := range headerLeftSegments(header) {
		width := DisplayWidth(segment.text)
		if width <= 0 {
			continue
		}
		if segment.actionID != "" {
			out = appendRegion(out, HitRegion{Kind: HitRegionContentAction, Rect: Rect{X: x, Y: rect.Y, W: width, H: rect.H}, ActionID: segment.actionID}, viewport)
		}
		x += width
		if x >= rect.X+rect.W {
			break
		}
	}
	return out
}

func appendFloatingHitRegions(out []HitRegion, floating FloatingLayoutPlan, viewport Rect) []HitRegion {
	if floating.Rect.W <= 0 || floating.Rect.H <= 0 {
		return out
	}
	id := floating.Floating.ID
	out = appendRegion(out, HitRegion{Kind: HitRegionContentAction, Rect: floatingActionRect(floating.Rect), PaneID: id, ActionID: ActionFloatingClose.String()}, viewport)
	out = appendRegion(out, HitRegion{Kind: HitRegionContentAction, Rect: floatingResizeRect(floating.Rect), PaneID: id, ActionID: ActionFloatingResizeDrag.String()}, viewport)
	out = appendRegion(out, HitRegion{Kind: HitRegionContentAction, Rect: paneChromeRect(floating.Rect), PaneID: id, ActionID: ActionFloatingMoveDrag.String()}, viewport)
	if floating.ContentRect.W > 0 && floating.ContentRect.H > 0 {
		out = appendTranslatedRegions(out, floating.Floating.Content.HitRegions, floating.ContentRect, viewport)
		out = appendRegion(out, HitRegion{Kind: HitRegionContentAction, Rect: floating.ContentRect, PaneID: id, ActionID: ActionFloatingRaise.String()}, viewport)
	}
	return out
}

func appendPanelChromeHitRegions(out []HitRegion, panel PanelLayoutPlan, includeActions bool, viewport Rect) []HitRegion {
	if panel.Rect.W <= 0 || panel.Rect.H <= 0 {
		return out
	}
	paneID := panel.Panel.ID
	if includeActions {
		out = appendPaneActionRegions(out, panel.Rect, paneID, viewport)
	}
	out = appendRegion(out, HitRegion{Kind: HitRegionPaneChrome, Rect: paneChromeRect(panel.Rect), PaneID: paneID, ActionID: ActionPaneFocus.String()}, viewport)
	return out
}

func appendPaneActionRegions(out []HitRegion, rect Rect, paneID string, viewport Rect) []HitRegion {
	actionRect := paneActionRect(rect)
	items := paneChromeActionItems(rect.W)
	x := actionRect.X
	for i, item := range items {
		if i > 0 {
			x += 2
		}
		width := DisplayWidth(item.Text)
		if width <= 0 {
			continue
		}
		out = appendRegion(out, HitRegion{Kind: HitRegionPaneAction, Rect: Rect{X: x, Y: actionRect.Y, W: width, H: actionRect.H}, PaneID: paneID, ActionID: item.ActionID}, viewport)
		x += width
	}
	return out
}

const rootSplitPath = "root"

func appendSplitResizeHitRegions(out []HitRegion, split SplitVM, rect Rect, viewport Rect, splitPath string) []HitRegion {
	if rect.W <= 1 || rect.H <= 1 || split.PaneID != "" || len(split.Children) < 2 {
		return out
	}
	first := split.Children[0]
	second := split.Children[1]
	targetPaneID := firstPaneIDInSplit(first)
	beforePaneID := lastPaneIDInSplit(first)
	afterPaneID := firstPaneIDInSplit(second)
	switch split.Direction {
	case SplitVertical:
		firstWidth := splitFirstExtent(split, rect.W)
		dividerX := rect.X + firstWidth
		if targetPaneID != "" {
			out = appendRegion(out, HitRegion{
				Kind:               HitRegionPaneResize,
				Rect:               Rect{X: dividerX, Y: rect.Y, W: 1, H: rect.H},
				PaneID:             targetPaneID,
				ActionID:           "pane.resize",
				Direction:          "right",
				SplitPath:          splitPath,
				ResizeBeforePaneID: beforePaneID,
				ResizeAfterPaneID:  afterPaneID,
				ResizeBeforeCells:  paneExtent(first, beforePaneID, firstWidth, rect.H, SplitVertical),
				ResizeAfterCells:   paneExtent(second, afterPaneID, rect.W-firstWidth, rect.H, SplitVertical),
				ResizeGroup:        resizeGroupItems(split, rect, SplitVertical),
			}, viewport)
		}
		out = appendSplitResizeHitRegions(out, first, Rect{X: rect.X, Y: rect.Y, W: firstWidth, H: rect.H}, viewport, childSplitPath(splitPath, 0))
		out = appendSplitResizeHitRegions(out, second, Rect{X: dividerX, Y: rect.Y, W: rect.W - firstWidth, H: rect.H}, viewport, childSplitPath(splitPath, 1))
	default:
		firstHeight := splitFirstExtent(split, rect.H)
		dividerY := rect.Y + firstHeight
		if targetPaneID != "" {
			out = appendRegion(out, HitRegion{
				Kind:               HitRegionPaneResize,
				Rect:               Rect{X: rect.X, Y: dividerY, W: rect.W, H: 1},
				PaneID:             targetPaneID,
				ActionID:           "pane.resize",
				Direction:          "down",
				SplitPath:          splitPath,
				ResizeBeforePaneID: beforePaneID,
				ResizeAfterPaneID:  afterPaneID,
				ResizeBeforeCells:  paneExtent(first, beforePaneID, rect.W, firstHeight, SplitHorizontal),
				ResizeAfterCells:   paneExtent(second, afterPaneID, rect.W, rect.H-firstHeight, SplitHorizontal),
				ResizeGroup:        resizeGroupItems(split, rect, SplitHorizontal),
			}, viewport)
		}
		out = appendSplitResizeHitRegions(out, first, Rect{X: rect.X, Y: rect.Y, W: rect.W, H: firstHeight}, viewport, childSplitPath(splitPath, 0))
		out = appendSplitResizeHitRegions(out, second, Rect{X: rect.X, Y: dividerY, W: rect.W, H: rect.H - firstHeight}, viewport, childSplitPath(splitPath, 1))
	}
	return out
}

func childSplitPath(parent string, index int) string {
	if parent == "" {
		parent = rootSplitPath
	}
	if index == 0 {
		return parent + "/0"
	}
	return parent + "/1"
}

func firstPaneIDInSplit(split SplitVM) string {
	if split.PaneID != "" {
		return split.PaneID
	}
	for _, child := range split.Children {
		if paneID := firstPaneIDInSplit(child); paneID != "" {
			return paneID
		}
	}
	return ""
}

func lastPaneIDInSplit(split SplitVM) string {
	if split.PaneID != "" {
		return split.PaneID
	}
	for i := len(split.Children) - 1; i >= 0; i-- {
		if paneID := lastPaneIDInSplit(split.Children[i]); paneID != "" {
			return paneID
		}
	}
	return ""
}

func paneExtent(split SplitVM, paneID string, width int, height int, axis SplitDirection) int {
	rects := make(map[string]Rect)
	assignSplitRects(split, Rect{W: width, H: height}, rects)
	rect := rects[paneID]
	if axis == SplitVertical {
		return rect.W
	}
	return rect.H
}

func resizeGroupItems(split SplitVM, rect Rect, axis SplitDirection) []ResizeGroupItem {
	rects := make(map[string]Rect)
	assignSplitRects(split, Rect{W: rect.W, H: rect.H}, rects)
	panes := paneIDsInSplitOrder(split, nil)
	divider := splitFirstExtent(split, rect.H)
	if axis == SplitVertical {
		divider = splitFirstExtent(split, rect.W)
	}
	out := make([]ResizeGroupItem, 0, len(panes))
	for _, paneID := range panes {
		paneRect := rects[paneID]
		cells := paneRect.H
		deltaSign := 0
		if axis == SplitVertical {
			cells = paneRect.W
			if paneRect.X+paneRect.W == divider {
				deltaSign = 1
			} else if paneRect.X == divider {
				deltaSign = -1
			}
		} else {
			if paneRect.Y+paneRect.H == divider {
				deltaSign = 1
			} else if paneRect.Y == divider {
				deltaSign = -1
			}
		}
		out = append(out, ResizeGroupItem{PaneID: paneID, Cells: cells, DeltaSign: deltaSign})
	}
	return out
}

func paneIDsInSplitOrder(split SplitVM, out []string) []string {
	if split.PaneID != "" {
		return append(out, split.PaneID)
	}
	for _, child := range split.Children {
		out = paneIDsInSplitOrder(child, out)
	}
	return out
}

func appendPanelEdgeResizeRegions(out []HitRegion, panel PanelLayoutPlan, viewport Rect) []HitRegion {
	rect := panel.Rect
	if rect.W <= 0 || rect.H <= 0 {
		return out
	}
	paneID := panel.Panel.ID
	rightX := rect.X + rect.W - 1
	if panel.Panel.Presentation == PanelPresentationSplitLine && rect.X+rect.W >= panel.Body.X+panel.Body.W {
		rightX = panel.Body.X + panel.Body.W - 1
	}
	if rect.W > 1 {
		out = appendRegion(out, HitRegion{Kind: HitRegionPaneResize, Rect: Rect{X: rightX, Y: rect.Y, W: 1, H: rect.H}, PaneID: paneID, ActionID: ActionPaneResize.String(), Direction: "right"}, viewport)
	}
	if rect.H > 1 {
		out = appendRegion(out, HitRegion{Kind: HitRegionPaneResize, Rect: Rect{X: rect.X, Y: rect.Y + rect.H - 1, W: rect.W, H: 1}, PaneID: paneID, ActionID: ActionPaneResize.String(), Direction: "down"}, viewport)
	}
	return out
}

func appendPanelContentHitRegion(out []HitRegion, panel PanelLayoutPlan, viewport Rect) []HitRegion {
	if panel.ContentRect.W <= 0 || panel.ContentRect.H <= 0 {
		return out
	}
	paneID := panel.Panel.ID
	out = appendRegion(out, HitRegion{Kind: HitRegionPaneContent, Rect: panel.ContentRect, PaneID: paneID, ActionID: ActionPaneFocus.String()}, viewport)
	return out
}

func paneActionRect(rect Rect) Rect {
	return chromeActionRect(rect, DisplayWidth(paneChromeActionText(rect.W)))
}

func floatingActionRect(rect Rect) Rect {
	if rect.W <= 0 {
		return Rect{X: rect.X, Y: rect.Y, W: 0, H: 1}
	}
	width := minInt(DisplayWidth(paneChromeCloseActionText()), maxInt(0, rect.W-2))
	return Rect{X: maxInt(rect.X, rect.X+rect.W-width-2), Y: rect.Y, W: width, H: 1}
}

func chromeActionRect(rect Rect, width int) Rect {
	inner := maxInt(0, rect.W-2)
	if rect.W <= 0 || width <= 0 || inner <= 0 {
		return Rect{X: rect.X, Y: rect.Y, W: 0, H: 1}
	}
	width = minInt(width, inner)
	return Rect{X: maxInt(rect.X, rect.X+rect.W-1-width), Y: rect.Y, W: width, H: 1}
}

func paneResizeRect(rect Rect) Rect {
	return Rect{X: maxInt(rect.X, rect.X+rect.W-1), Y: maxInt(rect.Y, rect.Y+rect.H-1), W: 1, H: 1}
}

func floatingResizeRect(rect Rect) Rect {
	return Rect{X: maxInt(rect.X, rect.X+rect.W-2), Y: maxInt(rect.Y, rect.Y+rect.H-1), W: minInt(2, rect.W), H: 1}
}

func paneChromeRect(rect Rect) Rect {
	return Rect{X: rect.X, Y: rect.Y, W: rect.W, H: minInt(1, rect.H)}
}

func appendTranslatedRegions(out []HitRegion, regions []HitRegion, origin Rect, viewport Rect) []HitRegion {
	for _, region := range regions {
		region.Rect.X += origin.X
		region.Rect.Y += origin.Y
		out = appendRegion(out, region, viewport)
	}
	return out
}

func appendRegion(out []HitRegion, region HitRegion, viewport Rect) []HitRegion {
	region.Rect = intersectRect(region.Rect, viewport)
	if region.Rect.W <= 0 || region.Rect.H <= 0 {
		return out
	}
	return append(out, region)
}

func measureCursor(shell ShellVM, plan LayoutPlan) (Cursor, Rect) {
	if overlayOwnsCursor(shell.Overlay) {
		return cursorWithRectOrAnchor(shell.Overlay.Content.Cursor, plan.OverlayContentRect)
	}
	for i := len(plan.Floatings) - 1; i >= 0; i-- {
		floating := plan.Floatings[i]
		if !floating.Floating.Active {
			continue
		}
		cursor := floating.Floating.Content.Cursor
		return cursorWithRectOrAnchor(cursor, floating.ContentRect)
	}
	for _, panel := range plan.Panels {
		if !panel.Panel.Active {
			continue
		}
		cursor := panel.Panel.Content.Cursor
		if !cursor.Visible {
			cursor = shell.Cursor
		}
		return cursorWithRectOrAnchor(cursor, panel.ContentRect)
	}
	return cursorWithRectOrAnchor(shell.Cursor, plan.Body)
}

func overlayOwnsCursor(overlay OverlayVM) bool {
	return overlay.Kind != OverlayNone && overlay.Content.Kind != "" && (overlay.Opaque || overlay.Content.Kind == ContentPrompt || overlay.Content.Kind == ContentTerminalPicker || overlay.Content.Kind == ContentTerminalPool || overlay.Content.Kind == ContentWorkbenchTree || overlay.Content.Kind == ContentHelp)
}

func cursorWithRect(cursor Cursor, origin Rect) (Cursor, Rect) {
	if !cursor.Visible || origin.W <= 0 || origin.H <= 0 {
		return Cursor{}, Rect{}
	}
	rect := Rect{X: origin.X + cursor.Col, Y: origin.Y + cursor.Row, W: 1, H: 1}
	if intersectRect(rect, origin) != rect {
		return Cursor{}, Rect{}
	}
	return cursor, rect
}

func cursorWithRectOrAnchor(cursor Cursor, origin Rect) (Cursor, Rect) {
	if measured, rect := cursorWithRect(cursor, origin); measured.Visible {
		return measured, rect
	}
	if origin.W <= 0 || origin.H <= 0 {
		return Cursor{}, Rect{}
	}
	// 中文输入法候选区跟随宿主真实光标；内容暂无 cursor 时也要把隐藏光标锚到输入目标内。
	anchor := Cursor{Visible: true, Row: 0, Col: 0, Shape: CursorShapeBar}
	return anchor, Rect{X: origin.X, Y: origin.Y, W: 1, H: 1}
}

func intersectRect(left Rect, right Rect) Rect {
	x1 := maxInt(left.X, right.X)
	y1 := maxInt(left.Y, right.Y)
	x2 := minInt(left.X+maxInt(0, left.W), right.X+maxInt(0, right.W))
	y2 := minInt(left.Y+maxInt(0, left.H), right.Y+maxInt(0, right.H))
	if x2 <= x1 || y2 <= y1 {
		return Rect{}
	}
	return Rect{X: x1, Y: y1, W: x2 - x1, H: y2 - y1}
}
