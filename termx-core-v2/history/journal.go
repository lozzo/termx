package history

import (
	"sort"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

// HistoryJournalSource 描述 compact history journal 的真值入口。
// 当前只允许来自 history SemanticTap/vterm semantic pass 后的
// TerminalSemanticTransaction；失败条件是把 raw PTY、live snapshot diff、
// TUI rows 或 live SurfaceTrack 伪造成 journal 来源。
type HistoryJournalSource string

const (
	HistoryJournalSourceSemanticTapTransaction HistoryJournalSource = "semantic-tap-transaction"
)

// HistoryJournal 是 tap 后 history backlog 的 compact semantic 命令边界。
// domain owner 是 core-v2 history；truth source 是同一次 vterm semantic pass
// 产出的 transaction。它只是未应用命令队列，不提供 history.window/copy/search
// 的 authoritative truth，最终 truth 仍属于 HistoryStore 的 logical-line model。
type HistoryJournal struct {
	TerminalID string
	Seq        uint64
	Size       TerminalSemanticSize
	Source     HistoryJournalSource
	Items      []HistoryJournalItem
}

// HistoryJournalItemKind 标识 compact journal item 的语义种类。
// item 按 terminal semantic order 排列；ordinary batch 不能跨 boundary 合并，
// frame/scroll proof 不能从 live snapshot 反推。
type HistoryJournalItemKind string

const (
	HistoryJournalItemOrdinaryLineBatch HistoryJournalItemKind = "ordinary-line-batch"
	HistoryJournalItemBoundary          HistoryJournalItemKind = "boundary"
	HistoryJournalItemScrollOutProof    HistoryJournalItemKind = "scroll-out-proof"
	HistoryJournalItemFrameEvent        HistoryJournalItemKind = "frame-event"
)

// HistoryJournalItem 是 HistoryJournal 中的一条 history-specific semantic 命令。
// Order 是 journal 内顺序；SemanticOrder/OrderSource 说明它来自 vterm op 级顺序
// 还是 transaction 级 side proof，调用方不能把 side proof 伪装成 raw stream
// 中的精确位置。
type HistoryJournalItem struct {
	Kind          HistoryJournalItemKind
	Order         int
	SemanticOrder int
	OrderSource   HistorySemanticEventOrderSource
	Ordinary      *OrdinaryLineBatch
	Boundary      *HistoryJournalBoundary
	ScrollOut     *HistoryJournalScrollOutProof
	Frame         *HistoryJournalFrameEvent
}

// HistoryJournalOrigin 描述 journal payload 在 terminal semantic 中的来源。
// 它只用于 history state machine 选择后续 reducer 路径，不能扩展为进程名或
// UI pane 状态分支。
type HistoryJournalOrigin string

const (
	HistoryJournalOriginOrdinaryPrimary HistoryJournalOrigin = "ordinary-primary"
)

// OrdinaryLineBatch 表达 ordinary stdout/stderr 的 compact logical-line 生命周期。
// Lines 是本 journal 内已经按 terminal 语义 sealed 的 logical lines；OpenUpdate
// 只是推进 renderer-owned open line 的命令，不能让 tap 或 journal 成为 open-line
// truth owner。
type OrdinaryLineBatch struct {
	Cols       int
	Lines      []JournalLogicalLine
	OpenUpdate *JournalOpenLineUpdate
	Commands   []JournalOpenLineCommand
	Origin     HistoryJournalOrigin
}

// JournalLogicalLine 是 compact journal 中的 history-owned logical line payload。
// Cells 保存 terminal 语义属性副本；它不是 visual row，也不是 vterm scrollback
// 或 live screen row。
type JournalLogicalLine struct {
	Cells    []Cell
	Runs     []CellRun
	TailFill *RowTailFill
	Wrapped  bool
	Origin   HistoryJournalOrigin
}

// JournalOpenLineUpdate 是 ordinary open line 的命令化更新。
// 它只描述同一 semantic pass 后 open line 应推进到的 payload/cursor；authoritative
// open line 仍由 HistoryJournalRenderer/HistoryStore 持有。
type JournalOpenLineUpdate struct {
	Cells     []Cell
	CursorCol int
	Row       int
	TailFill  *RowTailFill
}

// JournalOpenLineCommandKind 描述 ordinary open line 的 compact command。
// 它是从同一 vterm semantic pass 的 ordered op 裁剪出的 history 专用命令；
// 不能扩展成 raw PTY replay，也不能引用 live snapshot rows。
type JournalOpenLineCommandKind string

const (
	JournalOpenLineCommandWrite     JournalOpenLineCommandKind = "write"
	JournalOpenLineCommandSetCursor JournalOpenLineCommandKind = "set-cursor"
	JournalOpenLineCommandMoveCol   JournalOpenLineCommandKind = "move-col"
	JournalOpenLineCommandMoveRow   JournalOpenLineCommandKind = "move-row"
	JournalOpenLineCommandEraseLine JournalOpenLineCommandKind = "erase-line"
	JournalOpenLineCommandSoftWrap  JournalOpenLineCommandKind = "soft-wrap"
	JournalOpenLineCommandSealLine  JournalOpenLineCommandKind = "seal-line"
)

// JournalOpenLineCommand 是 ordinary journal reducer 的最小可应用命令。
// domain owner 是 history renderer；truth source 是 ordered terminal op。它表达
// write、cursor/edit 和 line seal 生命周期，避免把完整 TerminalSemanticOp 逐条
// 交给 StreamLineReducer 热路径。
type JournalOpenLineCommand struct {
	Kind     JournalOpenLineCommandKind
	Row      int
	Col      int
	Delta    int
	Mode     int
	Cells    []Cell
	TailFill *RowTailFill
}

// HistoryJournalBoundaryKind 枚举会改变 history state machine 的 terminal boundary。
// 这些 boundary 只能来自 ordered semantic op 或 transaction side proof，不能由
// snapshot diff、raw bytes scanner 或程序名推断。
type HistoryJournalBoundaryKind string

const (
	HistoryJournalBoundaryED2       HistoryJournalBoundaryKind = "ed2"
	HistoryJournalBoundaryED3       HistoryJournalBoundaryKind = "ed3"
	HistoryJournalBoundaryRIS       HistoryJournalBoundaryKind = "ris"
	HistoryJournalBoundaryResize    HistoryJournalBoundaryKind = "resize"
	HistoryJournalBoundaryAltEnter  HistoryJournalBoundaryKind = "alt-enter"
	HistoryJournalBoundaryAltExit   HistoryJournalBoundaryKind = "alt-exit"
	HistoryJournalBoundarySyncBegin HistoryJournalBoundaryKind = "sync-begin"
	HistoryJournalBoundarySyncEnd   HistoryJournalBoundaryKind = "sync-end"
)

// HistoryJournalBoundary 是 journal state machine 的控制命令。
// 它不携带 live snapshot；调用方只能据此 flush ordinary state、切换 primary/alt
// frame ownership 或记录 resize/clear/reset boundary。
type HistoryJournalBoundary struct {
	Kind   HistoryJournalBoundaryKind
	Size   TerminalSemanticSize
	Reason string
}

// HistoryJournalScrollOutProof 承载同一 semantic pass 中 primary 内容离开 viewport
// 的 proof。Rows 必须来自 vterm transaction 的 ordered proof 或 transaction side
// proof，不能从最终当前屏幕反推。
type HistoryJournalScrollOutProof struct {
	Rows      []TerminalSemanticScrollOut
	ClearTime bool
}

// HistoryJournalFrameEventKind 描述 screen-app frame journal 的 compact 命令。
// Replace 类事件必须携带 vterm semantic frame proof；Archive/Clear 类事件只允许
// 来自 alt、ED2、RIS 或 lifecycle 等明确 semantic boundary。
type HistoryJournalFrameEventKind string

const (
	HistoryJournalFrameReplacePrimary HistoryJournalFrameEventKind = "replace-primary"
	HistoryJournalFrameArchivePrimary HistoryJournalFrameEventKind = "archive-primary"
	HistoryJournalFrameClearPrimary   HistoryJournalFrameEventKind = "clear-primary"
	HistoryJournalFrameReplaceAlt     HistoryJournalFrameEventKind = "replace-alt"
	HistoryJournalFrameClearAlt       HistoryJournalFrameEventKind = "clear-alt"
	HistoryJournalFrameClosePrimary   HistoryJournalFrameEventKind = "close-primary"
	HistoryJournalFrameFinalPrimary   HistoryJournalFrameEventKind = "final-primary"
)

// HistoryJournalFrameEvent 是 primary/alt mutable frame state machine 的命令。
// Frame 只能来自 TerminalSemanticFrame side proof；TouchedRows 只能来自 vterm 的
// ordered/damage proof；FixedCols 只用于 final/fixed-grid frame 语义，不能被后续
// resize 改写。
type HistoryJournalFrameEvent struct {
	Kind        HistoryJournalFrameEventKind
	Frame       *TerminalSemanticFrame
	TouchedRows []int
	FixedCols   int
	Reason      string
}

// HistoryJournalBuildHook 是测试用诊断 hook，用来证明 journal 裁剪发生在 history
// fan-out 阶段，而不是 SemanticTap live wake 热路径；生产代码不得依赖它。
var HistoryJournalBuildHook func()

// HistoryJournalFromTransaction 把 history semantic consumer 之后的一条 terminal
// semantic transaction 裁剪成 history-specific compact journal。消息链路是
// PTY/resize -> history SemanticTap/vterm -> TerminalSemanticTransaction -> HistoryJournal；
// 本函数不读取 tx.Raw、SourceDamage、live snapshot 或任何 renderer/TUI rows。
func HistoryJournalFromTransaction(terminalID string, tx TerminalSemanticTransaction) HistoryJournal {
	return HistoryJournalFromDecision(terminalID, tx, HistoryDecision{
		Mode:                           HistoryOutputModeOrdinaryStream,
		PublishPrimaryFrame:            true,
		PublishAltFrame:                true,
		ArchivePrimaryBeforeAlt:        true,
		ClearAltFrame:                  true,
		ConsumeScrollOutProof:          true,
		ConsumeClearTimeScrollOutProof: true,
		ConsumeClearBoundary:           true,
	})
}

// HistoryJournalFromDecision 把 terminal classifier 的领域决策编码进 compact journal。
// domain owner 是 history：这里只消费同一份 vterm semantic transaction 和
// HistoryDecision，不读取 live snapshot 或 store payload。失败条件是不能把
// decision 无法证明的 scroll-out/frame proof 写成 history 命令；生产 history
// consumer 应优先使用该入口，避免 journal renderer 和 full transaction renderer
// 形成两套 truth。
func HistoryJournalFromDecision(terminalID string, tx TerminalSemanticTransaction, decision HistoryDecision) HistoryJournal {
	if HistoryJournalBuildHook != nil {
		HistoryJournalBuildHook()
	}
	builder := historyJournalBuilder{
		journal: HistoryJournal{
			TerminalID: terminalID,
			Seq:        tx.Seq,
			Size:       tx.Size,
			Source:     HistoryJournalSourceSemanticTapTransaction,
		},
		ordinary:                        newJournalOrdinaryRecorder(tx.Size.Cols, tx.Size.Rows),
		primaryFrameTouchedRows:         cloneIntSlice(tx.PrimaryFrameTouchedRows),
		skipSideProofFrameEvents:        isResizeFullReplace(tx),
		decision:                        decision,
		closePrimaryBeforeStream:        decision.ClosePrimaryFrameBeforeStream,
		consumeStreamOps:                decision.Mode == HistoryOutputModeOrdinaryStream || decision.ConsumeStreamOps,
		skipClearTimeScrollOut:          !decision.ConsumeClearTimeScrollOutProof,
		inSync:                          tx.SynchronizedActive || (tx.SynchronizedEnd && !tx.SynchronizedBegin),
		trackPrimaryFrameRows:           decision.PublishPrimaryFrame && decision.PublishPrimaryFrameTouchedRowsOnly,
		skipPreExistingPrimaryScrollOut: decision.SkipPreExistingPrimaryScrollOut,
	}
	builder.seedPreExistingPrimaryScrollOutBudget(tx)
	if decision.Mode == HistoryOutputModeOrdinaryStream && !decision.ClosePrimaryFrameBeforeStream {
		builder.closePrimaryBeforeStream = false
	}
	if builder.closePrimaryBeforeStream {
		builder.appendFrameFromSideProof(HistoryJournalFrameEvent{
			Kind:        HistoryJournalFrameClosePrimary,
			Frame:       cloneTerminalSemanticFrame(tx.PrimaryFrame),
			TouchedRows: historyJournalTouchedRowsFromTransaction(tx),
			Reason:      string(SealReasonSessionClose),
		})
		builder.closePrimaryBeforeStream = false
	}
	events := HistorySemanticEventsFromTransaction(tx)
	syncBoundariesInserted := false
	for _, event := range events {
		if !syncBoundariesInserted && event.OrderSource != HistorySemanticEventOrderFromOps {
			builder.appendTransactionLevelSyncBoundaries(tx)
			syncBoundariesInserted = true
		}
		builder.applyEvent(event)
	}
	if !syncBoundariesInserted {
		builder.appendTransactionLevelSyncBoundaries(tx)
	}
	builder.flushOrdinary(HistorySemanticEventOrderFromTransactionSideProof, len(events))
	return builder.journal
}

type historyJournalBuilder struct {
	journal                           HistoryJournal
	ordinary                          journalOrdinaryRecorder
	primaryFrameTouchedRows           []int
	primaryFrameOwnedRows             map[int]struct{}
	ordinaryTouchedRows               map[int]struct{}
	ordinarySealedProofs              map[string]struct{}
	decision                          HistoryDecision
	inAlt                             bool
	inSync                            bool
	skipSideProofFrameEvents          bool
	closePrimaryBeforeStream          bool
	consumeStreamOps                  bool
	skipClearTimeScrollOut            bool
	trackPrimaryFrameRows             bool
	skipPreExistingPrimaryScrollOut   bool
	preExistingPrimaryScrollOutBudget int
	preExistingPrimaryScrollOutSeen   bool
	primaryFrameSideScrollOutCount    int
}

func (builder *historyJournalBuilder) applyEvent(event HistorySemanticEvent) {
	switch event.Kind {
	case HistorySemanticEventOp:
		builder.applyOpEvent(event)
	case HistorySemanticEventPrimaryScrollOut:
		if event.ScrollOut == nil {
			return
		}
		if !builder.decision.ConsumeScrollOutProof {
			return
		}
		if event.ClearScrollOut && builder.skipClearTimeScrollOut {
			return
		}
		if builder.shouldLetOrdinaryOpsOwnScrollOut(event) {
			return
		}
		if builder.shouldSkipUnownedPrimaryFrameScrollOut(event) {
			return
		}
		builder.flushOrdinary(event.OrderSource, event.Order)
		builder.appendScrollOut(event, []TerminalSemanticScrollOut{*event.ScrollOut})
	case HistorySemanticEventPrimaryFrame:
		if event.Frame == nil || builder.skipSideProofFrameEvents {
			return
		}
		if !builder.decision.PublishPrimaryFrame {
			return
		}
		builder.flushOrdinary(event.OrderSource, event.Order)
		if builder.closePrimaryBeforeStream {
			builder.appendFrame(event, HistoryJournalFrameEvent{
				Kind:        HistoryJournalFrameClosePrimary,
				Frame:       cloneTerminalSemanticFrame(event.Frame),
				TouchedRows: builder.sortedOrdinaryTouchedRows(event.Size),
				Reason:      string(SealReasonSessionClose),
			})
			return
		}
		builder.appendFrame(event, HistoryJournalFrameEvent{
			Kind:        HistoryJournalFrameReplacePrimary,
			Frame:       cloneTerminalSemanticFrame(event.Frame),
			TouchedRows: nil,
			Reason:      string(FrameReasonPrimaryRepaint),
		})
		if builder.decision.ArchivePrimaryAfterPrimaryFrame {
			builder.appendFrame(event, HistoryJournalFrameEvent{Kind: HistoryJournalFrameArchivePrimary, Reason: string(SealReasonAltEnter)})
		}
	case HistorySemanticEventAltFrame:
		if event.Frame == nil || builder.skipSideProofFrameEvents {
			return
		}
		if !builder.decision.PublishAltFrame {
			return
		}
		builder.flushOrdinary(event.OrderSource, event.Order)
		builder.appendFrame(event, HistoryJournalFrameEvent{
			Kind:   HistoryJournalFrameReplaceAlt,
			Frame:  cloneTerminalSemanticFrame(event.Frame),
			Reason: string(FrameReasonAltRepaint),
		})
	case HistorySemanticEventAltEnter:
		builder.flushOrdinary(event.OrderSource, event.Order)
		builder.appendBoundary(event, HistoryJournalBoundary{Kind: HistoryJournalBoundaryAltEnter, Size: event.Size, Reason: "alt-enter"})
		if builder.decision.ArchivePrimaryBeforeAlt {
			builder.appendFrame(event, HistoryJournalFrameEvent{Kind: HistoryJournalFrameArchivePrimary, Reason: string(SealReasonAltEnter)})
		}
		builder.inAlt = true
	case HistorySemanticEventAltExit:
		builder.flushOrdinary(event.OrderSource, event.Order)
		builder.appendBoundary(event, HistoryJournalBoundary{Kind: HistoryJournalBoundaryAltExit, Size: event.Size, Reason: "alt-exit"})
		if builder.decision.ClearAltFrame {
			builder.appendFrame(event, HistoryJournalFrameEvent{Kind: HistoryJournalFrameClearAlt, Reason: string(FrameReasonAltExit)})
		}
		builder.inAlt = false
	case HistorySemanticEventResize:
		builder.flushOrdinary(event.OrderSource, event.Order)
		builder.appendBoundary(event, HistoryJournalBoundary{Kind: HistoryJournalBoundaryResize, Size: event.Size, Reason: event.Reason})
	case HistorySemanticEventClearScrollback:
		builder.flushOrdinary(event.OrderSource, event.Order)
		builder.appendBoundary(event, HistoryJournalBoundary{Kind: HistoryJournalBoundaryED3, Size: event.Size, Reason: "ed3"})
	case HistorySemanticEventReset:
		builder.flushOrdinary(event.OrderSource, event.Order)
		builder.appendBoundary(event, HistoryJournalBoundary{Kind: HistoryJournalBoundaryRIS, Size: event.Size, Reason: "ris"})
		builder.appendFrame(event, HistoryJournalFrameEvent{Kind: HistoryJournalFrameClearPrimary, Reason: string(FrameReasonPrimaryRepaint)})
		builder.appendFrame(event, HistoryJournalFrameEvent{Kind: HistoryJournalFrameClearAlt, Reason: string(FrameReasonAltExit)})
	}
}

func (builder *historyJournalBuilder) shouldLetOrdinaryOpsOwnScrollOut(event HistorySemanticEvent) bool {
	if builder.inAlt || builder.inSync || !builder.consumeStreamOps {
		return false
	}
	if builder.decision.Mode != HistoryOutputModeOrdinaryStream {
		return false
	}
	if event.ClearScrollOut {
		return false
	}
	if event.OrderSource == HistorySemanticEventOrderFromTransactionSideProof && !builder.ordinaryProofAlreadyOwned(*event.ScrollOut) {
		return false
	}
	// 中文说明：普通 stdout 的 history truth 是 ordered write/control op
	// 形成的 logical line。vterm 同时给出的 scroll-out proof 只是屏幕
	// visual row 离开 viewport 的证据，不能在 ordinary 模式下把 wrapped
	// row 提前 seal 成第二份 history truth；transaction side proof 也不能
	// 重新追加同一 visual fragment。clear-time 和 primary frame 场景仍由
	// proof/frame state machine 消费。
	return true
}

func (builder *historyJournalBuilder) shouldSkipUnownedPrimaryFrameScrollOut(event HistorySemanticEvent) bool {
	if builder == nil || !builder.trackPrimaryFrameRows || event.ScrollOut == nil || event.ClearScrollOut {
		return false
	}
	if event.OrderSource == HistorySemanticEventOrderFromTransactionSideProof {
		builder.primaryFrameSideScrollOutCount++
		builder.advancePrimaryFrameOwnershipForSideScrollOut(event.Size)
		if builder.skipPreExistingPrimaryScrollOut && builder.preExistingPrimaryScrollOutBudget > 0 {
			builder.preExistingPrimaryScrollOutBudget--
			return true
		}
		return false
	}
	proof := *event.ScrollOut
	if !proof.RowSet {
		return false
	}
	_, owned := builder.primaryFrameOwnedRows[proof.Row]
	return !owned
}

func (builder *historyJournalBuilder) seedPreExistingPrimaryScrollOutBudget(tx TerminalSemanticTransaction) {
	if builder == nil || !builder.skipPreExistingPrimaryScrollOut {
		return
	}
	for _, row := range tx.PrimaryFrameTouchedRows {
		builder.notePreExistingPrimaryScrollOutBudget(row)
	}
}

func (builder *historyJournalBuilder) notePreExistingPrimaryScrollOutBudget(row int) {
	if builder == nil || !builder.skipPreExistingPrimaryScrollOut || row < 0 {
		return
	}
	if !builder.preExistingPrimaryScrollOutSeen || row < builder.preExistingPrimaryScrollOutBudget {
		builder.preExistingPrimaryScrollOutBudget = row
		builder.preExistingPrimaryScrollOutSeen = true
	}
}

func (builder *historyJournalBuilder) advancePrimaryFrameOwnershipForSideScrollOut(size TerminalSemanticSize) {
	if builder == nil || !builder.trackPrimaryFrameRows {
		return
	}
	builder.scrollPrimaryFrameOwnership(vterm.DamageRect{
		X:      0,
		Y:      0,
		Width:  maxInt(1, size.Cols),
		Height: maxInt(1, size.Rows),
	}, -1, size)
}

func (builder *historyJournalBuilder) ordinaryProofAlreadyOwned(proof TerminalSemanticScrollOut) bool {
	if len(builder.ordinarySealedProofs) == 0 {
		return false
	}
	_, ok := builder.ordinarySealedProofs[scrollOutProofSignature(proof)]
	return ok
}

func (builder *historyJournalBuilder) applyOpEvent(event HistorySemanticEvent) {
	if event.Op == nil {
		return
	}
	if boundary, ok := journalBoundaryFromOp(*event.Op, event.Size); ok {
		builder.applyPrimaryFrameOwnershipOp(*event.Op, event.Size)
		builder.flushOrdinary(event.OrderSource, event.Order)
		builder.appendBoundary(event, boundary)
		switch boundary.Kind {
		case HistoryJournalBoundaryAltEnter:
			builder.inAlt = true
			if builder.decision.ArchivePrimaryBeforeAlt {
				builder.appendFrame(event, HistoryJournalFrameEvent{Kind: HistoryJournalFrameArchivePrimary, Reason: string(SealReasonAltEnter)})
			}
		case HistoryJournalBoundaryAltExit:
			builder.inAlt = false
			if builder.decision.ClearAltFrame {
				builder.appendFrame(event, HistoryJournalFrameEvent{Kind: HistoryJournalFrameClearAlt, Reason: string(FrameReasonAltExit)})
			}
		case HistoryJournalBoundarySyncBegin:
			builder.inSync = true
		case HistoryJournalBoundarySyncEnd:
			builder.inSync = false
		}
		if boundary.Kind == HistoryJournalBoundaryED2 && builder.decision.ConsumeClearBoundary {
			builder.appendFrame(event, HistoryJournalFrameEvent{Kind: HistoryJournalFrameClearPrimary, Reason: string(FrameReasonPrimaryRepaint)})
		}
		return
	}
	builder.applyPrimaryFrameOwnershipOp(*event.Op, event.Size)
	if builder.inAlt || builder.inSync || !builder.consumeStreamOps {
		// 中文说明：alt-screen 与 synchronized primary repaint 的 payload 只能由
		// 同一 transaction 的 frame proof 表达；ordered write ops 不能退回
		// ordinary primary batch，否则 screen app 内容会进入 ordinary timeline。
		return
	}
	builder.recordOrdinaryTouchedRows(*event.Op, event.Size)
	if builder.ordinary.ApplyOp(*event.Op) {
		if builder.ordinary.NeedsFlush() {
			builder.flushOrdinary(event.OrderSource, event.Order)
		}
		return
	}
	builder.flushOrdinary(event.OrderSource, event.Order)
	if builder.ordinary.ApplyOp(*event.Op) {
		if builder.ordinary.NeedsFlush() {
			builder.flushOrdinary(event.OrderSource, event.Order)
		}
	}
}

func (builder *historyJournalBuilder) applyPrimaryFrameOwnershipOp(op TerminalSemanticOp, size TerminalSemanticSize) {
	if builder == nil || !builder.trackPrimaryFrameRows {
		return
	}
	mark := func(row int) {
		if row < 0 {
			return
		}
		if size.Rows > 0 && row >= size.Rows {
			return
		}
		builder.notePreExistingPrimaryScrollOutBudget(row)
		if builder.primaryFrameOwnedRows == nil {
			builder.primaryFrameOwnedRows = make(map[int]struct{})
		}
		builder.primaryFrameOwnedRows[row] = struct{}{}
	}
	markRange := func(start int, end int) {
		if end < start {
			start, end = end, start
		}
		if size.Rows > 0 && end > size.Rows {
			end = size.Rows
		}
		for row := start; row < end; row++ {
			mark(row)
		}
	}
	switch op.Code {
	case vterm.ScreenOpWriteSpan, vterm.ScreenOpClearToEOL:
		mark(op.Row)
	case vterm.ScreenOpClearRect:
		markRange(op.Rect.Y, op.Rect.Y+op.Rect.Height)
	case vterm.ScreenOpScrollRect:
		builder.scrollPrimaryFrameOwnership(op.Rect, op.Dy, size)
	case vterm.ScreenOpCopyRect:
		builder.copyPrimaryFrameOwnership(op.Src, op.DstY, size)
	case vterm.ScreenOpControl:
		switch op.Control {
		case "ed":
			switch op.Mode {
			case 1:
				markRange(0, op.Row+1)
			case 2, 3:
				builder.primaryFrameOwnedRows = nil
			default:
				markRange(op.Row, size.Rows)
			}
		case "el", "ech", "dch", "ich":
			mark(op.Row)
		case "lf", "ind", "nel":
			if len(op.ScrollOut) > 0 {
				builder.primaryFrameSideScrollOutCount += len(op.ScrollOut)
				builder.scrollPrimaryFrameOwnership(vterm.DamageRect{
					X:      0,
					Y:      0,
					Width:  maxInt(1, size.Cols),
					Height: maxInt(1, size.Rows),
				}, -len(op.ScrollOut), size)
			}
		case "il", "dl", "su", "sd":
			markRange(op.Row, op.Bottom)
		case "ri":
			mark(op.Row)
		}
	}
}

func (builder *historyJournalBuilder) scrollPrimaryFrameOwnership(rect vterm.DamageRect, dy int, size TerminalSemanticSize) {
	if builder == nil || len(builder.primaryFrameOwnedRows) == 0 || rect.Width <= 0 || rect.Height <= 0 {
		return
	}
	before := builder.primaryFrameOwnedRows
	next := make(map[int]struct{}, len(before))
	for row := range before {
		if row < rect.Y || row >= rect.Y+rect.Height {
			next[row] = struct{}{}
		}
	}
	end := rect.Y + rect.Height
	if size.Rows > 0 && end > size.Rows {
		end = size.Rows
	}
	for row := rect.Y; row < end; row++ {
		if row < 0 {
			continue
		}
		srcRow := row - dy
		if srcRow < rect.Y || srcRow >= rect.Y+rect.Height {
			continue
		}
		if _, owned := before[srcRow]; owned {
			next[row] = struct{}{}
		}
	}
	builder.primaryFrameOwnedRows = next
}

func (builder *historyJournalBuilder) copyPrimaryFrameOwnership(src vterm.DamageRect, dstY int, size TerminalSemanticSize) {
	if builder == nil || len(builder.primaryFrameOwnedRows) == 0 || src.Width <= 0 || src.Height <= 0 {
		return
	}
	for offset := 0; offset < src.Height; offset++ {
		srcRow := src.Y + offset
		if _, owned := builder.primaryFrameOwnedRows[srcRow]; !owned {
			continue
		}
		dstRow := dstY + offset
		if dstRow < 0 {
			continue
		}
		if size.Rows > 0 && dstRow >= size.Rows {
			continue
		}
		builder.primaryFrameOwnedRows[dstRow] = struct{}{}
	}
}

func (builder *historyJournalBuilder) recordOrdinaryTouchedRows(op TerminalSemanticOp, size TerminalSemanticSize) {
	if !historyJournalOpTouchesRows(op) {
		return
	}
	mark := func(row int) {
		if row < 0 {
			return
		}
		if size.Rows > 0 && row >= size.Rows {
			return
		}
		if builder.ordinaryTouchedRows == nil {
			builder.ordinaryTouchedRows = make(map[int]struct{})
		}
		builder.ordinaryTouchedRows[row] = struct{}{}
	}
	markRange := func(start int, end int) {
		if end < start {
			start, end = end, start
		}
		if size.Rows > 0 && end > size.Rows {
			end = size.Rows
		}
		for row := start; row < end; row++ {
			mark(row)
		}
	}
	switch op.Code {
	case vterm.ScreenOpWriteSpan, vterm.ScreenOpClearToEOL:
		mark(op.Row)
	case vterm.ScreenOpClearRect:
		markRange(op.Rect.Y, op.Rect.Y+op.Rect.Height)
	case vterm.ScreenOpScrollRect:
		markRange(op.Rect.Y, op.Rect.Y+op.Rect.Height)
	case vterm.ScreenOpCopyRect:
		markRange(op.DstY, op.DstY+op.Src.Height)
	case vterm.ScreenOpControl:
		switch op.Control {
		case "ed":
			if op.Mode == 2 || op.Mode == 3 {
				markRange(0, size.Rows)
				return
			}
			mark(op.Row)
		case "el", "ech", "dch", "ich":
			mark(op.Row)
		case "il", "dl", "su", "sd":
			markRange(op.Row, op.Bottom)
		case "ri":
			mark(op.Row)
		}
	}
}

func historyJournalOpTouchesRows(op TerminalSemanticOp) bool {
	switch op.Code {
	case vterm.ScreenOpWriteSpan, vterm.ScreenOpClearRect, vterm.ScreenOpClearToEOL, vterm.ScreenOpScrollRect, vterm.ScreenOpCopyRect:
		return true
	case vterm.ScreenOpControl:
		switch op.Control {
		case "ed", "el", "ech", "dch", "ich", "il", "dl", "su", "sd", "ri", "ris":
			return true
		}
	}
	return len(op.ScrollOut) > 0
}

func (builder *historyJournalBuilder) sortedOrdinaryTouchedRows(size TerminalSemanticSize) []int {
	if len(builder.ordinaryTouchedRows) == 0 {
		return nil
	}
	rows := make([]int, 0, len(builder.ordinaryTouchedRows))
	for row := range builder.ordinaryTouchedRows {
		if row < 0 {
			continue
		}
		if size.Rows > 0 && row >= size.Rows {
			continue
		}
		rows = append(rows, row)
	}
	sort.Ints(rows)
	return rows
}

func (builder *historyJournalBuilder) appendTransactionLevelSyncBoundaries(tx TerminalSemanticTransaction) {
	if tx.SynchronizedBegin && !journalHasSyncModeOp(tx, true) {
		builder.flushOrdinary(HistorySemanticEventOrderFromTransactionSideProof, len(builder.journal.Items))
		builder.appendBoundaryFromSideProof(HistoryJournalBoundary{Kind: HistoryJournalBoundarySyncBegin, Size: tx.Size, Reason: "sync-begin"})
	}
	if tx.SynchronizedEnd && !journalHasSyncModeOp(tx, false) {
		builder.flushOrdinary(HistorySemanticEventOrderFromTransactionSideProof, len(builder.journal.Items))
		builder.appendBoundaryFromSideProof(HistoryJournalBoundary{Kind: HistoryJournalBoundarySyncEnd, Size: tx.Size, Reason: "sync-end"})
	}
}

func (builder *historyJournalBuilder) flushOrdinary(source HistorySemanticEventOrderSource, semanticOrder int) {
	batch, ok := builder.ordinary.Flush()
	if !ok {
		return
	}
	builder.recordOrdinarySealedLines(batch.Lines)
	item := HistoryJournalItem{
		Kind:          HistoryJournalItemOrdinaryLineBatch,
		Order:         len(builder.journal.Items),
		SemanticOrder: semanticOrder,
		OrderSource:   source,
		Ordinary:      &batch,
	}
	builder.journal.Items = append(builder.journal.Items, item)
}

func (builder *historyJournalBuilder) recordOrdinarySealedLines(lines []JournalLogicalLine) {
	if len(lines) == 0 {
		return
	}
	if builder.ordinarySealedProofs == nil {
		builder.ordinarySealedProofs = make(map[string]struct{}, len(lines))
	}
	for _, line := range lines {
		signature := journalLogicalLineSignature(line)
		if signature == "" {
			continue
		}
		builder.ordinarySealedProofs[signature] = struct{}{}
	}
}

func (builder *historyJournalBuilder) appendBoundary(event HistorySemanticEvent, boundary HistoryJournalBoundary) {
	item := HistoryJournalItem{
		Kind:          HistoryJournalItemBoundary,
		Order:         len(builder.journal.Items),
		SemanticOrder: event.Order,
		OrderSource:   event.OrderSource,
		Boundary:      &boundary,
	}
	builder.journal.Items = append(builder.journal.Items, item)
}

func (builder *historyJournalBuilder) appendBoundaryFromSideProof(boundary HistoryJournalBoundary) {
	item := HistoryJournalItem{
		Kind:          HistoryJournalItemBoundary,
		Order:         len(builder.journal.Items),
		SemanticOrder: -1,
		OrderSource:   HistorySemanticEventOrderFromTransactionSideProof,
		Boundary:      &boundary,
	}
	builder.journal.Items = append(builder.journal.Items, item)
}

func (builder *historyJournalBuilder) appendScrollOut(event HistorySemanticEvent, rows []TerminalSemanticScrollOut) {
	item := HistoryJournalItem{
		Kind:          HistoryJournalItemScrollOutProof,
		Order:         len(builder.journal.Items),
		SemanticOrder: event.Order,
		OrderSource:   event.OrderSource,
		ScrollOut: &HistoryJournalScrollOutProof{
			Rows:      cloneTerminalSemanticScrollOuts(rows),
			ClearTime: event.ClearScrollOut,
		},
	}
	builder.journal.Items = append(builder.journal.Items, item)
}

func (builder *historyJournalBuilder) appendFrame(event HistorySemanticEvent, frame HistoryJournalFrameEvent) {
	if frame.Kind == HistoryJournalFrameReplacePrimary && len(frame.TouchedRows) == 0 {
		frame.TouchedRows = builder.primaryFrameRowsForReplace(event.Size)
	}
	frame.TouchedRows = cloneIntSlice(frame.TouchedRows)
	item := HistoryJournalItem{
		Kind:          HistoryJournalItemFrameEvent,
		Order:         len(builder.journal.Items),
		SemanticOrder: event.Order,
		OrderSource:   event.OrderSource,
		Frame:         &frame,
	}
	builder.journal.Items = append(builder.journal.Items, item)
}

func (builder *historyJournalBuilder) primaryFrameRowsForReplace(size TerminalSemanticSize) []int {
	if builder == nil {
		return nil
	}
	rows := make(map[int]struct{}, len(builder.primaryFrameOwnedRows)+len(builder.primaryFrameTouchedRows))
	mark := func(row int) {
		if row < 0 {
			return
		}
		if size.Rows > 0 && row >= size.Rows {
			return
		}
		rows[row] = struct{}{}
	}
	for row := range builder.primaryFrameOwnedRows {
		mark(row)
	}
	for _, row := range builder.primaryFrameTouchedRows {
		mark(row)
	}
	if builder.trackPrimaryFrameRows && builder.primaryFrameSideScrollOutCount > 0 && len(builder.primaryFrameTouchedRows) > 0 {
		minTouched := builder.primaryFrameTouchedRows[0]
		maxTouched := builder.primaryFrameTouchedRows[0]
		for _, row := range builder.primaryFrameTouchedRows[1:] {
			if row < minTouched {
				minTouched = row
			}
			if row > maxTouched {
				maxTouched = row
			}
		}
		start := minTouched - builder.primaryFrameSideScrollOutCount
		if start < 0 {
			start = 0
		}
		// 中文说明：vterm 的 transaction side scroll-out 证明没有精确 op order，
		// 但它证明本次 synchronized repaint 内底部 touched row 被整体上移。final/current
		// primary frame 必须接管这个上移后的 frame 区间；被顶出的旧 shell proof 仍由
		// skipPreExistingPrimaryScrollOut 在 scroll-out proof 路径跳过，避免重复 sealed 行。
		for row := start; row <= maxTouched; row++ {
			mark(row)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	out := make([]int, 0, len(rows))
	for row := range rows {
		out = append(out, row)
	}
	sort.Ints(out)
	return out
}

func (builder *historyJournalBuilder) appendFrameFromSideProof(frame HistoryJournalFrameEvent) {
	frame.TouchedRows = cloneIntSlice(frame.TouchedRows)
	item := HistoryJournalItem{
		Kind:          HistoryJournalItemFrameEvent,
		Order:         len(builder.journal.Items),
		SemanticOrder: -1,
		OrderSource:   HistorySemanticEventOrderFromTransactionSideProof,
		Frame:         &frame,
	}
	builder.journal.Items = append(builder.journal.Items, item)
}

func historyJournalTouchedRowsFromTransaction(tx TerminalSemanticTransaction) []int {
	touched := make(map[int]struct{})
	mark := func(row int) {
		if row < 0 {
			return
		}
		if tx.Size.Rows > 0 && row >= tx.Size.Rows {
			return
		}
		touched[row] = struct{}{}
	}
	markRange := func(start int, end int) {
		if end < start {
			start, end = end, start
		}
		if tx.Size.Rows > 0 && end > tx.Size.Rows {
			end = tx.Size.Rows
		}
		for row := start; row < end; row++ {
			mark(row)
		}
	}
	for _, row := range tx.PrimaryFrameTouchedRows {
		mark(row)
	}
	for _, op := range tx.Ops {
		if !historyJournalOpTouchesRows(op) {
			continue
		}
		switch op.Code {
		case vterm.ScreenOpWriteSpan, vterm.ScreenOpClearToEOL:
			mark(op.Row)
		case vterm.ScreenOpClearRect:
			markRange(op.Rect.Y, op.Rect.Y+op.Rect.Height)
		case vterm.ScreenOpScrollRect:
			markRange(op.Rect.Y, op.Rect.Y+op.Rect.Height)
		case vterm.ScreenOpCopyRect:
			markRange(op.DstY, op.DstY+op.Src.Height)
		case vterm.ScreenOpControl:
			switch op.Control {
			case "ed":
				if op.Mode == 2 || op.Mode == 3 {
					markRange(0, tx.Size.Rows)
					continue
				}
				mark(op.Row)
			case "el", "ech", "dch", "ich":
				mark(op.Row)
			case "il", "dl", "su", "sd":
				markRange(op.Row, op.Bottom)
			case "ri":
				mark(op.Row)
			}
		}
	}
	rows := make([]int, 0, len(touched))
	for row := range touched {
		rows = append(rows, row)
	}
	sort.Ints(rows)
	return rows
}

func journalBoundaryFromOp(op TerminalSemanticOp, size TerminalSemanticSize) (HistoryJournalBoundary, bool) {
	switch op.Code {
	case vterm.ScreenOpControl:
		switch {
		case op.Control == "ed" && op.Mode == 2:
			return HistoryJournalBoundary{Kind: HistoryJournalBoundaryED2, Size: size, Reason: "ed2"}, true
		case op.Control == "ed" && op.Mode == 3:
			return HistoryJournalBoundary{Kind: HistoryJournalBoundaryED3, Size: size, Reason: "ed3"}, true
		case op.Control == "ris":
			return HistoryJournalBoundary{Kind: HistoryJournalBoundaryRIS, Size: size, Reason: "ris"}, true
		}
	case vterm.ScreenOpResize:
		resizeSize := TerminalSemanticSize{Cols: int(op.Size.Cols), Rows: int(op.Size.Rows)}
		if resizeSize.Cols == 0 || resizeSize.Rows == 0 {
			resizeSize = size
		}
		return HistoryJournalBoundary{Kind: HistoryJournalBoundaryResize, Size: resizeSize, Reason: "resize"}, true
	case vterm.ScreenOpModes:
		if op.Private && (op.Mode == 47 || op.Mode == 1047 || op.Mode == 1049) {
			if op.Enabled {
				return HistoryJournalBoundary{Kind: HistoryJournalBoundaryAltEnter, Size: size, Reason: "alt-enter"}, true
			}
			return HistoryJournalBoundary{Kind: HistoryJournalBoundaryAltExit, Size: size, Reason: "alt-exit"}, true
		}
		if op.Private && op.Mode == 2026 {
			if op.Enabled {
				return HistoryJournalBoundary{Kind: HistoryJournalBoundarySyncBegin, Size: size, Reason: "sync-begin"}, true
			}
			return HistoryJournalBoundary{Kind: HistoryJournalBoundarySyncEnd, Size: size, Reason: "sync-end"}, true
		}
	}
	return HistoryJournalBoundary{}, false
}

func journalHasSyncModeOp(tx TerminalSemanticTransaction, enabled bool) bool {
	for _, op := range tx.Ops {
		if op.Code == vterm.ScreenOpModes && op.Private && op.Mode == 2026 && op.Enabled == enabled {
			return true
		}
	}
	return false
}

type journalOrdinaryRecorder struct {
	cols   int
	rows   int
	active bool
	row    int
	cursor int
	cells  []Cell
	runs   []CellRun
	fill   *RowTailFill
	lines  []JournalLogicalLine
	cmds   []JournalOpenLineCommand
	edited bool
	// 中文说明：ordinary truth 是 logical line；soft-wrap 只是 visual row
	// continuation。这里记录每个 visual row 在当前 logical line 中的起始列，
	// 避免高压长行被拆成多条 history line 或反复 open-line update。
	visualRows []journalOrdinaryVisualRow
	// 中文说明：普通终端输出常见 CRLF。CR 只有在后面发生覆盖/光标编辑时才是
	// open-line command；紧跟 LF 时只是换行习惯，不能让线性 stdout 退回命令重放。
	pendingCR             bool
	afterLineFeed         bool
	flushAfterCommandSeal bool
}

type journalOrdinaryVisualRow struct {
	row  int
	base int
}

func newJournalOrdinaryRecorder(cols int, rows int) journalOrdinaryRecorder {
	return journalOrdinaryRecorder{cols: cols, rows: rows}
}

func (recorder *journalOrdinaryRecorder) ApplyOp(op TerminalSemanticOp) bool {
	switch op.Code {
	case vterm.ScreenOpWriteSpan:
		displayWidth := journalOpDisplayWidth(op)
		if recorder.pendingCR && recorder.hasDirectLinesBeforeCommand() {
			return false
		}
		if recorder.pendingCR {
			recorder.flushPendingCRCommand()
		}
		requiresCommand := recorder.writeRequiresCommand(op)
		if requiresCommand && recorder.hasDirectLinesBeforeCommand() {
			return false
		}
		var cells []Cell
		if requiresCommand {
			cells = journalCellsFromOp(op)
			recorder.beginEditedReplay()
			recorder.appendCommand(JournalOpenLineCommand{
				Kind:  JournalOpenLineCommandWrite,
				Row:   op.Row,
				Col:   op.Col,
				Cells: cloneHistoryCells(cells),
			})
		}
		logicalCol := recorder.logicalColumnForWrite(op)
		recorder.active = true
		recorder.afterLineFeed = false
		recorder.row = op.Row
		recorder.ensureVisualRowBase(op.Row, logicalCol-op.Col)
		if !recorder.edited && logicalCol == recorder.cursor {
			if len(op.Runs) > 0 && len(op.Cells) == 0 {
				if len(recorder.cells) > 0 {
					recorder.cells = appendJournalCells(recorder.cells, appendJournalRuns(nil, op.Runs, 0), journalInitialLineCapacity(recorder.cols))
				} else {
					recorder.runs = appendJournalSemanticRuns(recorder.runs, op.Runs)
				}
			} else {
				if cells == nil {
					cells = journalCellsFromOp(op)
				}
				recorder.materializeRunsForEditing()
				recorder.cells = appendJournalCells(recorder.cells, cells, journalInitialLineCapacity(recorder.cols))
			}
		} else {
			if cells == nil {
				cells = journalCellsFromOp(op)
			}
			recorder.cells = writeCellsAt(recorder.cells, logicalCol, cells)
			recorder.cells = trimTrailingBlankCellsInPlace(recorder.cells)
		}
		recorder.cursor = logicalCol + displayWidth
		return true
	case vterm.ScreenOpControl:
		return recorder.applyControl(op)
	case vterm.ScreenOpClearToEOL:
		if !recorder.active || op.Row != recorder.row {
			return false
		}
		recorder.materializeRunsForEditing()
		recorder.appendCommand(JournalOpenLineCommand{
			Kind: JournalOpenLineCommandEraseLine,
			Row:  op.Row,
			Col:  op.Col,
		})
		recorder.cells = eraseJournalCells(recorder.cells, op.Col, 0)
		if recorder.cursor > historyCellsDisplayWidth(recorder.cells) {
			recorder.cursor = historyCellsDisplayWidth(recorder.cells)
		}
		return true
	}
	return false
}

func (recorder *journalOrdinaryRecorder) applyControl(op TerminalSemanticOp) bool {
	switch op.Control {
	case "cr":
		if !recorder.edited && recorder.active && op.Row == recorder.row && recorder.cursor == recorder.contentWidth() {
			recorder.pendingCR = true
			recorder.row = op.Row
			recorder.cursor = 0
			return true
		}
		if recorder.hasDirectLinesBeforeCommand() {
			return false
		}
		recorder.beginEditedReplay()
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandSetCursor, Row: op.Row, Col: 0})
		recorder.row = op.Row
		recorder.cursor = 0
		return true
	case "lf", "ind", "nel":
		if recorder.pendingCR {
			recorder.pendingCR = false
		}
		if !recorder.active && len(recorder.cells) == 0 {
			if recorder.hasDirectLinesBeforeCommand() {
				return false
			}
			recorder.edited = true
			recorder.appendCommand(JournalOpenLineCommand{
				Kind:     JournalOpenLineCommandSealLine,
				TailFill: rowTailFillFromTerminal(op.TailFill),
			})
			recorder.flushAfterCommandSeal = true
			recorder.afterLineFeed = true
			recorder.row++
			if op.Control == "nel" {
				recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandSetCursor, Row: recorder.row, Col: 0})
				recorder.cursor = 0
			}
			return true
		}
		var command JournalOpenLineCommand
		if recorder.edited {
			command = JournalOpenLineCommand{Kind: JournalOpenLineCommandSealLine}
		}
		if op.TailFill != nil {
			recorder.fill = rowTailFillFromTerminal(op.TailFill)
			if recorder.edited {
				command.TailFill = cloneRowTailFill(recorder.fill)
			}
		}
		wasEdited := recorder.edited
		if recorder.edited {
			recorder.appendCommand(command)
		}
		recorder.sealCurrentLine()
		if wasEdited {
			recorder.flushAfterCommandSeal = true
		}
		recorder.afterLineFeed = true
		recorder.row++
		if op.Control == "nel" {
			recorder.edited = true
			recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandSetCursor, Row: recorder.row, Col: 0})
			recorder.cursor = 0
		}
		return true
	case "soft-wrap":
		if !recorder.active {
			return false
		}
		if recorder.pendingCR {
			if recorder.hasDirectLinesBeforeCommand() {
				return false
			}
			recorder.flushPendingCRCommand()
		}
		nextRow := op.Row + 1
		if recorder.rows > 0 && nextRow >= recorder.rows {
			nextRow = recorder.rows - 1
		}
		nextBase := maxInt(recorder.cursor, recorder.contentWidth())
		if op.TailFill != nil {
			recorder.fill = rowTailFillFromTerminal(op.TailFill)
		}
		if recorder.edited {
			recorder.appendCommand(JournalOpenLineCommand{
				Kind:     JournalOpenLineCommandSoftWrap,
				Row:      op.Row,
				Col:      op.Col,
				TailFill: cloneRowTailFill(recorder.fill),
			})
		}
		recorder.row = nextRow
		recorder.cursor = nextBase
		recorder.ensureVisualRowBase(nextRow, nextBase)
		return true
	case "bs":
		if recorder.hasDirectLinesBeforeCommand() {
			return false
		}
		recorder.flushPendingCRCommand()
		recorder.beginEditedReplay()
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandMoveCol, Delta: -1})
		recorder.cursor = maxInt(0, recorder.cursor-1)
		return true
	case "cub":
		if recorder.hasDirectLinesBeforeCommand() {
			return false
		}
		recorder.flushPendingCRCommand()
		recorder.beginEditedReplay()
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandMoveCol, Delta: -controlCount(op)})
		recorder.cursor = maxInt(0, recorder.cursor-controlCount(op))
		return true
	case "cuf":
		if recorder.hasDirectLinesBeforeCommand() {
			return false
		}
		recorder.flushPendingCRCommand()
		recorder.beginEditedReplay()
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandMoveCol, Delta: controlCount(op)})
		recorder.cursor += controlCount(op)
		return true
	case "cha", "hpa":
		if recorder.hasDirectLinesBeforeCommand() {
			return false
		}
		recorder.flushPendingCRCommand()
		recorder.beginEditedReplay()
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandSetCursor, Row: recorder.row, Col: maxInt(0, op.Col)})
		recorder.cursor = maxInt(0, op.Col)
		return true
	case "cup":
		if recorder.hasDirectLinesBeforeCommand() {
			return false
		}
		recorder.flushPendingCRCommand()
		recorder.beginEditedReplay()
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandSetCursor, Row: maxInt(0, op.Row), Col: maxInt(0, op.Col)})
		recorder.row = maxInt(0, op.Row)
		recorder.cursor = maxInt(0, op.Col)
		return true
	case "vpa":
		if recorder.hasDirectLinesBeforeCommand() {
			return false
		}
		recorder.flushPendingCRCommand()
		recorder.beginEditedReplay()
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandSetCursor, Row: maxInt(0, op.Row), Col: recorder.cursor})
		recorder.row = maxInt(0, op.Row)
		return true
	case "cuu":
		if recorder.hasDirectLinesBeforeCommand() {
			return false
		}
		recorder.flushPendingCRCommand()
		recorder.beginEditedReplay()
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandMoveRow, Delta: -controlCount(op)})
		recorder.row = maxInt(0, recorder.row-controlCount(op))
		return true
	case "cud":
		if recorder.hasDirectLinesBeforeCommand() {
			return false
		}
		recorder.flushPendingCRCommand()
		recorder.beginEditedReplay()
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandMoveRow, Delta: controlCount(op)})
		recorder.row += controlCount(op)
		return true
	case "el":
		if !recorder.active {
			return false
		}
		if recorder.hasDirectLinesBeforeCommand() {
			return false
		}
		recorder.flushPendingCRCommand()
		recorder.beginEditedReplay()
		recorder.appendCommand(JournalOpenLineCommand{
			Kind: JournalOpenLineCommandEraseLine,
			Row:  op.Row,
			Col:  op.Col,
			Mode: op.Mode,
		})
		recorder.cells = eraseJournalCells(recorder.cells, op.Col, op.Mode)
		return true
	}
	return false
}

