package app

func (m *Model) View() string {
	if m == nil || m.render == nil {
		return ""
	}
	frame := m.render.RenderFrame()
	cursor := m.render.CursorSequence()
	if m.cursorOut != nil {
		m.cursorOut.SetCursorSequence(cursor)
		return frame
	}
	return frame + cursor
}
