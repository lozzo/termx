package history

import vterm "github.com/lozzow/termx/termx-vterm/vterm"

// HistoryJournalSource 描述 compact history journal 的真值入口。
// 当前只允许来自 single SemanticTap/vterm 同一次 semantic pass 后的
// TerminalSemanticTransaction；失败条件是把 raw PTY、live snapshot diff、
// TUI rows 或第二个 vterm consumer 伪造成 journal 来源。
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
	JournalOpenLineCommandSealLine  JournalOpenLineCommandKind = "seal-line"
)

// JournalOpenLineCommand 是 ordinary journal fast path 的最小可应用命令。
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
	Rows []TerminalSemanticScrollOut
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

// HistoryJournalFromTransaction 把 single SemanticTap 之后的一条 terminal semantic
// transaction 裁剪成 history-specific compact journal。消息链路是
// PTY/resize -> SemanticTap/vterm -> TerminalSemanticTransaction -> HistoryJournal；
// 本函数不读取 tx.Raw、SourceDamage、live snapshot 或任何 renderer/TUI rows。
func HistoryJournalFromTransaction(terminalID string, tx TerminalSemanticTransaction) HistoryJournal {
	builder := historyJournalBuilder{
		journal: HistoryJournal{
			TerminalID: terminalID,
			Seq:        tx.Seq,
			Size:       tx.Size,
			Source:     HistoryJournalSourceSemanticTapTransaction,
		},
		ordinary:                 newJournalOrdinaryRecorder(tx.Size.Cols),
		primaryFrameTouchedRows:  cloneIntSlice(tx.PrimaryFrameTouchedRows),
		skipSideProofFrameEvents: isResizeFullReplace(tx),
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
	journal                  HistoryJournal
	ordinary                 journalOrdinaryRecorder
	primaryFrameTouchedRows  []int
	inAlt                    bool
	skipSideProofFrameEvents bool
}

func (builder *historyJournalBuilder) applyEvent(event HistorySemanticEvent) {
	switch event.Kind {
	case HistorySemanticEventOp:
		builder.applyOpEvent(event)
	case HistorySemanticEventPrimaryScrollOut:
		if event.ScrollOut == nil {
			return
		}
		builder.flushOrdinary(event.OrderSource, event.Order)
		builder.appendScrollOut(event, []TerminalSemanticScrollOut{*event.ScrollOut})
	case HistorySemanticEventPrimaryFrame:
		if event.Frame == nil || builder.skipSideProofFrameEvents {
			return
		}
		builder.flushOrdinary(event.OrderSource, event.Order)
		builder.appendFrame(event, HistoryJournalFrameEvent{
			Kind:        HistoryJournalFrameReplacePrimary,
			Frame:       cloneTerminalSemanticFrame(event.Frame),
			TouchedRows: nil,
			Reason:      string(FrameReasonPrimaryRepaint),
		})
	case HistorySemanticEventAltFrame:
		if event.Frame == nil || builder.skipSideProofFrameEvents {
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
		builder.appendFrame(event, HistoryJournalFrameEvent{Kind: HistoryJournalFrameArchivePrimary, Reason: string(SealReasonAltEnter)})
		builder.inAlt = true
	case HistorySemanticEventAltExit:
		builder.flushOrdinary(event.OrderSource, event.Order)
		builder.appendBoundary(event, HistoryJournalBoundary{Kind: HistoryJournalBoundaryAltExit, Size: event.Size, Reason: "alt-exit"})
		builder.appendFrame(event, HistoryJournalFrameEvent{Kind: HistoryJournalFrameClearAlt, Reason: string(FrameReasonAltExit)})
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

func (builder *historyJournalBuilder) applyOpEvent(event HistorySemanticEvent) {
	if event.Op == nil {
		return
	}
	if boundary, ok := journalBoundaryFromOp(*event.Op, event.Size); ok {
		builder.flushOrdinary(event.OrderSource, event.Order)
		builder.appendBoundary(event, boundary)
		switch boundary.Kind {
		case HistoryJournalBoundaryAltEnter:
			builder.inAlt = true
			builder.appendFrame(event, HistoryJournalFrameEvent{Kind: HistoryJournalFrameArchivePrimary, Reason: string(SealReasonAltEnter)})
		case HistoryJournalBoundaryAltExit:
			builder.inAlt = false
			builder.appendFrame(event, HistoryJournalFrameEvent{Kind: HistoryJournalFrameClearAlt, Reason: string(FrameReasonAltExit)})
		}
		if boundary.Kind == HistoryJournalBoundaryED2 {
			builder.appendFrame(event, HistoryJournalFrameEvent{Kind: HistoryJournalFrameClearPrimary, Reason: string(FrameReasonPrimaryRepaint)})
		}
		return
	}
	if builder.inAlt {
		// 中文说明：alt-screen payload 只能由同一 transaction 的 AltFrame proof
		// 表达 transient frame；ordered write ops 不能退回 ordinary primary batch，
		// 否则 alt 内容会进入 primary history truth。
		return
	}
	if builder.ordinary.ApplyOp(*event.Op) {
		return
	}
	builder.flushOrdinary(event.OrderSource, event.Order)
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
	item := HistoryJournalItem{
		Kind:          HistoryJournalItemOrdinaryLineBatch,
		Order:         len(builder.journal.Items),
		SemanticOrder: semanticOrder,
		OrderSource:   source,
		Ordinary:      &batch,
	}
	builder.journal.Items = append(builder.journal.Items, item)
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
			Rows: cloneTerminalSemanticScrollOuts(rows),
		},
	}
	builder.journal.Items = append(builder.journal.Items, item)
}

func (builder *historyJournalBuilder) appendFrame(event HistorySemanticEvent, frame HistoryJournalFrameEvent) {
	if frame.Kind == HistoryJournalFrameReplacePrimary && len(frame.TouchedRows) == 0 {
		frame.TouchedRows = cloneIntSlice(builder.primaryFrameTouchedRows)
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
	active bool
	row    int
	cursor int
	cells  []Cell
	fill   *RowTailFill
	lines  []JournalLogicalLine
	cmds   []JournalOpenLineCommand
}

func newJournalOrdinaryRecorder(cols int) journalOrdinaryRecorder {
	return journalOrdinaryRecorder{cols: cols}
}

func (recorder *journalOrdinaryRecorder) ApplyOp(op TerminalSemanticOp) bool {
	switch op.Code {
	case vterm.ScreenOpWriteSpan:
		cells := journalCellsFromOp(op)
		recorder.appendCommand(JournalOpenLineCommand{
			Kind:  JournalOpenLineCommandWrite,
			Row:   op.Row,
			Col:   op.Col,
			Cells: cloneHistoryCells(cells),
		})
		recorder.active = true
		recorder.row = op.Row
		recorder.cells = writeCellsAt(recorder.cells, op.Col, cells)
		recorder.cells = trimTrailingBlankCells(recorder.cells)
		recorder.cursor = op.Col + journalOpDisplayWidth(op)
		return true
	case vterm.ScreenOpControl:
		return recorder.applyControl(op)
	case vterm.ScreenOpClearToEOL:
		if !recorder.active || op.Row != recorder.row {
			return false
		}
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
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandSetCursor, Row: op.Row, Col: 0})
		recorder.row = op.Row
		recorder.cursor = 0
		return true
	case "lf", "ind", "nel":
		command := JournalOpenLineCommand{Kind: JournalOpenLineCommandSealLine}
		if op.TailFill != nil {
			recorder.fill = rowTailFillFromTerminal(op.TailFill)
			command.TailFill = cloneRowTailFill(recorder.fill)
		}
		recorder.appendCommand(command)
		recorder.sealCurrentLine()
		recorder.row++
		if op.Control == "nel" {
			recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandSetCursor, Row: recorder.row, Col: 0})
			recorder.cursor = 0
		}
		return true
	case "bs":
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandMoveCol, Delta: -1})
		recorder.cursor = maxInt(0, recorder.cursor-1)
		return true
	case "cub":
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandMoveCol, Delta: -controlCount(op)})
		recorder.cursor = maxInt(0, recorder.cursor-controlCount(op))
		return true
	case "cuf":
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandMoveCol, Delta: controlCount(op)})
		recorder.cursor += controlCount(op)
		return true
	case "cha", "hpa":
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandSetCursor, Row: recorder.row, Col: maxInt(0, op.Col)})
		recorder.cursor = maxInt(0, op.Col)
		return true
	case "cup":
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandSetCursor, Row: maxInt(0, op.Row), Col: maxInt(0, op.Col)})
		recorder.row = maxInt(0, op.Row)
		recorder.cursor = maxInt(0, op.Col)
		return true
	case "vpa":
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandSetCursor, Row: maxInt(0, op.Row), Col: recorder.cursor})
		recorder.row = maxInt(0, op.Row)
		return true
	case "cuu":
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandMoveRow, Delta: -controlCount(op)})
		recorder.row = maxInt(0, recorder.row-controlCount(op))
		return true
	case "cud":
		recorder.appendCommand(JournalOpenLineCommand{Kind: JournalOpenLineCommandMoveRow, Delta: controlCount(op)})
		recorder.row += controlCount(op)
		return true
	case "el":
		if !recorder.active {
			return false
		}
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
	if !recorder.active && len(recorder.cells) == 0 {
		return
	}
	recorder.lines = append(recorder.lines, JournalLogicalLine{
		Cells:    cloneHistoryCells(recorder.cells),
		TailFill: cloneRowTailFill(recorder.fill),
		Origin:   HistoryJournalOriginOrdinaryPrimary,
	})
	recorder.active = false
	recorder.cells = nil
	recorder.fill = nil
	recorder.cursor = 0
}

