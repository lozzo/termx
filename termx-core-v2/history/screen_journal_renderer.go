package history

// NewScreenBackedHistoryJournalRenderer 创建只更新 ScreenHistoryBuffer 的 journal renderer。
// domain owner 是 ScreenHistoryBuffer：journal 在该路径里只承载 boundary/meta/event
// 命令，不再产出 logical-line/frame mutation。调用方必须把同一个 buffer 交给
// ScreenBackedHistoryStore，HistoryWindow/Copy 才能读取同一份 physical row truth。
func NewScreenBackedHistoryJournalRenderer(buffer *ScreenHistoryBuffer) HistoryJournalRenderer {
	if buffer == nil {
		buffer = NewScreenHistoryBuffer(80, 24)
	}
	return &screenBackedJournalRenderer{buffer: buffer}
}

type screenBackedJournalRenderer struct {
	buffer *ScreenHistoryBuffer
}

func (renderer *screenBackedJournalRenderer) ApplyJournal(journal HistoryJournal) (HistoryMutationBatch, error) {
	if renderer == nil {
		return HistoryMutationBatch{}, nil
	}
	renderer.ensureScreenBuffer(journal.Size, journal.Seq)
	for _, item := range journal.Items {
		if err := renderer.applyJournalItem(item, journal.Seq, journal.Size); err != nil {
			return HistoryMutationBatch{}, err
		}
	}
	// 中文说明：screen-backed journal renderer 的正文 truth 已经写入
	// ScreenHistoryBuffer；返回空 mutation batch 是刻意的隔离边界，防止旧
	// logical/frame reducer 再写出第二份 history truth。
	return HistoryMutationBatch{Seq: journal.Seq}, nil
}

func (renderer *screenBackedJournalRenderer) applyJournalItem(item HistoryJournalItem, seq uint64, size TerminalSemanticSize) error {
	switch item.Kind {
	case HistoryJournalItemOrdinaryLineBatch:
		// 中文说明：ordinary logical-line batch 是旧 ingest truth。screen-backed
		// 路径不能把它重新 seal 成 physical history；后续默认接入必须让
		// ScreenHistoryBuffer 直接消费同一 vterm transaction ops。
		return nil
	case HistoryJournalItemBoundary:
		if item.Boundary == nil {
			return ErrHistoryInvalidMutation
		}
		return renderer.applyBoundary(*item.Boundary, seq, size)
	case HistoryJournalItemScrollOutProof:
		if item.ScrollOut == nil {
			return ErrHistoryInvalidMutation
		}
		return renderer.buffer.sealScrollOutProofRows(item.ScrollOut.Rows, seq)
	case HistoryJournalItemFrameEvent:
		if item.Frame == nil {
			return ErrHistoryInvalidMutation
		}
		return renderer.applyFrameEvent(*item.Frame, seq)
	default:
		return ErrHistoryInvalidMutation
	}
}

func (renderer *screenBackedJournalRenderer) applyBoundary(boundary HistoryJournalBoundary, seq uint64, size TerminalSemanticSize) error {
	if boundary.Size.Cols > 0 || boundary.Size.Rows > 0 {
		size = boundary.Size
	}
	switch boundary.Kind {
	case HistoryJournalBoundaryED2:
		renderer.buffer.clearPrimaryFrameRows(seq)
	case HistoryJournalBoundaryED3:
		return nil
	case HistoryJournalBoundaryRIS:
		renderer.buffer.clearPrimaryFrameRows(seq)
		renderer.buffer.clearAltRows(seq)
	case HistoryJournalBoundaryResize:
		renderer.ensureScreenBuffer(size, seq)
	case HistoryJournalBoundaryAltEnter:
		renderer.buffer.enterAltScreen(seq, true)
	case HistoryJournalBoundaryAltExit:
		renderer.buffer.leaveAltScreen(true)
	case HistoryJournalBoundarySyncBegin, HistoryJournalBoundarySyncEnd:
		return nil
	default:
		return ErrHistoryInvalidMutation
	}
	return nil
}

func (renderer *screenBackedJournalRenderer) applyFrameEvent(event HistoryJournalFrameEvent, seq uint64) error {
	switch event.Kind {
	case HistoryJournalFrameReplacePrimary:
		if event.Frame == nil {
			return nil
		}
		return renderer.buffer.applyPrimaryFrameRows(*event.Frame, event.TouchedRows, seq)
	case HistoryJournalFrameArchivePrimary:
		return renderer.buffer.sealPrimaryVisibleRowsAs(seq, HistorySegmentArchivedPrimaryFrame)
	case HistoryJournalFrameClosePrimary, HistoryJournalFrameFinalPrimary:
		return renderer.buffer.sealPrimaryVisibleRowsAs(seq, HistorySegmentCommitted)
	case HistoryJournalFrameClearPrimary:
		renderer.buffer.clearPrimaryFrameRows(seq)
	case HistoryJournalFrameReplaceAlt:
		if event.Frame == nil {
			return nil
		}
		return renderer.buffer.applyAltFrameRows(*event.Frame, seq)
	case HistoryJournalFrameClearAlt:
		renderer.buffer.clearAltRows(seq)
	default:
		return ErrHistoryInvalidMutation
	}
	return nil
}

func (renderer *screenBackedJournalRenderer) ensureScreenBuffer(size TerminalSemanticSize, seq uint64) {
	if renderer.buffer == nil {
		cols := size.Cols
		rows := size.Rows
		if cols <= 0 {
			cols = 80
		}
		if rows <= 0 {
			rows = 24
		}
		renderer.buffer = NewScreenHistoryBuffer(cols, rows)
		return
	}
	if size.Cols > 0 || size.Rows > 0 {
		renderer.buffer.resizeMutableScreen(size.Cols, size.Rows, seq)
	}
}
