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
	case input.ActionMovePaneContentLeft, input.ActionMovePaneContentRight, input.ActionMovePaneContentUp, input.ActionMovePaneContentDown,
		input.ActionAlignPaneContentLeft, input.ActionAlignPaneContentRight, input.ActionAlignPaneContentTop, input.ActionAlignPaneContentBottom,
		input.ActionCenterPaneContent, input.ActionCenterPaneContentHorizontal, input.ActionCenterPaneContentVertical, input.ActionResetPaneContentOffset:
		if m.mode().Kind != input.ModeResize {
			return false, nil
		}
		paneID := m.currentOrActionPaneID(action.PaneID)
		if paneID == "" {
			return true, nil
		}
		changed := false
		switch action.Kind {
		case input.ActionMovePaneContentLeft:
			changed = m.adjustPaneContentOffsetClamped(paneID, -1, 0)
		case input.ActionMovePaneContentRight:
			changed = m.adjustPaneContentOffsetClamped(paneID, 1, 0)
		case input.ActionMovePaneContentUp:
			changed = m.adjustPaneContentOffsetClamped(paneID, 0, -1)
		case input.ActionMovePaneContentDown:
			changed = m.adjustPaneContentOffsetClamped(paneID, 0, 1)
		case input.ActionAlignPaneContentLeft:
			changed = m.alignPaneContentOffset(paneID, func(bounds paneContentOffsetBounds, current int) int { return bounds.maxX }, nil)
		case input.ActionAlignPaneContentRight:
			changed = m.alignPaneContentOffset(paneID, func(bounds paneContentOffsetBounds, current int) int { return bounds.minX }, nil)
		case input.ActionAlignPaneContentTop:
			changed = m.alignPaneContentOffset(paneID, nil, func(bounds paneContentOffsetBounds, current int) int { return bounds.maxY })
		case input.ActionAlignPaneContentBottom:
			changed = m.alignPaneContentOffset(paneID, nil, func(bounds paneContentOffsetBounds, current int) int { return bounds.minY })
		case input.ActionCenterPaneContent:
			changed = m.alignPaneContentOffset(paneID,
				func(bounds paneContentOffsetBounds, current int) int {
					return centeredPaneContentOffset(bounds.minX, bounds.maxX)
				},
				func(bounds paneContentOffsetBounds, current int) int {
					return centeredPaneContentOffset(bounds.minY, bounds.maxY)
				},
			)
		case input.ActionCenterPaneContentHorizontal:
			changed = m.alignPaneContentOffset(paneID, func(bounds paneContentOffsetBounds, current int) int {
				return centeredPaneContentOffset(bounds.minX, bounds.maxX)
			}, nil)
		case input.ActionCenterPaneContentVertical:
			changed = m.alignPaneContentOffset(paneID, nil, func(bounds paneContentOffsetBounds, current int) int {
				return centeredPaneContentOffset(bounds.minY, bounds.maxY)
			})
		case input.ActionResetPaneContentOffset:
			changed = m.setPaneContentOffsetClamped(paneID, 0, 0)
		}
		m.render.Invalidate()
		if changed {
			m.render.RevealCursorBlink()
		}
		return true, nil
	case input.ActionPasteBuffer:
		if m.effectiveInputMode() != input.ModeDisplay {
			return false, nil
		}
		m.leaveCopyMode()
		m.setMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.pasteBufferToActiveCmd()
	case input.ActionPasteClipboard:
		if m.effectiveInputMode() != input.ModeDisplay {
			return false, nil
		}
		m.leaveCopyMode()
		m.setMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.pasteClipboardToActiveCmd()
	case input.ActionOpenClipboardHistory:
		if m.effectiveInputMode() != input.ModeDisplay {
			return false, nil
		}
		return true, m.openClipboardHistory()
	case input.ActionZoomPane:
		switch m.mode().Kind {
		case input.ModeDisplay:
			return true, nil
		case input.ModePane:
		case input.ModeWorkspacePicker:
			return false, nil
		default:
			return false, nil
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
		if pane := m.workbench.ActivePane(); pane != nil {
			if _, changed := m.adjustPaneViewportOffset(pane.ID, 1); changed {
				m.render.Invalidate()
			}
		}
		return true, m.ensureActivePaneScrollbackCmd()
	case input.ActionScrollDown:
		if pane := m.workbench.ActivePane(); pane != nil {
			if _, changed := m.adjustPaneViewportOffset(pane.ID, -1); changed {
				m.render.Invalidate()
			}
		}
		return true, m.ensureActivePaneScrollbackCmd()
	default:
		return false, nil
	}
}
