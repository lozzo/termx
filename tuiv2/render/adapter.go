package render

import (
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type VisibleRenderState struct {
	Workbench                 *workbench.VisibleWorkbench
	Runtime                   *VisibleRuntimeStateProxy
	Surface                   VisibleSurface
	Overlay                   VisibleOverlay
	TermSize                  TermSize
	Notice                    string
	Error                     string
	InputMode                 string
	OwnerConfirmPaneID        string
	EmptyPaneSelectionPaneID  string
	EmptyPaneSelectionIndex   int
	ExitedPaneSelectionPaneID string
	ExitedPaneSelectionIndex  int
}

type VisibleRuntimeStateProxy = runtime.VisibleRuntime

type VisibleSurfaceKind uint8

const (
	VisibleSurfaceWorkbench VisibleSurfaceKind = iota
	VisibleSurfaceTerminalPool
)

type VisibleSurface struct {
	Kind         VisibleSurfaceKind
	TerminalPool *modal.TerminalManagerState
}

type VisibleOverlayKind uint8

const (
	VisibleOverlayNone VisibleOverlayKind = iota
	VisibleOverlayPrompt
	VisibleOverlayPicker
	VisibleOverlayWorkspacePicker
	VisibleOverlayTerminalManager
	VisibleOverlayHelp
	VisibleOverlayFloatingOverview
)

type VisibleOverlay struct {
	Kind             VisibleOverlayKind
	Prompt           *modal.PromptState
	Picker           *modal.PickerState
	WorkspacePicker  *modal.WorkspacePickerState
	TerminalManager  *modal.TerminalManagerState
	Help             *modal.HelpState
	FloatingOverview *modal.FloatingOverviewState
}

type TermSize struct {
	Width  int
	Height int
}

func AdaptVisibleState(wb *workbench.Workbench, rt *runtime.Runtime) VisibleRenderState {
	return AdaptVisibleStateWithSize(wb, rt, 0, 0)
}

func AdaptVisibleStateWithSize(wb *workbench.Workbench, rt *runtime.Runtime, bodyWidth, bodyHeight int) VisibleRenderState {
	state := VisibleRenderState{
		Surface: VisibleSurface{Kind: VisibleSurfaceWorkbench},
		Overlay: VisibleOverlay{Kind: VisibleOverlayNone},
	}
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
	return WithOverlayPicker(state, picker)
}

func AttachWorkspacePicker(state VisibleRenderState, picker *modal.WorkspacePickerState) VisibleRenderState {
	return WithOverlayWorkspacePicker(state, picker)
}

func AttachTerminalManager(state VisibleRenderState, manager *modal.TerminalManagerState) VisibleRenderState {
	state.Overlay = VisibleOverlay{
		Kind:            VisibleOverlayTerminalManager,
		TerminalManager: manager,
	}
	return state
}

func AttachTerminalPool(state VisibleRenderState, pool *modal.TerminalManagerState) VisibleRenderState {
	if pool == nil {
		state.Surface = VisibleSurface{Kind: VisibleSurfaceWorkbench}
		return state
	}
	state.Surface = VisibleSurface{
		Kind:         VisibleSurfaceTerminalPool,
		TerminalPool: pool,
	}
	return state
}

func AttachModalHost(state VisibleRenderState, host *modal.ModalHost) VisibleRenderState {
	if host == nil || host.Session == nil {
		state.Overlay = VisibleOverlay{Kind: VisibleOverlayNone}
		return state
	}
	switch host.Session.Kind {
	case input.ModePicker:
		return AttachPicker(state, host.Picker)
	case input.ModeWorkspacePicker:
		return AttachWorkspacePicker(state, host.WorkspacePicker)
	case input.ModeHelp:
		return AttachHelp(state, host.Help)
	case input.ModePrompt:
		return AttachPrompt(state, host.Prompt)
	case input.ModeFloatingOverview:
		return AttachFloatingOverview(state, host.FloatingOverview)
	default:
		state.Overlay = VisibleOverlay{Kind: VisibleOverlayNone}
		return state
	}
}

func AttachHelp(state VisibleRenderState, help *modal.HelpState) VisibleRenderState {
	state.Overlay = VisibleOverlay{
		Kind: VisibleOverlayHelp,
		Help: help,
	}
	return state
}

func AttachPrompt(state VisibleRenderState, prompt *modal.PromptState) VisibleRenderState {
	state.Overlay = VisibleOverlay{
		Kind:   VisibleOverlayPrompt,
		Prompt: prompt,
	}
	return state
}

func AttachFloatingOverview(state VisibleRenderState, overview *modal.FloatingOverviewState) VisibleRenderState {
	state.Overlay = VisibleOverlay{
		Kind:             VisibleOverlayFloatingOverview,
		FloatingOverview: overview,
	}
	return state
}

func WithOverlayPicker(state VisibleRenderState, picker *modal.PickerState) VisibleRenderState {
	state.Overlay = VisibleOverlay{
		Kind:   VisibleOverlayPicker,
		Picker: picker,
	}
	return state
}

func WithOverlayWorkspacePicker(state VisibleRenderState, picker *modal.WorkspacePickerState) VisibleRenderState {
	state.Overlay = VisibleOverlay{
		Kind:            VisibleOverlayWorkspacePicker,
		WorkspacePicker: picker,
	}
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

func WithEmptyPaneSelection(state VisibleRenderState, paneID string, selected int) VisibleRenderState {
	state.EmptyPaneSelectionPaneID = paneID
	state.EmptyPaneSelectionIndex = selected
	return state
}

func WithExitedPaneSelection(state VisibleRenderState, paneID string, selected int) VisibleRenderState {
	state.ExitedPaneSelectionPaneID = paneID
	state.ExitedPaneSelectionIndex = selected
	return state
}
