package history

// HistoryJournalRenderer 把 compact semantic history journal 转成 store mutation。
// domain owner 是 history；truth source 是 single SemanticTap 后的 HistoryJournal。
// R383 支持 ordinary line batch 与不依赖 frame payload 的 boundary state machine；
// scroll-out proof 和 frame event 仍是 R384 后续边界，遇到时必须原子退出。
type HistoryJournalRenderer interface {
	// ApplyJournal 把 compact journal 转成 HistoryMutationBatch。若 journal 含有
	// 尚未接管的 scroll-out/frame item，返回 ErrHistoryJournalUnsupported，且不能
	// 改写 renderer-owned open line 或 boundary 状态。
	ApplyJournal(journal HistoryJournal) (HistoryMutationBatch, error)
}

// NewHistoryJournalRenderer 创建独立 compact journal renderer。
// 调用边界：仅适合独立 domain harness。生产 Terminal 应使用 NewHistoryRenderers
// 创建共享 allocator 的 transaction/journal renderer pair，避免 fallback 混用时
// logical line id、record id 和 timeline seq 冲突。
func NewHistoryJournalRenderer() HistoryJournalRenderer {
	return newHistoryJournalRenderer(newHistoryIDAllocator())
}

func newHistoryJournalRenderer(allocator *historyIDAllocator) HistoryJournalRenderer {
	return newHistoryJournalRendererWithReducers(allocator, nil, nil)
}

func newHistoryJournalRendererWithReducers(allocator *historyIDAllocator, stream StreamLineReducer, frames FrameReducer) HistoryJournalRenderer {
	if allocator == nil {
		allocator = newHistoryIDAllocator()
	}
	if stream == nil {
		stream = newStreamLineReducerWithAllocator(allocator)
	}
	if frames == nil {
		frames = newFrameReducerWithAllocator(allocator)
	}
	return &journalRenderer{
		ids:    allocator,
		stream: stream,
		frames: frames,
	}
}

type journalRenderer struct {
	ids      *historyIDAllocator
	stream   StreamLineReducer
	frames   FrameReducer
	openLine *journalOpenLineState
	inAlt    bool
	inSync   bool
}

type journalOpenLineState struct {
	lineID LogicalLineID
	cells  []Cell
	row    int
	cursor int
	fill   *RowTailFill
}

func (renderer *journalRenderer) ApplyJournal(journal HistoryJournal) (HistoryMutationBatch, error) {
	if renderer == nil {
		return HistoryMutationBatch{}, nil
	}
	if !renderer.supportsJournal(journal) {
		return HistoryMutationBatch{}, ErrHistoryJournalUnsupported
	}
	var mutations []HistoryMutation
	for _, item := range journal.Items {
		next, err := renderer.applyJournalItem(item)
		if err != nil {
			return HistoryMutationBatch{}, err
		}
		mutations = append(mutations, next...)
	}
	return HistoryMutationBatch{Seq: journal.Seq, Mutations: mutations}, nil
}

func (renderer *journalRenderer) supportsJournal(journal HistoryJournal) bool {
	// 中文说明：journal renderer 必须先全量校验再应用，避免普通 batch 已改写
	// renderer-owned open line 后遇到 frame/scroll-out 才 fallback，形成半事务双写。
	state := journalBoundaryScanState{inAlt: renderer != nil && renderer.inAlt, inSync: renderer != nil && renderer.inSync}
	for _, item := range journal.Items {
		if !state.accept(item) {
			return false
		}
	}
	return true
}

type journalBoundaryScanState struct {
	inAlt  bool
	inSync bool
}

func (state *journalBoundaryScanState) accept(item HistoryJournalItem) bool {
	switch item.Kind {
	case HistoryJournalItemOrdinaryLineBatch:
		return item.Ordinary != nil && !state.inAlt && !state.inSync
	case HistoryJournalItemBoundary:
		if item.Boundary == nil {
			return false
		}
		return state.acceptBoundary(item.Boundary.Kind)
	case HistoryJournalItemFrameEvent:
		return item.Frame != nil && journalFrameEventIsBoundaryOnly(item.Frame.Kind)
	case HistoryJournalItemScrollOutProof:
		return false
	default:
		return false
	}
}

