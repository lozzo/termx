package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
)

func (m *Model) switchCurrentTabByOffset(offset int) error {
	if m == nil || m.workbench == nil {
		return fmt.Errorf("workbench unavailable")
	}
	workspace := m.workbench.CurrentWorkspace()
	if workspace == nil || len(workspace.Tabs) == 0 {
		return fmt.Errorf("no tabs available")
	}
	current := workspace.ActiveTab
	if current < 0 || current >= len(workspace.Tabs) {
		current = 0
	}
	next := (current + offset + len(workspace.Tabs)) % len(workspace.Tabs)
	return m.workbench.SwitchTab(workspace.Name, next)
}

func (m *Model) killCurrentTabCmd() tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}
	service := m.tabLifecycleService()
	if service == nil {
		return nil
	}
	tabID := tab.ID
	bindings, terminalIDs := service.snapshotTabBindings(tabID)
	return func() tea.Msg {
		if err := service.close(tabID, bindings, terminalIDs, true); err != nil {
			return err
		}
		m.setMode(input.ModeState{Kind: input.ModeNormal})
		if cmd := m.saveStateCmd(); cmd != nil {
			return cmd()
		}
		return nil
	}
}
