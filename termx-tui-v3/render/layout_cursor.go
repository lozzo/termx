package render

func measureCursor(shell ShellVM, plan LayoutPlan) (Cursor, Rect) {
	if overlayOwnsCursor(shell.Overlay) {
		return cursorWithRectOrAnchor(shell.Overlay.Content.Cursor, plan.OverlayContentRect)
	}
	for i := len(plan.Floatings) - 1; i >= 0; i-- {
		floating := plan.Floatings[i]
		if !floating.Floating.Active {
			continue
		}
		cursor := floating.Floating.Content.Cursor
		return cursorWithRectOrAnchor(cursor, floating.ContentRect)
	}
	for _, panel := range plan.Panels {
		if !panel.Panel.Active {
			continue
		}
		cursor := panel.Panel.Content.Cursor
		if !cursor.Visible {
			cursor = shell.Cursor
		}
		return cursorWithRectOrAnchor(cursor, panel.ContentRect)
	}
	return cursorWithRectOrAnchor(shell.Cursor, plan.Body)
}

func overlayOwnsCursor(overlay OverlayVM) bool {
	return overlay.Kind != OverlayNone && overlay.Content.Kind != "" && (overlay.Opaque || overlay.Content.Kind == ContentPrompt || overlay.Content.Kind == ContentTerminalPicker || overlay.Content.Kind == ContentTerminalPool || overlay.Content.Kind == ContentWorkbenchTree || overlay.Content.Kind == ContentHelp)
}

func cursorWithRect(cursor Cursor, origin Rect) (Cursor, Rect) {
	if !cursor.Visible || origin.W <= 0 || origin.H <= 0 {
		return Cursor{}, Rect{}
	}
	rect := Rect{X: origin.X + cursor.Col, Y: origin.Y + cursor.Row, W: 1, H: 1}
	if intersectRect(rect, origin) != rect {
		return Cursor{}, Rect{}
	}
	return cursor, rect
}

func cursorWithRectOrAnchor(cursor Cursor, origin Rect) (Cursor, Rect) {
	if measured, rect := cursorWithRect(cursor, origin); measured.Visible {
		return measured, rect
	}
	if origin.W <= 0 || origin.H <= 0 {
		return Cursor{}, Rect{}
	}
	// 中文输入法候选区跟随宿主真实光标；内容暂无 cursor 时也要把隐藏光标锚到输入目标内。
	anchor := Cursor{Visible: true, Row: 0, Col: 0, Shape: CursorShapeBar}
	return anchor, Rect{X: origin.X, Y: origin.Y, W: 1, H: 1}
}
