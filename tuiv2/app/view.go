package app

func (m *Model) View() string {
	if m == nil || m.render == nil {
		return ""
	}
	m.reconcileCopyModeContext()
	frame := m.render.RenderFrame()
	cursor := m.render.CursorSequence()
	if m.frameOut != nil {
		_ = m.frameOut.WriteFrame(frame, cursor)
		m.lastViewFrame = frame
		m.lastViewCursor = cursor
		return ""
	}
	if m.cursorOut != nil {
		// 中文说明：Bubble Tea 的 standardRenderer.flush() 在渲染完所有行后，
		// 总会追加 MoveCursor(linesRendered, 0) 把终端光标移到屏幕左下角。
		// 仅靠 View() 内嵌 cursor CUP 无法最终生效，因为 BT 的尾部 MoveCursor
		// 会覆盖它。
		//
		// 双路策略：
		// 1. View() 仍然返回 frame+cursor，确保 cursor 变化时 BT 能检测到
		//    字符串差异并触发 flush（否则仅 cursor 移动但 frame 不变时 BT 会
		//    跳过输出）。
		// 2. 同时把 cursor 设到 outputCursorWriter 上，让它在 BT 整段输出
		//   （含尾部 MoveCursor）之后再追加一次 cursor CUP。
		//    这样终端光标最终停留在 pane 内的正确输入位置，输入法锚点就不会跑偏。
		m.cursorOut.SetCursorSequence(cursor)
		m.lastViewFrame = frame
		m.lastViewCursor = cursor
		return frame + cursor
	}
	m.lastViewFrame = frame
	m.lastViewCursor = cursor
	return frame + cursor
}
