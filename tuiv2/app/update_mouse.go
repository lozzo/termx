package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) handleMouseMsg(msg tea.MouseMsg) tea.Cmd {
	decision := m.routeMouseInteraction(msg)
	switch decision.Kind {
	case interactionDecisionMouseDrag:
		perftrace.Count("app.mouse.drag.motion", 0)
	}
	return m.dispatchMouseInteraction(decision)
}

func (m *Model) handleMouseClickNonFloating(x, y int) tea.Cmd {
	if m.workbench == nil {
		return nil
	}

	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}

	contentY := y - m.contentOriginY()
	if contentY < 0 {
		return nil
	}

	bodyRect := m.bodyRect()
	visible := m.workbench.VisibleWithSize(bodyRect)
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		return nil
	}

	visibleTab := visible.Tabs[visible.ActiveTab]
	for _, pane := range visibleTab.Panes {
		rect := pane.Rect
		if x >= rect.X && x < rect.X+rect.W && contentY >= rect.Y && contentY < rect.Y+rect.H {
			if region, ok := m.mousePaneChromeRegion(pane, x, contentY); ok {
				if pane.ID != tab.ActivePaneID {
					_ = m.workbench.FocusPane(tab.ID, pane.ID)
				}
				m.render.Invalidate()
				return m.handlePaneChromeRegion(region)
			}
			if cmd := m.handleEmptyPaneClick(pane, x, contentY); cmd != nil {
				if pane.ID != tab.ActivePaneID {
					_ = m.workbench.FocusPane(tab.ID, pane.ID)
				}
				m.render.Invalidate()
				return cmd
			}
			if cmd := m.handleExitedPaneClick(pane, x, contentY); cmd != nil {
				if pane.ID != tab.ActivePaneID {
					_ = m.workbench.FocusPane(tab.ID, pane.ID)
				}
				m.render.Invalidate()
				return cmd
			}
		}
	}

	if tab.Root != nil {
		if hit, ok := tab.Root.DividerAt(bodyRect, x, contentY); ok {
			if m.currentTabHasLockedTerminal() {
				return m.showNotice(terminalSizeLockedNotice)
			}
			m.mouseDragPaneID = ""
			m.mouseDragMode = mouseDragResizeSplit
			m.mouseDragSplit = hit.Node
			m.mouseDragBounds = hit.Root
			m.mouseDragDirty = false
			m.mouseDragOffsetX = x - hit.Rect.X
			m.mouseDragOffsetY = contentY - hit.Rect.Y
			return nil
		}
	}

	tiled, _, ok := m.visiblePaneAt(x, contentY)
	if !ok || tiled == nil {
		return nil
	}
	if tiled.ID != tab.ActivePaneID {
		_ = m.workbench.FocusPane(tab.ID, tiled.ID)
		m.render.Invalidate()
	}
	return nil
}

func (m *Model) handleMouseClick(msg tea.MouseMsg) tea.Cmd {
	x := msg.X
	y := msg.Y
	vm := m.renderVM()
	if handled, cmd := m.handleOverlayMouseClick(vm, x, y); handled {
		return cmd
	}
	if handled, cmd := m.handleTerminalPoolMouseClick(vm, x, y); handled {
		return cmd
	}
	if handled, cmd := m.handleTopChromeMouseClick(vm, x, y); handled {
		return cmd
	}
	if handled, cmd := m.handleBottomChromeMouseClick(vm, x, y); handled {
		return cmd
	}
	if m.mode().Kind == input.ModeDisplay && m.startMouseCopySelection(x, y) {
		return nil
	}
	if cmd := m.forwardTerminalMouseInputCmd(msg); cmd != nil {
		return cmd
	}

	if m.workbench == nil {
		return nil
	}

	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}

	contentY := y - m.contentOriginY()
	if contentY < 0 {
		return nil
	}

	paneID, rect, isResize := m.findFloatingPaneAt(tab, x, contentY)
	if paneID != "" {
		bodyRect := m.bodyRect()
		visible := m.workbench.VisibleWithSize(bodyRect)
		if visible != nil {
			for _, pane := range visible.FloatingPanes {
				if pane.ID != paneID {
					continue
				}
				if region, ok := m.mousePaneChromeRegion(pane, x, contentY); ok {
					if tab.ActivePaneID != paneID {
						_ = m.workbench.FocusPane(tab.ID, paneID)
					}
					m.workbench.ReorderFloatingPane(tab.ID, paneID, true)
					m.render.Invalidate()
					return m.handlePaneChromeRegion(region)
				}
				if cmd := m.handleEmptyPaneClick(pane, x, contentY); cmd != nil {
					if tab.ActivePaneID != paneID {
						_ = m.workbench.FocusPane(tab.ID, paneID)
					}
					m.workbench.ReorderFloatingPane(tab.ID, paneID, true)
					m.render.Invalidate()
					return cmd
				}
				if cmd := m.handleExitedPaneClick(pane, x, contentY); cmd != nil {
					if tab.ActivePaneID != paneID {
						_ = m.workbench.FocusPane(tab.ID, paneID)
					}
					m.workbench.ReorderFloatingPane(tab.ID, paneID, true)
					m.render.Invalidate()
					return cmd
				}
				break
			}
		}
		if tab.ActivePaneID != paneID {
			_ = m.workbench.FocusPane(tab.ID, paneID)
		}
		m.workbench.ReorderFloatingPane(tab.ID, paneID, true)

		m.mouseDragPaneID = paneID
		m.mouseDragSplit = nil
		m.mouseDragBounds = workbench.Rect{}
		if isResize {
			if m.paneTerminalSizeLocked(paneID) {
				return m.showNotice(terminalSizeLockedNotice)
			}
			m.mouseDragMode = mouseDragResize
			m.mouseDragOffsetX = 0
			m.mouseDragOffsetY = 0
		} else if contentY == rect.Y {
			m.mouseDragMode = mouseDragMove
			m.mouseDragOffsetX = x - rect.X
			m.mouseDragOffsetY = contentY - rect.Y
			m.beginFloatingDragPreview(paneID, rect)
		} else {
			m.mouseDragPaneID = ""
			m.mouseDragMode = mouseDragNone
		}
		m.render.Invalidate()
		return nil
	}

	return m.handleMouseClickNonFloating(x, y)
}
