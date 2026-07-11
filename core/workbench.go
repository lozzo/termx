package core

import (
	"fmt"
	"strings"
	"sync"

	"github.com/lozzow/termx/internal/protocol"
)

type WorkbenchSnapshot = protocol.WorkbenchSnapshot
type WorkbenchWorkspace = protocol.WorkbenchWorkspace
type WorkbenchTab = protocol.WorkbenchTab
type WorkbenchPane = protocol.WorkbenchPane
type WorkbenchPaneKind = protocol.WorkbenchPaneKind
type WorkbenchSplitNode = protocol.WorkbenchSplitNode
type WorkbenchSplitDirection = protocol.WorkbenchSplitDirection
type WorkbenchMutationAction = protocol.WorkbenchMutationAction
type WorkbenchMutateParams = protocol.WorkbenchMutateParams
type WorkbenchMutateResult = protocol.WorkbenchMutateResult

type WorkbenchChanged struct {
	WorkspaceID string
	Version     uint64
	Action      string
	ResourceID  string
}

type workbenchStore struct {
	mu       sync.RWMutex
	snapshot WorkbenchSnapshot
	nextID   uint64
}

func newWorkbenchStore() *workbenchStore {
	return &workbenchStore{snapshot: defaultWorkbenchSnapshot(), nextID: 1}
}

func defaultWorkbenchSnapshot() WorkbenchSnapshot {
	return WorkbenchSnapshot{
		Version:           1,
		ActiveWorkspaceID: "workspace-main",
		Workspaces: []WorkbenchWorkspace{{
			ID:          "workspace-main",
			Name:        "main",
			ActiveTabID: "tab-main",
			Tabs: []WorkbenchTab{{
				ID:           "tab-main",
				Title:        "shell",
				ActivePaneID: "pane-main",
				Panes: []WorkbenchPane{{
					ID:    "pane-main",
					Title: "shell",
					Kind:  protocol.WorkbenchPaneTerminalLive,
				}},
				RootSplit: WorkbenchSplitNode{PaneID: "pane-main"},
			}},
		}},
	}
}

func (store *workbenchStore) get(workspaceID string) (WorkbenchSnapshot, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	snapshot := cloneWorkbenchSnapshot(store.snapshot)
	if strings.TrimSpace(workspaceID) == "" {
		return snapshot, nil
	}
	workspace, ok := findWorkbenchWorkspace(snapshot.Workspaces, workspaceID)
	if !ok {
		return WorkbenchSnapshot{}, ErrWorkbenchNotFound
	}
	snapshot.ActiveWorkspaceID = workspace.ID
	snapshot.Workspaces = []WorkbenchWorkspace{workspace}
	return snapshot, nil
}

func (store *workbenchStore) apply(params WorkbenchMutateParams) (WorkbenchMutateResult, WorkbenchChanged, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if params.CheckVersion && params.ExpectedVersion != store.snapshot.Version {
		return WorkbenchMutateResult{}, WorkbenchChanged{}, ErrWorkbenchVersionConflict
	}
	action := params.Action
	if action == "" {
		return WorkbenchMutateResult{}, WorkbenchChanged{}, ErrInvalidWorkbenchMutation
	}
	resourceID, workspaceID, err := store.mutateLocked(params)
	if err != nil {
		return WorkbenchMutateResult{}, WorkbenchChanged{}, err
	}
	store.snapshot.Version++
	snapshot := cloneWorkbenchSnapshot(store.snapshot)
	change := WorkbenchChanged{
		WorkspaceID: workspaceID,
		Version:     snapshot.Version,
		Action:      string(action),
		ResourceID:  resourceID,
	}
	return WorkbenchMutateResult{
		Snapshot:   snapshot,
		Action:     action,
		ResourceID: resourceID,
	}, change, nil
}