func (recorder *journalOrdinaryRecorder) sealCurrentLine() {
	if !recorder.active && len(recorder.cells) == 0 && len(recorder.runs) == 0 {
		return
	}
	if !recorder.edited {
		recorder.lines = append(recorder.lines, JournalLogicalLine{
			Cells:    trimTrailingBlankCellsInPlace(recorder.cells),
			Runs:     trimTrailingBlankRunsInPlace(recorder.runs),
			TailFill: cloneRowTailFill(recorder.fill),
			Origin:   HistoryJournalOriginOrdinaryPrimary,
		})
	}
	recorder.active = false
	recorder.cells = nil
	recorder.runs = nil
	recorder.fill = nil
	recorder.cursor = 0
	recorder.visualRows = recorder.visualRows[:0]
	recorder.edited = false
	recorder.pendingCR = false
}

func (recorder *journalOrdinaryRecorder) Flush() (OrdinaryLineBatch, bool) {
	if recorder.pendingCR {
		if recorder.hasDirectLinesBeforeCommand() {
			// 中文说明：batch contract 不允许 direct sealed lines 和 command
			// replay 混在一起。这里保留 pending CR 后的 cursor=0 open update，
			// 让下一条 journal item 从 renderer-owned open line 继续命令回放。
			recorder.pendingCR = false
		} else {
			recorder.flushPendingCRCommand()
		}
	}
	if len(recorder.lines) == 0 && !recorder.active && len(recorder.cmds) == 0 {
		return OrdinaryLineBatch{}, false
	}
	batch := OrdinaryLineBatch{
		Cols:   recorder.cols,
		Origin: HistoryJournalOriginOrdinaryPrimary,
	}
	if len(recorder.lines) > 0 {
		batch.Lines = make([]JournalLogicalLine, len(recorder.lines))
		for i, line := range recorder.lines {
			batch.Lines[i] = line
		}
	}
	if recorder.active {
		batch.OpenUpdate = &JournalOpenLineUpdate{
			Cells:     recorder.openUpdateCells(),
			CursorCol: recorder.cursor,
			Row:       recorder.row,
			TailFill:  cloneRowTailFill(recorder.fill),
		}
	}
	if len(recorder.cmds) > 0 {
		batch.Commands = cloneJournalOpenLineCommands(recorder.cmds)
	}
	recorder.lines = nil
	recorder.active = false
	recorder.cells = nil
	recorder.runs = nil
	recorder.fill = nil
	recorder.cursor = 0
	recorder.cmds = nil
	recorder.visualRows = recorder.visualRows[:0]
	recorder.edited = false
	recorder.pendingCR = false
	recorder.afterLineFeed = false
	recorder.flushAfterCommandSeal = false
	return batch, true
}