func (recorder *journalOrdinaryRecorder) Flush() (OrdinaryLineBatch, bool) {
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
			batch.Lines[i] = cloneJournalLogicalLine(line)
		}
	}
	if recorder.active {
		batch.OpenUpdate = &JournalOpenLineUpdate{
			Cells:     cloneHistoryCells(recorder.cells),
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
	recorder.fill = nil
	recorder.cursor = 0
	recorder.cmds = nil
	return batch, true
}

func (recorder *journalOrdinaryRecorder) appendCommand(command JournalOpenLineCommand) {
	command.Cells = cloneHistoryCells(command.Cells)
	command.TailFill = cloneRowTailFill(command.TailFill)
	recorder.cmds = append(recorder.cmds, command)
}

func journalCellsFromOp(op TerminalSemanticOp) []Cell {
	if len(op.Cells) > 0 {
		return historyCellsFromTerminal(op.Cells)
	}
	if len(op.Runs) > 0 {
		return cellsFromScrollOutProof(TerminalSemanticScrollOut{Runs: op.Runs})
	}
	return nil
}

func journalOpDisplayWidth(op TerminalSemanticOp) int {
	if len(op.Cells) > 0 {
		return terminalCellsDisplayWidth(op.Cells)
	}
	return historyCellsDisplayWidth(cellsFromScrollOutProof(TerminalSemanticScrollOut{Runs: op.Runs}))
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

func cloneJournalLogicalLine(line JournalLogicalLine) JournalLogicalLine {
	line.Cells = cloneHistoryCells(line.Cells)
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

func cloneIntSlice(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	out := make([]int, len(values))
	copy(out, values)
	return out
}
