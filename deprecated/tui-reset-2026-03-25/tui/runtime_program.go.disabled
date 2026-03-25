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
	// 真实运行时需要进入 alt-screen，避免 runtime terminal 输出把 TUI chrome 冲掉，
	// 也让 header/body/footer 这些稳定壳层始终留在当前可视区里。
	program := tea.NewProgram(
		model,
		tea.WithInput(input),
		tea.WithOutput(output),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := program.Run()
	return err
}
