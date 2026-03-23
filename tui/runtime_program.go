package tui

import (
	"io"

	tea "github.com/charmbracelet/bubbletea"
	btui "github.com/lozzow/termx/tui/bt"
)

type ProgramRunner interface {
	Run(model *btui.Model, input io.Reader, output io.Writer) error
}

type bubbleteaProgramRunner struct{}

func (bubbleteaProgramRunner) Run(model *btui.Model, input io.Reader, output io.Writer) error {
	program := tea.NewProgram(model, tea.WithInput(input), tea.WithOutput(output))
	_, err := program.Run()
	return err
}
