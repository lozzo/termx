package app

func (m *Model) View() string {
	if m == nil || m.render == nil {
		return ""
	}
	m.reconcileCopyModeContext()
	frame := m.render.RenderFrame()
	cursor := m.render.CursorSequence()
	if m.cursorOut != nil {
		// 中文说明：把 host cursor 直接并入最终 View 输出，避免 pane 场景下
		// 光标单独走旁路写入，导致输入法预编辑文本和候选框锚到错误位置。
		m.cursorOut.SetCursorSequence("")
		m.lastViewFrame = frame
		m.lastViewCursor = cursor
		return frame + cursor
	}
	m.lastViewFrame = frame
	m.lastViewCursor = cursor
	return frame + cursor
}
