package workbenchdoc

func (d *Doc) Clone() *Doc {
	if d == nil {
		return nil
	}
	cloned := &Doc{
		CurrentWorkspace: d.CurrentWorkspace,
		WorkspaceOrder:   append([]string(nil), d.WorkspaceOrder...),
	}
	if len(d.Workspaces) > 0 {
		cloned.Workspaces = make(map[string]*Workspace, len(d.Workspaces))
		for name, ws := range d.Workspaces {
			cloned.Workspaces[name] = cloneWorkspace(ws)
		}
	}
	return cloned
}

func cloneWorkspace(ws *Workspace) *Workspace {
	if ws == nil {
		return nil
	}
	cloned := &Workspace{
		Name:      ws.Name,
		ActiveTab: ws.ActiveTab,
		Tabs:      make([]*Tab, len(ws.Tabs)),
	}
	for i, tab := range ws.Tabs {
		cloned.Tabs[i] = cloneTab(tab)
	}
	return cloned
}

func cloneTab(tab *Tab) *Tab {
	if tab == nil {
		return nil
	}
	cloned := &Tab{
		ID:              tab.ID,
		Name:            tab.Name,
		Root:            cloneLayout(tab.Root),
		FloatingVisible: tab.FloatingVisible,
		ActivePaneID:    tab.ActivePaneID,
		ZoomedPaneID:    tab.ZoomedPaneID,
		ScrollOffset:    tab.ScrollOffset,
		LayoutPreset:    tab.LayoutPreset,
	}
	if len(tab.Panes) > 0 {
		cloned.Panes = make(map[string]*Pane, len(tab.Panes))
		for paneID, pane := range tab.Panes {
			cloned.Panes[paneID] = clonePane(pane)
		}
	}
	if len(tab.Floating) > 0 {
		cloned.Floating = make([]*FloatingPane, len(tab.Floating))
		for i, floating := range tab.Floating {
			cloned.Floating[i] = cloneFloating(floating)
		}
	}
	return cloned
}

func cloneLayout(node *LayoutNode) *LayoutNode {
	if node == nil {
		return nil
	}
	return &LayoutNode{
		PaneID:    node.PaneID,
		Direction: node.Direction,
		Ratio:     node.Ratio,
		First:     cloneLayout(node.First),
		Second:    cloneLayout(node.Second),
	}
}

func clonePane(pane *Pane) *Pane {
	if pane == nil {
		return nil
	}
	return &Pane{
		ID:         pane.ID,
		Title:      pane.Title,
		TerminalID: pane.TerminalID,
	}
}

func cloneFloating(floating *FloatingPane) *FloatingPane {
	if floating == nil {
		return nil
	}
	return &FloatingPane{
		PaneID: floating.PaneID,
		Rect:   floating.Rect,
		Z:      floating.Z,
	}
}
