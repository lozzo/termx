package types

import "time"

type WorkspaceID string
type TabID string
type PaneID string
type TerminalID string

type Rect struct {
	X int
	Y int
	W int
	H int
}

type Direction string

const (
	DirectionLeft  Direction = "left"
	DirectionRight Direction = "right"
	DirectionUp    Direction = "up"
	DirectionDown  Direction = "down"
)

type SplitDirection string

const (
	SplitDirectionHorizontal SplitDirection = "horizontal"
	SplitDirectionVertical   SplitDirection = "vertical"
)

type PaneKind string

const (
	PaneKindTiled    PaneKind = "tiled"
	PaneKindFloating PaneKind = "floating"
)

type PaneSlotState string

const (
	PaneSlotConnected PaneSlotState = "connected"
	PaneSlotEmpty     PaneSlotState = "empty"
	PaneSlotExited    PaneSlotState = "exited"
	PaneSlotWaiting   PaneSlotState = "waiting"
)

type FocusLayer string

const (
	FocusLayerTiled    FocusLayer = "tiled"
	FocusLayerFloating FocusLayer = "floating"
	FocusLayerOverlay  FocusLayer = "overlay"
	FocusLayerPrompt   FocusLayer = "prompt"
)

type TerminalRunState string

const (
	TerminalRunStateRunning TerminalRunState = "running"
	TerminalRunStateExited  TerminalRunState = "exited"
	TerminalRunStateStopped TerminalRunState = "stopped"
)

type PaneState struct {
	ID           PaneID
	Kind         PaneKind
	Rect         Rect
	TitleHint    string
	TerminalID   TerminalID
	SlotState    PaneSlotState
	LastExitCode *int
}

type SplitNode struct {
	PaneID    PaneID
	Direction SplitDirection
	Ratio     float64
	First     *SplitNode
	Second    *SplitNode
}

type TabState struct {
	ID               TabID
	Name             string
	RootSplit        *SplitNode
	Panes            map[PaneID]PaneState
	FloatingOrder    []PaneID
	ActivePaneID     PaneID
	ActiveLayer      FocusLayer
	AutoAcquireOwner bool
}

type WorkspaceState struct {
	ID          WorkspaceID
	Name        string
	Tabs        map[TabID]TabState
	TabOrder    []TabID
	ActiveTabID TabID
}

type TerminalRef struct {
	ID       TerminalID
	Name     string
	Command  []string
	Tags     map[string]string
	State    TerminalRunState
	ExitCode *int
	Visible  bool
}

type FocusState struct {
	Layer         FocusLayer
	WorkspaceID   WorkspaceID
	TabID         TabID
	PaneID        PaneID
	OverlayTarget OverlayKind
}

type UIState struct {
	Focus   FocusState
	Overlay OverlayState
	Mode    ModeState
}

type DomainState struct {
	ActiveWorkspaceID WorkspaceID
	Workspaces        map[WorkspaceID]WorkspaceState
	WorkspaceOrder    []WorkspaceID
	Terminals         map[TerminalID]TerminalRef
	Connections       map[TerminalID]ConnectionState
}

type AppState struct {
	Domain DomainState
	UI     UIState
}

type AutoAcquirePolicy string

const (
	AutoAcquireDisabled AutoAcquirePolicy = "disabled"
	AutoAcquireTabEnter AutoAcquirePolicy = "tab_enter"
)

type ConnectionState struct {
	TerminalID        TerminalID
	ConnectedPaneIDs  []PaneID
	OwnerPaneID       PaneID
	AutoAcquirePolicy AutoAcquirePolicy
}

type OverlayKind string

const (
	OverlayNone            OverlayKind = "none"
	OverlayLayoutResolve   OverlayKind = "layout_resolve"
	OverlayTerminalPicker  OverlayKind = "terminal_picker"
	OverlayTerminalManager OverlayKind = "terminal_manager"
	OverlayWorkspacePicker OverlayKind = "workspace_picker"
	OverlayHelp            OverlayKind = "help"
	OverlayPrompt          OverlayKind = "prompt"
	OverlayConfirm         OverlayKind = "confirm"
)

// OverlayData 用接口隔离各类 overlay 自身状态，避免 UIState 直接依赖具体实现包。
type OverlayData interface {
	OverlayKind() OverlayKind
	CloneOverlayData() OverlayData
}

type OverlayState struct {
	Kind        OverlayKind
	Data        OverlayData
	ReturnFocus FocusState
}

type ModeKind string

const (
	ModeNone      ModeKind = "none"
	ModePane      ModeKind = "pane"
	ModeResize    ModeKind = "resize"
	ModeTab       ModeKind = "tab"
	ModeWorkspace ModeKind = "workspace"
	ModeFloating  ModeKind = "floating"
	ModePicker    ModeKind = "picker"
	ModeGlobal    ModeKind = "global"
)

type ModeState struct {
	Active     ModeKind
	Sticky     bool
	DeadlineAt *time.Time
}
