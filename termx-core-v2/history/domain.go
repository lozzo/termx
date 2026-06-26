package history

import "time"

// ScreenSessionID identifies a primary screen app session owned by history.
// It is a domain id, not a terminal id, process id, pane id, or TUI view id.
type ScreenSessionID uint64

// ScreenFrameID identifies one fixed-grid frame within a screen app session or
// transient alt-screen frame journal. It must stay stable inside cursor tokens.
type ScreenFrameID uint64

// HistoryToken identifies a history window or frozen copy boundary created by
// core-v2. Clients can echo it back, but must not derive history truth from it.
type HistoryToken string

// LineKind 是 authoritative history row 的语义种类，不是 TUI 渲染样式。
type LineKind string

const (
	LineKindOrdinary            LineKind = "ordinary"
	LineKindScreenFrame         LineKind = "screen-frame"
	LineKindArchivedScreenFrame LineKind = "archived-screen-frame"
	LineKindAltScreenFrame      LineKind = "alt-screen-frame"
)

// ScreenOutputMode is the classifier output mode for a semantic transaction.
// It chooses the history projection path without looking at process names.
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

// ArchiveReason records why a primary current frame crossed from current-only
// state into bounded frame journal state. Ordinary repaint must not archive.
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

// ScreenSessionParams opens a history-owned screen session. The caller must
// provide semantic transaction metadata, never live snapshot rows.
type ScreenSessionParams struct {
	SessionID ScreenSessionID
	StartSeq  uint64
	Size      TerminalSemanticSize
	StartedAt time.Time
}

// ClosePolicy describes how history should resolve active current frames when
// a session closes. It is policy input, not a process lifecycle state.
type ClosePolicy string

const (
	ClosePolicyDropCurrent           ClosePolicy = "drop-current"
	ClosePolicyCommitPrimaryFinal    ClosePolicy = "commit-primary-final"
	ClosePolicyDiscardAltTransient   ClosePolicy = "discard-alt-transient"
	ClosePolicyArchivePrimaryCurrent ClosePolicy = "archive-primary-current"
)

// CloseReason records the lifecycle boundary that forced projector shutdown.
// Process exit, kill and daemon cleanup must enter history through this reason.
type CloseReason string

const (
	CloseReasonProcessExit     CloseReason = "process-exit"
	CloseReasonTerminalKill    CloseReason = "terminal-kill"
	CloseReasonTerminalRemove  CloseReason = "terminal-remove"
	CloseReasonDaemonShutdown  CloseReason = "daemon-shutdown"
	CloseReasonSessionBoundary CloseReason = "session-boundary"
)
