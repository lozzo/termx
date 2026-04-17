package app

import "github.com/lozzow/termx/tuiv2/workbench"

func (m *Model) paneViewportBindingOffset(paneID string) (int, bool) {
	if m == nil || m.runtime == nil || paneID == "" {
		return 0, false
	}
	binding := m.runtime.Binding(paneID)
	if binding == nil {
		return 0, false
	}
	if binding.Viewport.Offset < 0 {
		return 0, true
	}
	return binding.Viewport.Offset, true
}

func (m *Model) legacyPaneViewportOffset(paneID string) int {
	if m == nil || m.workbench == nil || paneID == "" {
		return 0
	}
	for _, wsName := range m.workbench.ListWorkspaces() {
		ws := m.workbench.WorkspaceByName(wsName)
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil || tab.ActivePaneID != paneID || tab.Panes[paneID] == nil {
				continue
			}
			if tab.ScrollOffset < 0 {
				return 0
			}
			return tab.ScrollOffset
		}
	}
	return 0
}

func (m *Model) paneViewportOffset(paneID string) int {
	if offset, ok := m.paneViewportBindingOffset(paneID); ok {
		return offset
	}
	return m.legacyPaneViewportOffset(paneID)
}

func (m *Model) effectiveTabViewportOffset(tab *workbench.TabState) int {
	if tab == nil {
		return 0
	}
	if tab.ActivePaneID != "" {
		return m.paneViewportOffset(tab.ActivePaneID)
	}
	if tab.ScrollOffset < 0 {
		return 0
	}
	return tab.ScrollOffset
}

func (m *Model) setPaneViewportOffset(paneID string, offset int) bool {
	if m == nil || paneID == "" {
		return false
	}
	if m.runtime != nil {
		return m.runtime.SetPaneViewportOffset(paneID, offset)
	}
	if m.workbench == nil {
		return false
	}
	for _, wsName := range m.workbench.ListWorkspaces() {
		ws := m.workbench.WorkspaceByName(wsName)
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil || tab.ActivePaneID != paneID || tab.Panes[paneID] == nil {
				continue
			}
			return m.workbench.SetTabScrollOffset(tab.ID, offset)
		}
	}
	return false
}

func (m *Model) adjustPaneViewportOffset(paneID string, delta int) (int, bool) {
	if m == nil || paneID == "" {
		return 0, false
	}
	if m.runtime != nil {
		if _, ok := m.paneViewportBindingOffset(paneID); !ok {
			if legacy := m.legacyPaneViewportOffset(paneID); legacy > 0 {
				_ = m.runtime.SetPaneViewportOffset(paneID, legacy)
			}
		}
		return m.runtime.AdjustPaneViewportOffset(paneID, delta)
	}
	if m.workbench == nil {
		return 0, false
	}
	for _, wsName := range m.workbench.ListWorkspaces() {
		ws := m.workbench.WorkspaceByName(wsName)
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil || tab.ActivePaneID != paneID || tab.Panes[paneID] == nil {
				continue
			}
			return m.workbench.AdjustTabScrollOffset(tab.ID, delta)
		}
	}
	return 0, false
}

func (m *Model) resetPaneViewport(paneID string) {
	if m == nil || paneID == "" {
		return
	}
	if m.setPaneViewportOffset(paneID, 0) {
		m.render.Invalidate()
	}
}
