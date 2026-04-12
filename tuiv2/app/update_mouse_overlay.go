package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/render"
)

func (m *Model) handleOverlayMouseClick(state render.VisibleRenderState, x, y int) (bool, tea.Cmd) {
	if state.Overlay.Kind == render.VisibleOverlayNone {
		return false, nil
	}
	bodyY := y - m.contentOriginY()
	if bodyY < 0 {
		return true, nil
	}
	region, ok := render.HitRegionAt(render.OverlayHitRegions(state), x, bodyY)
	if !ok {
		return true, nil
	}
	switch state.Overlay.Kind {
	case render.VisibleOverlayPicker:
		if m.modalHost == nil || m.modalHost.Picker == nil {
			return true, nil
		}
		switch region.Kind {
		case render.HitRegionOverlayDismiss:
			return true, m.cancelActiveModal()
		case render.HitRegionOverlayQueryInput:
			return true, m.handleOverlayQueryInputMouseClick(region, overlayQueryPicker, x)
		case render.HitRegionPickerItem:
			m.modalHost.Picker.Selected = region.ItemIndex
			normalizeModalSelection(&m.modalHost.Picker.Selected, len(m.modalHost.Picker.VisibleItems()))
			m.render.Invalidate()
			if m.modalHost.Picker.SelectedItem() == nil {
				return true, nil
			}
			return true, m.dispatchOverlayRegionAction(input.SemanticAction{Kind: input.ActionSubmitPrompt})
		default:
			return true, m.dispatchOverlayRegionAction(region.Action)
		}
	case render.VisibleOverlayWorkspacePicker:
		if m.modalHost == nil || m.modalHost.WorkspacePicker == nil {
			return true, nil
		}
		switch region.Kind {
		case render.HitRegionOverlayDismiss:
			return true, m.cancelActiveModal()
		case render.HitRegionOverlayQueryInput:
			return true, m.handleOverlayQueryInputMouseClick(region, overlayQueryWorkspace, x)
		case render.HitRegionWorkspaceItem:
			m.modalHost.WorkspacePicker.Selected = region.ItemIndex
			normalizeModalSelection(&m.modalHost.WorkspacePicker.Selected, len(m.modalHost.WorkspacePicker.VisibleItems()))
			m.render.Invalidate()
			return true, nil
		default:
			return true, m.dispatchOverlayRegionAction(region.Action)
		}
	case render.VisibleOverlayFloatingOverview:
		if m.modalHost == nil || m.modalHost.FloatingOverview == nil {
			return true, nil
		}
		switch region.Kind {
		case render.HitRegionOverlayDismiss:
			return true, m.cancelActiveModal()
		case render.HitRegionFloatingOverviewItem:
			m.modalHost.FloatingOverview.Selected = region.ItemIndex
			m.render.Invalidate()
			return true, m.dispatchOverlayRegionAction(input.SemanticAction{Kind: input.ActionSubmitPrompt})
		default:
			return true, m.dispatchOverlayRegionAction(region.Action)
		}
	case render.VisibleOverlayPrompt, render.VisibleOverlayHelp, render.VisibleOverlayTerminalManager:
		if region.Kind == render.HitRegionOverlayDismiss {
			return true, m.cancelActiveModal()
		}
		if state.Overlay.Kind == render.VisibleOverlayPrompt && region.Kind == render.HitRegionPromptInput {
			return true, m.handlePromptInputMouseClick(region, x)
		}
		if state.Overlay.Kind == render.VisibleOverlayTerminalManager && region.Kind == render.HitRegionOverlayQueryInput {
			return true, m.handleOverlayQueryInputMouseClick(region, overlayQueryTerminalManager, x)
		}
		return true, m.dispatchOverlayRegionAction(region.Action)
	default:
		return true, nil
	}
}

func (m *Model) handleOverlayMouseWheel(state render.VisibleRenderState, delta int) (bool, tea.Cmd) {
	switch state.Overlay.Kind {
	case render.VisibleOverlayPicker:
		if m.modalHost != nil && m.modalHost.Picker != nil {
			m.modalHost.Picker.Move(-delta)
			m.render.Invalidate()
		}
		return true, nil
	case render.VisibleOverlayWorkspacePicker:
		if m.modalHost != nil && m.modalHost.WorkspacePicker != nil {
			m.modalHost.WorkspacePicker.Move(-delta)
			m.render.Invalidate()
		}
		return true, nil
	case render.VisibleOverlayFloatingOverview:
		if m.modalHost != nil && m.modalHost.FloatingOverview != nil {
			m.modalHost.FloatingOverview.Move(-delta)
			m.render.Invalidate()
		}
		return true, nil
	case render.VisibleOverlayTerminalManager:
		if m.terminalPage != nil {
			m.terminalPage.Move(-delta)
			m.render.Invalidate()
		}
		return true, nil
	case render.VisibleOverlayPrompt, render.VisibleOverlayHelp:
		return true, nil
	default:
		return false, nil
	}
}

type overlayQueryTarget int

