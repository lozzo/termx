package app

import "github.com/lozzow/termx/tuiv2/render"

func (m *Model) visibleRenderState() render.VisibleRenderState {
	bodyHeight := m.bodyHeight()
	state := render.AdaptVisibleStateWithSize(m.workbench, m.runtime, m.width, bodyHeight)
	state = render.WithTermSize(state, m.width, m.height)
	state = render.WithStatus(state, m.notice, renderErrorText(m.err), string(m.visibleInputMode()))
	state.OwnerConfirmPaneID = m.ownerConfirmPaneID
	if paneID, selected, ok := m.currentEmptyPaneSelection(); ok {
		state = render.WithEmptyPaneSelection(state, paneID, selected)
	}
	if paneID, selected, ok := m.currentExitedPaneSelection(); ok {
		state = render.WithExitedPaneSelection(state, paneID, selected)
	}
	if m.copyMode.PaneID != "" {
		markSet := m.copyMode.Mark != nil
		markRow := 0
		markCol := 0
		if markSet {
			markRow = m.copyMode.Mark.Row
			markCol = m.copyMode.Mark.Col
		}
		state = render.WithCopyMode(state, m.copyMode.PaneID, m.copyMode.Cursor.Row, m.copyMode.Cursor.Col, m.copyMode.ViewTopRow, markSet, markRow, markCol)
	}
	state = render.AttachTerminalPool(state, m.terminalPage)
	return render.AttachModalHost(state, m.modalHost)
}

func (m *Model) visibleInputMode() string {
	if m == nil || m.ui == nil {
		return ""
	}
	return string(m.ui.VisibleInputMode())
}
