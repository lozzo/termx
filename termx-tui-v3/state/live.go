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
	TerminalID string
	Cols       int
	Rows       int
	Lines      []string
	Screen     [][]LiveCell
	Title      string
	Cursor     LiveCursor
	Ready      bool
	State      TerminalLiveState
	ExitCode   int
	ExitReason string
	Err        string
	Surfaces   map[string]LiveSurfaceSnapshot
}

// LiveCursor 是 live surface 的 content-local 光标状态。
type LiveCursor struct {
	Visible bool
	Row     int
	Col     int
	Shape   string
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
	Cols       int
	Rows       int
	Lines      []string
	Screen     [][]LiveCell
	Title      string
	Cursor     LiveCursor
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
	snapshot.Lines = cloneStrings(snapshot.Lines)
	snapshot.Screen = cloneLiveCellRows(snapshot.Screen)
	if snapshot.State == "" {
		snapshot.State = TerminalLiveAttached
	}
	store.Surfaces = cloneLiveSurfaceSnapshots(store.Surfaces)
	store.Surfaces[snapshot.TerminalID] = snapshot
	if store.TerminalID == "" || store.TerminalID == snapshot.TerminalID {
		return store.projectSnapshot(snapshot, true)
	}
	return store
}

func (store TerminalSurfaceStore) Attach(terminalID string, cols int, rows int) TerminalSurfaceStore {
	if store.TerminalID != "" && store.TerminalID != terminalID {
		store.Lines = nil
		store.Screen = nil
		store.Cursor = LiveCursor{}
		store.Ready = false
	}
	store.TerminalID = terminalID
	store.Cols = cols
	store.Rows = rows
	store.State = TerminalLiveAttached
	store.ExitCode = 0
	store.ExitReason = ""
	store.Err = ""
	if terminalID != "" {
		store.Surfaces = cloneLiveSurfaceSnapshots(store.Surfaces)
		snapshot := store.Surfaces[terminalID]
		snapshot.TerminalID = terminalID
		if snapshot.Cols == 0 {
			snapshot.Cols = cols
		}
		if snapshot.Rows == 0 {
			snapshot.Rows = rows
		}
		if snapshot.State == "" {
			snapshot.State = TerminalLiveAttached
		}
		store.Surfaces[terminalID] = snapshot
	}
	return store
}

func (store TerminalSurfaceStore) MarkExited(terminalID string, exitCode int, reason string) TerminalSurfaceStore {
	if terminalID != "" {
		store.Surfaces = cloneLiveSurfaceSnapshots(store.Surfaces)
		snapshot := store.Surfaces[terminalID]
		snapshot.TerminalID = terminalID
		snapshot.State = TerminalLiveExited
		snapshot.ExitCode = exitCode
		snapshot.ExitReason = reason
		snapshot.Err = ""
		snapshot.Cursor = LiveCursor{}
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
		store.Cursor = LiveCursor{}
	}
	return store
}

func (store TerminalSurfaceStore) SetError(err string) TerminalSurfaceStore {
	store.Err = err
	store.State = TerminalLiveError
	if store.TerminalID != "" {
		store.Surfaces = cloneLiveSurfaceSnapshots(store.Surfaces)
		snapshot := store.Surfaces[store.TerminalID]
		snapshot.TerminalID = store.TerminalID
		snapshot.Err = err
		snapshot.State = TerminalLiveError
		store.Surfaces[store.TerminalID] = snapshot
	}
	return store
}

func (store TerminalSurfaceStore) Resize(cols int, rows int) TerminalSurfaceStore {
	store.Cols = cols
	store.Rows = rows
	if store.TerminalID != "" {
		store.Surfaces = cloneLiveSurfaceSnapshots(store.Surfaces)
		snapshot := store.Surfaces[store.TerminalID]
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
	store.Cols = snapshot.Cols
	store.Rows = snapshot.Rows
	store.Lines = cloneStrings(snapshot.Lines)
	store.Screen = cloneLiveCellRows(snapshot.Screen)
	store.Title = snapshot.Title
	store.Cursor = snapshot.Cursor
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
		cloned[key] = value
	}
	return cloned
}
