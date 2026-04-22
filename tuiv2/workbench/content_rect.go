package workbench

// FramedPaneContentRect returns the interactive content area inside a pane
// frame.
//
// Framed split panes intentionally keep distinct borders on every side. The
// left/top flags are accepted for call-site compatibility, but they must not
// collapse the content rect into a neighboring pane's frame again.
func FramedPaneContentRect(rect Rect, sharedLeft, sharedTop bool) (Rect, bool) {
	_ = sharedLeft
	_ = sharedTop
	content := Rect{X: rect.X + 1, Y: rect.Y + 1, W: rect.W - 2, H: rect.H - 2}
	if content.W <= 0 || content.H <= 0 {
		return Rect{}, false
	}
	return content, true
}
