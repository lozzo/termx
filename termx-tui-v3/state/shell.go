package state

import (
	"fmt"
	"strconv"
	"strings"
)

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
	Title             string
	Context           string
	TargetID          string
	TargetTabID       string
	TargetWorkspaceID string
	Purpose           string
	Value             string
	Placeholder       string
	Destructive       bool
	ConfirmText       string
	Submitted         bool
	Canceled          bool
	LastResult        string
}

type TerminalPickerItem struct {
	PaneID     string
	Title      string
	Kind       PaneKind
	TerminalID string
	Active     bool
	Selected   bool
	FromPool   bool
	PoolState  string
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

func (store ShellStore) ApplyFloatingCommand(command FloatingCommand) (ShellStore, FloatingCommandResult) {
	store = store.EnsureDefaults()
	if command.Source == "" {
		command.Source = PaneCommandSourceKeyboard
	}
	switch command.Action {
	case FloatingCommandCreate:
		return store.createFloating(command)
	case FloatingCommandFocusRaise:
		return store.focusRaiseFloating(command.TargetID, command.Action)
	case FloatingCommandDeactivate:
		return store.deactivateFloating(command.Action)
	case FloatingCommandClose:
		return store.closeFloating(command.TargetID)
	case FloatingCommandCenter:
		return store.centerFloating(command)
	case FloatingCommandToggleCollapse:
		return store.toggleCollapseFloating(command.TargetID)
	case FloatingCommandMove:
		return store.moveFloating(command)
	case FloatingCommandResize:
		return store.resizeFloating(command)
	default:
		return store, floatingCommandInvalid(command.Action, "unknown action")
	}
}

func (store ShellStore) ApplyWorkbenchCommand(command WorkbenchCommand) (ShellStore, WorkbenchCommandResult) {
	store = store.EnsureDefaults()
	if command.Source == "" {
		command.Source = PaneCommandSourceKeyboard
	}
	switch command.Action {
	case WorkbenchCommandTabCreate:
		return store.createTab(command)
	case WorkbenchCommandTabSwitch:
		return store.switchTab(command)
	case WorkbenchCommandTabNext:
		return store.switchRelativeTab(1, command.Action)
	case WorkbenchCommandTabPrevious:
		return store.switchRelativeTab(-1, command.Action)
	case WorkbenchCommandTabRename:
		return store.renameTab(command)
	case WorkbenchCommandTabClose:
		return store.closeTab(command)
	case WorkbenchCommandTabKill:
		return store.killTab(command)
	case WorkbenchCommandWorkspaceCreate:
		return store.createWorkspace(command)
	case WorkbenchCommandWorkspaceSwitch:
		return store.switchWorkspace(command.TargetID, command.Action)
	case WorkbenchCommandWorkspaceNext:
		return store.switchRelativeWorkspace(1, command.Action)
	case WorkbenchCommandWorkspacePrevious:
		return store.switchRelativeWorkspace(-1, command.Action)
	case WorkbenchCommandWorkspaceRename:
		return store.renameWorkspace(command)
	case WorkbenchCommandWorkspaceDelete:
		return store.deleteWorkspace(command)
	case WorkbenchCommandPaneSplit:
		return store.splitPaneWorkbench(command)
	case WorkbenchCommandPaneRename:
		return store.renamePane(command)
	case WorkbenchCommandPaneDetach:
		return store.detachPane(command)
	case WorkbenchCommandPaneClose:
		return store.closePaneWorkbench(command)
	case WorkbenchCommandPaneKill:
		return store.killPane(command)
	default:
		return store, workbenchCommandInvalid(command.Action, "unknown action")
	}
}

func (store ShellStore) createTab(command WorkbenchCommand) (ShellStore, WorkbenchCommandResult) {
	id := strings.TrimSpace(command.TargetID)
	if id == "" {
		id = nextTabID(store.Workspace)
	}
	if store.tabIndexByID(id) >= 0 {
		return store, workbenchCommandInvalid(command.Action, "tab already exists")
	}
	name := strings.TrimSpace(command.Name)
	if name == "" {
		name = id
	}
	paneID := id + "-pane"
	tab := TabState{
		ID:           id,
		Title:        name,
		ActivePaneID: paneID,
		Panes:        []PaneState{{ID: paneID, Title: "shell", Kind: PaneTerminalLive, Active: true}},
		RootSplit:    SplitNode{PaneID: paneID},
	}
	store.Workspace.Tabs = append(cloneTabs(store.Workspace.Tabs), tab)
	return store.focusTabByIndex(len(store.Workspace.Tabs) - 1), WorkbenchCommandResult{Status: WorkbenchCommandOK, Action: command.Action, ID: id}
}

func (store ShellStore) switchRelativeTab(offset int, action WorkbenchCommandAction) (ShellStore, WorkbenchCommandResult) {
	if len(store.Workspace.Tabs) == 0 {
		return store, workbenchCommandInvalid(action, "no tab")
	}
	index := store.activeTabIndex()
	if index < 0 {
		index = 0
	}
	next := (index + offset) % len(store.Workspace.Tabs)
	if next < 0 {
		next += len(store.Workspace.Tabs)
	}
	store = store.focusTabByIndex(next)
	return store, WorkbenchCommandResult{Status: WorkbenchCommandOK, Action: action, ID: store.Workspace.ActiveTabID}
}

func (store ShellStore) switchTab(command WorkbenchCommand) (ShellStore, WorkbenchCommandResult) {
	if len(store.Workspace.Tabs) == 0 {
		return store, workbenchCommandInvalid(command.Action, "no tab")
	}
	index := store.tabIndexByID(command.TargetID)
	if index < 0 {
		return store, workbenchCommandInvalid(command.Action, "tab not found")
	}
	store = store.focusTabByIndex(index)
	return store, WorkbenchCommandResult{Status: WorkbenchCommandOK, Action: command.Action, ID: store.Workspace.ActiveTabID}
}

func (store ShellStore) renameTab(command WorkbenchCommand) (ShellStore, WorkbenchCommandResult) {
	name := strings.TrimSpace(command.Name)
	if name == "" {
		return store, workbenchCommandInvalid(command.Action, "missing tab name")
	}
	index := store.activeTabIndex()
	if command.TargetID != "" {
		index = store.tabIndexByID(command.TargetID)
	}
	if index < 0 {
		return store, workbenchCommandInvalid(command.Action, "tab not found")
	}
	store.Workspace.Tabs[index].Title = name
	store.Workspaces = upsertWorkspace(store.Workspaces, store.Workspace)
	return store.EnsureDefaults(), WorkbenchCommandResult{Status: WorkbenchCommandOK, Action: command.Action, ID: store.Workspace.Tabs[index].ID}
}

func (store ShellStore) closeTab(command WorkbenchCommand) (ShellStore, WorkbenchCommandResult) {
	if len(store.Workspace.Tabs) <= 1 {
		return store, workbenchCommandInvalid(command.Action, "cannot close last tab")
	}
	index := store.activeTabIndex()
	if command.TargetID != "" {
		index = store.tabIndexByID(command.TargetID)
	}
	if index < 0 {
		return store, workbenchCommandInvalid(command.Action, "tab not found")
	}
	closedID := store.Workspace.Tabs[index].ID
	nextTabs := make([]TabState, 0, len(store.Workspace.Tabs)-1)
	for i, tab := range store.Workspace.Tabs {
		if i != index {
			nextTabs = append(nextTabs, tab)
		}
	}
	store.Workspace.Tabs = nextTabs
	if index >= len(nextTabs) {
		index = len(nextTabs) - 1
	}
	store = store.focusTabByIndex(index)
	store.Workspaces = upsertWorkspace(store.Workspaces, store.Workspace)
	return store, WorkbenchCommandResult{Status: WorkbenchCommandOK, Action: command.Action, ID: closedID}
}

func (store ShellStore) killTab(command WorkbenchCommand) (ShellStore, WorkbenchCommandResult) {
	if command.Confirm != PaneConfirmAccepted {
		return store, WorkbenchCommandResult{Status: WorkbenchCommandNeedsConfirmation, Action: command.Action, Reason: "confirm tab kill"}
	}
	index := store.activeTabIndex()
	if command.TargetID != "" {
		index = store.tabIndexByID(command.TargetID)
	}
	if index < 0 {
		return store, workbenchCommandInvalid(command.Action, "tab not found")
	}
	killed := terminalIDsForTab(store.Workspace.Tabs[index])
	next, result := store.closeTab(WorkbenchCommand{
		Action:   WorkbenchCommandTabClose,
		TargetID: command.TargetID,
		Source:   command.Source,
	})
	if result.Status != WorkbenchCommandOK {
		result.Action = command.Action
		result.Killed = nil
		return store, result
	}
	result.Action = command.Action
	result.Killed = killed
	return next, result
}

func (store ShellStore) createWorkspace(command WorkbenchCommand) (ShellStore, WorkbenchCommandResult) {
	id := strings.TrimSpace(command.TargetID)
	if id == "" {
		id = nextWorkspaceID(store.Workspaces)
	}
	if _, ok := workspaceByID(store.Workspaces, id); ok {
		return store, workbenchCommandInvalid(command.Action, "workspace already exists")
	}
	name := strings.TrimSpace(command.Name)
	if name == "" {
		name = id
	}
	paneID := id + "-pane"
	workspace := WorkspaceState{
		ID:          id,
		Name:        name,
		ActiveTabID: DefaultTabID,
		Tabs: []TabState{{
			ID:           DefaultTabID,
			Title:        "main",
			ActivePaneID: paneID,
			Panes:        []PaneState{{ID: paneID, Title: "shell", Kind: PaneTerminalLive, Active: true}},
			RootSplit:    SplitNode{PaneID: paneID},
		}},
	}
	store.Workspaces = upsertWorkspace(store.Workspaces, store.Workspace)
	store.Workspaces = appendWorkspaceIfMissing(store.Workspaces, workspace.ensureDefaults())
	return store.switchWorkspace(id, command.Action)
}

func (store ShellStore) switchRelativeWorkspace(offset int, action WorkbenchCommandAction) (ShellStore, WorkbenchCommandResult) {
	store.Workspaces = upsertWorkspace(ensureWorkspaceList(store.Workspaces, store.Workspace), store.Workspace)
	if len(store.Workspaces) == 0 {
		return store, workbenchCommandInvalid(action, "no workspace")
	}
	current := workspaceIndexByID(store.Workspaces, store.Workspace.ID)
	if current < 0 {
		current = 0
	}
	next := (current + offset) % len(store.Workspaces)
	if next < 0 {
		next += len(store.Workspaces)
	}
	return store.switchWorkspace(store.Workspaces[next].ID, action)
}

func (store ShellStore) renameWorkspace(command WorkbenchCommand) (ShellStore, WorkbenchCommandResult) {
	name := strings.TrimSpace(command.Name)
	if name == "" {
		return store, workbenchCommandInvalid(command.Action, "missing workspace name")
	}
	id := command.TargetID
	if id == "" {
		id = store.Workspace.ID
	}
	index := workspaceIndexByID(store.Workspaces, id)
	if index < 0 {
		return store, workbenchCommandInvalid(command.Action, "workspace not found")
	}
	store.Workspaces[index].Name = name
	if store.Workspace.ID == id {
		store.Workspace.Name = name
	}
	store.Workspaces = upsertWorkspace(store.Workspaces, store.Workspace)
	return store.EnsureDefaults(), WorkbenchCommandResult{Status: WorkbenchCommandOK, Action: command.Action, ID: id}
}

func (store ShellStore) deleteWorkspace(command WorkbenchCommand) (ShellStore, WorkbenchCommandResult) {
	if command.Confirm != PaneConfirmAccepted {
		return store, WorkbenchCommandResult{Status: WorkbenchCommandNeedsConfirmation, Action: command.Action, Reason: "confirm workspace delete"}
	}
	store.Workspaces = upsertWorkspace(ensureWorkspaceList(store.Workspaces, store.Workspace), store.Workspace)
	if len(store.Workspaces) <= 1 {
		return store, workbenchCommandInvalid(command.Action, "cannot delete last workspace")
	}
	id := strings.TrimSpace(command.TargetID)
	if id == "" {
		id = store.Workspace.ID
	}
	index := workspaceIndexByID(store.Workspaces, id)
	if index < 0 {
		return store, workbenchCommandInvalid(command.Action, "workspace not found")
	}
	deletedID := store.Workspaces[index].ID
	nextWorkspaces := make([]WorkspaceState, 0, len(store.Workspaces)-1)
	for i, workspace := range store.Workspaces {
		if i != index {
			nextWorkspaces = append(nextWorkspaces, workspace)
		}
	}
	store.Workspaces = nextWorkspaces
	if store.Workspace.ID == deletedID {
		if index >= len(nextWorkspaces) {
			index = len(nextWorkspaces) - 1
		}
		// 删除当前 workspace 时不能走 switchWorkspace：它会先把旧 active workspace upsert 回列表。
		next := cloneWorkspace(nextWorkspaces[index]).ensureDefaults()
		store.Workspace = next
		store.ActivePaneID = next.activeTab().ActivePaneID
		store.ZoomedPaneID = ""
		store.Workspace = store.Workspace.ensureActive(store.ActivePaneID)
		store.Workspaces = upsertWorkspace(nextWorkspaces, store.Workspace)
		return store.EnsureDefaults(), WorkbenchCommandResult{Status: WorkbenchCommandOK, Action: command.Action, ID: deletedID}
	}
	store.Workspaces = upsertWorkspace(store.Workspaces, store.Workspace)
	return store.EnsureDefaults(), WorkbenchCommandResult{Status: WorkbenchCommandOK, Action: command.Action, ID: deletedID}
}

func (store ShellStore) switchWorkspace(id string, action WorkbenchCommandAction) (ShellStore, WorkbenchCommandResult) {
	index := workspaceIndexByID(store.Workspaces, id)
	if index < 0 {
		return store, workbenchCommandInvalid(action, "workspace not found")
	}
	store.Workspaces = upsertWorkspace(store.Workspaces, store.Workspace)
	next := cloneWorkspace(store.Workspaces[index]).ensureDefaults()
	store.Workspace = next
	store.ActivePaneID = next.activeTab().ActivePaneID
	store.ZoomedPaneID = ""
	store.Workspace = store.Workspace.ensureActive(store.ActivePaneID)
	store.Workspaces = upsertWorkspace(store.Workspaces, store.Workspace)
	return store.EnsureDefaults(), WorkbenchCommandResult{Status: WorkbenchCommandOK, Action: action, ID: id}
}

func (store ShellStore) splitPaneWorkbench(command WorkbenchCommand) (ShellStore, WorkbenchCommandResult) {
	paneCommand := command.Pane
	paneCommand.Action = PaneCommandSplit
	if paneCommand.Source == "" {
		paneCommand.Source = command.Source
	}
	if paneCommand.Target.PaneID == "" || paneCommand.Target.TabID == "" || paneCommand.Target.WorkspaceID == "" {
		paneCommand.Target = store.workbenchPaneTarget(WorkbenchCommand{Target: paneCommand.Target, TargetID: command.TargetID})
	}
	next, result := store.ApplyPaneCommand(paneCommand)
	if result.Status != PaneCommandOK {
		return store, workbenchCommandInvalid(command.Action, result.Reason)
	}
	next.Workspaces = upsertWorkspace(next.Workspaces, next.Workspace)
	return next, WorkbenchCommandResult{Status: WorkbenchCommandOK, Action: command.Action, ID: paneCommand.NewPane.ID}
}

func (store ShellStore) renamePane(command WorkbenchCommand) (ShellStore, WorkbenchCommandResult) {
	name := strings.TrimSpace(command.Name)
	if name == "" {
		return store, workbenchCommandInvalid(command.Action, "missing pane name")
	}
	target := store.workbenchPaneTarget(command)
	tabIndex := store.tabIndexForTarget(target)
	if tabIndex < 0 {
		return store, workbenchCommandInvalid(command.Action, "pane not found")
	}
	paneID := target.PaneID
	if paneID == "" {
		paneID = store.ActivePaneID
	}
	for index := range store.Workspace.Tabs[tabIndex].Panes {
		if store.Workspace.Tabs[tabIndex].Panes[index].ID != paneID {
			continue
		}
		store.Workspace.Tabs[tabIndex].Panes[index].Title = name
		store.Workspace = store.Workspace.ensureActive(store.ActivePaneID)
		store.Workspaces = upsertWorkspace(store.Workspaces, store.Workspace)
		return store.EnsureDefaults(), WorkbenchCommandResult{Status: WorkbenchCommandOK, Action: command.Action, ID: paneID}
	}
	return store, workbenchCommandInvalid(command.Action, "pane not found")
}

func (store ShellStore) detachPane(command WorkbenchCommand) (ShellStore, WorkbenchCommandResult) {
	target := store.workbenchPaneTarget(command)
	pane, ok := store.Pane(target)
	if !ok {
		return store, workbenchCommandInvalid(command.Action, "pane not found")
	}
	// detach 只断开 workbench pane 与 terminal 的绑定，不销毁 daemon terminal。
	store = store.setPaneDetached(target)
	return store, WorkbenchCommandResult{Status: WorkbenchCommandOK, Action: command.Action, ID: pane.ID}
}

func (store ShellStore) closePaneWorkbench(command WorkbenchCommand) (ShellStore, WorkbenchCommandResult) {
	target := store.workbenchPaneTarget(command)
	return store.removePaneWorkbench(command.Action, target, nil)
}

func (store ShellStore) killPane(command WorkbenchCommand) (ShellStore, WorkbenchCommandResult) {
	if command.Confirm != PaneConfirmAccepted {
		return store, WorkbenchCommandResult{Status: WorkbenchCommandNeedsConfirmation, Action: command.Action, Reason: "confirm pane kill"}
	}
	target := store.workbenchPaneTarget(command)
	pane, ok := store.Pane(target)
	if !ok {
		return store, workbenchCommandInvalid(command.Action, "pane not found")
	}
	killed := []string{}
	if pane.TerminalID != "" {
		killed = []string{pane.TerminalID}
	}
	return store.removePaneWorkbench(command.Action, target, killed)
}

func (store ShellStore) focusTabByIndex(index int) ShellStore {
	if index < 0 || index >= len(store.Workspace.Tabs) {
		return store.EnsureDefaults()
	}
	tab := store.Workspace.Tabs[index]
	store.Workspace.ActiveTabID = tab.ID
	if tab.ActivePaneID == "" && len(tab.Panes) > 0 {
		tab.ActivePaneID = tab.Panes[0].ID
		store.Workspace.Tabs[index] = tab
	}
	store.ActivePaneID = tab.ActivePaneID
	store.ZoomedPaneID = ""
	store.Workspace = store.Workspace.ensureActive(store.ActivePaneID)
	store.Workspaces = upsertWorkspace(store.Workspaces, store.Workspace)
	return store.EnsureDefaults()
}

func (store ShellStore) createFloating(command FloatingCommand) (ShellStore, FloatingCommandResult) {
	pane := command.Pane
	if pane.ID == "" {
		store.nextFloatingSeq++
		pane.ID = formatFloatingID(store.nextFloatingSeq) + "-pane"
	}
	if pane.Title == "" {
		pane.Title = "floating"
	}
	if pane.Kind == "" {
		pane.Kind = PaneEmpty
	}
	id := command.TargetID
	if id == "" {
		id = strings.TrimSuffix(pane.ID, "-pane")
		if id == "" || id == pane.ID {
			store.nextFloatingSeq++
			id = formatFloatingID(store.nextFloatingSeq)
		}
	}
	if store.floatingIndex(id) >= 0 {
		return store, floatingCommandInvalid(command.Action, "floating already exists")
	}
	rect := command.Rect
	if rect.W <= 0 {
		rect.W = 44
	}
	if rect.H <= 0 {
		rect.H = 12
	}
	if rect.X == 0 && rect.Y == 0 && command.BoundsW > 0 && command.BoundsH > 0 {
		rect = centerFloatingRect(rect, command.BoundsW, command.BoundsH)
	}
	rect = clampFloatingRect(rect, command.BoundsW, command.BoundsH)
	floating := FloatingPaneState{
		ID:     id,
		Title:  floatingTitle(command.Title, pane),
		Pane:   pane,
		Rect:   rect,
		Z:      store.nextFloatingZ() + 1,
		Active: true,
	}
	store.Floatings = append(cloneFloatings(store.Floatings), floating)
	store.ActiveFloatingID = id
	store = store.ensureFloatingDefaults()
	return store, FloatingCommandResult{Status: FloatingCommandOK, Action: command.Action, ID: id}
}

func (store ShellStore) focusRaiseFloating(id string, action FloatingCommandAction) (ShellStore, FloatingCommandResult) {
	index := store.floatingIndexOrActive(id)
	if index < 0 {
		return store, floatingCommandInvalid(action, "floating not found")
	}
	id = store.Floatings[index].ID
	store.Floatings[index].Z = store.nextFloatingZ() + 1
	store.ActiveFloatingID = id
	store = store.ensureFloatingDefaults()
	return store, FloatingCommandResult{Status: FloatingCommandOK, Action: action, ID: id}
}

func (store ShellStore) deactivateFloating(action FloatingCommandAction) (ShellStore, FloatingCommandResult) {
	if store.ActiveFloatingID == "" {
		return store, FloatingCommandResult{Status: FloatingCommandOK, Action: action}
	}
	store.ActiveFloatingID = ""
	store = store.ensureFloatingDefaults()
	return store, FloatingCommandResult{Status: FloatingCommandOK, Action: action}
}

func (store ShellStore) closeFloating(id string) (ShellStore, FloatingCommandResult) {
	index := store.floatingIndexOrActive(id)
	if index < 0 {
		return store, floatingCommandInvalid(FloatingCommandClose, "floating not found")
	}
	id = store.Floatings[index].ID
	next := make([]FloatingPaneState, 0, len(store.Floatings)-1)
	for i, floating := range store.Floatings {
		if i != index {
			next = append(next, floating)
		}
	}
	store.Floatings = next
	store.ActiveFloatingID = topFloatingID(store.Floatings)
	store = store.ensureFloatingDefaults()
	return store, FloatingCommandResult{Status: FloatingCommandOK, Action: FloatingCommandClose, ID: id}
}

func (store ShellStore) centerFloating(command FloatingCommand) (ShellStore, FloatingCommandResult) {
	index := store.floatingIndexOrActive(command.TargetID)
	if index < 0 {
		return store, floatingCommandInvalid(command.Action, "floating not found")
	}
	rect := store.Floatings[index].Rect
	store.Floatings[index].Rect = centerFloatingRect(rect, command.BoundsW, command.BoundsH)
	return store.focusRaiseFloating(store.Floatings[index].ID, command.Action)
}

func (store ShellStore) toggleCollapseFloating(id string) (ShellStore, FloatingCommandResult) {
	index := store.floatingIndexOrActive(id)
	if index < 0 {
		return store, floatingCommandInvalid(FloatingCommandToggleCollapse, "floating not found")
	}
	store.Floatings[index].Collapsed = !store.Floatings[index].Collapsed
	return store.focusRaiseFloating(store.Floatings[index].ID, FloatingCommandToggleCollapse)
}

func (store ShellStore) moveFloating(command FloatingCommand) (ShellStore, FloatingCommandResult) {
	index := store.floatingIndexOrActive(command.TargetID)
	if index < 0 {
		return store, floatingCommandInvalid(command.Action, "floating not found")
	}
	rect := store.Floatings[index].Rect
	rect.X += command.DeltaX
	rect.Y += command.DeltaY
	store.Floatings[index].Rect = clampFloatingRect(rect, command.BoundsW, command.BoundsH)
	return store.focusRaiseFloating(store.Floatings[index].ID, command.Action)
}

func (store ShellStore) resizeFloating(command FloatingCommand) (ShellStore, FloatingCommandResult) {
	index := store.floatingIndexOrActive(command.TargetID)
	if index < 0 {
		return store, floatingCommandInvalid(command.Action, "floating not found")
	}
	rect := store.Floatings[index].Rect
	rect.W += command.DeltaW
	rect.H += command.DeltaH
	store.Floatings[index].Rect = clampFloatingRect(rect, command.BoundsW, command.BoundsH)
	return store.focusRaiseFloating(store.Floatings[index].ID, command.Action)
}

func (store ShellStore) ExitInteractionMode() ShellStore {
	store = store.EnsureDefaults()
	store.InteractionMode = InteractionModeNormal
	return store
}

func (store ShellStore) AddToast(spec ToastSpec) ShellStore {
	store = store.EnsureDefaults()
	if spec.Severity == "" {
		spec.Severity = ToastInfo
	}
	dismissAfterTicks := spec.DismissAfterTicks
	if dismissAfterTicks == 0 {
		dismissAfterTicks = defaultToastDismissAfterTicks(spec)
	}
	if index := store.findMatchingToast(spec); index >= 0 {
		// 同内容 toast 只刷新生命周期并移到当前 toast，避免拖动等连续操作刷屏。
		toasts := cloneToasts(store.Toasts)
		toast := ToastState{
			ID:                toasts[index].ID,
			Severity:          spec.Severity,
			Title:             spec.Title,
			Body:              spec.Body,
			Pending:           spec.Pending,
			DismissAfterTicks: dismissAfterTicks,
		}
		toasts = append(toasts[:index], toasts[index+1:]...)
		toasts = append(toasts, toast)
		store.Toasts = toasts
		return store
	}
	if spec.ID == "" {
		store.nextToastSeq++
		spec.ID = formatToastID(store.nextToastSeq)
	}
	toast := ToastState{
		ID:                spec.ID,
		Severity:          spec.Severity,
		Title:             spec.Title,
		Body:              spec.Body,
		Pending:           spec.Pending,
		DismissAfterTicks: dismissAfterTicks,
	}
	store.Toasts = append(cloneToasts(store.Toasts), toast)
	return store
}

func (store ShellStore) findMatchingToast(spec ToastSpec) int {
	if spec.ID != "" {
		for index, toast := range store.Toasts {
			if toast.ID == spec.ID {
				return index
			}
		}
	}
	for index, toast := range store.Toasts {
		if toast.Severity == spec.Severity &&
			toast.Title == spec.Title &&
			toast.Body == spec.Body &&
			toast.Pending == spec.Pending {
			return index
		}
	}
	return -1
}

func (store ShellStore) TickToasts(ticks uint64) ShellStore {
	if ticks == 0 || len(store.Toasts) == 0 {
		return store
	}
	kept := make([]ToastState, 0, len(store.Toasts))
	for _, toast := range store.Toasts {
		toast.AgeTicks += ticks
		if toast.DismissAfterTicks > 0 && toast.AgeTicks >= toast.DismissAfterTicks {
			continue
		}
		kept = append(kept, toast)
	}
	store.Toasts = cloneToasts(kept)
	return store
}

func (store ShellStore) CloseCurrentToast() ShellStore {
	if len(store.Toasts) == 0 {
		return store
	}
	store.Toasts = cloneToasts(store.Toasts[:len(store.Toasts)-1])
	return store
}

func (store ShellStore) ClearToasts() ShellStore {
	store.Toasts = nil
	return store
}

// defaultToastDismissAfterTicks 给新增 toast 明确生命周期，避免真实 runtime 中遗留静态消息。
func defaultToastDismissAfterTicks(spec ToastSpec) uint64 {
	if spec.Pending {
		return pendingToastDismissTicks
	}
	switch spec.Severity {
	case ToastWarning, ToastError:
		return attentionToastDismissTicks
	default:
		return defaultToastDismissTicks
	}
}

func (store ShellStore) OpenTerminalPicker() ShellStore {
	store = store.EnsureDefaults()
	store.Overlay = OverlayState{
		Kind:          OverlayTerminalPicker,
		Open:          true,
		TargetID:      store.ActivePaneID,
		SelectedIndex: 0,
	}
	return store
}

func (store ShellStore) OpenTerminalPool() ShellStore {
	store = store.EnsureDefaults()
	store.Overlay = OverlayState{
		Kind:          OverlayTerminalPool,
		Open:          true,
		TargetID:      store.ActivePaneID,
		SelectedIndex: 0,
	}
	return store
}

func (store ShellStore) OpenWorkbenchTree() ShellStore {
	store = store.EnsureDefaults()
	store.Overlay = OverlayState{
		Kind:          OverlayWorkbenchTree,
		Open:          true,
		TargetID:      store.ActivePaneID,
		SelectedIndex: 0,
	}
	return store
}

func (store ShellStore) OpenPrompt(prompt PromptState) ShellStore {
	store = store.EnsureDefaults()
	if prompt.Title == "" {
		prompt.Title = "Command Prompt"
	}
	if prompt.Placeholder == "" {
		prompt.Placeholder = "type command"
	}
	if prompt.Destructive && prompt.ConfirmText == "" {
		prompt.ConfirmText = "confirm"
	}
	store.Overlay = OverlayState{
		Kind:   OverlayPrompt,
		Open:   true,
		Prompt: prompt,
	}
	return store
}

func (store ShellStore) OpenHelp(section string) ShellStore {
	store = store.EnsureDefaults()
	store.Overlay = OverlayState{
		Kind:        OverlayHelp,
		Open:        true,
		HelpSection: section,
	}
	return store
}

func (store ShellStore) SetPromptValue(value string) ShellStore {
	store = store.EnsureDefaults()
	if store.Overlay.Kind != OverlayPrompt || !store.Overlay.Open {
		return store
	}
	store.Overlay.Prompt.Value = value
	return store
}

func (store ShellStore) SubmitPrompt() ShellStore {
	store = store.EnsureDefaults()
	if store.Overlay.Kind != OverlayPrompt || !store.Overlay.Open {
		return store
	}
	prompt := store.Overlay.Prompt
	value := strings.TrimSpace(prompt.Value)
	if prompt.Destructive && value != prompt.ConfirmText {
		prompt.LastResult = "confirm required: " + prompt.ConfirmText
		store.Overlay.Prompt = prompt
		return store
	}
	prompt.Submitted = true
	prompt.LastResult = value
	store.Overlay.Prompt = prompt
	return store
}

func (store ShellStore) CancelPrompt() ShellStore {
	store = store.EnsureDefaults()
	if store.Overlay.Kind != OverlayPrompt || !store.Overlay.Open {
		return store
	}
	store.Overlay.Prompt.Canceled = true
	return store
}

func (store ShellStore) SetTerminalPickerQuery(query string) ShellStore {
	store = store.EnsureDefaults()
	if store.Overlay.Kind != OverlayTerminalPicker || !store.Overlay.Open {
		return store
	}
	store.Overlay.Query = query
	store.Overlay.SelectedIndex = 0
	return store
}

func (store ShellStore) SetTerminalPoolQuery(query string) ShellStore {
	store = store.EnsureDefaults()
	if store.Overlay.Kind != OverlayTerminalPool || !store.Overlay.Open {
		return store
	}
	store.Overlay.Query = query
	store.Overlay.SelectedIndex = 0
	return store
}

func (store ShellStore) SetWorkbenchTreeQuery(query string) ShellStore {
	store = store.EnsureDefaults()
	if store.Overlay.Kind != OverlayWorkbenchTree || !store.Overlay.Open {
		return store
	}
	store.Overlay.Query = query
	store.Overlay.SelectedIndex = 0
	return store
}

func (store ShellStore) MoveTerminalPickerSelection(delta int, itemCount int) ShellStore {
	store = store.EnsureDefaults()
	if store.Overlay.Kind != OverlayTerminalPicker || !store.Overlay.Open || itemCount <= 0 || delta == 0 {
		return store
	}
	next := store.Overlay.SelectedIndex + delta
	next %= itemCount
	if next < 0 {
		next += itemCount
	}
	store.Overlay.SelectedIndex = next
	return store
}

func (store ShellStore) MoveTerminalPoolSelection(delta int, itemCount int) ShellStore {
	store = store.EnsureDefaults()
	if store.Overlay.Kind != OverlayTerminalPool || !store.Overlay.Open || itemCount <= 0 || delta == 0 {
		return store
	}
	next := store.Overlay.SelectedIndex + delta
	next %= itemCount
	if next < 0 {
		next += itemCount
	}
	store.Overlay.SelectedIndex = next
	return store
}

func (store ShellStore) MoveWorkbenchTreeSelection(delta int, itemCount int) ShellStore {
	store = store.EnsureDefaults()
	if store.Overlay.Kind != OverlayWorkbenchTree || !store.Overlay.Open || itemCount <= 0 || delta == 0 {
		return store
	}
	next := store.Overlay.SelectedIndex + delta
	next %= itemCount
	if next < 0 {
		next += itemCount
	}
	store.Overlay.SelectedIndex = next
	return store
}

func (store ShellStore) SetTerminalPickerSelectedIndex(index int, itemCount int) ShellStore {
	store = store.EnsureDefaults()
	if store.Overlay.Kind != OverlayTerminalPicker || !store.Overlay.Open || itemCount <= 0 {
		return store
	}
	if index < 0 {
		index = 0
	}
	if index >= itemCount {
		index = itemCount - 1
	}
	store.Overlay.SelectedIndex = index
	return store
}

func (store ShellStore) SetTerminalPoolSelectedIndex(index int, itemCount int) ShellStore {
	store = store.EnsureDefaults()
	if store.Overlay.Kind != OverlayTerminalPool || !store.Overlay.Open || itemCount <= 0 {
		return store
	}
	if index < 0 {
		index = 0
	}
	if index >= itemCount {
		index = itemCount - 1
	}
	store.Overlay.SelectedIndex = index
	return store
}

func (store ShellStore) SetWorkbenchTreeSelectedIndex(index int, itemCount int) ShellStore {
	store = store.EnsureDefaults()
	if store.Overlay.Kind != OverlayWorkbenchTree || !store.Overlay.Open || itemCount <= 0 {
		return store
	}
	if index < 0 {
		index = 0
	}
	if index >= itemCount {
		index = itemCount - 1
	}
	store.Overlay.SelectedIndex = index
	return store
}

func (store ShellStore) CloseOverlay() ShellStore {
	store.Overlay = OverlayState{}
	return store.EnsureDefaults()
}

func (store ShellStore) SplitActivePane(newPane PaneState, direction SplitDirection) ShellStore {
	if direction != SplitDirectionHorizontal && direction != SplitDirectionVertical {
		return store.EnsureDefaults()
	}
	store = store.EnsureDefaults()
	if newPane.ID == "" {
		return store
	}
	tabIndex := store.activeTabIndex()
	if tabIndex < 0 {
		return store
	}
	tab := &store.Workspace.Tabs[tabIndex]
	for _, pane := range tab.Panes {
		if pane.ID == newPane.ID {
			return store
		}
	}
	if newPane.Title == "" {
		newPane.Title = newPane.ID
	}
	if newPane.Kind == "" {
		newPane.Kind = PaneEmpty
	}
	previousActive := tab.ActivePaneID
	if previousActive == "" {
		previousActive = store.ActivePaneID
	}
	tab.Panes = append(clonePanes(tab.Panes), newPane)
	tab.RootSplit = insertSplitNode(tab.RootSplit, previousActive, newPane.ID, direction)
	tab.ActivePaneID = newPane.ID
	store.ActivePaneID = newPane.ID
	store.ZoomedPaneID = ""
	store.Workspace = store.Workspace.ensureActive(store.ActivePaneID)
	return store
}

func (store ShellStore) FocusPane(target PaneCommandTarget) ShellStore {
	store = store.EnsureDefaults()
	if !store.HasPane(target) {
		return store
	}
	tabIndex := store.tabIndexForTarget(target)
	if tabIndex < 0 {
		return store
	}
	paneID := target.PaneID
	store.Workspace.ActiveTabID = store.Workspace.Tabs[tabIndex].ID
	store.Workspace.Tabs[tabIndex].ActivePaneID = paneID
	store.ActivePaneID = paneID
	if store.ZoomedPaneID != "" {
		store.ZoomedPaneID = paneID
	}
	store.Workspace = store.Workspace.ensureActive(store.ActivePaneID)
	return store
}

func (store ShellStore) BindPaneTerminal(target PaneCommandTarget, terminalID string) ShellStore {
	store = store.EnsureDefaults()
	if terminalID == "" {
		return store
	}
	tabIndex := store.tabIndexForTarget(target)
	if tabIndex < 0 {
		return store
	}
	paneID := target.PaneID
	if paneID == "" {
		paneID = store.ActivePaneID
	}
	for index := range store.Workspace.Tabs[tabIndex].Panes {
		if store.Workspace.Tabs[tabIndex].Panes[index].ID != paneID {
			continue
		}
		store.Workspace.Tabs[tabIndex].Panes[index].TerminalID = terminalID
		store.Workspace.Tabs[tabIndex].Panes[index].Kind = PaneTerminalLive
		if store.Workspace.Tabs[tabIndex].Panes[index].Title == "" {
			store.Workspace.Tabs[tabIndex].Panes[index].Title = terminalID
		}
		store.Workspace = store.Workspace.ensureActive(store.ActivePaneID)
		store.Workspaces = upsertWorkspace(store.Workspaces, store.Workspace)
		return store
	}
	return store
}

func (store ShellStore) FocusRelativePane(offset int) ShellStore {
	store = store.EnsureDefaults()
	if offset == 0 {
		return store
	}
	tabIndex := store.activeTabIndex()
	if tabIndex < 0 {
		return store
	}
	tab := store.Workspace.Tabs[tabIndex]
	if len(tab.Panes) <= 1 {
		return store
	}
	current := 0
	for i, pane := range tab.Panes {
		if pane.ID == store.ActivePaneID {
			current = i
			break
		}
	}
	next := (current + offset) % len(tab.Panes)
	if next < 0 {
		next += len(tab.Panes)
	}
	return store.FocusPane(PaneCommandTarget{WorkspaceID: store.Workspace.ID, TabID: tab.ID, PaneID: tab.Panes[next].ID})
}

func (store ShellStore) ClosePane(target PaneCommandTarget) ShellStore {
	store = store.EnsureDefaults()
	tabIndex := store.tabIndexForTarget(target)
	if tabIndex < 0 {
		return store
	}
	tab := &store.Workspace.Tabs[tabIndex]
	if len(tab.Panes) <= 1 {
		return store
	}
	paneID := target.PaneID
	nextPanes := make([]PaneState, 0, len(tab.Panes)-1)
	for _, pane := range tab.Panes {
		if pane.ID != paneID {
			nextPanes = append(nextPanes, pane)
		}
	}
	if len(nextPanes) == len(tab.Panes) || len(nextPanes) == 0 {
		return store
	}
	tab.Panes = nextPanes
	if nextRoot, ok := removePaneFromSplit(tab.RootSplit, paneID); ok {
		tab.RootSplit = nextRoot
	} else {
		tab.RootSplit = SplitNode{PaneID: nextPanes[0].ID}
	}
	if tab.ActivePaneID == paneID || store.ActivePaneID == paneID {
		tab.ActivePaneID = firstPaneIDInSplit(tab.RootSplit)
		if tab.ActivePaneID == "" {
			tab.ActivePaneID = nextPanes[0].ID
		}
		store.ActivePaneID = tab.ActivePaneID
	}
	if store.ZoomedPaneID == paneID {
		store.ZoomedPaneID = ""
	}
	store.Workspace = store.Workspace.ensureActive(store.ActivePaneID)
	store.Workspaces = upsertWorkspace(store.Workspaces, store.Workspace)
	return store
}

func (store ShellStore) ZoomPane(target PaneCommandTarget) ShellStore {
	store = store.FocusPane(target)
	if store.HasPane(target) {
		store.ZoomedPaneID = target.PaneID
	}
	return store
}

func (store ShellStore) UnzoomPane() ShellStore {
	store = store.EnsureDefaults()
	store.ZoomedPaneID = ""
	return store
}

func (store ShellStore) ToggleZoomPane(target PaneCommandTarget) ShellStore {
	store = store.EnsureDefaults()
	if store.ZoomedPaneID == target.PaneID && store.ZoomedPaneID != "" {
		return store.UnzoomPane()
	}
	return store.ZoomPane(target)
}

func (store ShellStore) ResizePane(target PaneCommandTarget, direction PaneResizeDirection, delta int) ShellStore {
	store = store.EnsureDefaults()
	if delta == 0 {
		return store
	}
	tabIndex := store.tabIndexForTarget(target)
	if tabIndex < 0 {
		return store
	}
	tab := &store.Workspace.Tabs[tabIndex]
	tab.RootSplit, _ = resizeSplitNode(tab.RootSplit, target.PaneID, direction, delta)
	return store
}

func (store ShellStore) ResizeSplitPath(target PaneCommandTarget, splitPath string, direction PaneResizeDirection, delta int) ShellStore {
	store = store.EnsureDefaults()
	if delta == 0 {
		return store
	}
	tabIndex := store.tabIndexForTarget(target)
	if tabIndex < 0 {
		return store
	}
	path, ok := parseSplitPath(splitPath)
	if !ok {
		return store
	}
	tab := &store.Workspace.Tabs[tabIndex]
	tab.RootSplit, _ = resizeSplitNodeByPath(tab.RootSplit, path, direction, delta)
	return store
}

func (store ShellStore) ResizePaneGroup(target PaneCommandTarget, direction PaneResizeDirection, group []PaneResizeGroupItem) ShellStore {
	store = store.EnsureDefaults()
	if len(group) < 2 {
		return store
	}
	tabIndex := store.tabIndexForTarget(target)
	if tabIndex < 0 {
		return store
	}
	axis, ok := splitDirectionForResize(direction)
	if !ok {
		return store
	}
	items := clonePaneResizeGroupItems(group)
	if !validPaneResizeGroup(items) {
		return store
	}
	tab := &store.Workspace.Tabs[tabIndex]
	next, changed := resizePaneGroupNode(tab.RootSplit, axis, items)
	if !changed {
		return store
	}
	tab.RootSplit = next
	return store
}

func (store ShellStore) SetPaneSize(command PaneCommand) ShellStore {
	store = store.EnsureDefaults()
	tabIndex := store.tabIndexForTarget(command.Target)
	if tabIndex < 0 {
		return store
	}
	tab := &store.Workspace.Tabs[tabIndex]
	tab.RootSplit, _ = setSplitNodeSize(tab.RootSplit, command)
	return store
}

func (store ShellStore) BalancePanes(target PaneCommandTarget) ShellStore {
	store = store.EnsureDefaults()
	tabIndex := store.tabIndexForTarget(target)
	if tabIndex < 0 {
		return store
	}
	tab := &store.Workspace.Tabs[tabIndex]
	tab.RootSplit = balanceSplitNode(tab.RootSplit)
	return store
}

func (store ShellStore) activeTab() TabState {
	for _, tab := range store.Workspace.Tabs {
		if tab.ID == store.Workspace.ActiveTabID {
			return tab
		}
	}
	if len(store.Workspace.Tabs) > 0 {
		return store.Workspace.Tabs[0]
	}
	return TabState{}
}

func (store ShellStore) activeTabIndex() int {
	for index, tab := range store.Workspace.Tabs {
		if tab.ID == store.Workspace.ActiveTabID {
			return index
		}
	}
	if len(store.Workspace.Tabs) > 0 {
		return 0
	}
	return -1
}

func (store ShellStore) PaneByID(paneID string) (PaneState, bool) {
	store = store.EnsureDefaults()
	for _, tab := range store.Workspace.Tabs {
		for _, pane := range tab.Panes {
			if pane.ID == paneID {
				return pane, true
			}
		}
	}
	return PaneState{}, false
}

func (store ShellStore) tabIndexForTarget(target PaneCommandTarget) int {
	store = store.EnsureDefaults()
	for index, tab := range store.Workspace.Tabs {
		if target.TabID != "" && tab.ID != target.TabID {
			continue
		}
		for _, pane := range tab.Panes {
			if pane.ID == target.PaneID {
				return index
			}
		}
	}
	return -1
}

func (store ShellStore) paneCountForTarget(target PaneCommandTarget) int {
	index := store.tabIndexForTarget(target)
	if index < 0 {
		return 0
	}
	return len(store.Workspace.Tabs[index].Panes)
}

func (store ShellStore) hasPaneInActiveTab(paneID string) bool {
	activeTabID := store.Workspace.ActiveTabID
	for _, tab := range store.Workspace.Tabs {
		if activeTabID != "" && tab.ID != activeTabID {
			continue
		}
		for _, pane := range tab.Panes {
			if pane.ID == paneID {
				return true
			}
		}
	}
	return false
}

func (workspace WorkspaceState) ensureActive(activePaneID string) WorkspaceState {
	for tabIndex := range workspace.Tabs {
		tab := &workspace.Tabs[tabIndex]
		tabActive := tab.ID == workspace.ActiveTabID
		if tab.ActivePaneID == "" && len(tab.Panes) > 0 {
			tab.ActivePaneID = tab.Panes[0].ID
		}
		for paneIndex := range tab.Panes {
			pane := &tab.Panes[paneIndex]
			pane.Active = tabActive && pane.ID == activePaneID
		}
	}
	return workspace
}

func (workspace WorkspaceState) ensureTabDefaults() WorkspaceState {
	for tabIndex := range workspace.Tabs {
		tab := &workspace.Tabs[tabIndex]
		if tab.ID == "" {
			tab.ID = DefaultTabID
		}
		if tab.Title == "" {
			tab.Title = tab.ID
		}
		if tab.ActivePaneID == "" && len(tab.Panes) > 0 {
			tab.ActivePaneID = tab.Panes[0].ID
		}
		if tab.RootSplit.PaneID == "" && tab.RootSplit.Direction == "" && len(tab.RootSplit.Children) == 0 {
			tab.RootSplit = SplitNode{PaneID: tab.ActivePaneID}
		}
	}
	return workspace
}

func (workspace WorkspaceState) ensureActiveTab() WorkspaceState {
	if len(workspace.Tabs) == 0 {
		return workspace
	}
	for _, tab := range workspace.Tabs {
		if tab.ID == workspace.ActiveTabID {
			return workspace
		}
	}
	workspace.ActiveTabID = workspace.Tabs[0].ID
	return workspace
}

func (workspace WorkspaceState) ensureDefaults() WorkspaceState {
	if workspace.ID == "" {
		workspace.ID = DefaultWorkspaceID
	}
	if workspace.Name == "" {
		workspace.Name = workspace.ID
		if workspace.Name == DefaultWorkspaceID {
			workspace.Name = "main"
		}
	}
	if len(workspace.Tabs) == 0 {
		workspace.ActiveTabID = DefaultTabID
		workspace.Tabs = []TabState{{
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
	workspace = workspace.ensureTabDefaults()
	return workspace.ensureActiveTab()
}

func (workspace WorkspaceState) activeTab() TabState {
	for _, tab := range workspace.Tabs {
		if tab.ID == workspace.ActiveTabID {
			return tab
		}
	}
	if len(workspace.Tabs) > 0 {
		return workspace.Tabs[0]
	}
	return TabState{}
}

// TerminalPickerItems 从 reducer-owned root 推导 picker 列表；服务端 Terminal Pool 必须先回投到 TerminalPoolStore。
func TerminalPickerItems(root Root) []TerminalPickerItem {
	shell := root.Shell.EnsureDefaults()
	tab := shell.activeTab()
	query := strings.ToLower(strings.TrimSpace(shell.Overlay.Query))
	items := make([]TerminalPickerItem, 0, len(tab.Panes)+len(root.TerminalPool.Items))
	seenTerminal := map[string]struct{}{}
	for _, pane := range tab.Panes {
		terminalID := pickerTerminalID(root, pane)
		if pane.Kind == PaneEmpty && terminalID == "" {
			continue
		}
		item := TerminalPickerItem{
			PaneID:     pane.ID,
			Title:      paneTitle(pane),
			Kind:       pane.Kind,
			TerminalID: terminalID,
			Active:     pane.Active,
		}
		if !matchesTerminalPickerQuery(item, query) {
			continue
		}
		items = append(items, item)
		if terminalID != "" && terminalID != "none" {
			seenTerminal[terminalID] = struct{}{}
		}
	}
	for _, poolItem := range root.TerminalPool.Items {
		if poolItem.TerminalID == "" {
			continue
		}
		if _, seen := seenTerminal[poolItem.TerminalID]; seen {
			continue
		}
		item := TerminalPickerItem{
			Title:      terminalPoolTitle(poolItem),
			Kind:       PaneTerminalLive,
			TerminalID: poolItem.TerminalID,
			Active:     poolItem.Attached,
			FromPool:   true,
			PoolState:  poolItem.State,
		}
		if !matchesTerminalPickerQuery(item, query) {
			continue
		}
		items = append(items, item)
	}
	if len(items) == 0 && query == "" {
		items = append(items, TerminalPickerItem{
			PaneID:     shell.ActivePaneID,
			Title:      "current pane",
			Kind:       PaneEmpty,
			TerminalID: "none",
			Active:     true,
		})
	}
	if len(items) > 0 {
		selected := shell.Overlay.SelectedIndex
		if selected < 0 {
			selected = 0
		}
		if selected >= len(items) {
			selected = len(items) - 1
		}
		items[selected].Selected = true
	}
	return items
}

func TerminalPoolPageItems(root Root) []TerminalPoolPageItem {
	shell := root.Shell.EnsureDefaults()
	query := strings.ToLower(strings.TrimSpace(shell.Overlay.Query))
	items := make([]TerminalPoolPageItem, 0, len(root.TerminalPool.Items))
	for _, poolItem := range root.TerminalPool.Items {
		if poolItem.TerminalID == "" {
			continue
		}
		item := TerminalPoolPageItem{
			TerminalID: poolItem.TerminalID,
			Title:      terminalPoolTitle(poolItem),
			State:      poolItem.State,
			CWD:        poolItem.CWD,
			Tags:       cloneStringMap(poolItem.Tags),
			Cols:       poolItem.Cols,
			Rows:       poolItem.Rows,
			Attached:   poolItem.Attached,
		}
		if !matchesTerminalPoolPageQuery(item, query) {
			continue
		}
		items = append(items, item)
	}
	if len(items) > 0 {
		selected := shell.Overlay.SelectedIndex
		if selected < 0 {
			selected = 0
		}
		if selected >= len(items) {
			selected = len(items) - 1
		}
		items[selected].Selected = true
	}
	return items
}

func WorkbenchTreeItems(root Root) []WorkbenchTreeItem {
	shell := root.Shell.EnsureDefaults()
	query := strings.ToLower(strings.TrimSpace(shell.Overlay.Query))
	workspace := shell.Workspace
	items := make([]WorkbenchTreeItem, 0, 2+len(workspace.Tabs)*2)
	appendItem := func(item WorkbenchTreeItem) {
		if matchesWorkbenchTreeQuery(item, query) {
			items = append(items, item)
		}
	}

	appendItem(WorkbenchTreeItem{
		Kind:          WorkbenchTreeKindWorkspace,
		WorkspaceID:   workspace.ID,
		WorkspaceName: workspace.Name,
		Depth:         0,
		Active:        true,
		Summary:       workbenchWorkspaceSummary(workspace),
	})
	for _, tab := range workspace.Tabs {
		tabActive := tab.ID == workspace.ActiveTabID
		appendItem(WorkbenchTreeItem{
			Kind:          WorkbenchTreeKindTab,
			WorkspaceID:   workspace.ID,
			WorkspaceName: workspace.Name,
			TabID:         tab.ID,
			TabTitle:      tab.Title,
			PaneID:        tab.ActivePaneID,
			Depth:         1,
			Active:        tabActive,
			Summary:       workbenchTabSummary(tab),
		})
		for _, pane := range tab.Panes {
			terminalID := pickerTerminalID(root, pane)
			appendItem(WorkbenchTreeItem{
				Kind:          WorkbenchTreeKindPane,
				WorkspaceID:   workspace.ID,
				WorkspaceName: workspace.Name,
				TabID:         tab.ID,
				TabTitle:      tab.Title,
				PaneID:        pane.ID,
				PaneTitle:     paneTitle(pane),
				PaneKind:      pane.Kind,
				TerminalID:    terminalID,
				Depth:         2,
				Active:        tabActive && pane.ID == shell.ActivePaneID,
				Summary:       workbenchPaneSummary(pane, terminalID),
			})
		}
	}
	appendItem(WorkbenchTreeItem{
		Kind:          WorkbenchTreeKindFloating,
		WorkspaceID:   workspace.ID,
		WorkspaceName: workspace.Name,
		Depth:         1,
		Active:        len(shell.Floatings) > 0,
		Summary:       fmt.Sprintf("float:%d", len(shell.Floatings)),
	})
	if len(items) > 0 {
		selected := shell.Overlay.SelectedIndex
		if selected < 0 {
			selected = 0
		}
		if selected >= len(items) {
			selected = len(items) - 1
		}
		items[selected].Selected = true
	}
	return items
}

func pickerTerminalID(root Root, pane PaneState) string {
	if pane.TerminalID != "" {
		return pane.TerminalID
	}
	if pane.Active && root.Session.TerminalID != "" {
		return root.Session.TerminalID
	}
	if pane.Active && root.Surface.TerminalID != "" {
		return root.Surface.TerminalID
	}
	if pane.Active && root.History.TerminalID != "" {
		return root.History.TerminalID
	}
	return ""
}

func matchesTerminalPickerQuery(item TerminalPickerItem, query string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(item.Title), query) ||
		strings.Contains(strings.ToLower(item.PaneID), query) ||
		strings.Contains(strings.ToLower(item.TerminalID), query) ||
		strings.Contains(strings.ToLower(string(item.Kind)), query) ||
		strings.Contains(strings.ToLower(item.PoolState), query)
}

func matchesTerminalPoolPageQuery(item TerminalPoolPageItem, query string) bool {
	if query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(item.Title), query) ||
		strings.Contains(strings.ToLower(item.TerminalID), query) ||
		strings.Contains(strings.ToLower(item.State), query) ||
		strings.Contains(strings.ToLower(item.CWD), query) {
		return true
	}
	for key, value := range item.Tags {
		if strings.Contains(strings.ToLower(key), query) || strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func matchesWorkbenchTreeQuery(item WorkbenchTreeItem, query string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(item.Kind), query) ||
		strings.Contains(strings.ToLower(item.WorkspaceName), query) ||
		strings.Contains(strings.ToLower(item.WorkspaceID), query) ||
		strings.Contains(strings.ToLower(item.TabTitle), query) ||
		strings.Contains(strings.ToLower(item.TabID), query) ||
		strings.Contains(strings.ToLower(item.PaneTitle), query) ||
		strings.Contains(strings.ToLower(item.PaneID), query) ||
		strings.Contains(strings.ToLower(string(item.PaneKind)), query) ||
		strings.Contains(strings.ToLower(item.TerminalID), query) ||
		strings.Contains(strings.ToLower(item.Summary), query)
}

func workbenchWorkspaceSummary(workspace WorkspaceState) string {
	return fmt.Sprintf("tabs:%d panes:%d", len(workspace.Tabs), workspacePaneCount(workspace))
}

func workbenchTabSummary(tab TabState) string {
	return fmt.Sprintf("panes:%d active:%s", len(tab.Panes), tab.ActivePaneID)
}

func workbenchPaneSummary(pane PaneState, terminalID string) string {
	summary := string(pane.Kind)
	if terminalID != "" {
		summary += " term:" + terminalID
	}
	return summary
}

func workspacePaneCount(workspace WorkspaceState) int {
	count := 0
	for _, tab := range workspace.Tabs {
		count += len(tab.Panes)
	}
	return count
}

func (store ShellStore) tabIndexByID(id string) int {
	for index, tab := range store.Workspace.Tabs {
		if tab.ID == id {
			return index
		}
	}
	return -1
}

func ensureWorkspaceList(workspaces []WorkspaceState, active WorkspaceState) []WorkspaceState {
	active = active.ensureDefaults()
	if len(workspaces) == 0 {
		return []WorkspaceState{active}
	}
	out := cloneWorkspaces(workspaces)
	for index := range out {
		out[index] = out[index].ensureDefaults()
	}
	return out
}

func workspaceByID(workspaces []WorkspaceState, id string) (WorkspaceState, bool) {
	index := workspaceIndexByID(workspaces, id)
	if index < 0 {
		return WorkspaceState{}, false
	}
	return cloneWorkspace(workspaces[index]).ensureDefaults(), true
}

func workspaceIndexByID(workspaces []WorkspaceState, id string) int {
	for index, workspace := range workspaces {
		if workspace.ID == id {
			return index
		}
	}
	return -1
}

func upsertWorkspace(workspaces []WorkspaceState, workspace WorkspaceState) []WorkspaceState {
	workspace = workspace.ensureDefaults()
	out := cloneWorkspaces(workspaces)
	if len(out) == 0 {
		return []WorkspaceState{workspace}
	}
	for index := range out {
		if out[index].ID == workspace.ID {
			out[index] = workspace
			return out
		}
	}
	return append(out, workspace)
}

func appendWorkspaceIfMissing(workspaces []WorkspaceState, workspace WorkspaceState) []WorkspaceState {
	if _, ok := workspaceByID(workspaces, workspace.ID); ok {
		return cloneWorkspaces(workspaces)
	}
	return append(cloneWorkspaces(workspaces), workspace.ensureDefaults())
}

func terminalIDsForTab(tab TabState) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, pane := range tab.Panes {
		if pane.TerminalID == "" {
			continue
		}
		if _, ok := seen[pane.TerminalID]; ok {
			continue
		}
		seen[pane.TerminalID] = struct{}{}
		out = append(out, pane.TerminalID)
	}
	return out
}

func (store ShellStore) workbenchPaneTarget(command WorkbenchCommand) PaneCommandTarget {
	target := command.Target
	if target.PaneID == "" {
		target.PaneID = strings.TrimSpace(command.TargetID)
	}
	if target.PaneID == "" {
		target.PaneID = store.EnsureDefaults().ActivePaneID
	}
	if target.WorkspaceID == "" {
		target.WorkspaceID = store.EnsureDefaults().Workspace.ID
	}
	if target.TabID == "" {
		target.TabID = store.EnsureDefaults().Workspace.ActiveTabID
	}
	return target
}

func (store ShellStore) setPaneDetached(target PaneCommandTarget) ShellStore {
	store = store.EnsureDefaults()
	tabIndex := store.tabIndexForTarget(target)
	if tabIndex < 0 {
		return store
	}
	paneID := target.PaneID
	if paneID == "" {
		paneID = store.ActivePaneID
	}
	for index := range store.Workspace.Tabs[tabIndex].Panes {
		pane := &store.Workspace.Tabs[tabIndex].Panes[index]
		if pane.ID != paneID {
			continue
		}
		pane.TerminalID = ""
		pane.Kind = PaneEmpty
		if pane.Title == "" {
			pane.Title = "empty"
		}
		store.Workspace = store.Workspace.ensureActive(store.ActivePaneID)
		store.Workspaces = upsertWorkspace(store.Workspaces, store.Workspace)
		return store
	}
	return store
}

func (store ShellStore) removePaneWorkbench(action WorkbenchCommandAction, target PaneCommandTarget, killed []string) (ShellStore, WorkbenchCommandResult) {
	target = store.workbenchPaneTarget(WorkbenchCommand{Target: target})
	pane, ok := store.Pane(target)
	if !ok {
		return store, workbenchCommandInvalid(action, "pane not found")
	}
	if store.paneCountForTarget(target) <= 1 {
		return store, workbenchCommandInvalid(action, "cannot close last pane")
	}
	store = store.ClosePane(target)
	return store, WorkbenchCommandResult{Status: WorkbenchCommandOK, Action: action, ID: pane.ID, Killed: killed}
}

func nextTabID(workspace WorkspaceState) string {
	for i := 2; ; i++ {
		id := fmt.Sprintf("tab-%d", i)
		exists := false
		for _, tab := range workspace.Tabs {
			if tab.ID == id {
				exists = true
				break
			}
		}
		if !exists {
			return id
		}
	}
}

func nextWorkspaceID(workspaces []WorkspaceState) string {
	for i := 2; ; i++ {
		id := fmt.Sprintf("workspace-%d", i)
		if workspaceIndexByID(workspaces, id) < 0 {
			return id
		}
	}
}

func (store ShellStore) floatingIndex(id string) int {
	for index, floating := range store.Floatings {
		if floating.ID == id {
			return index
		}
	}
	return -1
}

func (store ShellStore) floatingIndexOrActive(id string) int {
	if id != "" {
		return store.floatingIndex(id)
	}
	if store.ActiveFloatingID != "" {
		return store.floatingIndex(store.ActiveFloatingID)
	}
	if len(store.Floatings) == 0 {
		return -1
	}
	topID := topFloatingID(store.Floatings)
	return store.floatingIndex(topID)
}

func (store ShellStore) nextFloatingZ() int {
	maxZ := 0
	for _, floating := range store.Floatings {
		if floating.Z > maxZ {
			maxZ = floating.Z
		}
	}
	return maxZ
}

func topFloatingID(floatings []FloatingPaneState) string {
	if len(floatings) == 0 {
		return ""
	}
	top := floatings[0]
	for _, floating := range floatings {
		if floating.Z >= top.Z {
			top = floating
		}
	}
	return top.ID
}

func floatingTitle(title string, pane PaneState) string {
	if title != "" {
		return title
	}
	if pane.Title != "" {
		return pane.Title
	}
	if pane.ID != "" {
		return pane.ID
	}
	return "floating"
}

func centerFloatingRect(rect FloatingRect, boundsW int, boundsH int) FloatingRect {
	if boundsW > 0 {
		rect.X = maxIntState(0, (boundsW-rect.W)/2)
	}
	if boundsH > 0 {
		rect.Y = maxIntState(0, (boundsH-rect.H)/2)
	}
	return clampFloatingRect(rect, boundsW, boundsH)
}

func clampFloatingRect(rect FloatingRect, boundsW int, boundsH int) FloatingRect {
	const minW = 16
	const minH = 4
	rect.W = maxIntState(minW, rect.W)
	rect.H = maxIntState(minH, rect.H)
	if boundsW > 0 {
		rect.W = minIntState(rect.W, maxIntState(minW, boundsW))
		rect.X = clampIntState(rect.X, 0, maxIntState(0, boundsW-rect.W))
	} else if rect.X < 0 {
		rect.X = 0
	}
	if boundsH > 0 {
		rect.H = minIntState(rect.H, maxIntState(minH, boundsH))
		rect.Y = clampIntState(rect.Y, 0, maxIntState(0, boundsH-rect.H))
	} else if rect.Y < 0 {
		rect.Y = 0
	}
	return rect
}

func clampIntState(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func minIntState(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxIntState(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func paneTitle(pane PaneState) string {
	if pane.Title != "" {
		return pane.Title
	}
	if pane.TerminalID != "" {
		return pane.TerminalID
	}
	if pane.ID != "" {
		return pane.ID
	}
	return "pane"
}

func terminalPoolTitle(item TerminalPoolItem) string {
	if item.Title != "" {
		return item.Title
	}
	if item.TerminalID != "" {
		return item.TerminalID
	}
	return "terminal"
}

func formatToastID(seq uint64) string {
	if seq == 0 {
		return "toast-0"
	}
	digits := make([]byte, 0, 20)
	for seq > 0 {
		digits = append(digits, byte('0'+seq%10))
		seq /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return "toast-" + string(digits)
}

func formatFloatingID(seq uint64) string {
	if seq == 0 {
		return "floating-0"
	}
	return "floating-" + formatToastID(seq)[len("toast-"):]
}

func floatingCommandInvalid(action FloatingCommandAction, reason string) FloatingCommandResult {
	return FloatingCommandResult{Status: FloatingCommandInvalid, Action: action, Reason: reason}
}

func workbenchCommandInvalid(action WorkbenchCommandAction, reason string) WorkbenchCommandResult {
	return WorkbenchCommandResult{Status: WorkbenchCommandInvalid, Action: action, Reason: reason}
}

func insertSplitNode(node SplitNode, targetPaneID string, newPaneID string, direction SplitDirection) SplitNode {
	if node.PaneID == targetPaneID || (node.PaneID == "" && len(node.Children) == 0) {
		return SplitNode{
			Direction: direction,
			Children: []SplitNode{
				{PaneID: targetPaneID},
				{PaneID: newPaneID},
			},
		}
	}
	children := cloneSplitNodes(node.Children)
	for i, child := range children {
		children[i] = insertSplitNode(child, targetPaneID, newPaneID, direction)
	}
	node.Children = children
	return node
}

func removePaneFromSplit(node SplitNode, paneID string) (SplitNode, bool) {
	if node.PaneID != "" || len(node.Children) == 0 {
		return node, node.PaneID != paneID
	}
	children := make([]SplitNode, 0, len(node.Children))
	for _, child := range node.Children {
		if next, keep := removePaneFromSplit(child, paneID); keep {
			children = append(children, next)
		}
	}
	if len(children) == 0 {
		return SplitNode{}, false
	}
	if len(children) == 1 {
		return children[0], true
	}
	node.Children = children
	return node, true
}

func firstPaneIDInSplit(node SplitNode) string {
	if node.PaneID != "" {
		return node.PaneID
	}
	for _, child := range node.Children {
		if paneID := firstPaneIDInSplit(child); paneID != "" {
			return paneID
		}
	}
	return ""
}

func resizeSplitNode(node SplitNode, paneID string, direction PaneResizeDirection, delta int) (SplitNode, bool) {
	if node.PaneID != "" || len(node.Children) < 2 {
		return node, node.PaneID == paneID
	}
	firstContains := splitContainsPane(node.Children[0], paneID)
	secondContains := splitContainsPane(node.Children[1], paneID)
	if firstContains || secondContains {
		if splitDirectionMatchesResize(node.Direction, direction) {
			node.BiasCells += resizeBiasDelta(node.Direction, direction, firstContains, delta)
			node.FixedPaneID = ""
			node.FixedCols = 0
			node.FixedRows = 0
			node.Ratio = 0
			return node, true
		}
		childIndex := 0
		if secondContains {
			childIndex = 1
		}
		children := cloneSplitNodes(node.Children)
		children[childIndex], _ = resizeSplitNode(children[childIndex], paneID, direction, delta)
		node.Children = children
		return node, true
	}
	children := cloneSplitNodes(node.Children)
	changed := false
	for i, child := range children {
		children[i], changed = resizeSplitNode(child, paneID, direction, delta)
		if changed {
			break
		}
	}
	node.Children = children
	return node, changed
}

func resizeSplitNodeByPath(node SplitNode, path []int, direction PaneResizeDirection, delta int) (SplitNode, bool) {
	if len(path) == 0 {
		if node.PaneID != "" || len(node.Children) < 2 || !splitDirectionMatchesResize(node.Direction, direction) {
			return node, false
		}
		// 鼠标拖拽 divider 时目标就是当前 split，delta 已按拖拽方向带符号；不能再按 pane 所在侧反推祖先。
		node.BiasCells += delta
		node.FixedPaneID = ""
		node.FixedCols = 0
		node.FixedRows = 0
		node.Ratio = 0
		return node, true
	}
	index := path[0]
	if index < 0 || index >= len(node.Children) {
		return node, false
	}
	children := cloneSplitNodes(node.Children)
	next, changed := resizeSplitNodeByPath(children[index], path[1:], direction, delta)
	if !changed {
		return node, false
	}
	children[index] = next
	node.Children = children
	return node, true
}

func parseSplitPath(path string) ([]int, bool) {
	if path == PaneResizeRootSplitPath {
		return nil, true
	}
	prefix := PaneResizeRootSplitPath + "/"
	if !strings.HasPrefix(path, prefix) {
		return nil, false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) == 0 {
		return nil, false
	}
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		index, err := strconv.Atoi(part)
		if err != nil || (index != 0 && index != 1) {
			return nil, false
		}
		out = append(out, index)
	}
	return out, true
}

func resizePaneGroupNode(node SplitNode, axis SplitDirection, group []PaneResizeGroupItem) (SplitNode, bool) {
	panes := paneIDsInSplitOrder(node, nil)
	if samePaneOrder(panes, group) {
		// 鼠标命中的是一条真实 divider；group 带有每个叶子在该轴上的目标尺寸。
		// 这里保留原有异轴 stacked 结构，只给同轴 split 写入固定尺寸 hint。
		return applyPaneResizeGroupNode(node, axis, paneResizeGroupByPane(group))
	}
	if node.PaneID != "" || len(node.Children) == 0 {
		return node, false
	}
	children := cloneSplitNodes(node.Children)
	for i, child := range children {
		next, changed := resizePaneGroupNode(child, axis, group)
		if changed {
			children[i] = next
			node.Children = children
			return node, true
		}
	}
	return node, false
}

func paneIDsInSplitOrder(node SplitNode, out []string) []string {
	if node.PaneID != "" {
		return append(out, node.PaneID)
	}
	for _, child := range node.Children {
		out = paneIDsInSplitOrder(child, out)
	}
	return out
}

func paneResizeGroupByPane(group []PaneResizeGroupItem) map[string]PaneResizeGroupItem {
	out := make(map[string]PaneResizeGroupItem, len(group))
	for _, item := range group {
		out[item.PaneID] = item
	}
	return out
}

func applyPaneResizeGroupNode(node SplitNode, axis SplitDirection, group map[string]PaneResizeGroupItem) (SplitNode, bool) {
	if node.PaneID != "" {
		_, ok := group[node.PaneID]
		return node, ok
	}
	children := cloneSplitNodes(node.Children)
	changed := false
	for i, child := range children {
		next, childChanged := applyPaneResizeGroupNode(child, axis, group)
		if childChanged {
			children[i] = next
			changed = true
		}
	}
	if !changed {
		return node, false
	}
	node.Children = children
	if node.Direction == axis && len(node.Children) >= 2 {
		firstExtent, firstOK := paneResizeGroupExtent(node.Children[0], axis, group)
		secondExtent, secondOK := paneResizeGroupExtent(node.Children[1], axis, group)
		if firstOK && secondOK && firstExtent > 0 && secondExtent > 0 {
			node.BiasCells = 0
			node.Ratio = 0
			node.FixedPaneID = firstPaneIDInSplit(node.Children[0])
			if axis == SplitDirectionVertical {
				node.FixedCols = firstExtent
				node.FixedRows = 0
			} else {
				node.FixedRows = firstExtent
				node.FixedCols = 0
			}
		}
	}
	return node, true
}

func paneResizeGroupExtent(node SplitNode, axis SplitDirection, group map[string]PaneResizeGroupItem) (int, bool) {
	if node.PaneID != "" {
		item, ok := group[node.PaneID]
		return item.Cells, ok && item.Cells > 0
	}
	if len(node.Children) == 0 {
		return 0, false
	}
	extent := 0
	for _, child := range node.Children {
		childExtent, ok := paneResizeGroupExtent(child, axis, group)
		if !ok {
			return 0, false
		}
		if node.Direction == axis {
			extent += childExtent
		} else if childExtent > extent {
			extent = childExtent
		}
	}
	return extent, extent > 0
}

func flattenSameAxisSplit(node SplitNode, axis SplitDirection, out []string) ([]string, bool) {
	if node.PaneID != "" {
		return append(out, node.PaneID), true
	}
	if node.Direction != axis || len(node.Children) < 2 {
		return out, false
	}
	for _, child := range node.Children {
		var ok bool
		out, ok = flattenSameAxisSplit(child, axis, out)
		if !ok {
			return out, false
		}
	}
	return out, true
}

func samePaneOrder(panes []string, group []PaneResizeGroupItem) bool {
	if len(panes) != len(group) {
		return false
	}
	for i, paneID := range panes {
		if paneID != group[i].PaneID {
			return false
		}
	}
	return true
}

func buildFixedAxisSplit(axis SplitDirection, group []PaneResizeGroupItem) SplitNode {
	if len(group) == 0 {
		return SplitNode{}
	}
	if len(group) == 1 {
		return SplitNode{PaneID: group[0].PaneID}
	}
	first := SplitNode{PaneID: group[0].PaneID}
	rest := buildFixedAxisSplit(axis, group[1:])
	node := SplitNode{
		Direction: axis,
		Children:  []SplitNode{first, rest},
	}
	// 鼠标拖动视觉相邻 pane 时，后侧 subtree 必须保持自己的原始总尺寸，避免后续 pane 被比例缩放。
	node.FixedPaneID = group[0].PaneID
	if axis == SplitDirectionVertical {
		node.FixedCols = group[0].Cells
	} else {
		node.FixedRows = group[0].Cells
	}
	return node
}

func clonePaneResizeGroupItems(items []PaneResizeGroupItem) []PaneResizeGroupItem {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]PaneResizeGroupItem, len(items))
	copy(cloned, items)
	return cloned
}

func validPaneResizeGroup(items []PaneResizeGroupItem) bool {
	for _, item := range items {
		if item.PaneID == "" || item.Cells <= 0 {
			return false
		}
	}
	return true
}

func setSplitNodeSize(node SplitNode, command PaneCommand) (SplitNode, bool) {
	if node.PaneID != "" || len(node.Children) < 2 {
		return node, node.PaneID == command.Target.PaneID
	}
	firstContains := splitContainsPane(node.Children[0], command.Target.PaneID)
	secondContains := splitContainsPane(node.Children[1], command.Target.PaneID)
	if firstContains || secondContains {
		node.BiasCells = 0
		node.FixedPaneID = ""
		node.FixedCols = 0
		node.FixedRows = 0
		node.Ratio = 0
		switch command.SizeMode {
		case PaneSizeRatio:
			if firstContains {
				node.Ratio = command.Ratio
			} else {
				node.Ratio = 1 - command.Ratio
			}
		case PaneSizeCells:
			node.FixedPaneID = command.Target.PaneID
			if node.Direction == SplitDirectionVertical {
				node.FixedCols = command.Cols
				if node.FixedCols <= 0 {
					node.FixedCols = command.Rows
				}
			} else {
				node.FixedRows = command.Rows
				if node.FixedRows <= 0 {
					node.FixedRows = command.Cols
				}
			}
		}
		return node, true
	}
	children := cloneSplitNodes(node.Children)
	changed := false
	for i, child := range children {
		children[i], changed = setSplitNodeSize(child, command)
		if changed {
			break
		}
	}
	node.Children = children
	return node, changed
}

func balanceSplitNode(node SplitNode) SplitNode {
	node.Ratio = 0
	node.BiasCells = 0
	node.FixedPaneID = ""
	node.FixedCols = 0
	node.FixedRows = 0
	for i, child := range node.Children {
		node.Children[i] = balanceSplitNode(child)
	}
	return node
}

func splitContainsPane(node SplitNode, paneID string) bool {
	if node.PaneID == paneID {
		return true
	}
	for _, child := range node.Children {
		if splitContainsPane(child, paneID) {
			return true
		}
	}
	return false
}

func splitDirectionMatchesResize(splitDirection SplitDirection, resizeDirection PaneResizeDirection) bool {
	switch splitDirection {
	case SplitDirectionVertical:
		return resizeDirection == PaneResizeLeft || resizeDirection == PaneResizeRight
	case SplitDirectionHorizontal:
		return resizeDirection == PaneResizeUp || resizeDirection == PaneResizeDown
	default:
		return false
	}
}

func splitDirectionForResize(resizeDirection PaneResizeDirection) (SplitDirection, bool) {
	switch resizeDirection {
	case PaneResizeLeft, PaneResizeRight:
		return SplitDirectionVertical, true
	case PaneResizeUp, PaneResizeDown:
		return SplitDirectionHorizontal, true
	default:
		return "", false
	}
}

func resizeBiasDelta(splitDirection SplitDirection, resizeDirection PaneResizeDirection, firstContains bool, delta int) int {
	switch splitDirection {
	case SplitDirectionVertical:
		if firstContains && resizeDirection == PaneResizeRight {
			return delta
		}
		if firstContains && resizeDirection == PaneResizeLeft {
			return -delta
		}
		if !firstContains && resizeDirection == PaneResizeLeft {
			return -delta
		}
		return delta
	case SplitDirectionHorizontal:
		if firstContains && resizeDirection == PaneResizeDown {
			return delta
		}
		if firstContains && resizeDirection == PaneResizeUp {
			return -delta
		}
		if !firstContains && resizeDirection == PaneResizeUp {
			return -delta
		}
		return delta
	default:
		return 0
	}
}

func cloneToasts(toasts []ToastState) []ToastState {
	if len(toasts) == 0 {
		return nil
	}
	cloned := make([]ToastState, len(toasts))
	copy(cloned, toasts)
	return cloned
}

func cloneWorkspace(workspace WorkspaceState) WorkspaceState {
	workspace.Tabs = cloneTabs(workspace.Tabs)
	return workspace
}

func cloneWorkspaces(workspaces []WorkspaceState) []WorkspaceState {
	if len(workspaces) == 0 {
		return nil
	}
	cloned := make([]WorkspaceState, len(workspaces))
	for index, workspace := range workspaces {
		cloned[index] = cloneWorkspace(workspace)
	}
	return cloned
}

func cloneFloatings(floatings []FloatingPaneState) []FloatingPaneState {
	if len(floatings) == 0 {
		return nil
	}
	cloned := make([]FloatingPaneState, len(floatings))
	copy(cloned, floatings)
	return cloned
}

func cloneTabs(tabs []TabState) []TabState {
	if len(tabs) == 0 {
		return nil
	}
	cloned := make([]TabState, len(tabs))
	for i, tab := range tabs {
		cloned[i] = tab
		cloned[i].Panes = clonePanes(tab.Panes)
		cloned[i].RootSplit = cloneSplitNode(tab.RootSplit)
	}
	return cloned
}

func clonePanes(panes []PaneState) []PaneState {
	if len(panes) == 0 {
		return nil
	}
	cloned := make([]PaneState, len(panes))
	copy(cloned, panes)
	return cloned
}

func cloneSplitNode(node SplitNode) SplitNode {
	node.Children = cloneSplitNodes(node.Children)
	return node
}

func cloneSplitNodes(nodes []SplitNode) []SplitNode {
	if len(nodes) == 0 {
		return nil
	}
	cloned := make([]SplitNode, len(nodes))
	for i, node := range nodes {
		cloned[i] = cloneSplitNode(node)
	}
	return cloned
}
