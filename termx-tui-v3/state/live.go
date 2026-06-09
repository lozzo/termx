package state

type TerminalLiveState string

const (
	TerminalLivePending  TerminalLiveState = "pending"
	TerminalLiveAttached TerminalLiveState = "attached"
	TerminalLiveExited   TerminalLiveState = "exited"
	TerminalLiveError    TerminalLiveState = "error"
)

// TerminalSurfaceStore 保存当前实时 terminal surface 投影，不是历史 truth。
type TerminalSurfaceStore struct {
	TerminalID     string
	Revision       uint64
	Cols           int
	Rows           int
	Lines          []string
	Screen         [][]LiveCell
	Title          string
	Cursor         LiveCursor
	Modes          LiveTerminalModes
	Ready          bool
	State          TerminalLiveState
	ExitCode       int
	ExitReason     string
	Err            string
	ResizeBoundary LiveResizeBoundary
	Surfaces       map[string]LiveSurfaceSnapshot
}

// LiveResizeBoundary 是一次 content rect resize 后等待匹配 surface 的基线。
type LiveResizeBoundary struct {
	Active       bool
	PreviousCols int
	PreviousRows int
	Cols         int
	Rows         int
}

// LiveCursor 是 live surface 的 content-local 光标状态。
type LiveCursor struct {
	Visible bool
	Row     int
	Col     int
	Shape   string
}

// LiveTerminalModes 只保存影响 TUI 输入转发的终端模式位。
type LiveTerminalModes struct {
	MouseTracking bool
	MouseX10      bool
	MouseNormal   bool
	MouseButton   bool
	MouseAny      bool
	MouseSGR      bool
}

func (modes LiveTerminalModes) MousePassthroughEnabled() bool {
	return modes.MouseTracking || modes.MouseX10 || modes.MouseNormal || modes.MouseButton || modes.MouseAny || modes.MouseSGR
}

// LiveCell 是 protocol snapshot cell 的 reducer-owned 副本，只服务实时 terminal 展示。
type LiveCell struct {
	Text          string
	Width         int
	FG            string
	BG            string
	Bold          bool
	Italic        bool
	Underline     bool
	Blink         bool
	Reverse       bool
	Strikethrough bool
	LinkURL       string
	LinkParams    string
}

// TerminalSessionStore 保存当前 attach/live path 的 reducer-owned 会话状态。
type TerminalSessionStore struct {
	TerminalID   string
	Channel      uint16
	Attached     bool
	Cols         int
	Rows         int
	ResizePolicy string
	SurfaceID    string
	ViewID       string
	// Desired* 只记录已发出的 terminal resize 目标，用于连续 pane command 去重和 stale response guard。
	DesiredCols        int
	DesiredRows        int
	ResizeRequestSeq   uint64
	ResizeConfirmedSeq uint64
	LastError          string
	State              TerminalLiveState
	ExitCode           int
	ExitReason         string
}

// LiveSurfaceSnapshot 是 terminal service/event 回投给 reducer 的实时投影。
type LiveSurfaceSnapshot struct {
	TerminalID string
	Revision   uint64
	Cols       int
	Rows       int
	Lines      []string
	Screen     [][]LiveCell
	Title      string
	Cursor     LiveCursor
	Modes      LiveTerminalModes
	State      TerminalLiveState
	ExitCode   int
	ExitReason string
	Err        string
}

func (store TerminalSurfaceStore) ApplySnapshot(snapshot LiveSurfaceSnapshot) TerminalSurfaceStore {
	if snapshot.TerminalID == "" {
		snapshot.TerminalID = store.TerminalID
	}
	if snapshot.TerminalID == "" {
		return store
	}
	if snapshot.State == "" {
		snapshot.State = TerminalLiveAttached
	}
	if store.resizeBoundaryRejects(snapshot) {
		return store
	}
	if current, ok := store.snapshotForTerminal(snapshot.TerminalID); ok {
		if shouldRejectLiveSnapshot(current, snapshot) {
			return store
		}
		if snapshot.Revision == 0 {
			snapshot.Revision = current.Revision
		}
	}
	snapshot.Lines = cloneStrings(snapshot.Lines)
	snapshot.Screen = cloneLiveCellRows(snapshot.Screen)
	store.Surfaces = cloneLiveSurfaceSnapshots(store.Surfaces)
	store.Surfaces[snapshot.TerminalID] = snapshot
	if store.TerminalID == "" || store.TerminalID == snapshot.TerminalID {
		store = store.projectSnapshot(snapshot, true)
		if store.ResizeBoundary.Active && snapshot.Cols == store.ResizeBoundary.Cols && snapshot.Rows == store.ResizeBoundary.Rows {
			store.ResizeBoundary = LiveResizeBoundary{}
		}
		return store
	}
	return store
}

