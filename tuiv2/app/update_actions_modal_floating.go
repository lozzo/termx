package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
)

func (m *Model) handleFloatingOverviewModalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil || m.modalHost == nil || m.modalHost.FloatingOverview == nil {
		return false, nil
	}
	switch action.Kind {
	case input.ActionPickerUp:
		m.modalHost.FloatingOverview.Move(-1)
		m.render.Invalidate()
		return true, nil
	case input.ActionPickerDown:
		m.modalHost.FloatingOverview.Move(1)
		m.render.Invalidate()
		return true, nil
	case input.ActionCancelMode:
		m.closeFloatingOverview()
		return true, nil
	case input.ActionSubmitPrompt:
		return true, m.focusFloatingOverviewSelection()
	case input.ActionExpandAllFloatingPanes:
		return true, m.expandAllFloatingPanes()
	case input.ActionCollapseAllFloatingPanes:
		return true, m.collapseAllFloatingPanes()
	case input.ActionCloseFloatingPane:
		if selected := m.modalHost.FloatingOverview.SelectedItem(); selected != nil {
			return true, m.closeFloatingPaneDirect(selected.PaneID)
		}
		return true, nil
	default:
		return false, nil
	}
}
