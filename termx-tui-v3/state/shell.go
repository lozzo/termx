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

// ShellStore 保存 Workbench 外壳相关的 reducer-owned 产品状态。
// 它只描述用户可操作的结构，不计算最终屏幕矩形，也不画 panel chrome。
type ShellStore struct {
	Workspace         WorkspaceState
	PanelPresentation PanelPresentation
	ActivePaneID      string
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
}

type OverlayState struct {
	Kind     OverlayKind
	Open     bool
	TargetID string
	Query    string
}

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
		Kind:     OverlayTerminalPicker,
		Open:     true,
		TargetID: store.ActivePaneID,
	}
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
	tab.RootSplit = SplitNode{
		Direction: direction,
		Children: []SplitNode{
			{PaneID: previousActive},
			{PaneID: newPane.ID},
		},
	}
	tab.ActivePaneID = newPane.ID
	store.ActivePaneID = newPane.ID
	store.Workspace = store.Workspace.ensureActive(store.ActivePaneID)
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
