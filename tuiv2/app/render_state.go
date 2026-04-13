package app

import "github.com/lozzow/termx/tuiv2/render"

func (m *Model) renderVM() render.RenderVM {
	bodyHeight := m.bodyHeight()
	vm := render.AdaptRenderVMWithSize(m.workbench, m.runtime, m.width, bodyHeight)
	vm = render.WithRenderTermSize(vm, m.width, m.height)
	vm = render.WithRenderStatus(vm, m.notice, renderErrorText(m.err), string(m.visibleInputMode()))
	vm.Body.OwnerConfirmPaneID = m.ownerConfirmPaneID
	if paneID, selected, ok := m.currentEmptyPaneSelection(); ok {
		vm = render.WithRenderEmptyPaneSelection(vm, paneID, selected)
	}
	if paneID, selected, ok := m.currentExitedPaneSelection(); ok {
		vm = render.WithRenderExitedPaneSelection(vm, paneID, selected)
	}
	if paneID, snapshot, ok := m.activeCopyModeResumeSnapshot(); ok {
		vm = render.WithRenderPaneSnapshotOverride(vm, paneID, snapshot)
	}
	if m.copyMode.PaneID != "" {
		copyMode := render.RenderCopyModeVM{
			PaneID:     m.copyMode.PaneID,
			CursorRow:  m.copyMode.Cursor.Row,
			CursorCol:  m.copyMode.Cursor.Col,
			ViewTopRow: m.copyMode.ViewTopRow,
			Snapshot:   m.copyMode.Snapshot,
		}
		if m.copyMode.Mark != nil {
			copyMode.MarkSet = true
			copyMode.MarkRow = m.copyMode.Mark.Row
			copyMode.MarkCol = m.copyMode.Mark.Col
		}
		vm = render.WithRenderCopyMode(vm, copyMode)
	}
	vm = render.AttachRenderTerminalPool(vm, m.terminalPage)
	vm = render.AttachRenderModalHost(vm, m.modalHost)
	vm = render.WithRenderStatusHints(vm, m.buildStatusHints(vm))
	return render.WithRenderStatusRightTokens(vm, m.buildStatusBarRightTokens(vm))
}

func (m *Model) visibleInputMode() string {
	if m == nil || m.ui == nil {
		return ""
	}
	return string(m.ui.VisibleInputMode())
}
