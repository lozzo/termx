package render

import (
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type RenderVM struct {
	Workbench *workbench.VisibleWorkbench
	Runtime   *VisibleRuntimeStateProxy
	Surface   RenderSurfaceVM
	Overlay   RenderOverlayVM
	TermSize  TermSize
	Chrome    UIChromeConfig
	Theme     UIThemeConfig
	Status    RenderStatusVM
	Body      RenderBodyVM
}

type RenderSurfaceVM struct {
	Kind         VisibleSurfaceKind
	TerminalPool *modal.TerminalManagerState
}

type RenderOverlayVM struct {
	Kind             VisibleOverlayKind
	Prompt           *modal.PromptState
	Picker           *modal.PickerState
	WorkspacePicker  *modal.WorkspacePickerState
	Help             *modal.HelpState
	FloatingOverview *modal.FloatingOverviewState
}

type RenderStatusVM struct {
	Notice      string
	Error       string
	InputMode   string
	Hints       []string
	RightTokens []RenderStatusToken
}

type RenderStatusToken struct {
	Kind   HitRegionKind
	Label  string
	Action input.SemanticAction
}

type RenderBodyVM struct {
	OwnerConfirmPaneID string
	EmptySelection     RenderPaneSelectionVM
	ExitedSelection    RenderPaneSelectionVM
	SnapshotOverride   RenderSnapshotOverrideVM
	CopyMode           RenderCopyModeVM
	FloatingDragPreview RenderFloatingDragPreviewVM
}

type RenderPaneSelectionVM struct {
	PaneID string
	Index  int
}

type RenderSnapshotOverrideVM struct {
	PaneID   string
	Snapshot *protocol.Snapshot
}

type RenderCopyModeVM struct {
	PaneID     string
	CursorRow  int
	CursorCol  int
	ViewTopRow int
	MarkSet    bool
	MarkRow    int
	MarkCol    int
	Snapshot   *protocol.Snapshot
}

type RenderFloatingDragPreviewVM struct {
	PaneID   string
	Rect     workbench.Rect
	Snapshot *protocol.Snapshot
}

func AdaptRenderVMWithSize(wb *workbench.Workbench, rt *runtime.Runtime, bodyWidth, bodyHeight int) RenderVM {
	vm := RenderVM{
		Surface: RenderSurfaceVM{Kind: VisibleSurfaceWorkbench},
		Overlay: RenderOverlayVM{Kind: VisibleOverlayNone},
		Chrome:  DefaultUIChromeConfig(),
	}
	if wb != nil {
		if bodyWidth > 0 && bodyHeight > 0 {
			vm.Workbench = wb.VisibleWithSize(workbench.Rect{W: bodyWidth, H: bodyHeight})
		} else {
			vm.Workbench = wb.Visible()
		}
	}
	if rt != nil {
		vm.Runtime = rt.Visible()
	}
	return vm
}

func WithRenderTermSize(vm RenderVM, width, height int) RenderVM {
	vm.TermSize = TermSize{Width: width, Height: height}
	return vm
}

func WithRenderChromeConfig(vm RenderVM, cfg UIChromeConfig) RenderVM {
	vm.Chrome = normalizeUIChromeConfig(cfg)
	return vm
}

func WithRenderThemeConfig(vm RenderVM, cfg UIThemeConfig) RenderVM {
	vm.Theme = cfg
	return vm
}

func WithRenderStatus(vm RenderVM, notice, errText, inputMode string) RenderVM {
	vm.Status.Notice = notice
	vm.Status.Error = errText
	vm.Status.InputMode = inputMode
	return vm
}

func WithRenderStatusHints(vm RenderVM, hints []string) RenderVM {
	if len(hints) == 0 {
		vm.Status.Hints = nil
		return vm
	}
	vm.Status.Hints = append([]string(nil), hints...)
	return vm
}

func WithRenderStatusRightTokens(vm RenderVM, tokens []RenderStatusToken) RenderVM {
	if len(tokens) == 0 {
		vm.Status.RightTokens = nil
		return vm
	}
	vm.Status.RightTokens = append([]RenderStatusToken(nil), tokens...)
	return vm
}

func WithRenderEmptyPaneSelection(vm RenderVM, paneID string, selected int) RenderVM {
	vm.Body.EmptySelection = RenderPaneSelectionVM{PaneID: paneID, Index: selected}
	return vm
}

func WithRenderExitedPaneSelection(vm RenderVM, paneID string, selected int) RenderVM {
	vm.Body.ExitedSelection = RenderPaneSelectionVM{PaneID: paneID, Index: selected}
	return vm
}

func WithRenderPaneSnapshotOverride(vm RenderVM, paneID string, snapshot *protocol.Snapshot) RenderVM {
	vm.Body.SnapshotOverride = RenderSnapshotOverrideVM{PaneID: paneID, Snapshot: snapshot}
	return vm
}

func WithRenderCopyMode(vm RenderVM, copyMode RenderCopyModeVM) RenderVM {
	vm.Body.CopyMode = copyMode
	return vm
}

func WithRenderFloatingDragPreview(vm RenderVM, paneID string, rect workbench.Rect, snapshot *protocol.Snapshot) RenderVM {
	vm.Body.FloatingDragPreview = RenderFloatingDragPreviewVM{PaneID: paneID, Rect: rect, Snapshot: snapshot}
	return vm
}

func AttachRenderTerminalPool(vm RenderVM, pool *modal.TerminalManagerState) RenderVM {
	if pool == nil {
		vm.Surface = RenderSurfaceVM{Kind: VisibleSurfaceWorkbench}
		return vm
	}
	vm.Surface = RenderSurfaceVM{
		Kind:         VisibleSurfaceTerminalPool,
		TerminalPool: pool,
	}
	return vm
}

func AttachRenderModalHost(vm RenderVM, host *modal.ModalHost) RenderVM {
	if host == nil || host.Session == nil {
		vm.Overlay = RenderOverlayVM{Kind: VisibleOverlayNone}
		return vm
	}
	switch host.Session.Kind {
	case input.ModePicker:
		return AttachRenderPicker(vm, host.Picker)
	case input.ModeWorkspacePicker:
		return AttachRenderWorkspacePicker(vm, host.WorkspacePicker)
	case input.ModeHelp:
		return AttachRenderHelp(vm, host.Help)
	case input.ModePrompt:
		return AttachRenderPrompt(vm, host.Prompt)
	case input.ModeFloatingOverview:
		return AttachRenderFloatingOverview(vm, host.FloatingOverview)
	default:
		vm.Overlay = RenderOverlayVM{Kind: VisibleOverlayNone}
		return vm
	}
}

func AttachRenderPicker(vm RenderVM, picker *modal.PickerState) RenderVM {
	vm.Overlay = RenderOverlayVM{
		Kind:   VisibleOverlayPicker,
		Picker: picker,
	}
	return vm
}

func AttachRenderWorkspacePicker(vm RenderVM, picker *modal.WorkspacePickerState) RenderVM {
	vm.Overlay = RenderOverlayVM{
		Kind:            VisibleOverlayWorkspacePicker,
		WorkspacePicker: picker,
	}
	return vm
}

func AttachRenderHelp(vm RenderVM, help *modal.HelpState) RenderVM {
	vm.Overlay = RenderOverlayVM{
		Kind: VisibleOverlayHelp,
		Help: help,
	}
	return vm
}

func AttachRenderPrompt(vm RenderVM, prompt *modal.PromptState) RenderVM {
	vm.Overlay = RenderOverlayVM{
		Kind:   VisibleOverlayPrompt,
		Prompt: prompt,
	}
	return vm
}

func AttachRenderFloatingOverview(vm RenderVM, overview *modal.FloatingOverviewState) RenderVM {
	vm.Overlay = RenderOverlayVM{
		Kind:             VisibleOverlayFloatingOverview,
		FloatingOverview: overview,
	}
	return vm
}

func RenderVMFromVisibleState(state VisibleRenderState) RenderVM {
	vm := RenderVM{
		Workbench: state.Workbench,
		Runtime:   state.Runtime,
		Surface: RenderSurfaceVM{
			Kind:         state.Surface.Kind,
			TerminalPool: state.Surface.TerminalPool,
		},
		Overlay: RenderOverlayVM{
			Kind:             state.Overlay.Kind,
			Prompt:           state.Overlay.Prompt,
			Picker:           state.Overlay.Picker,
			WorkspacePicker:  state.Overlay.WorkspacePicker,
			Help:             state.Overlay.Help,
			FloatingOverview: state.Overlay.FloatingOverview,
		},
		TermSize: state.TermSize,
		Chrome:   normalizeUIChromeConfig(state.Chrome),
		Theme:    state.Theme,
		Status: RenderStatusVM{
			Notice:      state.Notice,
			Error:       state.Error,
			InputMode:   state.InputMode,
			Hints:       append([]string(nil), state.StatusHints...),
			RightTokens: append([]RenderStatusToken(nil), statusBarRightTokens(state)...),
		},
		Body: RenderBodyVM{
			OwnerConfirmPaneID: state.OwnerConfirmPaneID,
			EmptySelection: RenderPaneSelectionVM{
				PaneID: state.EmptyPaneSelectionPaneID,
				Index:  state.EmptyPaneSelectionIndex,
			},
			ExitedSelection: RenderPaneSelectionVM{
				PaneID: state.ExitedPaneSelectionPaneID,
				Index:  state.ExitedPaneSelectionIndex,
			},
			SnapshotOverride: RenderSnapshotOverrideVM{
				PaneID:   state.PaneSnapshotOverridePaneID,
				Snapshot: state.PaneSnapshotOverride,
			},
			CopyMode: RenderCopyModeVM{
				PaneID:     state.CopyModePaneID,
				CursorRow:  state.CopyModeCursorRow,
				CursorCol:  state.CopyModeCursorCol,
				ViewTopRow: state.CopyModeViewTopRow,
				MarkSet:    state.CopyModeMarkSet,
				MarkRow:    state.CopyModeMarkRow,
				MarkCol:    state.CopyModeMarkCol,
				Snapshot:   state.CopyModeSnapshot,
			},
		},
	}
	return vm
}

func VisibleStateFromRenderVM(vm RenderVM) VisibleRenderState {
	return VisibleRenderState{
		Workbench: vm.Workbench,
		Runtime:   vm.Runtime,
		Surface: VisibleSurface{
			Kind:         vm.Surface.Kind,
			TerminalPool: vm.Surface.TerminalPool,
		},
		Overlay: VisibleOverlay{
			Kind:             vm.Overlay.Kind,
			Prompt:           vm.Overlay.Prompt,
			Picker:           vm.Overlay.Picker,
			WorkspacePicker:  vm.Overlay.WorkspacePicker,
			Help:             vm.Overlay.Help,
			FloatingOverview: vm.Overlay.FloatingOverview,
		},
		TermSize:                   vm.TermSize,
		Chrome:                     normalizeUIChromeConfig(vm.Chrome),
		Theme:                      vm.Theme,
		Notice:                     vm.Status.Notice,
		Error:                      vm.Status.Error,
		InputMode:                  vm.Status.InputMode,
		StatusHints:                append([]string(nil), vm.Status.Hints...),
		OwnerConfirmPaneID:         vm.Body.OwnerConfirmPaneID,
		EmptyPaneSelectionPaneID:   vm.Body.EmptySelection.PaneID,
		EmptyPaneSelectionIndex:    vm.Body.EmptySelection.Index,
		ExitedPaneSelectionPaneID:  vm.Body.ExitedSelection.PaneID,
		ExitedPaneSelectionIndex:   vm.Body.ExitedSelection.Index,
		PaneSnapshotOverridePaneID: vm.Body.SnapshotOverride.PaneID,
		PaneSnapshotOverride:       vm.Body.SnapshotOverride.Snapshot,
		CopyModePaneID:             vm.Body.CopyMode.PaneID,
		CopyModeCursorRow:          vm.Body.CopyMode.CursorRow,
		CopyModeCursorCol:          vm.Body.CopyMode.CursorCol,
		CopyModeViewTopRow:         vm.Body.CopyMode.ViewTopRow,
		CopyModeMarkSet:            vm.Body.CopyMode.MarkSet,
		CopyModeMarkRow:            vm.Body.CopyMode.MarkRow,
		CopyModeMarkCol:            vm.Body.CopyMode.MarkCol,
		CopyModeSnapshot:           vm.Body.CopyMode.Snapshot,
	}
}
