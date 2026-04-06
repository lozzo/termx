package app

import (
	"context"
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
	terminalIDs := make([]string, 0, len(tab.Panes))
	seen := make(map[string]struct{}, len(tab.Panes))
	for _, pane := range tab.Panes {
		if pane == nil || pane.TerminalID == "" {
			continue
		}
		if _, exists := seen[pane.TerminalID]; exists {
			continue
		}
		seen[pane.TerminalID] = struct{}{}
		terminalIDs = append(terminalIDs, pane.TerminalID)
	}
	tabID := tab.ID
	return func() tea.Msg {
		if err := m.workbench.CloseTab(tabID); err != nil {
			return err
		}
		if m.runtime != nil && m.runtime.Client() != nil {
			for _, terminalID := range terminalIDs {
				_ = m.runtime.Client().Kill(context.Background(), terminalID)
			}
		}
		m.setMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		if cmd := m.saveStateCmd(); cmd != nil {
			return cmd()
		}
		return nil
	}
}
