package history

// NewScreenBackedHistoryLogicalRenderer 创建直接写入 ScreenHistoryBuffer 的
// transaction renderer。domain owner 是 ScreenHistoryBuffer；truth source 是同一
// vterm TerminalSemanticTransaction 与 lifecycle CloseReason。返回的 mutation batch
// 始终为空，调用方必须使用同一个 buffer 创建 ScreenBackedHistoryStore 才能查询。
func NewScreenBackedHistoryLogicalRenderer(buffer *ScreenHistoryBuffer) HistoryLogicalRenderer {
	if buffer == nil {
		buffer = NewScreenHistoryBuffer(80, 24)
	}
	return &screenBackedLogicalRenderer{buffer: buffer}
}

// NewScreenBackedHistoryRenderers 创建共享 ScreenHistoryBuffer 的 transaction 与
// compact journal renderer。调用边界是 Terminal 默认 production history path：
// transaction consumer 负责普通输出和 screen repaint ops，journal renderer 只保留
// boundary/meta/event backlog，不得重新产出 logical-line 正文 mutation。
func NewScreenBackedHistoryRenderers(buffer *ScreenHistoryBuffer) (HistoryLogicalRenderer, HistoryJournalRenderer) {
	if buffer == nil {
		buffer = NewScreenHistoryBuffer(80, 24)
	}
	return NewScreenBackedHistoryLogicalRenderer(buffer), NewScreenBackedHistoryJournalRenderer(buffer)
}

type screenBackedLogicalRenderer struct {
	buffer *ScreenHistoryBuffer
}

func (renderer *screenBackedLogicalRenderer) Apply(tx TerminalSemanticTransaction, decision HistoryDecision) (HistoryMutationBatch, error) {
	if renderer == nil {
		return HistoryMutationBatch{}, nil
	}
	if renderer.buffer == nil {
		renderer.ensureScreenBuffer(tx.Size, tx.Seq)
	}
	if renderer.buffer == nil {
		return HistoryMutationBatch{Seq: tx.Seq}, nil
	}
	if tx.Seq > 0 && tx.Seq <= renderer.buffer.AppliedSeq {
		return HistoryMutationBatch{Seq: tx.Seq}, nil
	}
	renderer.ensureScreenBuffer(tx.Size, tx.Seq)
	if decision.ClosePrimaryFrameBeforePrimaryReplace || decision.ClosePrimaryFrameBeforeStream {
		// 中文说明：这些 decision 表达 primary mutable ownership 离开当前屏。
		// screen-backed path 必须先 seal 当前 physical rows，再消费本 transaction
		// 的后续普通输出或 alt enter，避免 prompt/alt transient 被并入旧 frame。
		if err := renderer.buffer.sealPrimaryVisibleRowsAs(tx.Seq, HistorySegmentCommitted); err != nil {
			return HistoryMutationBatch{}, err
		}
	}
	archivedPrimaryBeforeOps := false
	if decision.PublishPrimaryFrame && decision.ArchivePrimaryAfterPrimaryFrame && tx.PrimaryFrame != nil {
		if err := renderer.applyPrimaryFrame(tx, decision); err != nil {
			return HistoryMutationBatch{}, err
		}
		if err := renderer.buffer.sealPrimaryVisibleRowsAs(tx.Seq, HistorySegmentArchivedPrimaryFrame); err != nil {
			return HistoryMutationBatch{}, err
		}
		archivedPrimaryBeforeOps = true
	}
	committedBeforeOps := len(renderer.buffer.Committed)
	if err := renderer.buffer.ApplyTransaction(tx); err != nil {
		return HistoryMutationBatch{}, err
	}
	if decision.Mode == HistoryOutputModeOrdinaryStream || decision.ConsumeStreamOps {
		if err := renderer.sealOrdinaryHardLineBoundaries(tx); err != nil {
			return HistoryMutationBatch{}, err
		}
	}
	if decision.ConsumeScrollOutProof {
		// 中文说明：默认 screen path 已经通过 ordered ops 模拟 scroll/ED2 并 seal
		// physical rows。带 RowSet 的 side proof 很可能对应 ordered ED2/scroll，
		// 只能在没有 ordered owner 时消费；非 RowSet proof 是 vterm 给出的额外
		// scrollback payload，必须作为独立 physical row 保留。
		if err := renderer.buffer.sealScrollOutProofRows(screenBackedSideScrollOutProofs(tx, decision, renderer.buffer.Committed[committedBeforeOps:]), tx.Seq); err != nil {
			return HistoryMutationBatch{}, err
		}
	}
	if decision.PublishPrimaryFrame && !archivedPrimaryBeforeOps && tx.PrimaryFrame != nil {
		if err := renderer.applyPrimaryFrame(tx, decision); err != nil {
			return HistoryMutationBatch{}, err
		}
	}
	if decision.ArchivePrimaryBeforeAlt {
		if err := renderer.buffer.sealPrimaryVisibleRowsAs(tx.Seq, HistorySegmentArchivedPrimaryFrame); err != nil {
			return HistoryMutationBatch{}, err
		}
	}
	if decision.PublishAltFrame && tx.AltFrame != nil {
		if err := renderer.buffer.applyAltFrameRows(*tx.AltFrame, tx.Seq); err != nil {
			return HistoryMutationBatch{}, err
		}
	}
	if decision.ClosePrimaryFrame {
		if err := renderer.buffer.sealPrimaryVisibleRowsAs(tx.Seq, HistorySegmentCommitted); err != nil {
			return HistoryMutationBatch{}, err
		}
	}
	if decision.ClearPrimaryCurrent && !decision.ArchivePrimaryBeforeAlt && !decision.ClosePrimaryFrameBeforeStream && !decision.ClosePrimaryFrameBeforePrimaryReplace {
		renderer.buffer.clearPrimaryFrameRows(tx.Seq)
	}
	if decision.ClearAltFrame {
		renderer.buffer.clearAltRows(tx.Seq)
	}
	renderer.markAppliedSeq(tx.Seq)
	return HistoryMutationBatch{Seq: tx.Seq}, nil
}

