package state

// TerminalSurfaceStore 保存当前实时 terminal surface 投影，不是历史 truth。
type TerminalSurfaceStore struct {
	TerminalID string
	Cols       int
	Rows       int
	Lines      []string
	Title      string
	Cursor     LiveCursor
	Ready      bool
	Err        string
}

// LiveCursor 是 live surface 的 content-local 光标状态。
type LiveCursor struct {
	Visible bool
	Row     int
	Col     int
	Shape   string
}

// TerminalSessionStore 保存当前 attach/live path 的 reducer-owned 会话状态。
type TerminalSessionStore struct {
	TerminalID string
	Channel    uint16
	Attached   bool
	Cols       int
	Rows       int
	// Desired* 只记录已发出的 terminal resize 目标，用于连续 pane command 去重和 stale response guard。
	DesiredCols        int
	DesiredRows        int
	ResizeRequestSeq   uint64
	ResizeConfirmedSeq uint64
	LastError          string
}

// LiveSurfaceSnapshot 是 terminal service/event 回投给 reducer 的实时投影。
type LiveSurfaceSnapshot struct {
	TerminalID string
	Cols       int
	Rows       int
	Lines      []string
	Title      string
	Cursor     LiveCursor
}

func (store TerminalSurfaceStore) ApplySnapshot(snapshot LiveSurfaceSnapshot) TerminalSurfaceStore {
	store.TerminalID = snapshot.TerminalID
	store.Cols = snapshot.Cols
	store.Rows = snapshot.Rows
	store.Lines = cloneStrings(snapshot.Lines)
	store.Title = snapshot.Title
	store.Cursor = snapshot.Cursor
	store.Ready = true
	store.Err = ""
	return store
}

func (store TerminalSurfaceStore) SetError(err string) TerminalSurfaceStore {
	store.Err = err
	return store
}

func (store TerminalSurfaceStore) Resize(cols int, rows int) TerminalSurfaceStore {
	store.Cols = cols
	store.Rows = rows
	return store
}

func (store TerminalSessionStore) Attach(terminalID string, channel uint16, cols int, rows int) TerminalSessionStore {
	store.TerminalID = terminalID
	store.Channel = channel
	store.Attached = true
	store.Cols = cols
	store.Rows = rows
	store.DesiredCols = cols
	store.DesiredRows = rows
	store.ResizeRequestSeq = 0
	store.ResizeConfirmedSeq = 0
	store.LastError = ""
	return store
}

func (store TerminalSessionStore) Resize(cols int, rows int) TerminalSessionStore {
	store.Cols = cols
	store.Rows = rows
	store.DesiredCols = cols
	store.DesiredRows = rows
	store.ResizeConfirmedSeq = store.ResizeRequestSeq
	store.LastError = ""
	return store
}

func (store TerminalSessionStore) RequestResize(cols int, rows int) TerminalSessionStore {
	store.ResizeRequestSeq++
	store.DesiredCols = cols
	store.DesiredRows = rows
	store.LastError = ""
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
	return store, true
}

func (store TerminalSessionStore) IsStaleResizeResult(seq uint64) bool {
	return seq != 0 && seq < store.ResizeRequestSeq
}

func (store TerminalSessionStore) SetError(err string) TerminalSessionStore {
	store.LastError = err
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
