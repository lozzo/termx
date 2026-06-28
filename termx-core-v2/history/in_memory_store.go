package history

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// NewInMemoryHistoryStore 创建新的 R323 内存 HistoryStore。
// domain owner：history store；写入真值只来自 HistoryMutationBatch，store 不解释
// terminal ops、不读取 live snapshot，也不恢复旧 memoryHistoryStore 双路径。
func NewInMemoryHistoryStore(terminalID string) HistoryStore {
	return &inMemoryHistoryStore{
		terminalID: terminalID,
		lines:      make(map[LogicalLineID]LogicalLine),
		frozen:     make(map[HistoryToken]frozenWindowProjection),
	}
}

// NewBackendHistoryStore 创建带 payload backend 的 HistoryStore。
// domain owner 仍是 core-v2 HistoryStore：backend 只承载 sealed payload 驻留和
// 按 id 读取，timeline/window/cursor truth 仍由 store 内部索引裁决。
func NewBackendHistoryStore(terminalID string, backend StorageBackend) HistoryStore {
	store := NewInMemoryHistoryStore(terminalID).(*inMemoryHistoryStore)
	store.storage = backend
	if payload, ok := backend.(LinePayloadBackend); ok {
		store.payload = payload
	}
	return store
}

// NewInMemoryHistoryStoreFromRecovered 从 StorageBackend 恢复结果创建内存 store。
// 调用边界：恢复只重建 logical payload、sealed timeline 和 frame record metadata，
// 不 replay raw PTY，也不把 storage residency 当成 mutability truth。
func NewInMemoryHistoryStoreFromRecovered(terminalID string, recovered RecoveredHistoryState) HistoryStore {
	store := NewInMemoryHistoryStore(terminalID).(*inMemoryHistoryStore)
	store.generation = recovered.Generation
	for _, line := range recovered.Lines {
		store.lines[line.ID] = cloneLogicalLine(line)
	}
	store.timeline = cloneHistoryRecords(recovered.Timeline)
	store.frameRecords = cloneFrameRecords(recovered.Frames)
	store.reindexCounters()
	return store
}

type inMemoryHistoryStore struct {
	terminalID    string
	generation    Generation
	lines         map[LogicalLineID]LogicalLine
	storage       StorageBackend
	payload       LinePayloadBackend
	dirtyLines    map[LogicalLineID]struct{}
	timeline      []HistoryRecord
	openLine      *OpenLine
	frameJournal  FrameJournal
	frameRecords  []FrameRecord
	frozen        map[HistoryToken]frozenWindowProjection
	nextToken     uint64
	nextRecordID  HistoryRecordID
	nextLineID    LogicalLineID
	nextFrameID   ScreenFrameID
	nextSessionID ScreenSessionID
}

type frozenWindowProjection struct {
	snapshot FrozenHistorySnapshot
	rows     []HistoryRow
	index    []projectedRowRef
	lines    []HistoryLineSpan
	boundary HistoryBoundary
}

type projectedRowRef struct {
	LineID     LogicalLineID
	Kind       LineKind
	Segment    HistorySegment
	SessionID  ScreenSessionID
	FrameID    ScreenFrameID
	RowInLine  int
	Index      int
	FixedGrid  bool
	ScreenCols int
	Committed  bool
}

func (store *inMemoryHistoryStore) Apply(batch HistoryMutationBatch) error {
	if store == nil {
		return nil
	}
	store.ensureState()
	store.generation++
	if batch.Generation > store.generation {
		store.generation = batch.Generation
	}
	for _, mutation := range batch.Mutations {
		if err := store.applyMutation(mutation); err != nil {
			return err
		}
	}
	if err := store.flushStorage(); err != nil {
		return err
	}
	return nil
}

func (store *inMemoryHistoryStore) ReadState() HistoryReadState {
	if store == nil {
		return HistoryReadState{}
	}
	store.ensureState()
	return HistoryReadState{
		Generation:        store.generation,
		HasOpenLine:       store.openLine != nil,
		HasTimeline:       len(store.timeline) > 0,
		HasPrimaryCurrent: store.frameJournal.PrimaryCurrent != nil,
		HasAltCurrent:     store.frameJournal.AltCurrent != nil,
	}
}

func (store *inMemoryHistoryStore) LatestWindow(req HistoryWindowRequest) (HistoryWindow, error) {
	if store == nil {
		return HistoryWindow{}, nil
	}
	store.ensureState()
	if req.Token != "" {
		frozen, ok := store.frozen[req.Token]
		if !ok {
			return HistoryWindow{}, ErrHistoryInvalidMutation
		}
		if len(frozen.index) > 0 {
			return store.latestWindowFromIndex(req, frozen.index, frozen.snapshot.Generation)
		}
		return store.latestWindowFromRows(req, frozen.rows, frozen.snapshot.Generation)
	}
	if store.canUseIndexedProjection() {
		index := store.liveProjectionIndex()
		return store.latestWindowFromIndex(req, index, store.generation)
	}
	return store.latestWindowFromRows(req, store.projectLiveRows(), store.generation)
}