func (recorder *journalOrdinaryRecorder) NeedsFlush() bool {
	return recorder.flushAfterCommandSeal
}

func (recorder *journalOrdinaryRecorder) hasDirectLinesBeforeCommand() bool {
	return len(recorder.lines) > 0 && len(recorder.cmds) == 0 && !recorder.edited
}

func (recorder *journalOrdinaryRecorder) beginEditedReplay() {
	if recorder.edited {
		return
	}
	recorder.materializeRunsForEditing()
	recorder.cells = expandHistoryCellsForEditing(recorder.cells)
	recorder.edited = true
	if recorder.active && len(recorder.cmds) == 0 && len(recorder.cells) > 0 {
		recorder.appendReplaySeedCommands()
	}
}

func (recorder *journalOrdinaryRecorder) materializeRunsForEditing() {
	if len(recorder.runs) == 0 {
		return
	}
	if len(recorder.cells) == 0 {
		recorder.cells = cellsFromHistoryRunsPreserveTrailing(recorder.runs)
	} else {
		recorder.cells = append(recorder.cells, cellsFromHistoryRunsPreserveTrailing(recorder.runs)...)
	}
	recorder.runs = nil
}

func (recorder *journalOrdinaryRecorder) flushPendingCRCommand() {
	if !recorder.pendingCR {
		return
	}
	recorder.pendingCR = false
	recorder.beginEditedReplay()
	recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandSetCursor, Row: recorder.row, Col: 0})
	recorder.cursor = 0
}

