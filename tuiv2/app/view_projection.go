package app

type localViewProjection struct {
	WorkspaceName   string
	ActiveTabID     string
	FocusedPaneID   string
	ZoomedPaneByTab map[string]string
	ViewportByPane  map[string]int
}

func (m *Model) captureLocalViewProjection() localViewProjection {
	proj := localViewProjection{
		ZoomedPaneByTab: make(map[string]string),
		ViewportByPane:  make(map[string]int),
	}
	if m == nil || m.workbench == nil {
		return proj
	}
	if ws := m.workbench.CurrentWorkspace(); ws != nil {
		proj.WorkspaceName = ws.Name
	}
	if tab := m.workbench.CurrentTab(); tab != nil {
		proj.ActiveTabID = tab.ID
		proj.FocusedPaneID = tab.ActivePaneID
	}
	for _, wsName := range m.workbench.ListWorkspaces() {
		ws := m.workbench.WorkspaceByName(wsName)
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			proj.ZoomedPaneByTab[tab.ID] = tab.ZoomedPaneID
			for paneID := range tab.Panes {
				if offset, ok := m.paneViewportBindingOffset(paneID); ok {
					proj.ViewportByPane[paneID] = offset
				}
			}
			if tab.ActivePaneID != "" {
				if _, ok := proj.ViewportByPane[tab.ActivePaneID]; !ok {
					proj.ViewportByPane[tab.ActivePaneID] = m.effectiveTabViewportOffset(tab)
				}
			}
		}
	}
	return proj
}

func (m *Model) applyLocalViewProjection(proj localViewProjection) {
	if m == nil || m.workbench == nil {
		return
	}
	for _, wsName := range m.workbench.ListWorkspaces() {
		ws := m.workbench.WorkspaceByName(wsName)
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			if zoomed, ok := proj.ZoomedPaneByTab[tab.ID]; ok && (zoomed == "" || tab.Panes[zoomed] != nil) {
				tab.ZoomedPaneID = zoomed
			}
			for paneID := range tab.Panes {
				scroll, ok := proj.ViewportByPane[paneID]
				if !ok {
					continue
				}
				_ = m.setPaneViewportOffset(paneID, scroll)
			}
		}
	}
	if proj.WorkspaceName != "" {
		_ = m.workbench.SwitchWorkspace(proj.WorkspaceName)
	}
	if proj.ActiveTabID != "" {
		if ws := m.workbench.CurrentWorkspace(); ws != nil {
			for index, tab := range ws.Tabs {
				if tab != nil && tab.ID == proj.ActiveTabID {
					_ = m.workbench.SwitchTab(ws.Name, index)
					break
				}
			}
		}
	}
	if proj.FocusedPaneID != "" {
		if tab := m.workbench.CurrentTab(); tab != nil && tab.Panes[proj.FocusedPaneID] != nil {
			_ = m.workbench.FocusPane(tab.ID, proj.FocusedPaneID)
		}
	}
}
