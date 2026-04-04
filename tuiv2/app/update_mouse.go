package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) handleMouseMsg(msg tea.MouseMsg) tea.Cmd {
	if m.modalHost != nil && m.modalHost.Session != nil {
		kind := m.modalHost.Session.Kind
		if kind == input.ModePicker || kind == input.ModePrompt ||
			kind == input.ModeHelp || kind == input.ModeTerminalManager ||
			kind == input.ModeWorkspacePicker {
			return nil
		}
	}

	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			return m.handleMouseClick(msg.X, msg.Y)
		}
	case tea.MouseActionMotion:
		if msg.Button == tea.MouseButtonLeft && m.mouseDragPaneID != "" {
			return m.handleMouseDrag(msg.X, msg.Y)
		}
	case tea.MouseActionRelease:
		if msg.Button == tea.MouseButtonLeft {
			return m.handleMouseRelease()
		}
	}
	return nil
}

func (m *Model) handleMouseClickNonFloating(x, y int) tea.Cmd {
	if m.workbench == nil {
		return nil
	}

	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}

	contentY := y - 1
	if contentY < 0 {
		return nil
	}

	bodyRect := workbench.Rect{W: maxInt(1, m.width), H: maxInt(1, m.height-2)}
	visible := m.workbench.VisibleWithSize(bodyRect)
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		return nil
	}

	visibleTab := visible.Tabs[visible.ActiveTab]
	for _, pane := range visibleTab.Panes {
		rect := pane.Rect
		if x >= rect.X && x < rect.X+rect.W && contentY >= rect.Y && contentY < rect.Y+rect.H {
			if m.mouseHitsOwnerButton(pane, x, contentY) {
				if pane.ID != tab.ActivePaneID {
					_ = m.workbench.FocusPane(tab.ID, pane.ID)
				}
				m.render.Invalidate()
				return m.handleOwnerActionClick(pane.ID)
			}
			if pane.ID != tab.ActivePaneID {
				_ = m.workbench.FocusPane(tab.ID, pane.ID)
				m.render.Invalidate()
			}
			return nil
		}
	}

	return nil
}

func (m *Model) handleMouseClick(x, y int) tea.Cmd {
	if m.workbench == nil {
		return nil
	}

	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}

	contentY := y - 1
	if contentY < 0 {
		return nil
	}

	paneID, rect, isResize := m.findFloatingPaneAt(tab, x, contentY)
	if paneID != "" {
		bodyRect := workbench.Rect{W: maxInt(1, m.width), H: maxInt(1, m.height-2)}
		visible := m.workbench.VisibleWithSize(bodyRect)
		if visible != nil {
			for _, pane := range visible.FloatingPanes {
				if pane.ID != paneID {
					continue
				}
				if m.mouseHitsOwnerButton(pane, x, contentY) {
					if tab.ActivePaneID != paneID {
						_ = m.workbench.FocusPane(tab.ID, paneID)
					}
					m.workbench.ReorderFloatingPane(tab.ID, paneID, true)
					m.render.Invalidate()
					return m.handleOwnerActionClick(paneID)
				}
				break
			}
		}
		if tab.ActivePaneID != paneID {
			_ = m.workbench.FocusPane(tab.ID, paneID)
		}
		m.workbench.ReorderFloatingPane(tab.ID, paneID, true)

		m.mouseDragPaneID = paneID
		if isResize {
			m.mouseDragMode = mouseDragResize
			m.mouseDragOffsetX = 0
			m.mouseDragOffsetY = 0
		} else {
			m.mouseDragMode = mouseDragMove
			m.mouseDragOffsetX = x - rect.X
			m.mouseDragOffsetY = contentY - rect.Y
		}
		m.render.Invalidate()
		return nil
	}

	return m.handleMouseClickNonFloating(x, y)
}

func (m *Model) handleMouseDrag(x, y int) tea.Cmd {
	if m.workbench == nil || m.mouseDragPaneID == "" {
		return nil
	}

	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}

	contentY := y - 1
	if contentY < 0 {
		contentY = 0
	}

	switch m.mouseDragMode {
	case mouseDragMove:
		newX := x - m.mouseDragOffsetX
		newY := contentY - m.mouseDragOffsetY
		m.workbench.MoveFloatingPane(tab.ID, m.mouseDragPaneID, newX, newY)
		m.workbench.ClampFloatingPanesToBounds(workbench.Rect{W: maxInt(1, m.width), H: maxInt(1, m.height-2)})
		m.render.Invalidate()
	case mouseDragResize:
		for _, floating := range tab.Floating {
			if floating != nil && floating.PaneID == m.mouseDragPaneID {
				newW := x - floating.Rect.X + 1
				newH := contentY - floating.Rect.Y + 1
				m.workbench.ResizeFloatingPane(tab.ID, m.mouseDragPaneID, newW, newH)
				m.workbench.ClampFloatingPanesToBounds(workbench.Rect{W: maxInt(1, m.width), H: maxInt(1, m.height-2)})
				m.render.Invalidate()
				return m.resizeVisiblePanesCmd()
			}
		}
	}

	return nil
}

func (m *Model) handleMouseRelease() tea.Cmd {
	m.mouseDragPaneID = ""
	m.mouseDragOffsetX = 0
	m.mouseDragOffsetY = 0
	m.mouseDragMode = mouseDragNone
	return nil
}

func (m *Model) findFloatingPaneAt(tab *workbench.TabState, x, y int) (string, workbench.Rect, bool) {
	if tab == nil || len(tab.Floating) == 0 {
		return "", workbench.Rect{}, false
	}

	for i := len(tab.Floating) - 1; i >= 0; i-- {
		floating := tab.Floating[i]
		if floating == nil {
			continue
		}

		rect := floating.Rect
		if x >= rect.X && x < rect.X+rect.W && y >= rect.Y && y < rect.Y+rect.H {
			isResize := x >= rect.X+rect.W-2 && y >= rect.Y+rect.H-2
			return floating.PaneID, rect, isResize
		}
	}

	return "", workbench.Rect{}, false
}

func (m *Model) mouseHitsOwnerButton(pane workbench.VisiblePane, x, contentY int) bool {
	if m == nil || m.runtime == nil {
		return false
	}
	rect, ok := render.PaneOwnerButtonRect(pane, m.runtime.Visible(), m.ownerConfirmPaneID)
	if !ok {
		return false
	}
	return x >= rect.X && x < rect.X+rect.W && contentY >= rect.Y && contentY < rect.Y+rect.H
}

func (m *Model) becomeOwnerCmd(paneID string) tea.Cmd {
	if strings.TrimSpace(paneID) == "" {
		return nil
	}
	return func() tea.Msg {
		return input.SemanticAction{Kind: input.ActionBecomeOwner, PaneID: paneID}
	}
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
	return clearOwnerConfirmCmd(m.ownerSeq)
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
		if floating != nil && floating.PaneID == tab.ActivePaneID {
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
		if paneID == "" || floating.Z >= maxZ {
			paneID = floating.PaneID
			maxZ = floating.Z
		}
	}
	return paneID
}
