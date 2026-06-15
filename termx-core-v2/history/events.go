package history

import "errors"

// EventKind 是 HistoryTrack 接收的显式历史语义事件。
type EventKind string

const (
	EventWritePrimaryCells        EventKind = "write-primary-cells"
	EventCarriageReturn           EventKind = "carriage-return"
	EventCursorForward            EventKind = "cursor-forward"
	EventCursorBackward           EventKind = "cursor-backward"
	EventCursorHorizontalAbsolute EventKind = "cursor-horizontal-absolute"
	EventEraseInLine              EventKind = "erase-in-line"
	EventEraseInDisplay           EventKind = "erase-in-display"
	EventSealLogicalLine          EventKind = "seal-logical-line"
	EventMutateFrontier           EventKind = "mutate-frontier"
	EventResetFrontier            EventKind = "reset-frontier"
	EventCommitFrontier           EventKind = "commit-frontier"
	EventForceCommitFrontier      EventKind = "force-commit-frontier"
	EventReclaimCommittedSuffix   EventKind = "reclaim-committed-suffix"
	EventHideFrontier             EventKind = "hide-frontier"
	EventTruncateCommittedHistory EventKind = "truncate-committed-history"
	EventSwitchAltScreen          EventKind = "switch-alt-screen"
	EventNonHistoryBoundary       EventKind = "non-history-boundary"
	EventResize                   EventKind = "resize"
)

// ResizeDirection 只表达 resize 对历史侧的语义：失效窗口、按整条 logical
// line reclaim，或把 frontier 尾部隐藏起来。
type ResizeDirection string

const (
	ResizeSame   ResizeDirection = "same"
	ResizeGrow   ResizeDirection = "grow"
	ResizeShrink ResizeDirection = "shrink"
)

// HistoryEvent 保持在 domain 层，不能携带 visual rows、snapshot 或 grid
// viewport 数据。
type HistoryEvent struct {
	Kind            EventKind
	Cells           []Cell
	Style           CellStyle
	LineID          LogicalLineID
	LineIDs         []LogicalLineID
	Count           int
	EraseMode       int
	EnterAltScreen  bool
	ResizeDirection ResizeDirection
}

var (
	ErrInvalidEventKind       = errors.New("invalid history event kind")
	ErrInvalidResizeDirection = errors.New("invalid resize direction")
	ErrLineNotCommitted       = errors.New("logical line is not in committed history")
	ErrLineNotMutable         = errors.New("logical line is not in mutable frontier")
)
