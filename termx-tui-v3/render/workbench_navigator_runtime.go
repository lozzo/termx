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
	layout := PanelLayoutPlan{Panel: panel, Rect: snapshotRect, Body: snapshotRect, ContentRect: contentRect}
	renderCardPanel(c, layout)
	if contentRect.W > 0 && contentRect.H > 0 {
		renderContent(c, panel.Content, contentRect, owner+":workbench-snapshot:"+panel.ID, layer)
	}
}
