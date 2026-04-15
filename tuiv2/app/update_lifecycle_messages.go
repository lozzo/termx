package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/workbench"
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
		m.forceFullRedraw()
		return nil, true
	case tea.WindowSizeMsg:
		oldBodyRect := m.bodyRect()
		newBodyRect := workbench.Rect{W: maxInt(1, typed.Width), H: render.FrameBodyHeight(typed.Height)}
		if m.workbench != nil {
			if m.width > 0 && m.height > 0 {
				m.workbench.ReflowFloatingPanes(oldBodyRect, newBodyRect)
			} else {
				m.workbench.ClampFloatingPanesToBounds(newBodyRect)
			}
		}
		m.width = typed.Width
		m.height = typed.Height
		m.render.Invalidate()
		return batchCmds(m.resizeVisiblePanesCmd(), m.resizePendingPaneResizesCmd(), m.maybeAutoFitFloatingPanesCmd(), m.updateSessionViewCmd()), true
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