func (recorder *journalOrdinaryRecorder) writeRequiresCommand(op TerminalSemanticOp) bool {
	if recorder.edited {
		return true
	}
	if !recorder.active {
		// 中文说明：普通 LF/IND 只表示 logical line boundary；真实 vterm
		// 在 LF-only 输入或 journal 分片后会让下一次 WriteSpan 继续使用
		// 上一列。ordinary history 的 truth 是新 logical line，不应把
		// physical screen col 解释成 cursor-addressed repaint；真正的
		// primary repaint 已由 classifier/frame path 拥有。
		return false
	}
	if op.Row != recorder.row {
		return true
	}
	return recorder.logicalColumnForWrite(op) != recorder.cursor
}

func (recorder *journalOrdinaryRecorder) appendCommand(command JournalOpenLineCommand) {
	command.Cells = cloneHistoryCells(command.Cells)
	command.TailFill = cloneRowTailFill(command.TailFill)
	recorder.cmds = append(recorder.cmds, command)
}

func (recorder *journalOrdinaryRecorder) logicalColumnForWrite(op TerminalSemanticOp) int {
	if !recorder.active {
		return 0
	}
	base, ok := recorder.visualRowBaseForWrite(op.Row, op.Col)
	if !ok {
		return maxInt(0, op.Col)
	}
	return maxInt(0, base+op.Col)
}

