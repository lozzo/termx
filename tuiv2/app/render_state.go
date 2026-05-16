package app

import (
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/runtime"
)

func (m *Model) renderVM() render.RenderVM {
	bodyHeight := m.bodyHeight()
	vm := render.AdaptRenderVMWithSize(m.workbench, m.runtime, m.width, bodyHeight)
	vm = m.withLiveSurfaceForSplitDragPreview(vm)
	vm = m.withFloatingDragPreview(vm)
	vm = render.WithRenderTermSize(vm, m.width, m.height)
	vm = render.WithRenderChromeConfig(vm, m.chromeConfig())
	vm = render.WithRenderThemeConfig(vm, m.theme)
	vm = render.WithRenderStatus(vm, m.notice, renderErrorText(m.err), string(m.effectiveInputMode()))
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
	if copyModes := m.renderCopyModeVMs(); len(copyModes) > 0 {
		vm = render.WithRenderCopyModes(vm, copyModes)
	}
	vm = render.AttachRenderTerminalPool(vm, m.terminalPage)
	vm = render.AttachRenderModalHost(vm, m.modalHost)
	vm = render.WithRenderStatusHints(vm, m.buildStatusHints(vm))
	return render.WithRenderStatusRightTokens(vm, m.buildStatusBarRightTokens(vm))
}

func (m *Model) renderCopyModeVMs() []render.RenderCopyModeVM {
	if m == nil {
		return nil
	}
	states := m.allCopyModeStates()
	if len(states) == 0 {
		return nil
	}
	out := make([]render.RenderCopyModeVM, 0, len(states))
	for _, state := range states {
		copyMode := render.RenderCopyModeVM{
			PaneID:     state.PaneID,
			CursorRow:  state.Cursor.Row,
			CursorCol:  state.Cursor.Col,
			ViewTopRow: state.ViewTopRow,
			Snapshot:   state.Snapshot,
		}
		if state.Mark != nil {
			copyMode.MarkSet = true
			copyMode.MarkRow = state.Mark.Row
			copyMode.MarkCol = state.Mark.Col
		}
		out = append(out, copyMode)
	}
	return out
}

func (m *Model) visibleInputMode() string {
	if m == nil || m.ui == nil {
		return ""
	}
	return string(m.effectiveInputMode())
}

func (m *Model) withLiveSurfaceForSplitDragPreview(vm render.RenderVM) render.RenderVM {
	if m == nil || m.runtime == nil || vm.Runtime == nil || vm.Workbench == nil {
		return vm
	}
	if m.mouseDragMode != mouseDragResizeSplit || !m.mouseDragDirty {
		return vm
	}
	terminalIDs := make(map[string]struct{})
	if vm.Workbench.ActiveTab >= 0 && vm.Workbench.ActiveTab < len(vm.Workbench.Tabs) {
		for _, pane := range vm.Workbench.Tabs[vm.Workbench.ActiveTab].Panes {
			if pane.TerminalID != "" {
				terminalIDs[pane.TerminalID] = struct{}{}
			}
		}
	}
	for _, pane := range vm.Workbench.FloatingPanes {
		if pane.TerminalID != "" {
			terminalIDs[pane.TerminalID] = struct{}{}
		}
	}
	if len(terminalIDs) == 0 {
		return vm
	}
	runtimeCopy := *vm.Runtime
	runtimeCopy.Terminals = append([]runtime.VisibleTerminal(nil), vm.Runtime.Terminals...)
	patched := false
	for i := range runtimeCopy.Terminals {
		terminal := runtimeCopy.Terminals[i]
		if _, ok := terminalIDs[terminal.TerminalID]; !ok {
			continue
		}
		if terminal.Surface != nil {
			continue
		}
		liveSurface := m.runtime.LiveSurface(terminal.TerminalID)
		if liveSurface == nil {
			continue
		}
		terminal.Surface = liveSurface
		runtimeCopy.Terminals[i] = terminal
		patched = true
	}
	if patched {
		vm.Runtime = &runtimeCopy
	}
	return vm
}

func (m *Model) withFloatingDragPreview(vm render.RenderVM) render.RenderVM {
	if m == nil || !m.floatingDragPreview.Active || m.floatingDragPreview.PaneID == "" || m.floatingDragPreview.Snapshot == nil {
		return vm
	}
	return render.WithRenderFloatingDragPreview(vm, m.floatingDragPreview.PaneID, m.floatingDragPreview.Rect, m.floatingDragPreview.Snapshot)
}
