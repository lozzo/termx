package state

const (
	DefaultWorkspaceID = "workspace-main"
	DefaultTabID       = "tab-main"
	DefaultPaneID      = "pane-main"
)

type PanelPresentation string

const (
	PanelPresentationCard      PanelPresentation = "card"
	PanelPresentationSplitLine PanelPresentation = "split-line"
)

type PaneKind string

const (
	PaneEmpty        PaneKind = "empty"
	PaneTerminalLive PaneKind = "terminal-live"
	PaneCopyHistory  PaneKind = "copy-history"
	PaneExited       PaneKind = "exited"
)

type SplitDirection string

const (
	SplitDirectionHorizontal SplitDirection = "horizontal"
	SplitDirectionVertical   SplitDirection = "vertical"
)

type OverlayKind string

const (
	OverlayNone           OverlayKind = ""
	OverlayTerminalPicker OverlayKind = "terminal-picker"
	OverlayTerminalPool   OverlayKind = "terminal-pool"
	OverlayWorkbenchTree  OverlayKind = "workbench-tree"
	OverlayPrompt         OverlayKind = "prompt"
	OverlayHelp           OverlayKind = "help"
)

type ToastSeverity string

const (
	ToastInfo    ToastSeverity = "info"
	ToastSuccess ToastSeverity = "success"
	ToastWarning ToastSeverity = "warning"
	ToastError   ToastSeverity = "error"
)

type InteractionMode string

const (
	InteractionModeNormal    InteractionMode = ""
	InteractionModePane      InteractionMode = "pane"
	InteractionModeResize    InteractionMode = "resize"
	InteractionModeGlobal    InteractionMode = "global"
	InteractionModeFloating  InteractionMode = "floating"
	InteractionModeTab       InteractionMode = "tab"
	InteractionModeWorkspace InteractionMode = "workspace"
)

type WorkbenchCommandAction string

const (
	WorkbenchCommandTabCreate         WorkbenchCommandAction = "tab.create"
	WorkbenchCommandTabSwitch         WorkbenchCommandAction = "tab.switch"
	WorkbenchCommandTabNext           WorkbenchCommandAction = "tab.next"
	WorkbenchCommandTabPrevious       WorkbenchCommandAction = "tab.previous"
	WorkbenchCommandTabRename         WorkbenchCommandAction = "tab.rename"
	WorkbenchCommandTabClose          WorkbenchCommandAction = "tab.close"
	WorkbenchCommandTabKill           WorkbenchCommandAction = "tab.kill"
	WorkbenchCommandWorkspaceCreate   WorkbenchCommandAction = "workspace.create"
	WorkbenchCommandWorkspaceSwitch   WorkbenchCommandAction = "workspace.switch"
	WorkbenchCommandWorkspaceNext     WorkbenchCommandAction = "workspace.next"
	WorkbenchCommandWorkspacePrevious WorkbenchCommandAction = "workspace.previous"
	WorkbenchCommandWorkspaceRename   WorkbenchCommandAction = "workspace.rename"
	WorkbenchCommandWorkspaceDelete   WorkbenchCommandAction = "workspace.delete"
	WorkbenchCommandPaneSplit         WorkbenchCommandAction = "pane.split"
	WorkbenchCommandPaneRename        WorkbenchCommandAction = "pane.rename"
	WorkbenchCommandPaneDetach        WorkbenchCommandAction = "pane.detach"
	WorkbenchCommandPaneClose         WorkbenchCommandAction = "pane.close"
	WorkbenchCommandPaneKill          WorkbenchCommandAction = "pane.kill"
)

type WorkbenchCommand struct {
	Action   WorkbenchCommandAction
	TargetID string
	Target   PaneCommandTarget
	Name     string
	Pane     PaneCommand
	Source   PaneCommandSource
	Confirm  PaneConfirmPolicy
}

type WorkbenchCommandStatus string

const (
	WorkbenchCommandOK                WorkbenchCommandStatus = "ok"
	WorkbenchCommandNeedsConfirmation WorkbenchCommandStatus = "needs-confirmation"
	WorkbenchCommandInvalid           WorkbenchCommandStatus = "invalid"
)

type WorkbenchCommandResult struct {
	Status WorkbenchCommandStatus
	Action WorkbenchCommandAction
	Reason string
	ID     string
	Killed []string
}

// ShellStore 保存 Workbench 外壳相关的 reducer-owned 产品状态。
// 它只描述用户可操作的结构，不计算最终屏幕矩形，也不画 panel chrome。
type ShellStore struct {
	Workspace         WorkspaceState
	Workspaces        []WorkspaceState
	Floatings         []FloatingPaneState
	ActiveFloatingID  string
	PanelPresentation PanelPresentation
	ActivePaneID      string
	ZoomedPaneID      string
	InteractionMode   InteractionMode
	HeaderVisible     bool
	FooterVisible     bool
	Overlay           OverlayState
	Toasts            []ToastState
	nextToastSeq      uint64
	nextFloatingSeq   uint64
	initialized       bool
}

