package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
)

func (m *Model) applyMouseSemanticAction(action input.SemanticAction) tea.Cmd {
	if m == nil {
		return nil
	}
	if handled, cmd := m.handleMouseLocalAction(action); handled {
		if m.isStickyMode() {
			return tea.Batch(cmd, m.rearmPrefixTimeoutCmd())
		}
		return cmd
	}
	return m.dispatchSemanticActionCmd(action, true)
}

func (m *Model) cancelActiveModal() tea.Cmd {
	if m == nil {
		return nil
	}
	if handled, cmd := m.handleModalAction(input.SemanticAction{Kind: input.ActionCancelMode}); handled {
		return cmd
	}
	return nil
}

func (m *Model) handleMouseLocalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil || m.workbench == nil {
		return false, nil
	}
	switch action.Kind {
	case input.ActionCreateTab, input.ActionOpenWorkspacePicker, input.ActionZoomPane:
		return true, m.dispatchSemanticActionCmd(action, false)
	case input.ActionRenameTab:
		if m.workbench.CurrentTab() == nil {
			return true, nil
		}
		m.openRenameTabPrompt()
		return true, nil
	case input.ActionKillTab:
		if m.workbench.CurrentTab() == nil {
			return true, nil
		}
		return true, m.killCurrentTabCmd()
	case input.ActionRenameWorkspace:
		if m.workbench.CurrentWorkspace() == nil {
			return true, nil
		}
		m.openRenameWorkspacePrompt()
		return true, nil
	case input.ActionPrevWorkspace:
		return true, m.switchWorkspaceByOffsetMouse(-1)
	case input.ActionNextWorkspace:
		return true, m.switchWorkspaceByOffsetMouse(1)
	default:
		return false, nil
	}
}

func (m *Model) switchWorkspaceByOffsetMouse(offset int) tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	if err := m.workbench.SwitchWorkspaceByOffset(offset); err != nil {
		return m.showError(err)
	}
	m.render.Invalidate()
	return m.saveStateCmd()
}
