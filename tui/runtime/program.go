package runtime

import (
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

type ProgramRunner interface {
	Run(model tea.Model, input io.Reader, output io.Writer) error
}

type bubbleTeaProgramRunner struct{}

func NewProgramRunner() ProgramRunner {
	return bubbleTeaProgramRunner{}
}

// Run 把根模型挂到 Bubble Tea 程序上。
// 这里单独抽接口，是为了让 tui.Run 在测试里只验证接线，不依赖真实终端事件循环。
func (bubbleTeaProgramRunner) Run(model tea.Model, input io.Reader, output io.Writer) error {
	options := make([]tea.ProgramOption, 0, 2)
	if input != nil {
		options = append(options, tea.WithInput(input))
	}
	if output != nil {
		options = append(options, tea.WithOutput(output))
	}

	_, err := tea.NewProgram(model, options...).Run()
	return err
}
