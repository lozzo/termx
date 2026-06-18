package render

func measureClipboardHistoryOverlay(content ContentVM, viewport Rect) Rect {
	// 中文说明：clipboard history 是全局弹窗，宽高跟随外部 terminal viewport 留边展开。
	width := viewport.W - clipboardHistoryHorizontalMargin(viewport.W)
	maxHeight := viewport.H - clipboardHistoryVerticalMargin(viewport.H)
	height := minInt(maxInt(len(content.Lines)+2, 8), 16)
	width = maxInt(width, clipboardHistoryMinimumWidth(content))
	height = minInt(height, maxHeight)
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
		return maxInt(24, width/5)
	case width >= 100:
		return 16
	case width >= 48:
		return 8
	case width >= 16:
		return 2
	default:
		return 0
	}
}

func clipboardHistoryVerticalMargin(height int) int {
	switch {
	case height >= 32:
		return maxInt(8, height/4)
	case height >= 18:
		return 6
	case height >= 8:
		return 2
	default:
		return 0
	}
}

func clipboardHistoryMinimumWidth(content ContentVM) int {
	return clipboardHistoryContentNameWidth(content) + 1 + 32 + 2
}