func (renderer *screenBackedLogicalRenderer) Close(reason CloseReason) (HistoryMutationBatch, error) {
	if renderer == nil {
		return HistoryMutationBatch{}, nil
	}
	renderer.ensureScreenBuffer(TerminalSemanticSize{}, 0)
	if renderer.buffer == nil {
		return HistoryMutationBatch{}, nil
	}
	seq := renderer.buffer.AppliedSeq + 1
	// 中文说明：process/terminal lifecycle close 不是 PTY transaction；screen-backed
	// truth source 是当前 primary physical rows，因此只 seal 已可见 primary rows，
	// 并丢弃 alt transient，不能从 live snapshot 或旧 journal fallback 补内容。
	if err := renderer.buffer.sealPrimaryVisibleRowsAs(seq, HistorySegmentCommitted); err != nil {
		return HistoryMutationBatch{}, err
	}
	renderer.buffer.clearAltRows(seq)
	renderer.markAppliedSeq(seq)
	_ = reason
	return HistoryMutationBatch{Seq: seq}, nil
}

func (renderer *screenBackedLogicalRenderer) applyPrimaryFrame(tx TerminalSemanticTransaction, decision HistoryDecision) error {
	if renderer == nil || renderer.buffer == nil || tx.PrimaryFrame == nil {
		return nil
	}
	touchedRows := []int(nil)
	if decision.PublishPrimaryFrameTouchedRowsOnly {
		touchedRows = tx.PrimaryFrameTouchedRows
	}
	return renderer.buffer.applyPrimaryFrameRows(*tx.PrimaryFrame, touchedRows, tx.Seq)
}