func (store TerminalSurfaceStore) Attach(terminalID string, cols int, rows int) TerminalSurfaceStore {
	if store.TerminalID != "" && store.TerminalID != terminalID {
		store.Lines = nil
		store.Screen = nil
		store.Cursor = LiveCursor{}
		store.Modes = LiveTerminalModes{}
		store.Ready = false
	}
	store.TerminalID = terminalID
	store.Revision = 0
	store.Cols = cols
	store.Rows = rows
	store.State = TerminalLiveAttached
	store.ExitCode = 0
	store.ExitReason = ""
	store.Err = ""
	store.ResizeBoundary = LiveResizeBoundary{}
	if terminalID != "" {
		store.Surfaces = cloneLiveSurfaceSnapshots(store.Surfaces)
		snapshot, ok := store.snapshotForTerminal(terminalID)
		if !ok {
			snapshot = LiveSurfaceSnapshot{}
		}
		snapshot.TerminalID = terminalID
		snapshot.Revision = 0
		if snapshot.Cols == 0 {
			snapshot.Cols = cols
		}
		if snapshot.Rows == 0 {
			snapshot.Rows = rows
		}
		snapshot.State = TerminalLiveAttached
		snapshot.ExitCode = 0
		snapshot.ExitReason = ""
		snapshot.Err = ""
		store.Surfaces[terminalID] = snapshot
	}
	return store
}

func (store TerminalSurfaceStore) MarkExited(terminalID string, exitCode int, reason string) TerminalSurfaceStore {
	if terminalID != "" {
		store.Surfaces = cloneLiveSurfaceSnapshots(store.Surfaces)
		snapshot, ok := store.snapshotForTerminal(terminalID)
		if !ok {
			snapshot = LiveSurfaceSnapshot{}
		}
		snapshot.TerminalID = terminalID
		snapshot.State = TerminalLiveExited
		snapshot.ExitCode = exitCode
		snapshot.ExitReason = reason
		snapshot.Err = ""
		snapshot.Cursor = LiveCursor{}
		snapshot.Modes = LiveTerminalModes{}
		store.Surfaces[terminalID] = snapshot
	}
	if terminalID == "" || store.TerminalID == "" || store.TerminalID == terminalID {
		if terminalID != "" {
			store.TerminalID = terminalID
		}
		store.State = TerminalLiveExited
		store.ExitCode = exitCode
		store.ExitReason = reason
		store.Err = ""
		store.ResizeBoundary = LiveResizeBoundary{}
		store.Cursor = LiveCursor{}
		store.Modes = LiveTerminalModes{}
	}
	return store
}

func (store TerminalSurfaceStore) SetError(err string) TerminalSurfaceStore {
	store.Err = err
	store.State = TerminalLiveError
	store.ResizeBoundary = LiveResizeBoundary{}
	if store.TerminalID != "" {
		store.Surfaces = cloneLiveSurfaceSnapshots(store.Surfaces)
		snapshot, ok := store.snapshotForTerminal(store.TerminalID)
		if !ok {
			snapshot = LiveSurfaceSnapshot{}
		}
		snapshot.TerminalID = store.TerminalID
		snapshot.Err = err
		snapshot.State = TerminalLiveError
		store.Surfaces[store.TerminalID] = snapshot
	}
	return store
}

