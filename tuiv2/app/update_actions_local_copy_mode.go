package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
)

func (m *Model) handleCopyModeLocalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil {
		return false, nil
	}
	switch action.Kind {
	case input.ActionCopyModeCursorLeft:
		return true, m.moveCopyCursor(0, -1)
	case input.ActionCopyModeCursorRight:
		return true, m.moveCopyCursor(0, 1)
	case input.ActionCopyModeCursorUp:
		return true, m.moveCopyCursorVertical(-1)
	case input.ActionCopyModeCursorDown:
		return true, m.moveCopyCursorVertical(1)
	case input.ActionCopyModePageUp:
		if buffer, ok := m.activeCopyModeBuffer(); ok {
			return true, m.moveCopyCursorVertical(-maxInt(1, buffer.height))
		}
		return true, nil
	case input.ActionCopyModePageDown:
		if buffer, ok := m.activeCopyModeBuffer(); ok {
			return true, m.moveCopyCursorVertical(maxInt(1, buffer.height))
		}
		return true, nil
	case input.ActionCopyModeHalfPageUp:
		if buffer, ok := m.activeCopyModeBuffer(); ok {
			return true, m.moveCopyCursorVertical(-maxInt(1, buffer.height/2))
		}
		return true, nil
	case input.ActionCopyModeHalfPageDown:
		if buffer, ok := m.activeCopyModeBuffer(); ok {
			return true, m.moveCopyCursorVertical(maxInt(1, buffer.height/2))
		}
		return true, nil
	case input.ActionCopyModeStartOfLine:
		m.setCopyCursorCol(0)
		return true, nil
	case input.ActionCopyModeEndOfLine:
		if !m.ensureCopyMode() {
			return true, nil
		}
		if buffer, ok := m.activeCopyModeBuffer(); ok {
			m.setCopyCursorCol(buffer.rowMaxCol(m.copyMode.Cursor.Row))
		}
		return true, nil
	case input.ActionCopyModeTop:
		return true, m.jumpCopyCursor(0)
	case input.ActionCopyModeBottom:
		if buffer, ok := m.activeCopyModeBuffer(); ok {
			return true, m.jumpCopyCursor(buffer.totalRows() - 1)
		}
		return true, nil
	case input.ActionCopyModeBeginSelection:
		if m.ensureCopyMode() && m.copyMode.Mark != nil {
			return true, m.copySelectionToClipboard(false)
		}
		m.beginCopySelection()
		return true, nil
	case input.ActionCopyModeCopySelection:
		return true, m.copySelectionToClipboard(false)
	case input.ActionCopyModeCopySelectionExit:
		if m.ensureCopyMode() && m.copyMode.Mark != nil {
			return true, m.copySelectionToClipboard(true)
		}
		m.setMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, nil
	default:
		return false, nil
	}
}
