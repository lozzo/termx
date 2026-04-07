package workbenchops

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/workbenchdoc"
)

type OpKind string

const (
	OpCreateWorkspace          OpKind = "create-workspace"
	OpRenameWorkspace          OpKind = "rename-workspace"
	OpDeleteWorkspace          OpKind = "delete-workspace"
	OpCreateTab                OpKind = "create-tab"
	OpRenameTab                OpKind = "rename-tab"
	OpDeleteTab                OpKind = "delete-tab"
	OpCreateFirstPane          OpKind = "create-first-pane"
	OpSplitPane                OpKind = "split-pane"
	OpClosePane                OpKind = "close-pane"
	OpFocusPane                OpKind = "focus-pane"
	OpBindTerminal             OpKind = "bind-terminal"
	OpDetachTerminal           OpKind = "detach-terminal"
	OpReplaceTerminal          OpKind = "replace-terminal"
	OpPromoteFloating          OpKind = "promote-floating"
	OpDemoteFloating           OpKind = "demote-floating"
	OpToggleFloatingVisibility OpKind = "toggle-floating-visibility"
)

type Op struct {
	Kind          OpKind                      `json:"op"`
	WorkspaceName string                      `json:"workspace_name,omitempty"`
	NewName       string                      `json:"new_name,omitempty"`
	TabID         string                      `json:"tab_id,omitempty"`
	TabName       string                      `json:"tab_name,omitempty"`
	PaneID        string                      `json:"pane_id,omitempty"`
	NewPaneID     string                      `json:"new_pane_id,omitempty"`
	TerminalID    string                      `json:"terminal_id,omitempty"`
	Direction     workbenchdoc.SplitDirection `json:"direction,omitempty"`
}

func Apply(doc *workbenchdoc.Doc, ops []Op) (*workbenchdoc.Doc, error) {
	if doc == nil {
		return nil, fmt.Errorf("workbenchops: nil document")
	}
	next := doc.Clone()
	if next.Workspaces == nil {
		next.Workspaces = make(map[string]*workbenchdoc.Workspace)
	}
	for _, op := range ops {
		if err := applyOne(next, op); err != nil {
			return nil, err
		}
	}
	normalizeDoc(next)
	return next, nil
}

func applyOne(doc *workbenchdoc.Doc, op Op) error {
	switch op.Kind {
	case OpCreateWorkspace:
		return createWorkspace(doc, op.NewName)
	case OpRenameWorkspace:
		return renameWorkspace(doc, op.WorkspaceName, op.NewName)
	case OpDeleteWorkspace:
		return deleteWorkspace(doc, op.WorkspaceName)
	case OpCreateTab:
		return createTab(doc, op.WorkspaceName, op.TabID, op.TabName)
	case OpRenameTab:
		_, ws, tab, err := findTab(doc, op.TabID)
		if err != nil {
			return err
		}
		nextName := strings.TrimSpace(op.NewName)
		if nextName == "" {
			return fmt.Errorf("workbenchops: tab name must not be empty")
		}
		if strings.TrimSpace(tab.Name) == nextName {
			return nil
		}
		if workspaceHasTabName(ws, nextName, op.TabID) {
			return fmt.Errorf("workbenchops: tab name %q already exists in workspace %q", nextName, ws.Name)
		}
		tab.Name = nextName
		return nil
	case OpDeleteTab:
		return deleteTab(doc, op.TabID)
	case OpCreateFirstPane:
		return createFirstPane(doc, op.TabID, op.PaneID)
	case OpSplitPane:
		return splitPane(doc, op.TabID, op.PaneID, op.NewPaneID, op.Direction)
	case OpClosePane:
		return closePane(doc, op.TabID, op.PaneID)
	case OpFocusPane:
		return focusPane(doc, op.TabID, op.PaneID)
	case OpBindTerminal:
		return bindTerminal(doc, op.TabID, op.PaneID, op.TerminalID)
	case OpDetachTerminal:
		return bindTerminal(doc, op.TabID, op.PaneID, "")
	case OpReplaceTerminal:
		return bindTerminal(doc, op.TabID, op.PaneID, op.TerminalID)
	case OpPromoteFloating:
		return promoteFloating(doc, op.TabID, op.PaneID)
	case OpDemoteFloating:
		return demoteFloating(doc, op.TabID, op.PaneID)
	case OpToggleFloatingVisibility:
		_, _, tab, err := findTab(doc, op.TabID)
		if err != nil {
			return err
		}
		tab.FloatingVisible = !tab.FloatingVisible
		return nil
	default:
		return fmt.Errorf("workbenchops: unsupported op %q", op.Kind)
	}
}

