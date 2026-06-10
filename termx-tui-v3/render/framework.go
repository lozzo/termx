package render

const (
	minFrameWidth  = 24
	minFrameHeight = 8
	defaultWidth   = 80
	defaultHeight  = 24
)

func (renderer Renderer) renderFramework(vm RenderVM) RenderResult {
	shell := vm.Shell
	// 暂时屏蔽右上角 toast 卡片，只保留 reducer 内的反馈状态供快捷键清理和后续恢复。
	shell.Toasts = nil
	plan := MeasureLayout(shell, shell.Layout.Viewport)
	c := newCanvas(plan.Viewport.W, plan.Viewport.H)

	renderShellFrame(c, plan)
	if plan.Header.W > 0 && plan.Header.H > 0 {
		renderHeader(c, shell.Header, plan.Header, plan.HeaderTopFrame, plan.HeaderDividerFrame)
	}
	if plan.Footer.W > 0 && plan.Footer.H > 0 {
		renderFooter(c, shell.Footer, plan.Footer, plan.FooterFrame)
	}

	layers := make([]Layer, 0)
	if len(plan.Panels) == 0 && plan.Body.W > 0 && plan.Body.H > 0 {
		contentResult := renderContent(c, shell.Layout.BodyContent, plan.Body, "shell:body:content", LayerPanel)
		layers = append(layers, Layer{Kind: LayerPanel, Rect: plan.Body, Lines: contentResult.Lines, ContentOverflow: contentResult.Overflow})
	}
	for _, layout := range plan.Panels {
		switch layout.Panel.Presentation {
		case PanelPresentationSplitLine:
			renderSplitPanel(c, layout)
		default:
			renderCardPanel(c, layout)
		}
		contentResult := renderContent(c, layout.Panel.Content, layout.ContentRect, "panel:"+layout.Panel.ID+":content", LayerPanel)
		renderPanelContentOverflowMarkers(c, layout, contentResult.Overflow)
		layers = append(layers, Layer{Kind: LayerPanel, Rect: layout.Rect, Lines: contentResult.Lines, ContentOverflow: contentResult.Overflow})
	}
	for _, floating := range plan.Floatings {
		layer := renderFloating(c, floating)
		if layer.Rect.W > 0 && layer.Rect.H > 0 {
			layers = append(layers, layer)
		}
	}
	applyChromePatches(c, shell.Layout.ChromePatches, plan)

	toastLayers := renderToasts(c, shell.Toasts, plan.Toasts)
	for _, layer := range toastLayers {
		layers = append(layers, layer)
	}
	overlayLayer := renderOverlay(c, shell.Overlay, plan.Overlay, plan.OverlayContentRect)
	if overlayLayer.Rect.W > 0 && overlayLayer.Rect.H > 0 {
		layers = append(layers, overlayLayer)
	}
	popupLayer := renderOverlayPopup(c, plan.OverlayPopup)
	if popupLayer.Rect.W > 0 && popupLayer.Rect.H > 0 {
		layers = append(layers, popupLayer)
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

func applyChromePatches(c *canvas, patches []ChromePatchVM, plan LayoutPlan) {
	for _, patch := range patches {
		x, y := chromePatchOrigin(patch, plan)
		width := patch.W
		if width <= 0 {
			width = DisplayWidth(patch.Text)
		}
		layer := patch.Layer
		if layer == "" {
			layer = LayerChrome
		}
		owner := patch.Owner
		if owner == "" {
			owner = "chrome:patch"
		}
		c.writeTextStyled(x, y, width, patch.Text, patch.Style, owner, layer)
	}
}

func chromePatchOrigin(patch ChromePatchVM, plan LayoutPlan) (int, int) {
	switch patch.Anchor {
	case ChromePatchAnchorBody:
		return plan.Body.X + patch.X, plan.Body.Y + patch.Y
	default:
		return patch.X, patch.Y
	}
}
