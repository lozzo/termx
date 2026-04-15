package app

func (m *Model) revealCursorAndInvalidate() {
	if m == nil || m.render == nil {
		return
	}
	m.render.RevealCursorBlink()
}

func (m *Model) forceFullRedraw() {
	if m == nil {
		return
	}
	if m.render != nil {
		m.render.ResetCaches()
		m.render.Invalidate()
	}
	if resetter, ok := m.frameOut.(frameResetWriter); ok {
		resetter.ResetFrameState()
	}
}
