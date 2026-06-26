package history

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
	stream StreamLineReducer
	frames FrameReducer
}

func (renderer *logicalRenderer) Apply(tx TerminalSemanticTransaction, decision HistoryDecision) (HistoryMutationBatch, error) {
	if renderer == nil {
		return HistoryMutationBatch{}, nil
	}
	var mutations []HistoryMutation
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
		if decision.Mode == HistoryOutputModeOrdinaryStream && event.Op != nil {
			return renderer.stream.ApplyOp(*event.Op)
		}
	case HistorySemanticEventPrimaryScrollOut:
		if event.ScrollOut != nil {
			return renderer.stream.SealScrollOut(*event.ScrollOut)
		}
	case HistorySemanticEventPrimaryFrame:
		if decision.PublishPrimaryFrame && event.Frame != nil {
			return renderer.frames.ReplacePrimaryCurrent(*event.Frame, FrameReasonPrimaryRepaint)
		}
	case HistorySemanticEventAltEnter:
		if decision.ArchivePrimaryBeforeAlt {
			// 中文说明：alt enter 的 primary 清理必须由 archive boundary 完成；
			// 不能重复提交 clear/fallback mutation，否则 frame journal 会变成第二份 truth。
			return renderer.frames.ArchivePrimaryCurrent(SealReasonAltEnter)
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
	}
	return nil, nil
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