func (recorder *journalOrdinaryRecorder) visualRowBaseForWrite(row int, col int) (int, bool) {
	for index := len(recorder.visualRows) - 1; index >= 0; index-- {
		current := recorder.visualRows[index]
		if current.row != row {
			continue
		}
		if current.base+col == recorder.cursor {
			return current.base, true
		}
	}
	for index := len(recorder.visualRows) - 1; index >= 0; index-- {
		if recorder.visualRows[index].row == row {
			return recorder.visualRows[index].base, true
		}
	}
	return 0, false
}

func (recorder *journalOrdinaryRecorder) ensureVisualRowBase(row int, base int) {
	for index := range recorder.visualRows {
		if recorder.visualRows[index].row == row && recorder.visualRows[index].base == base {
			return
		}
	}
	recorder.visualRows = append(recorder.visualRows, journalOrdinaryVisualRow{row: row, base: base})
}

func (recorder *journalOrdinaryRecorder) appendReplaySeedCommands() {
	rows := recorder.visualRows
	if len(rows) == 0 {
		rows = []journalOrdinaryVisualRow{{row: recorder.row, base: 0}}
	}
	for index, row := range rows {
		if index > 0 {
			previous := rows[index-1]
			recorder.appendCommand(JournalOpenLineCommand{
				Kind: JournalOpenLineCommandSoftWrap,
				Row:  previous.row,
				Col:  recorder.cols,
			})
		}
		end := historyCellsDisplayWidth(recorder.cells)
		if index+1 < len(rows) {
			end = rows[index+1].base
		}
		cells := sliceHistoryCellsByDisplayColumns(recorder.cells, row.base, end)
		if len(cells) == 0 {
			continue
		}
		recorder.appendCommand(JournalOpenLineCommand{
			Kind:  JournalOpenLineCommandWrite,
			Row:   row.row,
			Col:   0,
			Cells: cells,
		})
	}
}

