package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/shared"
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
	case input.ActionFocusPane:
		if m.input.Mode().Kind == input.ModeNormal {
			m.input.SetMode(input.ModeState{Kind: input.ModePane})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	case input.ActionOpenPrompt:
		if m.input.Mode().Kind == input.ModeNormal {
			m.input.SetMode(input.ModeState{Kind: input.ModeResize})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
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
		name := "workspace-" + shared.GenerateShortID()
		if err := m.workbench.CreateWorkspace(name); err != nil {
			return true, m.showError(err)
		}
		_ = m.workbench.SwitchWorkspace(name)
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
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
		return true, tea.Batch(m.resizeVisiblePanesCmd(), m.saveStateCmd())
	case input.ActionPrevTab:
		if m.input.Mode().Kind != input.ModeTab || m.workbench == nil {
			return false, nil
		}
		if err := m.switchCurrentTabByOffset(-1); err != nil {
			return true, m.showError(err)
		}
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, tea.Batch(m.resizeVisiblePanesCmd(), m.saveStateCmd())
	case input.ActionNextTab:
		if m.input.Mode().Kind != input.ModeTab || m.workbench == nil {
			return false, nil
		}
		if err := m.switchCurrentTabByOffset(1); err != nil {
			return true, m.showError(err)
		}
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, tea.Batch(m.resizeVisiblePanesCmd(), m.saveStateCmd())
	case input.ActionKillTab:
		if m.input.Mode().Kind != input.ModeTab {
			return false, nil
		}
		return true, m.killCurrentTabCmd()
	case input.ActionOpenTerminalManager:
		if m.input.Mode().Kind == input.ModeGlobal {
			m.terminalPage = &modal.TerminalManagerState{
				Title:  "Terminal Pool",
				Footer: input.FooterForMode(input.ModeTerminalManager, false),
			}
			m.input.SetMode(input.ModeState{Kind: input.ModeTerminalManager, RequestID: terminalPoolPageModeToken})
			m.render.Invalidate()
			return true, m.loadTerminalManagerItemsCmd()
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
		return true, nil
	case input.ActionScrollDown:
		if tab := m.workbench.CurrentTab(); tab != nil {
			if tab.ScrollOffset > 0 {
				tab.ScrollOffset -= 1
			}
			m.render.Invalidate()
		}
		return true, nil
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
