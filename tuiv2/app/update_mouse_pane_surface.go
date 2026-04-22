package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) mousePaneChromeRegion(pane workbench.VisiblePane, x, contentY int) (render.HitRegion, bool) {
	if m == nil {
		return render.HitRegion{}, false
	}
	var runtimeState *render.VisibleRuntimeStateProxy
	if m.runtime != nil {
		runtimeState = m.runtime.Visible()
	}
	return render.HitRegionAt(render.PaneChromeHitRegions(pane, runtimeState, m.ownerConfirmPaneID, m.chromeConfig()), x, contentY)
}

func (m *Model) handlePaneChromeRegion(region render.HitRegion) tea.Cmd {
	if m == nil {
		return nil
	}
	if region.Kind == render.HitRegionPaneOwner {
		return m.handleOwnerActionClick(region.PaneID)
	}
	return m.applyMouseSemanticAction(region.Action)
}

func (m *Model) handleEmptyPaneClick(pane workbench.VisiblePane, x, contentY int) tea.Cmd {
	region, ok := render.HitRegionAt(render.EmptyPaneActionRegions(pane), x, contentY)
	if !ok {
		return nil
	}
	switch region.Kind {
	case render.HitRegionEmptyPaneAttach:
		return m.applyMouseSemanticAction(input.SemanticAction{Kind: input.ActionOpenPicker, TargetID: pane.ID, PaneID: pane.ID})
	case render.HitRegionEmptyPaneCreate:
		m.openCreateTerminalPrompt(pane.ID, modal.CreateTargetReplace)
		return nil
	case render.HitRegionEmptyPaneManager:
		return m.openTerminalManagerMouse()
	case render.HitRegionEmptyPaneClose:
		return m.applyMouseSemanticAction(input.SemanticAction{Kind: input.ActionClosePane, PaneID: pane.ID})
	default:
		return nil
	}
}

func (m *Model) handleExitedPaneClick(pane workbench.VisiblePane, x, contentY int) tea.Cmd {
	if m == nil || m.runtime == nil {
		return nil
	}
	region, ok := render.HitRegionAt(render.ExitedPaneRecoveryRegions(pane, m.runtime.Visible()), x, contentY)
	if !ok {
		return nil
	}
	switch region.Kind {
	case render.HitRegionExitedPaneRestart:
		if pane.TerminalID == "" {
			return nil
		}
		return m.restartPaneTerminalCmd(pane.ID, pane.TerminalID)
	case render.HitRegionExitedPaneChoose:
		return tea.Batch(m.openPickerForPaneCmd(pane.ID), m.saveStateCmd())
	default:
		return nil
	}
}

func (m *Model) openTerminalManagerMouse() tea.Cmd {
	if m == nil {
		return nil
	}
	m.openTerminalPool()
	m.render.Invalidate()
	return m.loadTerminalManagerItemsCmd()
}

func (m *Model) ensureFloatingModeTarget() {
	if m == nil || m.workbench == nil {
		return
	}
	tab := m.workbench.CurrentTab()
	if tab == nil || len(tab.Floating) == 0 {
		return
	}
	if active := activeFloatingPaneID(tab); active != "" {
		return
	}
	paneID := topmostFloatingPaneID(tab)
	if paneID == "" {
		return
	}
	_ = m.workbench.FocusPane(tab.ID, paneID)
	m.workbench.ReorderFloatingPane(tab.ID, paneID, true)
}

func activeFloatingPaneID(tab *workbench.TabState) string {
	if tab == nil || tab.ActivePaneID == "" {
		return ""
	}
	for _, floating := range tab.Floating {
		if floating != nil && floating.PaneID == tab.ActivePaneID &&
			(floating.Display == "" || floating.Display == workbench.FloatingDisplayExpanded) {
			return tab.ActivePaneID
		}
	}
	return ""
}

func topmostFloatingPaneID(tab *workbench.TabState) string {
	if tab == nil || len(tab.Floating) == 0 {
		return ""
	}
	paneID := ""
	maxZ := 0
	for _, floating := range tab.Floating {
		if floating == nil || floating.PaneID == "" {
			continue
		}
		if floating.Display != "" && floating.Display != workbench.FloatingDisplayExpanded {
			continue
		}
		if paneID == "" || floating.Z >= maxZ {
			paneID = floating.PaneID
			maxZ = floating.Z
		}
	}
	return paneID
}

func (m *Model) handleOwnerActionClick(paneID string) tea.Cmd {
	if m == nil || strings.TrimSpace(paneID) == "" {
		return nil
	}
	if m.ownerConfirmPaneID == paneID {
		m.ownerConfirmPaneID = ""
		m.ownerSeq++
		return m.becomeOwnerCmd(paneID)
	}
	m.ownerConfirmPaneID = paneID
	m.ownerSeq++
	m.render.Invalidate()
	return clearOwnerConfirmCmd(m.ownerSeq)
}