func (recorder *journalOrdinaryRecorder) contentWidth() int {
	if len(recorder.runs) > 0 && len(recorder.cells) == 0 {
		return historyRunsDisplayWidth(recorder.runs)
	}
	return historyCellsDisplayWidth(recorder.cells)
}

func journalCellsFromOp(op TerminalSemanticOp) []Cell {
	cells, _ := journalCellsAndWidthFromOp(op)
	return cells
}

func journalCellsAndWidthFromOp(op TerminalSemanticOp) ([]Cell, int) {
	if len(op.Cells) > 0 {
		cells := historyCellsFromTerminal(op.Cells)
		return cells, terminalCellsDisplayWidth(op.Cells)
	}
	if len(op.Runs) > 0 {
		// 中文说明：WriteSpan run 是真实写入 payload，空格会影响后续
		// cursor/column 语义，不能复用 scroll-out proof 的 trailing blank trim。
		cells := appendJournalRuns(nil, op.Runs, journalInitialLineCapacity(0))
		return cells, historyCellsDisplayWidth(cells)
	}
	return nil, 0
}

func journalOpDisplayWidth(op TerminalSemanticOp) int {
	if len(op.Cells) > 0 {
		return terminalCellsDisplayWidth(op.Cells)
	}
	width := 0
	for _, run := range op.Runs {
		width += xansi.StringWidth(run.Text)
	}
	return width
}

