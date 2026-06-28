package state

import "time"

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
	ExitedAt       time.Time
	Command        []string
	Err            string
	ResizeBoundary LiveResizeBoundary
	Surfaces       map[string]LiveSurfaceSnapshot
	RenderRequests map[string]LiveRenderRequestState
}

// LiveResizeBoundary 是一次 content rect resize 后等待匹配 surface 的基线。
type LiveResizeBoundary struct {
	Active       bool
	PreviousCols int
	PreviousRows int
	Cols         int
	Rows         int
}

// LiveRenderRequestState 是 TUI render loop 对某个 terminal 的 latest native screen 请求状态。
// 它只归当前 TUI 客户端所有：core 不知道该状态，history/copy 也不能读取它作为 truth。
type LiveRenderRequestState struct {
	WantedRevision   uint64
	CurrentRevision  uint64
	InFlight         bool
	InFlightRevision uint64
	Dirty            bool
	Cols             int
	Rows             int
}

// LiveRenderFetchRequest 是 reducer 决定要异步拉取 core latest native screen 的请求。
// 它只携带 terminal identity 和当前 view size，不携带历史 token 或 frame payload。
type LiveRenderFetchRequest struct {
	TerminalID string
	Cols       int
	Rows       int
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
	MouseTracking  bool
	MouseX10       bool
	MouseNormal    bool
	MouseButton    bool
	MouseAny       bool
	MouseSGR       bool
	BracketedPaste bool
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
	TerminalID string
	Channel    uint16
	// InputChannels 只保存已 attach terminal 的输入 channel，用于 pane focus 后把输入发回对应 terminal。
	InputChannels map[string]uint16
	Attached      bool
	Cols          int
	Rows          int
	ResizePolicy  string
	SurfaceID     string
	ViewID        string
	// Desired* 只记录已发出的 terminal resize 目标，用于连续 pane command 去重和 stale response guard。
	DesiredCols        int
	DesiredRows        int
	ResizeRequestSeq   uint64
	ResizeConfirmedSeq uint64
	LastError          string
	State              TerminalLiveState
	ExitCode           int
	ExitReason         string
	ExitedAt           time.Time
	Command            []string
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
	ExitedAt   time.Time
	Command    []string
	Err        string
}

func (store TerminalSurfaceStore) ApplySnapshot(snapshot LiveSurfaceSnapshot) TerminalSurfaceStore {
	return store.ApplySnapshotWithLifecycle(snapshot, false)
}

