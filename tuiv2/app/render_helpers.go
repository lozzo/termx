package app

func (m *Model) revealCursorAndInvalidate() {
	if m == nil || m.render == nil {
		return
	}
	m.render.RevealCursorBlink()
}

func (m *Model) refreshRenderCaches() {
	if m == nil || m.render == nil {
		return
	}
	m.render.ResetCaches()
	m.render.Invalidate()
}

func (m *Model) beginHostThemeBootstrap(expectedPalette int) {
	if m == nil {
		return
	}
	m.hostThemeBootstrapPending = true
	m.hostThemeBootstrapPaletteN = maxInt(0, expectedPalette)
	m.hostThemeBootstrapSeenFG = false
	m.hostThemeBootstrapSeenBG = false
	if m.hostThemeBootstrapPalette == nil {
		m.hostThemeBootstrapPalette = make(map[int]struct{})
	} else {
		clear(m.hostThemeBootstrapPalette)
	}
}
