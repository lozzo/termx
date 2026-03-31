package app

import (
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/bootstrap"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/persist"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type Model struct {
	cfg       shared.Config
	statePath string
	width     int
	height    int
	quitting  bool
	err       error
	errShown  bool

	startup bootstrap.StartupResult

	send func(tea.Msg) // injected by run.go after tea.NewProgram

	input        *input.Router
	render       *render.Coordinator
	modalHost    *modal.ModalHost
	orchestrator *orchestrator.Orchestrator

	// 只读引用，仅用于将 visible state 注入 render 层。
	// 业务编排走 orchestrator，不直接通过这两个字段。
	workbench *workbench.Workbench
	runtime   *runtime.Runtime
}

func New(cfg shared.Config, wb *workbench.Workbench, rt *runtime.Runtime) *Model {
	if wb == nil {
		wb = workbench.NewWorkbench()
	}
	if rt == nil {
		rt = runtime.New(nil)
	}
	host := modal.NewHost()
	model := &Model{
		cfg:       cfg,
		statePath: cfg.WorkspaceStatePath,
		input:     input.NewRouter(),
		modalHost: host,
		workbench: wb,
		runtime:   rt,
	}
	model.orchestrator = orchestrator.New(model.workbench, model.runtime, model.modalHost)
	model.render = render.NewCoordinator(func() render.VisibleRenderState {
		bodyHeight := maxInt(1, model.height-2) // tab bar + status bar = 2 rows
		state := render.AdaptVisibleStateWithSize(model.workbench, model.runtime, model.width, bodyHeight)
		state = render.WithTermSize(state, model.width, model.height)
		state = render.WithStatus(state, "", renderErrorText(model.err), string(model.input.Mode().Kind))
		if model.modalHost != nil && model.modalHost.Session != nil && model.modalHost.Session.Kind == input.ModePicker {
			state = render.AttachPicker(state, model.modalHost.Picker)
		}
		if model.modalHost != nil && model.modalHost.Session != nil && model.modalHost.Session.Kind == input.ModeWorkspacePicker {
			state = render.AttachWorkspacePicker(state, model.modalHost.WorkspacePicker)
		}
		if model.modalHost != nil && model.modalHost.Session != nil && model.modalHost.Session.Kind == input.ModeHelp {
			state.Help = model.modalHost.Help
		}
		if model.modalHost != nil && model.modalHost.Session != nil && model.modalHost.Session.Kind == input.ModePrompt {
			state.Prompt = model.modalHost.Prompt
		}
		return state
	})
	// Default invalidate: no-op until SetSendFunc is called by run.go.
	if model.runtime != nil {
		model.runtime.SetInvalidate(func() {
			if model.send != nil {
				model.send(InvalidateMsg{})
			}
		})
	}
	return model
}

// SetSendFunc wires p.Send into the model so that the runtime stream goroutine
// can trigger a bubbletea redraw via InvalidateMsg. Must be called before p.Run().
func (m *Model) SetSendFunc(send func(tea.Msg)) {
	if m == nil {
		return
	}
	m.send = send
	if m.runtime != nil {
		m.runtime.SetInvalidate(func() {
			send(InvalidateMsg{})
		})
	}
}

func (m *Model) bootstrapStartup() error {
	if m == nil || m.workbench == nil {
		return nil
	}
	if m.workbench.CurrentWorkspace() != nil {
		return nil
	}
	var data []byte
	if m.cfg.WorkspaceStatePath != "" {
		data, _ = os.ReadFile(m.cfg.WorkspaceStatePath)
	}
	result, err := bootstrap.RestoreOrStartup(data, bootstrap.Config{}, m.workbench, m.runtime)
	if err != nil {
		return err
	}
	m.startup = result
	if result.ShouldOpenPicker {
		m.modalHost.Open(input.ModePicker, "startup-picker")
		m.modalHost.Picker = &modal.PickerState{}
		m.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "startup-picker"})
	}
	return nil
}

func (m *Model) saveStateCmd() tea.Cmd {
	if m == nil || m.statePath == "" || m.workbench == nil {
		return nil
	}
	wb := m.workbench
	path := m.statePath
	return func() tea.Msg {
		data, err := persist.Save(wb)
		if err != nil {
			return nil
		}
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, data, 0o644)
		return nil
	}
}
