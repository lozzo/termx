package runtime

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app"
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

func TestProgramRunnerFlushesWorkspaceSaveOnExit(t *testing.T) {
	runner := NewProgramRunner()
	saver := &recordingWorkspaceSaveScheduler{}
	model := WrapModelWithWorkspacePersistence(quitAfterInitModel{model: sampleWorkbenchStateForRuntimeTest()}, NewUpdateLoop(nil, saver))

	if err := runner.Run(model, strings.NewReader(""), io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(saver.flushed) != 1 {
		t.Fatalf("expected one flush on exit, got %d", len(saver.flushed))
	}
	if saver.flushed[0].Workspace == nil || saver.flushed[0].Workspace.ID != "restored" {
		t.Fatalf("expected flushed workspace to preserve restored state, got %#v", saver.flushed[0].Workspace)
	}
}

type quitAfterInitModel struct {
	model app.Model
}

func (m quitAfterInitModel) Init() tea.Cmd {
	return tea.Quit
}

func (m quitAfterInitModel) Update(tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m quitAfterInitModel) View() string {
	return ""
}

func (m quitAfterInitModel) AppModel() app.Model {
	return m.model
}
