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
	errorSeq  uint64
	ownerSeq  uint64

	startup bootstrap.StartupResult

	prefixSeq int // incremented each time a sticky mode is entered or a valid action fires

	send func(tea.Msg) // injected by run.go after tea.NewProgram

	input        *input.Router
	render       *render.Coordinator
	modalHost    *modal.ModalHost
	terminalPage *modal.TerminalManagerState
	orchestrator *orchestrator.Orchestrator

	// 只读引用，仅用于将 visible state 注入 render 层。
	// 业务编排走 orchestrator，不直接通过这两个字段。
	workbench *workbench.Workbench
	runtime   *runtime.Runtime

	// 鼠标拖动状态
	mouseDragPaneID  string
	mouseDragOffsetX int
	mouseDragOffsetY int
	mouseDragMode    mouseDragMode

	ownerConfirmPaneID string
}

type mouseDragMode int

const (
	mouseDragNone mouseDragMode = iota
	mouseDragMove
	mouseDragResize
)

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
	model.orchestrator = orchestrator.New(model.workbench, model.runtime)
	model.render = render.NewCoordinator(func() render.VisibleRenderState { return model.visibleRenderState() })
	// Default invalidate: no-op until SetSendFunc is called by run.go.
	if model.runtime != nil {
		model.runtime.SetInvalidate(func() {
			model.render.Invalidate()
			if model.send != nil {
				model.send(InvalidateMsg{})
			}
		})
		model.runtime.SetTitleChange(func(terminalID, title string) {
			if model.send != nil {
				model.send(terminalTitleMsg{TerminalID: terminalID, Title: title})
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
			m.render.Invalidate()
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
		m.resetPickerState()
		m.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: "startup-picker"})
	}
	return nil
}

func (m *Model) saveStateCmd() tea.Cmd {
	if m == nil || m.statePath == "" || m.workbench == nil {
		return nil
	}
	wb := m.workbench
	rt := m.runtime
	path := m.statePath
	return func() tea.Msg {
		if err := saveState(path, wb, rt); err != nil {
			return nil
		}
		return nil
	}
}

func saveState(path string, wb *workbench.Workbench, rt *runtime.Runtime) error {
	if path == "" || wb == nil {
		return nil
	}
	data, err := persist.Save(wb)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