func (store *inMemoryHistoryStore) latestWindowFromRows(req HistoryWindowRequest, rows []HistoryRow, generation Generation) (HistoryWindow, error) {
	limit := normalizedLimit(req.Limit)
	start := maxInt(0, len(rows)-limit)
	page := cloneHistoryRows(rows[start:])
	annotateProjectionRowIndexes(page, start)
	boundary := boundaryForRows(page, generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = HistoryCursor{
			Generation:     generation,
			BeforeRowIndex: start,
			Valid:          start > 0,
			Token:          req.Token,
		}
		if boundary.Cursor.Valid {
			boundary.Cursor.Segment = page[0].Segment
			boundary.Cursor.SessionID = page[0].SessionID
			boundary.Cursor.FrameID = page[0].FrameID
			boundary.Cursor.LineID = page[0].LineID
			boundary.Cursor.RowInLine = page[0].RowInLine
		}
	}
	return store.windowFromProjectedRows(req, page, HistoryWindowReplace, boundary, generation), nil
}

func (store *inMemoryHistoryStore) OlderWindow(req HistoryWindowRequest) (HistoryWindow, error) {
	if store == nil {
		return HistoryWindow{}, nil
	}
	store.ensureState()
	if req.Token != "" {
		if frozen, ok := store.frozen[req.Token]; ok && len(frozen.index) > 0 {
			return store.olderWindowFromIndex(req, frozen.index, frozen.snapshot.Generation)
		}
	}
	if req.Token == "" && store.canUseIndexedProjection() {
		return store.olderWindowFromIndex(req, store.liveProjectionIndex(), store.generation)
	}
	rows := store.projectRowsForOlder(req)
	limit := normalizedLimit(req.Limit)
	if !req.Cursor.Valid {
		return store.windowFromProjectedRows(req, nil, HistoryWindowPrepend, HistoryBoundary{Cursor: HistoryCursor{Generation: store.generation}}, store.generation), nil
	}
	end := req.Cursor.BeforeRowIndex
	if end <= 0 || end > len(rows) {
		end = len(rows)
	}
	start := maxInt(0, end-limit)
	page := cloneHistoryRows(rows[start:end])
	annotateProjectionRowIndexes(page, start)
	boundary := boundaryForRows(page, store.generation, req.Token)
	boundary.Cursor = HistoryCursor{Generation: store.generation, BeforeRowIndex: start, Token: req.Token, Valid: start > 0}
	if len(page) > 0 && boundary.Cursor.Valid {
		boundary.Cursor.Segment = page[0].Segment
		boundary.Cursor.SessionID = page[0].SessionID
		boundary.Cursor.FrameID = page[0].FrameID
		boundary.Cursor.LineID = page[0].LineID
		boundary.Cursor.RowInLine = page[0].RowInLine
	}
	return store.windowFromProjectedRows(req, page, HistoryWindowPrepend, boundary, store.generation), nil
}

func (store *inMemoryHistoryStore) NewerWindow(req HistoryWindowRequest) (HistoryWindow, error) {
	return HistoryWindow{}, ErrHistoryUnsupportedWindowMode
}

func (store *inMemoryHistoryStore) Freeze(req FreezeHistoryRequest) (FrozenHistorySnapshot, error) {
	if store == nil {
		return FrozenHistorySnapshot{}, nil
	}
	store.ensureState()
	store.nextToken++
	token := HistoryToken(fmt.Sprintf("hist-%d", store.nextToken))
	var rows []HistoryRow
	var index []projectedRowRef
	var latestRows []HistoryRow
	var boundary HistoryBoundary
	if store.canUseIndexedProjection() {
		index = store.liveProjectionIndex()
		page, start, err := store.rowsFromIndexWindow(index, maxInt(0, len(index)-normalizedLimit(req.Limit)), len(index))
		if err != nil {
			return FrozenHistorySnapshot{}, err
		}
		latestRows = page
		boundary = boundaryForRows(latestRows, store.generation, token)
		if len(latestRows) > 0 {
			boundary.Cursor = cursorBeforeIndex(latestRows[0], start, store.generation, token, start > 0)
		}
	} else {
		rows = store.projectLiveRows()
		limit := normalizedLimit(req.Limit)
		start := maxInt(0, len(rows)-limit)
		latestRows = cloneHistoryRows(rows[start:])
		annotateProjectionRowIndexes(latestRows, start)
		boundary = boundaryForRows(latestRows, store.generation, token)
		if len(latestRows) > 0 {
			boundary.Cursor = HistoryCursor{
				Segment:        latestRows[0].Segment,
				SessionID:      latestRows[0].SessionID,
				FrameID:        latestRows[0].FrameID,
				LineID:         latestRows[0].LineID,
				RowInLine:      latestRows[0].RowInLine,
				BeforeRowIndex: start,
				Generation:     store.generation,
				Token:          token,
				Valid:          start > 0,
			}
		}
	}
	snapshot := FrozenHistorySnapshot{
		Token:                 token,
		TerminalID:            req.TerminalID,
		Cols:                  req.Cols,
		CommittedUpperBound:   lastSealedLineID(latestRows),
		FrozenFrontierLineIDs: mutableLineIDs(latestRows),
		FrozenPrimaryFrames:   store.snapshotPrimaryFrames(),
		FrozenAltFrame:        store.snapshotAltFrame(),
		Boundary:              boundary,
		Generation:            store.generation,
		CreatedAt:             time.Now().UTC(),
	}
	if len(rows) == 0 && len(index) > 0 {
		rows = latestRows
	}
	store.frozen[token] = frozenWindowProjection{
		snapshot: snapshot,
		rows:     cloneHistoryRows(rows),
		index:    cloneProjectedRowRefs(index),
		lines:    spansForRows(rows),
		boundary: boundary,
	}
	return snapshot, nil
}

