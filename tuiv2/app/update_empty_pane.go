package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

const emptyPaneActionCount = 4

func (m *Model) handleEmptyPaneKeyMsg(msg tea.KeyMsg) (bool, tea.Cmd) {
	if !m.emptyPaneKeyboardNavigationEnabled() {
		return false, nil
	}
	if m.activeEmptyPaneID() == "" {
		return false, nil
	}
	switch msg.Type {
	case tea.KeyUp:
		if m.moveEmptyPaneSelection(-1) {
			m.render.Invalidate()
		}
		return true, nil
	case tea.KeyDown:
		if m.moveEmptyPaneSelection(1) {
			m.render.Invalidate()
		}
		return true, nil
	case tea.KeyEnter:
		return true, m.submitEmptyPaneSelection()
	default:
		return false, nil
	}
}

func (m *Model) emptyPaneKeyboardNavigationEnabled() bool {
	if m == nil || m.input == nil || m.terminalPage != nil {
		return false
	}
	if m.modalHost != nil && m.modalHost.Session != nil {
		return false
	}
	return m.mode().Kind == input.ModeNormal
}

func (m *Model) activeEmptyPaneID() string {
	if m == nil || m.workbench == nil {
		return ""
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID == "" || strings.TrimSpace(pane.TerminalID) != "" {
		return ""
	}
	return pane.ID
}

func (m *Model) currentEmptyPaneSelection() (string, int, bool) {
	if !m.emptyPaneKeyboardNavigationEnabled() {
		return "", 0, false
	}
	paneID := m.activeEmptyPaneID()
	if paneID == "" {
		return "", 0, false
	}
	selected := m.emptyPaneSelectionIndex
	if m.emptyPaneSelectionPaneID != paneID {
		selected = 0
	}
	return paneID, clampEmptyPaneSelection(selected), true
}

func (m *Model) moveEmptyPaneSelection(delta int) bool {
	paneID, selected, ok := m.currentEmptyPaneSelection()
	if !ok {
		return false
	}
	next := clampEmptyPaneSelection(selected + delta)
	if m.emptyPaneSelectionPaneID == paneID && m.emptyPaneSelectionIndex == next {
		return false
	}
	m.emptyPaneSelectionPaneID = paneID
	m.emptyPaneSelectionIndex = next
	return true
}

func (m *Model) submitEmptyPaneSelection() tea.Cmd {
	paneID, selected, ok := m.currentEmptyPaneSelection()
	if !ok {
		return nil
	}
	m.emptyPaneSelectionPaneID = paneID
	m.emptyPaneSelectionIndex = selected
	switch selected {
	case 0:
		return tea.Batch(m.openPickerForPaneCmd(paneID), m.saveStateCmd())
	case 1:
		m.openCreateTerminalPrompt(paneID, modal.CreateTargetReplace)
		return nil
	case 2:
		return m.openTerminalManagerCmd()
	case 3:
		return func() tea.Msg {
			return input.SemanticAction{Kind: input.ActionClosePane, PaneID: paneID}
		}
	default:
		return nil
	}
}

func clampEmptyPaneSelection(selected int) int {
	if selected < 0 {
		return 0
	}
	if selected >= emptyPaneActionCount {
		return emptyPaneActionCount - 1
	}
	return selected
}
