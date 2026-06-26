package history

import "time"

// NewFrameReducer 创建 primary/alt fixed-grid frame journal reducer。
// domain owner：history；truth source 只能是 vterm semantic frame payload 和明确
// lifecycle/boundary reason。它不从 write ops、live snapshot 或程序名重建 frame。
func NewFrameReducer() FrameReducer {
	return &frameReducer{
		nextSessionID: 1,
		nextFrameID:   1,
		ids:           newHistoryIDAllocator(),
	}
}

type frameReducer struct {
	journal       FrameJournal
	nextSessionID ScreenSessionID
	nextFrameID   ScreenFrameID
	ids           *historyIDAllocator
	nextSeq       uint64
}

func (reducer *frameReducer) ReplacePrimaryCurrent(frame TerminalSemanticFrame, reason FrameReason) ([]HistoryMutation, error) {
	if reducer == nil {
		return nil, nil
	}
	reducer.ensureCounters()
	id := reducer.currentPrimaryFrameID()
	mutable := MutableFrame{
		ID:        id,
		SessionID: reducer.currentSessionID(),
		Seq:       reducer.nextSequence(),
		Cols:      frame.Cols,
		Rows:      reducer.draftsFromSemanticFrame(frame, string(LineKindScreenFrame), SealStateOpen),
		Source:    FrameSourcePrimarySemantic,
		CreatedAt: time.Now().UTC(),
	}
	reducer.journal.PrimaryCurrent = cloneMutableFramePointer(&mutable)
	mutationFrame := cloneMutableFrame(mutable)
	return []HistoryMutation{{
		Kind:      HistoryMutationReplacePrimaryFrame,
		Mutable:   &mutationFrame,
		SessionID: mutable.SessionID,
		FrameID:   mutable.ID,
	}}, nil
}

func (reducer *frameReducer) ArchivePrimaryCurrent(reason SealReason) ([]HistoryMutation, error) {
	if reducer == nil || reducer.journal.PrimaryCurrent == nil {
		return nil, nil
	}
	sealed := reducer.sealMutableFrame(*reducer.journal.PrimaryCurrent, reason)
	reducer.journal.PrimaryArchived = append(reducer.journal.PrimaryArchived, cloneSealedFrame(sealed))
	reducer.journal.PrimaryCurrent = nil
	return reducer.sealedFrameMutations(sealed, HistoryMutationArchivePrimaryFrame, HistoryRecordArchivedPrimaryFrame), nil
}

func (reducer *frameReducer) ReplaceAltCurrent(frame TerminalSemanticFrame) ([]HistoryMutation, error) {
	if reducer == nil {
		return nil, nil
	}
	reducer.ensureCounters()
	id := reducer.currentAltFrameID()
	transient := TransientFrame{
		ID:        id,
		Seq:       reducer.nextSequence(),
		Cols:      frame.Cols,
		Rows:      reducer.draftsFromSemanticFrame(frame, string(LineKindAltScreenFrame), SealStateOpen),
		Source:    FrameSourceAltSemantic,
		CreatedAt: time.Now().UTC(),
	}
	reducer.journal.AltCurrent = cloneTransientFramePointer(&transient)
	mutationFrame := cloneTransientFrame(transient)
	return []HistoryMutation{{
		Kind:      HistoryMutationReplaceAltFrame,
		Transient: &mutationFrame,
		FrameID:   transient.ID,
	}}, nil
}

func (reducer *frameReducer) ClearAltCurrent(reason FrameReason) ([]HistoryMutation, error) {
	if reducer == nil || reducer.journal.AltCurrent == nil {
		return nil, nil
	}
	frameID := reducer.journal.AltCurrent.ID
	reducer.journal.AltCurrent = nil
	return []HistoryMutation{{
		Kind:    HistoryMutationClearAltFrame,
		FrameID: frameID,
		Reason:  sealReasonFromFrameReason(reason),
	}}, nil
}

func (reducer *frameReducer) ApplyNonHistoryBoundary(reason FrameReason) ([]HistoryMutation, error) {
	if reducer == nil {
		return nil, nil
	}
	return []HistoryMutation{{
		Kind:   HistoryMutationNonHistoryBoundary,
		Reason: sealReasonFromFrameReason(reason),
	}}, nil
}

