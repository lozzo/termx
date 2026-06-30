package history

import (
	"sort"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

// NewStreamLineReducer 创建 ordinary stream 的 logical-line reducer。
// domain owner：history；truth source 只能是 vterm semantic op 和 scroll-out proof。
// 调用边界：它不读取 live snapshot、不解析 raw PTY、不判断程序名，只产出
// HistoryMutation 交给后续 authoritative store 应用。
func NewStreamLineReducer() StreamLineReducer {
	return &streamLineReducer{
		ids:       newHistoryIDAllocator(),
		rowOwners: make(map[int]LogicalLineID),
		lines:     make(map[LogicalLineID]*streamLineDraft),
		fast:      ordinaryLineSink{},
	}
}

type streamLineReducer struct {
	ids                *historyIDAllocator
	rowOwners          map[int]LogicalLineID
	lines              map[LogicalLineID]*streamLineDraft
	fast               ordinaryLineSink
	cursorRow          int
	cursorCol          int
	scrollTop          int
	scrollBottom       int
	skipNextScrollRect *pendingScrollRect
}

type streamLineDraft struct {
	id        LogicalLineID
	rows      map[int][]Cell
	rowOrder  []int
	cursorRow int
	cursorCol int
	wrapped   bool
	tailFill  *RowTailFill
}

type pendingScrollRect struct {
	rect vterm.DamageRect
	dy   int
}

// ordinaryLineSink 是 ordinary stdout 的 logical-line-first 快路径。
// domain owner 仍是 history reducer；truth source 只来自已排序的 vterm semantic op。
// 它只持有一个当前 mutable logical line，不接管 screen app、alt、清屏或滚动几何语义。
type ordinaryLineSink struct {
	active bool
	lineID LogicalLineID
	cells  []Cell
	cursor int
	fill   *RowTailFill
	stats  ordinaryFastLaneStats
}

type ordinaryFastLaneStats struct {
	AppliedOps   int
	FallbackOps  int
	SealedLines  int
	MutableLines int
}

func (reducer *streamLineReducer) ApplyOp(op TerminalSemanticOp) ([]HistoryMutation, error) {
	if reducer == nil {
		return nil, nil
	}
	reducer.ensureState()
	if reducer.canApplyOrdinaryFastLane(op) {
		return reducer.applyOrdinaryFastLane(op), nil
	}
	if reducer.fast.active {
		// 中文说明：一旦遇到 ED/EL/scroll/copy/full-replace 等屏幕几何语义，
		// fast lane 必须先物化为原 row ownership draft，再由既有复杂 reducer
		// 处理；这里不从 live snapshot 或 raw PTY 重建第二份 history truth。
		reducer.materializeFastLaneToRowOwnership()
		reducer.fast.stats.FallbackOps++
	}
	switch op.Code {
	case vterm.ScreenOpWriteSpan:
		return reducer.applyWriteSpan(op), nil
	case vterm.ScreenOpControl:
		return reducer.applyControl(op), nil
	case vterm.ScreenOpClearToEOL:
		return reducer.applyEraseLine(op.Row, op.Col, 0), nil
	case vterm.ScreenOpClearRect:
		return reducer.applyClearRect(op), nil
	case vterm.ScreenOpScrollRect:
		if reducer.consumePendingScrollRect(op.Rect, op.Dy) {
			return nil, nil
		}
		return reducer.applyScrollRect(op), nil
	case vterm.ScreenOpCopyRect:
		return reducer.applyCopyRect(op), nil
	default:
		return nil, nil
	}
}

func (reducer *streamLineReducer) SealOpenLine(reason SealReason) ([]HistoryMutation, error) {
	if reducer == nil {
		return nil, nil
	}
	reducer.ensureState()
	var mutations []HistoryMutation
	mutations = append(mutations, reducer.sealFastLane(reason)...)
	ids := reducer.sortedLineIDs()
	for _, id := range ids {
		mutations = append(mutations, reducer.sealLine(id, reason, HistoryRecordOrdinaryLine)...)
	}
	return mutations, nil
}

func (reducer *streamLineReducer) SealScrollOut(proof TerminalSemanticScrollOut) ([]HistoryMutation, error) {
	if reducer == nil {
		return nil, nil
	}
	reducer.ensureState()
	line := reducer.newLogicalLine(HistoryRecordPrimaryScrollOutLine)
	line.Cells = cellsFromScrollOutProof(proof)
	if len(line.Cells) == 0 {
		return nil, nil
	}
	line.Seal = SealStateSealed
	return reducer.sealStandaloneLine(line, SealReasonScrollOut, HistoryRecordPrimaryScrollOutLine), nil
}

func (reducer *streamLineReducer) ClearScreenOwnership() ([]HistoryMutation, error) {
	if reducer == nil {
		return nil, nil
	}
	reducer.ensureState()
	var mutations []HistoryMutation
	for _, row := range reducer.sortedOwnedRows() {
		mutations = append(mutations, reducer.clearRow(row)...)
	}
	return mutations, nil
}

func (reducer *streamLineReducer) ResetForClearScrollback() {
	if reducer == nil {
		return
	}
	reducer.ids = newHistoryIDAllocator()
	reducer.rowOwners = make(map[int]LogicalLineID)
	reducer.lines = make(map[LogicalLineID]*streamLineDraft)
	reducer.fast = ordinaryLineSink{}
	reducer.cursorRow = 0
	reducer.cursorCol = 0
}

// canApplyOrdinaryFastLane 只接受单 logical line 的顺序写入和有限光标编辑。
// 失败条件：已有 row ownership、多行 wrap、清屏、滚动、区域复制或 screen app frame
// 语义出现时必须返回 false，让复杂 reducer 继续作为这些几何语义的唯一处理边界。
func (reducer *streamLineReducer) canApplyOrdinaryFastLane(op TerminalSemanticOp) bool {
	if reducer == nil {
		return false
	}
	switch op.Code {
	case vterm.ScreenOpWriteSpan:
		return op.Row == reducer.cursorRow && op.Col == reducer.fast.cursor && len(reducer.lines) == 0 && len(reducer.rowOwners) == 0
	case vterm.ScreenOpClearToEOL:
		return reducer.fast.active && op.Row == reducer.cursorRow && len(reducer.lines) == 0 && len(reducer.rowOwners) == 0
	case vterm.ScreenOpControl:
		switch op.Control {
		case "cr", "lf", "ind", "nel", "bs", "cub", "cuf", "cha", "hpa", "cup", "vpa", "cuu", "cud":
			return len(reducer.lines) == 0 && len(reducer.rowOwners) == 0
		case "el":
			return reducer.fast.active && op.Row == reducer.cursorRow && len(reducer.lines) == 0 && len(reducer.rowOwners) == 0
		}
	}
	return false
}

func (reducer *streamLineReducer) applyOrdinaryFastLane(op TerminalSemanticOp) []HistoryMutation {
	reducer.fast.stats.AppliedOps++
	switch op.Code {
	case vterm.ScreenOpWriteSpan:
		reducer.ensureFastLine()
		reducer.fast.cells = writeCellsAt(reducer.fast.cells, op.Col, historyCellsFromTerminal(op.Cells))
		reducer.fast.cells = trimTrailingBlankCells(reducer.fast.cells)
		reducer.fast.cursor = op.Col + terminalCellsDisplayWidth(op.Cells)
		reducer.cursorRow = op.Row
		reducer.cursorCol = reducer.fast.cursor
		reducer.fast.stats.MutableLines++
		return nil
	case vterm.ScreenOpControl:
		return reducer.applyOrdinaryFastLaneControl(op)
	case vterm.ScreenOpClearToEOL:
		return reducer.applyOrdinaryFastLaneEraseLine(op.Row, op.Col, 0)
	}
	return nil
}

func (reducer *streamLineReducer) applyOrdinaryFastLaneControl(op TerminalSemanticOp) []HistoryMutation {
	switch op.Control {
	case "cr":
		reducer.cursorRow = op.Row
		reducer.cursorCol = 0
		reducer.fast.cursor = 0
		if reducer.fast.active {
			return nil
		}
	case "lf", "ind", "nel":
		row := reducer.cursorRow
		if op.TailFill != nil {
			reducer.fast.fill = rowTailFillFromTerminal(op.TailFill)
		}
		mutations := reducer.sealFastLane(SealReasonLineFeed)
		reducer.cursorRow = row + 1
		if op.Control == "nel" {
			reducer.cursorCol = 0
			reducer.fast.cursor = 0
		}
		return mutations
	case "bs":
		reducer.fast.cursor = maxInt(0, reducer.fast.cursor-1)
		reducer.cursorCol = reducer.fast.cursor
	case "cub":
		reducer.fast.cursor = maxInt(0, reducer.fast.cursor-controlCount(op))
		reducer.cursorCol = reducer.fast.cursor
	case "cuf":
		reducer.fast.cursor += controlCount(op)
		reducer.cursorCol = reducer.fast.cursor
	case "cha", "hpa":
		reducer.cursorRow = op.Row
		reducer.fast.cursor = maxInt(0, op.Col)
		reducer.cursorCol = reducer.fast.cursor
	case "cup":
		reducer.cursorRow = maxInt(0, op.Row)
		reducer.fast.cursor = maxInt(0, op.Col)
		reducer.cursorCol = reducer.fast.cursor
	case "vpa":
		reducer.cursorRow = maxInt(0, op.Row)
	case "cuu":
		reducer.cursorRow = maxInt(0, reducer.cursorRow-controlCount(op))
	case "cud":
		reducer.cursorRow += controlCount(op)
	case "el":
		return reducer.applyOrdinaryFastLaneEraseLine(op.Row, op.Col, op.Mode)
	}
	return nil
}

func (reducer *streamLineReducer) FlushOpenLine() ([]HistoryMutation, error) {
	if reducer == nil {
		return nil, nil
	}
	reducer.ensureState()
	if reducer.fast.active {
		return []HistoryMutation{reducer.fastOpenLineMutation(reducer.cursorRow)}, nil
	}
	if len(reducer.lines) == 0 {
		return nil, nil
	}
	var mutations []HistoryMutation
	for _, row := range reducer.sortedOwnedRows() {
		draft := reducer.draftForRow(row)
		if draft == nil {
			continue
		}
		mutations = append(mutations, reducer.openLineMutation(draft, row))
	}
	return mutations, nil
}

func (reducer *streamLineReducer) ensureFastLine() {
	if reducer.fast.active {
		return
	}
	reducer.fast.active = true
	reducer.fast.lineID = reducer.ids.nextLogicalLineID()
	reducer.fast.cells = nil
	reducer.fast.cursor = maxInt(0, reducer.cursorCol)
}

func (reducer *streamLineReducer) sealFastLane(reason SealReason) []HistoryMutation {
	if !reducer.fast.active {
		return nil
	}
	line := logicalLineTemplate(HistoryRecordOrdinaryLine)
	line.ID = reducer.fast.lineID
	line.Cells = trimTrailingBlankCells(cloneHistoryCells(reducer.fast.cells))
	line.TailFill = cloneRowTailFill(reducer.fast.fill)
	line.Seal = SealStateSealed
	reducer.fast.active = false
	reducer.fast.cells = nil
	reducer.fast.cursor = 0
	reducer.fast.fill = nil
	reducer.fast.stats.SealedLines++
	return reducer.sealStandaloneLine(line, reason, HistoryRecordOrdinaryLine)
}

func (reducer *streamLineReducer) fastOpenLineMutation(row int) HistoryMutation {
	line := logicalLineTemplate(HistoryRecordOrdinaryLine)
	line.ID = reducer.fast.lineID
	line.Cells = cloneHistoryCells(reducer.fast.cells)
	line.TailFill = cloneRowTailFill(reducer.fast.fill)
	line.Seal = SealStateOpen
	open := OpenLine{
		Active: true,
		Draft: LogicalLineDraft{
			Line:      line,
			CursorCol: reducer.fast.cursor,
			Row:       row,
		},
	}
	openCopy := open
	openCopy.Draft.Line = cloneLogicalLine(open.Draft.Line)
	return HistoryMutation{Kind: HistoryMutationUpsertOpenLine, OpenLine: &openCopy}
}

func (reducer *streamLineReducer) applyOrdinaryFastLaneEraseLine(row int, col int, mode int) []HistoryMutation {
	cells := cloneHistoryCells(reducer.fast.cells)
	switch mode {
	case 1:
		cells = ensureCellWidth(cells, col+1)
		for i := 0; i <= col && i < len(cells); i++ {
			cells[i] = blankHistoryCell()
		}
	case 2:
		cells = nil
	default:
		if col < len(cells) {
			cells = cells[:maxInt(0, col)]
		}
	}
	reducer.fast.cells = trimTrailingBlankCells(cells)
	reducer.fast.cursor = maxInt(0, col)
	reducer.cursorRow = row
	reducer.cursorCol = reducer.fast.cursor
	reducer.fast.stats.MutableLines++
	return []HistoryMutation{reducer.fastOpenLineMutation(row)}
}

// materializeFastLaneToRowOwnership 把当前 ordinary mutable line 转回
// streamLineReducer 的 row ownership draft。调用边界是 fast lane 已遇到复杂 op；
// 转换只搬运同一 logical line id/cells/tail fill，不 seal、不生成新历史记录。
func (reducer *streamLineReducer) materializeFastLaneToRowOwnership() {
	if reducer == nil || !reducer.fast.active {
		return
	}
	row := reducer.cursorRow
	draft := &streamLineDraft{
		id:        reducer.fast.lineID,
		rows:      map[int][]Cell{row: cloneHistoryCells(reducer.fast.cells)},
		rowOrder:  []int{row},
		cursorRow: row,
		cursorCol: reducer.fast.cursor,
		tailFill:  cloneRowTailFill(reducer.fast.fill),
	}
	reducer.lines[draft.id] = draft
	reducer.rowOwners[row] = draft.id
	reducer.fast.active = false
	reducer.fast.cells = nil
	reducer.fast.cursor = 0
	reducer.fast.fill = nil
}

func (reducer *streamLineReducer) applyWriteSpan(op TerminalSemanticOp) []HistoryMutation {
	reducer.cursorRow = op.Row
	reducer.cursorCol = op.Col
	draft := reducer.ensureDraftForRow(op.Row)
	rowCells := cloneHistoryCells(draft.rows[op.Row])
	rowCells = writeCellsAt(rowCells, op.Col, historyCellsFromTerminal(op.Cells))
	draft.rows[op.Row] = trimTrailingBlankCells(rowCells)
	draft.cursorRow = op.Row
	draft.cursorCol = op.Col + terminalCellsDisplayWidth(op.Cells)
	reducer.cursorCol = draft.cursorCol
	return nil
}

func (reducer *streamLineReducer) applyControl(op TerminalSemanticOp) []HistoryMutation {
	switch op.Control {
	case "cr":
		reducer.cursorRow = op.Row
		reducer.cursorCol = 0
		if draft := reducer.draftForRow(op.Row); draft != nil {
			draft.cursorRow = op.Row
			draft.cursorCol = 0
			return nil
		}
	case "lf", "ind", "nel":
		row := reducer.cursorRow
		if _, ok := reducer.rowOwners[op.Row]; ok {
			row = op.Row
		}
		id, ok := reducer.rowOwners[row]
		if ok && op.TailFill != nil {
			reducer.setDraftTailFill(id, op.TailFill)
		}
		reducer.cursorRow = row + 1
		if op.Control == "nel" {
			reducer.cursorCol = 0
		}
		if ok {
			return reducer.sealLine(id, SealReasonLineFeed, HistoryRecordOrdinaryLine)
		}
	case "soft-wrap":
		return reducer.applySoftWrap(op)
	case "bs":
		reducer.cursorCol = maxInt(0, reducer.cursorCol-1)
	case "cub":
		reducer.cursorCol = maxInt(0, reducer.cursorCol-controlCount(op))
	case "cuf":
		reducer.cursorCol += controlCount(op)
	case "cha", "hpa":
		reducer.cursorRow = op.Row
		reducer.cursorCol = maxInt(0, op.Col)
	case "cup":
		reducer.cursorRow = maxInt(0, op.Row)
		reducer.cursorCol = maxInt(0, op.Col)
	case "vpa":
		reducer.cursorRow = maxInt(0, op.Row)
	case "cuu":
		reducer.cursorRow = maxInt(0, reducer.cursorRow-controlCount(op))
	case "cud":
		reducer.cursorRow += controlCount(op)
	case "el":
		return reducer.applyEraseLine(op.Row, op.Col, op.Mode)
	case "ed":
		return reducer.applyEraseDisplay(op)
	case "il":
		return reducer.applyLineInsertDelete(op.Row, op.Bottom, controlCount(op))
	case "dl":
		return reducer.applyLineInsertDelete(op.Row, op.Bottom, -controlCount(op))
	case "su":
		return reducer.applyLineInsertDelete(op.Row, op.Bottom, -controlCount(op))
	case "sd":
		return reducer.applyLineInsertDelete(op.Row, op.Bottom, controlCount(op))
	case "ri":
		return reducer.applyReverseIndex(op)
	case "ht", "cbt":
		reducer.cursorRow = maxInt(0, op.Row)
		reducer.cursorCol = maxInt(0, op.Col)
	case "ris":
		sealed, _ := reducer.SealOpenLine(SealReasonFullReplace)
		reducer.rowOwners = make(map[int]LogicalLineID)
		reducer.lines = make(map[LogicalLineID]*streamLineDraft)
		reducer.cursorRow = 0
		reducer.cursorCol = 0
		reducer.scrollTop = 0
		reducer.scrollBottom = 0
		reducer.skipNextScrollRect = nil
		return sealed
	case "decstbm":
		reducer.scrollTop = maxInt(0, op.Mode-1)
		reducer.scrollBottom = maxInt(reducer.scrollTop+1, op.Bottom)
		reducer.cursorRow = reducer.scrollTop
		reducer.cursorCol = 0
	case "decslrm", "hts", "tbc", "decst8c", "scs", "da", "da2", "dsr", "decxcpr", "decrqm", "decscusr":
		return nil
	case "ech":
		return reducer.applyEraseCharacters(op.Row, op.Col, controlCount(op))
	case "dch":
		return reducer.applyDeleteCharacters(op.Row, op.Col, controlCount(op))
	case "ich":
		return reducer.applyInsertCharacters(op.Row, op.Col, controlCount(op))
	}
	return nil
}

func (reducer *streamLineReducer) applySoftWrap(op TerminalSemanticOp) []HistoryMutation {
	draft := reducer.ensureDraftForRow(op.Row)
	draft.wrapped = true
	if op.TailFill != nil {
		draft.tailFill = rowTailFillFromTerminal(op.TailFill)
	}
	nextRow := op.Row + 1
	reducer.rowOwners[nextRow] = draft.id
	reducer.addDraftRow(draft, nextRow)
	reducer.cursorRow = nextRow
	reducer.cursorCol = 0
	draft.cursorRow = nextRow
	draft.cursorCol = 0
	return nil
}

func (reducer *streamLineReducer) applyEraseLine(row int, col int, mode int) []HistoryMutation {
	draft := reducer.draftForRow(row)
	if draft == nil {
		return nil
	}
	cells := cloneHistoryCells(draft.rows[row])
	switch mode {
	case 1:
		cells = ensureCellWidth(cells, col+1)
		for i := 0; i <= col && i < len(cells); i++ {
			cells[i] = blankHistoryCell()
		}
	case 2:
		cells = nil
	default:
		if col < len(cells) {
			cells = cells[:maxInt(0, col)]
		}
	}
	draft.rows[row] = trimTrailingBlankCells(cells)
	draft.cursorRow = row
	draft.cursorCol = maxInt(0, col)
	reducer.cursorRow = row
	reducer.cursorCol = draft.cursorCol
	return nil
}

func (reducer *streamLineReducer) applyEraseDisplay(op TerminalSemanticOp) []HistoryMutation {
	var mutations []HistoryMutation
	switch op.Mode {
	case 1:
		for _, row := range reducer.sortedOwnedRows() {
			if row < op.Row {
				mutations = append(mutations, reducer.clearRow(row)...)
			}
		}
		mutations = append(mutations, reducer.applyEraseLine(op.Row, op.Col, 1)...)
	case 2, 3:
		// 中文说明：ED2/ED3 会把整屏从 current ownership 清掉。对普通流来说，
		// 已经写到可见屏但尚未 LF 的 open line 也是真实 PTY 输出，不能直接丢弃。
		sealed, _ := reducer.SealOpenLine(SealReasonFullReplace)
		mutations = append(mutations, sealed...)
		if op.Mode == 3 {
			return mutations
		}
		for _, row := range reducer.sortedOwnedRows() {
			mutations = append(mutations, reducer.clearRow(row)...)
		}
	default:
		mutations = append(mutations, reducer.applyEraseLine(op.Row, op.Col, 0)...)
		for _, row := range reducer.sortedOwnedRows() {
			if row > op.Row {
				mutations = append(mutations, reducer.clearRow(row)...)
			}
		}
	}
	return mutations
}

func (reducer *streamLineReducer) applyEraseCharacters(row int, col int, count int) []HistoryMutation {
	draft := reducer.draftForRow(row)
	if draft == nil {
		return nil
	}
	cells := ensureCellWidth(cloneHistoryCells(draft.rows[row]), col)
	end := minInt(len(cells), col+count)
	for i := col; i < end; i++ {
		cells[i] = blankHistoryCell()
	}
	draft.rows[row] = trimTrailingBlankCells(cells)
	return nil
}

func (reducer *streamLineReducer) applyDeleteCharacters(row int, col int, count int) []HistoryMutation {
	draft := reducer.draftForRow(row)
	if draft == nil {
		return nil
	}
	cells := cloneHistoryCells(draft.rows[row])
	if col < len(cells) {
		end := minInt(len(cells), col+count)
		cells = append(cells[:col], cells[end:]...)
	}
	draft.rows[row] = trimTrailingBlankCells(cells)
	return nil
}

func (reducer *streamLineReducer) applyInsertCharacters(row int, col int, count int) []HistoryMutation {
	draft := reducer.draftForRow(row)
	if draft == nil {
		return nil
	}
	cells := ensureCellWidth(cloneHistoryCells(draft.rows[row]), col)
	blanks := make([]Cell, count)
	for i := range blanks {
		blanks[i] = blankHistoryCell()
	}
	cells = append(cells[:col], append(blanks, cells[col:]...)...)
	draft.rows[row] = trimTrailingBlankCells(cells)
	return nil
}

func (reducer *streamLineReducer) applyClearRect(op TerminalSemanticOp) []HistoryMutation {
	var mutations []HistoryMutation
	for row := op.Rect.Y; row < op.Rect.Y+op.Rect.Height; row++ {
		draft := reducer.draftForRow(row)
		if draft == nil {
			continue
		}
		cells := ensureCellWidth(cloneHistoryCells(draft.rows[row]), op.Rect.X)
		end := minInt(len(cells), op.Rect.X+op.Rect.Width)
		for col := op.Rect.X; col < end; col++ {
			cells[col] = blankHistoryCell()
		}
		draft.rows[row] = trimTrailingBlankCells(cells)
	}
	return mutations
}

func (reducer *streamLineReducer) applyScrollRect(op TerminalSemanticOp) []HistoryMutation {
	if op.Rect.Width <= 0 || op.Rect.Height <= 0 {
		return nil
	}
	beforeOwners := reducer.cloneRowOwnership()
	beforeRows := reducer.cloneRows()
	affectedRows := rowsInRect(op.Rect)
	for _, row := range affectedRows {
		if id, ok := reducer.rowOwners[row]; ok {
			reducer.removeRowFromDraft(id, row)
		}
	}
	var mutations []HistoryMutation
	for _, row := range affectedRows {
		srcRow := row - op.Dy
		srcID, ok := beforeOwners[srcRow]
		if ok && srcRow >= op.Rect.Y && srcRow < op.Rect.Y+op.Rect.Height {
			draft := reducer.ensureDraftWithID(srcID)
			draft.rows[row] = cloneHistoryCells(beforeRows[srcID][srcRow])
			reducer.addDraftRow(draft, row)
			continue
		}
	}
	reducer.deleteEmptyDrafts()
	return mutations
}

func (reducer *streamLineReducer) applyLineInsertDelete(row int, bottom int, dy int) []HistoryMutation {
	if bottom <= row || dy == 0 {
		return nil
	}
	rect := vterm.DamageRect{X: 0, Y: row, Width: maxInt(1, reducer.maxOwnedWidth()), Height: bottom - row}
	op := TerminalSemanticOp{Code: vterm.ScreenOpScrollRect, Rect: rect, Dy: dy}
	mutations := reducer.applyScrollRect(op)
	reducer.skipNextScrollRect = &pendingScrollRect{rect: rect, dy: dy}
	reducer.cursorRow = maxInt(0, row)
	reducer.cursorCol = 0
	return mutations
}

func (reducer *streamLineReducer) applyReverseIndex(op TerminalSemanticOp) []HistoryMutation {
	row := maxInt(0, op.Row)
	top, bottom := reducer.effectiveScrollRegionForRow(row)
	if row == top {
		return reducer.applyLineInsertDelete(top, bottom, 1)
	}
	reducer.cursorRow = maxInt(0, row-1)
	reducer.cursorCol = maxInt(0, op.Col)
	return nil
}

func (reducer *streamLineReducer) effectiveScrollRegionForRow(row int) (int, int) {
	if reducer != nil && reducer.scrollBottom > reducer.scrollTop && row >= reducer.scrollTop && row < reducer.scrollBottom {
		return reducer.scrollTop, reducer.scrollBottom
	}
	bottom := reducer.maxOwnedRow() + 1
	if bottom <= row {
		bottom = row + 1
	}
	return row, bottom
}

func (reducer *streamLineReducer) consumePendingScrollRect(rect vterm.DamageRect, dy int) bool {
	if reducer == nil || reducer.skipNextScrollRect == nil {
		return false
	}
	pending := reducer.skipNextScrollRect
	if pending.dy == dy && scrollRectsEquivalent(pending.rect, rect) {
		reducer.skipNextScrollRect = nil
		return true
	}
	reducer.skipNextScrollRect = nil
	return false
}

func (reducer *streamLineReducer) applyCopyRect(op TerminalSemanticOp) []HistoryMutation {
	if op.Src.Width <= 0 || op.Src.Height <= 0 {
		return nil
	}
	beforeOwners := reducer.cloneRowOwnership()
	beforeRows := reducer.cloneRows()
	var mutations []HistoryMutation
	for offset := 0; offset < op.Src.Height; offset++ {
		srcRow := op.Src.Y + offset
		dstRow := op.DstY + offset
		srcID, ok := beforeOwners[srcRow]
		if !ok {
			continue
		}
		dstDraft := reducer.ensureDraftForRow(dstRow)
		srcCells := beforeRows[srcID][srcRow]
		dstCells := ensureCellWidth(cloneHistoryCells(dstDraft.rows[dstRow]), op.DstX)
		copyWidth := minInt(op.Src.Width, maxInt(0, len(srcCells)-op.Src.X))
		dstCells = ensureCellWidth(dstCells, op.DstX+copyWidth)
		for col := 0; col < copyWidth; col++ {
			dstCells[op.DstX+col] = srcCells[op.Src.X+col]
		}
		dstDraft.rows[dstRow] = trimTrailingBlankCells(dstCells)
	}
	return mutations
}

func (reducer *streamLineReducer) maxOwnedWidth() int {
	width := 0
	for _, draft := range reducer.lines {
		if draft == nil {
			continue
		}
		for _, cells := range draft.rows {
			if len(cells) > width {
				width = len(cells)
			}
		}
	}
	return width
}

func (reducer *streamLineReducer) maxOwnedRow() int {
	maxRow := -1
	for row := range reducer.rowOwners {
		if row > maxRow {
			maxRow = row
		}
	}
	return maxRow
}

func scrollRectsEquivalent(left vterm.DamageRect, right vterm.DamageRect) bool {
	if left.Y != right.Y || left.Height != right.Height {
		return false
	}
	if left.X != 0 || right.X != 0 {
		return left == right
	}
	if left.Width <= 0 || right.Width <= 0 {
		return left.Width == right.Width
	}
	return true
}

func (reducer *streamLineReducer) ensureState() {
	if reducer.rowOwners == nil {
		reducer.rowOwners = make(map[int]LogicalLineID)
	}
	if reducer.lines == nil {
		reducer.lines = make(map[LogicalLineID]*streamLineDraft)
	}
	if reducer.ids == nil {
		reducer.ids = newHistoryIDAllocator()
	}
	reducer.ids.ensure()
}

func (reducer *streamLineReducer) ensureDraftForRow(row int) *streamLineDraft {
	if id, ok := reducer.rowOwners[row]; ok {
		if draft := reducer.lines[id]; draft != nil {
			reducer.addDraftRow(draft, row)
			return draft
		}
	}
	id := reducer.ids.nextLogicalLineID()
	draft := &streamLineDraft{
		id:       id,
		rows:     map[int][]Cell{row: nil},
		rowOrder: []int{row},
	}
	reducer.lines[id] = draft
	reducer.rowOwners[row] = id
	return draft
}

func (reducer *streamLineReducer) ensureDraftWithID(id LogicalLineID) *streamLineDraft {
	if draft := reducer.lines[id]; draft != nil {
		return draft
	}
	draft := &streamLineDraft{
		id:      id,
		rows:    make(map[int][]Cell),
		wrapped: false,
	}
	reducer.lines[id] = draft
	reducer.ids.reserveLogicalLineID(id)
	return draft
}

func (reducer *streamLineReducer) draftForRow(row int) *streamLineDraft {
	id, ok := reducer.rowOwners[row]
	if !ok {
		return nil
	}
	return reducer.lines[id]
}

func (reducer *streamLineReducer) addDraftRow(draft *streamLineDraft, row int) {
	if draft == nil {
		return
	}
	if draft.rows == nil {
		draft.rows = make(map[int][]Cell)
	}
	if _, ok := draft.rows[row]; !ok {
		draft.rows[row] = nil
	}
	if !intSliceContains(draft.rowOrder, row) {
		draft.rowOrder = append(draft.rowOrder, row)
		sort.Ints(draft.rowOrder)
	}
	reducer.rowOwners[row] = draft.id
}

func (reducer *streamLineReducer) clearRow(row int) []HistoryMutation {
	id, ok := reducer.rowOwners[row]
	if !ok {
		return nil
	}
	draft := reducer.lines[id]
	reducer.removeRowFromDraft(id, row)
	if draft == nil || len(draft.rowOrder) == 0 {
		return nil
	}
	return nil
}

func (reducer *streamLineReducer) deleteEmptyDrafts() {
	for id, draft := range reducer.lines {
		if draft == nil || len(draft.rowOrder) == 0 {
			delete(reducer.lines, id)
		}
	}
}

func (reducer *streamLineReducer) removeRowFromDraft(id LogicalLineID, row int) {
	draft := reducer.lines[id]
	if draft == nil {
		delete(reducer.rowOwners, row)
		return
	}
	delete(draft.rows, row)
	delete(reducer.rowOwners, row)
	out := draft.rowOrder[:0]
	for _, current := range draft.rowOrder {
		if current != row {
			out = append(out, current)
		}
	}
	draft.rowOrder = out
	if len(draft.rowOrder) == 0 {
		delete(reducer.lines, id)
	}
}

func (reducer *streamLineReducer) setDraftTailFill(id LogicalLineID, style *TerminalSemanticStyle) {
	if reducer == nil || style == nil {
		return
	}
	draft := reducer.lines[id]
	if draft == nil {
		return
	}
	draft.tailFill = rowTailFillFromTerminal(style)
}

func (reducer *streamLineReducer) sealLine(id LogicalLineID, reason SealReason, kind HistoryRecordKind) []HistoryMutation {
	draft := reducer.lines[id]
	if draft == nil {
		return nil
	}
	line := reducer.logicalLineFromDraft(draft, kind)
	line.Seal = SealStateSealed
	for _, row := range append([]int(nil), draft.rowOrder...) {
		delete(reducer.rowOwners, row)
	}
	delete(reducer.lines, id)
	return reducer.sealStandaloneLine(line, reason, kind)
}

func (reducer *streamLineReducer) sealStandaloneLine(line LogicalLine, reason SealReason, kind HistoryRecordKind) []HistoryMutation {
	record := HistoryRecord{
		ID:      reducer.ids.nextHistoryRecordID(),
		Seq:     reducer.ids.nextTimelineSeq(),
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

func (reducer *streamLineReducer) openLineMutation(draft *streamLineDraft, row int) HistoryMutation {
	open := OpenLine{
		Active: true,
		Draft: LogicalLineDraft{
			Line:      reducer.logicalLineForRow(draft, row),
			CursorCol: draft.cursorCol,
			Wrapped:   draft.wrapped,
			Row:       row,
		},
	}
	openCopy := open
	openCopy.Draft.Line = cloneLogicalLine(open.Draft.Line)
	return HistoryMutation{
		Kind:     HistoryMutationUpsertOpenLine,
		OpenLine: &openCopy,
	}
}

func (reducer *streamLineReducer) logicalLineForRow(draft *streamLineDraft, row int) LogicalLine {
	line := logicalLineTemplate(HistoryRecordOrdinaryLine)
	line.ID = draft.id
	line.Cells = cloneHistoryCells(draft.rows[row])
	line.TailFill = cloneRowTailFill(draft.tailFill)
	line.Seal = SealStateOpen
	return line
}

func (reducer *streamLineReducer) logicalLineFromDraft(draft *streamLineDraft, kind HistoryRecordKind) LogicalLine {
	line := logicalLineTemplate(kind)
	line.ID = draft.id
	for _, row := range draft.rowOrder {
		line.Cells = append(line.Cells, cloneHistoryCells(draft.rows[row])...)
	}
	line.Cells = trimTrailingBlankCells(line.Cells)
	line.TailFill = cloneRowTailFill(draft.tailFill)
	line.Seal = SealStateOpen
	return line
}

func (reducer *streamLineReducer) newLogicalLine(kind HistoryRecordKind) LogicalLine {
	line := logicalLineTemplate(kind)
	line.ID = reducer.ids.nextLogicalLineID()
	return line
}

func logicalLineTemplate(kind HistoryRecordKind) LogicalLine {
	return LogicalLine{
		Seal:      SealStateOpen,
		Kind:      string(kind),
		Residency: ResidencyMemory,
	}
}

func rowTailFillFromTerminal(style *TerminalSemanticStyle) *RowTailFill {
	if style == nil {
		return nil
	}
	return &RowTailFill{Style: CellStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}}
}

func cloneRowTailFill(fill *RowTailFill) *RowTailFill {
	if fill == nil {
		return nil
	}
	out := *fill
	return &out
}

func (reducer *streamLineReducer) sortedLineIDs() []LogicalLineID {
	ids := make([]LogicalLineID, 0, len(reducer.lines))
	for id := range reducer.lines {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})
	return ids
}

func (reducer *streamLineReducer) sortedOwnedRows() []int {
	rows := make([]int, 0, len(reducer.rowOwners))
	for row := range reducer.rowOwners {
		rows = append(rows, row)
	}
	sort.Ints(rows)
	return rows
}

func rowsInRect(rect vterm.DamageRect) []int {
	if rect.Height <= 0 {
		return nil
	}
	rows := make([]int, 0, rect.Height)
	for row := rect.Y; row < rect.Y+rect.Height; row++ {
		rows = append(rows, row)
	}
	return rows
}

func (reducer *streamLineReducer) cloneRowOwnership() map[int]LogicalLineID {
	out := make(map[int]LogicalLineID, len(reducer.rowOwners))
	for row, id := range reducer.rowOwners {
		out[row] = id
	}
	return out
}

func (reducer *streamLineReducer) cloneRows() map[LogicalLineID]map[int][]Cell {
	out := make(map[LogicalLineID]map[int][]Cell, len(reducer.lines))
	for id, draft := range reducer.lines {
		rows := make(map[int][]Cell, len(draft.rows))
		for row, cells := range draft.rows {
			rows[row] = cloneHistoryCells(cells)
		}
		out[id] = rows
	}
	return out
}

func (reducer *streamLineReducer) debugOpenLinesByRow() map[int]OpenLine {
	out := make(map[int]OpenLine, len(reducer.rowOwners))
	if reducer.fast.active {
		out[reducer.cursorRow] = OpenLine{
			Active: true,
			Draft: LogicalLineDraft{
				Line:      reducer.fastLogicalLine(),
				CursorCol: reducer.fast.cursor,
				Row:       reducer.cursorRow,
			},
		}
	}
	for _, row := range reducer.sortedOwnedRows() {
		draft := reducer.draftForRow(row)
		if draft == nil {
			continue
		}
		open := OpenLine{
			Active: true,
			Draft: LogicalLineDraft{
				Line:      reducer.logicalLineForRow(draft, row),
				CursorCol: draft.cursorCol,
				Wrapped:   draft.wrapped,
				Row:       row,
			},
		}
		out[row] = open
	}
	return out
}

func (reducer *streamLineReducer) debugOrdinaryFastLaneStats() ordinaryFastLaneStats {
	if reducer == nil {
		return ordinaryFastLaneStats{}
	}
	return reducer.fast.stats
}

func (reducer *streamLineReducer) fastLogicalLine() LogicalLine {
	line := logicalLineTemplate(HistoryRecordOrdinaryLine)
	line.ID = reducer.fast.lineID
	line.Cells = cloneHistoryCells(reducer.fast.cells)
	line.TailFill = cloneRowTailFill(reducer.fast.fill)
	line.Seal = SealStateOpen
	return line
}

func historyCellsFromTerminal(cells []TerminalSemanticCell) []Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]Cell, 0, len(cells))
	for _, cell := range cells {
		text := cell.Content
		width := cell.Width
		if width <= 0 {
			if text == "" {
				// 中文说明：width=0 且无文本是宽字符 continuation cell；
				// 历史文本 truth 已在前一个 Width=2 cell 中，不能投影成真实空格。
				continue
			}
			width = xansi.StringWidth(text)
			if width <= 0 {
				continue
			}
		}
		if text == "" {
			text = strings.Repeat(" ", width)
		}
		out = append(out, Cell{
			Text:       text,
			Width:      width,
			Style:      historyStyleFromTerminal(cell.Style),
			LinkURL:    cell.LinkURL,
			LinkParams: cell.LinkParams,
		})
	}
	return out
}

