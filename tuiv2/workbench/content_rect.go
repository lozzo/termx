package workbench

const framedPaneRightGutterCols = 1

// FramedPaneContentRect returns the interactive content area inside a pane
// frame.
//
// A one-column gutter is reserved at the right edge even though the terminal
// snapshot itself does not draw there. This extra column gives render a safe
// place to absorb host-side width mismatches from wide / ambiguous graphemes so
// the visible pane border stays in its own dedicated column.
func FramedPaneContentRect(rect Rect, sharedLeft, sharedTop bool) (Rect, bool) {
	content := Rect{X: rect.X + 1, Y: rect.Y + 1, W: rect.W - 2, H: rect.H - 2}
	if sharedLeft {
		content.X--
		content.W++
	}
	if sharedTop {
		content.Y--
		content.H++
	}
	if content.W > framedPaneRightGutterCols {
		content.W -= framedPaneRightGutterCols
	}
	if content.W <= 0 || content.H <= 0 {
		return Rect{}, false
	}
	return content, true
}
