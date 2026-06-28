package history

import (
	"time"

	xansi "github.com/charmbracelet/x/ansi"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

// HistorySemanticEventKind 枚举 R319 后 history renderer 内部使用的有序语义事件。
// 这些事件来自同一份 TerminalSemanticTransaction，不允许从 raw PTY 或 live
// snapshot 重新合成。
type HistorySemanticEventKind string

const (
	HistorySemanticEventOp               HistorySemanticEventKind = "op"
	HistorySemanticEventPrimaryScrollOut HistorySemanticEventKind = "primary-scroll-out"
	HistorySemanticEventPrimaryFrame     HistorySemanticEventKind = "primary-frame"
	HistorySemanticEventAltFrame         HistorySemanticEventKind = "alt-frame"
	HistorySemanticEventAltEnter         HistorySemanticEventKind = "alt-enter"
	HistorySemanticEventAltExit          HistorySemanticEventKind = "alt-exit"
	HistorySemanticEventResize           HistorySemanticEventKind = "resize"
	HistorySemanticEventFullReplace      HistorySemanticEventKind = "full-replace"
	HistorySemanticEventClearScrollback  HistorySemanticEventKind = "clear-scrollback"
	HistorySemanticEventReset            HistorySemanticEventKind = "reset"
	HistorySemanticEventClose            HistorySemanticEventKind = "close"
)

// HistorySemanticEventOrderSource 说明 event 的顺序真值来自哪里。
// domain owner：vterm 只给 Ops 提供精确 op 间顺序；scroll-out proof、frame payload
// 和 full replace 当前仍是 transaction side proof，history 不能伪造它们在 raw
// stream 中的精确位置。
type HistorySemanticEventOrderSource string

const (
	// HistorySemanticEventOrderFromOps 表示 event 顺序直接来自 tx.Ops，renderer 可以
	// 按 Order 与其它 op 级 event 精确合并。
	HistorySemanticEventOrderFromOps HistorySemanticEventOrderSource = "ops"
	// HistorySemanticEventOrderFromTransactionSideProof 表示 event 来自 transaction
	// 级 side proof。它进入同一消费链，但不宣称拥有 op 间精确顺序。
	HistorySemanticEventOrderFromTransactionSideProof HistorySemanticEventOrderSource = "transaction-side-proof"
	// HistorySemanticEventOrderFromLifecycle 表示 event 来自 process/terminal close
	// 等非 PTY lifecycle boundary。
	HistorySemanticEventOrderFromLifecycle HistorySemanticEventOrderSource = "lifecycle"
)

// HistorySemanticEvent 是 renderer 内部的 ordered event 形状。它把 ops、scroll-out
// proof 和 frame payload 放到同一条消费链上，便于 harness 检查是否漏处理某类
// terminal semantic。Order 是 renderer 消费顺序；只有 OrderSource 为
// HistorySemanticEventOrderFromOps 时才表示 vterm op 级精确顺序。
type HistorySemanticEvent struct {
	Seq            uint64
	Order          int
	OrderSource    HistorySemanticEventOrderSource
	Kind           HistorySemanticEventKind
	Time           time.Time
	Op             *TerminalSemanticOp
	ScrollOut      *TerminalSemanticScrollOut
	ClearScrollOut bool
	Frame          *TerminalSemanticFrame
	Size           TerminalSemanticSize
	Reason         string
}

// HistorySemanticEventsFromTransaction 把一个 vterm semantic transaction 归一化为
// renderer 内部的 ordered event 链。truth source 仍是同一个 transaction：Ops 按
// vterm 给出的顺序进入；op.ScrollOut 是 vterm 明确挂在该 op 上的 ordered proof。
// 只有 transaction 级 PrimaryScrollOut、PrimaryFrame、AltFrame、Resize 和
// RequiresFullReplace 仍作为 side proof 进入，调用方不得把这些 side proof 当作
// raw stream 中的精确位置。
func HistorySemanticEventsFromTransaction(tx TerminalSemanticTransaction) []HistorySemanticEvent {
	events := make([]HistorySemanticEvent, 0, len(tx.Ops)+len(tx.PrimaryScrollOut)+6)
	nextOrder := 0
	hasAltEnterOp := false
	hasAltExitOp := false
	hasResizeOp := false
	hasClearScrollbackOp := false
	firstEraseDisplayAllOp := firstEraseDisplayAllOpIndex(tx.Ops)
	var orderedScrollOut []TerminalSemanticScrollOut

	appendEvent := func(event HistorySemanticEvent) {
		event.Seq = tx.Seq
		event.Order = nextOrder
		event.Size = tx.Size
		events = append(events, event)
		nextOrder++
	}

	for opIndex, op := range tx.Ops {
		opCopy := cloneTerminalSemanticOp(op)
		for _, proof := range terminalScrollOutProofsFromOp(opCopy) {
			proofCopy := cloneTerminalSemanticScrollOut(proof)
			orderedScrollOut = append(orderedScrollOut, cloneTerminalSemanticScrollOut(proofCopy))
			appendEvent(HistorySemanticEvent{
				OrderSource:    HistorySemanticEventOrderFromOps,
				Kind:           HistorySemanticEventPrimaryScrollOut,
				ScrollOut:      &proofCopy,
				ClearScrollOut: isEraseDisplayAllOp(opCopy) || (firstEraseDisplayAllOp >= 0 && opIndex < firstEraseDisplayAllOp),
			})
		}
		kind := HistorySemanticEventOp
		reason := ""
		switch {
		case isAltModeOp(opCopy):
			if opCopy.Enabled {
				kind = HistorySemanticEventAltEnter
				hasAltEnterOp = true
			} else {
				kind = HistorySemanticEventAltExit
				hasAltExitOp = true
			}
		case opCopy.Code == vterm.ScreenOpResize:
			kind = HistorySemanticEventResize
			hasResizeOp = true
		case isClearScrollbackOp(opCopy):
			kind = HistorySemanticEventClearScrollback
			reason = "ed3"
			hasClearScrollbackOp = true
		case isResetOp(opCopy):
			kind = HistorySemanticEventReset
			reason = "ris"
		}
		appendEvent(HistorySemanticEvent{
			OrderSource: HistorySemanticEventOrderFromOps,
			Kind:        kind,
			Op:          &opCopy,
			Reason:      reason,
		})
	}

	if tx.AltEntered && !hasAltEnterOp {
		appendEvent(HistorySemanticEvent{
			OrderSource: HistorySemanticEventOrderFromTransactionSideProof,
			Kind:        HistorySemanticEventAltEnter,
		})
	}
	if tx.AltExited && !hasAltExitOp {
		appendEvent(HistorySemanticEvent{
			OrderSource: HistorySemanticEventOrderFromTransactionSideProof,
			Kind:        HistorySemanticEventAltExit,
		})
	}
	clearTransactionSideScrollOut := transactionHasEraseDisplayAll(tx) && len(orderedScrollOut) == 0
	for _, proof := range transactionSideScrollOutProofs(tx.PrimaryScrollOut, orderedScrollOut) {
		proofCopy := cloneTerminalSemanticScrollOut(proof)
		appendEvent(HistorySemanticEvent{
			OrderSource:    HistorySemanticEventOrderFromTransactionSideProof,
			Kind:           HistorySemanticEventPrimaryScrollOut,
			ScrollOut:      &proofCopy,
			ClearScrollOut: clearTransactionSideScrollOut,
		})
	}
	if tx.PrimaryFrame != nil {
		appendEvent(HistorySemanticEvent{
			OrderSource: HistorySemanticEventOrderFromTransactionSideProof,
			Kind:        HistorySemanticEventPrimaryFrame,
			Frame:       cloneTerminalSemanticFrame(tx.PrimaryFrame),
		})
	}
	if tx.AltFrame != nil {
		appendEvent(HistorySemanticEvent{
			OrderSource: HistorySemanticEventOrderFromTransactionSideProof,
			Kind:        HistorySemanticEventAltFrame,
			Frame:       cloneTerminalSemanticFrame(tx.AltFrame),
		})
	}
	if !hasResizeOp && isResizeFullReplace(tx) {
		appendEvent(HistorySemanticEvent{
			OrderSource: HistorySemanticEventOrderFromTransactionSideProof,
			Kind:        HistorySemanticEventResize,
			Reason:      tx.FullReplaceReason,
		})
	}
	if tx.RequiresFullReplace {
		appendEvent(HistorySemanticEvent{
			OrderSource: HistorySemanticEventOrderFromTransactionSideProof,
			Kind:        HistorySemanticEventFullReplace,
			Reason:      tx.FullReplaceReason,
		})
	}
	if tx.ClearScrollback && !hasClearScrollbackOp {
		appendEvent(HistorySemanticEvent{
			OrderSource: HistorySemanticEventOrderFromTransactionSideProof,
			Kind:        HistorySemanticEventClearScrollback,
			Reason:      "ed3",
		})
	}
	return events
}

func isAltModeOp(op TerminalSemanticOp) bool {
	return op.Code == vterm.ScreenOpModes && op.Private && (op.Mode == 47 || op.Mode == 1047 || op.Mode == 1049)
}

func isResizeFullReplace(tx TerminalSemanticTransaction) bool {
	return tx.RequiresFullReplace && tx.FullReplaceReason == "resize"
}

func isClearScrollbackOp(op TerminalSemanticOp) bool {
	return op.Code == vterm.ScreenOpControl && op.Control == "ed" && op.Mode == 3
}

func isEraseDisplayAllOp(op TerminalSemanticOp) bool {
	return op.Code == vterm.ScreenOpControl && op.Control == "ed" && op.Mode == 2
}

func transactionHasEraseDisplayAll(tx TerminalSemanticTransaction) bool {
	for _, op := range tx.Ops {
		if isEraseDisplayAllOp(op) {
			return true
		}
	}
	return false
}

func firstEraseDisplayAllOpIndex(ops []TerminalSemanticOp) int {
	for index, op := range ops {
		if isEraseDisplayAllOp(op) {
			return index
		}
	}
	return -1
}

func isResetOp(op TerminalSemanticOp) bool {
	return op.Code == vterm.ScreenOpControl && op.Control == "ris"
}

func terminalScrollOutProofsFromOp(op TerminalSemanticOp) []TerminalSemanticScrollOut {
	if len(op.ScrollOut) == 0 {
		return nil
	}
	out := make([]TerminalSemanticScrollOut, 0, len(op.ScrollOut))
	for _, row := range op.ScrollOut {
		out = append(out, TerminalSemanticScrollOut{
			Cells:      cloneTerminalSemanticCells(row.Cells),
			Runs:       cloneTerminalSemanticRuns(row.Runs),
			Row:        row.Row,
			RowSet:     row.RowSet,
			Wrapped:    row.Wrapped,
			WrappedSet: row.WrappedSet,
		})
	}
	return out
}

func transactionSideScrollOutProofs(primary []TerminalSemanticScrollOut, ordered []TerminalSemanticScrollOut) []TerminalSemanticScrollOut {
	if len(primary) == 0 {
		return nil
	}
	// 中文说明：vterm 会同时给出 op 级 ED2 清屏 proof 和 transaction 级
	// scrollback append proof。前者有精确 op 顺序，后者覆盖同一 write 中后续
	// payload 滚出的行；这里只按前缀去重，不能因为存在 ED2 就丢掉后续 payload。
	start := 0
	for start < len(primary) && start < len(ordered) && terminalScrollOutEqual(primary[start], ordered[start]) {
		start++
	}
	out := make([]TerminalSemanticScrollOut, 0, len(primary)-start)
	for _, proof := range primary[start:] {
		out = append(out, cloneTerminalSemanticScrollOut(proof))
	}
	return out
}

func terminalScrollOutEqual(left TerminalSemanticScrollOut, right TerminalSemanticScrollOut) bool {
	leftCells := normalizedTerminalScrollOutProofCells(left)
	rightCells := normalizedTerminalScrollOutProofCells(right)
	if len(leftCells) != len(rightCells) {
		return false
	}
	for index := range leftCells {
		if leftCells[index] != rightCells[index] {
			return false
		}
	}
	return true
}

func normalizedTerminalScrollOutProofCells(proof TerminalSemanticScrollOut) []Cell {
	if len(proof.Runs) > 0 {
		var cells []Cell
		for _, run := range proof.Runs {
			width := xansi.StringWidth(run.Text)
			if width <= 0 && run.Text == "" {
				continue
			}
			cells = append(cells, Cell{
				Text:       run.Text,
				Width:      width,
				Style:      historyStyleFromTerminal(run.Style),
				LinkURL:    run.Style.LinkURL,
				LinkParams: run.Style.LinkParams,
			})
		}
		return trimTrailingBlankCells(cells)
	}
	return trimTrailingBlankCells(historyCellsFromTerminal(proof.Cells))
}

func cloneTerminalSemanticOp(op TerminalSemanticOp) TerminalSemanticOp {
	op.Cells = cloneTerminalSemanticCells(op.Cells)
	op.Runs = cloneTerminalSemanticRuns(op.Runs)
	op.ScrollOut = cloneTerminalScrollbackRowAppends(op.ScrollOut)
	if op.TailFill != nil {
		fill := *op.TailFill
		op.TailFill = &fill
	}
	return op
}

func cloneTerminalSemanticScrollOut(proof TerminalSemanticScrollOut) TerminalSemanticScrollOut {
	proof.Cells = cloneTerminalSemanticCells(proof.Cells)
	proof.Runs = cloneTerminalSemanticRuns(proof.Runs)
	return proof
}

func cloneTerminalSemanticScrollOuts(proofs []TerminalSemanticScrollOut) []TerminalSemanticScrollOut {
	if len(proofs) == 0 {
		return nil
	}
	out := make([]TerminalSemanticScrollOut, len(proofs))
	for i, proof := range proofs {
		out[i] = cloneTerminalSemanticScrollOut(proof)
	}
	return out
}

func cloneTerminalSemanticFrame(frame *TerminalSemanticFrame) *TerminalSemanticFrame {
	if frame == nil {
		return nil
	}
	out := *frame
	out.Rows = cloneTerminalSemanticRows(frame.Rows)
	return &out
}

func cloneTerminalSemanticRows(rows [][]TerminalSemanticCell) [][]TerminalSemanticCell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]TerminalSemanticCell, len(rows))
	for i := range rows {
		out[i] = cloneTerminalSemanticCells(rows[i])
	}
	return out
}

func cloneTerminalSemanticCells(cells []TerminalSemanticCell) []TerminalSemanticCell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]TerminalSemanticCell, len(cells))
	copy(out, cells)
	return out
}

func cloneTerminalSemanticRuns(runs []TerminalSemanticCellRun) []TerminalSemanticCellRun {
	if len(runs) == 0 {
		return nil
	}
	out := make([]TerminalSemanticCellRun, len(runs))
	copy(out, runs)
	return out
}

func cloneTerminalScrollbackRowAppends(rows []vterm.ScrollbackRowAppend) []vterm.ScrollbackRowAppend {
	if len(rows) == 0 {
		return nil
	}
	out := make([]vterm.ScrollbackRowAppend, len(rows))
	for i, row := range rows {
		out[i] = row
		out[i].Cells = cloneTerminalSemanticCells(row.Cells)
		out[i].Runs = cloneTerminalSemanticRuns(row.Runs)
	}
	return out
}
