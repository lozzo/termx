package history

import vterm "github.com/lozzow/termx/termx-vterm/vterm"

// HistoryJournalRenderer 把 compact semantic history journal 转成 store mutation。
// domain owner 是 history；truth source 是 history semantic consumer 后的 HistoryJournal。
// R404 后它必须覆盖 ordinary stream、scroll-out proof、primary/alt frame、
// clear/resize/final frame 等 compact journal 命令；失败条件是回退 raw PTY、
// live snapshot 或第二套 open-line 状态。
type HistoryJournalRenderer interface {
	// ApplyJournal 把 compact journal 转成 HistoryMutationBatch。
	ApplyJournal(journal HistoryJournal) (HistoryMutationBatch, error)
}

// NewHistoryJournalRenderer 创建独立 compact journal renderer。
// 调用边界：仅适合独立 domain harness。生产 Terminal 应使用 NewHistoryRenderers
// 创建共享 allocator 的 lifecycle/journal renderer pair；PTY/resize history 只能走
// compact journal，不能在 journal renderer 失败时回到 full transaction 路径。
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
	ids         *historyIDAllocator
	stream      StreamLineReducer
	frames      FrameReducer
	screen      *ScreenHistoryBuffer
	currentSeq  uint64
	currentSize TerminalSemanticSize
	inAlt       bool
	inSync      bool
}

