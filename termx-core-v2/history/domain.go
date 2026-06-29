package history

import "time"

// ScreenSessionID 标识 history 拥有的 primary screen app session。它是 domain
// id，不是 terminal id、process id、pane id 或 TUI view id。
type ScreenSessionID uint64

// ScreenFrameID 标识 screen app session 或 transient alt-screen frame journal
// 内的一帧 fixed-grid frame；它必须在 cursor token 内保持稳定。
type ScreenFrameID uint64

// HistoryToken 标识 core-v2 创建的 history window 或 frozen copy boundary。
// client 可以原样传回，但不能从 token 反推出 history truth。
type HistoryToken string

// LineKind 是 authoritative history row 的语义种类，不是 TUI 渲染样式。
type LineKind string

const (
	LineKindOrdinary            LineKind = "ordinary"
	LineKindScreenFrame         LineKind = "screen-frame"
	LineKindArchivedScreenFrame LineKind = "archived-screen-frame"
	LineKindAltScreenFrame      LineKind = "alt-screen-frame"
)

// HistoryOutputMode 是 classifier 对 semantic transaction 的输出模式；它只选择
// history renderer 路径，不能查看进程名、命令行或 TUI pane 状态。
type HistoryOutputMode string

const (
	// HistoryOutputModeOrdinaryStream 表示普通 stdout/stderr 流式输出，由
	// StreamLineReducer 维护 open line 并按 terminal 语义 seal。
	HistoryOutputModeOrdinaryStream HistoryOutputMode = "ordinary-stream"
	// HistoryOutputModePrimaryFrameSession 表示 primary screen app 正在 repaint
	// current frame，由 FrameReducer 全量替换 current primary frame。
	HistoryOutputModePrimaryFrameSession HistoryOutputMode = "primary-frame-session"
	// HistoryOutputModeAltTransient 表示 alt-screen transient，只进入 latest/frozen
	// projection，不写 primary sealed timeline。
	HistoryOutputModeAltTransient HistoryOutputMode = "alt-transient"
	// HistoryOutputModeBoundaryOnly 表示 resize/full-replace 等不产生内容历史的
	// terminal boundary，只能推进 generation 或 cursor validity。
	HistoryOutputModeBoundaryOnly HistoryOutputMode = "boundary-only"
)

// HistoryRecordID 标识 sealed timeline 中的 record。它不是 logical line id：
// 一个 frame record 可以展开成多条 logical lines。
type HistoryRecordID uint64

// HistoryRecordKind 描述 sealed timeline record 的来源。它是分页和 copy/search
// 的领域语义，不是 renderer 样式。
type HistoryRecordKind string

const (
	// HistoryRecordOrdinaryLine 表示普通 stream 输出按 terminal 语义 seal 后进入
	// timeline 的 logical line。
	HistoryRecordOrdinaryLine HistoryRecordKind = "ordinary-line"
	// HistoryRecordPrimaryScrollOutLine 表示 primary screen app 中真实离开 viewport
	// 的 scroll-out proof，被 seal 成 logical line。
	HistoryRecordPrimaryScrollOutLine HistoryRecordKind = "primary-scroll-out-line"
	// HistoryRecordArchivedPrimaryFrame 表示 primary current frame 因 alt enter 等
	// 边界被 archive 后进入 timeline 的 fixed-grid frame。
	HistoryRecordArchivedPrimaryFrame HistoryRecordKind = "archived-primary-frame"
	// HistoryRecordClosedPrimaryFrame 表示 terminal/session close 时保留的 final
	// primary fixed-grid frame。
	HistoryRecordClosedPrimaryFrame HistoryRecordKind = "closed-primary-frame"
)

// SealReason 描述 mutable history 对象为什么离开 current ownership。它是
// history lifecycle 语义，不等于 storage persistence。
type SealReason string

const (
	SealReasonLineFeed      SealReason = "line-feed"
	SealReasonScrollOut     SealReason = "scroll-out"
	SealReasonAltEnter      SealReason = "alt-enter"
	SealReasonTerminalClose SealReason = "terminal-close"
	SealReasonSessionClose  SealReason = "session-close"
	SealReasonRetention     SealReason = "retention"
	SealReasonFullReplace   SealReason = "full-replace"
	SealReasonUnknown       SealReason = "unknown"
)

// FrameReason 描述 frame journal 的一次替换或清理原因。它帮助 harness 检查
// alt enter/exit、resize 和 terminal close 这类消息链路是否走对。
type FrameReason string

const (
	FrameReasonPrimaryRepaint FrameReason = "primary-repaint"
	FrameReasonAltRepaint     FrameReason = "alt-repaint"
	FrameReasonAltEnter       FrameReason = "alt-enter"
	FrameReasonAltExit        FrameReason = "alt-exit"
	FrameReasonResize         FrameReason = "resize"
	FrameReasonSessionClose   FrameReason = "session-close"
	FrameReasonTerminalClose  FrameReason = "terminal-close"
)

// FrameSource 描述 fixed-grid frame 的语义来源。它只来自 vterm semantic frame，
// 不能来自 write op fallback 或 live renderer snapshot。
type FrameSource string

const (
	FrameSourcePrimarySemantic FrameSource = "primary-semantic-frame"
	FrameSourceAltSemantic     FrameSource = "alt-semantic-frame"
)

