package tui

import "slices"

func (w *Workspace) ActivateTab(index int) bool {
	if index < 0 || index >= len(w.Tabs) || w.ActiveTab == index {
		return false
	}
	w.ActiveTab = index
	return true
}

func (w *Workspace) FocusPane(paneID string) bool {
	if paneID == "" {
		return false
	}
	for tabIndex, tab := range w.Tabs {
		if tab == nil || !tab.HasPane(paneID) {
			continue
		}
		w.ActiveTab = tabIndex
		return tab.FocusPane(paneID)
	}
	return false
}

func (w *Workspace) RemoveTab(index int) bool {
	if index < 0 || index >= len(w.Tabs) {
		return false
	}
	w.Tabs = append(w.Tabs[:index], w.Tabs[index+1:]...)
	switch {
	case len(w.Tabs) == 0:
		w.ActiveTab = 0
	case w.ActiveTab > index:
		w.ActiveTab--
	case w.ActiveTab >= len(w.Tabs):
		w.ActiveTab = len(w.Tabs) - 1
	}
	if tab := w.CurrentTab(); tab != nil {
		tab.EnsureActivePane()
	}
	return true
}

func (w *Workspace) CurrentTab() *Tab {
	if w == nil || w.ActiveTab < 0 || w.ActiveTab >= len(w.Tabs) {
		return nil
	}
	return w.Tabs[w.ActiveTab]
}

func (w *Workspace) RemovePane(paneID string) (tabRemoved bool, workspaceEmpty bool, removedTerminalID string) {
	if w == nil || paneID == "" {
		return false, false, ""
	}
	for tabIndex, tab := range w.Tabs {
		if tab == nil || !tab.HasPane(paneID) {
			continue
		}
		removedTerminalID = ""
		if pane := tab.Panes[paneID]; pane != nil {
			removedTerminalID = pane.TerminalID
		}
		tab.RemovePaneRef(paneID)
		if len(tab.Panes) == 0 {
			w.Tabs = append(w.Tabs[:tabIndex], w.Tabs[tabIndex+1:]...)
			switch {
			case len(w.Tabs) == 0:
				w.ActiveTab = 0
				return true, true, removedTerminalID
			case w.ActiveTab > tabIndex:
				w.ActiveTab--
			case w.ActiveTab >= len(w.Tabs):
				w.ActiveTab = len(w.Tabs) - 1
			}
			if current := w.CurrentTab(); current != nil {
				current.EnsureActivePane()
			}
			return true, false, removedTerminalID
		}
		return false, false, removedTerminalID
	}
	return false, false, ""
}

func (t *Tab) HasPane(paneID string) bool {
	if t == nil || paneID == "" {
		return false
	}
	_, ok := t.Panes[paneID]
	return ok
}

func (t *Tab) FocusPane(paneID string) bool {
	if t == nil || !t.HasPane(paneID) {
		return false
	}
	if t.ActivePaneID == paneID {
		return false
	}
	t.ActivePaneID = paneID
	t.renderCache = nil
	return true
}

func (t *Tab) EnsureActivePane() bool {
	if t == nil {
		return false
	}
	if t.ActivePaneID != "" && t.Panes[t.ActivePaneID] != nil {
		return false
	}
	next := firstPaneID(t.Panes)
	if next == t.ActivePaneID {
		return false
	}
	t.ActivePaneID = next
	t.renderCache = nil
	return true
}

func (t *Tab) ClampFloatingPanes(bounds Rect) bool {
	if t == nil || len(t.Floating) == 0 {
		return false
	}
	changed := false
	for _, floating := range t.Floating {
		if floating == nil {
			continue
		}
		next := clampFloatingRect(floating.Rect, bounds)
		if next != floating.Rect {
			floating.Rect = next
			changed = true
		}
	}
	if changed {
		t.renderCache = nil
	}
	return changed
}

func (t *Tab) RemovePaneRef(paneID string) bool {
	if t == nil || !t.HasPane(paneID) {
		return false
	}
	delete(t.Panes, paneID)
	t.Floating = removeFloatingPane(t.Floating, paneID)
	if t.Root != nil {
		t.Root = t.Root.Remove(paneID)
	}
	t.LayoutPreset = layoutPresetCustom
	if t.ZoomedPaneID == paneID {
		t.ZoomedPaneID = ""
	}
	if t.ActivePaneID == paneID || t.ActivePaneID == "" {
		t.ActivePaneID = firstPaneID(t.Panes)
	}
	t.renderCache = nil
	return true
}

func (t *Tab) MoveFocus(dir Direction, bounds Rect) bool {
	if t == nil || t.Root == nil {
		return false
	}
	rects := t.Root.Rects(bounds)
	next := t.Root.Adjacent(t.ActivePaneID, dir, rects)
	if next == "" || next == t.ActivePaneID {
		return false
	}
	t.ActivePaneID = next
	t.renderCache = nil
	return true
}

func (t *Tab) SwapActivePane(delta int) bool {
	if t == nil || t.Root == nil || t.ActivePaneID == "" {
		return false
	}
	if !t.Root.SwapWithNeighbor(t.ActivePaneID, delta) {
		return false
	}
	t.LayoutPreset = layoutPresetCustom
	t.renderCache = nil
	return true
}

func (t *Tab) ResizeActivePane(dir Direction, step int, bounds Rect) bool {
	if t == nil || t.Root == nil || t.ActivePaneID == "" || step <= 0 || t.ZoomedPaneID != "" {
		return false
	}
	if !t.Root.AdjustPaneBoundary(t.ActivePaneID, dir, step, 4, bounds) {
		return false
	}
	t.LayoutPreset = layoutPresetCustom
	t.renderCache = nil
	return true
}

func (t *Tab) CycleLayoutPreset() bool {
	if t == nil || t.Root == nil {
		return false
	}
	ids := t.Root.LeafIDs()
	if len(ids) < 2 {
		return false
	}
	next := t.LayoutPreset + 1
	if next < layoutPresetEvenHorizontal || next >= layoutPresetCount {
		next = layoutPresetEvenHorizontal
	}
	root := buildPresetLayout(ids, next)
	if root == nil {
		return false
	}
	t.Root = root
	t.LayoutPreset = next
	t.renderCache = nil
	return true
}

func (t *Tab) VisibleFloatingPanes(bounds Rect) []*FloatingPane {
	if t == nil || !t.FloatingVisible || len(t.Floating) == 0 {
		return nil
	}
	out := make([]*FloatingPane, 0, len(t.Floating))
	for _, floating := range t.Floating {
		if floating == nil {
			continue
		}
		entry := *floating
		entry.Rect = clampFloatingRect(entry.Rect, bounds)
		out = append(out, &entry)
	}
	slices.SortStableFunc(out, func(a, b *FloatingPane) int {
		if a.Z != b.Z {
			return a.Z - b.Z
		}
		if a.PaneID < b.PaneID {
			return -1
		}
		if a.PaneID > b.PaneID {
			return 1
		}
		return 0
	})
	return out
}