func (store *inMemoryHistoryStore) Copy(req HistoryCopyRequest) (string, error) {
	if store == nil {
		return "", nil
	}
	store.ensureState()
	var rows []HistoryRow
	if req.Token != "" {
		frozen, ok := store.frozen[req.Token]
		if !ok {
			return "", ErrHistoryInvalidMutation
		}
		if len(frozen.index) > 0 {
			return store.copyFromIndex(frozen.index, req)
		}
		rows = frozen.rows
	} else {
		if store.canUseIndexedProjection() {
			return store.copyFromIndex(store.liveProjectionIndex(), req)
		}
		rows = store.projectLiveRows()
	}
	selected := rowsBetweenCursors(rows, req.Start, req.End)
	texts := make([]string, 0, len(selected))
	for _, row := range selected {
		texts = append(texts, rowText(row.Cells))
	}
	return strings.Join(texts, "\n"), nil
}

func (store *inMemoryHistoryStore) Release(token HistoryToken) error {
	if store == nil || token == "" {
		return nil
	}
	delete(store.frozen, token)
	return nil
}

func (store *inMemoryHistoryStore) applyMutation(mutation HistoryMutation) error {
	switch mutation.Kind {
	case HistoryMutationUpsertOpenLine:
		if mutation.OpenLine != nil {
			open := *mutation.OpenLine
			open.Draft.Line = cloneLogicalLine(open.Draft.Line)
			store.openLine = &open
			store.lines[open.Draft.Line.ID] = cloneLogicalLine(open.Draft.Line)
			store.observeLineID(open.Draft.Line.ID)
			store.markLineDirty(open.Draft.Line.ID)
		}
	case HistoryMutationSealLine:
		if mutation.Line == nil {
			return ErrHistoryInvalidMutation
		}
		line := cloneLogicalLine(*mutation.Line)
		line.Seal = SealStateSealed
		store.lines[line.ID] = line
		store.observeLineID(line.ID)
		store.markLineDirty(line.ID)
		if store.openLine != nil && store.openLine.Draft.Line.ID == line.ID {
			store.openLine = nil
		}
	case HistoryMutationAppendTimelineRecord:
		if mutation.Record == nil {
			return ErrHistoryInvalidMutation
		}
		record := cloneHistoryRecord(*mutation.Record)
		store.timeline = append(store.timeline, record)
		store.observeHistoryRecord(record)
	case HistoryMutationReplacePrimaryFrame:
		if mutation.Mutable == nil {
			return ErrHistoryInvalidMutation
		}
		frame := cloneMutableFrame(*mutation.Mutable)
		store.frameJournal.PrimaryCurrent = &frame
		store.observeFrameID(frame.ID)
		store.observeSessionID(frame.SessionID)
		store.upsertDraftLines(frame.Rows)
	case HistoryMutationArchivePrimaryFrame:
		if mutation.Sealed == nil {
			return ErrHistoryInvalidMutation
		}
		frame := cloneSealedFrame(*mutation.Sealed)
		store.frameJournal.PrimaryArchived = append(store.frameJournal.PrimaryArchived, frame)
		store.observeFrameID(frame.ID)
		store.observeSessionID(frame.SessionID)
		store.upsertLogicalLines(frame.Lines)
		if store.frameJournal.PrimaryCurrent != nil && store.frameJournal.PrimaryCurrent.ID == frame.ID {
			store.frameJournal.PrimaryCurrent = nil
		}
		store.upsertFrameRecord(frame)
	case HistoryMutationClearPrimaryFrame:
		store.frameJournal.PrimaryCurrent = nil
	case HistoryMutationReplaceAltFrame:
		if mutation.Transient == nil {
			return ErrHistoryInvalidMutation
		}
		frame := cloneTransientFrame(*mutation.Transient)
		store.frameJournal.AltCurrent = &frame
		store.observeFrameID(frame.ID)
		store.upsertDraftLines(frame.Rows)
	case HistoryMutationClearAltFrame:
		store.frameJournal.AltCurrent = nil
	case HistoryMutationClosePrimaryFrame:
		if mutation.Sealed == nil {
			return ErrHistoryInvalidMutation
		}
		frame := cloneSealedFrame(*mutation.Sealed)
		store.observeFrameID(frame.ID)
		store.observeSessionID(frame.SessionID)
		store.upsertLogicalLines(frame.Lines)
		if store.frameJournal.PrimaryCurrent != nil && store.frameJournal.PrimaryCurrent.ID == frame.ID {
			store.frameJournal.PrimaryCurrent = nil
		}
		store.upsertFrameRecord(frame)
	case HistoryMutationNonHistoryBoundary:
		return nil
	case HistoryMutationClearScrollback:
		// 中文说明：ED3/clear-scrollback 在 TermX 无限历史里只是“新建一页”的
		// 软边界。它可以 bump generation 让旧窗口重取，但不能删除 core-v2
		// logical-line truth；真正删除历史必须是后续显式 truncate/retention 语义。
		store.frozen = make(map[HistoryToken]frozenWindowProjection)
	default:
		return ErrHistoryInvalidMutation
	}
	return nil
}

