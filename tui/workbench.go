package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/lozzow/termx/protocol"
)

// Workbench 是工作区树的根对象，负责持有当前迁移阶段的工作区集合与当前工作区。
type Workbench struct {
	current Workspace
	store   map[string]Workspace
	order   []string
	active  int
}

func NewWorkbench(workspace Workspace) *Workbench {
	owned := cloneWorkspace(workspace)
	if owned == nil {
		owned = cloneWorkspace(defaultWorkspace("main"))
	}
	name := strings.TrimSpace(owned.Name)
	if name == "" {
		name = "main"
		owned.Name = name
	}
	return &Workbench{
		current: *cloneWorkspace(*owned),
		store: map[string]Workspace{
			name: *cloneWorkspace(*owned),
		},
		order:  []string{name},
		active: 0,
	}
}

func (w *Workbench) Current() *Workspace {
	if w == nil {
		return nil
	}
	w.ensureInitialized()
	return &w.current
}

func (w *Workbench) CurrentWorkspace() *Workspace {
	return w.Current()
}

func (w *Workbench) CurrentTab() *Tab {
	workspace := w.CurrentWorkspace()
	if workspace == nil {
		return nil
	}
	if workspace.ActiveTab < 0 || workspace.ActiveTab >= len(workspace.Tabs) {
		return nil
	}
	return workspace.Tabs[workspace.ActiveTab]
}

func (w *Workbench) ActivePane() *Pane {
	tab := w.CurrentTab()
	if tab == nil {
		return nil
	}
	return tab.Panes[tab.ActivePaneID]
}

type WorkbenchVisibleState struct {
	Workspace  *Workspace
	Tab        *Tab
	ActivePane *Pane
}

func (w *Workbench) VisibleState() WorkbenchVisibleState {
	workspace := w.CurrentWorkspace()
	state := WorkbenchVisibleState{Workspace: workspace}
	if workspace == nil {
		return state
	}
	state.Tab = w.CurrentTab()
	if state.Tab != nil {
		state.ActivePane = state.Tab.Panes[state.Tab.ActivePaneID]
	}
	return state
}

func (w *Workbench) FindPane(paneID string) *Pane {
	workspace := w.CurrentWorkspace()
	if workspace == nil {
		return nil
	}
	return findPane(workspace.Tabs, paneID)
}

func (w *Workbench) ActivateTab(index int) bool {
	workspace := w.CurrentWorkspace()
	if workspace == nil {
		return false
	}
	if !workspace.ActivateTab(index) {
		return false
	}
	w.SnapshotCurrent()
	return true
}

func (w *Workbench) FocusPane(paneID string) bool {
	workspace := w.CurrentWorkspace()
	if workspace == nil {
		return false
	}
	if !workspace.FocusPane(paneID) {
		return false
	}
	w.SnapshotCurrent()
	return true
}

func (w *Workbench) RemovePane(paneID string) (tabRemoved bool, workspaceEmpty bool, terminalID string) {
	workspace := w.CurrentWorkspace()
	if workspace == nil {
		return false, false, ""
	}
	tabRemoved, workspaceEmpty, terminalID = workspace.RemovePane(paneID)
	w.SnapshotCurrent()
	return tabRemoved, workspaceEmpty, terminalID
}

func (w *Workbench) Order() []string {
	if w == nil {
		return nil
	}
	w.ensureInitialized()
	return append([]string(nil), w.order...)
}

func (w *Workbench) ActiveWorkspaceIndex() int {
	if w == nil {
		return 0
	}
	w.ensureInitialized()
	return w.active
}

func (w *Workbench) CloneStore() map[string]Workspace {
	if w == nil {
		return nil
	}
	w.ensureInitialized()
	store := make(map[string]Workspace, len(w.store))
	for name, workspace := range w.store {
		store[name] = *cloneWorkspace(workspace)
	}
	return store
}

