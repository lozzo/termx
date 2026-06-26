package history

import "time"

// HistorySegment names the segment currently addressed by a history cursor.
// It prevents archived/current frames from being paged through ordinary line ids.
type HistorySegment string

const (
	HistorySegmentCommitted            HistorySegment = "committed"
	HistorySegmentCurrentPrimaryFrame  HistorySegment = "current-primary-frame"
	HistorySegmentArchivedPrimaryFrame HistorySegment = "archived-primary-frame"
	HistorySegmentCurrentAltFrame      HistorySegment = "current-alt-frame"
)

// HistoryWindowMode describes the pagination direction requested from core-v2.
// The store decides replace/prepend/append; the client must not infer it.
type HistoryWindowMode string

const (
	HistoryWindowModeLatest HistoryWindowMode = "latest"
	HistoryWindowModeOlder  HistoryWindowMode = "older"
	HistoryWindowModeNewer  HistoryWindowMode = "newer"
	HistoryWindowModeOldest HistoryWindowMode = "oldest"
)

// HistoryWindowOp tells consumers how to apply a returned authoritative window.
// It is derived by core-v2 from token/cursor state, not by TUI local row counts.
type HistoryWindowOp string

const (
	HistoryWindowReplace HistoryWindowOp = "replace"
	HistoryWindowPrepend HistoryWindowOp = "prepend"
	HistoryWindowAppend  HistoryWindowOp = "append"
)

// HistoryCursor 是 segment-aware cursor；client 只能原样传回，不能猜 older 边界。
type HistoryCursor struct {
	Segment    HistorySegment
	SessionID  ScreenSessionID
	FrameID    ScreenFrameID
	LineID     LogicalLineID
	RowInLine  int
	Generation Generation
	Token      HistoryToken
	Valid      bool
}

// HistoryBoundary records the visible logical boundary and next segment cursor
// for a returned window or frozen snapshot.
type HistoryBoundary struct {
	FirstLineID LogicalLineID
	LastLineID  LogicalLineID
	Cursor      HistoryCursor
}

// HistoryWindowRequest is the domain request for latest/older/newer windows.
// Cursor and boundary are segment-aware and must be echoed, not reconstructed.
type HistoryWindowRequest struct {
	TerminalID string
	Mode       HistoryWindowMode
	Cols       int
	Limit      int
	Token      HistoryToken
	Cursor     HistoryCursor
	Boundary   HistoryBoundary
}

// FreezeHistoryRequest starts a copy/history snapshot. The returned token pins
// the visible logical boundary so later repaint cannot change selected text.
type FreezeHistoryRequest struct {
	TerminalID string
	Cols       int
	Limit      int
}

// HistoryCopyRequest asks core-v2 to copy text from an authoritative frozen
// history token. It must not be satisfied from TUI rows or live surface cache.
type HistoryCopyRequest struct {
	TerminalID string
	Token      HistoryToken
	Cols       int
	Start      HistoryCursor
	End        HistoryCursor
}

// HistoryLineSpan maps projected rows back to logical line and segment truth.
// It is metadata for selection/copy, not an alternate history payload store.
type HistoryLineSpan struct {
	StartRow       int
	EndRow         int
	Kind           LineKind
	Segment        HistorySegment
	LogicalLineID  LogicalLineID
	SessionID      ScreenSessionID
	FrameID        ScreenFrameID
	TimestampStart time.Time
	TimestampEnd   time.Time
	ClippedBefore  bool
	ClippedAfter   bool
}

// HistoryRow is one projected row returned by history.window. Its cells come
// from logical-line/frame payload truth; renderer output must not write it back.
type HistoryRow struct {
	Cells      []Cell
	Kind       LineKind
	Segment    HistorySegment
	LineID     LogicalLineID
	SessionID  ScreenSessionID
	FrameID    ScreenFrameID
	RowInLine  int
	FixedGrid  bool
	ScreenCols int
	Committed  bool
	Wrapped    bool
}

// HistoryWindow 是 core-v2 authoritative projection，不携带 TUI pane/workspace truth。
type HistoryWindow struct {
	TerminalID   string
	Token        HistoryToken
	Op           HistoryWindowOp
	Cols         int
	Rows         []HistoryRow
	Lines        []HistoryLineSpan
	Generation   Generation
	Boundary     HistoryBoundary
	HasMore      bool
	LogicalTotal int
	Timestamp    time.Time
}

// FrozenHistorySnapshot records the logical boundaries visible when copy mode
// starts. It is a tokenized boundary, not a full duplicate of all history.
type FrozenHistorySnapshot struct {
	Token                 HistoryToken
	TerminalID            string
	Cols                  int
	CommittedUpperBound   LogicalLineID
	FrozenFrontierLineIDs []LogicalLineID
	Boundary              HistoryBoundary
	Generation            Generation
	CreatedAt             time.Time
}
