package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

func (m *Model) handleModeAndFloatingLocalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil || m.modalHost == nil {
		return false, nil
	}
	switch action.Kind {
	case input.ActionEnterPaneMode:
		m.setMode(input.ModeState{Kind: input.ModePane})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterResizeMode:
		m.setMode(input.ModeState{Kind: input.ModeResize})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterTabMode:
		m.setMode(input.ModeState{Kind: input.ModeTab})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterWorkspaceMode:
		m.setMode(input.ModeState{Kind: input.ModeWorkspace})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterFloatingMode:
		m.ensureFloatingModeTarget()
		m.setMode(input.ModeState{Kind: input.ModeFloating})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionOpenFloatingOverview:
		return true, m.openFloatingOverview()
	case input.ActionEnterDisplayMode:
		m.setMode(input.ModeState{Kind: input.ModeDisplay})
		_ = m.ensureCopyMode()
		m.render.Invalidate()
		return true, m.ensureActivePaneScrollbackCmd()
	case input.ActionEnterGlobalMode:
		m.setMode(input.ModeState{Kind: input.ModeGlobal})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionCancelMode:
		if m.mode().Kind == input.ModeTerminalManager && m.terminalPage != nil {
			return false, nil
		}
		if m.modalHost == nil || m.modalHost.Session == nil {
			if m.mode().Kind == input.ModeDisplay {
				m.prepareCopyModeExit()
			} else {
				m.resetCopyMode()
			}
			m.setMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	case input.ActionOpenHelp:
		m.openModal(input.ModeHelp, "help")
		m.modalHost.Help = modal.DefaultHelp()
		m.markModalReady(input.ModeHelp, "help")
		m.render.Invalidate()
		return true, nil
	case input.ActionCollapseFloatingPane:
		return true, m.hideFloatingPane(action.PaneID)
	case input.ActionToggleFloatingVisibility:
		return true, m.toggleAllFloatingPanes()
	case input.ActionExpandAllFloatingPanes:
		return true, m.expandAllFloatingPanes()
	case input.ActionCollapseAllFloatingPanes:
		return true, m.collapseAllFloatingPanes()
	case input.ActionSummonFloatingPane:
		return true, m.summonFloatingPane(action.Text)
	case input.ActionAutoFitFloatingPane:
		return true, m.fitFloatingPaneToContent(action.PaneID)
	case input.ActionToggleFloatingAutoFit:
		return true, m.toggleFloatingAutoFit(action.PaneID)
	case input.ActionFocusPane:
		if m.mode().Kind == input.ModeNormal {
			m.setMode(input.ModeState{Kind: input.ModePane})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	case input.ActionOpenPrompt:
		if m.mode().Kind == input.ModeNormal {
			m.setMode(input.ModeState{Kind: input.ModeResize})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	case input.ActionMoveFloatingLeft, input.ActionMoveFloatingRight, input.ActionMoveFloatingUp, input.ActionMoveFloatingDown,
		input.ActionResizeFloatingLeft, input.ActionResizeFloatingRight, input.ActionResizeFloatingUp, input.ActionResizeFloatingDown:
		m.disableFloatingAutoFitForActionPane(action.PaneID)
		return false, nil
	case input.ActionOpenPicker:
		if m.workbench == nil {
			return false, nil
		}
		if pane := m.workbench.ActivePane(); pane != nil && pane.ID != "" {
			return false, nil
		}
		paneID, err := m.ensureRecoverablePane()
		if err != nil {
			return true, m.showError(err)
		}
		return true, tea.Batch(m.openPickerForPaneCmd(paneID), m.saveStateCmd())
	case input.ActionOpenTerminalManager:
		if m.workbench != nil && m.workbench.ActivePane() == nil && m.workbench.CurrentWorkspace() != nil {
			if _, err := m.ensureRecoverablePane(); err != nil {
				return true, m.showError(err)
			}
			return true, tea.Batch(m.openTerminalManagerCmd(), m.saveStateCmd())
		}
		if m.mode().Kind == input.ModeGlobal {
			return true, m.openTerminalManagerCmd()
		}
		return false, nil
	default:
		return false, nil
	}
}
