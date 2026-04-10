package workbench

import (
	"sort"

	"github.com/lozzow/termx/tuiv2/shared"
)

type TerminalBindingLocation struct {
	WorkspaceName string
	TabID         string
	TabName       string
	PaneID        string
	Visible       bool
}

func NewWorkbench() *Workbench {
	return &Workbench{store: make(map[string]*WorkspaceState)}
}

func (w *Workbench) touch() {
	if w == nil {
		return
	}
	w.version++
}

func (w *Workbench) CurrentWorkspace() *WorkspaceState {
	if w == nil {
		return nil
	}
	return w.store[w.current]
}

func (w *Workbench) CurrentWorkspaceName() string {
	if w == nil {
		return ""
	}
	return w.current
}

func (w *Workbench) WorkspaceByName(name string) *WorkspaceState {
	if w == nil || name == "" {
		return nil
	}
	return w.store[name]
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
	w.touch()
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
	w.touch()
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
	w.touch()
}

func (w *Workbench) ListWorkspaces() []string {
	return append([]string(nil), w.order...)
}

func (w *Workbench) SetPaneTitleByTerminalID(terminalID, title string) {
	if w == nil || terminalID == "" || title == "" {
		return
	}
	changed := false
	for _, wsName := range w.order {
		ws := w.store[wsName]
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			for _, pane := range tab.Panes {
				if pane != nil && pane.TerminalID == terminalID {
					pane.Title = title
					changed = true
				}
			}
		}
	}
	if changed {
		w.touch()
	}
}

func (w *Workbench) Visible() *VisibleWorkbench {
	return w.VisibleWithSize(Rect{W: 1, H: 1})
}

func (w *Workbench) VisibleWithSize(bodyRect Rect) *VisibleWorkbench {
	if w == nil {
		return nil
	}
	if w.visibleCache != nil && w.visibleVersion == w.version && w.visibleRect == bodyRect {
		return w.visibleCache
	}
	workspace := w.CurrentWorkspace()
	if workspace == nil {
		visible := &VisibleWorkbench{ActiveTab: -1}
		w.visibleCache = visible
		w.visibleRect = bodyRect
		w.visibleVersion = w.version
		return visible
	}
	visible := &VisibleWorkbench{
		WorkspaceName:  workspace.Name,
		WorkspaceCount: len(w.order),
		Tabs:           make([]VisibleTab, 0, len(workspace.Tabs)),
		ActiveTab:      -1,
		FloatingPanes:  nil,
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
			floatingLayerVisible := tab.FloatingVisible || hasExpandedFloating(tab.Floating)
			if item.ZoomedPaneID == "" {
				for _, floating := range orderedFloating(tab.Floating) {
					if floating == nil {
						continue
					}
					normalizeFloatingState(floating)
					pane := tab.Panes[floating.PaneID]
					if pane == nil {
						continue
					}
					visible.FloatingTotal++
					if floating.Display == FloatingDisplayCollapsed {
						visible.FloatingCollapsed++
						continue
					}
					if floating.Display == FloatingDisplayHidden || !floatingLayerVisible {
						visible.FloatingHidden++
						continue
					}
					if !floatingStateVisible(floating) {
						visible.FloatingHidden++
						continue
					}
					visible.FloatingPanes = append(visible.FloatingPanes, VisiblePane{
						ID:         pane.ID,
						Title:      pane.Title,
						TerminalID: pane.TerminalID,
						Rect:       floating.Rect,
						Floating:   true,
					})
				}
			}
		}
		if item.ZoomedPaneID != "" {
			pane := tab.Panes[item.ZoomedPaneID]
			if pane != nil {
				item.Panes = append(item.Panes, VisiblePane{
					ID:         pane.ID,
					Title:      pane.Title,
					TerminalID: pane.TerminalID,
					Rect:       bodyRect,
					Floating:   false,
					Frameless:  true,
				})
			}
			visible.Tabs = append(visible.Tabs, item)
			continue
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
				Floating:   false,
			})
		}
		visible.Tabs = append(visible.Tabs, item)
	}
	w.visibleCache = visible
	w.visibleRect = bodyRect
	w.visibleVersion = w.version
	return visible
}

func orderedFloating(entries []*FloatingState) []*FloatingState {
	if len(entries) == 0 {
		return nil
	}
	ordered := append([]*FloatingState(nil), entries...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Z < ordered[j].Z
	})
	return ordered
}

func (w *Workbench) TerminalBindings() map[string][]TerminalBindingLocation {
	if w == nil {
		return nil
	}
	index := make(map[string][]TerminalBindingLocation)
	currentTabID := ""
	if tab := w.CurrentTab(); tab != nil {
		currentTabID = tab.ID
	}
	for _, wsName := range w.order {
		ws := w.store[wsName]
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			for _, paneID := range orderedPaneIDsForBindings(tab) {
				pane := tab.Panes[paneID]
				if pane == nil || pane.TerminalID == "" {
					continue
				}
				index[pane.TerminalID] = append(index[pane.TerminalID], TerminalBindingLocation{
					WorkspaceName: ws.Name,
					TabID:         tab.ID,
					TabName:       tab.Name,
					PaneID:        pane.ID,
					Visible:       ws.Name == w.current && tab.ID == currentTabID,
				})
			}
		}
	}
	if len(index) == 0 {
		return nil
	}
	return index
}

func orderedPaneIDsForBindings(tab *TabState) []string {
	if tab == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(tab.Panes))
	order := make([]string, 0, len(tab.Panes))
	for _, paneID := range tab.paneOrder() {
		if _, exists := seen[paneID]; exists {
			continue
		}
		if _, ok := tab.Panes[paneID]; !ok {
			continue
		}
		seen[paneID] = struct{}{}
		order = append(order, paneID)
	}
	for _, entry := range orderedFloating(tab.Floating) {
		if entry == nil || entry.PaneID == "" {
			continue
		}
		if _, exists := seen[entry.PaneID]; exists {
			continue
		}
		if _, ok := tab.Panes[entry.PaneID]; !ok {
			continue
		}
		seen[entry.PaneID] = struct{}{}
		order = append(order, entry.PaneID)
	}
	extras := make([]string, 0, len(tab.Panes)-len(order))
	for paneID := range tab.Panes {
		if _, exists := seen[paneID]; exists {
			continue
		}
		extras = append(extras, paneID)
	}
	sort.Slice(extras, func(i, j int) bool {
		return shared.LessNumericStrings(extras[i], extras[j])
	})
	return append(order, extras...)
}

func (t *TabState) paneOrder() []string {
	if t == nil {
		return nil
	}
	floating := make(map[string]struct{}, len(t.Floating))
	for _, entry := range t.Floating {
		if entry == nil || entry.PaneID == "" {
			continue
		}
		floating[entry.PaneID] = struct{}{}
	}
	if t.Root != nil {
		leafIDs := t.Root.LeafIDs()
		if len(floating) == 0 {
			return leafIDs
		}
		order := make([]string, 0, len(leafIDs))
		for _, paneID := range leafIDs {
			if _, isFloating := floating[paneID]; isFloating {
				continue
			}
			order = append(order, paneID)
		}
		return order
	}
	order := make([]string, 0, len(t.Panes))
	for paneID := range t.Panes {
		if _, isFloating := floating[paneID]; isFloating {
			continue
		}
		order = append(order, paneID)
	}
	return order
}
