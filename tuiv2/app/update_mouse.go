package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) handleMouseMsg(msg tea.MouseMsg) tea.Cmd {
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			return m.handleMouseClick(msg)
		}
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
			return m.handleMouseWheel(msg)
		}
		return m.forwardTerminalMouseInputCmd(msg)
	case tea.MouseActionMotion:
		if msg.Button == tea.MouseButtonLeft && m.copyMode.MouseSelecting {
			return m.updateMouseCopySelection(msg.X, msg.Y)
		}
		if msg.Button == tea.MouseButtonLeft && m.mouseDragMode != mouseDragNone {
			perftrace.Count("app.mouse.drag.motion", 0)
			return m.handleMouseDrag(msg.X, msg.Y)
		}
		return m.forwardTerminalMouseInputCmd(msg)
	case tea.MouseActionRelease:
		if msg.Button == tea.MouseButtonLeft && m.copyMode.MouseSelecting {
			m.stopMouseCopySelection()
			return nil
		}
		if msg.Button == tea.MouseButtonLeft && m.mouseDragMode != mouseDragNone {
			return m.handleMouseRelease()
		}
		return m.forwardTerminalMouseInputCmd(msg)
	}
	return nil
}

func (m *Model) handleMouseWheel(msg tea.MouseMsg) tea.Cmd {
	return m.handleMouseWheelRepeated(msg, 1)
}

func (m *Model) handleMouseWheelBurstMsg(msg mouseWheelBurstMsg) tea.Cmd {
	return m.handleMouseWheelRepeated(msg.Msg, maxInt(1, msg.Repeat))
}

func (m *Model) handleMouseWheelRepeated(msg tea.MouseMsg, repeat int) tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	x := msg.X
	y := msg.Y
	button := msg.Button
	step := -1
	if button == tea.MouseButtonWheelUp {
		step = 1
	}
	repeat = maxInt(1, repeat)
	delta := step * repeat
	state := m.visibleRenderState()
	if handled, cmd := m.handleOverlayMouseWheel(state, delta); handled {
		return cmd
	}
	if handled, cmd := m.handleTerminalPoolMouseWheel(state, delta); handled {
		return cmd
	}
	if m.mode().Kind == input.ModeDisplay {
		return m.moveCopyCursorVertical(-delta)
	}
	if y < m.contentOriginY() {
		if delta > 0 {
			return m.switchCurrentTabByOffsetMouse(-repeat)
		}
		return m.switchCurrentTabByOffsetMouse(repeat)
	}
	if in, ok := m.terminalWheelInputForMouseMsg(msg, step, repeat); ok {
		return m.handleTerminalInput(in)
	}
	if in, ok := m.alternateScreenWheelInputForMouseMsg(msg, step, repeat); ok {
		return m.handleTerminalInput(in)
	}

	contentY := y - m.contentOriginY()
	if contentY < 0 {
		return nil
	}
	if _, floating, ok := m.visiblePaneAt(x, contentY); ok {
		tab := m.workbench.CurrentTab()
		if tab == nil {
			return nil
		}
		if floating != nil {
			if tab.ActivePaneID != floating.ID {
				_ = m.workbench.FocusPane(tab.ID, floating.ID)
				m.workbench.ReorderFloatingPane(tab.ID, floating.ID, true)
			}
		}
		tab.ScrollOffset += delta
		if tab.ScrollOffset < 0 {
			tab.ScrollOffset = 0
		}
		m.render.Invalidate()
		return m.ensureActivePaneScrollbackCmd()
	}
	return nil
}

func (m *Model) forwardTerminalWheelInputCmd(msg tea.MouseMsg, delta int) tea.Cmd {
	in, ok := m.terminalWheelInputForMouseMsg(msg, delta, 1)
	if !ok {
		return nil
	}
	return terminalWheelInputCmd(in)
}

func (m *Model) forwardAlternateScreenWheelCmd(msg tea.MouseMsg, delta int) tea.Cmd {
	in, ok := m.alternateScreenWheelInputForMouseMsg(msg, delta, 1)
	if !ok {
		return nil
	}
	return terminalWheelInputCmd(in)
}

func (m *Model) terminalWheelInputForMouseMsg(msg tea.MouseMsg, delta, repeat int) (input.TerminalInput, bool) {
	if m == nil || m.workbench == nil {
		return input.TerminalInput{}, false
	}
	if m.mode().Kind == input.ModeDisplay {
		return input.TerminalInput{}, false
	}
	state := m.visibleRenderState()
	if state.Overlay.Kind != render.VisibleOverlayNone || state.Surface.Kind == render.VisibleSurfaceTerminalPool {
		return input.TerminalInput{}, false
	}
	targetPaneID, contentRect, ok := m.activeContentMouseTarget(msg.X, msg.Y)
	if !ok {
		return input.TerminalInput{}, false
	}
	contentMsg := msg
	contentMsg.Y = msg.Y - m.contentOriginY()
	encoded := m.encodeTerminalMouseInput(contentMsg, targetPaneID, contentRect)
	if len(encoded) == 0 {
		return input.TerminalInput{}, false
	}
	return input.TerminalInput{
		Kind:           input.TerminalInputWheel,
		PaneID:         targetPaneID,
		Data:           encoded,
		Repeat:         maxInt(1, repeat),
		WheelDirection: delta,
	}, true
}

func (m *Model) alternateScreenWheelInputForMouseMsg(msg tea.MouseMsg, delta, repeat int) (input.TerminalInput, bool) {
	if m == nil || m.workbench == nil {
		return input.TerminalInput{}, false
	}
	targetPaneID, _, ok := m.activeContentMouseTarget(msg.X, msg.Y)
	if !ok {
		return input.TerminalInput{}, false
	}
	pane := m.activePaneForInput(targetPaneID)
	if pane == nil || pane.TerminalID == "" {
		return input.TerminalInput{}, false
	}
	modes := m.terminalModesForPane(pane)
	if (!modes.AlternateScreen && !modes.AlternateScroll) || modes.MouseTracking {
		return input.TerminalInput{}, false
	}
	encoded := encodeTerminalWheelFallback(msg, modes)
	if len(encoded) == 0 {
		return input.TerminalInput{}, false
	}
	return input.TerminalInput{
		Kind:           input.TerminalInputWheel,
		PaneID:         targetPaneID,
		Data:           encoded,
		Repeat:         maxInt(1, repeat),
		WheelDirection: delta,
	}, true
}

func terminalWheelInputCmd(in input.TerminalInput) tea.Cmd {
	if in.PaneID == "" || len(in.Data) == 0 || in.WheelDirection == 0 {
		return nil
	}
	return func() tea.Msg {
		in.Repeat = maxInt(1, in.Repeat)
		return in
	}
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
	state := m.visibleRenderState()
	if handled, cmd := m.handleOverlayMouseClick(state, x, y); handled {
		return cmd
	}
	if handled, cmd := m.handleTerminalPoolMouseClick(state, x, y); handled {
		return cmd
	}
	if handled, cmd := m.handleTopChromeMouseClick(state, x, y); handled {
		return cmd
	}
	if handled, cmd := m.handleBottomChromeMouseClick(state, x, y); handled {
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
		} else {
			m.mouseDragPaneID = ""
			m.mouseDragMode = mouseDragNone
		}
		m.render.Invalidate()
		return nil
	}

	return m.handleMouseClickNonFloating(x, y)
}
