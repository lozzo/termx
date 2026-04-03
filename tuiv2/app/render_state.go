package app

import "github.com/lozzow/termx/tuiv2/render"

func (m *Model) visibleRenderState() render.VisibleRenderState {
	bodyHeight := maxInt(1, m.height-2) // tab bar + status bar = 2 rows
	state := render.AdaptVisibleStateWithSize(m.workbench, m.runtime, m.width, bodyHeight)
	state = render.WithTermSize(state, m.width, m.height)
	state = render.WithStatus(state, "", renderErrorText(m.err), string(m.input.Mode().Kind))
	state = render.AttachTerminalPool(state, m.terminalPage)
	return render.AttachModalHost(state, m.modalHost)
}
