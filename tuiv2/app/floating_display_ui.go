package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) hideFloatingPane(paneID string) tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil || paneID == "" {
		return nil
	}
	if !m.workbench.SetFloatingPaneDisplay(tab.ID, paneID, workbench.FloatingDisplayCollapsed) {
		return nil
	}
	if tab.ActivePaneID == paneID {
		if next := m.workbench.TopmostVisibleFloatingPaneID(tab.ID); next != "" {
			_ = m.workbench.FocusPane(tab.ID, next)
			m.workbench.ReorderFloatingPane(tab.ID, next, true)
		} else {
			m.focusFallbackTiledPane(tab)
		}
	}
	m.refreshFloatingOverview("")
	m.render.Invalidate()
	return m.saveStateCmd()
}

func (m *Model) toggleAllFloatingPanes() tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}
	if m.workbench.Visible().FloatingTotal == 0 {
		return nil
	}
	changed := false
	if visible := m.workbench.TopmostVisibleFloatingPaneID(tab.ID); visible != "" {
		changed = m.workbench.CollapseAllFloatingPanes(tab.ID)
		m.focusFallbackTiledPane(tab)
	} else {
		changed = m.workbench.ExpandAllFloatingPanes(tab.ID)
		if next := m.workbench.TopmostVisibleFloatingPaneID(tab.ID); next != "" {
			_ = m.workbench.FocusPane(tab.ID, next)
			m.workbench.ReorderFloatingPane(tab.ID, next, true)
		}
	}
	if !changed {
		return nil
	}
	m.refreshFloatingOverview("")
	m.render.Invalidate()
	return m.saveStateCmd()
}

func (m *Model) expandAllFloatingPanes() tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil || !m.workbench.ExpandAllFloatingPanes(tab.ID) {
		return nil
	}
	if next := m.workbench.TopmostVisibleFloatingPaneID(tab.ID); next != "" {
		_ = m.workbench.FocusPane(tab.ID, next)
		m.workbench.ReorderFloatingPane(tab.ID, next, true)
	}
	m.refreshFloatingOverview("")
	m.render.Invalidate()
	return m.saveStateCmd()
}

func (m *Model) collapseAllFloatingPanes() tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil || !m.workbench.CollapseAllFloatingPanes(tab.ID) {
		return nil
	}
	m.focusFallbackTiledPane(tab)
	m.refreshFloatingOverview("")
	m.render.Invalidate()
	return m.saveStateCmd()
}

func (m *Model) closeFloatingPaneDirect(paneID string) tea.Cmd {
	if m == nil || m.orchestrator == nil || paneID == "" {
		return nil
	}
	return m.semanticActionEffectsCmd(input.SemanticAction{
		Kind:   input.ActionCloseFloatingPane,
		PaneID: paneID,
	})
}

func (m *Model) focusFallbackTiledPane(tab *workbench.TabState) {
	if m == nil || m.workbench == nil || tab == nil {
		return
	}
	if tab.Root != nil {
		for _, paneID := range tab.Root.LeafIDs() {
			if paneID == "" || tab.Panes[paneID] == nil {
				continue
			}
			_ = m.workbench.FocusPane(tab.ID, paneID)
			return
		}
	}
	for paneID := range tab.Panes {
		_ = m.workbench.FocusPane(tab.ID, paneID)
		return
	}
}
