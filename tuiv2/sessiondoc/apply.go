package sessiondoc

import (
	"fmt"
	"strings"
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
	Kind          OpKind         `json:"op"`
	WorkspaceName string         `json:"workspace_name,omitempty"`
	NewName       string         `json:"new_name,omitempty"`
	TabID         string         `json:"tab_id,omitempty"`
	TabName       string         `json:"tab_name,omitempty"`
	PaneID        string         `json:"pane_id,omitempty"`
	NewPaneID     string         `json:"new_pane_id,omitempty"`
	TerminalID    string         `json:"terminal_id,omitempty"`
	Direction     SplitDirection `json:"direction,omitempty"`
}

func Apply(doc *Doc, ops []Op) (*Doc, error) {
	if doc == nil {
		return nil, fmt.Errorf("sessiondoc: nil document")
	}
	next := doc.Clone()
	if next.Workspaces == nil {
		next.Workspaces = make(map[string]*Workspace)
	}
	for _, op := range ops {
		if err := applyOne(next, op); err != nil {
			return nil, err
		}
	}
	normalizeDoc(next)
	return next, nil
}

func applyOne(doc *Doc, op Op) error {
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
			return fmt.Errorf("sessiondoc: tab name must not be empty")
		}
		if strings.TrimSpace(tab.Name) == nextName {
			return nil
		}
		if workspaceHasTabName(ws, nextName, op.TabID) {
			return fmt.Errorf("sessiondoc: tab name %q already exists in workspace %q", nextName, ws.Name)
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
		return fmt.Errorf("sessiondoc: unsupported op %q", op.Kind)
	}
}

func createWorkspace(doc *Doc, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("sessiondoc: workspace name must not be empty")
	}
	if doc.Workspaces[name] != nil {
		return fmt.Errorf("sessiondoc: workspace %q already exists", name)
	}
	doc.Workspaces[name] = &Workspace{Name: name, ActiveTab: -1}
	doc.WorkspaceOrder = append(doc.WorkspaceOrder, name)
	if doc.CurrentWorkspace == "" {
		doc.CurrentWorkspace = name
	}
	return nil
}

func renameWorkspace(doc *Doc, oldName, newName string) error {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" {
		return fmt.Errorf("sessiondoc: workspace name must not be empty")
	}
	if oldName == newName {
		return nil
	}
	ws := doc.Workspaces[oldName]
	if ws == nil {
		return fmt.Errorf("sessiondoc: workspace %q not found", oldName)
	}
	if doc.Workspaces[newName] != nil {
		return fmt.Errorf("sessiondoc: workspace %q already exists", newName)
	}
	delete(doc.Workspaces, oldName)
	ws.Name = newName
	doc.Workspaces[newName] = ws
	for i, name := range doc.WorkspaceOrder {
		if name == oldName {
			doc.WorkspaceOrder[i] = newName
			break
		}
	}
	if doc.CurrentWorkspace == oldName {
		doc.CurrentWorkspace = newName
	}
	return nil
}

func deleteWorkspace(doc *Doc, name string) error {
	name = strings.TrimSpace(name)
	if doc.Workspaces[name] == nil {
		return fmt.Errorf("sessiondoc: workspace %q not found", name)
	}
	delete(doc.Workspaces, name)
	doc.WorkspaceOrder = removeString(doc.WorkspaceOrder, name)
	if doc.CurrentWorkspace == name {
		doc.CurrentWorkspace = ""
	}
	return nil
}

func createTab(doc *Doc, workspaceName, tabID, tabName string) error {
	ws := doc.Workspaces[workspaceName]
	if ws == nil {
		return fmt.Errorf("sessiondoc: workspace %q not found", workspaceName)
	}
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return fmt.Errorf("sessiondoc: tab id must not be empty")
	}
	nextName := strings.TrimSpace(tabName)
	if nextName == "" {
		nextName = tabID
	}
	for _, tab := range ws.Tabs {
		if tab == nil {
			continue
		}
		if tab.ID == tabID {
			return fmt.Errorf("sessiondoc: tab %q already exists", tabID)
		}
		if tab.Name == nextName {
			return fmt.Errorf("sessiondoc: tab name %q already exists in workspace %q", nextName, workspaceName)
		}
	}
	ws.Tabs = append(ws.Tabs, &Tab{
		ID:           tabID,
		Name:         nextName,
		Panes:        make(map[string]*Pane),
		ActivePaneID: "",
	})
	if ws.ActiveTab < 0 {
		ws.ActiveTab = 0
	}
	return nil
}