func (w *Workbench) SetOrder(order []string) {
	if w == nil {
		return
	}
	w.ensureInitialized()

	normalized := make([]string, 0, len(order))
	seen := make(map[string]struct{}, len(order))
	prunedStore := make(map[string]Workspace, len(order))
	for idx, name := range order {
		name = normalizeWorkspaceName(name, fmt.Sprintf("workspace-%d", idx+1))
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
		workspace, ok := w.store[name]
		if !ok {
			workspace = defaultWorkspace(name)
		}
		prunedStore[name] = *cloneWorkspace(workspace)
	}
	if len(normalized) == 0 {
		currentName := normalizeWorkspaceName(w.current.Name, "main")
		normalized = []string{currentName}
		workspace, ok := w.store[currentName]
		if !ok {
			workspace = *cloneWorkspace(w.current)
		}
		prunedStore[currentName] = *cloneWorkspace(workspace)
	}
	w.order = normalized
	w.store = prunedStore

	activeName := normalizeWorkspaceName(w.current.Name, w.order[0])
	if idx := slices.Index(w.order, activeName); idx >= 0 {
		w.active = idx
	} else if w.active < 0 {
		w.active = 0
	} else if w.active >= len(w.order) {
		w.active = len(w.order) - 1
	}
	w.loadCurrentFromStore(w.order[w.active])
}

func (w *Workbench) SnapshotCurrent() {
	if w == nil {
		return
	}
	if w.store == nil {
		w.store = make(map[string]Workspace)
	}
	currentName := normalizeWorkspaceName(w.current.Name, "main")
	if currentName != w.current.Name {
		w.current.Name = currentName
	}
	if len(w.order) == 0 {
		w.order = []string{currentName}
		w.active = 0
	}
	if w.active < 0 || w.active >= len(w.order) {
		w.active = 0
	}

	previousName := normalizeWorkspaceName(w.order[w.active], currentName)
	name := normalizeWorkspaceName(w.current.Name, previousName)
	w.current.Name = name
	if previousName != "" && previousName != name {
		delete(w.store, previousName)
		w.order[w.active] = name
	}
	if idx := slices.Index(w.order, name); idx >= 0 {
		w.active = idx
	} else {
		w.order = append(w.order, name)
		w.active = len(w.order) - 1
	}
	w.store[name] = *cloneWorkspace(w.current)
	w.loadCurrentFromStore(name)
}

func (w *Workbench) SwitchTo(name string) error {
	if w == nil {
		return fmt.Errorf("workbench is nil")
	}
	w.ensureInitialized()
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("workspace name is empty")
	}
	w.SnapshotCurrent()
	if _, ok := w.store[name]; !ok {
		w.store[name] = defaultWorkspace(name)
	}
	if idx := slices.Index(w.order, name); idx >= 0 {
		w.active = idx
	} else {
		w.order = append(w.order, name)
		w.active = len(w.order) - 1
	}
	w.loadCurrentFromStore(name)
	return nil
}

func (w *Workbench) ensureInitialized() {
	if w == nil {
		return
	}
	if w.store == nil {
		w.store = make(map[string]Workspace)
	}
	currentName := normalizeWorkspaceName(w.current.Name, "main")
	if currentName != w.current.Name {
		w.current.Name = currentName
	}
	if len(w.order) == 0 {
		w.order = []string{currentName}
		w.active = 0
	}
	if _, ok := w.store[currentName]; !ok {
		w.store[currentName] = *cloneWorkspace(w.current)
	}
	if w.active < 0 || w.active >= len(w.order) {
		w.active = 0
	}
	activeName := normalizeWorkspaceName(w.order[w.active], currentName)
	w.order[w.active] = activeName
	if _, ok := w.store[activeName]; !ok {
		w.store[activeName] = *cloneWorkspace(w.current)
	}
	w.loadCurrentFromStore(activeName)
}

func (w *Workbench) loadCurrentFromStore(name string) {
	workspace, ok := w.store[name]
	if !ok {
		workspace = defaultWorkspace(name)
		w.store[name] = *cloneWorkspace(workspace)
	}
	w.current = *cloneWorkspace(workspace)
}

