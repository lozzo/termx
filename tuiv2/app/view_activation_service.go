package app

import tea "github.com/charmbracelet/bubbletea"

func (m *Model) postViewActivationCmd() tea.Cmd {
	if m == nil {
		return nil
	}
	return batchCmds(
		m.syncActivePaneTabSwitchTakeoverCmd(),
		m.resizePendingPaneResizesCmd(),
		m.saveStateCmd(),
	)
}