func appendJournalCells(cells []Cell, incoming []Cell, initialCapacity int) []Cell {
	if len(cells) == 0 && cap(cells) == 0 && len(incoming) > 0 {
		// 中文说明：普通高压输出通常按 terminal cols 分成多段 soft-wrap。
		// 当前 line 是 journal-owned payload，可以预留几段 continuation 的容量，
		// 避免每个 visual row append 都触发整条 logical line 复制。
		capacity := maxInt(len(incoming), 0)
		if capacity < initialCapacity {
			capacity = initialCapacity
		}
		cells = make([]Cell, 0, capacity)
	}
	for _, cell := range incoming {
		cells = append(cells, cell)
	}
	return cells
}

func appendJournalRuns(cells []Cell, runs []TerminalSemanticCellRun, initialCapacity int) []Cell {
	if len(runs) == 0 {
		return cells
	}
	if len(cells) == 0 && cap(cells) == 0 {
		capacity := initialCapacity
		cells = make([]Cell, 0, capacity)
	}
	for _, run := range runs {
		style := historyStyleFromTerminal(run.Style)
		if isASCIIText(run.Text) {
			// 中文说明：vterm 可以用 compact run 减少 transaction 分配，但
			// history logical-line truth 的 Cell 仍是一格/一个 grapheme。
			// 把整段 ASCII 存成宽 Cell 会破坏 copy/history row projection。
			for i := 0; i < len(run.Text); i++ {
				cells = append(cells, Cell{Text: run.Text[i : i+1], Width: 1, Style: style})
			}
			continue
		}
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
			cells = append(cells, Cell{Text: cluster, Width: width, Style: style})
		}
	}
	return cells
}

