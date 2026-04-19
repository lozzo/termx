package app

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleLifecycleMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch typed := msg.(type) {
	case InvalidateMsg:
		m.invalidatePending.Store(false)
		m.render.Invalidate()
		if m.invalidateDeferred.Swap(false) {
			m.queueInvalidate()
		}
		return batchCmds(m.resizePendingPaneResizesCmd(), m.maybeAutoFitFloatingPanesCmd()), true
	case RenderTickMsg:
		if m.render != nil {
			m.render.AdvanceCursorBlink()
		}
		return nil, true
	case renderRefreshMsg:
		m.refreshRenderCaches()
		return nil, true
	case tea.WindowSizeMsg:
		if service := m.layoutResizeService(); service != nil {
			return service.applyWindowSizeMsg(typed), true
		}
		return nil, true
	case reattachFailedMsg:
		return m.openPickerIfUnattached(typed.paneID), true
	case clearErrorMsg:
		if typed.seq != m.errorSeq {
			return nil, true
		}
		m.err = nil
		m.render.Invalidate()
		return nil, true
	case clearOwnerConfirmMsg:
		if typed.seq != m.ownerSeq {
			return nil, true
		}
		m.ownerConfirmPaneID = ""
		m.render.Invalidate()
		return nil, true
	case clearNoticeMsg:
		if typed.seq != m.noticeSeq {
			return nil, true
		}
		m.notice = ""
		m.render.Invalidate()
		return nil, true
	case terminalTitleMsg:
		m.render.Invalidate()
		return nil, true
	case error:
		return m.showError(typed), true
	default:
		return nil, false
	}
}