func (state *journalBoundaryScanState) acceptBoundary(kind HistoryJournalBoundaryKind) bool {
	switch kind {
	case HistoryJournalBoundaryED2, HistoryJournalBoundaryED3, HistoryJournalBoundaryRIS, HistoryJournalBoundaryResize:
		return true
	case HistoryJournalBoundaryAltEnter:
		state.inAlt = true
		return true
	case HistoryJournalBoundaryAltExit:
		state.inAlt = false
		return true
	case HistoryJournalBoundarySyncBegin:
		state.inSync = true
		return true
	case HistoryJournalBoundarySyncEnd:
		state.inSync = false
		return true
	default:
		return false
	}
}

func (renderer *journalRenderer) applyJournalItem(item HistoryJournalItem) ([]HistoryMutation, error) {
	switch item.Kind {
	case HistoryJournalItemOrdinaryLineBatch:
		return renderer.applyOrdinaryLineBatch(*item.Ordinary)
	case HistoryJournalItemBoundary:
		return renderer.applyBoundary(*item.Boundary)
	case HistoryJournalItemFrameEvent:
		return renderer.applyBoundaryFrameEvent(*item.Frame)
	default:
		return nil, ErrHistoryJournalUnsupported
	}
}

func (renderer *journalRenderer) applyBoundary(boundary HistoryJournalBoundary) ([]HistoryMutation, error) {
	switch boundary.Kind {
	case HistoryJournalBoundaryED2:
		mutations := renderer.sealOpenLine(SealReasonFullReplace)
		cleared, err := renderer.clearPrimaryForBoundary(FrameReasonPrimaryRepaint)
		if err != nil {
			return nil, err
		}
		return append(mutations, cleared...), nil
	case HistoryJournalBoundaryED3:
		return []HistoryMutation{{Kind: HistoryMutationClearScrollback, Reason: SealReasonFullReplace}}, nil
	case HistoryJournalBoundaryRIS:
		mutations := renderer.sealOpenLine(SealReasonFullReplace)
		renderer.inAlt = false
		renderer.inSync = false
		renderer.resetReducersForClearScrollback()
		return append(mutations, []HistoryMutation{
			{Kind: HistoryMutationClearPrimaryFrame, Reason: SealReasonFullReplace},
			{Kind: HistoryMutationClearAltFrame, Reason: SealReasonFullReplace},
		}...), nil
	case HistoryJournalBoundaryResize:
		return renderer.applyNonHistoryBoundary(FrameReasonResize)
	case HistoryJournalBoundaryAltEnter:
		renderer.inAlt = true
		return renderer.archivePrimaryForBoundary(SealReasonAltEnter)
	case HistoryJournalBoundaryAltExit:
		renderer.inAlt = false
		return renderer.clearAltForBoundary(FrameReasonAltExit)
	case HistoryJournalBoundarySyncBegin:
		renderer.inSync = true
		return nil, nil
	case HistoryJournalBoundarySyncEnd:
		renderer.inSync = false
		return nil, nil
	default:
		return nil, ErrHistoryJournalUnsupported
	}
}

func journalFrameEventIsBoundaryOnly(kind HistoryJournalFrameEventKind) bool {
	switch kind {
	case HistoryJournalFrameArchivePrimary, HistoryJournalFrameClearPrimary, HistoryJournalFrameClearAlt:
		return true
	default:
		return false
	}
}

func (renderer *journalRenderer) applyBoundaryFrameEvent(event HistoryJournalFrameEvent) ([]HistoryMutation, error) {
	switch event.Kind {
	case HistoryJournalFrameArchivePrimary:
		return renderer.archivePrimaryForBoundary(SealReasonAltEnter)
	case HistoryJournalFrameClearPrimary:
		return renderer.clearPrimaryForBoundary(FrameReasonPrimaryRepaint)
	case HistoryJournalFrameClearAlt:
		return renderer.clearAltForBoundary(FrameReasonAltExit)
	default:
		return nil, ErrHistoryJournalUnsupported
	}
}

