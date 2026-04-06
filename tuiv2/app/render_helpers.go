package app

func (m *Model) revealCursorAndInvalidate() {
	if m == nil || m.render == nil {
		return
	}
	m.render.RevealCursorBlink()
}