func (store TerminalSurfaceStore) Resize(cols int, rows int) TerminalSurfaceStore {
	previousCols := store.Cols
	previousRows := store.Rows
	store.Cols = cols
	store.Rows = rows
	if previousCols != cols || previousRows != rows {
		store.ResizeBoundary = LiveResizeBoundary{Active: true, PreviousCols: previousCols, PreviousRows: previousRows, Cols: cols, Rows: rows}
	}
	if store.TerminalID != "" {
		store.Surfaces = cloneLiveSurfaceSnapshots(store.Surfaces)
		snapshot, ok := store.snapshotForTerminal(store.TerminalID)
		if !ok {
			snapshot = LiveSurfaceSnapshot{}
		}
		snapshot.TerminalID = store.TerminalID
		snapshot.Cols = cols
		snapshot.Rows = rows
		if snapshot.State == "" {
			snapshot.State = TerminalLiveAttached
		}
		store.Surfaces[store.TerminalID] = snapshot
	}
	return store
}

func (store TerminalSurfaceStore) SurfaceForTerminal(terminalID string) TerminalSurfaceStore {
	if terminalID == "" || terminalID == store.TerminalID {
		return store
	}
	snapshot, ok := store.Surfaces[terminalID]
	if !ok {
		return TerminalSurfaceStore{TerminalID: terminalID, State: TerminalLivePending}
	}
	return (TerminalSurfaceStore{}).projectSnapshot(snapshot, snapshot.State != TerminalLivePending)
}

func (store TerminalSurfaceStore) projectSnapshot(snapshot LiveSurfaceSnapshot, ready bool) TerminalSurfaceStore {
	store.TerminalID = snapshot.TerminalID
	store.Revision = snapshot.Revision
	store.Cols = snapshot.Cols
	store.Rows = snapshot.Rows
	store.Lines = cloneStrings(snapshot.Lines)
	store.Screen = cloneLiveCellRows(snapshot.Screen)
	store.Title = snapshot.Title
	store.Cursor = snapshot.Cursor
	store.Modes = snapshot.Modes
	store.Ready = ready
	store.State = snapshot.State
	if store.State == "" {
		store.State = TerminalLiveAttached
	}
	store.ExitCode = snapshot.ExitCode
	store.ExitReason = snapshot.ExitReason
	store.Err = snapshot.Err
	return store
}

func (store TerminalSurfaceStore) resizeBoundaryRejects(snapshot LiveSurfaceSnapshot) bool {
	if !store.ResizeBoundary.Active || store.TerminalID == "" || snapshot.TerminalID != store.TerminalID || !liveSnapshotIsOrdinary(snapshot) {
		return false
	}
	// resize 后只拒绝明确来自旧尺寸基线的普通帧，避免晚到旧帧回滚投影。
	return snapshot.Cols == store.ResizeBoundary.PreviousCols && snapshot.Rows == store.ResizeBoundary.PreviousRows
}

func (store TerminalSurfaceStore) snapshotForTerminal(terminalID string) (LiveSurfaceSnapshot, bool) {
	if terminalID == "" {
		return LiveSurfaceSnapshot{}, false
	}
	if snapshot, ok := store.Surfaces[terminalID]; ok {
		return snapshot, true
	}
	if store.TerminalID != terminalID {
		return LiveSurfaceSnapshot{}, false
	}
	return LiveSurfaceSnapshot{
		TerminalID: terminalID,
		Revision:   store.Revision,
		Cols:       store.Cols,
		Rows:       store.Rows,
		Lines:      cloneStrings(store.Lines),
		Screen:     cloneLiveCellRows(store.Screen),
		Title:      store.Title,
		Cursor:     store.Cursor,
		Modes:      store.Modes,
		State:      store.State,
		ExitCode:   store.ExitCode,
		ExitReason: store.ExitReason,
		Err:        store.Err,
	}, true
}

func shouldRejectLiveSnapshot(current LiveSurfaceSnapshot, incoming LiveSurfaceSnapshot) bool {
	if incoming.Revision != 0 && current.Revision != 0 && incoming.Revision < current.Revision {
		return true
	}
	if liveSnapshotIsBoundary(current) && liveSnapshotIsOrdinary(incoming) {
		return true
	}
	return false
}

func liveSnapshotIsBoundary(snapshot LiveSurfaceSnapshot) bool {
	return snapshot.State == TerminalLiveExited || snapshot.State == TerminalLiveError || snapshot.Err != ""
}

