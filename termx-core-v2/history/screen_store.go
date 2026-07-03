package history

import (
	"fmt"
	"strings"
	"time"
)

// ScreenBackedHistoryStore 是基于 ScreenHistoryBuffer projection 的 HistoryStore。
// domain owner 是 core-v2 history；正文 truth 来自 ScreenHistoryBuffer physical
// rows/cells，store 只负责把 projection 暴露成既有 HistoryWindow/Copy/Freeze
// 合同，不解释 terminal ops，也不从 TUI/live rows 推断历史。
type ScreenBackedHistoryStore struct {
	terminalID string
	buffer     *ScreenHistoryBuffer
	nextToken  uint64
	frozen     map[HistoryToken]screenBackedFrozenProjection
}

type screenBackedFrozenProjection struct {
	snapshot FrozenHistorySnapshot
	rows     []HistoryRow
}

// NewScreenBackedHistoryStore 创建 screen-backed HistoryStore。
// 调用方必须传入同一 terminal 的 ScreenHistoryBuffer；若 buffer 为 nil，则创建
// 空 buffer。Apply 在该 store 中不是写 truth 的入口，truth 写入发生在 screen buffer。
func NewScreenBackedHistoryStore(terminalID string, buffer *ScreenHistoryBuffer) *ScreenBackedHistoryStore {
	if buffer == nil {
		buffer = NewScreenHistoryBuffer(80, 24)
	}
	return &ScreenBackedHistoryStore{
		terminalID: terminalID,
		buffer:     buffer,
		frozen:     make(map[HistoryToken]screenBackedFrozenProjection),
	}
}

// Apply 保持 HistoryStore 接口兼容，但不消费 logical/frame mutations。
// screen-backed store 的写入边界是 ScreenHistoryBuffer physical row mutation；
// 若在 R426 前仍收到旧 renderer mutation，这里只能忽略，不能把旧 mutation
// 恢复成第二份正文 truth。
func (store *ScreenBackedHistoryStore) Apply(batch HistoryMutationBatch) error {
	return nil
}

// ReadState 返回 classifier 所需的只读 screen projection 边界。
func (store *ScreenBackedHistoryStore) ReadState() HistoryReadState {
	projection := store.liveProjection()
	state := HistoryReadState{
		Generation:  projection.Generation,
		InAltScreen: projection.InAlt,
	}
	for _, row := range projection.Rows {
		switch row.Segment {
		case HistorySegmentCommitted, HistorySegmentArchivedPrimaryFrame:
			state.HasTimeline = true
		case HistorySegmentCurrentPrimaryFrame:
			if row.OwnerKind == RowOwnerPrimaryFrame {
				// 中文说明：screen-backed projection 会把当前 main screen 暴露在
				// current-primary-frame segment；classifier 的 HasPrimaryCurrent 只表示
				// primary screen app frame ownership，普通 shell current row 不能置位。
				state.HasPrimaryCurrent = true
			}
		case HistorySegmentCurrentAltFrame:
			state.HasAltCurrent = true
		}
	}
	return state
}

// LatestWindow 返回 screen-backed latest projection。
func (store *ScreenBackedHistoryStore) LatestWindow(req HistoryWindowRequest) (HistoryWindow, error) {
	req = store.normalizeWindowRequest(req)
	if req.Token != "" {
		rows, snapshot, ok := store.frozenRows(req.Token)
		if !ok {
			return HistoryWindow{}, ErrHistoryInvalidMutation
		}
		return screenBackedLatestWindow(req, rows, snapshot.Generation), nil
	}
	projection := store.liveProjectionForRequest(req)
	return projection.LatestWindow(store.normalizeWindowRequest(req)), nil
}

