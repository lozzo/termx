package render

func measureClipboardHistoryOverlay(content ContentVM, viewport Rect) Rect {
	// 中文说明：clipboard history 是全局弹窗，宽高跟随外部 terminal viewport 留边展开。
	width := viewport.W - clipboardHistoryHorizontalMargin(viewport.W)
	height := viewport.H - clipboardHistoryVerticalMargin(viewport.H)
	width = maxInt(width, clipboardHistoryMinimumWidth())
	height = maxInt(height, minInt(maxInt(len(content.Lines)+3, 8), 18))
	width = minInt(width, viewport.W)
	height = minInt(height, viewport.H)
	return Rect{
		X: maxInt(0, (viewport.W-width)/2),
		Y: maxInt(0, (viewport.H-height)/2),
		W: width,
		H: height,
	}
}

func measureClipboardHistoryContentRect(rect Rect) Rect {
	return Rect{X: rect.X + 1, Y: rect.Y + 1, W: maxInt(0, rect.W-2), H: maxInt(0, rect.H-2)}
}

func clipboardHistoryHorizontalMargin(width int) int {
	switch {
	case width >= 160:
		return 16
	case width >= 100:
		return 8
	case width >= 48:
		return 4
	case width >= 16:
		return 2
	default:
		return 0
	}
}

func clipboardHistoryVerticalMargin(height int) int {
	switch {
	case height >= 32:
		return 8
	case height >= 18:
		return 4
	case height >= 8:
		return 2
	default:
		return 0
	}
}

func clipboardHistoryMinimumWidth() int {
	return clipboardHistoryNameWidth + 1 + 24 + 2
}
