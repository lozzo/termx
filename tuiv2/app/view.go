package app

func (m *Model) View() string {
	return m.render.RenderFrame()
}
