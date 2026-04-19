package orchestrator

import "github.com/lozzow/termx/tuiv2/input"

type Effect interface {
	effectTag()
}

type OpenPickerEffect struct {
	RequestID string
}

func (OpenPickerEffect) effectTag() {}

type LoadPickerItemsEffect struct{}

func (LoadPickerItemsEffect) effectTag() {}

type OpenWorkspacePickerEffect struct {
	RequestID string
}

func (OpenWorkspacePickerEffect) effectTag() {}

type CloseModalEffect struct {
	Kind input.ModeKind
}

func (CloseModalEffect) effectTag() {}

type LoadWorkspaceItemsEffect struct{}

func (LoadWorkspaceItemsEffect) effectTag() {}

type SwitchWorkspaceEffect struct {
	Name string
}

func (SwitchWorkspaceEffect) effectTag() {}

type AttachTerminalEffect struct {
	PaneID     string
	TerminalID string
	Mode       string
}

func (AttachTerminalEffect) effectTag() {}

type LoadSnapshotEffect struct {
	TerminalID string
	Offset     int
	Limit      int
}

func (LoadSnapshotEffect) effectTag() {}

type SetInputModeEffect struct {
	Mode input.ModeState
}

func (SetInputModeEffect) effectTag() {}

type InvalidateRenderEffect struct{}

func (InvalidateRenderEffect) effectTag() {}

type ClosePaneEffect struct {
	PaneID string
}

func (ClosePaneEffect) effectTag() {}

type DetachPaneEffect struct {
	PaneID string
}

func (DetachPaneEffect) effectTag() {}

type ReconnectPaneEffect struct {
	PaneID string
}

func (ReconnectPaneEffect) effectTag() {}

type ResizePaneLayoutEffect struct {
	PaneID string
	Kind   input.ActionKind
	Delta  float64
}

func (ResizePaneLayoutEffect) effectTag() {}

type BalancePanesEffect struct{}

func (BalancePanesEffect) effectTag() {}

type CycleLayoutEffect struct{}

func (CycleLayoutEffect) effectTag() {}

type MoveFloatingPaneEffect struct {
	PaneID string
	Kind   input.ActionKind
}

func (MoveFloatingPaneEffect) effectTag() {}

type ResizeFloatingPaneEffect struct {
	PaneID string
	Kind   input.ActionKind
}

func (ResizeFloatingPaneEffect) effectTag() {}

type CenterFloatingPaneEffect struct {
	PaneID string
}

func (CenterFloatingPaneEffect) effectTag() {}

type CreateTabEffect struct{}

func (CreateTabEffect) effectTag() {}

type SwitchTabEffect struct {
	Delta int
}

func (SwitchTabEffect) effectTag() {}

type CloseTabEffect struct {
	TabID string
}

func (CloseTabEffect) effectTag() {}

type KillTerminalEffect struct {
	TerminalID string
}

func (KillTerminalEffect) effectTag() {}
