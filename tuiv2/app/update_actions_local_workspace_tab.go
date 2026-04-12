package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
)

func (m *Model) handleWorkspaceAndTabLocalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil {
		return false, nil
	}
	switch action.Kind {
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
	default:
		return false, nil
	}
}
