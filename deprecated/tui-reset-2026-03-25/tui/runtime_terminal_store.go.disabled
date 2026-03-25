package tui

import (
	"fmt"
	"sync"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/domain/types"
	localvterm "github.com/lozzow/termx/vterm"
)

const runtimeScrollbackSize = 200

type RuntimeTerminalStore interface {
	Session(terminalID types.TerminalID) (TerminalRuntimeSession, bool)
	Snapshot(terminalID types.TerminalID) (*protocol.Snapshot, bool)
	Status(terminalID types.TerminalID) (RuntimeTerminalStatus, bool)
}

type RuntimeTerminalStatus struct {
	State                string
	ExitCode             *int
	Size                 protocol.Size
	Closed               bool
	ObserverOnly         bool
	SyncLost             bool
	SyncLostDroppedBytes uint64
	RemovedReason        string
	ReadError            string
}

type runtimeTerminalStore struct {
	mu        sync.RWMutex
	terminals map[types.TerminalID]*runtimeTerminalRecord
}

type runtimeTerminalRecord struct {
	session  TerminalRuntimeSession
	snapshot *protocol.Snapshot
	vt       *localvterm.VTerm
	status   RuntimeTerminalStatus
}

func NewRuntimeTerminalStore(sessions RuntimeSessions) *runtimeTerminalStore {
	store := &runtimeTerminalStore{
		terminals: make(map[types.TerminalID]*runtimeTerminalRecord, len(sessions.Terminals)),
	}
	for terminalID, session := range sessions.Terminals {
		store.terminals[terminalID] = newRuntimeTerminalRecord(session)
	}
	return store
}

func newRuntimeTerminalRecord(session TerminalRuntimeSession) *runtimeTerminalRecord {
	record := &runtimeTerminalRecord{
		session: session,
	}
	record.loadSnapshot(cloneSnapshot(session.Snapshot))
	return record
}

func (s *runtimeTerminalStore) Session(terminalID types.TerminalID) (TerminalRuntimeSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.terminals[terminalID]
	if !ok || record == nil {
		return TerminalRuntimeSession{}, false
	}
	return record.session, true
}

func (s *runtimeTerminalStore) Snapshot(terminalID types.TerminalID) (*protocol.Snapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.terminals[terminalID]
	if !ok || record == nil || record.snapshot == nil {
		return nil, false
	}
	return cloneSnapshot(record.snapshot), true
}

func (s *runtimeTerminalStore) Status(terminalID types.TerminalID) (RuntimeTerminalStatus, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.terminals[terminalID]
	if !ok || record == nil {
		return RuntimeTerminalStatus{}, false
	}
	return cloneRuntimeTerminalStatus(record.status), true
}

func (s *runtimeTerminalStore) WriteOutput(terminalID types.TerminalID, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.terminals[terminalID]
	if !ok || record == nil {
		return fmt.Errorf("runtime terminal %s not found", terminalID)
	}
	if record.vt == nil {
		record.ensureVTerm()
	}
	if _, err := record.vt.Write(payload); err != nil {
		return err
	}
	record.snapshot = snapshotFromVTerm(string(terminalID), record.vt)
	record.status.Size = record.snapshot.Size
	record.status.SyncLost = false
	record.status.SyncLostDroppedBytes = 0
	return nil
}

func (s *runtimeTerminalStore) LoadSnapshot(terminalID types.TerminalID, snapshot *protocol.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.terminals[terminalID]
	if !ok || record == nil {
		record = &runtimeTerminalRecord{session: TerminalRuntimeSession{TerminalID: terminalID}}
		s.terminals[terminalID] = record
	}
	record.loadSnapshot(cloneSnapshot(snapshot))
	if snapshot != nil {
		record.status.Size = snapshot.Size
	}
	record.status.SyncLost = false
	record.status.SyncLostDroppedBytes = 0
}

func (s *runtimeTerminalStore) Resize(terminalID types.TerminalID, size protocol.Size) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.terminals[terminalID]
	if !ok || record == nil {
		return
	}
	if record.vt == nil {
		record.ensureVTerm()
	}
	cols, rows := normalizedProtocolSize(size)
	record.vt.Resize(cols, rows)
	record.snapshot = snapshotFromVTerm(string(terminalID), record.vt)
	record.snapshot.Size = protocol.Size{Cols: uint16(cols), Rows: uint16(rows)}
	record.status.Size = record.snapshot.Size
}

func (s *runtimeTerminalStore) MarkClosed(terminalID types.TerminalID, exitCode int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.terminals[terminalID]
	if !ok || record == nil {
		return
	}
	record.status.Closed = true
	record.status.ExitCode = intPtr(exitCode)
	record.status.State = string(types.TerminalRunStateExited)
}

func (s *runtimeTerminalStore) MarkSyncLost(terminalID types.TerminalID, dropped uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.terminals[terminalID]
	if !ok || record == nil {
		return
	}
	record.status.SyncLost = true
	record.status.SyncLostDroppedBytes = dropped
}

