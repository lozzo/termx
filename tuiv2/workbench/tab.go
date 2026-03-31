package workbench

func (t *TabState) hasPane(paneID string) bool {
	if t == nil || paneID == "" {
		return false
	}
	_, ok := t.Panes[paneID]
	return ok
}

func (t *TabState) activePaneIDOrFallback() string {
	if t == nil {
		return ""
	}
	if t.ActivePaneID != "" && t.Panes[t.ActivePaneID] != nil {
		return t.ActivePaneID
	}
	for _, paneID := range t.paneOrder() {
		if t.Panes[paneID] != nil {
			return paneID
		}
	}
	for paneID := range t.Panes {
		return paneID
	}
	return ""
}

func (t *TabState) activePane() *PaneState {
	if t == nil {
		return nil
	}
	return t.Panes[t.activePaneIDOrFallback()]
}

func (t *TabState) ensureActivePane() {
	if t == nil {
		return
	}
	t.ActivePaneID = t.activePaneIDOrFallback()
}

func (t *TabState) visibleZoomedPaneID() string {
	if t == nil || t.ZoomedPaneID == "" || t.Panes[t.ZoomedPaneID] == nil {
		return ""
	}
	return t.ZoomedPaneID
}

func (t *TabState) removePane(paneID string) (string, bool) {
	if !t.hasPane(paneID) {
		return "", false
	}
	removedTerminalID := t.Panes[paneID].TerminalID
	delete(t.Panes, paneID)
	t.Floating = removeFloatingPane(t.Floating, paneID)
	if len(t.Floating) == 0 {
		t.FloatingVisible = false
	}
	if t.Root != nil {
		t.Root = t.Root.Remove(paneID)
	}
	if t.ZoomedPaneID == paneID {
		t.ZoomedPaneID = ""
	}
	if len(t.Panes) == 0 {
		t.ActivePaneID = ""
		t.Root = nil
		return removedTerminalID, true
	}
	t.ensureActivePane()
	return removedTerminalID, true
}
