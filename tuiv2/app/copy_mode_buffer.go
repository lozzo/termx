package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-shared/gridtrace"
	"github.com/lozzow/termx/tuiv2/historyview"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type copyModeBuffer struct {
	window *historyview.HistoryWindow
	height int
}

type copyModeLogicalLine struct {
	StartRow      int
	EndRow        int
	LogicalLineID uint64
	ClippedBefore bool
	ClippedAfter  bool
	Text          string
	Points        []copyModePoint
}

func (m *Model) activeCopyModeBuffer() (copyModeBuffer, bool) {
	_ = m.loadActiveCopyModeState()
	return m.copyModeBufferForPane("")
}

func (m *Model) copyModeBufferForPane(paneID string) (copyModeBuffer, bool) {
	if m == nil || m.workbench == nil {
		return copyModeBuffer{}, false
	}
	pane, contentRect, ok := m.copyModePaneAndContentRect(paneID)
	if !ok {
		return copyModeBuffer{}, false
	}
	if pane == nil || pane.TerminalID == "" {
		return copyModeBuffer{}, false
	}
	if state, ok := m.copyModeStateForPane(pane.ID); ok {
		if buffer, ok := m.authoritativeCopyModeBufferForPane(pane.ID, maxInt(1, contentRect.H)); ok {
			return buffer, true
		}
		_ = state
	}
	return copyModeBuffer{}, false
}

func (m *Model) copyModeBufferForState(state copyModeState, fallbackHeight int) (copyModeBuffer, bool) {
	if m == nil || state.PaneID == "" || state.TerminalID == "" {
		return copyModeBuffer{}, false
	}
	height := maxInt(1, fallbackHeight)
	if _, rect, ok := m.copyModePaneAndContentRect(state.PaneID); ok {
		height = maxInt(1, rect.H)
	}
	return m.authoritativeCopyModeBufferForState(state, height)
}

func (m *Model) authoritativeCopyModeBufferForPane(paneID string, height int) (copyModeBuffer, bool) {
	if m == nil || m.historyStore == nil || strings.TrimSpace(paneID) == "" {
		return copyModeBuffer{}, false
	}
	state, ok := m.copyModeStateForPane(paneID)
	if !ok || state.TerminalID == "" {
		return copyModeBuffer{}, false
	}
	return m.authoritativeCopyModeBufferForState(state, height)
}

func (m *Model) authoritativeCopyModeBufferForState(state copyModeState, height int) (copyModeBuffer, bool) {
	if m == nil || m.historyStore == nil || state.TerminalID == "" {
		return copyModeBuffer{}, false
	}
	window, ok := m.historyStore.HistoryWindow(state.TerminalID)
	if !ok || len(window.Rows) == 0 || len(window.Lines) == 0 {
		return copyModeBuffer{}, false
	}
	if state.WindowToken != "" && string(window.Token) != state.WindowToken {
		return copyModeBuffer{}, false
	}
	return copyModeBuffer{
		window: &window,
		height: maxInt(1, height),
	}, true
}

func (m *Model) copyModePaneAndContentRect(paneID string) (*workbench.PaneState, workbench.Rect, bool) {
	if m == nil || m.workbench == nil {
		return nil, workbench.Rect{}, false
	}
	if strings.TrimSpace(paneID) == "" {
		pane := m.workbench.ActivePane()
		if pane == nil {
			return nil, workbench.Rect{}, false
		}
		rect, ok := m.activePaneContentRect()
		return pane, rect, ok
	}
	tabID, err := m.workbench.ResolvePaneTab("", paneID)
	if err != nil || tabID == "" {
		return nil, workbench.Rect{}, false
	}
	tab := m.workbench.CurrentTab()
	if tab == nil || tab.ID != tabID {
		return nil, workbench.Rect{}, false
	}
	pane := tab.Panes[paneID]
	if pane == nil {
		return nil, workbench.Rect{}, false
	}
	visiblePane, ok := m.visiblePaneProjection(paneID)
	if !ok {
		return nil, workbench.Rect{}, false
	}
	rect, ok := paneContentRectForVisible(visiblePane)
	return pane, rect, ok
}

