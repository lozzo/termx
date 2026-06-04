package state

// TerminalSurfaceStore 保存当前实时 terminal surface 投影，不是历史 truth。
type TerminalSurfaceStore struct {
	TerminalID string
	Cols       int
	Rows       int
	Lines      []string
	Title      string
	Err        string
}

// TerminalSessionStore 保存当前 attach/live path 的 reducer-owned 会话状态。
type TerminalSessionStore struct {
	TerminalID string
	Channel    uint16
	Attached   bool
	Cols       int
	Rows       int
	LastError  string
}

// LiveSurfaceSnapshot 是 terminal service/event 回投给 reducer 的实时投影。
type LiveSurfaceSnapshot struct {
	TerminalID string
	Cols       int
	Rows       int
	Lines      []string
	Title      string
}

func (store TerminalSurfaceStore) ApplySnapshot(snapshot LiveSurfaceSnapshot) TerminalSurfaceStore {
	store.TerminalID = snapshot.TerminalID
	store.Cols = snapshot.Cols
	store.Rows = snapshot.Rows
	store.Lines = cloneStrings(snapshot.Lines)
	store.Title = snapshot.Title
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
	store.LastError = ""
	return store
}

func (store TerminalSessionStore) Resize(cols int, rows int) TerminalSessionStore {
	store.Cols = cols
	store.Rows = rows
	store.LastError = ""
	return store
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