func (store *workbenchStore) mutateLocked(params WorkbenchMutateParams) (string, string, error) {
	switch params.Action {
	case protocol.WorkbenchMutationWorkspaceCreate:
		id := firstNonBlank(params.TargetID, store.nextGeneratedID("workspace"))
		if _, ok := findWorkbenchWorkspace(store.snapshot.Workspaces, id); ok {
			return "", "", ErrDuplicateWorkbenchResource
		}
		name := firstNonBlank(params.Name, id)
		tabID := store.nextGeneratedID("tab")
		paneID := store.nextGeneratedID("pane")
		workspace := WorkbenchWorkspace{
			ID:          id,
			Name:        name,
			ActiveTabID: tabID,
			Tabs: []WorkbenchTab{{
				ID:           tabID,
				Title:        "shell",
				ActivePaneID: paneID,
				Panes:        []WorkbenchPane{{ID: paneID, Title: "shell", Kind: protocol.WorkbenchPaneTerminalLive}},
				RootSplit:    WorkbenchSplitNode{PaneID: paneID},
			}},
		}
		store.snapshot.Workspaces = append(store.snapshot.Workspaces, workspace)
		store.snapshot.ActiveWorkspaceID = id
		return id, id, nil
	case protocol.WorkbenchMutationWorkspaceRename:
		workspace, err := store.workspaceForMutation(params.WorkspaceID)
		if err != nil {
			return "", "", err
		}
		name := strings.TrimSpace(params.Name)
		if name == "" {
			return "", "", ErrInvalidWorkbenchMutation
		}
		workspace.Name = name
		return workspace.ID, workspace.ID, nil
	case protocol.WorkbenchMutationWorkspaceDelete:
		id := firstNonBlank(params.WorkspaceID, params.TargetID, store.snapshot.ActiveWorkspaceID)
		if len(store.snapshot.Workspaces) <= 1 {
			return "", "", ErrInvalidWorkbenchMutation
		}
		index := indexWorkbenchWorkspace(store.snapshot.Workspaces, id)
		if index < 0 {
			return "", "", ErrWorkbenchNotFound
		}
		store.snapshot.Workspaces = append(store.snapshot.Workspaces[:index], store.snapshot.Workspaces[index+1:]...)
		if store.snapshot.ActiveWorkspaceID == id {
			store.snapshot.ActiveWorkspaceID = store.snapshot.Workspaces[minInt(index, len(store.snapshot.Workspaces)-1)].ID
		}
		return id, store.snapshot.ActiveWorkspaceID, nil
	case protocol.WorkbenchMutationWorkspaceFocus:
		id := firstNonBlank(params.WorkspaceID, params.TargetID)
		if id == "" {
			return "", "", ErrInvalidWorkbenchMutation
		}
		if _, ok := findWorkbenchWorkspace(store.snapshot.Workspaces, id); !ok {
			return "", "", ErrWorkbenchNotFound
		}
		store.snapshot.ActiveWorkspaceID = id
		return id, id, nil
	case protocol.WorkbenchMutationTabCreate:
		workspace, err := store.workspaceForMutation(params.WorkspaceID)
		if err != nil {
			return "", "", err
		}
		id := firstNonBlank(params.TargetID, store.nextGeneratedID("tab"))
		if _, ok := findWorkbenchTab(workspace.Tabs, id); ok {
			return "", "", ErrDuplicateWorkbenchResource
		}
		paneID := store.nextGeneratedID("pane")
		title := firstNonBlank(params.Name, id)
		tab := WorkbenchTab{
			ID:           id,
			Title:        title,
			ActivePaneID: paneID,
			Panes:        []WorkbenchPane{{ID: paneID, Title: "shell", Kind: protocol.WorkbenchPaneTerminalLive}},
			RootSplit:    WorkbenchSplitNode{PaneID: paneID},
		}
		workspace.Tabs = append(workspace.Tabs, tab)
		workspace.ActiveTabID = id
		return id, workspace.ID, nil
	case protocol.WorkbenchMutationTabRename:
		workspace, tab, err := store.tabForMutation(params.WorkspaceID, params.TabID)
		if err != nil {
			return "", "", err
		}
		title := strings.TrimSpace(params.Name)
		if title == "" {
			return "", "", ErrInvalidWorkbenchMutation
		}
		tab.Title = title
		return tab.ID, workspace.ID, nil
	case protocol.WorkbenchMutationTabDelete:
		workspace, err := store.workspaceForMutation(params.WorkspaceID)
		if err != nil {
			return "", "", err
		}
		if len(workspace.Tabs) <= 1 {
			return "", "", ErrInvalidWorkbenchMutation
		}
		id := firstNonBlank(params.TabID, params.TargetID, workspace.ActiveTabID)
		index := indexWorkbenchTab(workspace.Tabs, id)
		if index < 0 {
			return "", "", ErrWorkbenchNotFound
		}
		workspace.Tabs = append(workspace.Tabs[:index], workspace.Tabs[index+1:]...)
		if workspace.ActiveTabID == id {
			workspace.ActiveTabID = workspace.Tabs[minInt(index, len(workspace.Tabs)-1)].ID
		}
		return id, workspace.ID, nil
	case protocol.WorkbenchMutationTabFocus:
		workspace, err := store.workspaceForMutation(params.WorkspaceID)
		if err != nil {
			return "", "", err
		}
		id := firstNonBlank(params.TabID, params.TargetID)
		if id == "" {
			return "", "", ErrInvalidWorkbenchMutation
		}
		if _, ok := findWorkbenchTab(workspace.Tabs, id); !ok {
			return "", "", ErrWorkbenchNotFound
		}
		workspace.ActiveTabID = id
		return id, workspace.ID, nil
	case protocol.WorkbenchMutationPaneCreate:
		workspace, tab, err := store.tabForMutation(params.WorkspaceID, params.TabID)
		if err != nil {
			return "", "", err
		}
		id := firstNonBlank(params.TargetID, store.nextGeneratedID("pane"))
		if _, ok := findWorkbenchPane(tab.Panes, id); ok {
			return "", "", ErrDuplicateWorkbenchResource
		}
		tab.Panes = append(tab.Panes, newWorkbenchPane(id, params))
		if tab.RootSplit.PaneID == "" && len(tab.RootSplit.Children) == 0 {
			tab.RootSplit = WorkbenchSplitNode{PaneID: id}
		}
		tab.ActivePaneID = id
		return id, workspace.ID, nil
	case protocol.WorkbenchMutationPaneRename:
		workspace, _, pane, err := store.paneForMutation(params.WorkspaceID, params.TabID, params.PaneID)
		if err != nil {
			return "", "", err
		}
		title := strings.TrimSpace(params.Name)
		if title == "" {
			return "", "", ErrInvalidWorkbenchMutation
		}
		pane.Title = title
		return pane.ID, workspace.ID, nil
	case protocol.WorkbenchMutationPaneDelete:
		workspace, tab, err := store.tabForMutation(params.WorkspaceID, params.TabID)
		if err != nil {
			return "", "", err
		}
		id := firstNonBlank(params.PaneID, params.TargetID, tab.ActivePaneID)
		if len(tab.Panes) <= 1 {
			return "", "", ErrInvalidWorkbenchMutation
		}
		index := indexWorkbenchPane(tab.Panes, id)
		if index < 0 {
			return "", "", ErrWorkbenchNotFound
		}
		tab.Panes = append(tab.Panes[:index], tab.Panes[index+1:]...)
		tab.RootSplit = removePaneFromSplit(tab.RootSplit, id)
		if tab.ActivePaneID == id {
			tab.ActivePaneID = tab.Panes[minInt(index, len(tab.Panes)-1)].ID
		}
		return id, workspace.ID, nil
	case protocol.WorkbenchMutationPaneFocus:
		workspace, tab, _, err := store.paneForMutation(params.WorkspaceID, params.TabID, params.PaneID)
		if err != nil {
			return "", "", err
		}
		tab.ActivePaneID = firstNonBlank(params.PaneID, params.TargetID)
		return tab.ActivePaneID, workspace.ID, nil
	case protocol.WorkbenchMutationPaneSplit:
		workspace, tab, targetPane, err := store.paneForMutation(params.WorkspaceID, params.TabID, params.PaneID)
		if err != nil {
			return "", "", err
		}
		newID := firstNonBlank(params.TargetID, store.nextGeneratedID("pane"))
		if _, ok := findWorkbenchPane(tab.Panes, newID); ok {
			return "", "", ErrDuplicateWorkbenchResource
		}
		direction := params.SplitDirection
		if direction == "" {
			direction = protocol.WorkbenchSplitHorizontal
		}
		if direction != protocol.WorkbenchSplitHorizontal && direction != protocol.WorkbenchSplitVertical {
			return "", "", ErrInvalidWorkbenchMutation
		}
		tab.Panes = append(tab.Panes, newWorkbenchPane(newID, params))
		tab.RootSplit = splitPaneNode(tab.RootSplit, targetPane.ID, newID, direction)
		tab.ActivePaneID = newID
		return newID, workspace.ID, nil
	case protocol.WorkbenchMutationPaneBindTerminal:
		workspace, _, pane, err := store.paneForMutation(params.WorkspaceID, params.TabID, params.PaneID)
		if err != nil {
			return "", "", err
		}
		terminalID := strings.TrimSpace(params.TerminalID)
		if terminalID == "" {
			return "", "", ErrInvalidWorkbenchMutation
		}
		pane.TerminalID = terminalID
		pane.Kind = protocol.WorkbenchPaneTerminalLive
		return pane.ID, workspace.ID, nil
	default:
		return "", "", ErrInvalidWorkbenchMutation
	}
}