func (b copyModeBuffer) totalRows() int {
	if b.window != nil {
		return len(b.window.Rows)
	}
	return 0
}

func (b copyModeBuffer) row(row int) []protocol.Cell {
	if b.window == nil || row < 0 || row >= len(b.window.Rows) {
		return nil
	}
	return b.window.Rows[row].Cells.DecodeCells()
}

func (b copyModeBuffer) rowWrapped(row int) bool {
	if b.window == nil || row < 0 || row >= len(b.window.Rows) {
		return false
	}
	return b.window.Rows[row].Wrapped
}

func (b copyModeBuffer) logicalLineAtRow(row int) (copyModeLogicalLine, bool) {
	if b.totalRows() == 0 || row < 0 || row >= b.totalRows() {
		return copyModeLogicalLine{}, false
	}
	start, end, ok := b.logicalLineBoundsAtRow(row)
	if !ok {
		return copyModeLogicalLine{}, false
	}
	line := copyModeLogicalLine{
		StartRow: start,
		EndRow:   end,
	}
	if span, ok := b.logicalLineSpanAtRow(row); ok {
		line.LogicalLineID = span.LogicalLineID
		line.ClippedBefore = span.ClippedBefore
		line.ClippedAfter = span.ClippedAfter
	}
	for current := start; current <= end; current++ {
		cells := b.row(current)
		for col, cell := range cells {
			if cell.Content == "" && cell.Width == 0 {
				continue
			}
			if cell.Content != "" {
				line.Text += cell.Content
				line.Points = append(line.Points, copyModePoint{Row: current, Col: col})
				continue
			}
			line.Text += " "
			line.Points = append(line.Points, copyModePoint{Row: current, Col: col})
		}
	}
	return line, true
}

func (b copyModeBuffer) logicalLineIndexAtRow(row int) int {
	if b.totalRows() == 0 {
		return -1
	}
	for index, span := range b.window.Lines {
		if row >= span.StartRow && row <= span.EndRow {
			return index
		}
	}
	return -1
}

func (b copyModeBuffer) logicalLineCount() int {
	if b.totalRows() == 0 {
		return 0
	}
	if b.window != nil {
		return len(b.window.Lines)
	}
	return 0
}

func (b copyModeBuffer) logicalLineByIndex(index int) (copyModeLogicalLine, bool) {
	if index < 0 || b.totalRows() == 0 {
		return copyModeLogicalLine{}, false
	}
	if b.window == nil || index >= len(b.window.Lines) {
		return copyModeLogicalLine{}, false
	}
	span := b.window.Lines[index]
	row := clampCopyModeInt(span.StartRow, 0, maxInt(0, b.totalRows()-1))
	return b.logicalLineAtRow(row)
}

func (b copyModeBuffer) logicalPosForPoint(point copyModePoint) (copyModeLogicalPos, bool) {
	point = b.clampPoint(point)
	line, ok := b.logicalLineAtRow(point.Row)
	if !ok {
		return copyModeLogicalPos{}, false
	}
	lineIndex := b.logicalLineIndexAtRow(point.Row)
	if lineIndex < 0 {
		return copyModeLogicalPos{}, false
	}
	offset := maxInt(0, len(line.Text)-1)
	for i, cellPoint := range line.Points {
		if point.Row < cellPoint.Row || (point.Row == cellPoint.Row && point.Col <= cellPoint.Col) {
			offset = i
			break
		}
	}
	if len(line.Text) == 0 {
		offset = 0
	}
	return copyModeLogicalPos{Line: lineIndex, Offset: offset}, true
}

