package render

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
	regions = appendFooterHitRegions(regions, shell.Footer, plan.Footer, plan.FooterFrame, plan.Viewport)
	if len(plan.Panels) == 0 && plan.Body.W > 0 && plan.Body.H > 0 {
		regions = appendTranslatedRegions(regions, shell.Layout.BodyContent.HitRegions, plan.Body, plan.Viewport)
	}
	for i := len(plan.Floatings) - 1; i >= 0; i-- {
		regions = appendFloatingHitRegions(regions, plan.Floatings[i], plan.Viewport)
	}
	for _, panel := range plan.Panels {
		regions = appendPaneActionRegions(regions, panel.Panel, panel.Rect, panel.Panel.ID, plan.Viewport)
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
	x := rect.X
	for _, segment := range headerLeftSegments(header) {
		width := DisplayWidth(segment.text)
		if width <= 0 {
			continue
		}
		if segment.actionID != "" {
			out = appendRegion(out, HitRegion{Kind: HitRegionContentAction, Rect: Rect{X: x, Y: rect.Y, W: width, H: rect.H}, ActionID: segment.actionID, PaneID: segment.targetID}, viewport)
		}
		x += width
		if x >= rect.X+rect.W {
			break
		}
	}
	return out
}

func appendFooterHitRegions(out []HitRegion, footer FooterVM, rect Rect, frame Rect, viewport Rect) []HitRegion {
	if rect.W <= 0 || rect.H <= 0 || !footer.Visible {
		return out
	}
	_ = frame
	y := rect.Y
	x := rect.X
	currentAction := ""
	currentRect := Rect{}
	flush := func() {
		if currentAction != "" && currentRect.W > 0 {
			out = appendRegion(out, HitRegion{Kind: HitRegionContentAction, Rect: currentRect, ActionID: currentAction}, viewport)
		}
		currentAction = ""
		currentRect = Rect{}
	}
	for _, segment := range footerLeftSegments(footer, rect.W) {
		width := DisplayWidth(segment.text)
		if width <= 0 {
			continue
		}
		if segment.actionID == "" {
			flush()
			x += width
			continue
		}
		if currentAction == segment.actionID && currentRect.X+currentRect.W == x {
			currentRect.W += width
		} else {
			flush()
			currentAction = segment.actionID
			currentRect = Rect{X: x, Y: y, W: width, H: 1}
		}
		x += width
		if x >= rect.X+rect.W {
			break
		}
	}
	flush()
	return out
}

func appendFloatingHitRegions(out []HitRegion, floating FloatingLayoutPlan, viewport Rect) []HitRegion {
	if floating.Rect.W <= 0 || floating.Rect.H <= 0 {
		return out
	}
	id := floating.Floating.ID
	out = appendFloatingActionRegions(out, floating.Rect, id, viewport)
	out = appendRegion(out, HitRegion{Kind: HitRegionContentAction, Rect: floatingResizeRect(floating.Rect), PaneID: id, ActionID: ActionFloatingResizeDrag.String()}, viewport)
	out = appendRegion(out, HitRegion{Kind: HitRegionContentAction, Rect: paneChromeRect(floating.Rect), PaneID: id, ActionID: ActionFloatingMoveDrag.String()}, viewport)
	if floating.ContentRect.W > 0 && floating.ContentRect.H > 0 {
		out = appendTranslatedRegions(out, floating.Floating.Content.HitRegions, floating.ContentRect, viewport)
		out = appendRegion(out, HitRegion{Kind: HitRegionContentAction, Rect: floating.ContentRect, PaneID: id, ActionID: ActionFloatingRaise.String()}, viewport)
	}
	return out
}

func appendFloatingActionRegions(out []HitRegion, rect Rect, paneID string, viewport Rect) []HitRegion {
	primitive := FloatingChromePrimitive(FloatingVM{ID: paneID, Rect: rect}, rect, StyleAccent)
	for _, slot := range primitive.ActionSlots {
		if slot.Rect.W <= 0 || slot.ActionID == "" {
			continue
		}
		out = appendRegion(out, HitRegion{Kind: HitRegionContentAction, Rect: slot.Rect, PaneID: paneID, ActionID: slot.ActionID}, viewport)
	}
	return out
}

func appendPanelChromeHitRegions(out []HitRegion, panel PanelLayoutPlan, includeActions bool, viewport Rect) []HitRegion {
	if panel.Rect.W <= 0 || panel.Rect.H <= 0 {
		return out
	}
	paneID := panel.Panel.ID
	if includeActions {
		out = appendPaneActionRegions(out, panel.Panel, panel.Rect, paneID, viewport)
	}
	out = appendRegion(out, HitRegion{Kind: HitRegionPaneChrome, Rect: paneChromeRect(panel.Rect), PaneID: paneID, ActionID: ActionPaneFocus.String()}, viewport)
	return out
}

func appendPaneActionRegions(out []HitRegion, panel PanelVM, rect Rect, paneID string, viewport Rect) []HitRegion {
	primitive := PaneChromePrimitive(panel, rect, paneChromeStyle(panel))
	for _, slot := range primitive.ActionSlots {
		if slot.Rect.W <= 0 || slot.ActionID == "" {
			continue
		}
		out = appendRegion(out, HitRegion{Kind: HitRegionPaneAction, Rect: slot.Rect, PaneID: paneID, ActionID: slot.ActionID}, viewport)
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
				ActionID:           ActionPaneResize.String(),
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
				ActionID:           ActionPaneResize.String(),
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

func paneActionRect(panel PanelVM, rect Rect) Rect {
	return chromeActionRect(rect, paneChromeActionItemsWidth(visiblePaneChromeActionItems(panel, rect.W)))
}

func floatingActionRect(rect Rect) Rect {
	width := paneChromeActionItemsWidth(floatingChromeActionItems(rect.W))
	if rect.W <= 0 || width <= 0 {
		return Rect{X: rect.X, Y: rect.Y, W: 0, H: 1}
	}
	return chromeActionRect(rect, width)
}

func chromeActionRect(rect Rect, width int) Rect {
	inner := maxInt(0, rect.W-2)
	if rect.W <= 0 || width <= 0 || inner <= 0 {
		return Rect{X: rect.X, Y: rect.Y, W: 0, H: 1}
	}
	width = minInt(width, inner)
	return Rect{X: maxInt(rect.X, rect.X+rect.W-2-width), Y: rect.Y, W: width, H: 1}
}

func paneResizeRect(rect Rect) Rect {
	return Rect{X: maxInt(rect.X, rect.X+rect.W-1), Y: maxInt(rect.Y, rect.Y+rect.H-1), W: 1, H: 1}
}

func floatingResizeRect(rect Rect) Rect {
	return Rect{X: maxInt(rect.X, rect.X+rect.W-2), Y: maxInt(rect.Y, rect.Y+rect.H-1), W: minInt(1, rect.W), H: 1}
}

func paneChromeRect(rect Rect) Rect {
	return Rect{X: rect.X, Y: rect.Y, W: rect.W, H: minInt(1, rect.H)}
}

func appendTranslatedRegions(out []HitRegion, regions []HitRegion, origin Rect, viewport Rect) []HitRegion {
	for _, region := range regions {
		region.Rect.X += origin.X
		region.Rect.Y += origin.Y
		region.Rect = intersectRect(region.Rect, origin)
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
