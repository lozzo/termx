package app

import (
	"strings"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/historyview"
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
		projection := (*render.RenderCopyModeProjectionVM)(nil)
		if buffer, ok := m.authoritativeCopyModeBufferForPane(state.PaneID, 1); ok && buffer.window != nil {
			projection = renderCopyModeProjectionFromHistoryWindow(*buffer.window)
		}
		copyMode := render.RenderCopyModeVM{
			PaneID:            state.PaneID,
			CursorRow:         state.Cursor.Row,
			CursorCol:         state.Cursor.Col,
			CursorLogicalLine: state.CursorLogical.Line,
			CursorLogicalCol:  state.CursorLogical.Offset,
			ViewTopRow:        state.ViewTopRow,
			Projection:        projection,
		}
		if state.Mark != nil {
			copyMode.MarkSet = true
			copyMode.MarkRow = state.Mark.Row
			copyMode.MarkCol = state.Mark.Col
			if state.MarkLogical != nil {
				copyMode.MarkLogicalLine = state.MarkLogical.Line
				copyMode.MarkLogicalCol = state.MarkLogical.Offset
			}
		}
		out = append(out, copyMode)
	}
	return out
}

func renderCopyModeProjectionFromHistoryWindow(window historyview.HistoryWindow) *render.RenderCopyModeProjectionVM {
	if strings.TrimSpace(window.TerminalID) == "" || len(window.Rows) == 0 {
		return nil
	}
	rows := make([]render.RenderCopyModeProjectionRowVM, 0, len(window.Rows))
	for _, row := range window.Rows {
		cells := append([]protocol.Cell(nil), row.Cells.DecodeCells()...)
		rows = append(rows, render.RenderCopyModeProjectionRowVM{
			Cells:     cells,
			Timestamp: row.Timestamp,
			Kind:      string(row.Kind),
			Wrapped:   row.Wrapped,
		})
	}
	lines := make([]render.RenderCopyModeProjectionLineVM, 0, len(window.Lines))
	for _, line := range window.Lines {
		lines = append(lines, render.RenderCopyModeProjectionLineVM{
			StartRow:      line.StartRow,
			EndRow:        line.EndRow,
			LogicalLineID: line.LogicalLineID,
			ClippedBefore: line.ClippedBefore,
			ClippedAfter:  line.ClippedAfter,
		})
	}
	return &render.RenderCopyModeProjectionVM{
		TerminalID:      window.TerminalID,
		Token:           string(window.Token),
		Generation:      window.Generation,
		Size:            window.Size,
		Rows:            rows,
		Lines:           lines,
		TotalRows:       window.TotalRows,
		TotalLines:      window.TotalLines,
		HasMore:         window.HasMore,
		FirstBoundaryID: window.FirstBoundaryID,
		LastBoundaryID:  window.LastBoundaryID,
	}
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