func (renderer *journalRenderer) ApplyJournal(journal HistoryJournal) (HistoryMutationBatch, error) {
	if renderer == nil {
		return HistoryMutationBatch{}, nil
	}
	renderer.currentSeq = journal.Seq
	renderer.currentSize = journal.Size
	renderer.ensureScreenBuffer(journal.Size)
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

func (renderer *journalRenderer) applyJournalItem(item HistoryJournalItem) ([]HistoryMutation, error) {
	switch item.Kind {
	case HistoryJournalItemOrdinaryLineBatch:
		if item.Ordinary == nil {
			return nil, ErrHistoryInvalidMutation
		}
		return renderer.applyOrdinaryLineBatch(*item.Ordinary)
	case HistoryJournalItemBoundary:
		if item.Boundary == nil {
			return nil, ErrHistoryInvalidMutation
		}
		return renderer.applyBoundary(*item.Boundary)
	case HistoryJournalItemFrameEvent:
		if item.Frame == nil {
			return nil, ErrHistoryInvalidMutation
		}
		return renderer.applyBoundaryFrameEvent(*item.Frame)
	case HistoryJournalItemScrollOutProof:
		if item.ScrollOut == nil {
			return nil, ErrHistoryInvalidMutation
		}
		return renderer.applyScrollOutProof(*item.ScrollOut)
	default:
		return nil, ErrHistoryInvalidMutation
	}
}

func (renderer *journalRenderer) applyScrollOutProof(proof HistoryJournalScrollOutProof) ([]HistoryMutation, error) {
	if renderer.stream == nil {
		return nil, nil
	}
	rows := cloneTerminalSemanticScrollOuts(proof.Rows)
	if proof.ClearTime && renderer.frames != nil {
		rows = renderer.frames.FilterPrimaryScrollOutRows(rows)
	}
	var mutations []HistoryMutation
	for _, row := range rows {
		next, err := renderer.stream.SealScrollOut(row)
		if err != nil {
			return nil, err
		}
		mutations = append(mutations, next...)
	}
	return mutations, nil
}

func (renderer *journalRenderer) applyBoundary(boundary HistoryJournalBoundary) ([]HistoryMutation, error) {
	switch boundary.Kind {
	case HistoryJournalBoundaryED2:
		renderer.clearPrimaryScreenBuffer()
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
		return nil, ErrHistoryInvalidMutation
	}
}

func (renderer *journalRenderer) applyBoundaryFrameEvent(event HistoryJournalFrameEvent) ([]HistoryMutation, error) {
	switch event.Kind {
	case HistoryJournalFrameReplacePrimary:
		if event.Frame == nil {
			return nil, nil
		}
		if len(event.TouchedRows) > 0 {
			return renderer.replacePrimaryTouchedRows(*event.Frame, event.TouchedRows, FrameReasonPrimaryRepaint)
		}
		return renderer.replacePrimaryCurrent(*event.Frame, FrameReasonPrimaryRepaint)
	case HistoryJournalFrameArchivePrimary:
		return renderer.archivePrimaryForBoundary(SealReasonAltEnter)
	case HistoryJournalFrameClearPrimary:
		return renderer.clearPrimaryForBoundary(FrameReasonPrimaryRepaint)
	case HistoryJournalFrameReplaceAlt:
		if event.Frame == nil {
			return nil, nil
		}
		return renderer.replaceAltCurrent(*event.Frame)
	case HistoryJournalFrameClearAlt:
		return renderer.clearAltForBoundary(FrameReasonAltExit)
	case HistoryJournalFrameClosePrimary:
		if event.Frame != nil {
			return renderer.closePrimaryFromFrameForBoundary(*event.Frame, event.TouchedRows, SealReasonSessionClose)
		}
		return renderer.closePrimaryForBoundary(SealReasonSessionClose)
	case HistoryJournalFrameFinalPrimary:
		return renderer.closePrimaryForBoundary(SealReasonTerminalClose)
	default:
		return nil, ErrHistoryInvalidMutation
	}
}

func (renderer *journalRenderer) replacePrimaryCurrent(frame TerminalSemanticFrame, reason FrameReason) ([]HistoryMutation, error) {
	if renderer.frames == nil {
		return nil, nil
	}
	screenFrame, err := renderer.applyPrimaryFrameToScreen(frame, nil)
	if err != nil {
		return nil, err
	}
	return renderer.frames.ReplacePrimaryCurrent(screenFrame, reason)
}

func (renderer *journalRenderer) replacePrimaryTouchedRows(frame TerminalSemanticFrame, rows []int, reason FrameReason) ([]HistoryMutation, error) {
	if renderer.frames == nil {
		return nil, nil
	}
	screenFrame, err := renderer.applyPrimaryFrameToScreen(frame, rows)
	if err != nil {
		return nil, err
	}
	return renderer.frames.ReplacePrimaryTouchedRows(screenFrame, rows, reason)
}

func (renderer *journalRenderer) replaceAltCurrent(frame TerminalSemanticFrame) ([]HistoryMutation, error) {
	if renderer.frames == nil {
		return nil, nil
	}
	return renderer.frames.ReplaceAltCurrent(frame)
}

func (renderer *journalRenderer) closePrimaryForBoundary(reason SealReason) ([]HistoryMutation, error) {
	if renderer.frames == nil {
		return nil, nil
	}
	return renderer.frames.ClosePrimaryCurrent(reason)
}

func (renderer *journalRenderer) closePrimaryFromFrameForBoundary(frame TerminalSemanticFrame, excludedRows []int, reason SealReason) ([]HistoryMutation, error) {
	if renderer.frames == nil {
		return nil, nil
	}
	return renderer.frames.ClosePrimaryCurrentFromFrameExcludingRows(frame, excludedRows, reason)
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

func (renderer *journalRenderer) ensureScreenBuffer(size TerminalSemanticSize) {
	if renderer == nil {
		return
	}
	cols := size.Cols
	rows := size.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	if renderer.screen == nil || renderer.screen.Cols != cols || renderer.screen.Rows != rows {
		renderer.screen = NewScreenHistoryBuffer(cols, rows)
	}
}

func (renderer *journalRenderer) applyPrimaryFrameToScreen(frame TerminalSemanticFrame, touchedRows []int) (TerminalSemanticFrame, error) {
	renderer.ensureScreenBuffer(renderer.currentSize)
	if renderer.screen == nil {
		return frame, nil
	}
	if frame.Cols > 0 && frame.Cols != renderer.screen.Cols {
		renderer.screen = NewScreenHistoryBuffer(frame.Cols, maxInt(1, len(frame.Rows)))
	}
	// 中文说明：primary repaint 的正文 truth 先进入 ScreenHistoryBuffer；
	// FrameReducer 在 R424 只作为旧 HistoryStore 的过渡 projection adapter。
	if err := renderer.screen.applyPrimaryFrameRows(frame, touchedRows, renderer.currentSeq); err != nil {
		return TerminalSemanticFrame{}, err
	}
	return renderer.primaryFrameFromScreen(), nil
}

func (renderer *journalRenderer) clearPrimaryScreenBuffer() {
	renderer.ensureScreenBuffer(renderer.currentSize)
	if renderer.screen != nil {
		renderer.screen.clearPrimaryFrameRows(renderer.currentSeq)
	}
}

func (renderer *journalRenderer) primaryFrameFromScreen() TerminalSemanticFrame {
	if renderer == nil || renderer.screen == nil || renderer.screen.Main == nil {
		return TerminalSemanticFrame{}
	}
	rows := make([][]TerminalSemanticCell, len(renderer.screen.Main.Rows))
	for index, row := range renderer.screen.Main.Rows {
		rows[index] = terminalCellsFromHistoryCells(row.Cells)
	}
	return TerminalSemanticFrame{Cols: renderer.screen.Cols, Rows: rows}
}

func (renderer *journalRenderer) resetReducersForClearScrollback() {
	if renderer.stream != nil {
		renderer.stream.ResetForClearScrollback()
	}
	if renderer.frames != nil {
		renderer.frames.ResetForClearScrollback()
	}
	renderer.screen = nil
}

func (renderer *journalRenderer) applyOrdinaryLineBatch(batch OrdinaryLineBatch) ([]HistoryMutation, error) {
	if renderer.inAlt || renderer.inSync {
		// 中文说明：alt/sync scope 内的文字 payload 必须由同一 journal 的
		// frame proof 表达；即使旧 harness 手工构造了 ordinary batch，也只能
		// 在当前 reducer 边界内忽略，不能回到另一套 transaction renderer。
		return nil, nil
	}
	var mutations []HistoryMutation
	if len(batch.Commands) == 0 {
		for _, line := range batch.Lines {
			mutations = append(mutations, renderer.sealJournalLine(line, SealReasonLineFeed)...)
		}
		if batch.OpenUpdate != nil {
			next, err := renderer.applyOpenLineUpdate(*batch.OpenUpdate)
			if err != nil {
				return nil, err
			}
			mutations = append(mutations, next...)
		}
		return mutations, nil
	}
	for _, command := range batch.Commands {
		next, err := renderer.applyOpenLineCommand(command)
		if err != nil {
			return nil, err
		}
		mutations = append(mutations, next...)
	}
	if batch.OpenUpdate != nil {
		next, err := renderer.stream.FlushOpenLine()
		if err != nil {
			return nil, err
		}
		mutations = append(mutations, next...)
	}
	return mutations, nil
}

func (renderer *journalRenderer) applyOpenLineCommand(command JournalOpenLineCommand) ([]HistoryMutation, error) {
	if renderer.stream == nil {
		return nil, nil
	}
	op, ok := terminalOpFromJournalCommand(command)
	if !ok {
		return nil, nil
	}
	return renderer.stream.ApplyOp(op)
}

func (renderer *journalRenderer) sealOpenLine(reason SealReason) []HistoryMutation {
	if renderer.stream == nil {
		return nil
	}
	mutations, err := renderer.stream.SealOpenLine(reason)
	if err != nil {
		return nil
	}
	return mutations
}

func (renderer *journalRenderer) sealJournalLine(line JournalLogicalLine, reason SealReason) []HistoryMutation {
	logical := logicalLineTemplate(HistoryRecordOrdinaryLine)
	logical.ID = renderer.ids.nextLogicalLineID()
	logical.Cells = trimTrailingBlankCellsInPlace(line.Cells)
	logical.Runs = cloneCellRuns(line.Runs)
	logical.TailFill = cloneRowTailFill(line.TailFill)
	if len(logical.Cells) == 0 && len(logical.Runs) == 0 {
		return nil
	}
	return renderer.sealStandaloneLine(logical, reason, HistoryRecordOrdinaryLine)
}

func (renderer *journalRenderer) sealStandaloneLine(line LogicalLine, reason SealReason, kind HistoryRecordKind) []HistoryMutation {
	line.Seal = SealStateSealed
	line.Kind = string(kind)
	lineCopy := line
	record := HistoryRecord{
		ID:      renderer.ids.nextHistoryRecordID(),
		Seq:     renderer.ids.nextTimelineSeq(),
		Kind:    kind,
		LineIDs: []LogicalLineID{line.ID},
		Reason:  reason,
	}
	recordCopy := cloneHistoryRecord(record)
	return []HistoryMutation{
		{
			Kind:   HistoryMutationSealLine,
			Line:   &lineCopy,
			Reason: reason,
		},
		{
			Kind:   HistoryMutationAppendTimelineRecord,
			Record: &recordCopy,
			Reason: reason,
		},
	}
}

func (renderer *journalRenderer) applyOpenLineUpdate(update JournalOpenLineUpdate) ([]HistoryMutation, error) {
	if renderer.stream == nil {
		return nil, nil
	}
	if len(update.Cells) > 0 {
		if _, err := renderer.stream.ApplyOp(TerminalSemanticOp{
			Code:  vterm.ScreenOpWriteSpan,
			Row:   update.Row,
			Col:   0,
			Cells: terminalCellsFromHistoryCells(update.Cells),
		}); err != nil {
			return nil, err
		}
	}
	return renderer.stream.FlushOpenLine()
}

func terminalOpFromJournalCommand(command JournalOpenLineCommand) (TerminalSemanticOp, bool) {
	switch command.Kind {
	case JournalOpenLineCommandWrite:
		return TerminalSemanticOp{
			Code:  vterm.ScreenOpWriteSpan,
			Row:   command.Row,
			Col:   command.Col,
			Cells: terminalCellsFromHistoryCells(command.Cells),
		}, true
	case JournalOpenLineCommandSetCursor:
		return TerminalSemanticOp{Code: vterm.ScreenOpControl, Control: "cup", Row: command.Row, Col: command.Col}, true
	case JournalOpenLineCommandMoveCol:
		if command.Delta < 0 {
			return TerminalSemanticOp{Code: vterm.ScreenOpControl, Control: "cub", Mode: -command.Delta}, true
		}
		return TerminalSemanticOp{Code: vterm.ScreenOpControl, Control: "cuf", Mode: command.Delta}, true
	case JournalOpenLineCommandMoveRow:
		if command.Delta < 0 {
			return TerminalSemanticOp{Code: vterm.ScreenOpControl, Control: "cuu", Mode: -command.Delta}, true
		}
		return TerminalSemanticOp{Code: vterm.ScreenOpControl, Control: "cud", Mode: command.Delta}, true
	case JournalOpenLineCommandEraseLine:
		return TerminalSemanticOp{Code: vterm.ScreenOpControl, Control: "el", Row: command.Row, Col: command.Col, Mode: command.Mode}, true
	case JournalOpenLineCommandSoftWrap:
		return TerminalSemanticOp{Code: vterm.ScreenOpControl, Control: "soft-wrap", Row: command.Row, Col: command.Col, TailFill: terminalTailFillFromHistory(command.TailFill)}, true
	case JournalOpenLineCommandSealLine:
		return TerminalSemanticOp{Code: vterm.ScreenOpControl, Control: "lf", TailFill: terminalTailFillFromHistory(command.TailFill)}, true
	default:
		return TerminalSemanticOp{}, false
	}
}

func terminalCellsFromHistoryCells(cells []Cell) []TerminalSemanticCell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]TerminalSemanticCell, 0, len(cells))
	for _, cell := range cells {
		out = append(out, TerminalSemanticCell{
			Content:    cell.Text,
			Width:      cell.Width,
			Style:      terminalStyleFromHistory(cell.Style),
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
		})
	}
	return out
}

func terminalTailFillFromHistory(fill *RowTailFill) *TerminalSemanticStyle {
	if fill == nil {
		return nil
	}
	style := terminalStyleFromHistory(fill.Style)
	return &style
}

func terminalStyleFromHistory(style CellStyle) TerminalSemanticStyle {
	return TerminalSemanticStyle{
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
