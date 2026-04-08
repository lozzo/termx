package app

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type copyModePoint struct {
	Row int
	Col int
}

type copyModeState struct {
	PaneID         string
	Snapshot       *protocol.Snapshot
	LoadedRows     int
	ViewTopRow     int
	Cursor         copyModePoint
	Mark           *copyModePoint
	MouseSelecting bool
	AutoScrollDir  int
	AutoScrollSeq  uint64
}

type copyModeResumeState struct {
	PaneID     string
	TerminalID string
	Snapshot   *protocol.Snapshot
	Baseline   *protocol.Snapshot
}

type copyModeBuffer struct {
	snapshot *protocol.Snapshot
	height   int
}

func clearNoticeCmd(seq uint64) tea.Cmd {
	return tea.Tick(noticeClearDelay, func(time.Time) tea.Msg {
		return clearNoticeMsg{seq: seq}
	})
}

func copyModeAutoScrollTickCmd(seq uint64) tea.Cmd {
	return tea.Tick(copyModeAutoScrollDelay, func(time.Time) tea.Msg {
		return copyModeAutoScrollMsg{seq: seq}
	})
}

func cloneProtocolRows(rows [][]protocol.Cell) [][]protocol.Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]protocol.Cell, len(rows))
	for i, row := range rows {
		out[i] = append([]protocol.Cell(nil), row...)
	}
	return out
}

func cloneSnapshot(snapshot *protocol.Snapshot) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.Screen = protocol.ScreenData{
		Cells:             cloneProtocolRows(snapshot.Screen.Cells),
		IsAlternateScreen: snapshot.Screen.IsAlternateScreen,
	}
	cloned.Scrollback = cloneProtocolRows(snapshot.Scrollback)
	cloned.ScreenTimestamps = append([]time.Time(nil), snapshot.ScreenTimestamps...)
	cloned.ScrollbackTimestamps = append([]time.Time(nil), snapshot.ScrollbackTimestamps...)
	cloned.ScreenRowKinds = append([]string(nil), snapshot.ScreenRowKinds...)
	cloned.ScrollbackRowKinds = append([]string(nil), snapshot.ScrollbackRowKinds...)
	return &cloned
}

func (m *Model) showNotice(text string) tea.Cmd {
	if m == nil {
		return nil
	}
	m.noticeSeq++
	m.notice = strings.TrimSpace(text)
	m.render.Invalidate()
	if m.notice == "" {
		return nil
	}
	return clearNoticeCmd(m.noticeSeq)
}

func (m *Model) resetCopyMode() {
	if m == nil {
		return
	}
	m.copyMode = copyModeState{}
}

func (m *Model) clearCopySelection() {
	if m == nil {
		return
	}
	m.copyMode.Mark = nil
	m.stopMouseCopySelection()
}

func (m *Model) reconcileCopyModeContext() {
	if m == nil || m.workbench == nil || m.copyMode.PaneID == "" {
		return
	}
	pane := m.workbench.ActivePane()
	if pane != nil && pane.ID == m.copyMode.PaneID {
		return
	}
	if tab := m.workbench.CurrentTab(); tab != nil {
		tab.ScrollOffset = 0
	}
	m.clearCopySelection()
	m.resetCopyMode()
	m.copyModeResume = copyModeResumeState{}
	if m.mode().Kind == input.ModeDisplay {
		m.setMode(input.ModeState{Kind: input.ModeNormal})
	}
	m.render.Invalidate()
}