func (b copyModeBuffer) pointForLogicalPos(pos copyModeLogicalPos) (copyModePoint, bool) {
	line, ok := b.logicalLineByIndex(pos.Line)
	if !ok {
		return copyModePoint{}, false
	}
	if len(line.Points) == 0 {
		return copyModePoint{Row: line.StartRow, Col: 0}, true
	}
	offset := pos.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(line.Points) {
		offset = len(line.Points) - 1
	}
	return line.Points[offset], true
}

func (b copyModeBuffer) cursorRow() int {
	return maxInt(0, b.totalRows()-1)
}

func (b copyModeBuffer) cursorCol() int {
	return 0
}

func (b copyModeBuffer) rowMaxCol(row int) int {
	cells := b.row(row)
	if len(cells) > 0 {
		return len(cells) - 1
	}
	if b.window == nil || b.window.Size.Cols == 0 {
		return 0
	}
	return int(b.window.Size.Cols) - 1
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
		return 0
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
		return minInt(totalRows, maxInt(1, b.height))
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

func (b copyModeBuffer) windowToken() string {
	if b.window == nil {
		return ""
	}
	return string(b.window.Token)
}

func clampCopyModeInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func (b copyModeBuffer) logicalLineBoundsAtRow(row int) (int, int, bool) {
	if b.totalRows() == 0 || row < 0 || row >= b.totalRows() {
		return 0, 0, false
	}
	if span, ok := b.logicalLineSpanAtRow(row); ok {
		start := clampCopyModeInt(span.StartRow, 0, b.totalRows()-1)
		end := clampCopyModeInt(span.EndRow, start, b.totalRows()-1)
		return start, end, true
	}
	return 0, 0, false
}

func (b copyModeBuffer) logicalLineSpanAtRow(row int) (historyview.LineSpan, bool) {
	if b.window == nil || len(b.window.Lines) == 0 || b.totalRows() == 0 || row < 0 || row >= b.totalRows() {
		return historyview.LineSpan{}, false
	}
	for _, span := range b.window.Lines {
		if row >= span.StartRow && row <= span.EndRow {
			return span, true
		}
	}
	return historyview.LineSpan{}, false
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
	if gridtrace.Enabled() {
		gridtrace.Log(
			"app.copy_mode.sync_viewport",
			"pane_id", m.copyMode.PaneID,
			"point_row", point.Row,
			"point_col", point.Col,
			"view_top_row", m.copyMode.ViewTopRow,
			"max_top", maxTop,
			"history_window_rows", buffer.totalRows(),
			"history_window_lines", buffer.logicalLineCount(),
			"render_offset", m.copyModeRenderOffset(buffer),
		)
	}
	m.saveCurrentCopyModeState()
}

func (m *Model) setCopyCursorLogicalPos(pos copyModeLogicalPos) tea.Cmd {
	if !m.ensureCopyMode() {
		return nil
	}
	buffer, ok := m.activeCopyModeBuffer()
	if !ok || buffer.totalRows() == 0 {
		return nil
	}
	if pos.Line < 0 {
		pos.Line = 0
	}
	maxLine := buffer.logicalLineCount() - 1
	if maxLine < 0 {
		maxLine = 0
	}
	if pos.Line > maxLine {
		pos.Line = maxLine
	}
	line, ok := buffer.logicalLineByIndex(pos.Line)
	if !ok {
		return nil
	}
	maxOffset := len(line.Text) - 1
	if maxOffset < 0 {
		maxOffset = 0
	}
	if pos.Offset < 0 {
		pos.Offset = 0
	}
	if pos.Offset > maxOffset {
		pos.Offset = maxOffset
	}
	next, ok := buffer.pointForLogicalPos(pos)
	if !ok {
		return nil
	}
	m.copyMode.CursorLogical = pos
	m.copyMode.Cursor = next
	m.syncCopyModeViewport(buffer, next)
	m.saveCurrentCopyModeState()
	m.render.Invalidate()
	return nil
}

func (m *Model) moveCopyCursorLogicalOffset(delta int) tea.Cmd {
	target := m.copyMode.CursorLogical
	target.Offset += delta
	return m.setCopyCursorLogicalPos(target)
}

func (m *Model) setCopyCursorLogicalOffset(offset int) tea.Cmd {
	target := m.copyMode.CursorLogical
	target.Offset = offset
	return m.setCopyCursorLogicalPos(target)
}

func (m *Model) moveCopyCursorVertical(delta int) tea.Cmd {
	return m.moveCopyCursorLogicalLines(delta)
}

func (m *Model) moveCopyCursorLogicalLines(delta int) tea.Cmd {
	if !m.ensureCopyMode() {
		return nil
	}
	buffer, ok := m.activeCopyModeBuffer()
	if !ok || buffer.totalRows() == 0 {
		return nil
	}
	before := m.copyMode.CursorLogical
	beforePoint := m.copyMode.Cursor
	target := m.copyMode.CursorLogical
	target.Line += delta
	if target.Line < 0 {
		target.Line = 0
	}
	maxLine := buffer.logicalLineCount() - 1
	if maxLine < 0 {
		maxLine = 0
	}
	if target.Line > maxLine {
		target.Line = maxLine
	}
	point, ok := buffer.pointForLogicalPos(target)
	if !ok {
		return nil
	}
	m.copyMode.CursorLogical = target
	m.copyMode.Cursor = buffer.clampPoint(point)
	m.syncCopyModeViewport(buffer, m.copyMode.Cursor)
	if gridtrace.Enabled() {
		gridtrace.Log(
			"app.copy_mode.move_logical_lines",
			"pane_id", m.copyMode.PaneID,
			"delta", delta,
			"before_line", before.Line,
			"before_offset", before.Offset,
			"before_row", beforePoint.Row,
			"before_col", beforePoint.Col,
			"after_line", m.copyMode.CursorLogical.Line,
			"after_offset", m.copyMode.CursorLogical.Offset,
			"after_row", m.copyMode.Cursor.Row,
			"after_col", m.copyMode.Cursor.Col,
			"view_top_row", m.copyMode.ViewTopRow,
			"max_line", maxLine,
			"history_window_rows", buffer.totalRows(),
			"history_window_lines", buffer.logicalLineCount(),
			"render_offset", m.copyModeRenderOffset(buffer),
		)
	}
	m.saveCurrentCopyModeState()
	m.render.Invalidate()
	return nil
}

func (m *Model) moveCopyCursorLogicalLinesAndMaybeLoadOlder(delta int) tea.Cmd {
	if !m.ensureCopyMode() {
		return nil
	}
	wasPinnedAtTop := delta < 0 && copyModePinnedAtTop(m.copyMode)
	cmd := m.moveCopyCursorLogicalLines(delta)
	if wasPinnedAtTop {
		cmd = batchCmds(cmd, m.loadOlderHistoryWindowForPaneCmd(m.copyMode.PaneID))
	}
	return cmd
}

func (m *Model) jumpCopyCursorLogicalLine(line int) tea.Cmd {
	if !m.ensureCopyMode() {
		return nil
	}
	buffer, ok := m.activeCopyModeBuffer()
	if !ok || buffer.totalRows() == 0 {
		return nil
	}
	if line < 0 {
		line = 0
	}
	maxLine := buffer.logicalLineCount() - 1
	if maxLine < 0 {
		maxLine = 0
	}
	if line > maxLine {
		line = maxLine
	}
	target := copyModeLogicalPos{Line: line, Offset: m.copyMode.CursorLogical.Offset}
	point, ok := buffer.pointForLogicalPos(target)
	if !ok {
		return nil
	}
	m.copyMode.CursorLogical = target
	m.copyMode.Cursor = buffer.clampPoint(point)
	m.syncCopyModeViewport(buffer, m.copyMode.Cursor)
	m.saveCurrentCopyModeState()
	m.render.Invalidate()
	return nil
}
