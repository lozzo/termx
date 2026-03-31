package render

import (
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type VisibleRenderState struct {
	Workbench       *workbench.VisibleWorkbench
	Runtime         *VisibleRuntimeStateProxy
	Picker          *modal.PickerState
	WorkspacePicker *modal.WorkspacePickerState
	Help            *modal.HelpState
	Prompt          *modal.PromptState
	TermSize        TermSize
	Notice          string
	Error           string
	InputMode       string
}

type VisibleRuntimeStateProxy = runtime.VisibleRuntime

type TermSize struct {
	Width  int
	Height int
}

func AdaptVisibleState(wb *workbench.Workbench, rt *runtime.Runtime) VisibleRenderState {
	return AdaptVisibleStateWithSize(wb, rt, 0, 0)
}

func AdaptVisibleStateWithSize(wb *workbench.Workbench, rt *runtime.Runtime, bodyWidth, bodyHeight int) VisibleRenderState {
	state := VisibleRenderState{}
	if wb != nil {
		if bodyWidth > 0 && bodyHeight > 0 {
			state.Workbench = wb.VisibleWithSize(workbench.Rect{W: bodyWidth, H: bodyHeight})
		} else {
			state.Workbench = wb.Visible()
		}
	}
	if rt != nil {
		state.Runtime = rt.Visible()
	}
	return state
}

func AttachPicker(state VisibleRenderState, picker *modal.PickerState) VisibleRenderState {
	state.Picker = picker
	return state
}

func AttachWorkspacePicker(state VisibleRenderState, picker *modal.WorkspacePickerState) VisibleRenderState {
	state.WorkspacePicker = picker
	return state
}

func WithTermSize(state VisibleRenderState, width, height int) VisibleRenderState {
	state.TermSize = TermSize{Width: width, Height: height}
	return state
}

func WithStatus(state VisibleRenderState, notice, errText, inputMode string) VisibleRenderState {
	state.Notice = notice
	state.Error = errText
	state.InputMode = inputMode
	return state
}
