package history

import "time"

// HistorySegment 命名 history cursor 当前指向的 segment，防止 archived/current
// frames 被当作 ordinary line ids 分页。
type HistorySegment string

const (
	// HistorySegmentCommitted 是 protocol/TUI 兼容 segment 名。新 history 领域
	// 语义应把它理解为 sealed timeline segment，而不是 commit 概念。
	HistorySegmentCommitted            HistorySegment = "committed"
	HistorySegmentCurrentPrimaryFrame  HistorySegment = "current-primary-frame"
	HistorySegmentArchivedPrimaryFrame HistorySegment = "archived-primary-frame"
	HistorySegmentCurrentAltFrame      HistorySegment = "current-alt-frame"
)

// HistoryWindowMode 描述向 core-v2 请求的分页方向。replace/prepend/append 由 store
// 决定，client 不能自行推断。
type HistoryWindowMode string

const (
	HistoryWindowModeLatest HistoryWindowMode = "latest"
	HistoryWindowModeOlder  HistoryWindowMode = "older"
	HistoryWindowModeNewer  HistoryWindowMode = "newer"
	HistoryWindowModeOldest HistoryWindowMode = "oldest"
)

// HistoryWindowOp 告诉 consumer 如何应用返回的 authoritative window。它由 core-v2
// 根据 token/cursor state 推导，不能由 TUI local row count 推导。
type HistoryWindowOp string

const (
	HistoryWindowReplace HistoryWindowOp = "replace"
	HistoryWindowPrepend HistoryWindowOp = "prepend"
	HistoryWindowAppend  HistoryWindowOp = "append"
)

// HistoryCursor 是 segment-aware cursor；client 只能原样传回，不能猜 older 边界。
// BeforeRowIndex 是 older 请求使用的 projection 绝对行号；RowInLine 只描述
// cursor 所在 logical line 内部的局部 row，二者不能复用。
type HistoryCursor struct {
	Segment        HistorySegment
	SessionID      ScreenSessionID
	FrameID        ScreenFrameID
	LineID         LogicalLineID
	RowInLine      int
	BeforeRowIndex int
	Generation     Generation
	Token          HistoryToken
	Valid          bool
}

// HistoryBoundary 记录返回 window 或 frozen snapshot 的可见 logical boundary 和
// next segment cursor。
type HistoryBoundary struct {
	FirstLineID LogicalLineID
	LastLineID  LogicalLineID
	Cursor      HistoryCursor
}

// HistoryWindowRequest 是 latest/older/newer window 的 domain request。Cursor 和
// boundary 都是 segment-aware，只能原样传回，不能重建。
type HistoryWindowRequest struct {
	TerminalID string
	Mode       HistoryWindowMode
	Cols       int
	Limit      int
	Token      HistoryToken
	Cursor     HistoryCursor
	Boundary   HistoryBoundary
}

// FreezeHistoryRequest 启动 copy/history snapshot。返回的 token 固定可见 logical
// boundary，使后续 repaint 不能改变已选择文本。
type FreezeHistoryRequest struct {
	TerminalID string
	Cols       int
	Limit      int
}

// HistoryCopyRequest 要求 core-v2 从 authoritative frozen history token 复制文本。
// 它不能从 TUI rows 或 live surface cache 满足。
type HistoryCopyRequest struct {
	TerminalID string
	Token      HistoryToken
	Cols       int
	Start      HistoryCursor
	End        HistoryCursor
}

// HistoryLineSpan 把 projected rows 映射回 logical line 和 segment truth。它是
// selection/copy metadata，不是另一份 history payload store。
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
	ScreenRow      int
	ScreenRowSet   bool
}

// HistoryRow 是 history.window 返回的一行 projected row。它的 cells 来自
// logical-line/frame payload truth，renderer output 不能写回它。
type HistoryRow struct {
	Cells     []Cell
	Kind      LineKind
	Segment   HistorySegment
	LineID    LogicalLineID
	SessionID ScreenSessionID
	FrameID   ScreenFrameID
	RowInLine int
	// ProjectionRowIndex 是该 row 在 core authoritative projection 中的绝对
	// row index；TUI 裁剪本地窗口后必须用它恢复 older cursor，不能用 RowInLine。
	ProjectionRowIndex int
	FixedGrid          bool
	ScreenCols         int
	// ScreenRow 表达 fixed-grid screen-frame row 在原 terminal frame 中的物理
	// 行号。它服务 copy/history display projection 的屏幕锚点；ordinary logical
	// line 不应使用该字段作为历史顺序。
	ScreenRow    int
	ScreenRowSet bool
	// Committed 是旧 protocol/wire 兼容字段；新领域语义只应把它当作
	// projected row 是否来自 sealed timeline 的标记，不能引入 commit owner。
	Committed bool
	Wrapped   bool
}

// HistoryWindow 是 core-v2 权威 projection，不携带 TUI pane/workspace truth。
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

// FrozenHistorySnapshot 记录 copy mode 启动时可见的 logical boundaries。它是
// tokenized boundary，不是整份历史的完整副本。
type FrozenHistorySnapshot struct {
	Token      HistoryToken
	TerminalID string
	Cols       int
	// CommittedUpperBound 是旧 token schema 的兼容字段；新 store 实现应按
	// sealed timeline boundary 填充，不应恢复 commit 领域概念。
	CommittedUpperBound LogicalLineID
	// FrozenFrontierLineIDs 是旧 token schema 的兼容字段；新 store 实现应按
	// mutable open/current projection 填充，不应恢复 frontier owner。
	FrozenFrontierLineIDs []LogicalLineID
	FrozenPrimaryFrames   []ScreenFrame
	FrozenAltFrame        *ScreenFrame
	Boundary              HistoryBoundary
	Generation            Generation
	CreatedAt             time.Time
}
