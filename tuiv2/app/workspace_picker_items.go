package app

import (
	"sort"
	"strings"

	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) workspacePickerItems() []modal.WorkspacePickerItem {
	if m == nil || m.workbench == nil {
		return nil
	}
	names := m.workbench.ListWorkspaces()
	items := make([]modal.WorkspacePickerItem, 0, len(names)*4)
	current := m.workbench.CurrentWorkspaceName()
	for _, name := range names {
		ws := m.workbench.WorkspaceByName(name)
		if ws == nil {
			continue
		}
		items = append(items, m.workspacePickerWorkspaceItem(name, ws, current))
		for index, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			items = append(items, m.workspacePickerTabItem(name, ws, tab, index, current))
			for _, paneID := range workspacePickerPaneIDs(tab) {
				if paneItem, ok := m.workspacePickerPaneItem(name, ws, tab, paneID, index, current); ok {
					items = append(items, paneItem)
				}
			}
		}
	}
	return items
}

func (m *Model) workspacePickerWorkspaceItem(name string, ws *workbench.WorkspaceState, current string) modal.WorkspacePickerItem {
	tabCount := 0
	paneCount := 0
	floatingCount := 0
	activeTabName := ""
	activePaneName := ""
	previewTerminalID := ""
	for index, tab := range ws.Tabs {
		if tab == nil {
			continue
		}
		tabCount++
		paneCount += len(tab.Panes)
		floatingCount += len(tab.Floating)
		if index == ws.ActiveTab {
			activeTabName = strings.TrimSpace(tab.Name)
			if pane := tab.Panes[tab.ActivePaneID]; pane != nil {
				activePaneName = strings.TrimSpace(pane.Title)
				previewTerminalID = strings.TrimSpace(pane.TerminalID)
				if activePaneName == "" {
					activePaneName = pane.ID
				}
			}
		}
	}
	return modal.WorkspacePickerItem{
		Kind:           modal.WorkspacePickerItemWorkspace,
		Name:           name,
		WorkspaceName:  name,
		Current:        name == current,
		Active:         name == current,
		TabCount:       tabCount,
		PaneCount:      paneCount,
		FloatingCount:  floatingCount,
		ActiveTabName:  activeTabName,
		ActivePaneName: activePaneName,
		TerminalID:     previewTerminalID,
	}
}

func (m *Model) workspacePickerTabItem(name string, ws *workbench.WorkspaceState, tab *workbench.TabState, index int, current string) modal.WorkspacePickerItem {
	activePaneName := ""
	terminalID := ""
	if pane := tab.Panes[tab.ActivePaneID]; pane != nil {
		activePaneName = strings.TrimSpace(pane.Title)
		terminalID = strings.TrimSpace(pane.TerminalID)
		if activePaneName == "" {
			activePaneName = pane.ID
		}
	}
	return modal.WorkspacePickerItem{
		Kind:           modal.WorkspacePickerItemTab,
		Name:           strings.TrimSpace(tab.Name),
		WorkspaceName:  name,
		TabID:          tab.ID,
		TabIndex:       index,
		Depth:          1,
		Active:         name == current && index == ws.ActiveTab,
		PaneCount:      len(tab.Panes),
		FloatingCount:  len(tab.Floating),
		ActivePaneName: activePaneName,
		TerminalID:     terminalID,
	}
}

func (m *Model) workspacePickerPaneItem(name string, ws *workbench.WorkspaceState, tab *workbench.TabState, paneID string, index int, current string) (modal.WorkspacePickerItem, bool) {
	pane := tab.Panes[paneID]
	if pane == nil {
		return modal.WorkspacePickerItem{}, false
	}
	terminalState := "unconnected"
	title := strings.TrimSpace(pane.Title)
	role := ""
	floating := workspacePickerTabFloating(tab, paneID)
	if m.runtime != nil {
		if binding := m.runtime.Binding(paneID); binding != nil && strings.TrimSpace(string(binding.Role)) != "" {
			role = strings.TrimSpace(string(binding.Role))
		}
		if terminal := m.runtime.Registry().Get(strings.TrimSpace(pane.TerminalID)); terminal != nil {
			if strings.TrimSpace(terminal.Name) != "" && title == "" {
				title = strings.TrimSpace(terminal.Name)
			}
			if strings.TrimSpace(terminal.State) != "" {
				terminalState = strings.TrimSpace(terminal.State)
			}
		}
	} else if strings.TrimSpace(pane.TerminalID) != "" {
		terminalState = "attached"
	}
	if title == "" {
		title = pane.ID
	}
	return modal.WorkspacePickerItem{
		Kind:          modal.WorkspacePickerItemPane,
		Name:          title,
		WorkspaceName: name,
		TabID:         tab.ID,
		TabIndex:      index,
		TabName:       strings.TrimSpace(tab.Name),
		PaneID:        pane.ID,
		Depth:         2,
		Active:        name == current && index == ws.ActiveTab && pane.ID == tab.ActivePaneID,
		Floating:      floating,
		TerminalID:    strings.TrimSpace(pane.TerminalID),
		State:         terminalState,
		Role:          role,
	}, true
}

func workspacePickerPaneIDs(tab *workbench.TabState) []string {
	if tab == nil {
		return nil
	}
	order := make([]string, 0, len(tab.Panes))
	seen := make(map[string]struct{}, len(tab.Panes))
	if tab.Root != nil {
		for _, paneID := range tab.Root.LeafIDs() {
			if paneID == "" || tab.Panes[paneID] == nil {
				continue
			}
			order = append(order, paneID)
			seen[paneID] = struct{}{}
		}
	}
	if len(tab.Floating) > 0 {
		for _, floating := range tab.Floating {
			if floating == nil || floating.PaneID == "" || tab.Panes[floating.PaneID] == nil {
				continue
			}
			if _, ok := seen[floating.PaneID]; ok {
				continue
			}
			order = append(order, floating.PaneID)
			seen[floating.PaneID] = struct{}{}
		}
	}
	extras := make([]string, 0, len(tab.Panes))
	for paneID := range tab.Panes {
		if _, ok := seen[paneID]; ok {
			continue
		}
		extras = append(extras, paneID)
	}
	sort.Strings(extras)
	order = append(order, extras...)
	return order
}

func workspacePickerTabFloating(tab *workbench.TabState, paneID string) bool {
	if tab == nil || paneID == "" {
		return false
	}
	for _, floating := range tab.Floating {
		if floating != nil && floating.PaneID == paneID {
			return true
		}
	}
	return false
}
