package app

import (
	tea "github.com/charmbracelet/bubbletea"
	featureoverlay "github.com/lozzow/termx/tui/features/overlay"
	featureworkbench "github.com/lozzow/termx/tui/features/workbench"
)

type Model struct {
	WorkspaceName string
	Screen        Screen
	Workbench     featureworkbench.State
	Overlay       featureoverlay.State
}

func NewModel(workspace string) Model {
	if workspace == "" {
		workspace = "main"
	}
	return Model{
		WorkspaceName: workspace,
		Screen:        ScreenWorkbench,
		Workbench:     featureworkbench.New(workspace),
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, _ := Reduce(m, msg)
	return next, nil
}

func (m Model) View() string {
	switch m.Screen {
	case ScreenTerminalPool:
		return "termx terminal pool"
	default:
		return "termx workbench"
	}
}