// ScreenFrame 是 fixed-grid frame payload；它可以投影成 rows，但不是 ordinary
// text，也不是 sealed timeline 的唯一 truth。是否 sealed 由 timeline/frame journal
// ownership 表达。
type ScreenFrame struct {
	ID         ScreenFrameID
	SessionID  ScreenSessionID
	Kind       LineKind
	Rows       [][]Cell
	ScreenCols int
	ScreenRows int
	SourceSeq  uint64
	CreatedAt  time.Time
}

// LogicalLineDraft 是 reducer 内部正在修改的 logical line 视图。它携带 cursor
// 和 wrapped row 关系，后续会被 seal 成 LogicalLine 或继续被 terminal op 修改。
type LogicalLineDraft struct {
	Line      LogicalLine
	CursorCol int
	Wrapped   bool
	Row       int
}

// OpenLine 是普通 stream 输出的 current draft。它只服务 ordinary-stream；
// screen app frame 不能偷偷复用 open line 当成第二份 frame truth。
type OpenLine struct {
	Active bool
	Draft  LogicalLineDraft
}

// MutableFrame 是 frame journal 中仍会被后续 repaint 全量替换的 primary frame。
// 它属于 history state 的 mutable frontier，不进入 sealed timeline。
type MutableFrame struct {
	ID        ScreenFrameID
	SessionID ScreenSessionID
	Seq       uint64
	Cols      int
	Rows      []LogicalLineDraft
	Source    FrameSource
	CreatedAt time.Time
}

// SealedFrame 是已经离开 current ownership 的 fixed-grid frame。它可以作为一个
// record 进入 sealed timeline，后续 resize 不得改写它的生成宽度。
type SealedFrame struct {
	ID        ScreenFrameID
	SessionID ScreenSessionID
	Seq       uint64
	Cols      int
	Lines     []LogicalLine
	Reason    SealReason
	CreatedAt time.Time
}

// TransientFrame 是 alt-screen 的 current frame。它可以投影到 latest/frozen
// window 供选择复制，但默认不进入 primary sealed timeline。
type TransientFrame struct {
	ID        ScreenFrameID
	Seq       uint64
	Cols      int
	Rows      []LogicalLineDraft
	Source    FrameSource
	CreatedAt time.Time
}

// HistoryRecord 是 sealed timeline 的统一元素。它保存顺序和引用，payload 仍在
// logical lines 或 frame journal 中，避免 timeline 成为第二份内容 truth。
type HistoryRecord struct {
	ID        HistoryRecordID
	Seq       uint64
	Kind      HistoryRecordKind
	LineIDs   []LogicalLineID
	FrameID   ScreenFrameID
	Reason    SealReason
	CreatedAt time.Time
}

// SealedTimeline 是所有 sealed records 的顺序索引。它不保存 payload，只保存
// record 引用、顺序和 generation/cursor 所需元数据。
type SealedTimeline struct {
	Records    []HistoryRecord
	Generation Generation
}

// FrameJournal 保存 primary current、primary archived 和 alt transient 三类
// frame ownership。它是 history state 的一部分，不属于 TUI 或 live renderer。
type FrameJournal struct {
	PrimaryCurrent  *MutableFrame
	PrimaryArchived []SealedFrame
	AltCurrent      *TransientFrame
}

// HistoryState 是单个 terminal 的 history truth。它只保存 logical-line-first
// 状态，不保存 process handle、pane kind、renderer cache 或 protocol client。
type HistoryState struct {
	TerminalID string
	Generation Generation
	OpenLine   OpenLine
	Timeline   SealedTimeline
	Frames     FrameJournal
	Frozen     map[HistoryToken]FrozenHistorySnapshot
}

// FrameRecord 是 storage/backend 视角的 frame journal index；payload truth 仍在
// logical line store 或 frame payload 中，backend 不能据此判断 current/sealed。
type FrameRecord struct {
	SessionID   ScreenSessionID
	FrameID     ScreenFrameID
	Sequence    uint64
	LineIDs     []LogicalLineID
	ContentHash string
	ScreenSize  TerminalSemanticSize
	PublishedAt time.Time
	SealReason  SealReason
}

// ScreenSessionParams 用来打开 history 拥有的 screen session；调用方必须提供
// semantic transaction metadata，不能提供 live snapshot rows。
type ScreenSessionParams struct {
	SessionID ScreenSessionID
	StartSeq  uint64
	Size      TerminalSemanticSize
	StartedAt time.Time
}

// ClosePolicy 描述 session close 时 history 如何处理 active current frame。它是
// policy 输入，不是 process lifecycle state；名称使用 seal/drop，不使用 commit。
type ClosePolicy string

const (
	ClosePolicyDropCurrent         ClosePolicy = "drop-current"
	ClosePolicySealPrimaryFinal    ClosePolicy = "seal-primary-final"
	ClosePolicyDiscardAltTransient ClosePolicy = "discard-alt-transient"
	ClosePolicyArchivePrimary      ClosePolicy = "archive-primary-current"
)

// CloseReason 记录 terminal lifecycle boundary。process exit、kill 和 daemon
// cleanup 都必须通过这个 reason 进入 HistoryLogicalRenderer.Close，而不是伪造成
// PTY bytes。
type CloseReason string

const (
	CloseReasonProcessExit     CloseReason = "process-exit"
	CloseReasonTerminalKill    CloseReason = "terminal-kill"
	CloseReasonTerminalRemove  CloseReason = "terminal-remove"
	CloseReasonDaemonShutdown  CloseReason = "daemon-shutdown"
	CloseReasonSessionBoundary CloseReason = "session-boundary"
)