func (store *inMemoryHistoryStore) ensureState() {
	if store.lines == nil {
		store.lines = make(map[LogicalLineID]LogicalLine)
	}
	if store.frozen == nil {
		store.frozen = make(map[HistoryToken]frozenWindowProjection)
	}
	if store.dirtyLines == nil {
		store.dirtyLines = make(map[LogicalLineID]struct{})
	}
}

func (store *inMemoryHistoryStore) markLineDirty(id LogicalLineID) {
	if store == nil || id == 0 {
		return
	}
	if store.dirtyLines == nil {
		store.dirtyLines = make(map[LogicalLineID]struct{})
	}
	store.dirtyLines[id] = struct{}{}
}

// 中文说明：next* 计数器只是后续分配 ID 的辅助状态，不是 history truth。
// PTY 输出热路径只能观察本批 mutation 携带的 id；全量扫描仅允许在恢复路径执行。
func (store *inMemoryHistoryStore) observeLineID(id LogicalLineID) {
	if store == nil || id == 0 {
		return
	}
	if store.nextLineID <= id {
		store.nextLineID = id + 1
	}
}

func (store *inMemoryHistoryStore) observeRecordID(id HistoryRecordID) {
	if store == nil || id == 0 {
		return
	}
	if store.nextRecordID <= id {
		store.nextRecordID = id + 1
	}
}

func (store *inMemoryHistoryStore) observeFrameID(id ScreenFrameID) {
	if store == nil || id == 0 {
		return
	}
	if store.nextFrameID <= id {
		store.nextFrameID = id + 1
	}
}

func (store *inMemoryHistoryStore) observeSessionID(id ScreenSessionID) {
	if store == nil || id == 0 {
		return
	}
	if store.nextSessionID <= id {
		store.nextSessionID = id + 1
	}
}

func (store *inMemoryHistoryStore) observeHistoryRecord(record HistoryRecord) {
	store.observeRecordID(record.ID)
	store.observeFrameID(record.FrameID)
	for _, id := range record.LineIDs {
		store.observeLineID(id)
	}
}

func (store *inMemoryHistoryStore) observeFrameRecord(record FrameRecord) {
	store.observeFrameID(record.FrameID)
	store.observeSessionID(record.SessionID)
	for _, id := range record.LineIDs {
		store.observeLineID(id)
	}
}

func (store *inMemoryHistoryStore) flushStorage() error {
	if store == nil || store.storage == nil || len(store.dirtyLines) == 0 {
		return nil
	}
	lines := make([]LogicalLine, 0, len(store.dirtyLines))
	for id := range store.dirtyLines {
		line, ok := store.lines[id]
		if !ok {
			continue
		}
		copyLine := cloneLogicalLine(line)
		copyLine.Residency = ResidencyFile
		lines = append(lines, copyLine)
	}
	tx := StorageTransaction{
		Generation: store.generation,
		Lines:      lines,
		Mutable:    store.mutableLineIDsForStorage(),
	}
	if err := store.storage.Apply(tx); err != nil {
		return err
	}
	if store.payload != nil {
		for _, line := range lines {
			if line.Seal == SealStateSealed && store.openLineLineID() != line.ID && !store.lineInCurrentFrame(line.ID) {
				delete(store.lines, line.ID)
			}
		}
	}
	store.dirtyLines = make(map[LogicalLineID]struct{})
	return nil
}

func (store *inMemoryHistoryStore) mutableLineIDsForStorage() []LogicalLineID {
	var ids []LogicalLineID
	if store.openLine != nil {
		ids = append(ids, store.openLine.Draft.Line.ID)
	}
	if store.frameJournal.PrimaryCurrent != nil {
		for _, draft := range store.frameJournal.PrimaryCurrent.Rows {
			ids = append(ids, draft.Line.ID)
		}
	}
	if store.frameJournal.AltCurrent != nil {
		for _, draft := range store.frameJournal.AltCurrent.Rows {
			ids = append(ids, draft.Line.ID)
		}
	}
	return ids
}

func (store *inMemoryHistoryStore) openLineLineID() LogicalLineID {
	if store == nil || store.openLine == nil {
		return 0
	}
	return store.openLine.Draft.Line.ID
}

func (store *inMemoryHistoryStore) lineInCurrentFrame(id LogicalLineID) bool {
	if store == nil || id == 0 {
		return false
	}
	if store.frameJournal.PrimaryCurrent != nil {
		for _, draft := range store.frameJournal.PrimaryCurrent.Rows {
			if draft.Line.ID == id {
				return true
			}
		}
	}
	if store.frameJournal.AltCurrent != nil {
		for _, draft := range store.frameJournal.AltCurrent.Rows {
			if draft.Line.ID == id {
				return true
			}
		}
	}
	return false
}