func (store *workbenchStore) nextGeneratedID(prefix string) string {
	store.nextID++
	return fmt.Sprintf("%s-%d", prefix, store.nextID)
}

func (store *workbenchStore) workspaceForMutation(workspaceID string) (*WorkbenchWorkspace, error) {
	id := firstNonBlank(workspaceID, store.snapshot.ActiveWorkspaceID)
	for i := range store.snapshot.Workspaces {
		if store.snapshot.Workspaces[i].ID == id {
			return &store.snapshot.Workspaces[i], nil
		}
	}
	return nil, ErrWorkbenchNotFound
}

func (store *workbenchStore) tabForMutation(workspaceID string, tabID string) (*WorkbenchWorkspace, *WorkbenchTab, error) {
	workspace, err := store.workspaceForMutation(workspaceID)
	if err != nil {
		return nil, nil, err
	}
	id := firstNonBlank(tabID, workspace.ActiveTabID)
	for i := range workspace.Tabs {
		if workspace.Tabs[i].ID == id {
			return workspace, &workspace.Tabs[i], nil
		}
	}
	return nil, nil, ErrWorkbenchNotFound
}

func (store *workbenchStore) paneForMutation(workspaceID string, tabID string, paneID string) (*WorkbenchWorkspace, *WorkbenchTab, *WorkbenchPane, error) {
	workspace, tab, err := store.tabForMutation(workspaceID, tabID)
	if err != nil {
		return nil, nil, nil, err
	}
	id := firstNonBlank(paneID, tab.ActivePaneID)
	for i := range tab.Panes {
		if tab.Panes[i].ID == id {
			return workspace, tab, &tab.Panes[i], nil
		}
	}
	return nil, nil, nil, ErrWorkbenchNotFound
}