// OlderWindow 返回 screen-backed older/prepend projection。
func (store *ScreenBackedHistoryStore) OlderWindow(req HistoryWindowRequest) (HistoryWindow, error) {
	req = store.normalizeWindowRequest(req)
	rows, generation, err := store.rowsForWindowRequest(req)
	if err != nil {
		return HistoryWindow{}, err
	}
	if !req.Cursor.Valid {
		return screenBackedWindow(req, nil, len(rows), HistoryWindowPrepend, HistoryBoundary{Cursor: HistoryCursor{Generation: generation, Token: req.Token}}, generation, false), nil
	}
	limit := normalizedLimit(req.Limit)
	end := req.Cursor.BeforeRowIndex
	if end <= 0 || end > len(rows) {
		end = len(rows)
	}
	start := maxInt(0, end-limit)
	page := cloneHistoryRows(rows[start:end])
	annotateProjectionRowIndexes(page, start)
	boundary := boundaryForRows(page, generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = cursorBeforeIndex(page[0], start, generation, req.Token, start > 0)
	}
	return screenBackedWindow(req, page, len(rows), HistoryWindowPrepend, boundary, generation, start > 0), nil
}

// OldestWindow 返回 screen-backed projection head。
func (store *ScreenBackedHistoryStore) OldestWindow(req HistoryWindowRequest) (HistoryWindow, error) {
	req = store.normalizeWindowRequest(req)
	rows, generation, err := store.rowsForWindowRequest(req)
	if err != nil {
		return HistoryWindow{}, err
	}
	limit := normalizedLimit(req.Limit)
	end := minInt(len(rows), limit)
	page := cloneHistoryRows(rows[:end])
	annotateProjectionRowIndexes(page, 0)
	boundary := boundaryForRows(page, generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = cursorBeforeIndex(page[len(page)-1], end, generation, req.Token, end < len(rows))
	}
	return screenBackedWindow(req, page, len(rows), HistoryWindowReplace, boundary, generation, end < len(rows)), nil
}

// NewerWindow 返回 screen-backed newer/append projection。
func (store *ScreenBackedHistoryStore) NewerWindow(req HistoryWindowRequest) (HistoryWindow, error) {
	req = store.normalizeWindowRequest(req)
	rows, generation, err := store.rowsForWindowRequest(req)
	if err != nil {
		return HistoryWindow{}, err
	}
	if !req.Cursor.Valid {
		return screenBackedWindow(req, nil, len(rows), HistoryWindowAppend, HistoryBoundary{Cursor: HistoryCursor{Generation: generation, Token: req.Token}}, generation, false), nil
	}
	limit := normalizedLimit(req.Limit)
	start := clampInt(req.Cursor.BeforeRowIndex, 0, len(rows))
	end := minInt(len(rows), start+limit)
	page := cloneHistoryRows(rows[start:end])
	annotateProjectionRowIndexes(page, start)
	boundary := boundaryForRows(page, generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = cursorBeforeIndex(page[len(page)-1], end, generation, req.Token, end < len(rows))
	}
	return screenBackedWindow(req, page, len(rows), HistoryWindowAppend, boundary, generation, end < len(rows)), nil
}

// Freeze 保存当前 screen-backed projection boundary，后续 repaint 不会改写 token rows。
func (store *ScreenBackedHistoryStore) Freeze(req FreezeHistoryRequest) (FrozenHistorySnapshot, error) {
	if store == nil {
		return FrozenHistorySnapshot{}, nil
	}
	store.ensureState()
	store.nextToken++
	token := HistoryToken(fmt.Sprintf("screen-hist-%d", store.nextToken))
	projection := store.liveProjection()
	rows := projection.HistoryRows()
	windowReq := HistoryWindowRequest{TerminalID: req.TerminalID, Cols: req.Cols, Limit: req.Limit, Token: token}
	latest := screenBackedLatestWindow(windowReq, rows, projection.Generation)
	snapshot := FrozenHistorySnapshot{
		Token:                 token,
		TerminalID:            store.terminalIDFor(req.TerminalID),
		Cols:                  req.Cols,
		CommittedUpperBound:   lastSealedLineID(latest.Rows),
		FrozenFrontierLineIDs: mutableLineIDs(latest.Rows),
		Boundary:              latest.Boundary,
		Generation:            projection.Generation,
		CreatedAt:             time.Now().UTC(),
	}
	store.frozen[token] = screenBackedFrozenProjection{
		snapshot: snapshot,
		rows:     cloneHistoryRows(rows),
	}
	return snapshot, nil
}

