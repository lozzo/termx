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
	EventCursorUp                 EventKind = "cursor-up"
	EventCursorDown               EventKind = "cursor-down"
	EventCursorPosition           EventKind = "cursor-position"
	EventEraseCharacters          EventKind = "erase-characters"
	EventEraseInLine              EventKind = "erase-in-line"
	EventEraseInDisplay           EventKind = "erase-in-display"
	EventSetActiveLineTailFill    EventKind = "set-active-line-tail-fill"
	EventEnterPrimaryFullscreen   EventKind = "enter-primary-fullscreen"
	EventExitPrimaryFullscreen    EventKind = "exit-primary-fullscreen"
	EventPrimaryScrollOut         EventKind = "primary-scroll-out"
	EventAppendAltScreenFrame     EventKind = "append-alt-screen-frame"
	EventSealLogicalLine          EventKind = "seal-logical-line"
	EventSoftWrapLine             EventKind = "soft-wrap-line"
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
	ownedCells      bool
	Rows            [][]Cell
	Style           CellStyle
	LineID          LogicalLineID
	LineIDs         []LogicalLineID
	Count           int
	Row             int
	Column          int
	EraseMode       int
	EraseCols       int
	EnterAltScreen  bool
	PrimaryMode     int
	ResizeDirection ResizeDirection
}

var (
	ErrInvalidEventKind       = errors.New("invalid history event kind")
	ErrInvalidResizeDirection = errors.New("invalid resize direction")
	ErrLineNotCommitted       = errors.New("logical line is not in committed history")
	ErrLineNotMutable         = errors.New("logical line is not in mutable frontier")
)
