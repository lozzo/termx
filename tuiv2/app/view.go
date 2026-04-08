package app

func (m *Model) View() string {
	if m == nil || m.render == nil {
		return ""
	}
	frame := m.render.RenderFrame()
	cursor := m.render.CursorSequence()
	if m.cursorOut != nil {
		m.cursorOut.SetCursorSequence(cursor)
		if m.lastViewFrame == frame && m.lastViewCursor != "" && m.lastViewCursor != cursor {
			_ = m.cursorOut.WriteControlSequence(cursor)
		}
		m.lastViewFrame = frame
		m.lastViewCursor = cursor
		return frame
	}
	m.lastViewFrame = frame
	m.lastViewCursor = cursor
	return frame + cursor
}
