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
		return true, m.moveCopyCursorLogicalOffset(-1)
	case input.ActionCopyModeCursorRight:
		return true, m.moveCopyCursorLogicalOffset(1)
	case input.ActionCopyModeCursorUp:
		return true, m.moveCopyCursorLogicalLines(-1)
	case input.ActionCopyModeCursorDown:
		return true, m.moveCopyCursorLogicalLines(1)
	case input.ActionCopyModePageUp:
		if buffer, ok := m.activeCopyModeBuffer(); ok {
			return true, m.moveCopyCursorLogicalLines(-maxInt(1, buffer.height))
		}
		return true, nil
	case input.ActionCopyModePageDown:
		if buffer, ok := m.activeCopyModeBuffer(); ok {
			return true, m.moveCopyCursorLogicalLines(maxInt(1, buffer.height))
		}
		return true, nil
	case input.ActionCopyModeHalfPageUp:
		if buffer, ok := m.activeCopyModeBuffer(); ok {
			return true, m.moveCopyCursorLogicalLines(-maxInt(1, buffer.height/2))
		}
		return true, nil
	case input.ActionCopyModeHalfPageDown:
		if buffer, ok := m.activeCopyModeBuffer(); ok {
			return true, m.moveCopyCursorLogicalLines(maxInt(1, buffer.height/2))
		}
		return true, nil
	case input.ActionCopyModeStartOfLine:
		return true, m.setCopyCursorLogicalOffset(0)
	case input.ActionCopyModeEndOfLine:
		if !m.ensureCopyMode() {
			return true, nil
		}
		if buffer, ok := m.activeCopyModeBuffer(); ok {
			if line, ok := buffer.logicalLineByIndex(m.copyMode.CursorLogical.Line); ok {
				return true, m.setCopyCursorLogicalOffset(len(line.Text) - 1)
			}
		}
		return true, nil
	case input.ActionCopyModeTop:
		if !m.ensureCopyMode() {
			return true, nil
		}
		cmd := m.jumpCopyCursorLogicalLine(0)
		if m.copyMode.CursorLogical.Line == 0 && m.copyMode.ViewTopRow == 0 {
			if buffer, ok := m.activeCopyModeBuffer(); ok {
				cmd = batchCmds(cmd, m.prefetchCopyModeScrollbackCmd(buffer))
			}
		}
		return true, cmd
	case input.ActionCopyModeBottom:
		if buffer, ok := m.activeCopyModeBuffer(); ok {
			return true, m.jumpCopyCursorLogicalLine(buffer.logicalLineCount() - 1)
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
