package runtime

import (
	"context"
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/input"
	renderoverlay "github.com/lozzow/termx/tui/render/overlay"
	"github.com/lozzow/termx/tui/render/projection"
	renderterminalpool "github.com/lozzow/termx/tui/render/terminalpool"
	renderworkbench "github.com/lozzow/termx/tui/render/workbench"
)

type ProgramRunner interface {
	Run(model tea.Model, input io.Reader, output io.Writer) error
}

type BubbleTeaProgramRunner struct{}

type renderModel struct {
	model   app.Model
	router  input.Router
	runner  EffectExecutor
	width   int
	height  int
}

type EffectExecutor interface {
	Run(ctx context.Context, effect app.Effect) app.Message
}

type effectResultMsg struct {
	message app.Message
}

func NewProgramRunner() ProgramRunner {
	return BubbleTeaProgramRunner{}
}

func NewRenderModel(model app.Model) tea.Model {
	return &renderModel{model: model, router: input.NewRouter(), width: 80, height: 24}
}

func NewRenderModelWithRunner(model app.Model, runner EffectExecutor) tea.Model {
	return &renderModel{model: model, router: input.NewRouter(), runner: runner, width: 80, height: 24}
}

func ExtractAppModel(model tea.Model) (app.Model, bool) {
	switch typed := model.(type) {
	case app.Model:
		return typed, true
	case *renderModel:
		return typed.model, true
	default:
		return app.Model{}, false
	}
}

func (BubbleTeaProgramRunner) Run(model tea.Model, input io.Reader, output io.Writer) error {
	if root, ok := model.(app.Model); ok {
		model = NewRenderModel(root)
	}
	program := tea.NewProgram(model, tea.WithInput(input), tea.WithOutput(output))
	_, err := program.Run()
	return err
}

func (m *renderModel) Init() tea.Cmd {
	return m.model.Init()
}

func (m *renderModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		m.height = size.Height
	}
	if result, ok := msg.(effectResultMsg); ok {
		msg = result.message
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		if intent := m.router.Translate(input.Context{Screen: m.model.Screen, OverlayKind: m.model.Overlay.Active.Kind}, key); intent != nil {
			msg = intent
		}
	}
	next, effects := app.Reduce(m.model, msg)
	m.model = next
	return m, batchEffectCmds(m.runner, effects)
}

func batchEffectCmds(runner EffectExecutor, effects []app.Effect) tea.Cmd {
	if runner == nil || len(effects) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(effects))
	for _, effect := range effects {
		effectCopy := effect
		cmds = append(cmds, func() tea.Msg {
			message := runner.Run(context.Background(), effectCopy)
			if message == nil {
				return nil
			}
			return effectResultMsg{message: message}
		})
	}
	return tea.Batch(cmds...)
}

func (m *renderModel) View() string {
	screen := projection.Project(m.model, m.width, m.height)
	view := renderworkbench.Render(screen, m.width, m.height)
	if screen.Screen == app.ScreenTerminalPool {
		view = renderterminalpool.Render(screen, m.width, m.height)
	}
	if screen.Overlay.Kind != "" {
		overlayView := renderoverlay.Render(screen)
		if overlayView != "" {
			view += "\n" + overlayView
		}
	}
	return view
}
