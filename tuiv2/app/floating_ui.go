package app

import (
	"context"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/workbench"
)

const floatingOverviewModalID = "floating-overview"

func (m *Model) openFloatingOverview() tea.Cmd {
	if m == nil || m.modalHost == nil || m.workbench == nil {
		return nil
	}
	returnMode := input.ModeNormal
	if m.ui != nil {
		returnMode = m.mode().Kind
	}
	requestID := string(returnMode)
	m.refreshFloatingOverview("")
	if m.modalHost.FloatingOverview == nil {
		return nil
	}
	m.openModal(input.ModeFloatingOverview, requestID)
	m.markModalReady(input.ModeFloatingOverview, requestID)
	m.render.Invalidate()
	return nil
}

func (m *Model) refreshFloatingOverview(selectedPaneID string) {
	if m == nil || m.modalHost == nil || m.workbench == nil {
		return
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		m.modalHost.FloatingOverview = &modal.FloatingOverviewState{}
		return
	}
	ordered := m.workbench.OrderedFloating(tab.ID)
	items := make([]modal.FloatingOverviewItem, 0, len(ordered))
	for i := len(ordered) - 1; i >= 0; i-- {
		floating := ordered[i]
		if floating == nil {
			continue
		}
		pane := tab.Panes[floating.PaneID]
		if pane == nil {
			continue
		}
		items = append(items, modal.FloatingOverviewItem{
			PaneID:     floating.PaneID,
			Title:      pane.Title,
			TerminalID: pane.TerminalID,
			Display:    floating.Display,
			FitMode:    floating.FitMode,
			Rect:       floating.Rect,
		})
	}
	for index := range items {
		if index < 9 {
			items[index].ShortcutSlot = index + 1
		}
	}
	selected := 0
	targetPaneID := strings.TrimSpace(selectedPaneID)
	if targetPaneID == "" && m.modalHost.FloatingOverview != nil {
		if selected := m.modalHost.FloatingOverview.SelectedItem(); selected != nil {
			targetPaneID = selected.PaneID
		}
	}
	if targetPaneID == "" && tab.ActivePaneID != "" {
		targetPaneID = tab.ActivePaneID
	}
	for index := range items {
		if items[index].PaneID == targetPaneID {
			selected = index
			break
		}
	}
	m.modalHost.FloatingOverview = &modal.FloatingOverviewState{
		Items:    items,
		Selected: selected,
	}
}

func (m *Model) closeFloatingOverview() {
	if m == nil || m.modalHost == nil || m.modalHost.Session == nil {
		return
	}
	requestID := m.modalHost.Session.RequestID
	nextMode := input.ModeNormal
	if requestID == string(input.ModeFloating) {
		nextMode = input.ModeFloating
	}
	m.closeModal(input.ModeFloatingOverview, requestID, input.ModeState{Kind: nextMode})
	m.render.Invalidate()
}

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

func (m *Model) summonFloatingPane(slotText string) tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	slot, err := strconv.Atoi(strings.TrimSpace(slotText))
	if err != nil || slot < 1 {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}
	ordered := m.workbench.OrderedFloating(tab.ID)
	if len(ordered) == 0 {
		return nil
	}
	index := 0
	for i := len(ordered) - 1; i >= 0; i-- {
		floating := ordered[i]
		if floating == nil || tab.Panes[floating.PaneID] == nil {
			continue
		}
		index++
		if index != slot {
			continue
		}
		m.workbench.SetFloatingPaneDisplay(tab.ID, floating.PaneID, workbench.FloatingDisplayExpanded)
		_ = m.workbench.FocusPane(tab.ID, floating.PaneID)
		m.workbench.ReorderFloatingPane(tab.ID, floating.PaneID, true)
		if m.modalHost != nil && m.modalHost.Session != nil && m.modalHost.Session.Kind == input.ModeFloatingOverview {
			m.closeFloatingOverview()
		}
		m.refreshFloatingOverview(floating.PaneID)
		m.render.Invalidate()
		return m.saveStateCmd()
	}
	return nil
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
	if m == nil || m.workbench == nil {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil || paneID == "" {
		return nil
	}
	terminalID, err := m.workbench.ClosePane(tab.ID, paneID)
	if err != nil {
		return m.showError(err)
	}
	if m.runtime != nil {
		m.runtime.UnbindPane(paneID, terminalID)
	}
	if m.modalHost != nil && m.modalHost.Session != nil && m.modalHost.Session.Kind == input.ModeFloatingOverview {
		m.refreshFloatingOverview("")
		if m.modalHost.FloatingOverview != nil && len(m.modalHost.FloatingOverview.Items) == 0 {
			m.closeFloatingOverview()
		}
	}
	m.render.Invalidate()
	return m.saveStateCmd()
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

func (m *Model) focusFloatingOverviewSelection() tea.Cmd {
	if m == nil || m.modalHost == nil || m.modalHost.FloatingOverview == nil {
		return nil
	}
	selected := m.modalHost.FloatingOverview.SelectedItem()
	if selected == nil {
		return nil
	}
	cmd := m.summonFloatingPane(strconv.Itoa(maxInt(1, selected.ShortcutSlot)))
	if selected.ShortcutSlot == 0 {
		cmd = m.summonFloatingPaneByPaneID(selected.PaneID)
	}
	m.closeFloatingOverview()
	return cmd
}

func (m *Model) summonFloatingPaneByPaneID(paneID string) tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil || paneID == "" {
		return nil
	}
	if m.workbench.SetFloatingPaneDisplay(tab.ID, paneID, workbench.FloatingDisplayExpanded) {
		_ = m.workbench.FocusPane(tab.ID, paneID)
		m.workbench.ReorderFloatingPane(tab.ID, paneID, true)
		m.refreshFloatingOverview(paneID)
		m.render.Invalidate()
		return m.saveStateCmd()
	}
	return nil
}

func (m *Model) refreshFloatingOverviewAfterAction(paneID string) {
	if m == nil || m.modalHost == nil || m.modalHost.Session == nil || m.modalHost.Session.Kind != input.ModeFloatingOverview {
		return
	}
	m.refreshFloatingOverview(paneID)
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