func workspaceHasTabName(ws *Workspace, name, exceptTabID string) bool {
	if ws == nil {
		return false
	}
	for _, tab := range ws.Tabs {
		if tab == nil || tab.ID == exceptTabID {
			continue
		}
		if tab.Name == name {
			return true
		}
	}
	return false
}

func deleteTab(doc *Doc, tabID string) error {
	for _, ws := range doc.Workspaces {
		if ws == nil {
			continue
		}
		for i, tab := range ws.Tabs {
			if tab == nil || tab.ID != tabID {
				continue
			}
			ws.Tabs = append(ws.Tabs[:i], ws.Tabs[i+1:]...)
			if len(ws.Tabs) == 0 {
				ws.ActiveTab = -1
				return nil
			}
			if ws.ActiveTab >= len(ws.Tabs) {
				ws.ActiveTab = len(ws.Tabs) - 1
			}
			if ws.ActiveTab < 0 {
				ws.ActiveTab = 0
			}
			return nil
		}
	}
	return fmt.Errorf("sessiondoc: tab %q not found", tabID)
}

func createFirstPane(doc *Doc, tabID, paneID string) error {
	_, _, tab, err := findTab(doc, tabID)
	if err != nil {
		return err
	}
	if tab.Root != nil {
		return fmt.Errorf("sessiondoc: tab %q already has a root", tabID)
	}
	paneID = strings.TrimSpace(paneID)
	if paneID == "" {
		return fmt.Errorf("sessiondoc: pane id must not be empty")
	}
	if tab.Panes == nil {
		tab.Panes = make(map[string]*Pane)
	}
	tab.Panes[paneID] = &Pane{ID: paneID}
	tab.Root = NewLeaf(paneID)
	tab.ActivePaneID = paneID
	return nil
}

func splitPane(doc *Doc, tabID, paneID, newPaneID string, dir SplitDirection) error {
	_, _, tab, err := findTab(doc, tabID)
	if err != nil {
		return err
	}
	if tab.Panes[paneID] == nil {
		return fmt.Errorf("sessiondoc: pane %q not found in tab %q", paneID, tabID)
	}
	newPaneID = strings.TrimSpace(newPaneID)
	if newPaneID == "" {
		return fmt.Errorf("sessiondoc: new pane id must not be empty")
	}
	if tab.Panes[newPaneID] != nil {
		return fmt.Errorf("sessiondoc: pane %q already exists in tab %q", newPaneID, tabID)
	}
	nextRoot, ok := replaceLeaf(tab.Root, paneID, newPaneID, dir)
	if !ok {
		return fmt.Errorf("sessiondoc: no layout leaf found for pane %q in tab %q", paneID, tabID)
	}
	tab.Root = nextRoot
	if tab.Panes == nil {
		tab.Panes = make(map[string]*Pane)
	}
	tab.Panes[newPaneID] = &Pane{ID: newPaneID}
	tab.ActivePaneID = newPaneID
	return nil
}

func closePane(doc *Doc, tabID, paneID string) error {
	_, _, tab, err := findTab(doc, tabID)
	if err != nil {
		return err
	}
	if tab.Panes[paneID] == nil {
		return fmt.Errorf("sessiondoc: pane %q not found in tab %q", paneID, tabID)
	}
	delete(tab.Panes, paneID)
	tab.Root = tab.Root.Remove(paneID)
	tab.Floating = removeFloating(tab.Floating, paneID)
	if tab.ActivePaneID == paneID {
		tab.ActivePaneID = activePaneID(tab)
	}
	if tab.ZoomedPaneID == paneID {
		tab.ZoomedPaneID = ""
	}
	return nil
}

func focusPane(doc *Doc, tabID, paneID string) error {
	_, _, tab, err := findTab(doc, tabID)
	if err != nil {
		return err
	}
	if tab.Panes[paneID] == nil {
		return fmt.Errorf("sessiondoc: pane %q not found in tab %q", paneID, tabID)
	}
	tab.ActivePaneID = paneID
	return nil
}

func bindTerminal(doc *Doc, tabID, paneID, terminalID string) error {
	_, _, tab, err := findTab(doc, tabID)
	if err != nil {
		return err
	}
	if tab.Panes[paneID] == nil {
		return fmt.Errorf("sessiondoc: pane %q not found in tab %q", paneID, tabID)
	}
	tab.Panes[paneID].TerminalID = terminalID
	return nil
}