func (store *inMemoryHistoryStore) canUseIndexedProjection() bool {
	return store != nil && store.payload != nil
}

func (store *inMemoryHistoryStore) liveProjectionIndex() []projectedRowRef {
	if store == nil {
		return nil
	}
	records := cloneHistoryRecords(store.timeline)
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Seq == records[j].Seq {
			return records[i].ID < records[j].ID
		}
		return records[i].Seq < records[j].Seq
	})
	index := make([]projectedRowRef, 0, len(records))
	for _, record := range records {
		switch record.Kind {
		case HistoryRecordArchivedPrimaryFrame:
			index = append(index, projectedRefsForRecord(record, HistorySegmentArchivedPrimaryFrame, LineKindArchivedScreenFrame)...)
		case HistoryRecordClosedPrimaryFrame:
			index = append(index, projectedRefsForRecord(record, HistorySegmentCommitted, LineKindScreenFrame)...)
		default:
			index = append(index, projectedRefsForRecord(record, HistorySegmentCommitted, lineKindFromRecord(record.Kind))...)
		}
	}
	if store.openLine != nil {
		index = append(index, projectedRowRef{
			LineID:    store.openLine.Draft.Line.ID,
			Kind:      LineKindOrdinary,
			Segment:   HistorySegmentCommitted,
			Committed: false,
		})
	}
	if store.frameJournal.PrimaryCurrent != nil {
		frame := store.frameJournal.PrimaryCurrent
		for _, draft := range frame.Rows {
			index = append(index, projectedRowRef{
				LineID:     draft.Line.ID,
				Kind:       LineKindScreenFrame,
				Segment:    HistorySegmentCurrentPrimaryFrame,
				SessionID:  frame.SessionID,
				FrameID:    frame.ID,
				FixedGrid:  true,
				ScreenCols: frame.Cols,
				Committed:  false,
			})
		}
	}
	if store.frameJournal.AltCurrent != nil {
		frame := store.frameJournal.AltCurrent
		for _, draft := range frame.Rows {
			index = append(index, projectedRowRef{
				LineID:     draft.Line.ID,
				Kind:       LineKindAltScreenFrame,
				Segment:    HistorySegmentCurrentAltFrame,
				FrameID:    frame.ID,
				FixedGrid:  true,
				ScreenCols: frame.Cols,
				Committed:  false,
			})
		}
	}
	for i := range index {
		index[i].Index = i
	}
	return index
}

func projectedRefsForRecord(record HistoryRecord, segment HistorySegment, kind LineKind) []projectedRowRef {
	refs := make([]projectedRowRef, 0, len(record.LineIDs))
	for _, id := range record.LineIDs {
		refs = append(refs, projectedRowRef{
			LineID:    id,
			Kind:      kind,
			Segment:   segment,
			FrameID:   record.FrameID,
			FixedGrid: record.FrameID != 0,
			Committed: true,
		})
	}
	return refs
}

func (store *inMemoryHistoryStore) latestWindowFromIndex(req HistoryWindowRequest, index []projectedRowRef, generation Generation) (HistoryWindow, error) {
	limit := normalizedLimit(req.Limit)
	start := maxInt(0, len(index)-limit)
	page, _, err := store.rowsFromIndexWindow(index, start, len(index))
	if err != nil {
		return HistoryWindow{}, err
	}
	boundary := boundaryForRows(page, generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = cursorBeforeIndex(page[0], start, generation, req.Token, start > 0)
	}
	return store.windowFromProjectedRows(req, page, HistoryWindowReplace, boundary, generation), nil
}

func (store *inMemoryHistoryStore) olderWindowFromIndex(req HistoryWindowRequest, index []projectedRowRef, generation Generation) (HistoryWindow, error) {
	limit := normalizedLimit(req.Limit)
	if !req.Cursor.Valid {
		return store.windowFromProjectedRows(req, nil, HistoryWindowPrepend, HistoryBoundary{Cursor: HistoryCursor{Generation: generation, Token: req.Token}}, generation), nil
	}
	end := req.Cursor.BeforeRowIndex
	if end <= 0 || end > len(index) {
		end = len(index)
	}
	start := maxInt(0, end-limit)
	page, _, err := store.rowsFromIndexWindow(index, start, end)
	if err != nil {
		return HistoryWindow{}, err
	}
	boundary := boundaryForRows(page, generation, req.Token)
	if len(page) > 0 {
		boundary.Cursor = cursorBeforeIndex(page[0], start, generation, req.Token, start > 0)
	} else {
		boundary.Cursor = HistoryCursor{Generation: generation, Token: req.Token}
	}
	return store.windowFromProjectedRows(req, page, HistoryWindowPrepend, boundary, generation), nil
}

func cursorBeforeIndex(row HistoryRow, beforeIndex int, generation Generation, token HistoryToken, valid bool) HistoryCursor {
	return HistoryCursor{
		Segment:        row.Segment,
		SessionID:      row.SessionID,
		FrameID:        row.FrameID,
		LineID:         row.LineID,
		RowInLine:      row.RowInLine,
		BeforeRowIndex: beforeIndex,
		Generation:     generation,
		Token:          token,
		Valid:          valid,
	}
}

