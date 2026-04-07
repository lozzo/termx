package workbench

import (
	"strconv"
	"strings"
)

func (ws *WorkspaceState) activeTabIndex() int {
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

func (ws *WorkspaceState) currentTab() *TabState {
	idx := ws.activeTabIndex()
	if idx < 0 {
		return nil
	}
	return ws.Tabs[idx]
}

func (ws *WorkspaceState) activateTab(index int) bool {
	if ws == nil || index < 0 || index >= len(ws.Tabs) || ws.Tabs[index] == nil {
		return false
	}
	ws.ActiveTab = index
	if tab := ws.currentTab(); tab != nil {
		tab.ensureActivePane()
	}
	return true
}

func (ws *WorkspaceState) appendTab(tab *TabState) {
	if ws == nil || tab == nil {
		return
	}
	ws.Tabs = append(ws.Tabs, tab)
	ws.ActiveTab = len(ws.Tabs) - 1
}

func (ws *WorkspaceState) closeTabByID(tabID string) bool {
	if ws == nil || tabID == "" {
		return false
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
			ws.ActiveTab = ws.activeTabIndex()
		}
		if current := ws.currentTab(); current != nil {
			current.ensureActivePane()
		}
		return true
	}
	return false
}

func (ws *WorkspaceState) NextAvailableTabName() string {
	if ws == nil {
		return "1"
	}
	used := make(map[string]struct{}, len(ws.Tabs))
	for _, tab := range ws.Tabs {
		if tab == nil {
			continue
		}
		name := strings.TrimSpace(tab.Name)
		if name == "" {
			continue
		}
		used[name] = struct{}{}
	}
	for next := len(ws.Tabs) + 1; ; next++ {
		candidate := strconv.Itoa(next)
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}
