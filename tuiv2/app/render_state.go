package app

import (
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/render"
)

func (m *Model) visibleRenderState() render.VisibleRenderState {
	bodyHeight := maxInt(1, m.height-2) // tab bar + status bar = 2 rows
	state := render.AdaptVisibleStateWithSize(m.workbench, m.runtime, m.width, bodyHeight)
	state = render.WithTermSize(state, m.width, m.height)
	state = render.WithStatus(state, "", renderErrorText(m.err), string(m.visibleInputMode()))
	state.OwnerConfirmPaneID = m.ownerConfirmPaneID
	state = render.AttachTerminalPool(state, m.terminalPage)
	return render.AttachModalHost(state, m.modalHost)
}

func (m *Model) visibleInputMode() string {
	if m == nil {
		return ""
	}
	if m.terminalPage != nil {
		return string(input.ModeTerminalManager)
	}
	if m.modalHost != nil && m.modalHost.Session != nil {
		return string(m.modalHost.Session.Kind)
	}
	if m.input == nil {
		return ""
	}
	return string(m.input.Mode().Kind)
}