func (store *inMemoryHistoryStore) rowsFromIndexWindow(index []projectedRowRef, start int, end int) ([]HistoryRow, int, error) {
	if start < 0 {
		start = 0
	}
	if end > len(index) {
		end = len(index)
	}
	if end < start {
		end = start
	}
	refs := index[start:end]
	ids := make([]LogicalLineID, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.LineID)
	}
	lines, err := store.loadLines(ids)
	if err != nil {
		return nil, start, err
	}
	lineByID := make(map[LogicalLineID]LogicalLine, len(lines))
	for _, line := range lines {
		lineByID[line.ID] = line
	}
	rows := make([]HistoryRow, 0, len(refs))
	for _, ref := range refs {
		line, ok := lineByID[ref.LineID]
		if !ok {
			continue
		}
		row := rowFromLogicalLine(line, ref.Segment, ref.Kind, ref.Committed, ref.SessionID, ref.FrameID, ref.FixedGrid)
		row.RowInLine = ref.RowInLine
		row.ProjectionRowIndex = ref.Index
		rows = append(rows, row)
	}
	return rows, start, nil
}

func (store *inMemoryHistoryStore) loadLines(ids []LogicalLineID) ([]LogicalLine, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if store.payload != nil {
		return store.payload.GetLines(ids)
	}
	lines := make([]LogicalLine, 0, len(ids))
	for _, id := range ids {
		if line, ok := store.lines[id]; ok {
			lines = append(lines, cloneLogicalLine(line))
		}
	}
	return lines, nil
}

func (store *inMemoryHistoryStore) copyFromIndex(index []projectedRowRef, req HistoryCopyRequest) (string, error) {
	if len(index) == 0 {
		return "", nil
	}
	start := indexByLineID(index, req.Start.LineID)
	end := indexByLineID(index, req.End.LineID)
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = len(index) - 1
	}
	if start > end {
		start, end = end, start
	}
	rows, _, err := store.rowsFromIndexWindow(index, start, end+1)
	if err != nil {
		return "", err
	}
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, rowText(row.Cells))
	}
	return strings.Join(texts, "\n"), nil
}

func indexByLineID(index []projectedRowRef, id LogicalLineID) int {
	if id == 0 {
		return -1
	}
	for i, ref := range index {
		if ref.LineID == id {
			return i
		}
	}
	return -1
}

func cloneProjectedRowRefs(refs []projectedRowRef) []projectedRowRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]projectedRowRef, len(refs))
	copy(out, refs)
	return out
}

func (store *inMemoryHistoryStore) projectLiveRows() []HistoryRow {
	rows := store.projectTimelineRows()
	if store.openLine != nil {
		rows = append(rows, rowFromLogicalLine(store.openLine.Draft.Line, HistorySegmentCommitted, LineKindOrdinary, false, 0, 0, false))
	}
	if store.frameJournal.PrimaryCurrent != nil {
		rows = append(rows, rowsFromMutableFrame(*store.frameJournal.PrimaryCurrent, HistorySegmentCurrentPrimaryFrame, LineKindScreenFrame, false)...)
	}
	if store.frameJournal.AltCurrent != nil {
		rows = append(rows, rowsFromTransientFrame(*store.frameJournal.AltCurrent)...)
	}
	return rows
}

func (store *inMemoryHistoryStore) projectTimelineRows() []HistoryRow {
	records := cloneHistoryRecords(store.timeline)
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Seq == records[j].Seq {
			return records[i].ID < records[j].ID
		}
		return records[i].Seq < records[j].Seq
	})
	var rows []HistoryRow
	for _, record := range records {
		switch record.Kind {
		case HistoryRecordArchivedPrimaryFrame:
			rows = append(rows, store.rowsFromTimelineFrame(record, HistorySegmentArchivedPrimaryFrame, LineKindArchivedScreenFrame)...)
		case HistoryRecordClosedPrimaryFrame:
			rows = append(rows, store.rowsFromTimelineFrame(record, HistorySegmentCommitted, LineKindScreenFrame)...)
		default:
			for _, lineID := range record.LineIDs {
				if line, ok := store.lines[lineID]; ok {
					rows = append(rows, rowFromLogicalLine(line, HistorySegmentCommitted, lineKindFromRecord(record.Kind), true, 0, 0, false))
				}
			}
		}
	}
	return rows
}

func (store *inMemoryHistoryStore) projectRowsForOlder(req HistoryWindowRequest) []HistoryRow {
	if req.Token != "" {
		if frozen, ok := store.frozen[req.Token]; ok {
			return cloneHistoryRows(frozen.rows)
		}
	}
	// 中文说明：older cursor 从 latest 的完整 live projection 向前翻页。
	// 如果 latest limit 截在 current frame 中间，必须先返回 current frame 的更早行，
	// 再接 sealed timeline；不能只看 timeline，否则 screen app 上滑会丢半个 current。
	return store.projectLiveRows()
}

