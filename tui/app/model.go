package app

import tea "github.com/charmbracelet/bubbletea"

type Model struct {
	WorkspaceName string
	Screen        Screen
}

func NewModel(workspace string) Model {
	if workspace == "" {
		workspace = "main"
	}
	return Model{
		WorkspaceName: workspace,
		Screen:        ScreenWorkbench,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	intent, ok := msg.(Intent)
	if !ok {
		return m, nil
	}
	next, _ := Reduce(m, intent)
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
