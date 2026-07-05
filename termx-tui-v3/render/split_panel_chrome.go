package render

func (c *canvas) drawStyledActiveSplitTrailingEdge(rect Rect, body Rect, style StyleToken, owner string, layer LayerKind) {
	if rect.X+rect.W < body.X+body.W {
		// 中文说明：内部 split divider 是共享格子；active pane 在左侧时必须主动声明右边框归属。
		startY, endY := splitPaneVerticalRange(rect, body)
		c.drawStyledPaneVBorder(rect.X+rect.W, startY, endY, style, owner, layer)
	}
	if rect.Y+rect.H < body.Y+body.H {
		// 中文说明：水平 split 同理，active pane 在上侧时不能让下侧 inactive pane 独占分隔线。
		c.drawStyledSplitHBorder(rect.X, splitPaneBorderEndX(rect, body), rect.Y+rect.H, body, style, owner, layer)
	}
}