const (
	overlayQueryPicker overlayQueryTarget = iota
	overlayQueryWorkspace
	overlayQueryTerminalManager
)

func (m *Model) handleOverlayQueryInputMouseClick(region render.HitRegion, target overlayQueryTarget, screenX int) tea.Cmd {
	if m == nil {
		return nil
	}
	cursor := screenX - region.Rect.X
	if cursor < 0 {
		cursor = 0
	}
	switch target {
	case overlayQueryPicker:
		if m.modalHost == nil || m.modalHost.Picker == nil {
			return nil
		}
		if setQueryCursor(&m.modalHost.Picker.Query, &m.modalHost.Picker.Cursor, &m.modalHost.Picker.CursorSet, cursor) {
			m.revealCursorAndInvalidate()
		}
	case overlayQueryWorkspace:
		if m.modalHost == nil || m.modalHost.WorkspacePicker == nil {
			return nil
		}
		if setQueryCursor(&m.modalHost.WorkspacePicker.Query, &m.modalHost.WorkspacePicker.Cursor, &m.modalHost.WorkspacePicker.CursorSet, cursor) {
			m.revealCursorAndInvalidate()
		}
	case overlayQueryTerminalManager:
		if m.terminalPage == nil {
			return nil
		}
		if setQueryCursor(&m.terminalPage.Query, &m.terminalPage.Cursor, &m.terminalPage.CursorSet, cursor) {
			m.revealCursorAndInvalidate()
		}
	}
	return nil
}

func (m *Model) handleTerminalPoolMouseClick(state render.VisibleRenderState, x, y int) (bool, tea.Cmd) {
	if m.terminalPage == nil {
		return false, nil
	}
	bodyY := y - m.contentOriginY()
	if bodyY < 0 {
		return true, nil
	}
	region, ok := render.HitRegionAt(render.TerminalPoolHitRegions(state), x, bodyY)
	if !ok {
		return true, nil
	}
	switch region.Kind {
	case render.HitRegionOverlayQueryInput:
		return true, m.handleOverlayQueryInputMouseClick(region, overlayQueryTerminalManager, x)
	case render.HitRegionTerminalPoolItem:
		m.terminalPage.Selected = region.ItemIndex
		normalizeModalSelection(&m.terminalPage.Selected, len(m.terminalPage.VisibleItems()))
		m.render.Invalidate()
		return true, nil
	case render.HitRegionTerminalPoolAction:
		handled, cmd := m.handleModalAction(region.Action)
		if handled {
			return true, cmd
		}
		return true, nil
	default:
		return true, nil
	}
}

func (m *Model) handleTerminalPoolMouseWheel(state render.VisibleRenderState, delta int) (bool, tea.Cmd) {
	if m.terminalPage == nil {
		return false, nil
	}
	m.terminalPage.Move(-delta)
	m.render.Invalidate()
	return true, nil
}

func (m *Model) handleTopChromeMouseClick(state render.VisibleRenderState, x, y int) (bool, tea.Cmd) {
	if m != nil && m.immersiveZoomActive() {
		return false, nil
	}
	if y != 0 {
		return false, nil
	}
	region, ok := render.HitRegionAt(render.TabBarHitRegions(state), x, y)
	if !ok {
		return false, nil
	}
	switch region.Kind {
	case render.HitRegionTabSwitch:
		return true, m.switchTabByIndexMouse(region.TabIndex)
	default:
		if region.Action.Kind != "" {
			return true, m.applyMouseSemanticAction(region.Action)
		}
		return true, nil
	}
}

func (m *Model) handleBottomChromeMouseClick(state render.VisibleRenderState, x, y int) (bool, tea.Cmd) {
	if m != nil && m.immersiveZoomActive() {
		return false, nil
	}
	if m == nil || y != m.height-1 {
		return false, nil
	}
	region, ok := render.HitRegionAt(render.StatusBarHitRegions(state), x, y)
	if !ok {
		return false, nil
	}
	if region.Action.Kind == "" {
		return true, nil
	}
	return true, m.applyMouseSemanticAction(region.Action)
}

func (m *Model) switchTabByIndexMouse(index int) tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	ws := m.workbench.CurrentWorkspace()
	if ws == nil {
		return nil
	}
	if err := m.workbench.SwitchTab(ws.Name, index); err != nil {
		return m.showError(err)
	}
	m.render.Invalidate()
	return batchCmds(m.resizeVisiblePanesCmd(), m.resizePendingPaneResizesCmd(), m.syncActivePaneInteractiveOwnershipCmd(), m.saveStateCmd())
}

func (m *Model) switchCurrentTabByOffsetMouse(offset int) tea.Cmd {
	if err := m.switchCurrentTabByOffset(offset); err != nil {
		return m.showError(err)
	}
	m.render.Invalidate()
	return batchCmds(m.resizeVisiblePanesCmd(), m.resizePendingPaneResizesCmd(), m.syncActivePaneInteractiveOwnershipCmd(), m.saveStateCmd())
}
