package render

import (
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type VisibleRenderState struct {
	Workbench *workbench.VisibleWorkbench
	Runtime   *runtime.VisibleRuntime
}

func AdaptVisibleState(wb *workbench.Workbench, rt *runtime.Runtime) VisibleRenderState {
	state := VisibleRenderState{}
	if wb != nil {
		state.Workbench = wb.Visible()
	}
	if rt != nil {
		state.Runtime = rt.Visible()
	}
	return state
}
