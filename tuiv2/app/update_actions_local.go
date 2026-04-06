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
				m.leaveCopyMode()
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
	case input.ActionCreateTab:
		if m.mode().Kind == input.ModeNormal {
			m.setMode(input.ModeState{Kind: input.ModeTab})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	case input.ActionOpenWorkspacePicker:
		if m.mode().Kind == input.ModeNormal {
			m.setMode(input.ModeState{Kind: input.ModeWorkspace})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	case input.ActionCreateWorkspace:
		if m.mode().Kind != input.ModeWorkspace || m.workbench == nil {
			return false, nil
		}
		m.openCreateWorkspaceNamePrompt(input.ModeNormal)
		return true, nil
	case input.ActionDeleteWorkspace:
		if m.mode().Kind != input.ModeWorkspace || m.workbench == nil {
			return false, nil
		}
		ws := m.workbench.CurrentWorkspace()
		if ws == nil {
			return true, nil
		}
		if err := m.workbench.DeleteWorkspace(ws.Name); err != nil {
			return true, m.showError(err)
		}
		m.setMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionRenameWorkspace:
		if m.mode().Kind != input.ModeWorkspace {
			return false, nil
		}
		m.openRenameWorkspacePrompt()
		return true, nil
	case input.ActionPrevWorkspace:
		if m.mode().Kind != input.ModeWorkspace || m.workbench == nil {
			return false, nil
		}
		if err := m.workbench.SwitchWorkspaceByOffset(-1); err != nil {
			return true, m.showError(err)
		}
		m.setMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionNextWorkspace:
		if m.mode().Kind != input.ModeWorkspace || m.workbench == nil {
			return false, nil
		}
		if err := m.workbench.SwitchWorkspaceByOffset(1); err != nil {
			return true, m.showError(err)
		}
		m.setMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionRenameTab:
		if m.mode().Kind != input.ModeTab {
			return false, nil
		}
		m.openRenameTabPrompt()
		return true, nil
	case input.ActionJumpTab:
		if m.mode().Kind != input.ModeTab || m.workbench == nil {
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
		m.setMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionPrevTab:
		if m.mode().Kind != input.ModeTab || m.workbench == nil {
			return false, nil
		}
		if err := m.switchCurrentTabByOffset(-1); err != nil {
			return true, m.showError(err)
		}
		m.setMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionNextTab:
		if m.mode().Kind != input.ModeTab || m.workbench == nil {
			return false, nil
		}
		if err := m.switchCurrentTabByOffset(1); err != nil {
			return true, m.showError(err)
		}
		m.setMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionKillTab:
		if m.mode().Kind != input.ModeTab {
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
		if m.mode().Kind == input.ModeGlobal {
			return true, m.openTerminalManagerCmd()
		}
		return false, nil
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
		if m.mode().Kind == input.ModeNormal {
			m.setMode(input.ModeState{Kind: input.ModeDisplay})
			m.render.Invalidate()
			return true, nil
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
		m.beginCopySelection()
		return true, nil
	case input.ActionCopyModeCopySelection:
		return true, m.copySelectionToClipboard(false)
	case input.ActionCopyModeCopySelectionExit:
		return true, m.copySelectionToClipboard(true)
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