func (m *Model) prepareCopyModeExit() {
	if m == nil || m.copyMode.PaneID == "" || m.copyMode.Snapshot == nil || m.workbench == nil || m.runtime == nil {
		m.copyModeResume = copyModeResumeState{}
		return
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID != m.copyMode.PaneID || pane.TerminalID == "" {
		m.copyModeResume = copyModeResumeState{}
		return
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil {
		m.copyModeResume = copyModeResumeState{}
		return
	}
	if terminal.VTerm != nil {
		m.runtime.RefreshSnapshotFromVTerm(pane.TerminalID)
		terminal = m.runtime.Registry().Get(pane.TerminalID)
	}
	if terminal == nil || !terminal.Stream.Active || terminal.Snapshot == nil {
		m.copyModeResume = copyModeResumeState{}
		return
	}
	m.copyModeResume = copyModeResumeState{
		PaneID:     pane.ID,
		TerminalID: pane.TerminalID,
		Snapshot:   cloneSnapshot(m.copyMode.Snapshot),
		Baseline:   terminal.Snapshot,
	}
}

func (m *Model) activeCopyModeResumeSnapshot() (string, *protocol.Snapshot, bool) {
	if m == nil || m.copyModeResume.Snapshot == nil || m.workbench == nil || m.runtime == nil {
		return "", nil, false
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID != m.copyModeResume.PaneID || pane.TerminalID != m.copyModeResume.TerminalID {
		return "", nil, false
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil || !terminal.Stream.Active || terminal.Snapshot == nil {
		return "", nil, false
	}
	if terminal.Snapshot != m.copyModeResume.Baseline {
		return "", nil, false
	}
	return pane.ID, m.copyModeResume.Snapshot, true
}

func (m *Model) leaveCopyMode() {
	if m == nil {
		return
	}
	if m.workbench != nil {
		if tab := m.workbench.CurrentTab(); tab != nil {
			tab.ScrollOffset = 0
		}
	}
	m.resetCopyMode()
	m.render.Invalidate()
}

func (m *Model) ensureCopyMode() bool {
	if m == nil || m.workbench == nil || m.runtime == nil {
		return false
	}
	pane := m.workbench.ActivePane()
	tab := m.workbench.CurrentTab()
	if pane == nil || tab == nil || pane.ID == "" || pane.TerminalID == "" {
		return false
	}
	if m.copyMode.PaneID != "" && m.copyMode.PaneID != pane.ID {
		return false
	}
	if m.copyMode.PaneID == pane.ID {
		buffer, ok := m.activeCopyModeBuffer()
		if !ok || buffer.totalRows() == 0 {
			return false
		}
		if delta := len(buffer.snapshot.Scrollback) - m.copyMode.LoadedRows; delta > 0 {
			m.copyMode.ViewTopRow += delta
			m.copyMode.Cursor.Row += delta
			if m.copyMode.Mark != nil {
				point := *m.copyMode.Mark
				point.Row += delta
				m.copyMode.Mark = &point
			}
		}
		m.copyMode.LoadedRows = len(buffer.snapshot.Scrollback)
		m.copyMode.Cursor = buffer.clampPoint(m.copyMode.Cursor)
		if m.copyMode.Mark != nil {
			point := buffer.clampPoint(*m.copyMode.Mark)
			m.copyMode.Mark = &point
		}
		m.syncCopyModeViewport(buffer, m.copyMode.Cursor)
		return true
	}
	liveBuffer, ok := m.activeLiveCopyModeBuffer()
	if !ok || liveBuffer.totalRows() == 0 {
		return false
	}
	frozenSnapshot := cloneSnapshot(liveBuffer.snapshot)
	if frozenSnapshot == nil {
		return false
	}
	buffer := copyModeBuffer{
		snapshot: frozenSnapshot,
		height:   liveBuffer.height,
	}
	start := copyModePoint{Row: maxInt(0, len(buffer.snapshot.Scrollback)+buffer.cursorRow()), Col: maxInt(0, buffer.cursorCol())}
	start = buffer.clampPoint(start)
	m.copyMode = copyModeState{
		PaneID:     pane.ID,
		Snapshot:   frozenSnapshot,
		LoadedRows: len(buffer.snapshot.Scrollback),
		ViewTopRow: maxInt(0, buffer.totalRows()-buffer.height),
		Cursor:     start,
	}
	m.syncCopyModeViewport(buffer, start)
	return true
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
	if m.workbench != nil {
		if tab := m.workbench.CurrentTab(); tab != nil {
			tab.ScrollOffset = m.copyModeRenderOffset(buffer)
		}
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

func (m *Model) beginCopySelection() {
	if !m.ensureCopyMode() {
		return
	}
	point := m.copyMode.Cursor
	m.copyMode.Mark = &copyModePoint{Row: point.Row, Col: point.Col}
	m.render.Invalidate()
}

func normalizeCopySelection(a, b copyModePoint) (copyModePoint, copyModePoint) {
	if a.Row > b.Row || (a.Row == b.Row && a.Col > b.Col) {
		return b, a
	}
	return a, b
}

func (m *Model) copyModeSelectedText() (string, bool) {
	if !m.ensureCopyMode() || m.copyMode.Mark == nil {
		return "", false
	}
	buffer, ok := m.activeCopyModeBuffer()
	if !ok || buffer.totalRows() == 0 {
		return "", false
	}
	start, end := normalizeCopySelection(buffer.clampPoint(*m.copyMode.Mark), buffer.clampPoint(m.copyMode.Cursor))
	var out strings.Builder
	for row := start.Row; row <= end.Row; row++ {
		cells := buffer.row(row)
		firstCol := 0
		lastCol := buffer.rowMaxCol(row)
		if row == start.Row {
			firstCol = start.Col
		}
		if row == end.Row {
			lastCol = end.Col
		}
		if lastCol < firstCol {
			lastCol = firstCol
		}
		firstCol = buffer.normalizeCol(row, firstCol)
		lastCol = buffer.normalizeCol(row, lastCol)
		for col := firstCol; col <= lastCol; col++ {
			if col >= 0 && col < len(cells) && cells[col].Content == "" && cells[col].Width == 0 {
				continue
			}
			if col >= 0 && col < len(cells) && cells[col].Content != "" {
				out.WriteString(cells[col].Content)
				continue
			}
			out.WriteByte(' ')
		}
		if row < end.Row {
			out.WriteByte('\n')
		}
	}
	return out.String(), true
}

func osc52ClipboardSequence(text string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	return "\x1b]52;c;" + encoded + "\x07"
}

func (m *Model) copySelectionToClipboard(exit bool) tea.Cmd {
	text, ok := m.copyModeSelectedText()
	if !ok || text == "" {
		return m.showError(fmt.Errorf("copy mode selection is empty"))
	}
	m.yankBuffer = text
	m.pushClipboardHistory(text, m.copyMode.PaneID)
	clipboardErr := error(nil)
	if systemClipboardWriter != nil {
		clipboardErr = systemClipboardWriter(text)
	}
	if m.cursorOut != nil {
		if err := m.cursorOut.WriteControlSequence(osc52ClipboardSequence(text)); err != nil && clipboardErr == nil {
			clipboardErr = err
		}
	}
	if exit {
		m.leaveCopyMode()
		m.setMode(input.ModeState{Kind: input.ModeNormal})
	} else {
		m.clearCopySelection()
	}
	m.render.Invalidate()
	if clipboardErr != nil && m.yankBuffer == "" {
		return m.showError(clipboardErr)
	}
	return m.showNotice(fmt.Sprintf("copied %d bytes", len([]byte(text))))
}

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

func (m *Model) startMouseCopySelection(screenX, screenY int) bool {
	point, ok := m.copyModePointAtMouse(screenX, screenY)
	if !ok {
		return false
	}
	m.copyMode.Cursor = point
	m.copyMode.Mark = &copyModePoint{Row: point.Row, Col: point.Col}
	m.copyMode.MouseSelecting = true
	m.copyMode.AutoScrollDir = 0
	m.copyMode.AutoScrollSeq++
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
	m.copyMode.AutoScrollSeq++
	seq := m.copyMode.AutoScrollSeq
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
	m.copyMode.AutoScrollSeq++
}

func (m *Model) handleCopyModeAutoScroll(seq uint64) tea.Cmd {
	if !m.ensureCopyMode() || !m.copyMode.MouseSelecting || m.copyMode.AutoScrollDir == 0 || seq != m.copyMode.AutoScrollSeq {
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

func (m *Model) adjustCopyModeAfterSnapshotLoaded(terminalID string) {
	if m == nil || terminalID == "" || m.copyMode.PaneID == "" || m.workbench == nil {
		return
	}
	if m.copyMode.Snapshot != nil {
		return
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID != m.copyMode.PaneID || pane.TerminalID != terminalID {
		return
	}
	buffer, ok := m.activeCopyModeBuffer()
	if !ok {
		return
	}
	if delta := len(buffer.snapshot.Scrollback) - m.copyMode.LoadedRows; delta > 0 {
		m.copyMode.ViewTopRow += delta
		m.copyMode.Cursor.Row += delta
		m.copyMode.Cursor = buffer.clampPoint(m.copyMode.Cursor)
		if m.copyMode.Mark != nil {
			point := *m.copyMode.Mark
			point.Row += delta
			point = buffer.clampPoint(point)
			m.copyMode.Mark = &point
		}
	}
	m.copyMode.LoadedRows = len(buffer.snapshot.Scrollback)
	m.syncCopyModeViewport(buffer, m.copyMode.Cursor)
}

func (m *Model) pasteBufferToActiveCmd() tea.Cmd {
	if m == nil {
		return nil
	}
	if m.yankBuffer == "" {
		return m.showError(fmt.Errorf("copy buffer is empty"))
	}
	return m.pasteTextToActiveCmd(m.yankBuffer)
}

func (m *Model) pasteClipboardToActiveCmd() tea.Cmd {
	if m == nil {
		return nil
	}
	if systemClipboardReader == nil {
		return m.showError(fmt.Errorf("system clipboard unavailable"))
	}
	text, err := systemClipboardReader()
	if err != nil {
		return m.showError(err)
	}
	if text == "" {
		return m.showError(fmt.Errorf("system clipboard is empty"))
	}
	return m.pasteTextToActiveCmd(text)
}

func (m *Model) pasteTextToActiveCmd(text string) tea.Cmd {
	if m == nil || text == "" {
		return nil
	}
	paneID := ""
	if m.workbench != nil {
		if pane := m.workbench.ActivePane(); pane != nil {
			paneID = pane.ID
		}
	}
	if paneID == "" {
		return m.showError(fmt.Errorf("no active pane"))
	}
	return m.handleTerminalInput(input.TerminalInput{
		Kind:   input.TerminalInputPaste,
		PaneID: paneID,
		Text:   text,
	})
}