func (s *runtimeTerminalStore) ApplyEvent(evt protocol.Event) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	terminalID := types.TerminalID(evt.TerminalID)
	record, ok := s.terminals[terminalID]
	if !ok || record == nil {
		record = &runtimeTerminalRecord{session: TerminalRuntimeSession{TerminalID: terminalID}}
		s.terminals[terminalID] = record
	}

	switch evt.Type {
	case protocol.EventTerminalResized:
		if evt.Resized != nil {
			record.ensureVTerm()
			cols, rows := normalizedProtocolSize(evt.Resized.NewSize)
			record.vt.Resize(cols, rows)
			record.snapshot = snapshotFromVTerm(string(terminalID), record.vt)
			record.snapshot.Size = evt.Resized.NewSize
			record.status.Size = evt.Resized.NewSize
		}
	case protocol.EventTerminalStateChanged:
		if evt.StateChanged != nil {
			record.status.State = evt.StateChanged.NewState
			record.status.ExitCode = cloneIntPtr(evt.StateChanged.ExitCode)
			if evt.StateChanged.NewState != "" && evt.StateChanged.NewState != string(types.TerminalRunStateRunning) {
				record.status.Closed = true
			}
		}
	case protocol.EventTerminalRemoved:
		if evt.Removed != nil {
			record.status.RemovedReason = evt.Removed.Reason
		}
	case protocol.EventTerminalReadError:
		if evt.ReadError != nil && evt.ReadError.Error != "" {
			record.status.ReadError = evt.ReadError.Error
			return []string{evt.ReadError.Error}
		}
	case protocol.EventCollaboratorsRevoked:
		record.status.ObserverOnly = true
		return []string{"terminal switched to observer-only mode"}
	}
	return nil
}

func (r *runtimeTerminalRecord) loadSnapshot(snapshot *protocol.Snapshot) {
	r.snapshot = snapshot
	r.vt = nil
	if snapshot == nil {
		return
	}
	r.status.Size = snapshot.Size
	r.ensureVTerm()
}

func (r *runtimeTerminalRecord) ensureVTerm() {
	if r.vt != nil {
		return
	}
	cols, rows := snapshotDimensions(r.snapshot)
	r.vt = localvterm.New(cols, rows, runtimeScrollbackSize, nil)
	if r.snapshot != nil {
		loadSnapshotIntoVTerm(r.vt, r.snapshot)
	}
}

func activePane(state types.AppState) (types.PaneState, bool) {
	workspace, ok := state.Domain.Workspaces[state.Domain.ActiveWorkspaceID]
	if !ok {
		return types.PaneState{}, false
	}
	tab, ok := workspace.Tabs[workspace.ActiveTabID]
	if !ok {
		return types.PaneState{}, false
	}
	pane, ok := tab.Panes[tab.ActivePaneID]
	if !ok {
		return types.PaneState{}, false
	}
	return pane, true
}

func activeTerminalSession(state types.AppState, store RuntimeTerminalStore) (TerminalRuntimeSession, bool) {
	if store == nil {
		return TerminalRuntimeSession{}, false
	}
	pane, ok := activePane(state)
	if !ok || pane.TerminalID == "" {
		return TerminalRuntimeSession{}, false
	}
	return store.Session(pane.TerminalID)
}

func cloneSnapshot(in *protocol.Snapshot) *protocol.Snapshot {
	if in == nil {
		return nil
	}
	out := *in
	out.Screen = protocol.ScreenData{
		Cells:             cloneCellRows(in.Screen.Cells),
		IsAlternateScreen: in.Screen.IsAlternateScreen,
	}
	out.Scrollback = cloneCellRows(in.Scrollback)
	out.Timestamp = in.Timestamp
	return &out
}

func cloneCellRows(rows [][]protocol.Cell) [][]protocol.Cell {
	if rows == nil {
		return nil
	}
	out := make([][]protocol.Cell, len(rows))
	for i, row := range rows {
		out[i] = append([]protocol.Cell(nil), row...)
	}
	return out
}

func loadSnapshotIntoVTerm(vt *localvterm.VTerm, snap *protocol.Snapshot) {
	if vt == nil || snap == nil {
		return
	}
	cols, rows := vt.Size()
	vt.LoadSnapshot(protocolScreenToVTerm(snap.Screen), protocolCursorToVTerm(snap.Cursor), protocolModesToVTerm(snap.Modes))
	if cols > 0 && rows > 0 {
		vt.Resize(cols, rows)
	}
}

