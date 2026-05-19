package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/internal/protocol"
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
	PaneID          string
	TerminalID      string
	Snapshot        *protocol.Snapshot
	BaselineVersion uint64
}

func (m *Model) activePaneInCopyMode() bool {
	if m == nil || m.workbench == nil {
		return false
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID == "" {
		return false
	}
	_, ok := m.copyModeStateForPane(pane.ID)
	return ok
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
	cloned.Scrollback = protocol.CloneCompactRows(snapshot.Scrollback)
	cloned.ScreenTimestamps = append([]time.Time(nil), snapshot.ScreenTimestamps...)
	cloned.ScrollbackTimestamps = append([]time.Time(nil), snapshot.ScrollbackTimestamps...)
	cloned.ScreenRowKinds = append([]string(nil), snapshot.ScreenRowKinds...)
	cloned.ScrollbackRowKinds = append([]string(nil), snapshot.ScrollbackRowKinds...)
	cloned.ScreenWrapped = append([]bool(nil), snapshot.ScreenWrapped...)
	cloned.ScrollbackWrapped = append([]bool(nil), snapshot.ScrollbackWrapped...)
	return &cloned
}

func snapshotUsesAlternateScreen(snapshot *protocol.Snapshot) bool {
	return snapshot != nil && (snapshot.Modes.AlternateScreen || snapshot.Screen.IsAlternateScreen)
}

func clearSnapshotScrollback(snapshot *protocol.Snapshot) {
	if snapshot == nil {
		return
	}
	snapshot.Scrollback = nil
	snapshot.ScrollbackTimestamps = nil
	snapshot.ScrollbackRowKinds = nil
	snapshot.ScrollbackWrapped = nil
	snapshot.ScrollbackOffset = 0
	snapshot.ScrollbackTotal = 0
	snapshot.ScrollbackHasMore = false
	snapshot.ScrollbackLoadedRows = 0
	snapshot.HistoryGeneration = 0
	snapshot.ScrollbackFirstRowID = 0
	snapshot.ScrollbackLastRowID = 0
}

func snapshotScrollbackLoadedDepth(snapshot *protocol.Snapshot) int {
	if snapshot == nil {
		return 0
	}
	if snapshot.ScrollbackLoadedRows > 0 {
		return snapshot.ScrollbackLoadedRows
	}
	return snapshot.ScrollbackOffset + len(snapshot.Scrollback)
}

func historyPageContinuesSnapshot(current, page *protocol.Snapshot) bool {
	if current == nil || page == nil {
		return false
	}
	if current.HistoryGeneration != 0 && page.HistoryGeneration != 0 && current.HistoryGeneration != page.HistoryGeneration {
		return false
	}
	if current.ScrollbackFirstRowID != 0 && page.ScrollbackLastRowID != 0 && page.ScrollbackLastRowID+1 != current.ScrollbackFirstRowID {
		return false
	}
	return true
}

func trimCopyModeSnapshotScrollbackWindow(snapshot *protocol.Snapshot, limit int, trimNewest bool) int {
	if snapshot == nil || limit <= 0 || len(snapshot.Scrollback) <= limit {
		return 0
	}
	drop := len(snapshot.Scrollback) - limit
	if trimNewest {
		keep := len(snapshot.Scrollback) - drop
		snapshot.Scrollback = protocol.CloneCompactRows(snapshot.Scrollback[:keep])
		snapshot.ScrollbackTimestamps = cloneTimePrefix(snapshot.ScrollbackTimestamps, keep)
		snapshot.ScrollbackRowKinds = cloneStringPrefix(snapshot.ScrollbackRowKinds, keep)
		snapshot.ScrollbackWrapped = cloneBoolPrefix(snapshot.ScrollbackWrapped, keep)
		snapshot.ScrollbackOffset += drop
		if snapshot.ScrollbackLastRowID >= uint64(drop) {
			snapshot.ScrollbackLastRowID -= uint64(drop)
		}
		return drop
	}
	snapshot.Scrollback = protocol.CloneCompactRows(snapshot.Scrollback[drop:])
	snapshot.ScrollbackTimestamps = cloneTimeSuffix(snapshot.ScrollbackTimestamps, drop)
	snapshot.ScrollbackRowKinds = cloneStringSuffix(snapshot.ScrollbackRowKinds, drop)
	snapshot.ScrollbackWrapped = cloneBoolSuffix(snapshot.ScrollbackWrapped, drop)
	snapshot.ScrollbackFirstRowID += uint64(drop)
	snapshot.ScrollbackHasMore = true
	return 0
}

func cloneTimePrefix(values []time.Time, keep int) []time.Time {
	if keep <= 0 || len(values) < keep {
		return nil
	}
	return append([]time.Time(nil), values[:keep]...)
}

func cloneStringPrefix(values []string, keep int) []string {
	if keep <= 0 || len(values) < keep {
		return nil
	}
	return append([]string(nil), values[:keep]...)
}

func cloneBoolPrefix(values []bool, keep int) []bool {
	if keep <= 0 || len(values) < keep {
		return nil
	}
	return append([]bool(nil), values[:keep]...)
}

func cloneTimeSuffix(values []time.Time, drop int) []time.Time {
	if drop < 0 || len(values) <= drop {
		return nil
	}
	return append([]time.Time(nil), values[drop:]...)
}

func cloneStringSuffix(values []string, drop int) []string {
	if drop < 0 || len(values) <= drop {
		return nil
	}
	return append([]string(nil), values[drop:]...)
}

func cloneBoolSuffix(values []bool, drop int) []bool {
	if drop < 0 || len(values) <= drop {
		return nil
	}
	return append([]bool(nil), values[drop:]...)
}

func firstNonZeroUint64(values ...uint64) uint64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func maxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func (m *Model) copyModeSnapshot(terminalID string, snapshot *protocol.Snapshot) *protocol.Snapshot {
	cloned := cloneSnapshot(snapshot)
	if snapshotUsesAlternateScreen(cloned) {
		if m != nil && m.runtime != nil {
			cloned = m.runtime.AlternateScrollbackSnapshot(terminalID, cloned)
		} else {
			clearSnapshotScrollback(cloned)
		}
	}
	return cloned
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
	if m.copyMode.PaneID != "" && m.copyModes != nil {
		delete(m.copyModes, m.copyMode.PaneID)
	}
	m.copyMode = copyModeState{}
}

func (m *Model) copyModeStateForPane(paneID string) (copyModeState, bool) {
	if m == nil || paneID == "" {
		return copyModeState{}, false
	}
	if m.copyMode.PaneID == paneID {
		return m.copyMode, true
	}
	if m.copyModes == nil {
		return copyModeState{}, false
	}
	state, ok := m.copyModes[paneID]
	if !ok || state.PaneID == "" {
		return copyModeState{}, false
	}
	return state, true
}

func (m *Model) saveCopyModeState(state copyModeState) {
	if m == nil || state.PaneID == "" {
		return
	}
	if m.copyModes == nil {
		m.copyModes = make(map[string]copyModeState)
	}
	m.copyModes[state.PaneID] = state
	if pane := m.workbenchActivePane(); pane != nil && pane.ID == state.PaneID {
		m.copyMode = state
		return
	}
	if m.copyMode.PaneID == state.PaneID {
		m.copyMode = state
	}
}

func (m *Model) saveCurrentCopyModeState() {
	if m == nil || m.copyMode.PaneID == "" {
		return
	}
	m.saveCopyModeState(m.copyMode)
}

func (m *Model) loadCopyModeStateForPane(paneID string) bool {
	state, ok := m.copyModeStateForPane(paneID)
	if !ok {
		return false
	}
	m.copyMode = state
	return true
}

func (m *Model) loadActiveCopyModeState() bool {
	pane := m.workbenchActivePane()
	if pane == nil {
		return false
	}
	return m.loadCopyModeStateForPane(pane.ID)
}

func (m *Model) deleteCopyModeStateForPane(paneID string) {
	if m == nil || paneID == "" {
		return
	}
	if m.copyModes != nil {
		delete(m.copyModes, paneID)
	}
	if m.copyMode.PaneID == paneID {
		m.copyMode = copyModeState{}
		if pane := m.workbenchActivePane(); pane != nil && pane.ID != paneID {
			_ = m.loadCopyModeStateForPane(pane.ID)
		}
	}
}

func (m *Model) workbenchActivePane() *workbench.PaneState {
	if m == nil || m.workbench == nil {
		return nil
	}
	return m.workbench.ActivePane()
}

func (m *Model) allCopyModeStates() []copyModeState {
	if m == nil {
		return nil
	}
	statesByPane := make(map[string]copyModeState)
	if m.copyModes != nil {
		for paneID, state := range m.copyModes {
			if paneID != "" && state.PaneID != "" {
				statesByPane[paneID] = state
			}
		}
	}
	if m.copyMode.PaneID != "" {
		statesByPane[m.copyMode.PaneID] = m.copyMode
	}
	if len(statesByPane) == 0 {
		return nil
	}
	paneIDs := make([]string, 0, len(statesByPane))
	for paneID := range statesByPane {
		paneIDs = append(paneIDs, paneID)
	}
	sort.Strings(paneIDs)
	out := make([]copyModeState, 0, len(paneIDs))
	for _, paneID := range paneIDs {
		out = append(out, statesByPane[paneID])
	}
	return out
}

func (m *Model) clearCopySelection() {
	if m == nil {
		return
	}
	m.copyMode.Mark = nil
	m.stopMouseCopySelection()
	m.saveCurrentCopyModeState()
}

func (m *Model) reconcileCopyModeContext() {
	if m == nil || m.workbench == nil {
		return
	}
	changed := false
	for _, state := range m.allCopyModeStates() {
		tabID, err := m.workbench.ResolvePaneTab("", state.PaneID)
		if err == nil && tabID != "" {
			continue
		}
		m.resetPaneViewport(state.PaneID)
		m.deleteCopyModeStateForPane(state.PaneID)
		changed = true
	}
	if m.copyMode.PaneID == "" {
		m.copyModeResume = copyModeResumeState{}
	}
	if changed && m.mode().Kind == input.ModeDisplay && !m.activePaneInCopyMode() {
		m.setMode(input.ModeState{Kind: input.ModeNormal})
	}
	if changed {
		m.render.Invalidate()
	}
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
		PaneID:          pane.ID,
		TerminalID:      pane.TerminalID,
		Snapshot:        cloneSnapshot(m.copyMode.Snapshot),
		BaselineVersion: terminal.SurfaceVersion,
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
	// If a live local surface exists, prefer it immediately on copy-mode exit.
	// Keeping the frozen snapshot until another interaction lands makes the
	// terminal appear stuck even though the local VTerm is already current.
	if terminal.VTerm != nil && terminal.SurfaceVersion > 0 {
		return "", nil, false
	}
	if terminal.SurfaceVersion != m.copyModeResume.BaselineVersion {
		return "", nil, false
	}
	return pane.ID, m.copyModeResume.Snapshot, true
}

func (m *Model) leaveCopyMode() {
	if m == nil {
		return
	}
	if m.copyMode.PaneID == "" {
		_ = m.loadActiveCopyModeState()
	}
	paneID := m.copyMode.PaneID
	if active := m.workbenchActivePane(); active != nil {
		if state, ok := m.copyModeStateForPane(active.ID); ok {
			m.copyMode = state
			paneID = state.PaneID
		}
	}
	m.resetPaneViewport(paneID)
	m.deleteCopyModeStateForPane(paneID)
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
		m.saveCurrentCopyModeState()
	}
	_ = m.loadCopyModeStateForPane(pane.ID)
	if m.copyMode.PaneID == pane.ID {
		buffer, ok := m.activeCopyModeBuffer()
		if !ok || buffer.totalRows() == 0 {
			return false
		}
		if delta := snapshotScrollbackLoadedDepth(buffer.snapshot) - m.copyMode.LoadedRows; delta > 0 {
			m.copyMode.ViewTopRow += delta
			m.copyMode.Cursor.Row += delta
			if m.copyMode.Mark != nil {
				point := *m.copyMode.Mark
				point.Row += delta
				m.copyMode.Mark = &point
			}
		}
		m.copyMode.LoadedRows = snapshotScrollbackLoadedDepth(buffer.snapshot)
		m.copyMode.Cursor = buffer.clampPoint(m.copyMode.Cursor)
		if m.copyMode.Mark != nil {
			point := buffer.clampPoint(*m.copyMode.Mark)
			m.copyMode.Mark = &point
		}
		m.syncCopyModeViewport(buffer, m.copyMode.Cursor)
		m.saveCurrentCopyModeState()
		return true
	}
	liveBuffer, ok := m.liveCopyModeBufferForPane(pane.ID)
	if !ok || liveBuffer.totalRows() == 0 {
		return false
	}
	frozenSnapshot := m.copyModeSnapshot(pane.TerminalID, liveBuffer.snapshot)
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
		LoadedRows: snapshotScrollbackLoadedDepth(buffer.snapshot),
		ViewTopRow: maxInt(0, buffer.totalRows()-buffer.height),
		Cursor:     start,
	}
	m.syncCopyModeViewport(buffer, start)
	m.saveCurrentCopyModeState()
	return true
}

