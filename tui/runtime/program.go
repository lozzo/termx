package runtime

import (
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
	model  app.Model
	router input.Router
	width  int
	height int
}

func NewProgramRunner() ProgramRunner {
	return BubbleTeaProgramRunner{}
}

func NewRenderModel(model app.Model) tea.Model {
	return &renderModel{model: model, router: input.NewRouter(), width: 80, height: 24}
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
	if key, ok := msg.(tea.KeyMsg); ok {
		if intent := m.router.Translate(input.Context{Screen: m.model.Screen}, key); intent != nil {
			msg = intent
		}
	}
	next, cmd := m.model.Update(msg)
	if typed, ok := next.(app.Model); ok {
		m.model = typed
	}
	return m, cmd
}

func (m *renderModel) View() string {
	screen := projection.Project(m.model, m.width, m.height)
	view := renderworkbench.Render(screen, m.width, m.height)
	if screen.Screen == app.ScreenTerminalPool {
		view = renderterminalpool.Render(screen, m.width, m.height)
	}
	if screen.OverlayKind != "" {
		overlayView := renderoverlay.Render(screen.OverlayKind)
		if overlayView != "" {
			view += "\n" + overlayView
		}
	}
	return view
}
