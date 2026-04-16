package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) copyModePointAtMouse(screenX, screenY int) (copyModePoint, bool) {
	if !m.ensureCopyMode() || screenY < m.contentOriginY() {
		return copyModePoint{}, false
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return copyModePoint{}, false
	}
	contentY := screenY - m.contentOriginY()
	tiled, floating, ok := m.visiblePaneAt(screenX, contentY)
	if !ok {
		return copyModePoint{}, false
	}
	var pane *workbench.VisiblePane
	if floating != nil {
		pane = floating
	} else {
		pane = tiled
	}
	if pane == nil || pane.ID != tab.ActivePaneID || pane.ID != m.copyMode.PaneID {
		return copyModePoint{}, false
	}
	contentRect, ok := paneContentRectForVisible(*pane)
	if !ok {
		return copyModePoint{}, false
	}
	buffer, ok := m.activeCopyModeBuffer()
	if !ok {
		return copyModePoint{}, false
	}
	col := screenX - contentRect.X
	row := contentY - contentRect.Y + m.copyMode.ViewTopRow
	return buffer.clampPoint(copyModePoint{Row: row, Col: col}), true
}

func (m *Model) noteCopyModeMouseActivity() uint64 {
	if m == nil {
		return 0
	}
	m.copyModeMouseActivitySeq++
	return m.copyModeMouseActivitySeq
}

func (m *Model) startMouseCopySelection(screenX, screenY int) bool {
	point, ok := m.copyModePointAtMouse(screenX, screenY)
	if !ok {
		return false
	}
	m.copyMode.Cursor = point
	m.copyMode.Mark = &copyModePoint{Row: point.Row, Col: point.Col}
	m.copyMode.MouseSelecting = true
	m.copyMode.AutoScrollDir = 0
	m.copyMode.AutoScrollSeq = m.noteCopyModeMouseActivity()
	m.render.Invalidate()
	return true
}

func (m *Model) updateMouseCopySelection(screenX, screenY int) tea.Cmd {
	if !m.ensureCopyMode() || !m.copyMode.MouseSelecting {
		return nil
	}
	buffer, ok := m.activeCopyModeBuffer()
	if !ok {
		return nil
	}
	rect, ok := m.activePaneContentRect()
	if !ok {
		return nil
	}
	dir := 0
	if screenY < m.contentOriginY()+rect.Y {
		dir = -1
		screenY = m.contentOriginY() + rect.Y
	} else if screenY >= m.contentOriginY()+rect.Y+rect.H {
		dir = 1
		screenY = m.contentOriginY() + rect.Y + rect.H - 1
	}
	point, pointOK := m.copyModePointAtMouse(screenX, screenY)
	if pointOK {
		m.copyMode.Cursor = point
		m.syncCopyModeViewport(buffer, point)
	}
	m.copyMode.AutoScrollDir = dir
	seq := m.noteCopyModeMouseActivity()
	m.copyMode.AutoScrollSeq = seq
	m.render.Invalidate()
	cmds := []tea.Cmd{m.ensureActivePaneScrollbackCmd()}
	if dir != 0 {
		cmds = append(cmds, copyModeAutoScrollTickCmd(seq))
	}
	return tea.Batch(cmds...)
}

func (m *Model) stopMouseCopySelection() {
	if m == nil {
		return
	}
	m.copyMode.MouseSelecting = false
	m.copyMode.AutoScrollDir = 0
	m.copyMode.AutoScrollSeq = m.noteCopyModeMouseActivity()
}

func (m *Model) handleCopyModeAutoScroll(seq uint64) tea.Cmd {
	if !m.ensureCopyMode() || !m.copyMode.MouseSelecting || m.copyMode.AutoScrollDir == 0 || seq != m.copyMode.AutoScrollSeq || seq != m.copyModeMouseActivitySeq {
		return nil
	}
	buffer, ok := m.activeCopyModeBuffer()
	if !ok || buffer.totalRows() == 0 {
		return nil
	}
	next := m.copyMode.Cursor
	next.Row += m.copyMode.AutoScrollDir
	next = buffer.clampPoint(next)
	if next == m.copyMode.Cursor && m.copyMode.AutoScrollDir != 0 {
		return nil
	}
	m.copyMode.Cursor = next
	m.syncCopyModeViewport(buffer, next)
	m.render.Invalidate()
	return tea.Batch(m.ensureActivePaneScrollbackCmd(), copyModeAutoScrollTickCmd(seq))
}
