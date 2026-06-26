package history

import (
	"errors"
	"fmt"
	"time"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

var (
	// ErrHistoryInvalidMutation 表示 projector 产出了 store 无法按 domain contract
	// 应用的 mutation。失败条件是 mutation 缺少 logical-line/frame payload，或者
	// 引用不存在的 session；store 不会回退读取 live surface 或 raw PTY 修补。
	ErrHistoryInvalidMutation = errors.New("invalid history mutation")

	// ErrHistoryUnsupportedWindowMode 表示当前 R303 内存 harness 尚未实现对应
	// window 模式。R303 只锁 latest/frozen/copy boundary，不提前接 R310 分页协议。
	ErrHistoryUnsupportedWindowMode = errors.New("unsupported history window mode")
)

const (
	terminalOpWriteSpan  = vterm.ScreenOpWriteSpan
	terminalOpScrollRect = vterm.ScreenOpScrollRect
	terminalOpCopyRect   = vterm.ScreenOpCopyRect
	terminalOpClearRect  = vterm.ScreenOpClearRect
	terminalOpClearToEOL = vterm.ScreenOpClearToEOL
	terminalOpCursor     = vterm.ScreenOpCursor
	terminalOpControl    = vterm.ScreenOpControl
	terminalOpResize     = vterm.ScreenOpResize
)

// NewMemoryHistoryProjector 创建 R303 的纯内存 history projector 骨架。
// domain owner：core-v2 history；truth source 只能是 TerminalSemanticTransaction、
// ScreenAppDecision 和 lifecycle CloseReason。调用方必须把返回的 mutation 交给
// InfiniteHistoryStore 应用，projector 自身不读取 store、live surface 或协议状态。
func NewMemoryHistoryProjector() HistoryProjector {
	return &memoryHistoryProjector{nextLineID: 1, nextSessionID: 1, nextFrameID: 1}
}

// NewMemoryHistoryStore 创建 R303 的纯内存 authoritative history store。
// 它保存 LogicalLineStore、CommittedHistoryIndex、MutableFrontier 与 frame journal 的
// 最小组合，用于 domain harness 证明边界；它不是文件 backend，也不定义 R309 持久化格式。
func NewMemoryHistoryStore() InfiniteHistoryStore {
	return &memoryHistoryStore{
		lines:    make(map[LogicalLineID]LogicalLine),
		sessions: make(map[ScreenSessionID]*memoryScreenSession),
		frozen:   make(map[HistoryToken]FrozenHistorySnapshot),
	}
}

type memoryHistoryProjector struct {
	generation Generation

	nextLineID    LogicalLineID
	nextSessionID ScreenSessionID
	nextFrameID   ScreenFrameID

	openLine *LogicalLine

	activeSessionID      ScreenSessionID
	activePrimaryFrameID ScreenFrameID
	activeAltFrameID     ScreenFrameID
	primaryCurrent       *ScreenFrame
	altCurrent           *ScreenFrame
	primaryClearedForAlt bool
	finalCommitted       bool
}

func (projector *memoryHistoryProjector) Apply(tx TerminalSemanticTransaction, decision ScreenAppDecision) (HistoryMutation, error) {
	if projector == nil {
		return HistoryMutation{}, nil
	}
	projector.generation++
	mutation := HistoryMutation{Seq: tx.Seq, Generation: projector.generation}

	// resize/full-replace/clear-screen 这类 boundary 只能失效投影或 mutate frontier，
	// 不能凭空从当前屏幕生成 committed history。
	if decision.NonHistoryBoundary || decision.Mode == ScreenOutputModeNonHistoryBoundary {
		mutation.Events = append(mutation.Events, HistoryMutationEvent{
			Kind:     HistoryMutationNonHistoryBoundary,
			Decision: decision,
		})
		return mutation, nil
	}

	if decision.ArchivePrimaryBeforeAlt {
		if projector.primaryCurrent != nil {
			frame := cloneHistoryFrame(*projector.primaryCurrent)
			mutation.Events = append(mutation.Events, HistoryMutationEvent{
				Kind:          HistoryMutationArchivePrimaryFrame,
				Frame:         &frame,
				SessionID:     frame.SessionID,
				FrameID:       frame.ID,
				ArchiveReason: ArchiveReasonAltEnter,
				Decision:      decision,
			})
		}
		if decision.ClearPrimaryCurrentForAlt {
			projector.primaryCurrent = nil
			projector.activePrimaryFrameID = 0
			projector.primaryClearedForAlt = true
		}
	}

	if decision.ExitAltTransientFrame || tx.AltExited {
		if projector.altCurrent != nil {
			mutation.Events = append(mutation.Events, HistoryMutationEvent{
				Kind:        HistoryMutationCloseScreenSession,
				FrameID:     projector.altCurrent.ID,
				ClosePolicy: ClosePolicyDiscardAltTransient,
				Decision:    decision,
			})
		}
		projector.altCurrent = nil
		projector.activeAltFrameID = 0
		projector.primaryCurrent = nil
		projector.activePrimaryFrameID = 0
	}

	switch decision.Mode {
	case ScreenOutputModeAltTransient:
		if decision.PublishFrame || decision.EnterAltTransientFrame || tx.AltFrame != nil {
			frame := projector.nextFrame(tx.AltFrame, LineKindAltScreenFrame, 0, tx)
			projector.altCurrent = &frame
			projector.activeAltFrameID = frame.ID
			mutation.Events = append(mutation.Events, HistoryMutationEvent{
				Kind:     HistoryMutationPublishAltFrame,
				Frame:    &frame,
				FrameID:  frame.ID,
				Decision: decision,
			})
		}
	case ScreenOutputModePrimaryScreenSession:
		if decision.PublishFrame || tx.PrimaryFrame != nil {
			sessionID := projector.activeSessionID
			opened := false
			if sessionID == 0 {
				sessionID = projector.nextSessionID
				projector.nextSessionID++
				projector.activeSessionID = sessionID
				opened = true
				projector.finalCommitted = false
			}
			if opened {
				mutation.Events = append(mutation.Events, HistoryMutationEvent{
					Kind:      HistoryMutationOpenScreenSession,
					SessionID: sessionID,
					Decision:  decision,
				})
			}
			frame := projector.nextFrame(tx.PrimaryFrame, LineKindScreenFrame, sessionID, tx)
			projector.primaryClearedForAlt = false
			projector.primaryCurrent = &frame
			projector.activePrimaryFrameID = frame.ID
			mutation.Events = append(mutation.Events, HistoryMutationEvent{
				Kind:      HistoryMutationPublishPrimaryFrame,
				Frame:     &frame,
				SessionID: sessionID,
				FrameID:   frame.ID,
				Decision:  decision,
			})
		}
	default:
		projector.applyOrdinaryTransaction(&mutation, tx, decision)
	}

	if decision.ForceCommitFrontier {
		projector.forceCommitOpenLine(&mutation)
	}
	if decision.ClosePrimarySession {
		projector.closePrimarySession(&mutation, decision, false)
	}
	if decision.ForceCommitPrimaryFinalFrame {
		projector.closePrimarySession(&mutation, decision, true)
	}
	return mutation, nil
}

func (projector *memoryHistoryProjector) ForceClose(reason CloseReason) (HistoryMutation, error) {
	if projector == nil {
		return HistoryMutation{}, nil
	}
	projector.generation++
	mutation := HistoryMutation{Generation: projector.generation}
	projector.forceCommitOpenLine(&mutation)
	if projector.primaryCurrent != nil && !projector.finalCommitted {
		frame := cloneHistoryFrame(*projector.primaryCurrent)
		frame.Committed = true
		mutation.Events = append(mutation.Events, HistoryMutationEvent{
			Kind:        HistoryMutationCommitFinalFrame,
			Frame:       &frame,
			SessionID:   frame.SessionID,
			FrameID:     frame.ID,
			ClosePolicy: ClosePolicyCommitPrimaryFinal,
			CloseReason: reason,
		})
		projector.finalCommitted = true
	}
	if projector.activeSessionID != 0 {
		mutation.Events = append(mutation.Events, HistoryMutationEvent{
			Kind:        HistoryMutationCloseScreenSession,
			SessionID:   projector.activeSessionID,
			CloseReason: reason,
			ClosePolicy: ClosePolicyCommitPrimaryFinal,
		})
	}
	projector.activeSessionID = 0
	projector.activePrimaryFrameID = 0
	projector.primaryCurrent = nil
	projector.altCurrent = nil
	projector.activeAltFrameID = 0
	projector.primaryClearedForAlt = false
	return mutation, nil
}

func (projector *memoryHistoryProjector) applyOrdinaryTransaction(mutation *HistoryMutation, tx TerminalSemanticTransaction, decision ScreenAppDecision) {
	if len(tx.Ops) > 0 {
		for _, op := range tx.Ops {
			switch op.Code {
			case terminalOpWriteSpan:
				projector.writeCellsToFrontier(mutation, convertTerminalCells(op.Cells), op.Col, decision)
			case terminalOpClearToEOL:
				projector.truncateFrontierAt(mutation, op.Col, decision)
			case terminalOpClearRect, terminalOpCursor, terminalOpScrollRect, terminalOpCopyRect:
				mutation.Events = append(mutation.Events, HistoryMutationEvent{
					Kind:     HistoryMutationFrontierMutate,
					Decision: decision,
				})
			case terminalOpResize:
				mutation.Events = append(mutation.Events, HistoryMutationEvent{
					Kind:     HistoryMutationNonHistoryBoundary,
					Decision: decision,
				})
			case terminalOpControl:
				projector.applyOrdinaryControl(mutation, op, decision)
			}
		}
	}
	for _, proof := range tx.PrimaryScrollOut {
		if len(proof.Cells) == 0 {
			continue
		}
		line := projector.newLogicalLine(convertTerminalCells(proof.Cells), LineKindOrdinary, SealStateSealed)
		mutation.Events = append(mutation.Events, HistoryMutationEvent{
			Kind:     HistoryMutationOrdinaryCommit,
			Line:     &line,
			LineIDs:  []LogicalLineID{line.ID},
			Decision: decision,
		})
	}
}

func (projector *memoryHistoryProjector) applyOrdinaryControl(mutation *HistoryMutation, op TerminalSemanticOp, decision ScreenAppDecision) {
	switch op.Control {
	case "lf", "ind", "nel":
		projector.forceCommitOpenLine(mutation)
	case "el":
		projector.truncateFrontierAt(mutation, op.Col, decision)
	case "cr", "bs", "cub", "cuu", "cud", "cuf", "cup", "cha", "hpa", "vpa":
		mutation.Events = append(mutation.Events, HistoryMutationEvent{
			Kind:     HistoryMutationFrontierMutate,
			Decision: decision,
		})
	default:
		mutation.Events = append(mutation.Events, HistoryMutationEvent{
			Kind:     HistoryMutationFrontierMutate,
			Decision: decision,
		})
	}
}

func (projector *memoryHistoryProjector) writeCellsToFrontier(mutation *HistoryMutation, cells []Cell, col int, decision ScreenAppDecision) {
	if len(cells) == 0 {
		return
	}
	if projector.openLine == nil {
		line := projector.newLogicalLine(nil, LineKindOrdinary, SealStateOpen)
		projector.openLine = &line
	}
	// 中文说明：R304 普通输出只允许 terminal semantic op 修改 frontier。
	// CR/CUP/EL 后的 write-span 通过列位置覆盖当前 open line，不追加中间态。
	if col < 0 {
		col = len(projector.openLine.Cells)
	}
	replacement := cloneHistoryCells(cells)
	if col >= len(projector.openLine.Cells) {
		projector.openLine.Cells = append(projector.openLine.Cells, replacement...)
	} else {
		updated := cloneHistoryCells(projector.openLine.Cells[:col])
		updated = append(updated, replacement...)
		if tailStart := col + len(replacement); tailStart < len(projector.openLine.Cells) {
			updated = append(updated, projector.openLine.Cells[tailStart:]...)
		}
		projector.openLine.Cells = updated
	}
	projector.openLine.ContentGeneration = projector.generation
	projector.openLine.Generation = projector.generation
	line := cloneLogicalLine(*projector.openLine)
	mutation.Events = append(mutation.Events, HistoryMutationEvent{
		Kind:     HistoryMutationFrontierMutate,
		Line:     &line,
		LineIDs:  []LogicalLineID{line.ID},
		Decision: decision,
	})
}

func (projector *memoryHistoryProjector) truncateFrontierAt(mutation *HistoryMutation, col int, decision ScreenAppDecision) {
	if projector.openLine == nil {
		mutation.Events = append(mutation.Events, HistoryMutationEvent{
			Kind:     HistoryMutationFrontierMutate,
			Decision: decision,
		})
		return
	}
	if col < 0 {
		col = 0
	}
	if col < len(projector.openLine.Cells) {
		projector.openLine.Cells = cloneHistoryCells(projector.openLine.Cells[:col])
	}
	line := cloneLogicalLine(*projector.openLine)
	mutation.Events = append(mutation.Events, HistoryMutationEvent{
		Kind:     HistoryMutationFrontierMutate,
		Line:     &line,
		LineIDs:  []LogicalLineID{line.ID},
		Decision: decision,
	})
}

func (projector *memoryHistoryProjector) forceCommitOpenLine(mutation *HistoryMutation) {
	if projector.openLine == nil || len(projector.openLine.Cells) == 0 {
		projector.openLine = nil
		return
	}
	projector.openLine.Seal = SealStateSealed
	line := cloneLogicalLine(*projector.openLine)
	mutation.Events = append(mutation.Events, HistoryMutationEvent{
		Kind:    HistoryMutationOrdinaryCommit,
		Line:    &line,
		LineIDs: []LogicalLineID{line.ID},
	})
	projector.openLine = nil
}

func (projector *memoryHistoryProjector) closePrimarySession(mutation *HistoryMutation, decision ScreenAppDecision, commitFinal bool) {
	if projector.activeSessionID == 0 {
		return
	}
	if commitFinal && projector.primaryCurrent != nil && !projector.finalCommitted {
		frame := cloneHistoryFrame(*projector.primaryCurrent)
		frame.Committed = true
		mutation.Events = append(mutation.Events, HistoryMutationEvent{
			Kind:        HistoryMutationCommitFinalFrame,
			Frame:       &frame,
			SessionID:   frame.SessionID,
			FrameID:     frame.ID,
			ClosePolicy: ClosePolicyCommitPrimaryFinal,
			Decision:    decision,
		})
		projector.finalCommitted = true
	}
	mutation.Events = append(mutation.Events, HistoryMutationEvent{
		Kind:        HistoryMutationCloseScreenSession,
		SessionID:   projector.activeSessionID,
		ClosePolicy: ClosePolicyDropCurrent,
		Decision:    decision,
	})
	projector.activeSessionID = 0
	projector.activePrimaryFrameID = 0
	projector.primaryCurrent = nil
	projector.primaryClearedForAlt = false
}

func (projector *memoryHistoryProjector) nextFrame(frame *TerminalSemanticFrame, kind LineKind, sessionID ScreenSessionID, tx TerminalSemanticTransaction) ScreenFrame {
	id := projector.nextFrameID
	projector.nextFrameID++
	rows, cols := convertTerminalFrame(frame)
	if kind == LineKindScreenFrame && projector.primaryClearedForAlt {
		if opRows, opCols := frameRowsFromWriteOps(tx.Ops, tx.Size.Cols); len(opRows) > 0 {
			rows = opRows
			cols = opCols
		}
	}
	if cols == 0 {
		cols = tx.Size.Cols
	}
	return ScreenFrame{
		ID:         id,
		SessionID:  sessionID,
		Kind:       kind,
		Rows:       rows,
		ScreenCols: cols,
		ScreenRows: len(rows),
		Committed:  false,
		SourceSeq:  tx.Seq,
		CreatedAt:  time.Now(),
	}
}

func (projector *memoryHistoryProjector) newLogicalLine(cells []Cell, kind LineKind, seal SealState) LogicalLine {
	id := projector.nextLineID
	projector.nextLineID++
	return LogicalLine{
		ID:                id,
		Generation:        projector.generation,
		CreatedGeneration: projector.generation,
		ContentGeneration: projector.generation,
		Seal:              seal,
		Kind:              string(kind),
		Cells:             cloneHistoryCells(cells),
		Residency:         ResidencyMemory,
	}
}

type memoryHistoryStore struct {
	generation Generation

	lines     map[LogicalLineID]LogicalLine
	committed []LogicalLineID
	frontier  []LogicalLineID

	sessions   map[ScreenSessionID]*memoryScreenSession
	currentAlt *ScreenFrame

	frozen     map[HistoryToken]FrozenHistorySnapshot
	nextToken  uint64
	terminalID string
}

type memoryScreenSession struct {
	id       ScreenSessionID
	current  *ScreenFrame
	archives []ScreenFrame
	closed   bool
}

func (store *memoryHistoryStore) ApplyMutation(mutation HistoryMutation) error {
	if store == nil {
		return nil
	}
	if mutation.Generation > store.generation {
		store.generation = mutation.Generation
	} else {
		store.generation++
	}
	for _, event := range mutation.Events {
		if err := store.applyMutationEvent(event); err != nil {
			return err
		}
	}
	return nil
}

func (store *memoryHistoryStore) ApplyOrdinaryEvent(event HistoryEvent) error {
	if store == nil {
		return nil
	}
	switch event.Kind {
	case HistoryEventWritePrimaryCells, HistoryEventMutateFrontier:
		line := LogicalLine{
			ID:                event.LineID,
			Generation:        store.generation + 1,
			CreatedGeneration: store.generation + 1,
			ContentGeneration: store.generation + 1,
			Seal:              SealStateOpen,
			Kind:              string(LineKindOrdinary),
			Cells:             cloneHistoryCells(event.Cells),
			Residency:         ResidencyMemory,
		}
		if line.ID == 0 {
			return fmt.Errorf("%w: ordinary event missing line id", ErrHistoryInvalidMutation)
		}
		store.lines[line.ID] = line
		store.frontier = appendUniqueLineID(store.frontier, line.ID)
	case HistoryEventCommitFrontier, HistoryEventForceCommitFrontier:
		if event.LineID == 0 {
			return fmt.Errorf("%w: commit event missing line id", ErrHistoryInvalidMutation)
		}
		store.committed = appendUniqueLineID(store.committed, event.LineID)
		store.frontier = removeLineID(store.frontier, event.LineID)
	case HistoryEventNonHistoryBoundary:
		store.generation++
	}
	return nil
}

func (store *memoryHistoryStore) OpenScreenSession(params ScreenSessionParams) (ScreenSessionID, error) {
	if store == nil {
		return 0, nil
	}
	id := params.SessionID
	if id == 0 {
		id = ScreenSessionID(len(store.sessions) + 1)
	}
	if _, ok := store.sessions[id]; !ok {
		store.sessions[id] = &memoryScreenSession{id: id}
	}
	return id, nil
}

func (store *memoryHistoryStore) PublishPrimaryFrame(session ScreenSessionID, frame ScreenFrame) error {
	if store == nil {
		return nil
	}
	if session == 0 {
		return fmt.Errorf("%w: primary frame missing session", ErrHistoryInvalidMutation)
	}
	record := store.ensureSession(session)
	cloned := cloneHistoryFrame(frame)
	cloned.SessionID = session
	cloned.Kind = LineKindScreenFrame
	cloned.Committed = false
	record.current = &cloned
	return nil
}

func (store *memoryHistoryStore) ArchivePrimaryFrame(session ScreenSessionID, frame ScreenFrame, _ ArchiveReason) error {
	if store == nil {
		return nil
	}
	if session == 0 {
		return fmt.Errorf("%w: archive missing session", ErrHistoryInvalidMutation)
	}
	record := store.ensureSession(session)
	cloned := cloneHistoryFrame(frame)
	cloned.SessionID = session
	cloned.Kind = LineKindArchivedScreenFrame
	cloned.Committed = false
	record.archives = append(record.archives, cloned)
	if record.current != nil && record.current.ID == frame.ID {
		record.current = nil
	}
	return nil
}

func (store *memoryHistoryStore) PublishAltFrame(frame ScreenFrame) error {
	if store == nil {
		return nil
	}
	cloned := cloneHistoryFrame(frame)
	cloned.Kind = LineKindAltScreenFrame
	cloned.Committed = false
	store.currentAlt = &cloned
	return nil
}

func (store *memoryHistoryStore) CloseScreenSession(session ScreenSessionID, policy ClosePolicy) error {
	if store == nil {
		return nil
	}
	if session == 0 {
		store.currentAlt = nil
		return nil
	}
	record := store.ensureSession(session)
	if policy == ClosePolicyDropCurrent || policy == ClosePolicyDiscardAltTransient {
		record.current = nil
	}
	record.closed = true
	return nil
}

func (store *memoryHistoryStore) LatestWindow(req HistoryWindowRequest) (HistoryWindow, error) {
	if store == nil {
		return HistoryWindow{}, nil
	}
	window := HistoryWindow{
		TerminalID: store.windowTerminalID(req.TerminalID),
		Token:      req.Token,
		Op:         HistoryWindowReplace,
		Cols:       req.Cols,
		Generation: store.generation,
		Timestamp:  time.Now(),
	}
	limit := req.Limit
	if limit <= 0 {
		limit = len(store.committed) + len(store.frontier) + 32
	}
	ids := append([]LogicalLineID(nil), store.committed...)
	ids = append(ids, store.frontier...)
	if len(ids) > limit {
		window.HasMore = true
		ids = ids[len(ids)-limit:]
	}
	for _, id := range ids {
		line, ok := store.lines[id]
		if !ok {
			continue
		}
		committed := containsLineID(store.committed, id)
		window.Rows = append(window.Rows, historyRowFromLine(line, committed))
		window.Lines = append(window.Lines, historySpanFromLine(len(window.Rows)-1, line, committed))
		window.Boundary.LastLineID = id
		if window.Boundary.FirstLineID == 0 {
			window.Boundary.FirstLineID = id
		}
	}
	for _, session := range store.sessionsInOrder() {
		for _, frame := range session.archives {
			window.appendFrameRows(frame, HistorySegmentArchivedPrimaryFrame)
		}
		if session.current != nil {
			window.appendFrameRows(*session.current, HistorySegmentCurrentPrimaryFrame)
		}
	}
	if store.currentAlt != nil {
		window.appendFrameRows(*store.currentAlt, HistorySegmentCurrentAltFrame)
	}
	window.LogicalTotal = len(store.committed)
	window.Boundary.Cursor = HistoryCursor{
		Segment:    HistorySegmentCommitted,
		LineID:     window.Boundary.FirstLineID,
		Generation: store.generation,
		Token:      window.Token,
		Valid:      window.Boundary.FirstLineID != 0,
	}
	return window, nil
}

func (store *memoryHistoryStore) OlderWindow(req HistoryWindowRequest) (HistoryWindow, error) {
	return HistoryWindow{}, ErrHistoryUnsupportedWindowMode
}

func (store *memoryHistoryStore) Freeze(req FreezeHistoryRequest) (FrozenHistorySnapshot, error) {
	if store == nil {
		return FrozenHistorySnapshot{}, nil
	}
	store.nextToken++
	token := HistoryToken(fmt.Sprintf("memory-freeze-%d", store.nextToken))
	snapshot := FrozenHistorySnapshot{
		Token:                 token,
		TerminalID:            store.windowTerminalID(req.TerminalID),
		Cols:                  req.Cols,
		CommittedUpperBound:   lastLineID(store.committed),
		FrozenFrontierLineIDs: append([]LogicalLineID(nil), store.frontier...),
		Generation:            store.generation,
		CreatedAt:             time.Now(),
	}
	if len(store.committed) > 0 {
		snapshot.Boundary.FirstLineID = store.committed[0]
		snapshot.Boundary.LastLineID = lastLineID(store.committed)
	}
	snapshot.Boundary.Cursor = HistoryCursor{
		Segment:    HistorySegmentCommitted,
		LineID:     snapshot.Boundary.FirstLineID,
		Generation: store.generation,
		Token:      token,
		Valid:      snapshot.Boundary.FirstLineID != 0,
	}
	for _, session := range store.sessionsInOrder() {
		if session.current == nil {
			continue
		}
		snapshot.Boundary.Cursor = HistoryCursor{
			Segment:    HistorySegmentCurrentPrimaryFrame,
			SessionID:  session.current.SessionID,
			FrameID:    session.current.ID,
			Generation: store.generation,
			Token:      token,
			Valid:      true,
		}
		break
	}
	if store.currentAlt != nil {
		snapshot.Boundary.Cursor = HistoryCursor{
			Segment:    HistorySegmentCurrentAltFrame,
			FrameID:    store.currentAlt.ID,
			Generation: store.generation,
			Token:      token,
			Valid:      true,
		}
	}
	store.frozen[token] = snapshot
	return snapshot, nil
}

func (store *memoryHistoryStore) Copy(req HistoryCopyRequest) (string, error) {
	if store == nil {
		return "", nil
	}
	ids := append([]LogicalLineID(nil), store.committed...)
	if req.Token != "" {
		snapshot, ok := store.frozen[req.Token]
		if !ok {
			return "", fmt.Errorf("%w: unknown frozen token", ErrHistoryInvalidMutation)
		}
		ids = committedIDsThrough(store.committed, snapshot.CommittedUpperBound)
		ids = append(ids, snapshot.FrozenFrontierLineIDs...)
	}
	var out string
	for i, id := range ids {
		line, ok := store.lines[id]
		if !ok {
			continue
		}
		if i > 0 {
			out += "\n"
		}
		out += plainText(line.Cells)
	}
	return out, nil
}

func (store *memoryHistoryStore) Release(token HistoryToken) error {
	if store == nil || token == "" {
		return nil
	}
	delete(store.frozen, token)
	return nil
}

func (store *memoryHistoryStore) applyMutationEvent(event HistoryMutationEvent) error {
	switch event.Kind {
	case HistoryMutationOrdinaryCommit:
		if event.Line == nil {
			return fmt.Errorf("%w: ordinary commit missing line", ErrHistoryInvalidMutation)
		}
		line := cloneLogicalLine(*event.Line)
		line.Seal = SealStateSealed
		line.Kind = string(LineKindOrdinary)
		store.lines[line.ID] = line
		store.committed = appendUniqueLineID(store.committed, line.ID)
		store.frontier = removeLineID(store.frontier, line.ID)
	case HistoryMutationFrontierMutate:
		if event.Line == nil {
			store.generation++
			return nil
		}
		line := cloneLogicalLine(*event.Line)
		line.Seal = SealStateOpen
		line.Kind = string(LineKindOrdinary)
		store.lines[line.ID] = line
		store.frontier = appendUniqueLineID(store.frontier, line.ID)
	case HistoryMutationOpenScreenSession:
		if event.SessionID == 0 {
			return fmt.Errorf("%w: open session missing session id", ErrHistoryInvalidMutation)
		}
		store.ensureSession(event.SessionID)
	case HistoryMutationPublishPrimaryFrame:
		if event.Frame == nil {
			return fmt.Errorf("%w: primary frame missing payload", ErrHistoryInvalidMutation)
		}
		return store.PublishPrimaryFrame(event.SessionID, *event.Frame)
	case HistoryMutationArchivePrimaryFrame:
		if event.Frame == nil {
			return fmt.Errorf("%w: archive frame missing payload", ErrHistoryInvalidMutation)
		}
		return store.ArchivePrimaryFrame(event.SessionID, *event.Frame, event.ArchiveReason)
	case HistoryMutationPublishAltFrame:
		if event.Frame == nil {
			return fmt.Errorf("%w: alt frame missing payload", ErrHistoryInvalidMutation)
		}
		return store.PublishAltFrame(*event.Frame)
	case HistoryMutationCloseScreenSession:
		return store.CloseScreenSession(event.SessionID, event.ClosePolicy)
	case HistoryMutationCommitFinalFrame:
		if event.Frame == nil {
			return fmt.Errorf("%w: final frame missing payload", ErrHistoryInvalidMutation)
		}
		lineIDs, err := store.commitFrame(event.Frame)
		if err != nil {
			return err
		}
		_ = lineIDs
	case HistoryMutationNonHistoryBoundary:
		store.generation++
	}
	return nil
}

func (store *memoryHistoryStore) commitFrame(frame *ScreenFrame) ([]LogicalLineID, error) {
	if frame == nil {
		return nil, fmt.Errorf("%w: nil frame", ErrHistoryInvalidMutation)
	}
	clonedFrame := cloneHistoryFrame(*frame)
	clonedFrame.Committed = true
	clonedFrame.Kind = LineKindScreenFrame
	record := store.ensureSession(clonedFrame.SessionID)
	record.current = nil
	var lineIDs []LogicalLineID
	for _, row := range clonedFrame.Rows {
		id := nextStoreLineID(store.lines)
		line := LogicalLine{
			ID:                id,
			Generation:        store.generation,
			CreatedGeneration: store.generation,
			ContentGeneration: store.generation,
			Seal:              SealStateSealed,
			Kind:              string(LineKindScreenFrame),
			Cells:             cloneHistoryCells(row),
			ScreenCols:        clonedFrame.ScreenCols,
			Residency:         ResidencyMemory,
		}
		store.lines[id] = line
		store.committed = appendUniqueLineID(store.committed, id)
		lineIDs = append(lineIDs, id)
	}
	return lineIDs, nil
}

func (store *memoryHistoryStore) ensureSession(id ScreenSessionID) *memoryScreenSession {
	if store.sessions == nil {
		store.sessions = make(map[ScreenSessionID]*memoryScreenSession)
	}
	if id == 0 {
		id = ScreenSessionID(len(store.sessions) + 1)
	}
	session, ok := store.sessions[id]
	if !ok {
		session = &memoryScreenSession{id: id}
		store.sessions[id] = session
	}
	return session
}

func (store *memoryHistoryStore) sessionsInOrder() []*memoryScreenSession {
	out := make([]*memoryScreenSession, 0, len(store.sessions))
	for id := ScreenSessionID(1); len(out) < len(store.sessions); id++ {
		if session, ok := store.sessions[id]; ok {
			out = append(out, session)
		}
	}
	return out
}

func (store *memoryHistoryStore) windowTerminalID(reqTerminalID string) string {
	if reqTerminalID != "" {
		store.terminalID = reqTerminalID
		return reqTerminalID
	}
	if store.terminalID != "" {
		return store.terminalID
	}
	return "memory-terminal"
}

func (window *HistoryWindow) appendFrameRows(frame ScreenFrame, segment HistorySegment) {
	for rowIndex, row := range frame.Rows {
		lineID := LogicalLineID(frame.ID)*100000 + LogicalLineID(rowIndex+1)
		window.Rows = append(window.Rows, HistoryRow{
			Cells:      cloneHistoryCells(row),
			Kind:       frame.Kind,
			Segment:    segment,
			LineID:     lineID,
			SessionID:  frame.SessionID,
			FrameID:    frame.ID,
			RowInLine:  rowIndex,
			FixedGrid:  true,
			ScreenCols: frame.ScreenCols,
			Committed:  frame.Committed,
		})
		window.Lines = append(window.Lines, HistoryLineSpan{
			StartRow:      len(window.Rows) - 1,
			EndRow:        len(window.Rows),
			Kind:          frame.Kind,
			Segment:       segment,
			LogicalLineID: lineID,
			SessionID:     frame.SessionID,
			FrameID:       frame.ID,
		})
	}
}

func historyRowFromLine(line LogicalLine, committed bool) HistoryRow {
	kind := LineKind(line.Kind)
	if kind == "" {
		kind = LineKindOrdinary
	}
	fixedGrid := kind == LineKindScreenFrame || kind == LineKindArchivedScreenFrame || kind == LineKindAltScreenFrame
	return HistoryRow{
		Cells:      cloneHistoryCells(line.Cells),
		Kind:       kind,
		Segment:    HistorySegmentCommitted,
		LineID:     line.ID,
		Committed:  committed,
		FixedGrid:  fixedGrid,
		ScreenCols: line.ScreenCols,
	}
}

func historySpanFromLine(row int, line LogicalLine, _ bool) HistoryLineSpan {
	kind := LineKind(line.Kind)
	if kind == "" {
		kind = LineKindOrdinary
	}
	return HistoryLineSpan{
		StartRow:      row,
		EndRow:        row + 1,
		Kind:          kind,
		Segment:       HistorySegmentCommitted,
		LogicalLineID: line.ID,
	}
}

func convertTerminalFrame(frame *TerminalSemanticFrame) ([][]Cell, int) {
	if frame == nil {
		return nil, 0
	}
	rows := make([][]Cell, len(frame.Rows))
	for i, row := range frame.Rows {
		rows[i] = convertTerminalCells(row)
	}
	return rows, frame.Cols
}

func frameRowsFromWriteOps(ops []TerminalSemanticOp, cols int) ([][]Cell, int) {
	var rows [][]Cell
	var current []Cell
	for _, op := range ops {
		switch op.Code {
		case terminalOpWriteSpan:
			current = append(current, convertTerminalCells(op.Cells)...)
		case terminalOpControl:
			if op.Control == "lf" || op.Control == "ind" || op.Control == "nel" {
				if len(current) > 0 {
					rows = append(rows, current)
					current = nil
				}
			}
		}
	}
	if len(current) > 0 {
		rows = append(rows, current)
	}
	if cols <= 0 {
		for _, row := range rows {
			if len(row) > cols {
				cols = len(row)
			}
		}
	}
	return rows, cols
}

func convertTerminalCells(cells []TerminalSemanticCell) []Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]Cell, len(cells))
	for i, cell := range cells {
		out[i] = Cell{
			Text:       cell.Content,
			Width:      cell.Width,
			Style:      convertTerminalStyle(cell.Style),
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
		}
		if out[i].Width == 0 && out[i].Text != "" {
			out[i].Width = 1
		}
	}
	return out
}