func createWorkspace(doc *workbenchdoc.Doc, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("workbenchops: workspace name must not be empty")
	}
	if doc.Workspaces[name] != nil {
		return fmt.Errorf("workbenchops: workspace %q already exists", name)
	}
	doc.Workspaces[name] = &workbenchdoc.Workspace{Name: name, ActiveTab: -1}
	doc.WorkspaceOrder = append(doc.WorkspaceOrder, name)
	if doc.CurrentWorkspace == "" {
		doc.CurrentWorkspace = name
	}
	return nil
}

func renameWorkspace(doc *workbenchdoc.Doc, oldName, newName string) error {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" {
		return fmt.Errorf("workbenchops: workspace name must not be empty")
	}
	if oldName == newName {
		return nil
	}
	ws := doc.Workspaces[oldName]
	if ws == nil {
		return fmt.Errorf("workbenchops: workspace %q not found", oldName)
	}
	if doc.Workspaces[newName] != nil {
		return fmt.Errorf("workbenchops: workspace %q already exists", newName)
	}
	delete(doc.Workspaces, oldName)
	ws.Name = newName
	doc.Workspaces[newName] = ws
	for i, name := range doc.WorkspaceOrder {
		if name == oldName {
			doc.WorkspaceOrder[i] = newName
		}
	}
	if doc.CurrentWorkspace == oldName {
		doc.CurrentWorkspace = newName
	}
	return nil
}

func deleteWorkspace(doc *workbenchdoc.Doc, name string) error {
	if doc.Workspaces[name] == nil {
		return fmt.Errorf("workbenchops: workspace %q not found", name)
	}
	delete(doc.Workspaces, name)
	kept := doc.WorkspaceOrder[:0]
	for _, existing := range doc.WorkspaceOrder {
		if existing != name {
			kept = append(kept, existing)
		}
	}
	doc.WorkspaceOrder = kept
	if doc.CurrentWorkspace == name {
		doc.CurrentWorkspace = ""
		if len(doc.WorkspaceOrder) > 0 {
			doc.CurrentWorkspace = doc.WorkspaceOrder[0]
		}
	}
	return nil
}

func createTab(doc *workbenchdoc.Doc, workspaceName, tabID, tabName string) error {
	ws := doc.Workspaces[workspaceName]
	if ws == nil {
		return fmt.Errorf("workbenchops: workspace %q not found", workspaceName)
	}
	if strings.TrimSpace(tabID) == "" {
		return fmt.Errorf("workbenchops: tab id must not be empty")
	}
	nextName := strings.TrimSpace(tabName)
	for _, existing := range ws.Tabs {
		if existing == nil {
			continue
		}
		if existing.ID == tabID {
			return fmt.Errorf("workbenchops: tab %q already exists", tabID)
		}
		if nextName != "" && strings.TrimSpace(existing.Name) == nextName {
			return fmt.Errorf("workbenchops: tab name %q already exists in workspace %q", nextName, workspaceName)
		}
	}
	ws.Tabs = append(ws.Tabs, &workbenchdoc.Tab{
		ID:    tabID,
		Name:  nextName,
		Panes: make(map[string]*workbenchdoc.Pane),
	})
	ws.ActiveTab = len(ws.Tabs) - 1
	return nil
}

func workspaceHasTabName(ws *workbenchdoc.Workspace, name, exceptTabID string) bool {
	name = strings.TrimSpace(name)
	if ws == nil || name == "" {
		return false
	}
	for _, tab := range ws.Tabs {
		if tab == nil || tab.ID == exceptTabID {
			continue
		}
		if strings.TrimSpace(tab.Name) == name {
			return true
		}
	}
	return false
}

func deleteTab(doc *workbenchdoc.Doc, tabID string) error {
	wsName, ws, _, err := findTab(doc, tabID)
	if err != nil {
		return err
	}
	for i, tab := range ws.Tabs {
		if tab == nil || tab.ID != tabID {
			continue
		}
		ws.Tabs = append(ws.Tabs[:i], ws.Tabs[i+1:]...)
		switch {
		case len(ws.Tabs) == 0:
			ws.ActiveTab = -1
		case ws.ActiveTab > i:
			ws.ActiveTab--
		case ws.ActiveTab >= len(ws.Tabs) || ws.Tabs[ws.ActiveTab] == nil:
			ws.ActiveTab = activeTabIndex(ws)
		}
		if len(ws.Tabs) == 0 && wsName == doc.CurrentWorkspace {
			doc.CurrentWorkspace = wsName
		}
		return nil
	}
	return fmt.Errorf("workbenchops: tab %q not found", tabID)
}

