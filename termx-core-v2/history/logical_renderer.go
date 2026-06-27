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
	if decision.ClosePrimaryFrameBeforeStream {
		// 中文说明：primary screen app 结束后出现新的普通 PTY 输出时，旧 current
		// frame 必须先离开 mutable ownership；否则 shell prompt 会在 projection 中
		// 插到旧 current frame 前面，形成与真实 PTY 顺序相反的历史。
		next, err := renderer.frames.ClosePrimaryCurrent(SealReasonSessionClose)
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
		if decision.ConsumeClearBoundary && event.Op != nil && isEraseDisplayAllOp(*event.Op) {
			var mutations []HistoryMutation
			streamMutations, err := renderer.stream.ClearScreenOwnership()
			if err != nil {
				return nil, err
			}
			mutations = append(mutations, streamMutations...)
			// 中文说明：ED2 的旧屏内容由 vterm ordered scroll-out proof 表达；
			// 这里只清 current frame ownership，不能再 archive 同一旧屏形成重复 truth。
			frameMutations, err := renderer.frames.ClearPrimaryCurrent(FrameReasonPrimaryRepaint)
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
		if event.ClearScrollOut && !decision.ConsumeClearScrollOutProof {
			return nil, nil
		}
		if decision.ConsumeScrollOutProof && event.ScrollOut != nil {
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
	case HistorySemanticEventClearScrollback:
		if renderer.stream != nil {
			renderer.stream.ResetForClearScrollback()
		}
		if renderer.frames != nil {
			renderer.frames.ResetForClearScrollback()
		}
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