func promoteFloating(doc *Doc, tabID, paneID string) error {
	_, _, tab, err := findTab(doc, tabID)
	if err != nil {
		return err
	}
	if tab.Panes[paneID] == nil {
		return fmt.Errorf("sessiondoc: pane %q not found in tab %q", paneID, tabID)
	}
	for _, floating := range tab.Floating {
		if floating != nil && floating.PaneID == paneID {
			return nil
		}
	}
	tab.Floating = append(tab.Floating, &FloatingPane{
		PaneID: paneID,
		Rect:   Rect{X: 4, Y: 2, W: 40, H: 12},
		Z:      nextFloatingZ(tab.Floating),
	})
	tab.FloatingVisible = true
	return nil
}

func demoteFloating(doc *Doc, tabID, paneID string) error {
	_, _, tab, err := findTab(doc, tabID)
	if err != nil {
		return err
	}
	tab.Floating = removeFloating(tab.Floating, paneID)
	return nil
}

func normalizeDoc(doc *Doc) {
	if doc == nil {
		return
	}
	if doc.Workspaces == nil {
		doc.Workspaces = make(map[string]*Workspace)
	}
	doc.WorkspaceOrder = compactWorkspaceOrder(doc.WorkspaceOrder, doc.Workspaces)
	if doc.CurrentWorkspace == "" || doc.Workspaces[doc.CurrentWorkspace] == nil {
		if len(doc.WorkspaceOrder) > 0 {
			doc.CurrentWorkspace = doc.WorkspaceOrder[0]
		}
	}
	for _, ws := range doc.Workspaces {
		if ws == nil {
			continue
		}
		if ws.ActiveTab >= len(ws.Tabs) {
			ws.ActiveTab = len(ws.Tabs) - 1
		}
		if len(ws.Tabs) == 0 {
			ws.ActiveTab = -1
			continue
		}
		if ws.ActiveTab < 0 {
			ws.ActiveTab = 0
		}
		for _, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			if tab.Panes == nil {
				tab.Panes = make(map[string]*Pane)
			}
			if tab.ActivePaneID == "" || tab.Panes[tab.ActivePaneID] == nil {
				tab.ActivePaneID = activePaneID(tab)
			}
			if tab.Root == nil && len(tab.Panes) == 1 {
				tab.Root = NewLeaf(activePaneID(tab))
			}
		}
	}
}

func findTab(doc *Doc, tabID string) (string, *Workspace, *Tab, error) {
	for name, ws := range doc.Workspaces {
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab != nil && tab.ID == tabID {
				return name, ws, tab, nil
			}
		}
	}
	return "", nil, nil, fmt.Errorf("sessiondoc: tab %q not found", tabID)
}

func replaceLeaf(node *LayoutNode, paneID, newPaneID string, dir SplitDirection) (*LayoutNode, bool) {
	if node == nil {
		return nil, false
	}
	if node.IsLeaf() {
		if node.PaneID != paneID {
			return node, false
		}
		return &LayoutNode{
			Direction: dir,
			Ratio:     0.5,
			First:     NewLeaf(paneID),
			Second:    NewLeaf(newPaneID),
		}, true
	}
	first, ok := replaceLeaf(node.First, paneID, newPaneID, dir)
	if ok {
		node.First = first
		return node, true
	}
	second, ok := replaceLeaf(node.Second, paneID, newPaneID, dir)
	if ok {
		node.Second = second
		return node, true
	}
	return node, false
}

func removeFloating(entries []*FloatingPane, paneID string) []*FloatingPane {
	if len(entries) == 0 {
		return nil
	}
	out := entries[:0]
	for _, entry := range entries {
		if entry == nil || entry.PaneID == paneID {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func activePaneID(tab *Tab) string {
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
	for paneID := range tab.Panes {
		return paneID
	}
	return ""
}

func compactWorkspaceOrder(order []string, workspaces map[string]*Workspace) []string {
	seen := make(map[string]struct{}, len(order))
	out := make([]string, 0, len(order))
	for _, name := range order {
		if name == "" || workspaces[name] == nil {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	for name, ws := range workspaces {
		if ws == nil {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		out = append(out, name)
	}
	return out
}

func removeString(values []string, target string) []string {
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

func nextFloatingZ(entries []*FloatingPane) int {
	maxZ := 0
	for _, entry := range entries {
		if entry != nil && entry.Z > maxZ {
			maxZ = entry.Z
		}
	}
	return maxZ + 1
}
