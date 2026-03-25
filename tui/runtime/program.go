package runtime

import (
	"context"
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app"
)

type ProgramRunner interface {
	Run(model tea.Model, input io.Reader, output io.Writer) error
}

type bubbleTeaProgramRunner struct{}

type workspaceSaveFlusher interface {
	FlushWorkspaceSave(context.Context) error
}

type persistentWorkspaceModel struct {
	model tea.Model
	loop  *UpdateLoop
}

func NewProgramRunner() ProgramRunner {
	return bubbleTeaProgramRunner{}
}

func WrapModelWithWorkspacePersistence(model tea.Model, loop *UpdateLoop) tea.Model {
	if loop == nil {
		return model
	}
	return &persistentWorkspaceModel{model: model, loop: loop}
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

	finalModel, err := tea.NewProgram(model, options...).Run()
	if finalModel != nil {
		if flusher, ok := finalModel.(workspaceSaveFlusher); ok {
			if flushErr := flusher.FlushWorkspaceSave(context.Background()); err == nil {
				err = flushErr
			}
		}
	}
	return err
}

func (m *persistentWorkspaceModel) Init() tea.Cmd {
	if m.model == nil {
		return nil
	}
	var cmds []tea.Cmd
	if cmd := m.model.Init(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if appModel, ok := extractAppModel(m.model); ok && appModel.PreviewStreamNext != nil {
		if cmd := appModel.PreviewStreamNext(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if m.loop != nil {
		if cmd := m.loop.NextCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (m *persistentWorkspaceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.model == nil {
		return m, nil
	}

	nextEventCmd := tea.Cmd(nil)
	if updateMsg, ok := msg.(UpdateMessage); ok {
		msg = app.DaemonEventMessage{Event: updateMsg.Event}
		if m.loop != nil {
			nextEventCmd = m.loop.NextCmd()
		}
	}
	previous := m.model
	next, cmd := m.model.Update(msg)
	if next != nil {
		m.model = next
	}
	saveCmd := m.loop.ObserveModelTransition(previous, m.model)
	return m, tea.Batch(cmd, saveCmd, nextEventCmd)
}

func (m *persistentWorkspaceModel) View() string {
	if m.model == nil {
		return ""
	}
	return m.model.View()
}

func (m *persistentWorkspaceModel) UnderlyingModel() tea.Model {
	return m.model
}

func (m *persistentWorkspaceModel) AppModel() app.Model {
	model, _ := extractAppModel(m.model)
	return model
}

func (m *persistentWorkspaceModel) FlushWorkspaceSave(ctx context.Context) error {
	if m == nil || m.loop == nil {
		return nil
	}
	return m.loop.Flush(ctx, m.model)
}
