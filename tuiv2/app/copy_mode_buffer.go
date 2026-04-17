package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
)

type copyModeBuffer struct {
	snapshot *protocol.Snapshot
	height   int
}

func (m *Model) activeCopyModeBuffer() (copyModeBuffer, bool) {
	if m == nil || m.workbench == nil || m.runtime == nil {
		return copyModeBuffer{}, false
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.TerminalID == "" {
		return copyModeBuffer{}, false
	}
	contentRect, ok := m.activePaneContentRect()
	if !ok {
		return copyModeBuffer{}, false
	}
	if m.copyMode.PaneID == pane.ID && m.copyMode.Snapshot != nil {
		return copyModeBuffer{
			snapshot: m.copyMode.Snapshot,
			height:   maxInt(1, contentRect.H),
		}, true
	}
	return m.activeLiveCopyModeBuffer()
}

func (m *Model) activeLiveCopyModeBuffer() (copyModeBuffer, bool) {
	if m == nil || m.workbench == nil || m.runtime == nil {
		return copyModeBuffer{}, false
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.TerminalID == "" {
		return copyModeBuffer{}, false
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil {
		return copyModeBuffer{}, false
	}
	if terminal.VTerm != nil && terminal.SnapshotVersion != terminal.SurfaceVersion {
		m.runtime.RefreshSnapshotFromVTerm(pane.TerminalID)
		terminal = m.runtime.Registry().Get(pane.TerminalID)
	}
	if terminal == nil || terminal.Snapshot == nil {
		return copyModeBuffer{}, false
	}
	contentRect, ok := m.activePaneContentRect()
	if !ok {
		return copyModeBuffer{}, false
	}
	return copyModeBuffer{
		snapshot: terminal.Snapshot,
		height:   maxInt(1, contentRect.H),
	}, true
}

func (b copyModeBuffer) totalRows() int {
	if b.snapshot == nil {
		return 0
	}
	return len(b.snapshot.Scrollback) + len(b.snapshot.Screen.Cells)
}

func (b copyModeBuffer) row(row int) []protocol.Cell {
	if b.snapshot == nil || row < 0 {
		return nil
	}
	if row < len(b.snapshot.Scrollback) {
		return b.snapshot.Scrollback[row]
	}
	row -= len(b.snapshot.Scrollback)
	if row < 0 || row >= len(b.snapshot.Screen.Cells) {
		return nil
	}
	return b.snapshot.Screen.Cells[row]
}

func (b copyModeBuffer) cursorRow() int {
	if b.snapshot == nil {
		return 0
	}
	return b.snapshot.Cursor.Row
}

func (b copyModeBuffer) cursorCol() int {
	if b.snapshot == nil {
		return 0
	}
	return b.snapshot.Cursor.Col
}

func (b copyModeBuffer) rowMaxCol(row int) int {
	cells := b.row(row)
	if len(cells) > 0 {
		return len(cells) - 1
	}
	if b.snapshot == nil || b.snapshot.Size.Cols == 0 {
		return 0
	}
	return int(b.snapshot.Size.Cols) - 1
}

func (b copyModeBuffer) normalizeCol(row, col int) int {
	maxCol := b.rowMaxCol(row)
	if maxCol < 0 {
		return 0
	}
	if col < 0 {
		col = 0
	}
	if col > maxCol {
		col = maxCol
	}
	cells := b.row(row)
	for col > 0 && col < len(cells) && cells[col].Content == "" && cells[col].Width == 0 {
		col--
	}
	return col
}

func (b copyModeBuffer) clampPoint(point copyModePoint) copyModePoint {
	totalRows := b.totalRows()
	if totalRows <= 0 {
		return copyModePoint{}
	}
	if point.Row < 0 {
		point.Row = 0
	}
	if point.Row >= totalRows {
		point.Row = totalRows - 1
	}
	point.Col = b.normalizeCol(point.Row, point.Col)
	return point
}

func (b copyModeBuffer) viewportStart(offset int) int {
	totalRows := b.totalRows()
	if totalRows <= 0 {
		return 0
	}
	if offset <= 0 {
		start := len(b.snapshot.Scrollback)
		if start < 0 {
			start = 0
		}
		if start >= totalRows {
			start = maxInt(0, totalRows-1)
		}
		return start
	}
	end := totalRows - offset
	if end < 0 {
		end = 0
	}
	start := end - maxInt(1, b.height)
	if start < 0 {
		start = 0
	}
	return start
}

func (b copyModeBuffer) viewportEnd(offset int) int {
	totalRows := b.totalRows()
	if totalRows <= 0 {
		return 0
	}
	if offset <= 0 {
		end := len(b.snapshot.Scrollback) + maxInt(1, b.height)
		if end > totalRows {
			end = totalRows
		}
		return end
	}
	end := totalRows - offset
	if end < 0 {
		return 0
	}
	if end > totalRows {
		return totalRows
	}
	return end
}

func (b copyModeBuffer) maxViewTopRow() int {
	return maxInt(0, b.totalRows()-maxInt(1, b.height))
}

func (m *Model) copyModeRenderOffset(buffer copyModeBuffer) int {
	if m == nil {
		return 0
	}
	totalRows := buffer.totalRows()
	offset := totalRows - (m.copyMode.ViewTopRow + maxInt(1, buffer.height))
	if offset < 0 {
		offset = 0
	}
	if m.copyMode.ViewTopRow < len(buffer.snapshot.Scrollback) && offset == 0 && len(buffer.snapshot.Scrollback) > 0 {
		offset = 1
	}
	return offset
}

func (m *Model) syncCopyModeViewport(buffer copyModeBuffer, point copyModePoint) {
	if m == nil {
		return
	}
	point = buffer.clampPoint(point)
	maxTop := buffer.maxViewTopRow()
	if m.copyMode.ViewTopRow < 0 {
		m.copyMode.ViewTopRow = 0
	}
	if m.copyMode.ViewTopRow > maxTop {
		m.copyMode.ViewTopRow = maxTop
	}
	if point.Row < m.copyMode.ViewTopRow {
		m.copyMode.ViewTopRow = point.Row
	}
	if point.Row >= m.copyMode.ViewTopRow+maxInt(1, buffer.height) {
		m.copyMode.ViewTopRow = point.Row - maxInt(1, buffer.height) + 1
	}
	if m.copyMode.ViewTopRow < 0 {
		m.copyMode.ViewTopRow = 0
	}
	if m.copyMode.ViewTopRow > maxTop {
		m.copyMode.ViewTopRow = maxTop
	}
	if m.copyMode.PaneID != "" {
		_ = m.setPaneViewportOffset(m.copyMode.PaneID, m.copyModeRenderOffset(buffer))
	}
}

func (m *Model) moveCopyCursor(deltaRow, deltaCol int) tea.Cmd {
	if !m.ensureCopyMode() {
		return nil
	}
	buffer, ok := m.activeCopyModeBuffer()
	if !ok || buffer.totalRows() == 0 {
		return nil
	}
	next := m.copyMode.Cursor
	next.Row += deltaRow
	next = buffer.clampPoint(next)
	next.Col += deltaCol
	next.Col = buffer.normalizeCol(next.Row, next.Col)
	m.copyMode.Cursor = next
	m.syncCopyModeViewport(buffer, next)
	m.render.Invalidate()
	return m.ensureActivePaneScrollbackCmd()
}

func (m *Model) moveCopyCursorVertical(delta int) tea.Cmd {
	if !m.ensureCopyMode() {
		return nil
	}
	buffer, ok := m.activeCopyModeBuffer()
	if !ok || buffer.totalRows() == 0 {
		return nil
	}
	next := m.copyMode.Cursor
	next.Row += delta
	next = buffer.clampPoint(next)
	next.Col = buffer.normalizeCol(next.Row, next.Col)
	m.copyMode.Cursor = next
	m.syncCopyModeViewport(buffer, next)
	m.render.Invalidate()
	return m.ensureActivePaneScrollbackCmd()
}

func (m *Model) jumpCopyCursor(row int) tea.Cmd {
	if !m.ensureCopyMode() {
		return nil
	}
	buffer, ok := m.activeCopyModeBuffer()
	if !ok || buffer.totalRows() == 0 {
		return nil
	}
	next := buffer.clampPoint(copyModePoint{Row: row, Col: m.copyMode.Cursor.Col})
	m.copyMode.Cursor = next
	m.syncCopyModeViewport(buffer, next)
	m.render.Invalidate()
	return m.ensureActivePaneScrollbackCmd()
}

func (m *Model) setCopyCursorCol(col int) {
	if !m.ensureCopyMode() {
		return
	}
	buffer, ok := m.activeCopyModeBuffer()
	if !ok {
		return
	}
	m.copyMode.Cursor.Col = buffer.normalizeCol(m.copyMode.Cursor.Row, col)
	m.render.Invalidate()
}