func (store *inMemoryHistoryStore) rowsFromTimelineFrame(record HistoryRecord, segment HistorySegment, kind LineKind) []HistoryRow {
	frame := store.sealedFrameByID(record.FrameID)
	if frame == nil {
		var rows []HistoryRow
		for _, lineID := range record.LineIDs {
			if line, ok := store.lines[lineID]; ok {
				rows = append(rows, rowFromLogicalLine(line, segment, kind, true, 0, record.FrameID, true))
			}
		}
		return rows
	}
	rows := make([]HistoryRow, 0, len(frame.Lines))
	for _, line := range frame.Lines {
		rows = append(rows, rowFromLogicalLine(line, segment, kind, true, frame.SessionID, frame.ID, true))
	}
	return rows
}

func (store *inMemoryHistoryStore) sealedFrameByID(id ScreenFrameID) *SealedFrame {
	for _, frame := range store.frameJournal.PrimaryArchived {
		if frame.ID == id {
			cloned := cloneSealedFrame(frame)
			return &cloned
		}
	}
	for _, record := range store.frameRecords {
		if record.FrameID != id {
			continue
		}
		lines := make([]LogicalLine, 0, len(record.LineIDs))
		for _, lineID := range record.LineIDs {
			if line, ok := store.lines[lineID]; ok {
				lines = append(lines, cloneLogicalLine(line))
			}
		}
		return &SealedFrame{
			ID:        record.FrameID,
			SessionID: record.SessionID,
			Seq:       record.Sequence,
			Cols:      record.ScreenSize.Cols,
			Lines:     lines,
			Reason:    record.SealReason,
		}
	}
	return nil
}

func (store *inMemoryHistoryStore) upsertLogicalLines(lines []LogicalLine) {
	for _, line := range lines {
		store.lines[line.ID] = cloneLogicalLine(line)
		store.observeLineID(line.ID)
		store.markLineDirty(line.ID)
	}
}

func (store *inMemoryHistoryStore) upsertDraftLines(drafts []LogicalLineDraft) {
	for _, draft := range drafts {
		store.lines[draft.Line.ID] = cloneLogicalLine(draft.Line)
		store.observeLineID(draft.Line.ID)
		store.markLineDirty(draft.Line.ID)
	}
}

func (store *inMemoryHistoryStore) upsertFrameRecord(frame SealedFrame) {
	record := FrameRecord{
		SessionID:  frame.SessionID,
		FrameID:    frame.ID,
		Sequence:   frame.Seq,
		LineIDs:    lineIDsFromLogicalLines(frame.Lines),
		ScreenSize: TerminalSemanticSize{Cols: frame.Cols, Rows: len(frame.Lines)},
		SealReason: frame.Reason,
	}
	store.observeFrameRecord(record)
	for i, current := range store.frameRecords {
		if current.FrameID == frame.ID {
			store.frameRecords[i] = cloneFrameRecord(record)
			return
		}
	}
	store.frameRecords = append(store.frameRecords, record)
}

func (store *inMemoryHistoryStore) snapshotPrimaryFrames() []ScreenFrame {
	if store.frameJournal.PrimaryCurrent == nil {
		return nil
	}
	frame := store.frameJournal.PrimaryCurrent
	return []ScreenFrame{{
		ID:         frame.ID,
		SessionID:  frame.SessionID,
		Kind:       LineKindScreenFrame,
		Rows:       frameRowsFromDrafts(frame.Rows),
		ScreenCols: frame.Cols,
		ScreenRows: len(frame.Rows),
		SourceSeq:  frame.Seq,
		CreatedAt:  frame.CreatedAt,
	}}
}

func (store *inMemoryHistoryStore) snapshotAltFrame() *ScreenFrame {
	if store.frameJournal.AltCurrent == nil {
		return nil
	}
	frame := store.frameJournal.AltCurrent
	return &ScreenFrame{
		ID:         frame.ID,
		Kind:       LineKindAltScreenFrame,
		Rows:       frameRowsFromDrafts(frame.Rows),
		ScreenCols: frame.Cols,
		ScreenRows: len(frame.Rows),
		SourceSeq:  frame.Seq,
		CreatedAt:  frame.CreatedAt,
	}
}

func (store *inMemoryHistoryStore) windowFromProjectedRows(req HistoryWindowRequest, rows []HistoryRow, op HistoryWindowOp, boundary HistoryBoundary, generation Generation) HistoryWindow {
	return HistoryWindow{
		TerminalID:   req.TerminalID,
		Token:        req.Token,
		Op:           op,
		Cols:         req.Cols,
		Rows:         cloneHistoryRows(rows),
		Lines:        spansForRows(rows),
		Generation:   generation,
		Boundary:     boundary,
		HasMore:      boundary.Cursor.Valid,
		LogicalTotal: len(rows),
		Timestamp:    time.Now().UTC(),
	}
}

func annotateProjectionRowIndexes(rows []HistoryRow, start int) {
	for index := range rows {
		rows[index].ProjectionRowIndex = start + index
	}
}