func createFirstPane(doc *workbenchdoc.Doc, tabID, paneID string) error {
	_, _, tab, err := findTab(doc, tabID)
	if err != nil {
		return err
	}
	if tab.Root != nil {
		return fmt.Errorf("workbenchops: tab %q already has a root", tabID)
	}
	if strings.TrimSpace(paneID) == "" {
		return fmt.Errorf("workbenchops: pane id must not be empty")
	}
	if tab.Panes == nil {
		tab.Panes = make(map[string]*workbenchdoc.Pane)
	}
	tab.Panes[paneID] = &workbenchdoc.Pane{ID: paneID}
	tab.Root = workbenchdoc.NewLeaf(paneID)
	tab.ActivePaneID = paneID
	return nil
}

func splitPane(doc *workbenchdoc.Doc, tabID, paneID, newPaneID string, dir workbenchdoc.SplitDirection) error {
	_, _, tab, err := findTab(doc, tabID)
	if err != nil {
		return err
	}
	if tab.Panes[paneID] == nil {
		return fmt.Errorf("workbenchops: pane %q not found in tab %q", paneID, tabID)
	}
	if tab.Panes[newPaneID] != nil {
		return fmt.Errorf("workbenchops: pane %q already exists in tab %q", newPaneID, tabID)
	}
	newRoot, replaced := replaceLeaf(tab.Root, paneID, newPaneID, dir)
	if !replaced {
		return fmt.Errorf("workbenchops: no layout leaf found for pane %q in tab %q", paneID, tabID)
	}
	tab.Root = newRoot
	if tab.Panes == nil {
		tab.Panes = make(map[string]*workbenchdoc.Pane)
	}
	tab.Panes[newPaneID] = &workbenchdoc.Pane{ID: newPaneID}
	tab.ActivePaneID = newPaneID
	return nil
}

func closePane(doc *workbenchdoc.Doc, tabID, paneID string) error {
	wsName, ws, tab, err := findTab(doc, tabID)
	if err != nil {
		return err
	}
	if tab.Panes[paneID] == nil {
		return fmt.Errorf("workbenchops: pane %q not found in tab %q", paneID, tabID)
	}
	delete(tab.Panes, paneID)
	tab.Floating = removeFloating(tab.Floating, paneID)
	if len(tab.Floating) == 0 {
		tab.FloatingVisible = false
	}
	if tab.Root != nil {
		tab.Root = tab.Root.Remove(paneID)
	}
	if tab.ZoomedPaneID == paneID {
		tab.ZoomedPaneID = ""
	}
	tab.ActivePaneID = activePaneID(tab)
	if len(tab.Panes) == 0 {
		return deleteTab(doc, tabID)
	}
	if wsName == doc.CurrentWorkspace && ws == nil {
		doc.CurrentWorkspace = wsName
	}
	return nil
}

func focusPane(doc *workbenchdoc.Doc, tabID, paneID string) error {
	_, _, tab, err := findTab(doc, tabID)
	if err != nil {
		return err
	}
	if tab.Panes[paneID] == nil {
		return fmt.Errorf("workbenchops: pane %q not found in tab %q", paneID, tabID)
	}
	tab.ActivePaneID = paneID
	return nil
}

func bindTerminal(doc *workbenchdoc.Doc, tabID, paneID, terminalID string) error {
	_, _, tab, err := findTab(doc, tabID)
	if err != nil {
		return err
	}
	pane := tab.Panes[paneID]
	if pane == nil {
		return fmt.Errorf("workbenchops: pane %q not found in tab %q", paneID, tabID)
	}
	pane.TerminalID = terminalID
	return nil
}

func promoteFloating(doc *workbenchdoc.Doc, tabID, paneID string) error {
	_, _, tab, err := findTab(doc, tabID)
	if err != nil {
		return err
	}
	if tab.Panes[paneID] == nil {
		return fmt.Errorf("workbenchops: pane %q not found in tab %q", paneID, tabID)
	}
	for _, floating := range tab.Floating {
		if floating != nil && floating.PaneID == paneID {
			tab.FloatingVisible = true
			return nil
		}
	}
	tab.Floating = append(tab.Floating, &workbenchdoc.FloatingPane{
		PaneID: paneID,
		Rect:   workbenchdoc.Rect{X: 4, Y: 2, W: 40, H: 12},
		Z:      nextFloatingZ(tab.Floating),
	})
	tab.FloatingVisible = true
	return nil
}