// Copy 从 live 或 frozen screen-backed projection rows 复制文本。
func (store *ScreenBackedHistoryStore) Copy(req HistoryCopyRequest) (string, error) {
	rows, _, err := store.rowsForWindowRequest(HistoryWindowRequest{TerminalID: req.TerminalID, Cols: req.Cols, Token: req.Token})
	if err != nil {
		return "", err
	}
	selected := rowsBetweenCursors(rows, req.Start, req.End)
	texts := make([]string, 0, len(selected))
	for _, row := range selected {
		texts = append(texts, rowText(row.Cells))
	}
	return strings.Join(texts, "\n"), nil
}

// Release 释放 screen-backed frozen token。
func (store *ScreenBackedHistoryStore) Release(token HistoryToken) error {
	if store == nil || token == "" {
		return nil
	}
	delete(store.frozen, token)
	return nil
}

func (store *ScreenBackedHistoryStore) liveProjection() ScreenHistoryProjection {
	if store == nil || store.buffer == nil {
		return ScreenHistoryProjection{}
	}
	return store.buffer.ProjectHistory()
}

func (store *ScreenBackedHistoryStore) liveProjectionForRequest(req HistoryWindowRequest) ScreenHistoryProjection {
	projection := store.liveProjection()
	if req.Cols > 0 {
		projection.Cols = req.Cols
	}
	return projection
}

func (store *ScreenBackedHistoryStore) rowsForWindowRequest(req HistoryWindowRequest) ([]HistoryRow, Generation, error) {
	if req.Token != "" {
		rows, snapshot, ok := store.frozenRows(req.Token)
		if !ok {
			return nil, 0, ErrHistoryInvalidMutation
		}
		return rows, snapshot.Generation, nil
	}
	projection := store.liveProjectionForRequest(req)
	return projection.HistoryRows(), projection.Generation, nil
}

func (store *ScreenBackedHistoryStore) frozenRows(token HistoryToken) ([]HistoryRow, FrozenHistorySnapshot, bool) {
	if store == nil {
		return nil, FrozenHistorySnapshot{}, false
	}
	store.ensureState()
	frozen, ok := store.frozen[token]
	if !ok {
		return nil, FrozenHistorySnapshot{}, false
	}
	return cloneHistoryRows(frozen.rows), frozen.snapshot, true
}

func (store *ScreenBackedHistoryStore) ensureState() {
	if store.frozen == nil {
		store.frozen = make(map[HistoryToken]screenBackedFrozenProjection)
	}
}

func (store *ScreenBackedHistoryStore) normalizeWindowRequest(req HistoryWindowRequest) HistoryWindowRequest {
	req.TerminalID = store.terminalIDFor(req.TerminalID)
	if req.Cols <= 0 && store != nil && store.buffer != nil {
		req.Cols = store.buffer.Cols
	}
	return req
}

func (store *ScreenBackedHistoryStore) terminalIDFor(terminalID string) string {
	if terminalID != "" {
		return terminalID
	}
	if store != nil {
		return store.terminalID
	}
	return ""
}

func screenBackedLatestWindow(req HistoryWindowRequest, rows []HistoryRow, generation Generation) HistoryWindow {
	limit := normalizedLimit(req.Limit)
	start := latestRowsWindowStart(rows, limit)
	page := cloneHistoryRows(rows[start:])
	annotateProjectionRowIndexes(page, start)
	boundary := boundaryForRows(page, generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = cursorBeforeIndex(page[0], start, generation, req.Token, start > 0)
	}
	return screenBackedWindow(req, page, len(rows), HistoryWindowReplace, boundary, generation, start > 0)
}

func screenBackedWindow(req HistoryWindowRequest, rows []HistoryRow, total int, op HistoryWindowOp, boundary HistoryBoundary, generation Generation, hasMore bool) HistoryWindow {
	if total < len(rows) {
		total = len(rows)
	}
	return HistoryWindow{
		TerminalID:   req.TerminalID,
		Token:        req.Token,
		Op:           op,
		Cols:         req.Cols,
		Rows:         cloneHistoryRows(rows),
		Lines:        spansForRows(rows),
		Generation:   generation,
		Boundary:     boundary,
		HasMore:      hasMore,
		LogicalTotal: total,
		Timestamp:    time.Now().UTC(),
	}
}
