package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
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

func (m *Model) toggleFloatingAutoFit(paneID string) tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}
	target := m.currentOrActionPaneID(paneID)
	if target == "" {
		target = activeFloatingPaneID(tab)
	}
	if target == "" {
		return nil
	}
	floating := m.workbench.FloatingState(tab.ID, target)
	if floating == nil {
		return nil
	}
	nextMode := workbench.FloatingFitAuto
	if floating.FitMode == workbench.FloatingFitAuto {
		nextMode = workbench.FloatingFitManual
	}
	if !m.workbench.SetFloatingPaneFitMode(tab.ID, target, nextMode) {
		return nil
	}
	m.refreshFloatingOverview(target)
	m.render.Invalidate()
	if nextMode == workbench.FloatingFitAuto {
		return tea.Batch(m.fitFloatingPaneToContent(target), m.saveStateCmd())
	}
	return m.saveStateCmd()
}

func (m *Model) fitFloatingPaneToContent(paneID string) tea.Cmd {
	if m == nil || m.workbench == nil || m.runtime == nil {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}
	target := m.currentOrActionPaneID(paneID)
	if target == "" {
		target = activeFloatingPaneID(tab)
	}
	if target == "" {
		return nil
	}
	floating := m.workbench.FloatingState(tab.ID, target)
	if floating == nil {
		return nil
	}
	pane := tab.Panes[target]
	if pane == nil || pane.TerminalID == "" {
		return nil
	}
	if binding := m.runtime.Binding(target); binding != nil && binding.Role == "follower" {
		return nil
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil {
		return nil
	}
	if terminal.VTerm != nil && terminal.SnapshotVersion != terminal.SurfaceVersion {
		m.runtime.RefreshSnapshotFromVTerm(pane.TerminalID)
		terminal = m.runtime.Registry().Get(pane.TerminalID)
	}
	if terminal == nil || terminal.Snapshot == nil {
		return nil
	}
	cols, rows := floatingContentExtent(terminal.Snapshot)
	if cols <= 0 || rows <= 0 {
		return nil
	}
	changed := false
	if floating.FitMode == workbench.FloatingFitAuto {
		changed = m.workbench.ApplyFloatingAutoFit(tab.ID, target, cols, rows, m.bodyRect())
	} else {
		nextW := cols + 2
		nextH := rows + 2
		if m.workbench.ResizeFloatingPane(tab.ID, target, nextW, nextH) {
			changed = true
		}
		if m.workbench.SetFloatingPaneAutoFitSize(tab.ID, target, cols, rows) {
			changed = true
		}
	}
	if !changed {
		return nil
	}
	m.workbench.ClampFloatingPanesToBounds(m.bodyRect())
	m.refreshFloatingOverview(target)
	m.render.Invalidate()
	return tea.Batch(m.resizePaneIfNeededCmd(target), m.saveStateCmd())
}

func (m *Model) maybeAutoFitFloatingPanesCmd() tea.Cmd {
	if m == nil || m.workbench == nil || m.runtime == nil {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}
	changed := false
	for _, floating := range tab.Floating {
		if floating == nil || floating.Display != workbench.FloatingDisplayExpanded || floating.FitMode != workbench.FloatingFitAuto {
			continue
		}
		pane := tab.Panes[floating.PaneID]
		if pane == nil || pane.TerminalID == "" {
			continue
		}
		if binding := m.runtime.Binding(floating.PaneID); binding != nil && binding.Role == "follower" {
			continue
		}
		terminal := m.runtime.Registry().Get(pane.TerminalID)
		if terminal == nil {
			continue
		}
		if terminal.VTerm != nil && terminal.SnapshotVersion != terminal.SurfaceVersion {
			m.runtime.RefreshSnapshotFromVTerm(pane.TerminalID)
			terminal = m.runtime.Registry().Get(pane.TerminalID)
		}
		if terminal == nil || terminal.Snapshot == nil {
			continue
		}
		cols, rows := floatingContentExtent(terminal.Snapshot)
		if cols <= 0 || rows <= 0 {
			continue
		}
		if cols == floating.AutoFitCols && rows == floating.AutoFitRows {
			continue
		}
		if m.workbench.ApplyFloatingAutoFit(tab.ID, floating.PaneID, cols, rows, m.bodyRect()) {
			changed = true
		}
	}
	if !changed {
		return nil
	}
	m.workbench.ClampFloatingPanesToBounds(m.bodyRect())
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

func (m *Model) disableFloatingAutoFitForActionPane(paneID string) {
	if m == nil || m.workbench == nil {
		return
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return
	}
	target := strings.TrimSpace(paneID)
	if target == "" {
		target = activeFloatingPaneID(tab)
	}
	if target == "" {
		return
	}
	m.workbench.SetFloatingPaneFitMode(tab.ID, target, workbench.FloatingFitManual)
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

func floatingContentExtent(snapshot *protocol.Snapshot) (int, int) {
	if snapshot == nil {
		return 0, 0
	}
	cols := int(snapshot.Size.Cols)
	rows := int(snapshot.Size.Rows)
	if rows < len(snapshot.Screen.Cells) {
		rows = len(snapshot.Screen.Cells)
	}
	for _, row := range snapshot.Screen.Cells {
		rowWidth := 0
		for _, cell := range row {
			switch {
			case cell.Width > 0:
				rowWidth += cell.Width
			case cell.Content != "":
				rowWidth++
			default:
				rowWidth++
			}
		}
		if rowWidth > cols {
			cols = rowWidth
		}
	}
	return maxInt(2, cols), maxInt(2, rows)
}

func (m *Model) loadFloatingPaneSnapshot(paneID string) *protocol.Snapshot {
	if m == nil || m.workbench == nil || m.runtime == nil {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}
	pane := tab.Panes[paneID]
	if pane == nil || pane.TerminalID == "" {
		return nil
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil {
		return nil
	}
	if terminal.VTerm != nil && terminal.SnapshotVersion != terminal.SurfaceVersion {
		m.runtime.RefreshSnapshotFromVTerm(pane.TerminalID)
		terminal = m.runtime.Registry().Get(pane.TerminalID)
	}
	if terminal == nil {
		return nil
	}
	return terminal.Snapshot
}

func (m *Model) refreshFloatingSnapshotForAutoFit(paneID string) tea.Cmd {
	if m == nil || m.workbench == nil || m.runtime == nil {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil || paneID == "" {
		return nil
	}
	pane := tab.Panes[paneID]
	if pane == nil || pane.TerminalID == "" {
		return nil
	}
	return func() tea.Msg {
		_, _ = m.runtime.LoadSnapshot(context.Background(), pane.TerminalID, 0, defaultTerminalSnapshotScrollbackLimit)
		return nil
	}
}