func (renderer *screenBackedLogicalRenderer) sealOrdinaryHardLineBoundaries(tx TerminalSemanticTransaction) error {
	if renderer == nil || renderer.buffer == nil || renderer.buffer.InAlt {
		return nil
	}
	for _, op := range tx.Ops {
		if op.Code != vtermScreenOpControl() {
			continue
		}
		switch op.Control {
		case "lf", "ind", "nel":
			// 中文说明：ordinary stdout 的 hard line boundary 是 physical row
			// 离开 mutable ordinary frontier 的语义。这里只在 ordinary decision 下
			// seal，不影响 primary screen app 的 repaint/scroll 事务。
			if err := renderer.buffer.sealPrimaryRowAt(op.Row, tx.Seq); err != nil {
				return err
			}
		}
	}
	return nil
}

func (renderer *screenBackedLogicalRenderer) ensureScreenBuffer(size TerminalSemanticSize, seq uint64) {
	if renderer == nil {
		return
	}
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

func (renderer *screenBackedLogicalRenderer) markAppliedSeq(seq uint64) {
	if renderer == nil || renderer.buffer == nil || seq == 0 || seq <= renderer.buffer.AppliedSeq {
		return
	}
	renderer.buffer.AppliedSeq = seq
}

func screenBackedTransactionOwnsScrollOut(tx TerminalSemanticTransaction) bool {
	for _, op := range tx.Ops {
		if len(op.ScrollOut) > 0 {
			return true
		}
		switch op.Code {
		case vtermScreenOpScrollRect():
			return true
		case vtermScreenOpControl():
			switch op.Control {
			case "lf", "ind", "nel", "ed", "dl", "su":
				return true
			}
		}
	}
	return false
}

func screenBackedSideScrollOutProofs(tx TerminalSemanticTransaction, decision HistoryDecision, committedByOrderedOps []PhysicalRow) []TerminalSemanticScrollOut {
	if len(tx.PrimaryScrollOut) == 0 {
		return nil
	}
	orderedOwnsScroll := screenBackedTransactionOwnsScrollOut(tx)
	clearTimeProof := screenBackedTransactionHasClearTimeProof(tx)
	preExistingBudget := screenBackedPreExistingScrollOutBudget(tx, decision)
	committedTextBudget := map[string]int(nil)
	if orderedOwnsScroll && len(committedByOrderedOps) > 0 {
		committedTextBudget = make(map[string]int, len(committedByOrderedOps))
		for _, row := range committedByOrderedOps {
			text := rowText(row.Cells)
			if text == "" {
				continue
			}
			committedTextBudget[text]++
		}
	}
	out := make([]TerminalSemanticScrollOut, 0, len(tx.PrimaryScrollOut))
	for _, proof := range tx.PrimaryScrollOut {
		if preExistingBudget > 0 {
			preExistingBudget--
			continue
		}
		if orderedOwnsScroll {
			text := rowText(cellsFromScrollOutProof(proof))
			if committedTextBudget[text] > 0 {
				committedTextBudget[text]--
				continue
			}
			if clearTimeProof && proof.RowSet {
				continue
			}
		}
		out = append(out, cloneTerminalSemanticScrollOut(proof))
	}
	return out
}

func screenBackedPreExistingScrollOutBudget(tx TerminalSemanticTransaction, decision HistoryDecision) int {
	if !decision.SkipPreExistingPrimaryScrollOut {
		return 0
	}
	budget := 0
	seen := false
	for _, row := range tx.PrimaryFrameTouchedRows {
		if row < 0 {
			continue
		}
		if !seen || row < budget {
			budget = row
			seen = true
		}
	}
	if !seen {
		return 0
	}
	return budget
}

func screenBackedTransactionHasClearTimeProof(tx TerminalSemanticTransaction) bool {
	for _, op := range tx.Ops {
		if op.Code != vtermScreenOpControl() || op.Control != "ed" || op.Mode != 2 {
			continue
		}
		if len(op.ScrollOut) > 0 {
			return true
		}
	}
	return false
}
