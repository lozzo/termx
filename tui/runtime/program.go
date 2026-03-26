package runtime

import (
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

type ProgramRunner interface {
	Run(model tea.Model, input io.Reader, output io.Writer) error
}

type BubbleTeaProgramRunner struct{}

func NewProgramRunner() ProgramRunner {
	return BubbleTeaProgramRunner{}
}

func (BubbleTeaProgramRunner) Run(model tea.Model, input io.Reader, output io.Writer) error {
	program := tea.NewProgram(model, tea.WithInput(input), tea.WithOutput(output))
	_, err := program.Run()
	return err
}