// ApplySnapshotWithLifecycle 只供 reducer 处理一次 core lifecycle 消息；lifecycleKnown 不写入 snapshot/store。
func (store TerminalSurfaceStore) ApplySnapshotWithLifecycle(snapshot LiveSurfaceSnapshot, lifecycleKnown bool) TerminalSurfaceStore {
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
		store = store.finishLiveRenderFetch(snapshot.TerminalID, snapshot.Revision, false)
		return store
	}
	if current, ok := store.snapshotForTerminal(snapshot.TerminalID); ok {
		if shouldRejectLiveSnapshotWithLifecycle(current, snapshot, lifecycleKnown) {
			store = store.finishLiveRenderFetch(snapshot.TerminalID, snapshot.Revision, false)
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
	store = store.finishLiveRenderFetch(snapshot.TerminalID, snapshot.Revision, true)
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
	store.ExitedAt = time.Time{}
	store.Command = nil
	store.Err = ""
	store.ResizeBoundary = LiveResizeBoundary{}
	if terminalID != "" {
		store.Surfaces = cloneLiveSurfaceSnapshots(store.Surfaces)
		snapshot, ok := store.snapshotForTerminal(terminalID)
		if !ok {
			snapshot = LiveSurfaceSnapshot{}
		}
		wasBoundary := liveSnapshotIsBoundary(snapshot)
		snapshot.TerminalID = terminalID
		if wasBoundary {
			snapshot.Revision = 0
		}
		if snapshot.Cols == 0 {
			snapshot.Cols = cols
		}
		if snapshot.Rows == 0 {
			snapshot.Rows = rows
		}
		snapshot.State = TerminalLiveAttached
		snapshot.ExitCode = 0
		snapshot.ExitReason = ""
		snapshot.ExitedAt = time.Time{}
		snapshot.Command = nil
		snapshot.Err = ""
		store.Surfaces[terminalID] = snapshot
		if len(snapshot.Lines) > 0 || len(snapshot.Screen) > 0 || snapshot.Title != "" || snapshot.Cursor.Visible {
			store = store.projectSnapshot(snapshot, true)
		}
	}
	return store
}

func (store TerminalSurfaceStore) RestartPreservingContent(terminalID string, cols int, rows int) TerminalSurfaceStore {
	if terminalID == "" {
		return store
	}
	snapshot, ok := store.snapshotForTerminal(terminalID)
	if !ok {
		return store.Attach(terminalID, cols, rows)
	}
	// 中文说明：restart 只重启 terminal process，不能把同 terminal 的 live tail 清空。
	// lifecycle 元数据清掉后，真实 channel/surface 仍等待 per-view reattach 回投。
	if snapshot.Cols == 0 {
		snapshot.Cols = cols
	}
	if snapshot.Rows == 0 {
		snapshot.Rows = rows
	}
	snapshot.State = TerminalLiveAttached
	snapshot.ExitCode = 0
	snapshot.ExitReason = ""
	snapshot.ExitedAt = time.Time{}
	snapshot.Command = nil
	snapshot.Err = ""
	snapshot.Cursor = LiveCursor{}
	snapshot.Modes = LiveTerminalModes{}
	store.Surfaces = cloneLiveSurfaceSnapshots(store.Surfaces)
	store.Surfaces[terminalID] = snapshot
	if store.TerminalID != "" && store.TerminalID != terminalID {
		store.ResizeBoundary = LiveResizeBoundary{}
		return store
	}
	store = store.projectSnapshot(snapshot, liveSnapshotHasContent(snapshot))
	store.ResizeBoundary = LiveResizeBoundary{}
	return store
}

func (store TerminalSurfaceStore) MarkAttached(terminalID string) TerminalSurfaceStore {
	if terminalID == "" {
		return store
	}
	snapshot, ok := store.snapshotForTerminal(terminalID)
	if ok {
		// 中文说明：只有 core live surface/event 明确返回 running 时才调用这里；
		// terminal pool/list 不能用它把 running 写进 live 投影缓存。
		snapshot.State = TerminalLiveAttached
		snapshot.ExitCode = 0
		snapshot.ExitReason = ""
		snapshot.ExitedAt = time.Time{}
		snapshot.Command = nil
		snapshot.Err = ""
		store.Surfaces = cloneLiveSurfaceSnapshots(store.Surfaces)
		store.Surfaces[terminalID] = snapshot
	}
	if store.TerminalID != terminalID {
		return store
	}
	if ok {
		store = store.projectSnapshot(snapshot, store.Ready || liveSnapshotHasContent(snapshot))
	} else {
		store.State = TerminalLiveAttached
		store.ExitCode = 0
		store.ExitReason = ""
		store.ExitedAt = time.Time{}
		store.Command = nil
		store.Err = ""
	}
	store.ResizeBoundary = LiveResizeBoundary{}
	return store
}

func (store TerminalSurfaceStore) MarkExited(terminalID string, exitCode int, reason string) TerminalSurfaceStore {
	return store.MarkExitedWithMetadata(terminalID, exitCode, reason, time.Time{}, nil)
}

func (store TerminalSurfaceStore) MarkExitedWithMetadata(terminalID string, exitCode int, reason string, exitedAt time.Time, command []string) TerminalSurfaceStore {
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
		snapshot.ExitedAt = exitedAt
		snapshot.Command = cloneStrings(command)
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
		store.ExitedAt = exitedAt
		store.Command = cloneStrings(command)
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

func (store TerminalSurfaceStore) Snapshot() LiveSurfaceSnapshot {
	return LiveSurfaceSnapshot{
		TerminalID: store.TerminalID,
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
		ExitedAt:   store.ExitedAt,
		Command:    cloneStrings(store.Command),
		Err:        store.Err,
	}
}

func (store TerminalSurfaceStore) RemoveTerminal(terminalID string) TerminalSurfaceStore {
	if terminalID == "" {
		return store
	}
	if len(store.Surfaces) > 0 {
		store.Surfaces = cloneLiveSurfaceSnapshots(store.Surfaces)
		delete(store.Surfaces, terminalID)
	}
	if len(store.RenderRequests) > 0 {
		store.RenderRequests = cloneLiveRenderRequestStates(store.RenderRequests)
		delete(store.RenderRequests, terminalID)
	}
	if store.TerminalID != terminalID {
		return store
	}
	store.TerminalID = ""
	store.Revision = 0
	store.Cols = 0
	store.Rows = 0
	store.Lines = nil
	store.Screen = nil
	store.Title = ""
	store.Cursor = LiveCursor{}
	store.Modes = LiveTerminalModes{}
	store.Ready = false
	store.State = ""
	store.ExitCode = 0
	store.ExitReason = ""
	store.ExitedAt = time.Time{}
	store.Command = nil
	store.Err = ""
	store.ResizeBoundary = LiveResizeBoundary{}
	return store
}

func (store TerminalSurfaceStore) RequestLiveRender(terminalID string, revision uint64, cols int, rows int) (TerminalSurfaceStore, bool) {
	if terminalID == "" {
		return store, false
	}
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	request := store.RenderRequests[terminalID]
	if revision > request.WantedRevision {
		request.WantedRevision = revision
	}
	if request.CurrentRevision == 0 {
		if snapshot, ok := store.snapshotForTerminal(terminalID); ok {
			request.CurrentRevision = snapshot.Revision
		}
	}
	if revision != 0 && revision <= request.CurrentRevision {
		if !request.InFlight && !request.Dirty && request.WantedRevision <= request.CurrentRevision {
			return store, false
		}
	}
	request.Cols = cols
	request.Rows = rows
	if request.InFlight {
		request.Dirty = true
		store.RenderRequests = cloneLiveRenderRequestStates(store.RenderRequests)
		store.RenderRequests[terminalID] = request
		return store, false
	}
	request.InFlight = true
	request.InFlightRevision = request.WantedRevision
	request.Dirty = false
	store.RenderRequests = cloneLiveRenderRequestStates(store.RenderRequests)
	store.RenderRequests[terminalID] = request
	return store, true
}

func (store TerminalSurfaceStore) FailLiveRenderFetch(terminalID string) TerminalSurfaceStore {
	if terminalID == "" {
		return store
	}
	request, ok := store.RenderRequests[terminalID]
	if !ok {
		return store
	}
	request.InFlight = false
	request.InFlightRevision = 0
	store.RenderRequests = cloneLiveRenderRequestStates(store.RenderRequests)
	if request.Dirty || request.WantedRevision > request.CurrentRevision {
		request.Dirty = true
		store.RenderRequests[terminalID] = request
		return store
	}
	delete(store.RenderRequests, terminalID)
	return store
}

func (store TerminalSurfaceStore) LiveFrameRendered() (TerminalSurfaceStore, []LiveRenderFetchRequest) {
	if len(store.RenderRequests) == 0 {
		return store, nil
	}
	store.RenderRequests = cloneLiveRenderRequestStates(store.RenderRequests)
	requests := make([]LiveRenderFetchRequest, 0, len(store.RenderRequests))
	for terminalID, request := range store.RenderRequests {
		if terminalID == "" || request.InFlight || (!request.Dirty && request.WantedRevision <= request.CurrentRevision) {
			continue
		}
		request.InFlight = true
		request.InFlightRevision = request.WantedRevision
		request.Dirty = false
		if request.Cols <= 0 {
			request.Cols = 80
		}
		if request.Rows <= 0 {
			request.Rows = 24
		}
		store.RenderRequests[terminalID] = request
		requests = append(requests, LiveRenderFetchRequest{TerminalID: terminalID, Cols: request.Cols, Rows: request.Rows})
	}
	return store, requests
}

func (store TerminalSurfaceStore) finishLiveRenderFetch(terminalID string, revision uint64, accepted bool) TerminalSurfaceStore {
	if terminalID == "" {
		return store
	}
	request, ok := store.RenderRequests[terminalID]
	if !ok {
		return store
	}
	request.InFlight = false
	request.InFlightRevision = 0
	if accepted && revision > request.CurrentRevision {
		request.CurrentRevision = revision
	}
	if accepted && revision != 0 && request.WantedRevision <= request.CurrentRevision {
		request.Dirty = false
	}
	if !accepted || request.Dirty || request.WantedRevision > request.CurrentRevision {
		request.Dirty = true
		store.RenderRequests = cloneLiveRenderRequestStates(store.RenderRequests)
		store.RenderRequests[terminalID] = request
		return store
	}
	store.RenderRequests = cloneLiveRenderRequestStates(store.RenderRequests)
	delete(store.RenderRequests, terminalID)
	return store
}

func (store TerminalSessionStore) RemoveTerminal(terminalID string) TerminalSessionStore {
	if terminalID == "" {
		return store
	}
	if len(store.InputChannels) > 0 {
		store.InputChannels = cloneInputChannels(store.InputChannels)
		delete(store.InputChannels, terminalID)
	}
	if store.TerminalID != terminalID {
		return store
	}
	store.TerminalID = ""
	store.Channel = 0
	store.Attached = false
	store.ResizePolicy = ""
	store.SurfaceID = ""
	store.ViewID = ""
	store.DesiredCols = 0
	store.DesiredRows = 0
	store.ResizeRequestSeq = 0
	store.ResizeConfirmedSeq = 0
	store.LastError = ""
	store.State = ""
	store.ExitCode = 0
	store.ExitReason = ""
	store.ExitedAt = time.Time{}
	store.Command = nil
	return store
}

func (store TerminalSessionStore) ClearInputChannel(terminalID string) TerminalSessionStore {
	if terminalID == "" {
		return store
	}
	if len(store.InputChannels) > 0 {
		store.InputChannels = cloneInputChannels(store.InputChannels)
		delete(store.InputChannels, terminalID)
	}
	if store.TerminalID == terminalID {
		store.Channel = 0
		store.Attached = false
		store.State = TerminalLivePending
		store.ExitCode = 0
		store.ExitReason = ""
		store.ExitedAt = time.Time{}
		store.Command = nil
		store.LastError = ""
	}
	return store
}

func (store TerminalSessionStore) MarkAttached(terminalID string) TerminalSessionStore {
	if terminalID == "" || store.TerminalID != terminalID {
		return store
	}
	if store.State == TerminalLiveError && !store.Attached {
		return store
	}
	store.ExitCode = 0
	store.ExitReason = ""
	store.ExitedAt = time.Time{}
	store.Command = nil
	store.LastError = ""
	if store.Attached {
		store.State = TerminalLiveAttached
		return store
	}
	store.State = TerminalLivePending
	return store
}

func (store TerminalSurfaceStore) projectSnapshot(snapshot LiveSurfaceSnapshot, ready bool) TerminalSurfaceStore {
	store.TerminalID = snapshot.TerminalID
	store.Revision = snapshot.Revision
	store.Cols = snapshot.Cols
	store.Rows = snapshot.Rows
	// 中文说明：写入 store 前已经克隆成 reducer-owned payload；active projection
	// 和 Surfaces map 共享同一份不可变帧，避免每个 live frame 再深拷贝一次。
	store.Lines = snapshot.Lines
	store.Screen = snapshot.Screen
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
	store.ExitedAt = snapshot.ExitedAt
	store.Command = cloneStrings(snapshot.Command)
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
		ExitedAt:   store.ExitedAt,
		Command:    cloneStrings(store.Command),
		Err:        store.Err,
	}, true
}

func shouldRejectLiveSnapshot(current LiveSurfaceSnapshot, incoming LiveSurfaceSnapshot) bool {
	return shouldRejectLiveSnapshotWithLifecycle(current, incoming, false)
}

func shouldRejectLiveSnapshotWithLifecycle(current LiveSurfaceSnapshot, incoming LiveSurfaceSnapshot, lifecycleKnown bool) bool {
	if incoming.Revision != 0 && current.Revision != 0 && incoming.Revision < current.Revision && !lifecycleKnown {
		return true
	}
	if incoming.Revision == 0 && liveSnapshotHasContent(current) && !liveSnapshotHasContent(incoming) && !lifecycleKnown {
		return true
	}
	if liveSnapshotIsBoundary(current) && liveSnapshotIsOrdinary(incoming) && !lifecycleKnown {
		return true
	}
	return false
}

func liveSnapshotHasContent(snapshot LiveSurfaceSnapshot) bool {
	return len(snapshot.Lines) > 0 || len(snapshot.Screen) > 0 || snapshot.Title != "" || snapshot.Cursor.Visible
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
	store.InputChannels = cloneInputChannels(store.InputChannels)
	if terminalID != "" && channel != 0 {
		store.InputChannels[terminalID] = channel
	}
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
	store.ExitedAt = time.Time{}
	store.Command = nil
	return store
}

func (store TerminalSessionStore) InputChannelFor(terminalID string) (uint16, bool) {
	if terminalID == "" {
		return 0, false
	}
	if terminalID == store.TerminalID && store.Channel != 0 {
		return store.Channel, true
	}
	channel, ok := store.InputChannels[terminalID]
	return channel, ok && channel != 0
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
	return store.MarkExitedWithMetadata(terminalID, exitCode, reason, time.Time{}, nil)
}

func (store TerminalSessionStore) MarkExitedWithMetadata(terminalID string, exitCode int, reason string, exitedAt time.Time, command []string) TerminalSessionStore {
	if terminalID != "" {
		store.TerminalID = terminalID
	}
	store.Attached = false
	store.State = TerminalLiveExited
	store.ExitCode = exitCode
	store.ExitReason = reason
	store.ExitedAt = exitedAt
	store.Command = cloneStrings(command)
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
		// 中文说明：Surfaces map 只需要结构级 COW；每个 snapshot 在写入 map 前已经
		// 克隆成 reducer-owned payload，更新其它 terminal 时不能重复深拷贝旧 screen。
		cloned[key] = value
	}
	return cloned
}

func cloneLiveRenderRequestStates(values map[string]LiveRenderRequestState) map[string]LiveRenderRequestState {
	cloned := make(map[string]LiveRenderRequestState, len(values)+1)
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func CloneLiveSurfaceSnapshot(value LiveSurfaceSnapshot) LiveSurfaceSnapshot {
	value.Lines = cloneStrings(value.Lines)
	value.Screen = cloneLiveCellRows(value.Screen)
	value.Command = cloneStrings(value.Command)
	return value
}

func cloneLiveSurfaceSnapshotPtr(value LiveSurfaceSnapshot) *LiveSurfaceSnapshot {
	if value.TerminalID == "" && len(value.Lines) == 0 && len(value.Screen) == 0 && value.Title == "" && !value.Cursor.Visible {
		return nil
	}
	cloned := CloneLiveSurfaceSnapshot(value)
	return &cloned
}

func cloneInputChannels(values map[string]uint16) map[string]uint16 {
	cloned := make(map[string]uint16, len(values)+1)
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
