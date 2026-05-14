package app

import (
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/termx-core/protocol"
	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type paneContentOffsetBounds struct {
	minX int
	maxX int
	minY int
	maxY int
}

func (m *Model) paneContentOffsetBinding(paneID string) (int, int, bool) {
	if m == nil || m.runtime == nil || paneID == "" {
		return 0, 0, false
	}
	binding := m.runtime.Binding(paneID)
	if binding == nil {
		return 0, 0, false
	}
	return binding.ContentOffset.X, binding.ContentOffset.Y, true
}

func (m *Model) paneContentOffsetBounds(paneID string) (paneContentOffsetBounds, bool) {
	if m == nil || m.runtime == nil || paneID == "" {
		return paneContentOffsetBounds{}, false
	}
	pane, ok := m.visiblePaneProjection(paneID)
	if !ok {
		return paneContentOffsetBounds{}, false
	}
	contentRect, ok := paneContentRectForVisible(pane)
	if !ok || contentRect.W <= 0 || contentRect.H <= 0 {
		return paneContentOffsetBounds{}, false
	}
	contentCols, contentRows := m.terminalVisibleContentSize(pane)
	if contentCols <= 0 {
		contentCols = contentRect.W
	}
	if contentRows <= 0 {
		contentRows = contentRect.H
	}
	minX, maxX := paneContentOffsetRange(contentRect.W, contentCols)
	minY, maxY := paneContentOffsetRange(contentRect.H, contentRows)
	return paneContentOffsetBounds{
		minX: minX,
		maxX: maxX,
		minY: minY,
		maxY: maxY,
	}, true
}

func paneContentOffsetRange(viewportSize, contentSize int) (int, int) {
	if viewportSize <= 0 || contentSize <= 0 {
		return 0, 0
	}
	delta := viewportSize - contentSize
	return minInt(0, delta), maxInt(0, delta)
}

func clampPaneContentOffset(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func centeredPaneContentOffset(minValue, maxValue int) int {
	return minValue + (maxValue-minValue)/2
}

func (m *Model) setPaneContentOffsetClamped(paneID string, x, y int) bool {
	if m == nil || m.runtime == nil || paneID == "" {
		return false
	}
	bounds, ok := m.paneContentOffsetBounds(paneID)
	if !ok {
		return false
	}
	x = clampPaneContentOffset(x, bounds.minX, bounds.maxX)
	y = clampPaneContentOffset(y, bounds.minY, bounds.maxY)
	return m.runtime.SetPaneContentOffset(paneID, x, y)
}

func (m *Model) adjustPaneContentOffsetClamped(paneID string, dx, dy int) bool {
	if m == nil || m.runtime == nil || paneID == "" {
		return false
	}
	currentX, currentY := m.runtime.PaneContentOffset(paneID)
	return m.setPaneContentOffsetClamped(paneID, currentX+dx, currentY+dy)
}

func (m *Model) alignPaneContentOffset(paneID string, targetX, targetY func(paneContentOffsetBounds, int) int) bool {
	if m == nil || m.runtime == nil || paneID == "" {
		return false
	}
	bounds, ok := m.paneContentOffsetBounds(paneID)
	if !ok {
		return false
	}
	currentX, currentY := m.runtime.PaneContentOffset(paneID)
	nextX := currentX
	nextY := currentY
	if targetX != nil {
		nextX = targetX(bounds, currentX)
	}
	if targetY != nil {
		nextY = targetY(bounds, currentY)
	}
	return m.setPaneContentOffsetClamped(paneID, nextX, nextY)
}

func (m *Model) resetPaneContentOffset(paneID string) {
	if m == nil || m.runtime == nil || paneID == "" {
		return
	}
	if m.runtime.ResetPaneContentOffset(paneID) {
		m.render.Invalidate()
	}
}

func (m *Model) terminalVisibleContentSize(pane workbench.VisiblePane) (int, int) {
	if m == nil || m.runtime == nil || pane.TerminalID == "" {
		return 0, 0
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil {
		return 0, 0
	}
	if terminal.VTerm != nil && terminal.SurfaceVersion != 0 && !terminal.PreferSnapshot {
		return visibleContentSizeForVTerm(terminal.VTerm)
	}
	return visibleContentSizeForSnapshot(terminal.Snapshot)
}

func visibleContentSizeForSnapshot(snapshot *protocol.Snapshot) (int, int) {
	if snapshot == nil {
		return 0, 0
	}
	cols := int(snapshot.Size.Cols)
	rows := int(snapshot.Size.Rows)
	renderedRows := len(snapshot.Screen.Cells)
	if renderedRows > 0 && (rows <= 0 || renderedRows < rows) {
		rows = renderedRows
	}
	if snapshot.Screen.IsAlternateScreen || snapshot.Modes.AlternateScreen {
		return cols, rows
	}
	renderedCols := 0
	for _, row := range snapshot.Screen.Cells {
		renderedCols = maxInt(renderedCols, protocolCellRowDisplayWidth(row))
	}
	if renderedCols > 0 && (cols <= 0 || renderedCols < cols) {
		cols = renderedCols
	}
	return cols, rows
}

func visibleContentSizeForVTerm(vt interface {
	Size() (int, int)
	ScreenContent() localvterm.ScreenData
	Modes() localvterm.TerminalModes
}) (int, int) {
	if vt == nil {
		return 0, 0
	}
	cols, rows := vt.Size()
	screen := vt.ScreenContent()
	renderedRows := len(screen.Cells)
	if renderedRows > 0 && (rows <= 0 || renderedRows < rows) {
		rows = renderedRows
	}
	modes := vt.Modes()
	if screen.IsAlternateScreen || modes.AlternateScreen {
		return cols, rows
	}
	renderedCols := 0
	for _, row := range screen.Cells {
		renderedCols = maxInt(renderedCols, vtermCellRowDisplayWidth(row))
	}
	if renderedCols > 0 && (cols <= 0 || renderedCols < cols) {
		cols = renderedCols
	}
	return cols, rows
}

func protocolCellRowDisplayWidth(row []protocol.Cell) int {
	width := 0
	for _, cell := range row {
		switch {
		case cell.Content == "" && cell.Width == 0:
			continue
		case cell.Width > 0:
			width += cell.Width
		case cell.Content != "":
			width += xansi.StringWidth(cell.Content)
		default:
			width++
		}
	}
	return width
}

func vtermCellRowDisplayWidth(row []localvterm.Cell) int {
	width := 0
	for _, cell := range row {
		switch {
		case cell.Content == "" && cell.Width == 0:
			continue
		case cell.Width > 0:
			width += cell.Width
		case cell.Content != "":
			width += xansi.StringWidth(cell.Content)
		default:
			width++
		}
	}
	return width
}