func historyStyleFromTerminal(style TerminalSemanticStyle) CellStyle {
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

func writeCellsAt(cells []Cell, col int, incoming []Cell) []Cell {
	if col < 0 {
		col = 0
	}
	cells = ensureCellWidth(cells, col)
	index := cellIndexForDisplayColumn(cells, col)
	for _, cell := range incoming {
		width := historyCellDisplayWidth(cell)
		if index < len(cells) {
			cells[index] = cell
		} else {
			cells = append(cells, cell)
		}
		index++
		for width > 1 && index < len(cells) {
			// 中文说明：普通流 rows 按 display column 定位；宽字符后续列不是文本。
			// 覆盖旧内容时必须删掉被宽字符占用的旧 cell，不能留下真实空格。
			cells = append(cells[:index], cells[index+1:]...)
			width--
		}
	}
	return cells
}

func ensureCellWidth(cells []Cell, width int) []Cell {
	for historyCellsDisplayWidth(cells) < width {
		cells = append(cells, blankHistoryCell())
	}
	return cells
}

func cellIndexForDisplayColumn(cells []Cell, col int) int {
	if col <= 0 {
		return 0
	}
	cursor := 0
	for index, cell := range cells {
		cellWidth := historyCellDisplayWidth(cell)
		if col < cursor+cellWidth {
			return index
		}
		cursor += cellWidth
	}
	return len(cells)
}

func historyCellsDisplayWidth(cells []Cell) int {
	width := 0
	for _, cell := range cells {
		width += historyCellDisplayWidth(cell)
	}
	return width
}

func historyCellDisplayWidth(cell Cell) int {
	if cell.Width > 0 {
		return cell.Width
	}
	if cell.Text == "" {
		return 0
	}
	width := xansi.StringWidth(cell.Text)
	if width > 0 {
		return width
	}
	return 0
}

func trimTrailingBlankCells(cells []Cell) []Cell {
	end := len(cells)
	for end > 0 && isDefaultBlankCell(cells[end-1]) {
		end--
	}
	if end == 0 {
		return nil
	}
	out := make([]Cell, end)
	copy(out, cells[:end])
	return out
}

func isDefaultBlankCell(cell Cell) bool {
	return cell.Text == " " && cell.Style == (CellStyle{}) && cell.LinkURL == "" && cell.LinkParams == ""
}

func blankHistoryCell() Cell {
	return Cell{Text: " ", Width: 1}
}

func controlCount(op TerminalSemanticOp) int {
	if op.Mode > 0 {
		return op.Mode
	}
	return 1
}

func terminalCellsDisplayWidth(cells []TerminalSemanticCell) int {
	width := 0
	for _, cell := range cells {
		if cell.Width > 0 {
			width += cell.Width
			continue
		}
		if cell.Content != "" {
			width += maxInt(0, xansi.StringWidth(cell.Content))
		}
	}
	return width
}

func cellsFromScrollOutProof(proof TerminalSemanticScrollOut) []Cell {
	if len(proof.Runs) > 0 {
		var cells []Cell
		for _, run := range proof.Runs {
			text := run.Text
			for text != "" {
				cluster, width := xansi.FirstGraphemeCluster(text, xansi.GraphemeWidth)
				if cluster == "" {
					break
				}
				text = text[len(cluster):]
				if width <= 0 {
					continue
				}
				cells = append(cells, Cell{
					Text:  cluster,
					Width: width,
					Style: historyStyleFromTerminal(run.Style),
				})
			}
		}
		return trimTrailingBlankCells(cells)
	}
	return trimTrailingBlankCells(historyCellsFromTerminal(proof.Cells))
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func intSliceContains(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
