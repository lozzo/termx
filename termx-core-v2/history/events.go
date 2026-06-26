package history

import (
	"time"

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
	Seq         uint64
	Order       int
	OrderSource HistorySemanticEventOrderSource
	Kind        HistorySemanticEventKind
	Time        time.Time
	Op          *TerminalSemanticOp
	ScrollOut   *TerminalSemanticScrollOut
	Frame       *TerminalSemanticFrame
	Size        TerminalSemanticSize
	Reason      string
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
	hasOrderedScrollOut := false

	appendEvent := func(event HistorySemanticEvent) {
		event.Seq = tx.Seq
		event.Order = nextOrder
		event.Size = tx.Size
		events = append(events, event)
		nextOrder++
	}

	for _, op := range tx.Ops {
		opCopy := cloneTerminalSemanticOp(op)
		for _, proof := range terminalScrollOutProofsFromOp(opCopy) {
			proofCopy := cloneTerminalSemanticScrollOut(proof)
			appendEvent(HistorySemanticEvent{
				OrderSource: HistorySemanticEventOrderFromOps,
				Kind:        HistorySemanticEventPrimaryScrollOut,
				ScrollOut:   &proofCopy,
			})
			hasOrderedScrollOut = true
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
	if !hasOrderedScrollOut {
		for _, proof := range tx.PrimaryScrollOut {
			proofCopy := cloneTerminalSemanticScrollOut(proof)
			appendEvent(HistorySemanticEvent{
				OrderSource: HistorySemanticEventOrderFromTransactionSideProof,
				Kind:        HistorySemanticEventPrimaryScrollOut,
				ScrollOut:   &proofCopy,
			})
		}
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
			Wrapped:    row.Wrapped,
			WrappedSet: row.WrappedSet,
		})
	}
	return out
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
