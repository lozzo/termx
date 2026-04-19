package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) handleMouseDrag(x, y int) tea.Cmd {
	if m.workbench == nil || m.mouseDragMode == mouseDragNone {
		return nil
	}
	appendMouseDebugLog("mouse_drag_apply", "mode", m.mouseDragMode, "pane", m.mouseDragPaneID, "x", x, "y", y)

	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}

	contentY := y - m.contentOriginY()
	if contentY < 0 {
		contentY = 0
	}

	switch m.mouseDragMode {
	case mouseDragMove:
		if m.mouseDragPaneID == "" {
			return nil
		}
		newX := x - m.mouseDragOffsetX
		newY := contentY - m.mouseDragOffsetY
		service := m.layoutResizeService()
		if service == nil || !service.moveFloatingPane(tab.ID, m.mouseDragPaneID, newX, newY) {
			perftrace.Count("app.mouse.drag.move.noop", 0)
			return nil
		}
		perftrace.Count("app.mouse.drag.move.changed", 0)
		m.render.Invalidate()
	case mouseDragResize:
		if m.mouseDragPaneID == "" {
			return nil
		}
		for _, floating := range tab.Floating {
			if floating != nil && floating.PaneID == m.mouseDragPaneID {
				newW := x - floating.Rect.X + 1
				newH := contentY - floating.Rect.Y + 1
				service := m.layoutResizeService()
				if service == nil || !service.resizeFloatingPane(tab.ID, m.mouseDragPaneID, newW, newH) {
					perftrace.Count("app.mouse.drag.resize.noop", 0)
					return nil
				}
				m.mouseDragDirty = true
				perftrace.Count("app.mouse.drag.resize.changed", 0)
				m.render.Invalidate()
				return nil
			}
		}
	case mouseDragResizeSplit:
		if m.mouseDragSplit == nil {
			return nil
		}
		service := m.layoutResizeService()
		if service == nil || !service.resizeSplit(tab.ID, m.mouseDragSplit, m.mouseDragBounds, x, contentY, m.mouseDragOffsetX, m.mouseDragOffsetY) {
			return nil
		}
		m.mouseDragDirty = true
		m.render.Invalidate()
		return nil
	}

	return nil
}

func (m *Model) handleMouseRelease() tea.Cmd {
	appendMouseDebugLog("mouse_drag_release", "mode", m.mouseDragMode, "pane", m.mouseDragPaneID, "dirty", m.mouseDragDirty)
	cmd := tea.Cmd(nil)
	switch m.mouseDragMode {
	case mouseDragResize:
		if m.mouseDragDirty {
			cmd = batchCmds(m.resizePaneIfNeededCmd(m.mouseDragPaneID), m.saveStateCmd())
		}
	case mouseDragResizeSplit:
		if m.mouseDragDirty {
			cmd = batchCmds(m.resizeVisiblePanesCmd(), m.saveStateCmd())
		}
	}
	m.mouseDragPaneID = ""
	m.mouseDragOffsetX = 0
	m.mouseDragOffsetY = 0
	m.mouseDragMode = mouseDragNone
	m.mouseDragSplit = nil
	m.mouseDragBounds = workbench.Rect{}
	m.mouseDragDirty = false
	return cmd
}

func (m *Model) findFloatingPaneAt(tab *workbench.TabState, x, y int) (string, workbench.Rect, bool) {
	if m == nil || m.workbench == nil || tab == nil || len(tab.Floating) == 0 {
		return "", workbench.Rect{}, false
	}

	visible := m.workbench.VisibleWithSize(m.bodyRect())
	if visible == nil {
		return "", workbench.Rect{}, false
	}
	for i := len(visible.FloatingPanes) - 1; i >= 0; i-- {
		pane := visible.FloatingPanes[i]
		rect := pane.Rect
		if x >= rect.X && x < rect.X+rect.W && y >= rect.Y && y < rect.Y+rect.H {
			isResize := x >= rect.X+rect.W-2 && y >= rect.Y+rect.H-2
			return pane.ID, rect, isResize
		}
	}

	return "", workbench.Rect{}, false
}

func (m *Model) mouseHitsOwnerButton(pane workbench.VisiblePane, x, contentY int) bool {
	region, ok := m.mousePaneChromeRegion(pane, x, contentY)
	if !ok {
		return false
	}
	return region.Kind == render.HitRegionPaneOwner
}

func (m *Model) becomeOwnerCmd(paneID string) tea.Cmd {
	if strings.TrimSpace(paneID) == "" {
		return nil
	}
	return func() tea.Msg {
		return input.SemanticAction{Kind: input.ActionBecomeOwner, PaneID: paneID}
	}
}
