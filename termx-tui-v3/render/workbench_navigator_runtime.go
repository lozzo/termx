package render

func renderWorkbenchNavigatorSnapshotContent(c *canvas, content ContentVM, rect Rect, owner string, layer LayerKind) {
	if content.Kind != ContentWorkbenchTree || content.Meta.WorkbenchSnapshotPanel == nil {
		return
	}
	// 中文说明：Workbench 投影只携带 snapshot panel VM，真实嵌套绘制留在 renderer runtime 边界。
	snapshotRect := content.Meta.WorkbenchSnapshotRect
	if snapshotRect.W <= 0 || snapshotRect.H <= 0 {
		return
	}
	snapshotRect.X += rect.X
	snapshotRect.Y += rect.Y
	if snapshotRect.X >= rect.X+rect.W || snapshotRect.Y >= rect.Y+rect.H {
		return
	}
	snapshotRect.W = minInt(snapshotRect.W, rect.X+rect.W-snapshotRect.X)
	snapshotRect.H = minInt(snapshotRect.H, rect.Y+rect.H-snapshotRect.Y)
	if snapshotRect.W <= 0 || snapshotRect.H <= 0 {
		return
	}
	contentRect := content.Meta.WorkbenchSnapshotContent
	contentRect.X += rect.X
	contentRect.Y += rect.Y
	contentRect.W = minInt(contentRect.W, snapshotRect.X+snapshotRect.W-contentRect.X)
	contentRect.H = minInt(contentRect.H, snapshotRect.Y+snapshotRect.H-contentRect.Y)
	if contentRect.W < 0 {
		contentRect.W = 0
	}
	if contentRect.H < 0 {
		contentRect.H = 0
	}
	panel := *content.Meta.WorkbenchSnapshotPanel
	style := paneChromeStyle(panel)
	ownerID := owner + ":workbench-snapshot:" + panel.ID
	c.drawStyledPaneFrame(snapshotRect, style, ownerID+":chrome", layer)
	renderWorkbenchNavigatorSnapshotTitle(c, snapshotRect, panel, style, ownerID+":title", layer)
	if contentRect.W > 0 && contentRect.H > 0 {
		renderContent(c, panel.Content, contentRect, ownerID, layer)
	}
}

func renderWorkbenchNavigatorSnapshotTitle(c *canvas, rect Rect, panel PanelVM, style StyleToken, owner string, layer LayerKind) {
	if rect.W < 8 || rect.H <= 0 {
		return
	}
	title := " " + panelTitle(panel) + " "
	width := minInt(DisplayWidth(title), maxInt(0, rect.W-4))
	if width <= 0 {
		return
	}
	c.overlayTextStyled(rect.X+2, rect.Y, width, title, style, owner, layer)
}
