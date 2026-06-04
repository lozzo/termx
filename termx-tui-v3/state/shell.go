package state

import (
	"fmt"
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
	InteractionModeNormal   InteractionMode = ""
	InteractionModePane     InteractionMode = "pane"
	InteractionModeResize   InteractionMode = "resize"
	InteractionModeGlobal   InteractionMode = "global"
	InteractionModeFloating InteractionMode = "floating"
)

// ShellStore 保存 Workbench 外壳相关的 reducer-owned 产品状态。
// 它只描述用户可操作的结构，不计算最终屏幕矩形，也不画 panel chrome。
type ShellStore struct {
	Workspace         WorkspaceState
	PanelPresentation PanelPresentation
	ActivePaneID      string
	ZoomedPaneID      string
	InteractionMode   InteractionMode
	HeaderVisible     bool
	FooterVisible     bool
	Overlay           OverlayState
	Toasts            []ToastState
	nextToastSeq      uint64
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

func DefaultShell() ShellStore {
	return (ShellStore{}).EnsureDefaults()
}

func (store ShellStore) EnsureDefaults() ShellStore {
	store.Workspace = cloneWorkspace(store.Workspace)
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
	store.Workspace = store.Workspace.ensureActive(store.ActivePaneID)
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
	case InteractionModeNormal, InteractionModePane, InteractionModeResize, InteractionModeGlobal, InteractionModeFloating:
		store.InteractionMode = mode
	}
	return store
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
		DismissAfterTicks: spec.DismissAfterTicks,
	}
	store.Toasts = append(cloneToasts(store.Toasts), toast)
	return store
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
	if delta < 0 {
		delta = -delta
	}
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
		Active:        false,
		Summary:       "float:0",
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
	return fmt.Sprintf("tabs:%d panes:%d float:0", len(workspace.Tabs), workspacePaneCount(workspace))
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