func (reducer *frameReducer) ClosePrimaryCurrent(reason SealReason) ([]HistoryMutation, error) {
	if reducer == nil || reducer.journal.PrimaryCurrent == nil {
		return nil, nil
	}
	sealed := reducer.sealMutableFrame(*reducer.journal.PrimaryCurrent, reason)
	reducer.journal.PrimaryCurrent = nil
	return reducer.sealedFrameMutations(sealed, HistoryMutationClosePrimaryFrame, HistoryRecordClosedPrimaryFrame), nil
}

func (reducer *frameReducer) ensureCounters() {
	if reducer.nextSessionID == 0 {
		reducer.nextSessionID = 1
	}
	if reducer.nextFrameID == 0 {
		reducer.nextFrameID = 1
	}
	if reducer.ids == nil {
		reducer.ids = newHistoryIDAllocator()
	}
	reducer.ids.ensure()
}

func (reducer *frameReducer) currentSessionID() ScreenSessionID {
	reducer.ensureCounters()
	if reducer.journal.PrimaryCurrent != nil {
		return reducer.journal.PrimaryCurrent.SessionID
	}
	if len(reducer.journal.PrimaryArchived) > 0 {
		return reducer.journal.PrimaryArchived[len(reducer.journal.PrimaryArchived)-1].SessionID
	}
	id := reducer.nextSessionID
	reducer.nextSessionID++
	return id
}

func (reducer *frameReducer) currentPrimaryFrameID() ScreenFrameID {
	reducer.ensureCounters()
	if reducer.journal.PrimaryCurrent != nil {
		return reducer.journal.PrimaryCurrent.ID
	}
	id := reducer.nextFrameID
	reducer.nextFrameID++
	return id
}

func (reducer *frameReducer) currentAltFrameID() ScreenFrameID {
	reducer.ensureCounters()
	if reducer.journal.AltCurrent != nil {
		return reducer.journal.AltCurrent.ID
	}
	id := reducer.nextFrameID
	reducer.nextFrameID++
	return id
}

func (reducer *frameReducer) nextSequence() uint64 {
	reducer.nextSeq++
	return reducer.nextSeq
}

func (reducer *frameReducer) nextLogicalLineID() LogicalLineID {
	return reducer.ids.nextLogicalLineID()
}

func (reducer *frameReducer) draftsFromSemanticFrame(frame TerminalSemanticFrame, kind string, seal SealState) []LogicalLineDraft {
	frameRows := trimmedFrameRows(frame.Rows)
	rows := make([]LogicalLineDraft, 0, len(frameRows))
	for row, cells := range frameRows {
		line := LogicalLine{
			ID:         reducer.nextLogicalLineID(),
			Seal:       seal,
			Kind:       kind,
			Cells:      cells,
			ScreenCols: frame.Cols,
			Residency:  ResidencyMemory,
		}
		rows = append(rows, LogicalLineDraft{
			Line: cloneLogicalLine(line),
			Row:  row,
		})
	}
	return rows
}

func trimmedFrameRows(rows [][]TerminalSemanticCell) [][]Cell {
	// 中文说明：vterm frame 为了保持屏幕索引会保留 used screen rows；history
	// 投影只能裁掉尾部纯 default blank rows，不能在 protocol/CLI 层再做症状过滤。
	converted := make([][]Cell, len(rows))
	lastContentRow := -1
	for row, cells := range rows {
		trimmed := trimTrailingBlankCells(historyCellsFromTerminal(cells))
		converted[row] = trimmed
		if !historyFrameRowIsDefaultBlank(trimmed) {
			lastContentRow = row
		}
	}
	if lastContentRow < 0 {
		return nil
	}
	return converted[:lastContentRow+1]
}

func historyFrameRowIsDefaultBlank(cells []Cell) bool {
	for _, cell := range cells {
		if !isDefaultBlankCell(cell) {
			return false
		}
	}
	return true
}

func (reducer *frameReducer) sealMutableFrame(frame MutableFrame, reason SealReason) SealedFrame {
	lines := make([]LogicalLine, 0, len(frame.Rows))
	for _, draft := range frame.Rows {
		line := cloneLogicalLine(draft.Line)
		line.Seal = SealStateSealed
		line.Kind = string(LineKindScreenFrame)
		line.ScreenCols = frame.Cols
		lines = append(lines, line)
	}
	return SealedFrame{
		ID:        frame.ID,
		SessionID: frame.SessionID,
		Seq:       reducer.nextSequence(),
		Cols:      frame.Cols,
		Lines:     lines,
		Reason:    reason,
		CreatedAt: time.Now().UTC(),
	}
}

