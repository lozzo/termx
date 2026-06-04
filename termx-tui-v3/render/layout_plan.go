package render

type LayoutPlan struct {
	Viewport           Rect
	Header             Rect
	Footer             Rect
	Body               Rect
	Panels             []PanelLayoutPlan
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
	ContentRect Rect
}

func MeasureLayout(shell ShellVM, viewport Rect) LayoutPlan {
	viewport.W = normalizeViewportDimension(viewport.W, defaultWidth)
	viewport.H = normalizeViewportDimension(viewport.H, defaultHeight)
	viewport.X = 0
	viewport.Y = 0

	body := viewport
	plan := LayoutPlan{Viewport: viewport}
	if shell.Header.Visible && body.H > 0 {
		plan.Header = Rect{X: 0, Y: 0, W: viewport.W, H: 1}
		body.Y++
		body.H--
	}
	if shell.Footer.Visible && body.H > 0 {
		body.H--
		plan.Footer = Rect{X: 0, Y: body.Y + body.H, W: viewport.W, H: 1}
	}
	plan.Body = body
	plan.Panels = measurePanels(shell.Layout, body)
	plan.Overlay = measureOverlay(shell.Overlay, viewport)
	plan.OverlayContentRect = measureOverlayContentRect(plan.Overlay)
	plan.Toasts = measureToasts(shell.Toasts, viewport)
	plan.Cursor, plan.CursorRect = measureCursor(shell, plan)
	plan.HitRegions = measureHitRegions(shell, plan)
	return plan
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
		contentRect := measurePanelContentRect(panel, rect)
		out[i] = PanelLayoutPlan{Panel: panel, Rect: rect, ContentRect: contentRect}
	}
	return out
}

func measurePanelContentRect(panel PanelVM, rect Rect) Rect {
	if panel.Presentation == PanelPresentationCard {
		return Rect{X: rect.X + 1, Y: rect.Y + 1, W: maxInt(0, rect.W-2), H: maxInt(0, rect.H-2)}
	}
	content := Rect{X: rect.X, Y: rect.Y + 1, W: rect.W, H: maxInt(0, rect.H-1)}
	if rect.X > 0 {
		content.X++
		content.W = maxInt(0, content.W-1)
	}
	return content
}

func measureOverlay(overlay OverlayVM, viewport Rect) Rect {
	if overlay.Kind == OverlayNone || overlay.Content.Kind == "" {
		return Rect{}
	}
	width := minInt(viewport.W-4, 48)
	height := minInt(viewport.H-4, 8)
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

func measureOverlayContentRect(rect Rect) Rect {
	if rect.W <= 0 || rect.H <= 0 {
		return Rect{}
	}
	return Rect{X: rect.X + 1, Y: rect.Y + 1, W: maxInt(0, rect.W-2), H: maxInt(0, rect.H-2)}
}

func measureToasts(toasts []ToastVM, viewport Rect) []Rect {
	if len(toasts) == 0 {
		return nil
	}
	rects := make([]Rect, 0, len(toasts))
	y := 1
	for i := len(toasts) - 1; i >= 0 && y < viewport.H; i-- {
		width := minInt(viewport.W, 36)
		if viewport.W < 40 {
			width = viewport.W
		}
		rect := Rect{X: maxInt(0, viewport.W-width), Y: y, W: width, H: minInt(3, viewport.H-y)}
		if rect.H <= 0 {
			break
		}
		rects = append(rects, rect)
		y += rect.H
	}
	return rects
}

func measureHitRegions(shell ShellVM, plan LayoutPlan) []HitRegion {
	regions := make([]HitRegion, 0)
	// 命中区域按前景到背景排序，后续鼠标分发可以直接取第一个匹配项。
	for _, rect := range plan.Toasts {
		regions = appendRegion(regions, HitRegion{Kind: HitRegionToast, Rect: rect}, plan.Viewport)
	}
	if plan.Overlay.W > 0 && plan.Overlay.H > 0 {
		regions = appendTranslatedRegions(regions, shell.Overlay.Content.HitRegions, plan.OverlayContentRect, plan.Viewport)
		regions = appendRegion(regions, HitRegion{Kind: HitRegionOverlay, Rect: plan.Overlay}, plan.Viewport)
	}
	if shell.Overlay.Opaque {
		return regions
	}
	for _, panel := range plan.Panels {
		regions = appendTranslatedRegions(regions, panel.Panel.Content.HitRegions, panel.ContentRect, plan.Viewport)
	}
	return regions
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
		return cursorWithRect(shell.Overlay.Content.Cursor, plan.OverlayContentRect)
	}
	for _, panel := range plan.Panels {
		if !panel.Panel.Active {
			continue
		}
		cursor := panel.Panel.Content.Cursor
		if !cursor.Visible {
			cursor = shell.Cursor
		}
		return cursorWithRect(cursor, panel.ContentRect)
	}
	return cursorWithRect(shell.Cursor, plan.Body)
}

func overlayOwnsCursor(overlay OverlayVM) bool {
	return overlay.Kind != OverlayNone && overlay.Content.Kind != "" && (overlay.Opaque || overlay.Content.Kind == ContentPrompt)
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