func (store *inMemoryHistoryStore) reindexCounters() {
	for id := range store.lines {
		if store.nextLineID <= id {
			store.nextLineID = id + 1
		}
	}
	for _, record := range store.timeline {
		if store.nextRecordID <= record.ID {
			store.nextRecordID = record.ID + 1
		}
		if store.nextFrameID <= record.FrameID {
			store.nextFrameID = record.FrameID + 1
		}
	}
	for _, record := range store.frameRecords {
		if store.nextFrameID <= record.FrameID {
			store.nextFrameID = record.FrameID + 1
		}
		if store.nextSessionID <= record.SessionID {
			store.nextSessionID = record.SessionID + 1
		}
	}
}

func rowsFromMutableFrame(frame MutableFrame, segment HistorySegment, kind LineKind, committed bool) []HistoryRow {
	rows := make([]HistoryRow, 0, len(frame.Rows))
	for _, draft := range frame.Rows {
		rows = append(rows, rowFromLogicalLine(draft.Line, segment, kind, committed, frame.SessionID, frame.ID, true))
	}
	return rows
}

func rowsFromTransientFrame(frame TransientFrame) []HistoryRow {
	rows := make([]HistoryRow, 0, len(frame.Rows))
	for _, draft := range frame.Rows {
		rows = append(rows, rowFromLogicalLine(draft.Line, HistorySegmentCurrentAltFrame, LineKindAltScreenFrame, false, 0, frame.ID, true))
	}
	return rows
}

func rowFromLogicalLine(line LogicalLine, segment HistorySegment, kind LineKind, committed bool, sessionID ScreenSessionID, frameID ScreenFrameID, fixed bool) HistoryRow {
	return HistoryRow{
		Cells:      cloneHistoryCells(line.Cells),
		Kind:       kind,
		Segment:    segment,
		LineID:     line.ID,
		SessionID:  sessionID,
		FrameID:    frameID,
		FixedGrid:  fixed,
		ScreenCols: line.ScreenCols,
		Committed:  committed,
	}
}

func boundaryForRows(rows []HistoryRow, generation Generation, token HistoryToken) HistoryBoundary {
	if len(rows) == 0 {
		return HistoryBoundary{Cursor: HistoryCursor{Generation: generation, Token: token}}
	}
	return HistoryBoundary{
		FirstLineID: rows[0].LineID,
		LastLineID:  rows[len(rows)-1].LineID,
		Cursor: HistoryCursor{
			Segment:    rows[0].Segment,
			SessionID:  rows[0].SessionID,
			FrameID:    rows[0].FrameID,
			LineID:     rows[0].LineID,
			Generation: generation,
			Token:      token,
			Valid:      true,
		},
	}
}

func spansForRows(rows []HistoryRow) []HistoryLineSpan {
	spans := make([]HistoryLineSpan, 0, len(rows))
	for i, row := range rows {
		spans = append(spans, HistoryLineSpan{
			StartRow:      i,
			EndRow:        i,
			Kind:          row.Kind,
			Segment:       row.Segment,
			LogicalLineID: row.LineID,
			SessionID:     row.SessionID,
			FrameID:       row.FrameID,
		})
	}
	return spans
}

func frameRowsFromDrafts(drafts []LogicalLineDraft) [][]Cell {
	rows := make([][]Cell, 0, len(drafts))
	for _, draft := range drafts {
		rows = append(rows, cloneHistoryCells(draft.Line.Cells))
	}
	return rows
}

func cloneHistoryRows(rows []HistoryRow) []HistoryRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]HistoryRow, len(rows))
	for i, row := range rows {
		out[i] = row
		out[i].Cells = cloneHistoryCells(row.Cells)
	}
	return out
}

func rowsBetweenCursors(rows []HistoryRow, start HistoryCursor, end HistoryCursor) []HistoryRow {
	if !start.Valid && !end.Valid {
		return cloneHistoryRows(rows)
	}
	startIndex := rowIndexByLineID(rows, start.LineID)
	endIndex := rowIndexByLineID(rows, end.LineID)
	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex < 0 {
		endIndex = len(rows) - 1
	}
	if startIndex > endIndex {
		startIndex, endIndex = endIndex, startIndex
	}
	if endIndex >= len(rows) {
		endIndex = len(rows) - 1
	}
	if startIndex < 0 || startIndex >= len(rows) || endIndex < startIndex {
		return nil
	}
	return cloneHistoryRows(rows[startIndex : endIndex+1])
}

func rowIndexByLineID(rows []HistoryRow, id LogicalLineID) int {
	for i, row := range rows {
		if row.LineID == id {
			return i
		}
	}
	return -1
}

func rowText(cells []Cell) string {
	var out string
	for _, cell := range cells {
		out += cell.Text
	}
	return out
}

func lineKindFromRecord(kind HistoryRecordKind) LineKind {
	switch kind {
	case HistoryRecordArchivedPrimaryFrame:
		return LineKindArchivedScreenFrame
	case HistoryRecordClosedPrimaryFrame:
		return LineKindScreenFrame
	default:
		return LineKindOrdinary
	}
}

func lastSealedLineID(rows []HistoryRow) LogicalLineID {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Committed {
			return rows[i].LineID
		}
	}
	return 0
}

func mutableLineIDs(rows []HistoryRow) []LogicalLineID {
	var ids []LogicalLineID
	for _, row := range rows {
		if !row.Committed {
			ids = append(ids, row.LineID)
		}
	}
	return ids
}

func normalizedLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	return limit
}
