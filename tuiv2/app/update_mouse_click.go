package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) visiblePaneAt(x, contentY int) (*workbench.VisiblePane, *workbench.VisiblePane, bool) {
	if m == nil || m.workbench == nil {
		return nil, nil, false
	}
	bodyRect := m.bodyRect()
	visible := m.workbench.VisibleWithSize(bodyRect)
	if visible == nil {
		return nil, nil, false
	}
	for i := len(visible.FloatingPanes) - 1; i >= 0; i-- {
		pane := &visible.FloatingPanes[i]
		if x >= pane.Rect.X && x < pane.Rect.X+pane.Rect.W && contentY >= pane.Rect.Y && contentY < pane.Rect.Y+pane.Rect.H {
			return nil, pane, true
		}
	}
	if visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		return nil, nil, false
	}
	for i := range visible.Tabs[visible.ActiveTab].Panes {
		pane := &visible.Tabs[visible.ActiveTab].Panes[i]
		if x >= pane.Rect.X && x < pane.Rect.X+pane.Rect.W && contentY >= pane.Rect.Y && contentY < pane.Rect.Y+pane.Rect.H {
			return pane, nil, true
		}
	}
	return nil, nil, false
}

func (m *Model) forwardTerminalMouseInputCmd(msg tea.MouseMsg) tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	if m.mode().Kind == input.ModeDisplay {
		return nil
	}
	vm := m.renderVM()
	if vm.Overlay.Kind != render.VisibleOverlayNone || vm.Surface.Kind == render.VisibleSurfaceTerminalPool {
		return nil
	}
	targetPaneID, contentRect, ok := m.activeContentMouseTarget(msg.X, msg.Y)
	if !ok {
		return nil
	}
	contentMsg := msg
	contentMsg.Y = msg.Y - m.contentOriginY()
	encoded := m.encodeTerminalMouseInput(contentMsg, targetPaneID, contentRect)
	if len(encoded) == 0 {
		return nil
	}
	return func() tea.Msg {
		return input.TerminalInput{PaneID: targetPaneID, Data: encoded}
	}
}

func (m *Model) activeContentMouseTarget(screenX, screenY int) (string, workbench.Rect, bool) {
	if m == nil || m.workbench == nil || screenY < m.contentOriginY() {
		return "", workbench.Rect{}, false
	}
	tab := m.workbench.CurrentTab()
	if tab == nil || strings.TrimSpace(tab.ActivePaneID) == "" {
		return "", workbench.Rect{}, false
	}
	contentY := screenY - m.contentOriginY()
	tiled, floating, ok := m.visiblePaneAt(screenX, contentY)
	if !ok {
		return "", workbench.Rect{}, false
	}
	var pane *workbench.VisiblePane
	if floating != nil {
		pane = floating
	} else {
		pane = tiled
	}
	if pane == nil || pane.ID != tab.ActivePaneID {
		return "", workbench.Rect{}, false
	}
	contentRect, ok := paneContentRectForVisible(*pane)
	if !ok || !pointInMouseRect(contentRect, screenX, contentY) {
		return "", workbench.Rect{}, false
	}
	return pane.ID, contentRect, true
}

func paneContentRect(rect workbench.Rect) (workbench.Rect, bool) {
	return workbench.FramedPaneContentRect(rect, false, false)
}

func paneContentRectForVisible(pane workbench.VisiblePane) (workbench.Rect, bool) {
	if pane.Frameless {
		if pane.Rect.W <= 0 || pane.Rect.H <= 0 {
			return workbench.Rect{}, false
		}
		return pane.Rect, true
	}
	return workbench.FramedPaneContentRect(pane.Rect, pane.SharedLeft, pane.SharedTop)
}

func pointInMouseRect(rect workbench.Rect, x, y int) bool {
	return rect.W > 0 && rect.H > 0 &&
		x >= rect.X && x < rect.X+rect.W &&
		y >= rect.Y && y < rect.Y+rect.H
}

func (m *Model) dispatchOverlayRegionAction(action input.SemanticAction) tea.Cmd {
	if m == nil || action.Kind == "" {
		return nil
	}
	action = m.resolveMouseActionScope(action)
	if handled, cmd := m.handleModalAction(action); handled {
		return cmd
	}
	return m.applyMouseSemanticAction(action)
}

func (m *Model) handlePromptInputMouseClick(region render.HitRegion, screenX int) tea.Cmd {
	if m == nil || m.modalHost == nil || m.modalHost.Prompt == nil {
		return nil
	}
	if m.modalHost.Prompt.IsForm() && region.ItemIndex >= 0 && region.ItemIndex < len(m.modalHost.Prompt.Fields) {
		m.modalHost.Prompt.ActiveField = region.ItemIndex
	}
	cursor := screenX - region.Rect.X
	if cursor < 0 {
		cursor = 0
	}
	if setPromptCursor(m.modalHost.Prompt, cursor) {
		m.revealCursorAndInvalidate()
	}
	return nil
}

func (m *Model) resolveMouseActionScope(action input.SemanticAction) input.SemanticAction {
	if m == nil {
		return action
	}
	if action.Kind == input.ActionSubmitPrompt && m.modalHost != nil && m.modalHost.Session != nil &&
		m.modalHost.Session.Kind == input.ModePicker && m.modalHost.Picker != nil {
		if selected := m.modalHost.Picker.SelectedItem(); selected != nil && action.TargetID == "" {
			action.TargetID = selected.TerminalID
		}
	}
	if action.PaneID == "" && m.workbench != nil {
		if pane := m.workbench.ActivePane(); pane != nil {
			action.PaneID = pane.ID
		}
	}
	return action
}