func (renderer *journalRenderer) clearPrimaryForBoundary(reason FrameReason) ([]HistoryMutation, error) {
	if renderer.frames == nil {
		return nil, nil
	}
	return renderer.frames.ClearPrimaryCurrent(reason)
}

func (renderer *journalRenderer) archivePrimaryForBoundary(reason SealReason) ([]HistoryMutation, error) {
	if renderer.frames == nil {
		return nil, nil
	}
	return renderer.frames.ArchivePrimaryCurrent(reason)
}

func (renderer *journalRenderer) clearAltForBoundary(reason FrameReason) ([]HistoryMutation, error) {
	if renderer.frames == nil {
		return nil, nil
	}
	return renderer.frames.ClearAltCurrent(reason)
}

func (renderer *journalRenderer) applyNonHistoryBoundary(reason FrameReason) ([]HistoryMutation, error) {
	if renderer.frames == nil {
		return []HistoryMutation{{Kind: HistoryMutationNonHistoryBoundary, Reason: sealReasonFromFrameReason(reason)}}, nil
	}
	return renderer.frames.ApplyNonHistoryBoundary(reason)
}

func (renderer *journalRenderer) resetReducersForClearScrollback() {
	if renderer.stream != nil {
		renderer.stream.ResetForClearScrollback()
	}
	if renderer.frames != nil {
		renderer.frames.ResetForClearScrollback()
	}
}

func (renderer *journalRenderer) resetOpenLine() {
	renderer.openLine = nil
}

func (renderer *journalRenderer) applyOrdinaryLineBatch(batch OrdinaryLineBatch) ([]HistoryMutation, error) {
	if renderer.openLine == nil && batch.OpenUpdate == nil && len(batch.Lines) > 0 {
		return renderer.applyOrdinaryLineBatchSnapshot(batch), nil
	}
	if len(batch.Commands) == 0 {
		return renderer.applyOrdinaryLineBatchSnapshot(batch), nil
	}
	var mutations []HistoryMutation
	for _, command := range batch.Commands {
		next := renderer.applyOpenLineCommand(command)
		mutations = append(mutations, next...)
	}
	if batch.OpenUpdate != nil && renderer.openLine != nil {
		// 中文说明：journal command 逐步推进 renderer-owned open line；transaction
		// 末尾只发布一次 mutable frontier 给 store，避免普通输出每个 op 都
		// clone/open-line upsert。
		mutations = append(mutations, renderer.openLineMutation())
	}
	return mutations, nil
}

func (renderer *journalRenderer) applyOrdinaryLineBatchSnapshot(batch OrdinaryLineBatch) []HistoryMutation {
	var mutations []HistoryMutation
	for _, line := range batch.Lines {
		mutations = append(mutations, renderer.sealJournalLine(line, SealReasonLineFeed)...)
	}
	if batch.OpenUpdate != nil {
		state := renderer.ensureOpenLine()
		state.cells = cloneHistoryCells(batch.OpenUpdate.Cells)
		state.cursor = batch.OpenUpdate.CursorCol
		state.row = batch.OpenUpdate.Row
		state.fill = cloneRowTailFill(batch.OpenUpdate.TailFill)
		mutations = append(mutations, renderer.openLineMutation())
	}
	return mutations
}

