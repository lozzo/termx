package app

import (
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/shared"
)

// Run creates a new Model with the given Config and starts the bubbletea
// program. stdin/stdout are wired via the provided readers/writers so that
// tests can inject fakes without touching os.Stdin / os.Stdout.
func Run(cfg shared.Config, stdin io.Reader, stdout io.Writer) error {
	model := New(cfg, nil, nil)
	opts := []tea.ProgramOption{
		tea.WithInput(stdin),
		tea.WithOutput(stdout),
	}
	p := tea.NewProgram(model, opts...)
	_, err := p.Run()
	return err
}
