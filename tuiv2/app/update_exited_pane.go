package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
)

const exitedPaneActionCount = 2

func (m *Model) handleExitedPaneKeyMsg(msg tea.KeyMsg) (bool, tea.Cmd) {
	if !m.exitedPaneKeyboardNavigationEnabled() {
		return false, nil
	}
	if m.activeExitedPaneID() == "" {
		return false, nil
	}
	switch msg.Type {
	case tea.KeyUp, tea.KeyShiftTab:
		if m.moveExitedPaneSelection(-1) {
			m.render.Invalidate()
		}
		return true, nil
	case tea.KeyDown, tea.KeyTab:
		if m.moveExitedPaneSelection(1) {
			m.render.Invalidate()
		}
		return true, nil
	case tea.KeyEnter:
		return true, m.submitExitedPaneSelection()
	default:
		return false, nil
	}
}

func (m *Model) exitedPaneKeyboardNavigationEnabled() bool {
	if m == nil || m.input == nil || m.terminalPage != nil {
		return false
	}
	if m.modalHost != nil && m.modalHost.Session != nil {
		return false
	}
	return m.mode().Kind == input.ModeNormal
}

func (m *Model) activeExitedPaneID() string {
	if m == nil || m.workbench == nil || m.runtime == nil {
		return ""
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID == "" || strings.TrimSpace(pane.TerminalID) == "" {
		return ""
	}
	if m.isPaneAttachPending(pane.ID) {
		return ""
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil || terminal.State != "exited" {
		return ""
	}
	return pane.ID
}

func (m *Model) currentExitedPaneSelection() (string, int, bool) {
	if !m.exitedPaneKeyboardNavigationEnabled() {
		return "", 0, false
	}
	paneID := m.activeExitedPaneID()
	if paneID == "" {
		return "", 0, false
	}
	selected := m.exitedPaneSelectionIndex
	if m.exitedPaneSelectionPaneID != paneID {
		selected = 0
	}
	return paneID, clampExitedPaneSelection(selected), true
}

func (m *Model) moveExitedPaneSelection(delta int) bool {
	paneID, selected, ok := m.currentExitedPaneSelection()
	if !ok {
		return false
	}
	next := clampExitedPaneSelection(selected + delta)
	if m.exitedPaneSelectionPaneID == paneID && m.exitedPaneSelectionIndex == next {
		return false
	}
	m.exitedPaneSelectionPaneID = paneID
	m.exitedPaneSelectionIndex = next
	return true
}

func (m *Model) submitExitedPaneSelection() tea.Cmd {
	paneID, selected, ok := m.currentExitedPaneSelection()
	if !ok {
		return nil
	}
	m.exitedPaneSelectionPaneID = paneID
	m.exitedPaneSelectionIndex = selected
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID != paneID || pane.TerminalID == "" {
		return nil
	}
	switch selected {
	case 0:
		return m.restartPaneTerminalCmd(paneID, pane.TerminalID)
	case 1:
		return tea.Batch(m.openPickerForPaneCmd(paneID), m.saveStateCmd())
	default:
		return nil
	}
}

func clampExitedPaneSelection(selected int) int {
	if selected < 0 {
		return 0
	}
	if selected >= exitedPaneActionCount {
		return exitedPaneActionCount - 1
	}
	return selected
}
