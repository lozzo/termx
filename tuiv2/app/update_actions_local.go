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
	if handled, cmd := m.handleCopyModeLocalAction(action); handled {
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
