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

// ScreenOutputMode 是 classifier 对 semantic transaction 的输出模式；它只选择
// history 投影路径，不能查看进程名。
type ScreenOutputMode string

const (
	ScreenOutputModeOrdinary             ScreenOutputMode = "ordinary"
	ScreenOutputModePrimaryScreenSession ScreenOutputMode = "primary-screen-session"
	ScreenOutputModeAltTransient         ScreenOutputMode = "alt-transient"
	ScreenOutputModeNonHistoryBoundary   ScreenOutputMode = "non-history-boundary"
)

// ScreenFrame 是 fixed-grid frame payload；它可以投影成 rows，但不是 ordinary text。
type ScreenFrame struct {
	ID         ScreenFrameID
	SessionID  ScreenSessionID
	Kind       LineKind
	Rows       [][]Cell
	ScreenCols int
	ScreenRows int
	Committed  bool
	SourceSeq  uint64
	CreatedAt  time.Time
}

// ArchiveReason 记录 primary current frame 为什么从 current-only 状态进入
// bounded frame journal；普通 repaint 不能触发 archive。
type ArchiveReason string

const (
	ArchiveReasonAltEnter        ArchiveReason = "alt-enter"
	ArchiveReasonSessionBoundary ArchiveReason = "session-boundary"
	ArchiveReasonRetentionPolicy ArchiveReason = "retention-policy"
)

// FrameRecord 是 frame journal index；payload truth 仍在 LogicalLineStore。
type FrameRecord struct {
	SessionID     ScreenSessionID
	FrameID       ScreenFrameID
	Sequence      uint64
	LineIDs       []LogicalLineID
	ContentHash   string
	ScreenSize    TerminalSemanticSize
	PublishedAt   time.Time
	ArchiveReason ArchiveReason
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
// policy 输入，不是 process lifecycle state。
type ClosePolicy string

const (
	ClosePolicyDropCurrent           ClosePolicy = "drop-current"
	ClosePolicyCommitPrimaryFinal    ClosePolicy = "commit-primary-final"
	ClosePolicyDiscardAltTransient   ClosePolicy = "discard-alt-transient"
	ClosePolicyArchivePrimaryCurrent ClosePolicy = "archive-primary-current"
)

// CloseReason 记录强制 projector close 的 lifecycle boundary。process exit、kill
// 和 daemon cleanup 都必须通过这个 reason 进入 history。
type CloseReason string

const (
	CloseReasonProcessExit     CloseReason = "process-exit"
	CloseReasonTerminalKill    CloseReason = "terminal-kill"
	CloseReasonTerminalRemove  CloseReason = "terminal-remove"
	CloseReasonDaemonShutdown  CloseReason = "daemon-shutdown"
	CloseReasonSessionBoundary CloseReason = "session-boundary"
)
