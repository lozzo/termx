package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

func (m *Model) handleLocalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil || m.modalHost == nil {
		return false, nil
	}
	switch action.Kind {
	case input.ActionEnterPaneMode:
		m.input.SetMode(input.ModeState{Kind: input.ModePane})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterResizeMode:
		m.input.SetMode(input.ModeState{Kind: input.ModeResize})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterTabMode:
		m.input.SetMode(input.ModeState{Kind: input.ModeTab})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterWorkspaceMode:
		m.input.SetMode(input.ModeState{Kind: input.ModeWorkspace})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterFloatingMode:
		m.ensureFloatingModeTarget()
		m.input.SetMode(input.ModeState{Kind: input.ModeFloating})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionOpenFloatingOverview:
		return true, m.openFloatingOverview()
	case input.ActionEnterDisplayMode:
		m.input.SetMode(input.ModeState{Kind: input.ModeDisplay})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterGlobalMode:
		m.input.SetMode(input.ModeState{Kind: input.ModeGlobal})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionCancelMode:
		if m.input.Mode().Kind == input.ModeTerminalManager && m.terminalPage != nil {
			return false, nil
		}
		if m.modalHost == nil || m.modalHost.Session == nil {
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	case input.ActionOpenHelp:
		m.modalHost.Open(input.ModeHelp, "help")
		m.modalHost.Help = modal.DefaultHelp()
		m.modalHost.MarkReady(input.ModeHelp, "help")
		m.input.SetMode(input.ModeState{Kind: input.ModeHelp, RequestID: "help"})
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
		if m.input.Mode().Kind == input.ModeNormal {
			m.input.SetMode(input.ModeState{Kind: input.ModePane})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	case input.ActionBecomeOwner:
		if m.runtime == nil || m.workbench == nil {
			return true, nil
		}
		switch m.input.Mode().Kind {
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
		if m.sessionID != "" {
			return true, m.acquireSessionLeaseAndResizeCmd(paneID, pane.TerminalID)
		}
		if err := m.runtime.AcquireTerminalOwnership(paneID, pane.TerminalID); err != nil {
			return true, m.showError(err)
		}
		m.render.Invalidate()
		if m.runtime.Client() == nil {
			return true, nil
		}
		return true, m.resizePaneIfNeededCmd(paneID)
	case input.ActionOpenPrompt:
		if m.input.Mode().Kind == input.ModeNormal {
			m.input.SetMode(input.ModeState{Kind: input.ModeResize})
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
	case input.ActionCreateTab:
		if m.input.Mode().Kind == input.ModeNormal {
			m.input.SetMode(input.ModeState{Kind: input.ModeTab})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	case input.ActionOpenWorkspacePicker:
		if m.input.Mode().Kind == input.ModeNormal {
			m.input.SetMode(input.ModeState{Kind: input.ModeWorkspace})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	case input.ActionCreateWorkspace:
		if m.input.Mode().Kind != input.ModeWorkspace || m.workbench == nil {
			return false, nil
		}
		m.openCreateWorkspaceNamePrompt(input.ModeNormal)
		return true, nil
	case input.ActionDeleteWorkspace:
		if m.input.Mode().Kind != input.ModeWorkspace || m.workbench == nil {
			return false, nil
		}
		ws := m.workbench.CurrentWorkspace()
		if ws == nil {
			return true, nil
		}
		if err := m.workbench.DeleteWorkspace(ws.Name); err != nil {
			return true, m.showError(err)
		}
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionRenameWorkspace:
		if m.input.Mode().Kind != input.ModeWorkspace {
			return false, nil
		}
		m.openRenameWorkspacePrompt()
		return true, nil
	case input.ActionPrevWorkspace:
		if m.input.Mode().Kind != input.ModeWorkspace || m.workbench == nil {
			return false, nil
		}
		if err := m.workbench.SwitchWorkspaceByOffset(-1); err != nil {
			return true, m.showError(err)
		}
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionNextWorkspace:
		if m.input.Mode().Kind != input.ModeWorkspace || m.workbench == nil {
			return false, nil
		}
		if err := m.workbench.SwitchWorkspaceByOffset(1); err != nil {
			return true, m.showError(err)
		}
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionRenameTab:
		if m.input.Mode().Kind != input.ModeTab {
			return false, nil
		}
		m.openRenameTabPrompt()
		return true, nil
	case input.ActionJumpTab:
		if m.input.Mode().Kind != input.ModeTab || m.workbench == nil {
			return false, nil
		}
		index, err := strconv.Atoi(strings.TrimSpace(action.Text))
		if err != nil || index < 1 {
			return true, nil
		}
		ws := m.workbench.CurrentWorkspace()
		if ws == nil {
			return true, nil
		}
		if err := m.workbench.SwitchTab(ws.Name, index-1); err != nil {
			return true, m.showError(err)
		}
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionPrevTab:
		if m.input.Mode().Kind != input.ModeTab || m.workbench == nil {
			return false, nil
		}
		if err := m.switchCurrentTabByOffset(-1); err != nil {
			return true, m.showError(err)
		}
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionNextTab:
		if m.input.Mode().Kind != input.ModeTab || m.workbench == nil {
			return false, nil
		}
		if err := m.switchCurrentTabByOffset(1); err != nil {
			return true, m.showError(err)
		}
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionKillTab:
		if m.input.Mode().Kind != input.ModeTab {
			return false, nil
		}
		return true, m.killCurrentTabCmd()
	case input.ActionOpenTerminalManager:
		if m.workbench != nil && m.workbench.ActivePane() == nil && m.workbench.CurrentWorkspace() != nil {
			if _, err := m.ensureRecoverablePane(); err != nil {
				return true, m.showError(err)
			}
			return true, tea.Batch(m.openTerminalManagerCmd(), m.saveStateCmd())
		}
		if m.input.Mode().Kind == input.ModeGlobal {
			return true, m.openTerminalManagerCmd()
		}
		return false, nil
	case input.ActionZoomPane:
		if m.input.Mode().Kind == input.ModeNormal {
			m.input.SetMode(input.ModeState{Kind: input.ModeDisplay})
			m.render.Invalidate()
			return true, nil
		}
		if m.workbench != nil {
			if tab := m.workbench.CurrentTab(); tab != nil {
				paneID := action.PaneID
				if paneID == "" {
					paneID = tab.ActivePaneID
				}
				if tab.ZoomedPaneID == paneID {
					tab.ZoomedPaneID = ""
				} else {
					tab.ZoomedPaneID = paneID
				}
				m.render.Invalidate()
			}
		}
		return true, nil
	case input.ActionScrollUp:
		if tab := m.workbench.CurrentTab(); tab != nil {
			tab.ScrollOffset += 1
			m.render.Invalidate()
		}
		return true, m.ensureActivePaneScrollbackCmd()
	case input.ActionScrollDown:
		if tab := m.workbench.CurrentTab(); tab != nil {
			if tab.ScrollOffset > 0 {
				tab.ScrollOffset -= 1
			}
			m.render.Invalidate()
		}
		return true, m.ensureActivePaneScrollbackCmd()
	case input.ActionQuit:
		if m.input.Mode().Kind == input.ModeNormal {
			m.input.SetMode(input.ModeState{Kind: input.ModeGlobal})
			m.render.Invalidate()
			return true, nil
		}
		m.quitting = true
		return true, tea.Batch(m.saveStateCmd(), tea.Quit)
	default:
		return false, nil
	}
}
