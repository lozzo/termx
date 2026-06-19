package render

func renderPanelContentOverflowMarkers(c *canvas, layout PanelLayoutPlan, overflow ContentOverflow) {
	style := paneChromeStyle(layout.Panel)
	owner := "panel:" + layout.Panel.ID + ":overflow"
	renderContentOverflowMarkers(c, layout.Rect, layout.ContentRect, overflow, style, owner, LayerChrome)
}

func renderFloatingContentOverflowMarkers(c *canvas, layout FloatingLayoutPlan, overflow ContentOverflow) {
	style := StyleMuted
	if layout.Floating.Active {
		style = StyleAccent
	}
	owner := "floating:" + layout.Floating.ID + ":overflow"
	renderContentOverflowMarkers(c, layout.Rect, layout.ContentRect, overflow, style, owner, LayerFloating)
}

func renderContentOverflowMarkers(c *canvas, chromeRect Rect, contentRect Rect, overflow ContentOverflow, style StyleToken, owner string, layer LayerKind) {
	if chromeRect.W <= 0 || chromeRect.H <= 0 || contentRect.W <= 0 || contentRect.H <= 0 {
		return
	}
	if marker := contentOverflowCornerMarker(overflow.Left, overflow.Top, "<", "^"); marker != "" {
		// 角标贴着拐角内侧绘制，保留 pane 本身的拐角 glyph。
		width := minInt(maxInt(0, chromeRect.W-2), DisplayWidth(marker))
		if width > 0 {
			c.overlayTextStyled(chromeRect.X+1, chromeRect.Y, width, marker, style, owner, layer)
		}
	}
	if marker := contentOverflowCornerMarker(overflow.Right, overflow.Bottom, ">", "v"); marker != "" {
		width := minInt(maxInt(0, chromeRect.W-2), DisplayWidth(marker))
		if width > 0 {
			x := chromeRect.X + chromeRect.W - 1 - width
			y := chromeRect.Y + chromeRect.H - 1
			c.overlayTextStyled(x, y, width, marker, style, owner, layer)
		}
	}
}

func contentOverflowCornerMarker(horizontal bool, vertical bool, horizontalMarker string, verticalMarker string) string {
	switch {
	case horizontal && vertical:
		return horizontalMarker + verticalMarker
	case horizontal:
		return horizontalMarker
	case vertical:
		return verticalMarker
	default:
		return ""
	}
}