type WorkspaceState struct {
	ID          string
	Name        string
	Tabs        []TabState
	ActiveTabID string
}

type TabState struct {
	ID           string
	Title        string
	Panes        []PaneState
	ActivePaneID string
	RootSplit    SplitNode
}

type PaneState struct {
	ID         string
	Title      string
	Kind       PaneKind
	TerminalID string
	Active     bool
}

type FloatingPaneState struct {
	ID        string
	Title     string
	Pane      PaneState
	Rect      FloatingRect
	Z         int
	Active    bool
	Collapsed bool
}

type FloatingRect struct {
	X int
	Y int
	W int
	H int
}

type SplitNode struct {
	PaneID    string
	Direction SplitDirection
	Children  []SplitNode
	// 几何 hint 属于 reducer-owned pane tree；renderer 只消费投影后的 SplitVM，不反写 state。
	Ratio       float64
	BiasCells   int
	FixedPaneID string
	FixedCols   int
	FixedRows   int
}

type OverlayState struct {
	Kind          OverlayKind
	Open          bool
	TargetID      string
	Query         string
	SelectedIndex int
	Prompt        PromptState
	HelpSection   string
}

type PromptState struct {
	Title              string
	Context            string
	TargetID           string
	TargetTabID        string
	TargetWorkspaceID  string
	Purpose            string
	Value              string
	Placeholder        string
	Destructive        bool
	ConfirmText        string
	Submitted          bool
	Canceled           bool
	LastResult         string
	Fields             []PromptFieldState
	ActiveField        int
	SuggestionFocused  bool
	SuggestionSelected int
	SuggestionOffset   int
	Command            []string
	Workdir            string
	Tags               map[string]string
	DefaultName        string
}

type PromptFieldState struct {
	Key             string
	Label           string
	Value           string
	Cursor          int
	Placeholder     string
	Required        bool
	SuggestionTitle string
	SuggestionItems []string
	SuggestionEmpty string
}

type TerminalPickerItem struct {
	PaneID     string
	Title      string
	Kind       PaneKind
	TerminalID string
	Location   string
	Active     bool
	Selected   bool
	FromPool   bool
	PoolState  string
	CreateNew  bool
}

type TerminalPoolPageItem struct {
	TerminalID string
	Title      string
	State      string
	CWD        string
	Tags       map[string]string
	Cols       int
	Rows       int
	Attached   bool
	Selected   bool
}

type WorkbenchTreeItem struct {
	Kind          string
	WorkspaceID   string
	WorkspaceName string
	TabID         string
	TabTitle      string
	PaneID        string
	PaneTitle     string
	PaneKind      PaneKind
	TerminalID    string
	Depth         int
	Active        bool
	Selected      bool
	Summary       string
}

type FloatingCommandAction string

const (
	FloatingCommandCreate         FloatingCommandAction = "floating.create"
	FloatingCommandFocusRaise     FloatingCommandAction = "floating.focus-raise"
	FloatingCommandDeactivate     FloatingCommandAction = "floating.deactivate"
	FloatingCommandClose          FloatingCommandAction = "floating.close"
	FloatingCommandCenter         FloatingCommandAction = "floating.center"
	FloatingCommandToggleCollapse FloatingCommandAction = "floating.toggle-collapse"
	FloatingCommandMove           FloatingCommandAction = "floating.move"
	FloatingCommandResize         FloatingCommandAction = "floating.resize"
)

type FloatingCommand struct {
	Action   FloatingCommandAction
	TargetID string
	Pane     PaneState
	Title    string
	Rect     FloatingRect
	DeltaX   int
	DeltaY   int
	DeltaW   int
	DeltaH   int
	BoundsW  int
	BoundsH  int
	Source   PaneCommandSource
}

type FloatingCommandStatus string

const (
	FloatingCommandOK      FloatingCommandStatus = "ok"
	FloatingCommandInvalid FloatingCommandStatus = "invalid"
)

type FloatingCommandResult struct {
	Status FloatingCommandStatus
	Action FloatingCommandAction
	Reason string
	ID     string
}

const (
	WorkbenchTreeKindWorkspace = "workspace"
	WorkbenchTreeKindTab       = "tab"
	WorkbenchTreeKindPane      = "pane"
	WorkbenchTreeKindFloating  = "floating"
)

type ToastState struct {
	ID                string
	Severity          ToastSeverity
	Title             string
	Body              string
	Pending           bool
	AgeTicks          uint64
	DismissAfterTicks uint64
}

type ToastSpec struct {
	ID                string
	Severity          ToastSeverity
	Title             string
	Body              string
	Pending           bool
	DismissAfterTicks uint64
}

const (
	defaultToastDismissTicks   uint64 = 3
	attentionToastDismissTicks uint64 = 6
	pendingToastDismissTicks   uint64 = 8
)

func DefaultShell() ShellStore {
	return (ShellStore{}).EnsureDefaults()
}