func convertTerminalStyle(style TerminalSemanticStyle) CellStyle {
	return CellStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
}

func cloneLogicalLine(line LogicalLine) LogicalLine {
	line.Cells = cloneHistoryCells(line.Cells)
	if line.TailFill != nil {
		tail := *line.TailFill
		line.TailFill = &tail
	}
	return line
}

func cloneHistoryFrame(frame ScreenFrame) ScreenFrame {
	frame.Rows = cloneHistoryRows(frame.Rows)
	return frame
}

func cloneHistoryRows(rows [][]Cell) [][]Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]Cell, len(rows))
	for i, row := range rows {
		out[i] = cloneHistoryCells(row)
	}
	return out
}

func cloneHistoryCells(cells []Cell) []Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]Cell, len(cells))
	copy(out, cells)
	return out
}

func plainText(cells []Cell) string {
	var out string
	for _, cell := range cells {
		out += cell.Text
	}
	return out
}

func appendUniqueLineID(ids []LogicalLineID, id LogicalLineID) []LogicalLineID {
	if id == 0 || containsLineID(ids, id) {
		return ids
	}
	return append(ids, id)
}

func removeLineID(ids []LogicalLineID, id LogicalLineID) []LogicalLineID {
	out := ids[:0]
	for _, existing := range ids {
		if existing != id {
			out = append(out, existing)
		}
	}
	return out
}

func containsLineID(ids []LogicalLineID, id LogicalLineID) bool {
	for _, existing := range ids {
		if existing == id {
			return true
		}
	}
	return false
}

func lastLineID(ids []LogicalLineID) LogicalLineID {
	if len(ids) == 0 {
		return 0
	}
	return ids[len(ids)-1]
}

func committedIDsThrough(ids []LogicalLineID, upper LogicalLineID) []LogicalLineID {
	if upper == 0 {
		return nil
	}
	out := make([]LogicalLineID, 0, len(ids))
	for _, id := range ids {
		out = append(out, id)
		if id == upper {
			break
		}
	}
	return out
}

func nextStoreLineID(lines map[LogicalLineID]LogicalLine) LogicalLineID {
	var max LogicalLineID
	for id := range lines {
		if id > max {
			max = id
		}
	}
	return max + 1
}
