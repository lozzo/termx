package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type pendingPaneResize struct {
	TabID      string
	PaneID     string
	TerminalID string
}

func (m *Model) hasViewportSize() bool {
	return m != nil && m.width > 0 && m.height > 0
}

func (m *Model) markPendingPaneResize(tabID, paneID, terminalID string) {
	if m == nil || paneID == "" || terminalID == "" {
		return
	}
	if m.pendingPaneResizes == nil {
		m.pendingPaneResizes = make(map[string]pendingPaneResize)
	}
	m.pendingPaneResizes[paneID] = pendingPaneResize{
		TabID:      tabID,
		PaneID:     paneID,
		TerminalID: terminalID,
	}
}

func (m *Model) clearPendingPaneResize(paneID, terminalID string) {
	if m == nil || len(m.pendingPaneResizes) == 0 || paneID == "" {
		return
	}
	current, ok := m.pendingPaneResizes[paneID]
	if !ok {
		return
	}
	if terminalID != "" && current.TerminalID != "" && current.TerminalID != terminalID {
		return
	}
	delete(m.pendingPaneResizes, paneID)
}

func (m *Model) paneResizeTarget(tabID, paneID string) (*workbench.PaneState, workbench.Rect, bool) {
	if m == nil || m.workbench == nil || paneID == "" || !m.hasViewportSize() {
		return nil, workbench.Rect{}, false
	}
	workspace := m.workbench.CurrentWorkspace()
	if workspace == nil {
		return nil, workbench.Rect{}, false
	}
	var tabState *workbench.TabState
	if tabID != "" {
		for _, tab := range workspace.Tabs {
			if tab != nil && tab.ID == tabID {
				tabState = tab
				break
			}
		}
	} else {
		current := m.workbench.CurrentTab()
		if current != nil && current.Panes[paneID] != nil {
			tabState = current
		}
		if tabState == nil {
			for _, tab := range workspace.Tabs {
				if tab != nil && tab.Panes[paneID] != nil {
					tabState = tab
					break
				}
			}
		}
	}
	if tabState == nil {
		return nil, workbench.Rect{}, false
	}
	pane := tabState.Panes[paneID]
	if pane == nil {
		return nil, workbench.Rect{}, false
	}
	visible := m.workbench.VisibleWithSize(m.bodyRect())
	if visible == nil {
		return nil, workbench.Rect{}, false
	}
	currentTab := m.workbench.CurrentTab()
	for _, floating := range tabState.Floating {
		if floating == nil || floating.PaneID != paneID {
			continue
		}
		if currentTab != nil && currentTab.ID == tabState.ID {
			for i := range visible.FloatingPanes {
				if visible.FloatingPanes[i].ID == paneID {
					return pane, visible.FloatingPanes[i].Rect, true
				}
			}
		}
		display := floating.Display
		if display == "" {
			display = workbench.FloatingDisplayExpanded
		}
		if display != workbench.FloatingDisplayExpanded || floating.Rect.W <= 0 || floating.Rect.H <= 0 {
			return nil, workbench.Rect{}, false
		}
		return pane, floating.Rect, true
	}
	for _, tab := range visible.Tabs {
		if tab.ID != tabState.ID {
			continue
		}
		for _, visiblePane := range tab.Panes {
			if visiblePane.ID == paneID && visiblePane.Rect.W > 0 && visiblePane.Rect.H > 0 {
				return pane, visiblePane.Rect, true
			}
		}
		return nil, workbench.Rect{}, false
	}
	return nil, workbench.Rect{}, false
}

func (m *Model) resizePendingPaneResizesCmd() tea.Cmd {
	if m == nil || m.runtime == nil || m.sessionID != "" || len(m.pendingPaneResizes) == 0 || !m.hasViewportSize() {
		return nil
	}
	pending := make([]pendingPaneResize, 0, len(m.pendingPaneResizes))
	for _, resize := range m.pendingPaneResizes {
		pending = append(pending, resize)
	}
	cmds := make([]tea.Cmd, 0, len(pending))
	for _, resize := range pending {
		pane, rect, ok := m.paneResizeTarget(resize.TabID, resize.PaneID)
		if !ok || pane == nil {
			m.clearPendingPaneResize(resize.PaneID, resize.TerminalID)
			continue
		}
		if pane.TerminalID == "" || pane.TerminalID != resize.TerminalID {
			m.clearPendingPaneResize(resize.PaneID, resize.TerminalID)
			continue
		}
		target := resize
		targetRect := rect
		cmds = append(cmds, func() tea.Msg {
			if err := m.ensurePaneTerminalSize(context.Background(), target.PaneID, target.TerminalID, targetRect); err != nil {
				return err
			}
			if m.pendingPaneResizeSatisfied(target.PaneID, target.TerminalID, targetRect) {
				m.clearPendingPaneResize(target.PaneID, target.TerminalID)
			}
			return nil
		})
	}
	return batchCmds(cmds...)
}

func (m *Model) pendingPaneResizeSatisfied(paneID, terminalID string, rect workbench.Rect) bool {
	if m == nil || terminalID == "" {
		return false
	}
	viewportRect, ok := m.terminalViewportRect(paneID, rect)
	if !ok {
		return false
	}
	cols := uint16(maxInt(2, viewportRect.W))
	rows := uint16(maxInt(2, viewportRect.H))
	return m.terminalAlreadySized(terminalID, cols, rows)
}
