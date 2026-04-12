package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
)

func (m *Model) handleLocalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil || m.modalHost == nil {
		return false, nil
	}
	if handled, cmd := m.handleModeAndFloatingLocalAction(action); handled {
		return true, cmd
	}
	if handled, cmd := m.handleWorkspaceAndTabLocalAction(action); handled {
		return true, cmd
	}
	if handled, cmd := m.handleDisplayAndViewportLocalAction(action); handled {
		return true, cmd
	}
	switch action.Kind {
	case input.ActionBecomeOwner:
		if m.runtime == nil || m.workbench == nil {
			return true, nil
		}
		switch m.mode().Kind {
		case input.ModeNormal, input.ModePane, input.ModeResize, input.ModeFloating:
		default:
			return false, nil
		}
		paneID := m.currentOrActionPaneID(action.PaneID)
		if paneID == "" {
			return true, nil
		}
		pane := m.workbench.ActivePane()
		if pane == nil || pane.ID != paneID {
			tab := m.workbench.CurrentTab()
			if tab != nil {
				pane = tab.Panes[paneID]
			}
		}
		if pane == nil {
			return true, nil
		}
		m.ownerConfirmPaneID = ""
		m.ownerSeq++
		m.render.Invalidate()
		return true, m.syncTerminalInteractionCmd(terminalInteractionRequest{
			PaneID:           paneID,
			TerminalID:       pane.TerminalID,
			ResizeIfNeeded:   true,
			ExplicitTakeover: true,
		})
	case input.ActionToggleTerminalSizeLock:
		return true, m.toggleTerminalSizeLockCmd(action.PaneID)
	case input.ActionRestartTerminal:
		if m.runtime == nil || m.workbench == nil {
			return true, nil
		}
		switch m.mode().Kind {
		case input.ModeNormal, input.ModePane:
		default:
			return false, nil
		}
		paneID := m.currentOrActionPaneID(action.PaneID)
		if paneID == "" {
			return true, nil
		}
		pane := m.workbench.ActivePane()
		if pane == nil || pane.ID != paneID {
			tab := m.workbench.CurrentTab()
			if tab != nil {
				pane = tab.Panes[paneID]
			}
		}
		if pane == nil || pane.TerminalID == "" {
			return true, nil
		}
		terminal := m.runtime.Registry().Get(pane.TerminalID)
		if terminal == nil || terminal.State != "exited" {
			return true, nil
		}
		return true, m.restartPaneTerminalCmd(paneID, pane.TerminalID)
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
	case input.ActionQuit:
		if m.mode().Kind == input.ModeNormal {
			m.setMode(input.ModeState{Kind: input.ModeGlobal})
			m.render.Invalidate()
			return true, nil
		}
		m.quitting = true
		return true, tea.Batch(m.saveStateCmd(), tea.Quit)
	default:
		return false, nil
	}
}
