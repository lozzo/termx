package history

import (
	"sort"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

// NewHistoryLogicalRenderer 组合 stream/frame reducers，创建 semantic transaction
// 到 mutation batch 的唯一转换层。domain owner：history；truth source 只允许是
// vterm TerminalSemanticTransaction、HistoryDecision 和 lifecycle CloseReason。
func NewHistoryLogicalRenderer(stream StreamLineReducer, frames FrameReducer) HistoryLogicalRenderer {
	allocator := newHistoryIDAllocator()
	if stream == nil {
		stream = &streamLineReducer{
			ids:       allocator,
			rowOwners: make(map[int]LogicalLineID),
			lines:     make(map[LogicalLineID]*streamLineDraft),
		}
	}
	if frames == nil {
		frames = &frameReducer{
			nextSessionID: 1,
			nextFrameID:   1,
			ids:           allocator,
		}
	}
	return &logicalRenderer{stream: stream, frames: frames}
}

type logicalRenderer struct {
	stream                  StreamLineReducer
	frames                  FrameReducer
	primaryFrameTouchedRows map[int]struct{}
}

func (renderer *logicalRenderer) Apply(tx TerminalSemanticTransaction, decision HistoryDecision) (HistoryMutationBatch, error) {
	if renderer == nil {
		return HistoryMutationBatch{}, nil
	}
	renderer.primaryFrameTouchedRows = nil
	if decision.PublishPrimaryFrame && decision.PublishPrimaryFrameTouchedRowsOnly {
		renderer.recordPrimaryFrameTouchedRowIndexes(tx.PrimaryFrameTouchedRows, tx.Size)
	}
	var mutations []HistoryMutation
	if decision.ClosePrimaryFrameBeforeStream {
		// 中文说明：primary screen app 结束后出现新的普通 PTY 输出时，旧 current
		// frame 必须先离开 mutable ownership；若本 transaction 带有 vterm 当前屏幕
		// proof，必须用该 proof 收束 final frame，并排除本次 ordinary stream 触达的
		// rows，否则一闪而过的旧 current 行会被错误 seal，prompt 行也会重复进入 frame。
		excludedRows := renderer.touchedRowsForTransaction(tx)
		var next []HistoryMutation
		var err error
		if tx.PrimaryFrame != nil {
			next, err = renderer.frames.ClosePrimaryCurrentFromFrameExcludingRows(*tx.PrimaryFrame, excludedRows, SealReasonSessionClose)
		} else {
			next, err = renderer.frames.ClosePrimaryCurrent(SealReasonSessionClose)
		}
		if err != nil {
			return HistoryMutationBatch{}, err
		}
		mutations = append(mutations, next...)
	}
	for _, event := range HistorySemanticEventsFromTransaction(tx) {
		next, err := renderer.applyEvent(event, decision)
		if err != nil {
			return HistoryMutationBatch{}, err
		}
		mutations = append(mutations, next...)
	}
	if decision.SealOpenLine {
		next, err := renderer.stream.SealOpenLine(SealReasonLineFeed)
		if err != nil {
			return HistoryMutationBatch{}, err
		}
		mutations = append(mutations, next...)
	}
	if decision.ClosePrimaryFrame {
		next, err := renderer.frames.ClosePrimaryCurrent(SealReasonSessionClose)
		if err != nil {
			return HistoryMutationBatch{}, err
		}
		mutations = append(mutations, next...)
	}
	if decision.NonHistoryBoundary && len(mutations) == 0 {
		next, err := renderer.frames.ApplyNonHistoryBoundary(FrameReasonResize)
		if err != nil {
			return HistoryMutationBatch{}, err
		}
		mutations = append(mutations, next...)
	}
	return HistoryMutationBatch{Seq: tx.Seq, Mutations: mutations}, nil
}

func (renderer *logicalRenderer) Close(reason CloseReason) (HistoryMutationBatch, error) {
	if renderer == nil {
		return HistoryMutationBatch{}, nil
	}
	var mutations []HistoryMutation
	streamMutations, err := renderer.stream.SealOpenLine(sealReasonFromCloseReason(reason))
	if err != nil {
		return HistoryMutationBatch{}, err
	}
	mutations = append(mutations, streamMutations...)
	frameMutations, err := renderer.frames.ClosePrimaryCurrent(sealReasonFromCloseReason(reason))
	if err != nil {
		return HistoryMutationBatch{}, err
	}
	mutations = append(mutations, frameMutations...)
	altMutations, err := renderer.frames.ClearAltCurrent(FrameReasonTerminalClose)
	if err != nil {
		return HistoryMutationBatch{}, err
	}
	mutations = append(mutations, altMutations...)
	return HistoryMutationBatch{Mutations: mutations}, nil
}

func (renderer *logicalRenderer) applyEvent(event HistorySemanticEvent, decision HistoryDecision) ([]HistoryMutation, error) {
	switch event.Kind {
	case HistorySemanticEventOp:
		if decision.PublishPrimaryFrame && decision.PublishPrimaryFrameTouchedRowsOnly && event.Op != nil {
			renderer.recordPrimaryFrameTouchedRows(*event.Op, event.Size)
		}
		if decision.ConsumeClearBoundary && event.Op != nil && isEraseDisplayAllOp(*event.Op) {
			var mutations []HistoryMutation
			streamMutations, err := renderer.stream.ClearScreenOwnership()
			if err != nil {
				return nil, err
			}
			mutations = append(mutations, streamMutations...)
			// 中文说明：ED2 是 repaint boundary，不是“每一帧都进入历史”的信号。
			// 旧屏内容能否进入 history，只能由同一 transaction 的 clear-time
			// scroll-out proof 证明；不能靠早前 payload scroll-out 留下的状态记忆，
			// 否则 pseudo-TUI 后续 repaint 会把 transient tail 一帧帧 seal 进去。
			var frameMutations []HistoryMutation
			frameMutations, err = renderer.frames.ClearPrimaryCurrent(FrameReasonPrimaryRepaint)
			if err != nil {
				return nil, err
			}
			mutations = append(mutations, frameMutations...)
			return mutations, nil
		}
		if (decision.Mode == HistoryOutputModeOrdinaryStream || decision.ConsumeStreamOps) && event.Op != nil {
			return renderer.stream.ApplyOp(*event.Op)
		}
	case HistorySemanticEventPrimaryScrollOut:
		if event.ClearScrollOut && !decision.ConsumeClearTimeScrollOutProof {
			return nil, nil
		}
		if decision.ConsumeScrollOutProof && event.ScrollOut != nil {
			proof := *event.ScrollOut
			if event.ClearScrollOut {
				proofs := renderer.frames.FilterPrimaryScrollOutRows([]TerminalSemanticScrollOut{proof})
				if len(proofs) == 0 {
					return nil, nil
				}
				proof = proofs[0]
			}
			mutations, err := renderer.stream.SealScrollOut(proof)
			if err != nil {
				return nil, err
			}
			return mutations, nil
		}
	case HistorySemanticEventPrimaryFrame:
		if decision.PublishPrimaryFrame && event.Frame != nil {
			if decision.PublishPrimaryFrameTouchedRowsOnly {
				return renderer.frames.ReplacePrimaryTouchedRows(*event.Frame, renderer.sortedPrimaryFrameTouchedRows(), FrameReasonPrimaryRepaint)
			}
			return renderer.frames.ReplacePrimaryCurrent(*event.Frame, FrameReasonPrimaryRepaint)
		}
	case HistorySemanticEventAltEnter:
		if decision.ArchivePrimaryBeforeAlt {
			// 中文说明：alt enter 的 primary 清理必须由 archive boundary 完成；
			// 不能重复提交 clear/fallback mutation，否则 frame journal 会变成第二份 truth。
			mutations, err := renderer.frames.ArchivePrimaryCurrent(SealReasonAltEnter)
			if err != nil {
				return nil, err
			}
			return mutations, nil
		}
		return nil, nil
	case HistorySemanticEventAltExit:
		if decision.ClearAltFrame {
			return renderer.frames.ClearAltCurrent(FrameReasonAltExit)
		}
	case HistorySemanticEventAltFrame:
		if decision.PublishAltFrame && event.Frame != nil {
			return renderer.frames.ReplaceAltCurrent(*event.Frame)
		}
	case HistorySemanticEventResize, HistorySemanticEventFullReplace:
		if decision.NonHistoryBoundary {
			return renderer.frames.ApplyNonHistoryBoundary(FrameReasonResize)
		}
	case HistorySemanticEventClearScrollback:
		// 中文说明：clear-scrollback 是 scrollback 页边界，不是 authoritative
		// history truncate。renderer 不能重置 stream/frame ownership，否则同一
		// terminal identity 下的旧页会被当成从未存在。
		return []HistoryMutation{{Kind: HistoryMutationClearScrollback, Reason: SealReasonFullReplace}}, nil
	case HistorySemanticEventReset:
		var mutations []HistoryMutation
		streamMutations, err := renderer.stream.SealOpenLine(SealReasonFullReplace)
		if err != nil {
			return nil, err
		}
		mutations = append(mutations, streamMutations...)
		frameMutations, err := renderer.frames.ClearPrimaryCurrent(FrameReasonPrimaryRepaint)
		if err != nil {
			return nil, err
		}
		mutations = append(mutations, frameMutations...)
		altMutations, err := renderer.frames.ClearAltCurrent(FrameReasonAltExit)
		if err != nil {
			return nil, err
		}
		mutations = append(mutations, altMutations...)
		return mutations, nil
	}
	return nil, nil
}

func (renderer *logicalRenderer) recordPrimaryFrameTouchedRows(op TerminalSemanticOp, size TerminalSemanticSize) {
	if renderer == nil {
		return
	}
	markRow := func(row int) {
		if row < 0 {
			return
		}
		if size.Rows > 0 && row >= size.Rows {
			return
		}
		if renderer.primaryFrameTouchedRows == nil {
			renderer.primaryFrameTouchedRows = make(map[int]struct{})
		}
		renderer.primaryFrameTouchedRows[row] = struct{}{}
	}
	markRange := func(start int, end int) {
		if end < start {
			start, end = end, start
		}
		if size.Rows > 0 && end > size.Rows {
			end = size.Rows
		}
		for row := start; row < end; row++ {
			markRow(row)
		}
	}
	switch op.Code {
	case vterm.ScreenOpWriteSpan, vterm.ScreenOpClearToEOL:
		markRow(op.Row)
	case vterm.ScreenOpClearRect:
		markRange(op.Rect.Y, op.Rect.Y+op.Rect.Height)
	case vterm.ScreenOpScrollRect:
		markRange(op.Rect.Y, op.Rect.Y+op.Rect.Height)
	case vterm.ScreenOpCopyRect:
		markRange(op.DstY, op.DstY+op.Src.Height)
	case vterm.ScreenOpControl:
		switch op.Control {
		case "el", "ech", "dch", "ich":
			markRow(op.Row)
		case "ed":
			switch op.Mode {
			case 1:
				markRange(0, op.Row+1)
			case 2, 3:
				markRange(0, size.Rows)
			default:
				markRange(op.Row, size.Rows)
			}
		case "il", "dl", "su", "sd":
			if op.Bottom > op.Row {
				markRange(op.Row, op.Bottom)
			} else {
				markRow(op.Row)
			}
		case "ri":
			markRow(op.Row)
		}
	}
}

func (renderer *logicalRenderer) recordPrimaryFrameTouchedRowIndexes(rows []int, size TerminalSemanticSize) {
	if renderer == nil {
		return
	}
	for _, row := range rows {
		if row < 0 {
			continue
		}
		if size.Rows > 0 && row >= size.Rows {
			continue
		}
		if renderer.primaryFrameTouchedRows == nil {
			renderer.primaryFrameTouchedRows = make(map[int]struct{})
		}
		renderer.primaryFrameTouchedRows[row] = struct{}{}
	}
}

func (renderer *logicalRenderer) touchedRowsForTransaction(tx TerminalSemanticTransaction) []int {
	if renderer == nil {
		return nil
	}
	saved := renderer.primaryFrameTouchedRows
	renderer.primaryFrameTouchedRows = nil
	for _, op := range tx.Ops {
		renderer.recordPrimaryFrameTouchedRows(op, tx.Size)
	}
	rows := renderer.sortedPrimaryFrameTouchedRows()
	renderer.primaryFrameTouchedRows = saved
	return rows
}

func (renderer *logicalRenderer) sortedPrimaryFrameTouchedRows() []int {
	if renderer == nil || len(renderer.primaryFrameTouchedRows) == 0 {
		return nil
	}
	rows := make([]int, 0, len(renderer.primaryFrameTouchedRows))
	for row := range renderer.primaryFrameTouchedRows {
		rows = append(rows, row)
	}
	sort.Ints(rows)
	return rows
}

func sealReasonFromCloseReason(reason CloseReason) SealReason {
	switch reason {
	case CloseReasonProcessExit, CloseReasonTerminalKill, CloseReasonTerminalRemove, CloseReasonDaemonShutdown:
		return SealReasonTerminalClose
	case CloseReasonSessionBoundary:
		return SealReasonSessionClose
	default:
		return SealReasonUnknown
	}
}