func (renderer *journalRenderer) applyOpenLineCommand(command JournalOpenLineCommand) []HistoryMutation {
	switch command.Kind {
	case JournalOpenLineCommandWrite:
		state := renderer.ensureOpenLine()
		state.row = command.Row
		state.cells = writeCellsAt(state.cells, command.Col, command.Cells)
		state.cells = trimTrailingBlankCells(state.cells)
		state.cursor = command.Col + historyCellsDisplayWidth(command.Cells)
	case JournalOpenLineCommandSetCursor:
		state := renderer.ensureOpenLine()
		state.row = maxInt(0, command.Row)
		state.cursor = maxInt(0, command.Col)
	case JournalOpenLineCommandMoveCol:
		state := renderer.ensureOpenLine()
		state.cursor = maxInt(0, state.cursor+command.Delta)
	case JournalOpenLineCommandMoveRow:
		state := renderer.ensureOpenLine()
		state.row = maxInt(0, state.row+command.Delta)
	case JournalOpenLineCommandEraseLine:
		state := renderer.ensureOpenLine()
		state.cells = eraseJournalCells(state.cells, command.Col, command.Mode)
		if state.cursor > historyCellsDisplayWidth(state.cells) {
			state.cursor = historyCellsDisplayWidth(state.cells)
		}
	case JournalOpenLineCommandSealLine:
		if command.TailFill != nil {
			state := renderer.ensureOpenLine()
			state.fill = cloneRowTailFill(command.TailFill)
		}
		return renderer.sealOpenLine(SealReasonLineFeed)
	}
	return nil
}

func (renderer *journalRenderer) ensureOpenLine() *journalOpenLineState {
	if renderer.openLine == nil {
		renderer.openLine = &journalOpenLineState{
			lineID: renderer.ids.nextLogicalLineID(),
		}
	}
	return renderer.openLine
}

func (renderer *journalRenderer) sealOpenLine(reason SealReason) []HistoryMutation {
	if renderer.openLine == nil {
		return nil
	}
	line := renderer.logicalLineFromOpenState()
	renderer.openLine = nil
	if len(line.Cells) == 0 {
		return nil
	}
	return renderer.sealStandaloneLine(line, reason, HistoryRecordOrdinaryLine)
}

func (renderer *journalRenderer) sealJournalLine(line JournalLogicalLine, reason SealReason) []HistoryMutation {
	logical := logicalLineTemplate(HistoryRecordOrdinaryLine)
	logical.ID = renderer.ids.nextLogicalLineID()
	logical.Cells = trimTrailingBlankCells(cloneHistoryCells(line.Cells))
	logical.TailFill = cloneRowTailFill(line.TailFill)
	if len(logical.Cells) == 0 {
		return nil
	}
	return renderer.sealStandaloneLine(logical, reason, HistoryRecordOrdinaryLine)
}

func (renderer *journalRenderer) logicalLineFromOpenState() LogicalLine {
	line := logicalLineTemplate(HistoryRecordOrdinaryLine)
	line.ID = renderer.openLine.lineID
	line.Cells = trimTrailingBlankCells(cloneHistoryCells(renderer.openLine.cells))
	line.TailFill = cloneRowTailFill(renderer.openLine.fill)
	line.Seal = SealStateOpen
	return line
}

func (renderer *journalRenderer) openLineMutation() HistoryMutation {
	state := renderer.ensureOpenLine()
	line := renderer.logicalLineFromOpenState()
	open := OpenLine{
		Active: true,
		Draft: LogicalLineDraft{
			Line:      line,
			CursorCol: state.cursor,
			Row:       state.row,
		},
	}
	openCopy := open
	openCopy.Draft.Line = cloneLogicalLine(open.Draft.Line)
	return HistoryMutation{Kind: HistoryMutationUpsertOpenLine, OpenLine: &openCopy}
}

func (renderer *journalRenderer) sealStandaloneLine(line LogicalLine, reason SealReason, kind HistoryRecordKind) []HistoryMutation {
	line.Seal = SealStateSealed
	line.Kind = string(kind)
	record := HistoryRecord{
		ID:      renderer.ids.nextHistoryRecordID(),
		Seq:     renderer.ids.nextTimelineSeq(),
		Kind:    kind,
		LineIDs: []LogicalLineID{line.ID},
		Reason:  reason,
	}
	lineCopy := cloneLogicalLine(line)
	recordCopy := cloneHistoryRecord(record)
	return []HistoryMutation{
		{
			Kind:    HistoryMutationSealLine,
			Line:    &lineCopy,
			LineIDs: []LogicalLineID{line.ID},
			Reason:  reason,
		},
		{
			Kind:    HistoryMutationAppendTimelineRecord,
			Record:  &recordCopy,
			LineIDs: []LogicalLineID{line.ID},
			Reason:  reason,
		},
	}
}
