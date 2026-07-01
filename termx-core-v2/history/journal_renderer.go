package history

// HistoryJournalRenderer 把 compact semantic history journal 转成 store mutation。
// domain owner 是 history；truth source 是 single SemanticTap 后的 HistoryJournal。
// R382 只接管 ordinary line batch，ED/RIS/alt/sync/frame/scroll-out state machine
// 留给后续切片，避免提前形成补丁式双路径。
type HistoryJournalRenderer interface {
	// ApplyJournal 把 compact journal 转成 HistoryMutationBatch。若 journal 含有
	// 尚未接管的非 ordinary item，返回 ErrHistoryJournalUnsupported。
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
	if allocator == nil {
		allocator = newHistoryIDAllocator()
	}
	return &journalRenderer{ids: allocator}
}

type journalRenderer struct {
	ids      *historyIDAllocator
	openLine *journalOpenLineState
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
	if !journalRendererSupportsJournal(journal) {
		return HistoryMutationBatch{}, ErrHistoryJournalUnsupported
	}
	var mutations []HistoryMutation
	for _, item := range journal.Items {
		next, err := renderer.applyOrdinaryLineBatch(*item.Ordinary)
		if err != nil {
			return HistoryMutationBatch{}, err
		}
		mutations = append(mutations, next...)
	}
	return HistoryMutationBatch{Seq: journal.Seq, Mutations: mutations}, nil
}

func journalRendererSupportsJournal(journal HistoryJournal) bool {
	// 中文说明：R382 renderer 只支持 ordinary batch。必须先全量校验再应用，
	// 否则遇到 ordinary+boundary 混合 journal 时会先污染 renderer-owned open
	// line，随后 full renderer fallback 再消费同一 transaction，形成双 truth。
	for _, item := range journal.Items {
		if item.Kind != HistoryJournalItemOrdinaryLineBatch || item.Ordinary == nil {
			return false
		}
	}
	return true
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
