package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
)

func (m *Model) handleDisplayAndViewportLocalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil {
		return false, nil
	}
	switch action.Kind {
	case input.ActionPasteBuffer:
		if m.mode().Kind != input.ModeDisplay {
			return false, nil
		}
		m.leaveCopyMode()
		m.setMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.pasteBufferToActiveCmd()
	case input.ActionPasteClipboard:
		if m.mode().Kind != input.ModeDisplay {
			return false, nil
		}
		m.leaveCopyMode()
		m.setMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.pasteClipboardToActiveCmd()
	case input.ActionOpenClipboardHistory:
		if m.mode().Kind != input.ModeDisplay {
			return false, nil
		}
		return true, m.openClipboardHistory()
	case input.ActionZoomPane:
		if m.mode().Kind == input.ModeWorkspacePicker {
			return false, nil
		}
		if m.mode().Kind == input.ModeNormal {
			m.setMode(input.ModeState{Kind: input.ModeDisplay})
			m.render.Invalidate()
			return true, nil
		}
		if m.blocksSemanticActionForTerminalSizeLock(action) {
			return true, m.showNotice(terminalSizeLockedNotice)
		}
		if m.workbench != nil {
			if tab := m.workbench.CurrentTab(); tab != nil {
				paneID := action.PaneID
				if paneID == "" {
					paneID = tab.ActivePaneID
				}
				enteringZoom := tab.ZoomedPaneID != paneID
				if tab.ZoomedPaneID == paneID {
					tab.ZoomedPaneID = ""
				} else {
					tab.ZoomedPaneID = paneID
				}
				m.setMode(input.ModeState{Kind: input.ModeNormal})
				m.render.Invalidate()
				return true, m.syncZoomViewportCmd(paneID, enteringZoom)
			}
		}
		return true, nil
	case input.ActionScrollUp:
		if tab := m.workbench.CurrentTab(); tab != nil {
			if _, changed := m.workbench.AdjustTabScrollOffset(tab.ID, 1); changed {
				m.render.Invalidate()
			}
		}
		return true, m.ensureActivePaneScrollbackCmd()
	case input.ActionScrollDown:
		if tab := m.workbench.CurrentTab(); tab != nil {
			if _, changed := m.workbench.AdjustTabScrollOffset(tab.ID, -1); changed {
				m.render.Invalidate()
			}
		}
		return true, m.ensureActivePaneScrollbackCmd()
	default:
		return false, nil
	}
}