func appendJournalSemanticRuns(out []CellRun, runs []TerminalSemanticCellRun) []CellRun {
	for _, run := range runs {
		if run.Text == "" {
			continue
		}
		next := CellRun{
			Text:  run.Text,
			Style: historyStyleFromTerminal(run.Style),
		}
		if len(out) > 0 {
			last := &out[len(out)-1]
			if last.Style == next.Style && last.LinkURL == next.LinkURL && last.LinkParams == next.LinkParams {
				last.Text += next.Text
				continue
			}
		}
		out = append(out, next)
	}
	return out
}

func historyRunsDisplayWidth(runs []CellRun) int {
	width := 0
	for _, run := range runs {
		width += xansi.StringWidth(run.Text)
	}
	return width
}

func trimTrailingBlankRunsInPlace(runs []CellRun) []CellRun {
	for len(runs) > 0 {
		last := runs[len(runs)-1]
		if last.Style != (CellStyle{}) || last.LinkURL != "" || last.LinkParams != "" || !strings.HasSuffix(last.Text, " ") {
			break
		}
		trimmed := strings.TrimRight(last.Text, " ")
		if trimmed != "" {
			runs[len(runs)-1].Text = trimmed
			break
		}
		runs = runs[:len(runs)-1]
	}
	if len(runs) == 0 {
		return nil
	}
	return runs
}

func journalLogicalLineSignature(line JournalLogicalLine) string {
	if len(line.Runs) > 0 && len(line.Cells) == 0 {
		var text strings.Builder
		for _, run := range trimTrailingBlankRunsInPlace(cloneCellRuns(line.Runs)) {
			text.WriteString(run.Text)
		}
		return text.String()
	}
	return rowText(trimTrailingBlankCellsInPlace(cloneHistoryCells(line.Cells)))
}

func scrollOutProofSignature(proof TerminalSemanticScrollOut) string {
	return rowText(cellsFromScrollOutProof(proof))
}

func journalInitialLineCapacity(cols int) int {
	if cols <= 0 {
		return 512
	}
	return maxInt(cols*4, 512)
}

func eraseJournalCells(cells []Cell, col int, mode int) []Cell {
	switch mode {
	case 1:
		return eraseJournalCellsBefore(cells, col)
	case 2:
		return nil
	default:
		return trimTrailingBlankCells(ensureCellWidth(cells[:cellIndexForDisplayColumn(cells, maxInt(0, col))], maxInt(0, col)))
	}
}

func eraseJournalCellsBefore(cells []Cell, col int) []Cell {
	if len(cells) == 0 {
		return nil
	}
	out := cloneHistoryCells(cells)
	target := maxInt(0, col)
	for historyCellsDisplayWidth(out) < target {
		out = append(out, blankHistoryCell())
	}
	index := cellIndexForDisplayColumn(out, target)
	for i := 0; i < index && i < len(out); i++ {
		out[i] = blankHistoryCell()
	}
	return trimTrailingBlankCells(out)
}

func sliceHistoryCellsByDisplayColumns(cells []Cell, start int, end int) []Cell {
	if len(cells) == 0 || end <= start {
		return nil
	}
	start = maxInt(0, start)
	out := make([]Cell, 0, len(cells))
	cursor := 0
	for _, cell := range cells {
		width := historyCellDisplayWidth(cell)
		next := cursor + width
		if width > 0 && cursor >= start && next <= end {
			out = append(out, cell)
		}
		cursor = next
		if cursor >= end {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneJournalLogicalLine(line JournalLogicalLine) JournalLogicalLine {
	line.Cells = cloneHistoryCells(line.Cells)
	line.Runs = cloneCellRuns(line.Runs)
	line.TailFill = cloneRowTailFill(line.TailFill)
	return line
}

func cloneJournalOpenLineCommands(commands []JournalOpenLineCommand) []JournalOpenLineCommand {
	if len(commands) == 0 {
		return nil
	}
	out := make([]JournalOpenLineCommand, len(commands))
	for i, command := range commands {
		out[i] = command
		out[i].Cells = cloneHistoryCells(command.Cells)
		out[i].TailFill = cloneRowTailFill(command.TailFill)
	}
	return out
}

func (recorder *journalOrdinaryRecorder) openUpdateCells() []Cell {
	if len(recorder.cells) > 0 {
		return cloneHistoryCells(recorder.cells)
	}
	return cellsFromHistoryRunsPreserveTrailing(recorder.runs)
}

// CloneHistoryJournal 返回 compact journal 的 history-owned 深拷贝。
// 调用边界是 SemanticTap fan-out 到 backlog；queue 可以保存该副本，但不能把
// 原始 TerminalSemanticTransaction、raw PTY 或 live snapshot 一起塞进 backlog。
func CloneHistoryJournal(journal HistoryJournal) HistoryJournal {
	journal.Items = cloneHistoryJournalItems(journal.Items)
	return journal
}

func cloneHistoryJournalItems(items []HistoryJournalItem) []HistoryJournalItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]HistoryJournalItem, len(items))
	for i, item := range items {
		out[i] = item
		if item.Ordinary != nil {
			ordinary := cloneOrdinaryLineBatch(*item.Ordinary)
			out[i].Ordinary = &ordinary
		}
		if item.Boundary != nil {
			boundary := *item.Boundary
			out[i].Boundary = &boundary
		}
		if item.ScrollOut != nil {
			scrollOut := HistoryJournalScrollOutProof{
				Rows:      cloneTerminalSemanticScrollOuts(item.ScrollOut.Rows),
				ClearTime: item.ScrollOut.ClearTime,
			}
			out[i].ScrollOut = &scrollOut
		}
		if item.Frame != nil {
			frame := cloneHistoryJournalFrameEvent(*item.Frame)
			out[i].Frame = &frame
		}
	}
	return out
}

func cloneOrdinaryLineBatch(batch OrdinaryLineBatch) OrdinaryLineBatch {
	if len(batch.Lines) > 0 {
		lines := make([]JournalLogicalLine, len(batch.Lines))
		for i, line := range batch.Lines {
			lines[i] = cloneJournalLogicalLine(line)
		}
		batch.Lines = lines
	}
	if batch.OpenUpdate != nil {
		update := *batch.OpenUpdate
		update.Cells = cloneHistoryCells(update.Cells)
		update.TailFill = cloneRowTailFill(update.TailFill)
		batch.OpenUpdate = &update
	}
	batch.Commands = cloneJournalOpenLineCommands(batch.Commands)
	return batch
}

func cloneHistoryJournalFrameEvent(frame HistoryJournalFrameEvent) HistoryJournalFrameEvent {
	frame.Frame = cloneTerminalSemanticFrame(frame.Frame)
	frame.TouchedRows = cloneIntSlice(frame.TouchedRows)
	return frame
}

func cloneIntSlice(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	out := make([]int, len(values))
	copy(out, values)
	return out
}
