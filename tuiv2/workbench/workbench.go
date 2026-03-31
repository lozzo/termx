package workbench

func NewWorkbench() *Workbench {
	return &Workbench{store: make(map[string]*WorkspaceState)}
}

func (w *Workbench) CurrentWorkspace() *WorkspaceState {
	if w == nil {
		return nil
	}
	return w.store[w.current]
}

func (w *Workbench) CurrentTab() *TabState {
	workspace := w.CurrentWorkspace()
	if workspace == nil {
		return nil
	}
	return workspace.currentTab()
}

func (w *Workbench) ActivePane() *PaneState {
	tab := w.CurrentTab()
	if tab == nil {
		return nil
	}
	return tab.activePane()
}

func (w *Workbench) SwitchWorkspace(name string) bool {
	if _, ok := w.store[name]; !ok {
		return false
	}
	w.current = name
	return true
}

func (w *Workbench) AddWorkspace(name string, ws *WorkspaceState) {
	if w == nil || ws == nil || name == "" {
		return
	}
	if _, exists := w.store[name]; !exists {
		w.order = append(w.order, name)
	}
	w.store[name] = ws
	if w.current == "" {
		w.current = name
	}
}

func (w *Workbench) RemoveWorkspace(name string) {
	if w == nil || name == "" {
		return
	}
	delete(w.store, name)
	kept := w.order[:0]
	for _, item := range w.order {
		if item != name {
			kept = append(kept, item)
		}
	}
	w.order = kept
	if w.current == name {
		w.current = ""
		if len(w.order) > 0 {
			w.current = w.order[0]
		}
	}
}

func (w *Workbench) ListWorkspaces() []string {
	return append([]string(nil), w.order...)
}

func (w *Workbench) Visible() *VisibleWorkbench {
	return w.VisibleWithSize(Rect{W: 1, H: 1})
}

func (w *Workbench) VisibleWithSize(bodyRect Rect) *VisibleWorkbench {
	if w == nil {
		return nil
	}
	workspace := w.CurrentWorkspace()
	if workspace == nil {
		return &VisibleWorkbench{ActiveTab: -1}
	}
	visible := &VisibleWorkbench{
		WorkspaceName: workspace.Name,
		Tabs:          make([]VisibleTab, 0, len(workspace.Tabs)),
		ActiveTab:     -1,
		FloatingPanes: nil,
	}
	activeTab := workspace.currentTab()
	for _, tab := range workspace.Tabs {
		if tab == nil {
			continue
		}
		activePaneID := tab.activePaneIDOrFallback()
		item := VisibleTab{
			ID:           tab.ID,
			Name:         tab.Name,
			Panes:        make([]VisiblePane, 0, len(tab.Panes)),
			ActivePaneID: activePaneID,
			ZoomedPaneID: tab.visibleZoomedPaneID(),
			ScrollOffset: tab.ScrollOffset,
		}
		var rects map[string]Rect
		if tab.Root != nil {
			rects = tab.Root.Rects(bodyRect)
		}
		if activeTab != nil && activeTab.ID == tab.ID {
			visible.ActiveTab = len(visible.Tabs)
			for _, floating := range tab.Floating {
				if floating == nil {
					continue
				}
				pane := tab.Panes[floating.PaneID]
				if pane == nil {
					continue
				}
				visible.FloatingPanes = append(visible.FloatingPanes, VisiblePane{
					ID:         pane.ID,
					Title:      pane.Title,
					TerminalID: pane.TerminalID,
					Rect:       floating.Rect,
				})
			}
		}
		for _, paneID := range tab.paneOrder() {
			pane := tab.Panes[paneID]
			if pane == nil {
				continue
			}
			item.Panes = append(item.Panes, VisiblePane{
				ID:         pane.ID,
				Title:      pane.Title,
				TerminalID: pane.TerminalID,
				Rect:       rects[pane.ID],
			})
		}
		visible.Tabs = append(visible.Tabs, item)
	}
	return visible
}

func (t *TabState) paneOrder() []string {
	if t == nil {
		return nil
	}
	if t.Root != nil {
		return t.Root.LeafIDs()
	}
	order := make([]string, 0, len(t.Panes))
	for paneID := range t.Panes {
		order = append(order, paneID)
	}
	return order
}