func newWorkbenchPane(id string, params WorkbenchMutateParams) WorkbenchPane {
	kind := params.Kind
	if kind == "" {
		kind = protocol.WorkbenchPaneTerminalLive
	}
	return WorkbenchPane{
		ID:         id,
		Title:      firstNonBlank(params.Name, "shell"),
		Kind:       kind,
		TerminalID: strings.TrimSpace(params.TerminalID),
	}
}

func splitPaneNode(root WorkbenchSplitNode, targetPaneID string, newPaneID string, direction WorkbenchSplitDirection) WorkbenchSplitNode {
	if root.PaneID == "" && len(root.Children) == 0 {
		return WorkbenchSplitNode{PaneID: newPaneID}
	}
	if root.PaneID == targetPaneID {
		return WorkbenchSplitNode{
			Direction: direction,
			Children:  []WorkbenchSplitNode{{PaneID: targetPaneID}, {PaneID: newPaneID}},
			Ratio:     0.5,
		}
	}
	for i := range root.Children {
		root.Children[i] = splitPaneNode(root.Children[i], targetPaneID, newPaneID, direction)
	}
	return root
}

func removePaneFromSplit(root WorkbenchSplitNode, paneID string) WorkbenchSplitNode {
	if root.PaneID == paneID {
		return WorkbenchSplitNode{}
	}
	children := make([]WorkbenchSplitNode, 0, len(root.Children))
	for _, child := range root.Children {
		child = removePaneFromSplit(child, paneID)
		if child.PaneID != "" || len(child.Children) > 0 {
			children = append(children, child)
		}
	}
	if len(children) == 1 {
		return children[0]
	}
	root.Children = children
	return root
}