func (m *Model) adjustCopyModeAfterSnapshotLoaded(terminalID string, snapshot *protocol.Snapshot) {
	m.adjustCopyModeAfterSnapshotLoadedWithWindow(terminalID, snapshot, 0)
}

func (m *Model) snapshotPageTargetsActiveCopyMode(terminalID string) bool {
	return m.snapshotPageTargetsAnyCopyMode(terminalID)
}

func (m *Model) snapshotPageTargetsAnyCopyMode(terminalID string) bool {
	if m == nil || terminalID == "" || m.workbench == nil {
		return false
	}
	for _, state := range m.allCopyModeStates() {
		if state.Snapshot == nil {
			continue
		}
		pane, _, ok := m.copyModePaneAndContentRect(state.PaneID)
		if ok && pane != nil && pane.TerminalID == terminalID {
			return true
		}
	}
	return false
}

func (m *Model) adjustCopyModeAfterSnapshotLoadedWithWindow(terminalID string, snapshot *protocol.Snapshot, offset int) {
	if m == nil || terminalID == "" || m.workbench == nil {
		return
	}
	originalPaneID := m.copyMode.PaneID
	for _, state := range m.allCopyModeStates() {
		pane, _, ok := m.copyModePaneAndContentRect(state.PaneID)
		if !ok || pane == nil || pane.ID != state.PaneID || pane.TerminalID != terminalID {
			continue
		}
		m.copyMode = state
		if m.copyMode.Snapshot != nil {
			m.extendFrozenCopyModeSnapshot(snapshot, offset)
			continue
		}
		buffer, ok := m.activeCopyModeBuffer()
		if !ok {
			continue
		}
		if delta := snapshotScrollbackLoadedDepth(buffer.snapshot) - m.copyMode.LoadedRows; delta > 0 {
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
		m.copyMode.LoadedRows = snapshotScrollbackLoadedDepth(buffer.snapshot)
		m.syncCopyModeViewport(buffer, m.copyMode.Cursor)
		m.saveCurrentCopyModeState()
	}
	if originalPaneID != "" {
		_ = m.loadCopyModeStateForPane(originalPaneID)
	}
}

func (m *Model) extendFrozenCopyModeSnapshot(loaded *protocol.Snapshot, offset int) {
	if m == nil || m.copyMode.Snapshot == nil || loaded == nil {
		return
	}
	if snapshotUsesAlternateScreen(m.copyMode.Snapshot) || snapshotUsesAlternateScreen(loaded) {
		return
	}
	next := cloneSnapshot(m.copyMode.Snapshot)
	if next == nil {
		return
	}
	delta := 0
	trimNewest := false
	switch {
	case offset > 0:
		if offset != snapshotScrollbackLoadedDepth(next) {
			return
		}
		if !historyPageContinuesSnapshot(next, loaded) {
			return
		}
		delta = snapshotScrollbackLoadedDepth(loaded) - snapshotScrollbackLoadedDepth(next)
		if delta == 0 {
			return
		}
		previousOffset := next.ScrollbackOffset
		next.Scrollback = append(protocol.CloneCompactRows(loaded.Scrollback), next.Scrollback...)
		next.ScrollbackTimestamps = append(append([]time.Time(nil), loaded.ScrollbackTimestamps...), next.ScrollbackTimestamps...)
		next.ScrollbackRowKinds = append(append([]string(nil), loaded.ScrollbackRowKinds...), next.ScrollbackRowKinds...)
		next.ScrollbackWrapped = append(append([]bool(nil), loaded.ScrollbackWrapped...), next.ScrollbackWrapped...)
		next.ScrollbackOffset = previousOffset
		trimNewest = true
	default:
		if len(loaded.Scrollback) <= len(next.Scrollback) {
			return
		}
		delta = len(loaded.Scrollback) - len(next.Scrollback)
		next.Scrollback = protocol.CloneCompactRows(loaded.Scrollback)
		next.ScrollbackTimestamps = append([]time.Time(nil), loaded.ScrollbackTimestamps...)
		next.ScrollbackRowKinds = append([]string(nil), loaded.ScrollbackRowKinds...)
		next.ScrollbackWrapped = append([]bool(nil), loaded.ScrollbackWrapped...)
		next.ScrollbackOffset = loaded.ScrollbackOffset
	}
	next.ScrollbackTotal = loaded.ScrollbackTotal
	next.ScrollbackHasMore = loaded.ScrollbackHasMore
	next.ScrollbackLoadedRows = maxInt(loaded.ScrollbackLoadedRows, next.ScrollbackLoadedRows)
	next.HistoryGeneration = loaded.HistoryGeneration
	next.ScrollbackFirstRowID = firstNonZeroUint64(loaded.ScrollbackFirstRowID, next.ScrollbackFirstRowID)
	next.ScrollbackLastRowID = maxUint64(loaded.ScrollbackLastRowID, next.ScrollbackLastRowID)
	trimmedNewest := trimCopyModeSnapshotScrollbackWindow(next, terminalMaterializedScrollbackLimit, trimNewest)
	m.copyMode.Snapshot = next
	m.copyMode.LoadedRows = snapshotScrollbackLoadedDepth(next)

	buffer, ok := m.activeCopyModeBuffer()
	if !ok {
		return
	}
	m.copyMode.ViewTopRow += delta - trimmedNewest
	m.copyMode.Cursor.Row += delta - trimmedNewest
	m.copyMode.Cursor = buffer.clampPoint(m.copyMode.Cursor)
	if m.copyMode.Mark != nil {
		point := *m.copyMode.Mark
		point.Row += delta - trimmedNewest
		point = buffer.clampPoint(point)
		m.copyMode.Mark = &point
	}
	m.syncCopyModeViewport(buffer, m.copyMode.Cursor)
	m.saveCurrentCopyModeState()
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
