package app

import (
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type Model struct {
	cfg      shared.Config
	width    int
	height   int
	quitting bool
	err      error

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
		input:     input.NewRouter(),
		modalHost: host,
		workbench: wb,
		runtime:   rt,
	}
	model.orchestrator = orchestrator.New(model.workbench, model.runtime, model.modalHost)
	model.render = render.NewCoordinator(func() render.VisibleRenderState {
		return render.AdaptVisibleState(model.workbench, model.runtime)
	})
	return model
}