func snapshotFromVTerm(terminalID string, vt *localvterm.VTerm) *protocol.Snapshot {
	if vt == nil {
		return nil
	}
	screen := vt.ScreenContent()
	rows := make([][]protocol.Cell, 0, len(screen.Cells))
	for _, row := range screen.Cells {
		out := make([]protocol.Cell, 0, len(row))
		for _, cell := range row {
			out = append(out, protocolCellFromVTermCell(cell))
		}
		rows = append(rows, out)
	}
	scrollback := vt.ScrollbackContent()
	backlog := make([][]protocol.Cell, 0, len(scrollback))
	for _, row := range scrollback {
		out := make([]protocol.Cell, 0, len(row))
		for _, cell := range row {
			out = append(out, protocolCellFromVTermCell(cell))
		}
		backlog = append(backlog, out)
	}
	cols, rowsCount := vt.Size()
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rowsCount)},
		Screen: protocol.ScreenData{
			Cells:             rows,
			IsAlternateScreen: screen.IsAlternateScreen,
		},
		Scrollback: backlog,
		Cursor:     protocolCursorFromVTerm(vt.CursorState()),
		Modes:      protocolModesFromVTerm(vt.Modes()),
		Timestamp:  time.Now(),
	}
}

func protocolCellFromVTermCell(cell localvterm.Cell) protocol.Cell {
	return protocol.Cell{
		Content: cell.Content,
		Width:   cell.Width,
		Style: protocol.CellStyle{
			FG:            cell.Style.FG,
			BG:            cell.Style.BG,
			Bold:          cell.Style.Bold,
			Italic:        cell.Style.Italic,
			Underline:     cell.Style.Underline,
			Blink:         cell.Style.Blink,
			Reverse:       cell.Style.Reverse,
			Strikethrough: cell.Style.Strikethrough,
		},
	}
}

func protocolScreenToVTerm(screen protocol.ScreenData) localvterm.ScreenData {
	rows := make([][]localvterm.Cell, len(screen.Cells))
	for y, row := range screen.Cells {
		rows[y] = make([]localvterm.Cell, len(row))
		for x, cell := range row {
			rows[y][x] = localvterm.Cell{
				Content: cell.Content,
				Width:   cell.Width,
				Style: localvterm.CellStyle{
					FG:            cell.Style.FG,
					BG:            cell.Style.BG,
					Bold:          cell.Style.Bold,
					Italic:        cell.Style.Italic,
					Underline:     cell.Style.Underline,
					Blink:         cell.Style.Blink,
					Reverse:       cell.Style.Reverse,
					Strikethrough: cell.Style.Strikethrough,
				},
			}
		}
	}
	return localvterm.ScreenData{
		Cells:             rows,
		IsAlternateScreen: screen.IsAlternateScreen,
	}
}

func protocolCursorToVTerm(cursor protocol.CursorState) localvterm.CursorState {
	return localvterm.CursorState{
		Row:     cursor.Row,
		Col:     cursor.Col,
		Visible: cursor.Visible,
		Shape:   localvterm.CursorShape(cursor.Shape),
		Blink:   cursor.Blink,
	}
}

func protocolModesToVTerm(modes protocol.TerminalModes) localvterm.TerminalModes {
	return localvterm.TerminalModes{
		AlternateScreen:   modes.AlternateScreen,
		MouseTracking:     modes.MouseTracking,
		BracketedPaste:    modes.BracketedPaste,
		ApplicationCursor: modes.ApplicationCursor,
		AutoWrap:          modes.AutoWrap,
	}
}

func protocolCursorFromVTerm(cursor localvterm.CursorState) protocol.CursorState {
	return protocol.CursorState{
		Row:     cursor.Row,
		Col:     cursor.Col,
		Visible: cursor.Visible,
		Shape:   string(cursor.Shape),
		Blink:   cursor.Blink,
	}
}

func protocolModesFromVTerm(modes localvterm.TerminalModes) protocol.TerminalModes {
	return protocol.TerminalModes{
		AlternateScreen:   modes.AlternateScreen,
		MouseTracking:     modes.MouseTracking,
		BracketedPaste:    modes.BracketedPaste,
		ApplicationCursor: modes.ApplicationCursor,
		AutoWrap:          modes.AutoWrap,
	}
}

func snapshotDimensions(snapshot *protocol.Snapshot) (int, int) {
	if snapshot != nil && snapshot.Size.Cols > 0 && snapshot.Size.Rows > 0 {
		return int(snapshot.Size.Cols), int(snapshot.Size.Rows)
	}
	if snapshot != nil {
		size := protocol.Size{
			Cols: uint16(maxSnapshotWidth(snapshot)),
			Rows: uint16(max(1, len(snapshot.Screen.Cells))),
		}
		return normalizedProtocolSize(size)
	}
	return 80, 24
}

func normalizedProtocolSize(size protocol.Size) (int, int) {
	cols := max(1, int(size.Cols))
	rows := max(1, int(size.Rows))
	return cols, rows
}

func maxSnapshotWidth(snapshot *protocol.Snapshot) int {
	width := 1
	if snapshot == nil {
		return width
	}
	for _, row := range snapshot.Screen.Cells {
		if len(row) > width {
			width = len(row)
		}
	}
	return width
}

func cloneRuntimeTerminalStatus(in RuntimeTerminalStatus) RuntimeTerminalStatus {
	in.ExitCode = cloneIntPtr(in.ExitCode)
	return in
}

func cloneIntPtr(in *int) *int {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func intPtr(value int) *int {
	return &value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