func liveSnapshotIsOrdinary(snapshot LiveSurfaceSnapshot) bool {
	if snapshot.Err != "" || snapshot.ExitCode != 0 || snapshot.ExitReason != "" {
		return false
	}
	return snapshot.State == "" || snapshot.State == TerminalLiveAttached
}

func cloneLiveCellRows(rows [][]LiveCell) [][]LiveCell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]LiveCell, len(rows))
	for i, row := range rows {
		if len(row) == 0 {
			continue
		}
		out[i] = make([]LiveCell, len(row))
		copy(out[i], row)
	}
	return out
}

func (store TerminalSessionStore) Attach(terminalID string, channel uint16, cols int, rows int) TerminalSessionStore {
	return store.AttachWithResizeOwner(terminalID, channel, cols, rows, "", "", "")
}

func (store TerminalSessionStore) AttachWithResizeOwner(terminalID string, channel uint16, cols int, rows int, resizePolicy string, surfaceID string, viewID string) TerminalSessionStore {
	store.TerminalID = terminalID
	store.Channel = channel
	store.Attached = true
	store.Cols = cols
	store.Rows = rows
	store.ResizePolicy = resizePolicy
	store.SurfaceID = surfaceID
	store.ViewID = viewID
	store.DesiredCols = cols
	store.DesiredRows = rows
	store.ResizeRequestSeq = 0
	store.ResizeConfirmedSeq = 0
	store.LastError = ""
	store.State = TerminalLiveAttached
	store.ExitCode = 0
	store.ExitReason = ""
	return store
}

func (store TerminalSessionStore) Resize(cols int, rows int) TerminalSessionStore {
	store.Cols = cols
	store.Rows = rows
	store.DesiredCols = cols
	store.DesiredRows = rows
	store.ResizeConfirmedSeq = store.ResizeRequestSeq
	store.LastError = ""
	store.State = TerminalLiveAttached
	return store
}

func (store TerminalSessionStore) RequestResize(cols int, rows int) TerminalSessionStore {
	store.ResizeRequestSeq++
	store.DesiredCols = cols
	store.DesiredRows = rows
	store.LastError = ""
	store.State = TerminalLiveAttached
	return store
}

func (store TerminalSessionStore) DesiredSize() (int, int) {
	if store.DesiredCols > 0 && store.DesiredRows > 0 {
		return store.DesiredCols, store.DesiredRows
	}
	return store.Cols, store.Rows
}

func (store TerminalSessionStore) ApplyResizeResult(seq uint64, cols int, rows int) (TerminalSessionStore, bool) {
	if store.IsStaleResizeResult(seq) {
		return store, false
	}
	store.Cols = cols
	store.Rows = rows
	store.DesiredCols = cols
	store.DesiredRows = rows
	if seq > store.ResizeConfirmedSeq {
		store.ResizeConfirmedSeq = seq
	}
	store.LastError = ""
	store.State = TerminalLiveAttached
	return store, true
}

func (store TerminalSessionStore) IsStaleResizeResult(seq uint64) bool {
	return seq != 0 && seq < store.ResizeRequestSeq
}

func (store TerminalSessionStore) SetError(err string) TerminalSessionStore {
	store.LastError = err
	store.State = TerminalLiveError
	store.Attached = false
	store.ResizePolicy = ""
	store.SurfaceID = ""
	store.ViewID = ""
	return store
}

func (store TerminalSessionStore) MarkExited(terminalID string, exitCode int, reason string) TerminalSessionStore {
	if terminalID != "" {
		store.TerminalID = terminalID
	}
	store.Attached = false
	store.State = TerminalLiveExited
	store.ExitCode = exitCode
	store.ExitReason = reason
	store.LastError = ""
	store.ResizePolicy = ""
	store.SurfaceID = ""
	store.ViewID = ""
	return store
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func cloneLiveSurfaceSnapshots(values map[string]LiveSurfaceSnapshot) map[string]LiveSurfaceSnapshot {
	cloned := make(map[string]LiveSurfaceSnapshot, len(values)+1)
	for key, value := range values {
		value.Lines = cloneStrings(value.Lines)
		value.Screen = cloneLiveCellRows(value.Screen)
		cloned[key] = value
	}
	return cloned
}