func (reducer *frameReducer) sealedFrameMutations(frame SealedFrame, mutationKind HistoryMutationKind, recordKind HistoryRecordKind) []HistoryMutation {
	record := HistoryRecord{
		ID:      reducer.ids.nextHistoryRecordID(),
		Seq:     frame.Seq,
		Kind:    recordKind,
		LineIDs: lineIDsFromLogicalLines(frame.Lines),
		FrameID: frame.ID,
		Reason:  frame.Reason,
	}
	sealedCopy := cloneSealedFrame(frame)
	recordCopy := cloneHistoryRecord(record)
	return []HistoryMutation{
		{
			Kind:      mutationKind,
			Sealed:    &sealedCopy,
			LineIDs:   append([]LogicalLineID(nil), record.LineIDs...),
			SessionID: frame.SessionID,
			FrameID:   frame.ID,
			Reason:    frame.Reason,
		},
		{
			Kind:      HistoryMutationAppendTimelineRecord,
			Record:    &recordCopy,
			LineIDs:   append([]LogicalLineID(nil), record.LineIDs...),
			SessionID: frame.SessionID,
			FrameID:   frame.ID,
			Reason:    frame.Reason,
		},
	}
}

func (reducer *frameReducer) debugFrameJournal() FrameJournal {
	state := FrameJournal{
		PrimaryArchived: cloneSealedFrames(reducer.journal.PrimaryArchived),
	}
	if reducer.journal.PrimaryCurrent != nil {
		state.PrimaryCurrent = cloneMutableFramePointer(reducer.journal.PrimaryCurrent)
	}
	if reducer.journal.AltCurrent != nil {
		state.AltCurrent = cloneTransientFramePointer(reducer.journal.AltCurrent)
	}
	return state
}

func lineIDsFromLogicalLines(lines []LogicalLine) []LogicalLineID {
	ids := make([]LogicalLineID, 0, len(lines))
	for _, line := range lines {
		ids = append(ids, line.ID)
	}
	return ids
}

func cloneMutableFramePointer(frame *MutableFrame) *MutableFrame {
	if frame == nil {
		return nil
	}
	cloned := cloneMutableFrame(*frame)
	return &cloned
}

func cloneMutableFrame(frame MutableFrame) MutableFrame {
	frame.Rows = cloneLogicalLineDrafts(frame.Rows)
	return frame
}

func cloneSealedFrame(frame SealedFrame) SealedFrame {
	frame.Lines = cloneLogicalLines(frame.Lines)
	return frame
}

func cloneSealedFrames(frames []SealedFrame) []SealedFrame {
	if len(frames) == 0 {
		return nil
	}
	out := make([]SealedFrame, len(frames))
	for i := range frames {
		out[i] = cloneSealedFrame(frames[i])
	}
	return out
}

func cloneTransientFramePointer(frame *TransientFrame) *TransientFrame {
	if frame == nil {
		return nil
	}
	cloned := cloneTransientFrame(*frame)
	return &cloned
}

func cloneTransientFrame(frame TransientFrame) TransientFrame {
	frame.Rows = cloneLogicalLineDrafts(frame.Rows)
	return frame
}

func cloneLogicalLineDrafts(drafts []LogicalLineDraft) []LogicalLineDraft {
	if len(drafts) == 0 {
		return nil
	}
	out := make([]LogicalLineDraft, len(drafts))
	for i, draft := range drafts {
		out[i] = draft
		out[i].Line = cloneLogicalLine(draft.Line)
	}
	return out
}

func cloneLogicalLines(lines []LogicalLine) []LogicalLine {
	if len(lines) == 0 {
		return nil
	}
	out := make([]LogicalLine, len(lines))
	for i := range lines {
		out[i] = cloneLogicalLine(lines[i])
	}
	return out
}

func sealReasonFromFrameReason(reason FrameReason) SealReason {
	switch reason {
	case FrameReasonAltEnter:
		return SealReasonAltEnter
	case FrameReasonResize:
		return SealReasonUnknown
	case FrameReasonTerminalClose:
		return SealReasonTerminalClose
	default:
		return SealReasonUnknown
	}
}