func normalizeWorkspaceName(name, fallback string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	fallback = strings.TrimSpace(fallback)
	if fallback != "" {
		return fallback
	}
	return "main"
}

func defaultWorkspace(name string) Workspace {
	name = normalizeWorkspaceName(name, "main")
	return Workspace{Name: name, Tabs: []*Tab{newTab("1")}, ActiveTab: 0}
}

func cloneWorkspace(workspace Workspace) *Workspace {
	owned := workspace
	if workspace.Tabs != nil {
		owned.Tabs = make([]*Tab, len(workspace.Tabs))
		for i, tab := range workspace.Tabs {
			owned.Tabs[i] = cloneTab(tab)
		}
	}
	return &owned
}

func cloneTab(tab *Tab) *Tab {
	if tab == nil {
		return nil
	}
	owned := *tab
	owned.renderCache = nil
	owned.Root = cloneLayoutNode(tab.Root)
	if tab.Panes != nil {
		owned.Panes = make(map[string]*Pane, len(tab.Panes))
		for id, pane := range tab.Panes {
			owned.Panes[id] = clonePane(pane)
		}
	}
	if tab.Floating != nil {
		owned.Floating = make([]*FloatingPane, len(tab.Floating))
		for i, floating := range tab.Floating {
			owned.Floating[i] = cloneFloatingPane(floating)
		}
	}
	return &owned
}

func cloneLayoutNode(node *LayoutNode) *LayoutNode {
	if node == nil {
		return nil
	}
	owned := *node
	owned.First = cloneLayoutNode(node.First)
	owned.Second = cloneLayoutNode(node.Second)
	return &owned
}

func clonePane(pane *Pane) *Pane {
	if pane == nil {
		return nil
	}
	owned := *pane
	owned.Viewport = cloneViewport(pane.Viewport)
	return &owned
}

func cloneViewport(viewport *Viewport) *Viewport {
	if viewport == nil {
		return nil
	}
	owned := *viewport
	owned.VTerm = nil
	owned.Snapshot = cloneProtocolSnapshot(viewport.Snapshot)
	if viewport.Command != nil {
		owned.Command = append([]string(nil), viewport.Command...)
	}
	if viewport.Tags != nil {
		owned.Tags = cloneStringMap(viewport.Tags)
	}
	if viewport.ExitCode != nil {
		exitCode := *viewport.ExitCode
		owned.ExitCode = &exitCode
	}
	owned.stopStream = nil
	owned.cellCache = nil
	owned.cellVersion = 0
	owned.viewportCache = nil
	owned.viewportOffset = Point{}
	owned.viewportWidth = 0
	owned.viewportHeight = 0
	owned.viewportVersion = 0
	owned.renderDirty = false
	owned.live = false
	owned.syncLost = false
	owned.droppedBytes = 0
	owned.recovering = false
	owned.catchingUp = false
	owned.dirtyTicks = 0
	owned.cleanTicks = 0
	owned.skipTick = false
	owned.dirtyRowsKnown = false
	owned.dirtyRowStart = 0
	owned.dirtyRowEnd = 0
	owned.dirtyColsKnown = false
	owned.dirtyColStart = 0
	owned.dirtyColEnd = 0
	return &owned
}

func cloneProtocolSnapshot(snapshot *protocol.Snapshot) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	owned := *snapshot
	owned.Screen.Cells = cloneProtocolCellGrid(snapshot.Screen.Cells)
	owned.Scrollback = cloneProtocolCellGrid(snapshot.Scrollback)
	return &owned
}

func cloneProtocolCellGrid(grid [][]protocol.Cell) [][]protocol.Cell {
	if grid == nil {
		return nil
	}
	owned := make([][]protocol.Cell, len(grid))
	for i, row := range grid {
		if row != nil {
			owned[i] = append([]protocol.Cell(nil), row...)
		}
	}
	return owned
}

func cloneFloatingPane(floating *FloatingPane) *FloatingPane {
	if floating == nil {
		return nil
	}
	owned := *floating
	return &owned
}
