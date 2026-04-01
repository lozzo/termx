package workbench

import "fmt"

// findTab searches all workspaces for the tab with the given ID and returns it
// together with its parent workspace. Returns an error if not found.
func (w *Workbench) findTab(tabID string) (*WorkspaceState, *TabState, error) {
	for _, ws := range w.store {
		for _, tab := range ws.Tabs {
			if tab != nil && tab.ID == tabID {
				return ws, tab, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("workbench: tab %q not found", tabID)
}

// CreateTab adds a new, empty tab to the workspace identified by wsName.
// Returns an error if the workspace does not exist or the tab ID is already
// taken within that workspace.
func (w *Workbench) CreateTab(wsName, tabID, tabName string) error {
	ws, ok := w.store[wsName]
	if !ok {
		return fmt.Errorf("workbench: workspace %q not found", wsName)
	}
	for _, t := range ws.Tabs {
		if t != nil && t.ID == tabID {
			return fmt.Errorf("workbench: tab ID %q already exists in workspace %q", tabID, wsName)
		}
	}
	ws.appendTab(&TabState{
		ID:    tabID,
		Name:  tabName,
		Panes: make(map[string]*PaneState),
	})
	return nil
}

// CreateFirstPane creates the first pane in a tab. It sets up the root
// LayoutNode as a single leaf and updates ActivePaneID. Returns an error if
// the tab already has a root (use SplitPane for subsequent panes) or if the
// tab does not exist.
func (w *Workbench) CreateFirstPane(tabID, paneID string) error {
	_, tab, err := w.findTab(tabID)
	if err != nil {
		return err
	}
	if tab.Root != nil {
		return fmt.Errorf("workbench: tab %q already has a root layout node; use SplitPane", tabID)
	}
	if tab.Panes == nil {
		tab.Panes = make(map[string]*PaneState)
	}
	tab.Panes[paneID] = &PaneState{ID: paneID}
	tab.Root = NewLeaf(paneID)
	tab.ActivePaneID = paneID
	return nil
}

// SplitPane replaces the leaf node for paneID with an internal split node
// whose First child is the existing pane and Second child is a new leaf for
// newPaneID. The split uses a default ratio of 0.5. ActivePaneID is updated
// to newPaneID on success.
//
// Returns an error if:
//   - the tab does not exist
//   - paneID is not present in the tab
//   - newPaneID already exists in the tab
//   - the layout tree contains no leaf for paneID (inconsistent state)
func (w *Workbench) SplitPane(tabID, paneID, newPaneID string, dir SplitDirection) error {
	_, tab, err := w.findTab(tabID)
	if err != nil {
		return err
	}
	if !tab.hasPane(paneID) {
		return fmt.Errorf("workbench: pane %q not found in tab %q", paneID, tabID)
	}
	if _, dup := tab.Panes[newPaneID]; dup {
		return fmt.Errorf("workbench: pane ID %q already exists in tab %q", newPaneID, tabID)
	}

	// Replace the leaf for paneID in the layout tree.
	newRoot, replaced := replaceLeaf(tab.Root, paneID, newPaneID, dir)
	if !replaced {
		return fmt.Errorf("workbench: no layout leaf found for pane %q in tab %q", paneID, tabID)
	}
	tab.Root = newRoot

	tab.Panes[newPaneID] = &PaneState{ID: newPaneID}
	tab.ActivePaneID = newPaneID
	return nil
}

// replaceLeaf recursively replaces the leaf node matching paneID with a new
// internal split node. Returns the (possibly new) root and whether the
// replacement was made.
func replaceLeaf(n *LayoutNode, paneID, newPaneID string, dir SplitDirection) (*LayoutNode, bool) {
	if n == nil {
		return n, false
	}
	if n.IsLeaf() {
		if n.PaneID != paneID {
			return n, false
		}
		// Wrap the existing leaf with a new split node.
		return &LayoutNode{
			Direction: dir,
			Ratio:     0.5,
			First:     NewLeaf(paneID),
			Second:    NewLeaf(newPaneID),
		}, true
	}
	// Recurse into children.
	newFirst, done := replaceLeaf(n.First, paneID, newPaneID, dir)
	if done {
		n.First = newFirst
		return n, true
	}
	newSecond, done := replaceLeaf(n.Second, paneID, newPaneID, dir)
	if done {
		n.Second = newSecond
		return n, true
	}
	return n, false
}

// FocusPane sets tab.ActivePaneID to paneID. Returns an error if the tab or
// pane does not exist.
func (w *Workbench) FocusPane(tabID, paneID string) error {
	_, tab, err := w.findTab(tabID)
	if err != nil {
		return err
	}
	if !tab.hasPane(paneID) {
		return fmt.Errorf("workbench: pane %q not found in tab %q", paneID, tabID)
	}
	tab.ActivePaneID = paneID
	return nil
}

// ClosePane removes a pane from the tab layout and returns the detached
// terminal binding, if any.
func (w *Workbench) ClosePane(tabID, paneID string) (string, error) {
	ws, tab, err := w.findTab(tabID)
	if err != nil {
		return "", err
	}
	terminalID, removed := tab.removePane(paneID)
	if !removed {
		return "", fmt.Errorf("workbench: pane %q not found in tab %q", paneID, tabID)
	}
	if len(tab.Panes) == 0 {
		ws.closeTabByID(tabID)
	}
	return terminalID, nil
}

// SwitchTab activates the workspace tab at index.
func (w *Workbench) SwitchTab(wsName string, index int) error {
	ws, ok := w.store[wsName]
	if !ok {
		return fmt.Errorf("workbench: workspace %q not found", wsName)
	}
	if !ws.activateTab(index) {
		return fmt.Errorf("workbench: tab index %d out of range in workspace %q", index, wsName)
	}
	return nil
}

// CloseTab removes the identified tab and updates the workspace active index.
func (w *Workbench) CloseTab(tabID string) error {
	ws, _, err := w.findTab(tabID)
	if err != nil {
		return err
	}
	if !ws.closeTabByID(tabID) {
		return fmt.Errorf("workbench: tab %q not found", tabID)
	}
	return nil
}

// CreateWorkspace adds a new empty workspace. Returns an error if name is
// empty or already taken.
func (w *Workbench) CreateWorkspace(name string) error {
	if name == "" {
		return fmt.Errorf("workbench: workspace name must not be empty")
	}
	if _, exists := w.store[name]; exists {
		return fmt.Errorf("workbench: workspace %q already exists", name)
	}
	w.AddWorkspace(name, &WorkspaceState{Name: name, ActiveTab: -1})
	return nil
}

// DeleteWorkspace removes the workspace identified by name. Returns an error
// if the workspace does not exist.
func (w *Workbench) DeleteWorkspace(name string) error {
	if _, exists := w.store[name]; !exists {
		return fmt.Errorf("workbench: workspace %q not found", name)
	}
	w.RemoveWorkspace(name)
	return nil
}

// CreateFloatingPane adds a new floating pane to the tab identified by tabID.
func (w *Workbench) CreateFloatingPane(tabID, paneID string, rect Rect) error {
	_, tab, err := w.findTab(tabID)
	if err != nil {
		return err
	}
	if _, exists := tab.Panes[paneID]; exists {
		return fmt.Errorf("workbench: pane %q already exists in tab %q", paneID, tabID)
	}
	if tab.Panes == nil {
		tab.Panes = make(map[string]*PaneState)
	}
	tab.Panes[paneID] = &PaneState{ID: paneID}
	tab.Floating = append(tab.Floating, &FloatingState{PaneID: paneID, Rect: rect})
	return nil
}

// AdjustPaneRatio walks the layout tree of the tab and adjusts the split ratio
// of the first ancestor node whose split axis aligns with dir. delta is added
// to (or subtracted from) Ratio and clamped to [0.1, 0.9].
func (w *Workbench) AdjustPaneRatio(tabID, paneID string, dir Direction, delta float64) error {
	_, tab, err := w.findTab(tabID)
	if err != nil {
		return err
	}
	if tab.Root == nil {
		return nil
	}
	adjustRatioForPane(tab.Root, paneID, dir, delta)
	return nil
}

func containsPane(node *LayoutNode, paneID string) bool {
	if node == nil {
		return false
	}
	if node.IsLeaf() {
		return node.PaneID == paneID
	}
	return containsPane(node.First, paneID) || containsPane(node.Second, paneID)
}

// adjustRatioForPane finds the nearest ancestor split node whose axis aligns
// with dir and adjusts its ratio so that the pane containing paneID grows or
// shrinks in the requested direction.
func adjustRatioForPane(node *LayoutNode, paneID string, dir Direction, delta float64) bool {
	if node == nil || node.IsLeaf() {
		return false
	}
	horizontal := node.Direction == SplitHorizontal && (dir == DirectionUp || dir == DirectionDown)
	vertical := node.Direction == SplitVertical && (dir == DirectionLeft || dir == DirectionRight)
	if horizontal || vertical {
		inFirst := containsPane(node.First, paneID)
		inSecond := containsPane(node.Second, paneID)
		if inFirst || inSecond {
			grow := dir == DirectionRight || dir == DirectionDown
			if inFirst {
				if grow {
					node.Ratio += delta
				} else {
					node.Ratio -= delta
				}
			} else {
				if grow {
					node.Ratio -= delta
				} else {
					node.Ratio += delta
				}
			}
			if node.Ratio < 0.1 {
				node.Ratio = 0.1
			}
			if node.Ratio > 0.9 {
				node.Ratio = 0.9
			}
			return true
		}
	}
	if adjustRatioForPane(node.First, paneID, dir, delta) {
		return true
	}
	return adjustRatioForPane(node.Second, paneID, dir, delta)
}

// BalancePanes resets all pane split ratios to equal in the given tab.
func (w *Workbench) BalancePanes(tabID string) {
	_, tab, err := w.findTab(tabID)
	if err != nil || tab == nil {
		return
	}
	resetNodeRatios(tab.Root)
}

func resetNodeRatios(node *LayoutNode) {
	if node == nil || node.IsLeaf() {
		return
	}
	node.Ratio = 0.5
	resetNodeRatios(node.First)
	resetNodeRatios(node.Second)
}

// CycleLayout cycles through layout presets for the given tab.
func (w *Workbench) CycleLayout(tabID string) {
	_, tab, err := w.findTab(tabID)
	if err != nil || tab == nil {
		return
	}
	tab.LayoutPreset = (tab.LayoutPreset + 1) % 3
}

// BindPaneTerminal sets the TerminalID for a pane. This is the sole write
// path for associating a backend terminal with a pane. Returns an error if
// the tab or pane does not exist.
func (w *Workbench) BindPaneTerminal(tabID, paneID, terminalID string) error {
	_, tab, err := w.findTab(tabID)
	if err != nil {
		return err
	}
	pane := tab.Panes[paneID]
	if pane == nil {
		return fmt.Errorf("workbench: pane %q not found in tab %q", paneID, tabID)
	}
	pane.TerminalID = terminalID
	return nil
}