func demoteFloating(doc *workbenchdoc.Doc, tabID, paneID string) error {
	_, _, tab, err := findTab(doc, tabID)
	if err != nil {
		return err
	}
	tab.Floating = removeFloating(tab.Floating, paneID)
	if len(tab.Floating) == 0 {
		tab.FloatingVisible = false
	}
	return nil
}

func normalizeDoc(doc *workbenchdoc.Doc) {
	if doc == nil {
		return
	}
	if doc.Workspaces == nil {
		doc.Workspaces = make(map[string]*workbenchdoc.Workspace)
	}
	if doc.CurrentWorkspace != "" && doc.Workspaces[doc.CurrentWorkspace] == nil {
		doc.CurrentWorkspace = ""
	}
	for _, name := range doc.WorkspaceOrder {
		if doc.Workspaces[name] != nil {
			if doc.CurrentWorkspace == "" {
				doc.CurrentWorkspace = name
			}
			break
		}
	}
	for _, ws := range doc.Workspaces {
		if ws == nil {
			continue
		}
		if ws.ActiveTab < -1 || ws.ActiveTab >= len(ws.Tabs) || (ws.ActiveTab >= 0 && ws.Tabs[ws.ActiveTab] == nil) {
			ws.ActiveTab = activeTabIndex(ws)
		}
		for _, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			if tab.Panes == nil {
				tab.Panes = make(map[string]*workbenchdoc.Pane)
			}
			if tab.ActivePaneID == "" || tab.Panes[tab.ActivePaneID] == nil {
				tab.ActivePaneID = activePaneID(tab)
			}
		}
	}
}

func findTab(doc *workbenchdoc.Doc, tabID string) (string, *workbenchdoc.Workspace, *workbenchdoc.Tab, error) {
	for _, wsName := range doc.WorkspaceOrder {
		ws := doc.Workspaces[wsName]
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab != nil && tab.ID == tabID {
				return wsName, ws, tab, nil
			}
		}
	}
	for wsName, ws := range doc.Workspaces {
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab != nil && tab.ID == tabID {
				return wsName, ws, tab, nil
			}
		}
	}
	return "", nil, nil, fmt.Errorf("workbenchops: tab %q not found", tabID)
}

func replaceLeaf(node *workbenchdoc.LayoutNode, paneID, newPaneID string, dir workbenchdoc.SplitDirection) (*workbenchdoc.LayoutNode, bool) {
	if node == nil {
		return nil, false
	}
	if node.IsLeaf() {
		if node.PaneID != paneID {
			return node, false
		}
		return &workbenchdoc.LayoutNode{
			Direction: dir,
			Ratio:     0.5,
			First:     workbenchdoc.NewLeaf(paneID),
			Second:    workbenchdoc.NewLeaf(newPaneID),
		}, true
	}
	first, done := replaceLeaf(node.First, paneID, newPaneID, dir)
	if done {
		node.First = first
		return node, true
	}
	second, done := replaceLeaf(node.Second, paneID, newPaneID, dir)
	if done {
		node.Second = second
		return node, true
	}
	return node, false
}

func removeFloating(entries []*workbenchdoc.FloatingPane, paneID string) []*workbenchdoc.FloatingPane {
	if len(entries) == 0 {
		return nil
	}
	kept := entries[:0]
	for _, entry := range entries {
		if entry == nil || entry.PaneID == paneID {
			continue
		}
		kept = append(kept, entry)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

func activeTabIndex(ws *workbenchdoc.Workspace) int {
	if ws == nil || len(ws.Tabs) == 0 {
		return -1
	}
	if ws.ActiveTab >= 0 && ws.ActiveTab < len(ws.Tabs) && ws.Tabs[ws.ActiveTab] != nil {
		return ws.ActiveTab
	}
	for i, tab := range ws.Tabs {
		if tab != nil {
			return i
		}
	}
	return -1
}

func activePaneID(tab *workbenchdoc.Tab) string {
	if tab == nil {
		return ""
	}
	if tab.ActivePaneID != "" && tab.Panes[tab.ActivePaneID] != nil {
		return tab.ActivePaneID
	}
	if tab.Root != nil {
		for _, paneID := range tab.Root.LeafIDs() {
			if tab.Panes[paneID] != nil {
				return paneID
			}
		}
	}
	for _, floating := range tab.Floating {
		if floating != nil && tab.Panes[floating.PaneID] != nil {
			return floating.PaneID
		}
	}
	for paneID := range tab.Panes {
		return paneID
	}
	return ""
}

func nextFloatingZ(entries []*workbenchdoc.FloatingPane) int {
	maxZ := 0
	for _, entry := range entries {
		if entry != nil && entry.Z > maxZ {
			maxZ = entry.Z
		}
	}
	return maxZ + 1
}
