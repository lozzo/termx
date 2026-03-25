package runtime

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type quitModel struct{}

func (quitModel) Init() tea.Cmd {
	return tea.Quit
}

func (m quitModel) Update(tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (quitModel) View() string {
	return ""
}

func TestProgramRunnerRunsTeaModel(t *testing.T) {
	runner := NewProgramRunner()
	if err := runner.Run(quitModel{}, strings.NewReader(""), io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}
}