func (store ShellStore) EnsureDefaults() ShellStore {
	store.Workspace = cloneWorkspace(store.Workspace)
	store.Workspaces = cloneWorkspaces(store.Workspaces)
	store.Floatings = cloneFloatings(store.Floatings)
	store.Toasts = cloneToasts(store.Toasts)
	if !store.initialized {
		store.HeaderVisible = true
		store.FooterVisible = true
		store.initialized = true
	}
	if store.Workspace.ID == "" {
		store.Workspace.ID = DefaultWorkspaceID
	}
	if store.Workspace.Name == "" {
		store.Workspace.Name = "main"
	}
	if len(store.Workspace.Tabs) == 0 {
		store.Workspace.ActiveTabID = DefaultTabID
		store.Workspace.Tabs = []TabState{{
			ID:           DefaultTabID,
			Title:        "main",
			ActivePaneID: DefaultPaneID,
			Panes: []PaneState{{
				ID:     DefaultPaneID,
				Title:  "shell",
				Kind:   PaneTerminalLive,
				Active: true,
			}},
			RootSplit: SplitNode{PaneID: DefaultPaneID},
		}}
	}
	store.Workspace = store.Workspace.ensureDefaults()
	store.Workspaces = upsertWorkspace(ensureWorkspaceList(store.Workspaces, store.Workspace), store.Workspace)
	store.Workspace = store.Workspace.ensureTabDefaults()
	store.Workspace = store.Workspace.ensureActiveTab()
	if store.ActivePaneID == "" {
		store.ActivePaneID = store.activeTab().ActivePaneID
	}
	if store.PanelPresentation == "" {
		store.PanelPresentation = PanelPresentationCard
	}
	if store.ZoomedPaneID != "" && !store.hasPaneInActiveTab(store.ZoomedPaneID) {
		store.ZoomedPaneID = ""
	}
	store = store.ensureFloatingDefaults()
	store.Workspace = store.Workspace.ensureActive(store.ActivePaneID)
	store.Workspaces = upsertWorkspace(store.Workspaces, store.Workspace)
	return store
}

func (store ShellStore) ensureFloatingDefaults() ShellStore {
	if len(store.Floatings) == 0 {
		store.ActiveFloatingID = ""
		return store
	}
	activeFound := false
	for index := range store.Floatings {
		floating := &store.Floatings[index]
		if floating.ID == "" {
			continue
		}
		if floating.Title == "" {
			floating.Title = floating.ID
		}
		if floating.Pane.ID == "" {
			floating.Pane = PaneState{ID: floating.ID + "-pane", Title: floating.Title, Kind: PaneEmpty}
		}
		if floating.Pane.Title == "" {
			floating.Pane.Title = floating.Title
		}
		if floating.Pane.Kind == "" {
			floating.Pane.Kind = PaneEmpty
		}
		if floating.Rect.W <= 0 {
			floating.Rect.W = 40
		}
		if floating.Rect.H <= 0 {
			floating.Rect.H = 10
		}
		if floating.Z <= 0 {
			floating.Z = index + 1
		}
		if floating.ID == store.ActiveFloatingID {
			activeFound = true
		}
	}
	if store.ActiveFloatingID != "" && !activeFound {
		store.ActiveFloatingID = topFloatingID(store.Floatings)
	}
	for index := range store.Floatings {
		store.Floatings[index].Active = store.Floatings[index].ID == store.ActiveFloatingID
	}
	return store
}

func (store ShellStore) SetPanelPresentation(presentation PanelPresentation) ShellStore {
	if presentation != PanelPresentationCard && presentation != PanelPresentationSplitLine {
		return store
	}
	store.PanelPresentation = presentation
	return store.EnsureDefaults()
}

func (store ShellStore) TogglePanelPresentation() ShellStore {
	store = store.EnsureDefaults()
	if store.PanelPresentation == PanelPresentationSplitLine {
		store.PanelPresentation = PanelPresentationCard
	} else {
		store.PanelPresentation = PanelPresentationSplitLine
	}
	return store
}

func (store ShellStore) SetHeaderVisible(visible bool) ShellStore {
	store = store.EnsureDefaults()
	store.HeaderVisible = visible
	return store
}

func (store ShellStore) ToggleHeaderVisible() ShellStore {
	store = store.EnsureDefaults()
	store.HeaderVisible = !store.HeaderVisible
	return store
}

func (store ShellStore) SetFooterVisible(visible bool) ShellStore {
	store = store.EnsureDefaults()
	store.FooterVisible = visible
	return store
}

func (store ShellStore) ToggleFooterVisible() ShellStore {
	store = store.EnsureDefaults()
	store.FooterVisible = !store.FooterVisible
	return store
}

func (store ShellStore) SetInteractionMode(mode InteractionMode) ShellStore {
	store = store.EnsureDefaults()
	switch mode {
	case InteractionModeNormal, InteractionModePane, InteractionModeResize, InteractionModeGlobal, InteractionModeFloating, InteractionModeTab, InteractionModeWorkspace:
		store.InteractionMode = mode
	}
	return store
}