func cloneWorkbenchSnapshot(snapshot WorkbenchSnapshot) WorkbenchSnapshot {
	out := WorkbenchSnapshot{
		Version:           snapshot.Version,
		ActiveWorkspaceID: snapshot.ActiveWorkspaceID,
		Workspaces:        make([]WorkbenchWorkspace, len(snapshot.Workspaces)),
	}
	for i, workspace := range snapshot.Workspaces {
		out.Workspaces[i] = cloneWorkbenchWorkspace(workspace)
	}
	return out
}

func cloneWorkbenchWorkspace(workspace WorkbenchWorkspace) WorkbenchWorkspace {
	out := WorkbenchWorkspace{
		ID:          workspace.ID,
		Name:        workspace.Name,
		ActiveTabID: workspace.ActiveTabID,
		Tabs:        make([]WorkbenchTab, len(workspace.Tabs)),
	}
	for i, tab := range workspace.Tabs {
		out.Tabs[i] = cloneWorkbenchTab(tab)
	}
	return out
}

func cloneWorkbenchTab(tab WorkbenchTab) WorkbenchTab {
	out := WorkbenchTab{
		ID:           tab.ID,
		Title:        tab.Title,
		ActivePaneID: tab.ActivePaneID,
		Panes:        append([]WorkbenchPane(nil), tab.Panes...),
		RootSplit:    cloneWorkbenchSplit(tab.RootSplit),
	}
	return out
}

func cloneWorkbenchSplit(node WorkbenchSplitNode) WorkbenchSplitNode {
	out := node
	if len(node.Children) > 0 {
		out.Children = make([]WorkbenchSplitNode, len(node.Children))
		for i, child := range node.Children {
			out.Children[i] = cloneWorkbenchSplit(child)
		}
	}
	return out
}

func findWorkbenchWorkspace(workspaces []WorkbenchWorkspace, id string) (WorkbenchWorkspace, bool) {
	for _, workspace := range workspaces {
		if workspace.ID == id {
			return workspace, true
		}
	}
	return WorkbenchWorkspace{}, false
}

func indexWorkbenchWorkspace(workspaces []WorkbenchWorkspace, id string) int {
	for i, workspace := range workspaces {
		if workspace.ID == id {
			return i
		}
	}
	return -1
}

func findWorkbenchTab(tabs []WorkbenchTab, id string) (WorkbenchTab, bool) {
	for _, tab := range tabs {
		if tab.ID == id {
			return tab, true
		}
	}
	return WorkbenchTab{}, false
}

func indexWorkbenchTab(tabs []WorkbenchTab, id string) int {
	for i, tab := range tabs {
		if tab.ID == id {
			return i
		}
	}
	return -1
}

func findWorkbenchPane(panes []WorkbenchPane, id string) (WorkbenchPane, bool) {
	for _, pane := range panes {
		if pane.ID == id {
			return pane, true
		}
	}
	return WorkbenchPane{}, false
}

func indexWorkbenchPane(panes []WorkbenchPane, id string) int {
	for i, pane := range panes {
		if pane.ID == id {
			return i
		}
	}
	return -1
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
