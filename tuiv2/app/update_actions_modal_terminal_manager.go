package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/orchestrator"
)

func (m *Model) handleTerminalManagerModalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil || m.terminalPage == nil {
		return false, nil
	}
	switch action.Kind {
	case input.ActionPickerUp:
		m.terminalPage.Move(-1)
		m.render.Invalidate()
		return true, nil
	case input.ActionPickerDown:
		m.terminalPage.Move(1)
		m.render.Invalidate()
		return true, nil
	case input.ActionCancelMode:
		m.closeTerminalManager()
		return true, nil
	case input.ActionSubmitPrompt:
		if selected := m.selectedTerminalManagerItem(); selected != nil {
			m.closeTerminalManager()
			if terminalSelectionNeedsReferenceBinding(selected) {
				return true, m.bindTerminalSelectionCmd("", m.currentOrActionPaneID(action.PaneID), *selected)
			}
			return true, m.attachPaneTerminalCmd("", m.currentOrActionPaneID(action.PaneID), selected.TerminalID)
		}
		return true, nil
	case input.ActionAttachTab:
		if selected := m.selectedTerminalManagerItem(); selected != nil {
			m.closeTerminalManager()
			if terminalSelectionNeedsReferenceBinding(selected) {
				return true, m.createTabAndBindTerminalCmd(*selected)
			}
			return true, m.createTabAndAttachTerminalCmd(selected.TerminalID)
		}
		return true, nil
	case input.ActionAttachFloating:
		if selected := m.selectedTerminalManagerItem(); selected != nil {
			m.closeTerminalManager()
			if terminalSelectionNeedsReferenceBinding(selected) {
				return true, m.createFloatingPaneAndBindTerminalCmd(*selected)
			}
			return true, m.createFloatingPaneAndAttachTerminalCmd(selected.TerminalID)
		}
		return true, nil
	case input.ActionEditTerminal:
		if selected := m.selectedTerminalManagerItem(); selected != nil {
			m.openEditTerminalPrompt(selected)
			return true, nil
		}
		return true, nil
	case input.ActionKillTerminal:
		if selected := m.selectedTerminalManagerItem(); selected != nil {
			terminalID := selected.TerminalID
			items := m.terminalPage.Items
			filtered := items[:0]
			for _, item := range items {
				if item.TerminalID != terminalID {
					filtered = append(filtered, item)
				}
			}
			m.terminalPage.Items = filtered
			m.terminalPage.ApplyFilter()
			normalizeModalSelection(&m.terminalPage.Selected, len(m.terminalPage.VisibleItems()))
			m.render.Invalidate()
			return true, m.effectCmd(orchestrator.KillTerminalEffect{TerminalID: terminalID})
		}
		return true, nil
	default:
		return false, nil
	}
}
