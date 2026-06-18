package render

func measureClipboardHistoryOverlay(content ContentVM, viewport Rect) Rect {
	lineWidth := clipboardHistoryRowWidth()
	for _, line := range content.Lines {
		lineWidth = maxInt(lineWidth, line.Width())
	}
	width := minInt(maxInt(lineWidth+2, 58), 86)
	height := minInt(maxInt(len(content.Lines)+3, 8), 18)
	if width > viewport.W-4 {
		width = maxInt(8, viewport.W-2)
	}
	if height > viewport.H-4 {
		height = maxInt(3, viewport.H-2)
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

func measureClipboardHistoryContentRect(rect Rect) Rect {
	return Rect{X: rect.X + 1, Y: rect.Y + 1, W: maxInt(0, rect.W-2), H: maxInt(0, rect.H-2)}
}
