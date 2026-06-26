package history

import (
	"sort"
	"strings"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

// NewStreamLineReducer 创建 ordinary stream 的 logical-line reducer。
// domain owner：history；truth source 只能是 vterm semantic op 和 scroll-out proof。
// 调用边界：它不读取 live snapshot、不解析 raw PTY、不判断程序名，只产出
// HistoryMutation 交给后续 authoritative store 应用。
func NewStreamLineReducer() StreamLineReducer {
	return &streamLineReducer{
		ids:              newHistoryIDAllocator(),
		rowOwners:        make(map[int]LogicalLineID),
		lines:            make(map[LogicalLineID]*streamLineDraft),
		sealedProofSigns: make(map[string]struct{}),
	}
}

type streamLineReducer struct {
	ids              *historyIDAllocator
	rowOwners        map[int]LogicalLineID
	lines            map[LogicalLineID]*streamLineDraft
	cursorRow        int
	cursorCol        int
	sealedProofSigns map[string]struct{}
}

type streamLineDraft struct {
	id        LogicalLineID
	rows      map[int][]Cell
	rowOrder  []int
	cursorRow int
	cursorCol int
	wrapped   bool
}

func (reducer *streamLineReducer) ApplyOp(op TerminalSemanticOp) ([]HistoryMutation, error) {
	if reducer == nil {
		return nil, nil
	}
	reducer.ensureState()
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
	ids := reducer.sortedLineIDs()
	var mutations []HistoryMutation
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
	sign := scrollOutProofSignature(proof)
	if sign == "" {
		return nil, nil
	}
	// vterm 当前 proof 没有独立 id；这里用 payload signature 防止同一 proof 被重复消费。
	// 如果后续需要允许相同文本的不同 proof，必须先在 vterm contract 增补 ordered proof id。
	if _, exists := reducer.sealedProofSigns[sign]; exists {
		return nil, nil
	}
	reducer.sealedProofSigns[sign] = struct{}{}
	line := reducer.newLogicalLine(HistoryRecordPrimaryScrollOutLine)
	line.Cells = cellsFromScrollOutProof(proof)
	line.Seal = SealStateSealed
	return reducer.sealStandaloneLine(line, SealReasonScrollOut, HistoryRecordPrimaryScrollOutLine), nil
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
	return []HistoryMutation{reducer.openLineMutation(draft, op.Row)}
}

func (reducer *streamLineReducer) applyControl(op TerminalSemanticOp) []HistoryMutation {
	switch op.Control {
	case "cr":
		reducer.cursorRow = op.Row
		reducer.cursorCol = 0
		if draft := reducer.draftForRow(op.Row); draft != nil {
			draft.cursorRow = op.Row
			draft.cursorCol = 0
			return []HistoryMutation{reducer.openLineMutation(draft, op.Row)}
		}
	case "lf", "ind", "nel":
		row := reducer.cursorRow
		if _, ok := reducer.rowOwners[op.Row]; ok {
			row = op.Row
		}
		id, ok := reducer.rowOwners[row]
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
	nextRow := op.Row + 1
	reducer.rowOwners[nextRow] = draft.id
	reducer.addDraftRow(draft, nextRow)
	reducer.cursorRow = nextRow
	reducer.cursorCol = 0
	draft.cursorRow = nextRow
	draft.cursorCol = 0
	return []HistoryMutation{reducer.openLineMutation(draft, op.Row)}
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
	return []HistoryMutation{reducer.openLineMutation(draft, row)}
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
	return []HistoryMutation{reducer.openLineMutation(draft, row)}
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
	return []HistoryMutation{reducer.openLineMutation(draft, row)}
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
	return []HistoryMutation{reducer.openLineMutation(draft, row)}
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
		mutations = append(mutations, reducer.openLineMutation(draft, row))
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
			mutations = append(mutations, reducer.openLineMutation(draft, row))
			continue
		}
	}
	reducer.deleteEmptyDrafts()
	return mutations
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
		mutations = append(mutations, reducer.openLineMutation(dstDraft, dstRow))
	}
	return mutations
}

func (reducer *streamLineReducer) ensureState() {
	if reducer.rowOwners == nil {
		reducer.rowOwners = make(map[int]LogicalLineID)
	}
	if reducer.lines == nil {
		reducer.lines = make(map[LogicalLineID]*streamLineDraft)
	}
	if reducer.sealedProofSigns == nil {
		reducer.sealedProofSigns = make(map[string]struct{})
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
	return []HistoryMutation{reducer.openLineMutation(draft, row)}
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

func historyCellsFromTerminal(cells []TerminalSemanticCell) []Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]Cell, 0, len(cells))
	for _, cell := range cells {
		text := cell.Content
		if text == "" {
			text = " "
		}
		width := cell.Width
		if width <= 0 {
			width = 1
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
	for offset, cell := range incoming {
		target := col + offset
		if target < len(cells) {
			cells[target] = cell
			continue
		}
		cells = append(cells, cell)
	}
	return cells
}

func ensureCellWidth(cells []Cell, width int) []Cell {
	for len(cells) < width {
		cells = append(cells, blankHistoryCell())
	}
	return cells
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
		width++
	}
	return width
}

func cellsFromScrollOutProof(proof TerminalSemanticScrollOut) []Cell {
	if len(proof.Runs) > 0 {
		var cells []Cell
		for _, run := range proof.Runs {
			for _, r := range run.Text {
				cells = append(cells, Cell{
					Text:  string(r),
					Width: 1,
					Style: historyStyleFromTerminal(run.Style),
				})
			}
		}
		return trimTrailingBlankCells(cells)
	}
	return trimTrailingBlankCells(historyCellsFromTerminal(proof.Cells))
}

func scrollOutProofSignature(proof TerminalSemanticScrollOut) string {
	var builder strings.Builder
	builder.WriteString("wrapped:")
	if proof.WrappedSet && proof.Wrapped {
		builder.WriteString("1")
	} else {
		builder.WriteString("0")
	}
	builder.WriteString("|runs:")
	for _, run := range proof.Runs {
		builder.WriteString(run.Style.FG)
		builder.WriteString("/")
		builder.WriteString(run.Style.BG)
		builder.WriteString(":")
		builder.WriteString(run.Text)
		builder.WriteString(";")
	}
	builder.WriteString("|cells:")
	for _, cell := range proof.Cells {
		builder.WriteString(cell.Style.FG)
		builder.WriteString("/")
		builder.WriteString(cell.Style.BG)
		builder.WriteString(":")
		builder.WriteString(cell.Content)
		builder.WriteString(";")
	}
	return builder.String()
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
