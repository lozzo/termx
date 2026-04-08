package app

import "github.com/lozzow/termx/tuiv2/input"

func (m *Model) shouldInlineCursorProjection() bool {
	if m == nil {
		return false
	}
	switch m.mode().Kind {
	case input.ModePrompt, input.ModePicker, input.ModeWorkspacePicker, input.ModeTerminalManager:
		return true
	default:
		return false
	}
}

func (m *Model) View() string {
	if m == nil || m.render == nil {
		return ""
	}
	m.reconcileCopyModeContext()
	frame := m.render.RenderFrame()
	cursor := m.render.CursorSequence()
	inlineCursor := m.shouldInlineCursorProjection()
	if m.cursorOut != nil {
		if inlineCursor {
			m.cursorOut.SetCursorSequence("")
			m.lastViewFrame = frame
			m.lastViewCursor = cursor
			return frame + cursor
		}
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
