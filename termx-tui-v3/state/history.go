package state

import (
	"errors"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// RequestID 是 TUI 本地请求 id，只用于关联 async response。
type RequestID uint64

// HistoryRequestKind 区分 latest replace、oldest replace 和 older prepend。
type HistoryRequestKind string

const (
	HistoryRequestLatest HistoryRequestKind = "latest"
	HistoryRequestOldest HistoryRequestKind = "oldest"
	HistoryRequestOlder  HistoryRequestKind = "older"
	HistoryRequestNewer  HistoryRequestKind = "newer"
)

// HistoryWindowOp 是 core-v2 authoritative window 合并方式。
type HistoryWindowOp string

const (
	HistoryWindowReplace HistoryWindowOp = "replace"
	HistoryWindowPrepend HistoryWindowOp = "prepend"
	HistoryWindowAppend  HistoryWindowOp = "append"
)

const (
	HistoryRowKindScreenFrame         = "screen-frame"
	HistoryRowKindArchivedScreenFrame = "archived-screen-frame"
	HistoryRowKindAltScreenFrame      = "alt-screen-frame"
)

const (
	HistoryCursorSegmentCommitted            = "committed"
	HistoryCursorSegmentCurrentPrimaryFrame  = "current-primary-frame"
	HistoryCursorSegmentArchivedPrimaryFrame = "archived-primary-frame"
	HistoryCursorSegmentCurrentAltFrame      = "current-alt-frame"
)

// HistoryCursor 是 older pagination 的 logical boundary。
// BeforeRowIndex 来自 core authoritative projection，是下一次 older 请求的
// 绝对行号；BeforeRowInLine 只描述 logical line 内部位置，不能参与分页。
type HistoryCursor struct {
	Valid           bool
	BeforeLineID    uint64
	BeforeRowInLine int
	BeforeRowIndex  int
	Segment         string
}

// HistoryBoundary 绑定 window 的 logical line 边界。
type HistoryBoundary struct {
	FirstLineID uint64
	LastLineID  uint64
}

// HistoryPendingRequest 保存 reducer 接纳 response 所需的本地 pending 状态。
type HistoryPendingRequest struct {
	ID              RequestID
	Kind            HistoryRequestKind
	PaneID          string
	ViewID          string
	TerminalID      string
	Cols            int
	Token           string
	Generation      uint64
	Cursor          HistoryCursor
	Boundary        HistoryBoundary
	BoundCopyModeID uint64
	// older 返回时先保持原内容锚点，再消费用户在顶部继续向上滚动但尚未满足的行数。
	ScrollDeltaAfterPrepend int
}

// HistoryRow 是 authoritative HistoryWindow 的 visual row 投影。
type HistoryRow struct {
	Text       string
	Cells      []HistoryCell
	TailFill   *HistoryCellStyle
	LineID     uint64
	RowInLine  int
	Kind       string
	Segment    string
	SessionID  uint64
	FrameID    uint64
	FixedGrid  bool
	ScreenCols int
	// ProjectionRowIndex 来自 core authoritative projection；copy/history 本地
	// trim 后用它恢复 older cursor，不能从 RowInLine 或本地 visual row 推断。
	ProjectionRowIndex int
	ClippedStart       bool
	ClippedEnd         bool
	LiveTail           bool
}

// HistoryCell 是 core-v2 authoritative HistoryWindow 的 styled cell 投影。
// Text 仍保留在 HistoryRow 上作为搜索/复制派生视图，渲染优先消费 Cells。
type HistoryCell struct {
	Text       string
	Width      int
	Style      HistoryCellStyle
	LinkURL    string
	LinkParams string
}

type HistoryCellStyle struct {
	FG            string
	BG            string
	Bold          bool
	Italic        bool
	Underline     bool
	Blink         bool
	Reverse       bool
	Strikethrough bool
}

// HistoryLogicalLine 是 copy/history 冻结快照里的单条 logical line payload。
// TUI 只把它当作 authoritative source，再按当前 pane 宽度本地重排成 Rows。
type HistoryLogicalLine struct {
	Text               string
	Cells              []HistoryCell
	LineID             uint64
	Kind               string
	Segment            string
	SessionID          uint64
	FrameID            uint64
	FixedGrid          bool
	ScreenCols         int
	ProjectionRowIndex int
	TailFill           *HistoryCellStyle
	LiveTail           bool
	// clipped 标记表达这条 frozen source 是否只是 authoritative logical line
	// 的局部片段；本地 reflow 时必须继续保留，不能把 partial 片段误当成完整行。
	ClippedBefore bool
	ClippedAfter  bool
}

func sameHistoryLogicalLineSource(left HistoryLogicalLine, right HistoryLogicalLine) bool {
	return left.LineID != 0 &&
		left.LineID == right.LineID &&
		left.Kind == right.Kind &&
		left.Segment == right.Segment &&
		left.SessionID == right.SessionID &&
		left.FrameID == right.FrameID &&
		left.FixedGrid == right.FixedGrid &&
		(!left.FixedGrid || left.ScreenCols == right.ScreenCols)
}

// HistoryLineSpan 是 authoritative window 中 logical line 到 visual rows 的映射。
type HistoryLineSpan struct {
	LineID             uint64
	StartRow           int
	EndRow             int
	Kind               string
	Segment            string
	SessionID          uint64
	FrameID            uint64
	FixedGrid          bool
	ScreenCols         int
	ProjectionRowIndex int
	ClippedBefore      bool
	ClippedAfter       bool
}

// HistoryWindow 是 state 层使用的 core-v2 authoritative window DTO。
type HistoryWindow struct {
	ViewID       string
	PaneID       string
	TerminalID   string
	Token        string
	Op           HistoryWindowOp
	Cols         int
	SourceLines  []HistoryLogicalLine
	Rows         []HistoryRow
	Lines        []HistoryLineSpan
	Cursor       HistoryCursor
	HasMore      bool
	Generation   uint64
	Boundary     HistoryBoundary
	LoadedLines  int
	TotalLines   int
	ResponseKind HistoryRequestKind
}

// HistoryStore 只保存 authoritative window、请求状态和 exhausted marker。
type HistoryStore struct {
	ViewID      string
	PaneID      string
	TerminalID  string
	Token       string
	Cols        int
	SourceLines []HistoryLogicalLine
	Rows        []HistoryRow
	Lines       []HistoryLineSpan
	Cursor      HistoryCursor
	Generation  uint64
	Boundary    HistoryBoundary
	HasMore     bool
	Exhausted   ExhaustedMarker
	Pending     *HistoryPendingRequest
}

// ExhaustedMarker 表示某次 older response 证明对应 cursor 已 exhausted。
type ExhaustedMarker struct {
	Valid     bool
	RequestID RequestID
	Token     string
	Cols      int
	Cursor    HistoryCursor
	Boundary  HistoryBoundary
}

// OlderRequestState 汇总 authoritative window 与 pending/exhausted guard 下的 older 请求状态。
type OlderRequestState string

const (
	OlderRequestReady     OlderRequestState = "ready"
	OlderRequestPending   OlderRequestState = "pending"
	OlderRequestExhausted OlderRequestState = "exhausted"
	OlderRequestMissing   OlderRequestState = "missing"
)

// NewerRequestState 汇总本地窗口尾部是否已经被回收，以及能否从 frozen backend 拉回。
type NewerRequestState string

const (
	NewerRequestReady   NewerRequestState = "ready"
	NewerRequestPending NewerRequestState = "pending"
	NewerRequestMissing NewerRequestState = "missing"
)

// CopyModePhase 是 TUI copy mode 的 reducer-owned 阶段。
// preview 只冻结进入瞬间的 live native screen 供显示和吞输入；materialized
// 只消费 core 已应用 history frontier；frozen 才拥有 core frozen token，可用于
// older/newer/search/backend copy 合同。
type CopyModePhase string

const (
	CopyModePhaseNone              CopyModePhase = ""
	CopyModeEnteringLivePreview    CopyModePhase = "entering-live-preview"
	CopyModeMaterializedProjection CopyModePhase = "materialized-projection"
	CopyModeFrozenHistory          CopyModePhase = "frozen-history"
)

// CopyModeCapabilityBits 描述当前 copy projection 允许的交互。
// 这些能力来自 core copy-entry/materialized 合同或 frozen token 合同；TUI 不能
// 因为本地有 rows 就自行升级成 search/page/backend copy truth。
type CopyModeCapabilityBits struct {
	Selectable bool
	Copyable   bool
	Searchable bool
	Pageable   bool
}

// CopyModeMaterializedProjectionState 保存 copy-entry projection 的元数据。
// Window rows 仍在 HistoryStore 中；这里仅记录能力位和 history backlog 追平状态，
// 防止 materialized projection 被误当成 frozen token。
type CopyModeMaterializedProjectionState struct {
	Valid             bool
	NativeCols        int
	AppliedHistorySeq uint64
	TargetHistorySeq  uint64
	CatchupPending    bool
	Capabilities      CopyModeCapabilityBits
}

// CopyModeStore 只保存 copy mode 交互态。
type CopyModeStore struct {
	Active   bool
	Entering bool
	// Phase 是 R377 copy 三态的显式阶段；旧测试或恢复态未填时由 PhaseKind 按兼容字段推导。
	Phase        CopyModePhase
	PaneID       string
	ViewID       string
	TerminalID   string
	EnteringLive *LiveSurfaceSnapshot
	// latest 飞行期间只累计净滚动行数；不能保存输入事件队列或历史文本副本。
	EnteringScrollDelta int
	ViewportTop         int
	// ViewportTopPadding 是进入 copy/history 时从 live viewport 继承的顶部空白显示锚点。
	// 它不是 authoritative history row，也不参与搜索、复制、分页 cursor 或 older 请求；用户滚动后会清零。
	ViewportTopPadding int
	ViewRows           int
	Cursor             CopyPosition
	Mark               *CopyPosition
	Selection          *CopySelection
	Query              string
	Matches            []CopyMatch
	ActiveMatch        int
	BoundToken         string
	BoundCols          int
	RequestID          RequestID
	// ProjectionRequestID 只绑定 copy-entry materialized projection，不进入 HistoryStore.Pending。
	ProjectionRequestID RequestID
	Materialized        CopyModeMaterializedProjectionState
	Empty               bool
}

type CopyPosition struct {
	Row int
	// Col 是 authoritative visual row 内的 display cell column，不是 rune index。
	Col int
}

type CopyLogicalPosition struct {
	Valid  bool
	LineID uint64
	// Col 是 logical line 内的 display cell column，用于窗口被裁剪后回 core/backend 拉文本。
	Col int
}

type CopyMatch struct {
	// Start/End 使用 authoritative row + display cell column，允许同一 logical line
	// 的匹配跨越本地 reflow 产生的多个 visual row。
	StartRow int
	StartCol int
	EndRow   int
	EndCol   int
}

type CopySelection struct {
	Anchor        CopyPosition
	Focus         CopyPosition
	LogicalAnchor CopyLogicalPosition
	LogicalFocus  CopyLogicalPosition
}

// HistoryTrimResult 描述一次 TUI 本地 history window 裁剪。
type HistoryTrimResult struct {
	DroppedRowsBefore  int
	DroppedRowsAfter   int
	DroppedLinesBefore int
	DroppedLinesAfter  int
}

var (
	ErrHistoryRequestPending = errors.New("history request pending")
	ErrStaleHistoryResponse  = errors.New("stale history response")
	ErrHistoryWindowMismatch = errors.New("history window mismatch")
)

func (store HistoryStore) BeginLatest(req HistoryPendingRequest) (HistoryStore, error) {
	if store.Pending != nil {
		return store, ErrHistoryRequestPending
	}
	req.Kind = HistoryRequestLatest
	store.Pending = &req
	store.Exhausted = ExhaustedMarker{}
	return store, nil
}

func (store HistoryStore) BeginOlder(req HistoryPendingRequest) (HistoryStore, error) {
	if store.Pending != nil {
		return store, ErrHistoryRequestPending
	}
	req.Kind = HistoryRequestOlder
	store.Pending = &req
	return store, nil
}

func (store HistoryStore) BeginNewer(req HistoryPendingRequest) (HistoryStore, error) {
	if store.Pending != nil {
		return store, ErrHistoryRequestPending
	}
	req.Kind = HistoryRequestNewer
	store.Pending = &req
	return store, nil
}

func (store HistoryStore) BeginOldest(req HistoryPendingRequest) (HistoryStore, error) {
	if store.Pending != nil {
		return store, ErrHistoryRequestPending
	}
	req.Kind = HistoryRequestOldest
	store.Pending = &req
	return store, nil
}

func (store HistoryStore) ApplyWindow(requestID RequestID, window HistoryWindow) (HistoryStore, int, error) {
	if store.Pending == nil || store.Pending.ID != requestID {
		return store, 0, ErrStaleHistoryResponse
	}
	pending := *store.Pending
	if err := validateWindowAgainstPending(pending, window); err != nil {
		return store, 0, err
	}
	store.Pending = nil
	switch window.Op {
	case HistoryWindowReplace:
		store = store.replace(window, pending.Cols)
		return store, len(window.Rows), nil
	case HistoryWindowPrepend:
		beforeRows := len(store.Rows)
		if len(window.Rows) == 0 && !window.HasMore {
			store.Exhausted = ExhaustedMarker{
				Valid:     true,
				RequestID: requestID,
				Token:     pending.Token,
				Cols:      pending.Cols,
				Cursor:    pending.Cursor,
				Boundary:  pending.Boundary,
			}
			return store, 0, nil
		}
		store = store.prepend(window)
		inserted := len(store.Rows) - beforeRows
		if inserted < 0 {
			inserted = 0
		}
		return store, inserted, nil
	case HistoryWindowAppend:
		beforeRows := len(store.Rows)
		store = store.append(window)
		inserted := len(store.Rows) - beforeRows
		if inserted < 0 {
			inserted = 0
		}
		return store, inserted, nil
	default:
		return store, 0, ErrHistoryWindowMismatch
	}
}

// ApplyMaterializedProjection 接纳 copy-entry projection 的 replace window。
// copy-entry 不经过 HistoryStore.Pending，也不生成 frozen token；调用方必须先在
// CopyModeStore 上校验 projection request id，避免把 stale projection 当成 latest。
func (store HistoryStore) ApplyMaterializedProjection(window HistoryWindow, cols int) (HistoryStore, int, error) {
	if window.Op != HistoryWindowReplace {
		return store, 0, ErrHistoryWindowMismatch
	}
	store.Exhausted = ExhaustedMarker{}
	store = store.replace(window, cols)
	// 中文说明：materialized projection 没有 frozen token；分页能力由 CopyModeStore
	// 的 materialized capability bits 表达，HistoryStore 不能自行暴露 older/newer ready。
	store.Token = ""
	return store, len(store.Rows), nil
}

func (store HistoryStore) InvalidateWindow() HistoryStore {
	store.Token = ""
	store.ViewID = ""
	store.PaneID = ""
	store.Cols = 0
	store.SourceLines = nil
	store.Rows = nil
	store.Lines = nil
	store.Cursor = HistoryCursor{}
	store.Generation = 0
	store.Boundary = HistoryBoundary{}
	store.HasMore = false
	store.Exhausted = ExhaustedMarker{}
	store.Pending = nil
	return store
}

func (store HistoryStore) OlderRequestState() OlderRequestState {
	if store.Pending != nil {
		return OlderRequestPending
	}
	if store.Exhausted.Valid &&
		store.Exhausted.Token == store.Token &&
		store.Exhausted.Cursor == store.Cursor &&
		store.Exhausted.Boundary == store.Boundary {
		return OlderRequestExhausted
	}
	if store.Token == "" || !store.Cursor.Valid {
		return OlderRequestMissing
	}
	return OlderRequestReady
}

func (store HistoryStore) NewerRequestState() NewerRequestState {
	if store.Pending != nil {
		return NewerRequestPending
	}
	if store.Token == "" || len(store.Rows) == 0 || store.Boundary.LastLineID == 0 {
		return NewerRequestMissing
	}
	tail := store.Rows[len(store.Rows)-1]
	if tail.LineID == 0 || tail.LineID == store.Boundary.LastLineID {
		return NewerRequestMissing
	}
	return NewerRequestReady
}

func validateWindowAgainstPending(pending HistoryPendingRequest, window HistoryWindow) error {
	if pending.TerminalID != "" && pending.TerminalID != window.TerminalID {
		return ErrHistoryWindowMismatch
	}
	if pending.ViewID != "" && pending.ViewID != window.ViewID {
		return ErrStaleHistoryResponse
	}
	if pending.PaneID != "" && window.PaneID != "" && pending.PaneID != window.PaneID {
		return ErrStaleHistoryResponse
	}
	switch pending.Kind {
	case HistoryRequestLatest:
		if window.Op != HistoryWindowReplace {
			return ErrHistoryWindowMismatch
		}
		// frozen snapshot latest 接纳的是 authoritative logical-line source，
		// response 的 window.Cols 只是 core 当前投影使用的 source cols；TUI 会
		// 基于 SourceLines 按本地 pane 宽度重新 reflow，因此 latest 不要求
		// response cols 与本地请求 cols 完全一致。
	case HistoryRequestOldest:
		if window.Op != HistoryWindowReplace {
			return ErrHistoryWindowMismatch
		}
		if pending.Token == "" || pending.Token != window.Token {
			return ErrStaleHistoryResponse
		}
		if pending.Generation != 0 && pending.Generation != window.Generation {
			return ErrStaleHistoryResponse
		}
	case HistoryRequestOlder:
		if window.Op != HistoryWindowPrepend {
			return ErrHistoryWindowMismatch
		}
		if pending.Cols != 0 && pending.Cols != window.Cols {
			return ErrHistoryWindowMismatch
		}
		if pending.Token != "" && pending.Token != window.Token {
			return ErrStaleHistoryResponse
		}
		if pending.Generation != 0 && pending.Generation != window.Generation {
			return ErrStaleHistoryResponse
		}
		// 中文说明：older prepend 返回的是 existing head 之前的页面，
		// 它的 LastLineID 应该是 older page 自己的尾部，不应等于当前窗口尾部。
		// stale guard 只依赖 token/generation/request id 和 core cursor。
	case HistoryRequestNewer:
		if window.Op != HistoryWindowAppend {
			return ErrHistoryWindowMismatch
		}
		if pending.Cols != 0 && pending.Cols != window.Cols {
			return ErrHistoryWindowMismatch
		}
		if pending.Token != "" && pending.Token != window.Token {
			return ErrStaleHistoryResponse
		}
		if pending.Generation != 0 && pending.Generation != window.Generation {
			return ErrStaleHistoryResponse
		}
		if len(window.SourceLines) != 0 && pending.Boundary.FirstLineID != 0 && pending.Boundary.FirstLineID != window.Boundary.FirstLineID {
			return ErrStaleHistoryResponse
		}
		if len(window.SourceLines) != 0 && pending.Boundary.LastLineID != 0 && pending.Boundary.LastLineID != window.Boundary.LastLineID {
			return ErrStaleHistoryResponse
		}
	default:
		return ErrHistoryWindowMismatch
	}
	return nil
}

func (store HistoryStore) replace(window HistoryWindow, cols int) HistoryStore {
	store.ViewID = window.ViewID
	store.PaneID = window.PaneID
	store.TerminalID = window.TerminalID
	store.Token = window.Token
	if cols <= 0 {
		cols = window.Cols
	}
	store.Cols = cols
	store.SourceLines = historyWindowSourceLinesOwned(window)
	if len(window.SourceLines) > 0 && cols == window.Cols && len(window.Rows) > 0 {
		// 中文说明：带 SourceLines 的 HistoryWindow 经 message 进入 reducer 后由 reducer 接管；
		// cols 未变化时直接复用已投影 rows/spans，避免 copy/history 入口重复 reflow 和深拷贝 cells。
		store.Rows = window.Rows
		store.Lines = window.Lines
	} else {
		store.Rows, store.Lines = ReflowHistoryLogicalLines(store.SourceLines, cols)
	}
	store.Cursor = window.Cursor
	store.Generation = window.Generation
	store.Boundary = window.Boundary
	store.HasMore = window.HasMore
	store.Exhausted = ExhaustedMarker{}
	return store
}

func (store HistoryStore) prepend(window HistoryWindow) HistoryStore {
	existing := store.SourceLines
	if len(existing) == 0 && len(store.Rows) > 0 {
		existing = historyRowsToLogicalLines(store.Rows, store.Lines)
	}
	older := historyWindowSourceLinesOwned(window)
	if fast, rows, spans := fastPrependedHistoryRows(older, existing, store.Rows, store.Lines, store.Cols, window); fast {
		store.SourceLines = prependHistoryLogicalLines(older, existing)
		store.Rows = rows
		store.Lines = spans
	} else {
		store.SourceLines = mergePrependedHistoryLogicalLines(older, existing)
		store.Rows, store.Lines = ReflowHistoryLogicalLines(store.SourceLines, store.Cols)
	}
	store.Token = window.Token
	store.Cursor = window.Cursor
	store.Generation = window.Generation
	store.Boundary.FirstLineID = window.Boundary.FirstLineID
	store.HasMore = window.HasMore
	store.Exhausted = ExhaustedMarker{}
	return store
}

func (store HistoryStore) append(window HistoryWindow) HistoryStore {
	existing := store.SourceLines
	if len(existing) == 0 && len(store.Rows) > 0 {
		existing = historyRowsToLogicalLines(store.Rows, store.Lines)
	}
	newer := historyWindowSourceLinesOwned(window)
	if fast, rows, spans := fastAppendedHistoryRows(existing, newer, store.Rows, store.Lines, store.Cols, window); fast {
		store.SourceLines = appendHistoryLogicalLines(existing, newer)
		store.Rows = rows
		store.Lines = spans
	} else {
		store.SourceLines = mergeAppendedHistoryLogicalLines(existing, newer)
		store.Rows, store.Lines = ReflowHistoryLogicalLines(store.SourceLines, store.Cols)
	}
	store.Token = window.Token
	store.Generation = window.Generation
	store.Boundary.LastLineID = window.Boundary.LastLineID
	store.HasMore = window.HasMore
	store.Exhausted = ExhaustedMarker{}
	return store
}

func fastPrependedHistoryRows(older []HistoryLogicalLine, existing []HistoryLogicalLine, existingRows []HistoryRow, existingSpans []HistoryLineSpan, cols int, window HistoryWindow) (bool, []HistoryRow, []HistoryLineSpan) {
	if historyPrependNeedsBoundaryMerge(older, existing) {
		return false, nil, nil
	}
	olderRows, olderSpans := windowRowsForCols(window, older, cols)
	if len(existing) > 0 && len(existingRows) == 0 {
		return false, nil, nil
	}
	// 中文说明：existing tail 来自当前 frozen history，reducer 后续只读它；
	// prepend older 时只复制 slice header，避免每次加载上一页都深拷贝全部已加载历史。
	rows := make([]HistoryRow, 0, len(olderRows)+len(existingRows))
	rows = append(rows, olderRows...)
	rows = append(rows, existingRows...)
	spans := make([]HistoryLineSpan, 0, len(olderSpans)+len(existingSpans))
	spans = append(spans, olderSpans...)
	if len(existingSpans) == 0 && len(existingRows) > 0 {
		existingSpans = historyLineSpansForSearch(HistoryStore{Rows: existingRows})
	}
	spans = appendRebasedHistoryLineSpans(spans, existingSpans, len(olderRows))
	return true, rows, spans
}

func fastAppendedHistoryRows(existing []HistoryLogicalLine, newer []HistoryLogicalLine, existingRows []HistoryRow, existingSpans []HistoryLineSpan, cols int, window HistoryWindow) (bool, []HistoryRow, []HistoryLineSpan) {
	if historyAppendNeedsBoundaryMerge(existing, newer) {
		return false, nil, nil
	}
	newerRows, newerSpans := windowRowsForCols(window, newer, cols)
	if len(existing) > 0 && len(existingRows) == 0 {
		return false, nil, nil
	}
	rows := make([]HistoryRow, 0, len(existingRows)+len(newerRows))
	rows = append(rows, existingRows...)
	rows = append(rows, newerRows...)
	if len(existingSpans) == 0 && len(existingRows) > 0 {
		existingSpans = historyLineSpansForSearch(HistoryStore{Rows: existingRows})
	}
	spans := make([]HistoryLineSpan, 0, len(existingSpans)+len(newerSpans))
	spans = append(spans, existingSpans...)
	spans = appendRebasedHistoryLineSpans(spans, newerSpans, len(existingRows))
	return true, rows, spans
}

func historyPrependNeedsBoundaryMerge(older []HistoryLogicalLine, existing []HistoryLogicalLine) bool {
	if len(older) == 0 || len(existing) == 0 {
		return false
	}
	lastOlder := older[len(older)-1]
	firstExisting := existing[0]
	return sameHistoryLogicalLineSource(lastOlder, firstExisting) &&
		lastOlder.ClippedAfter &&
		firstExisting.ClippedBefore
}

func historyAppendNeedsBoundaryMerge(existing []HistoryLogicalLine, newer []HistoryLogicalLine) bool {
	if len(existing) == 0 || len(newer) == 0 {
		return false
	}
	lastExisting := existing[len(existing)-1]
	firstNewer := newer[0]
	return sameHistoryLogicalLineSource(lastExisting, firstNewer) &&
		lastExisting.ClippedAfter &&
		firstNewer.ClippedBefore
}

func prependHistoryLogicalLines(older []HistoryLogicalLine, existing []HistoryLogicalLine) []HistoryLogicalLine {
	if len(older) == 0 {
		return existing
	}
	if len(existing) == 0 {
		return older
	}
	out := make([]HistoryLogicalLine, 0, len(older)+len(existing))
	out = append(out, older...)
	return append(out, existing...)
}

func appendHistoryLogicalLines(existing []HistoryLogicalLine, newer []HistoryLogicalLine) []HistoryLogicalLine {
	if len(newer) == 0 {
		return existing
	}
	if len(existing) == 0 {
		return newer
	}
	out := make([]HistoryLogicalLine, 0, len(existing)+len(newer))
	out = append(out, existing...)
	return append(out, newer...)
}

func mergePrependedHistoryLogicalLines(older []HistoryLogicalLine, existing []HistoryLogicalLine) []HistoryLogicalLine {
	if len(older) == 0 {
		return cloneHistoryLogicalLines(existing)
	}
	if len(existing) == 0 {
		return cloneHistoryLogicalLines(older)
	}
	merged := cloneHistoryLogicalLines(older)
	rest := cloneHistoryLogicalLines(existing)
	lastOlder := &merged[len(merged)-1]
	firstExisting := rest[0]
	if sameHistoryLogicalLineSource(*lastOlder, firstExisting) &&
		lastOlder.ClippedAfter &&
		firstExisting.ClippedBefore {
		lastOlder.Text += firstExisting.Text
		lastOlder.Cells = append(lastOlder.Cells, cloneHistoryCells(firstExisting.Cells)...)
		if firstExisting.TailFill != nil {
			lastOlder.TailFill = cloneHistoryCellStyle(firstExisting.TailFill)
		}
		lastOlder.LiveTail = lastOlder.LiveTail || firstExisting.LiveTail
		// 中文说明：boundary overlap 代表 older partial 的尾部和 existing partial 的头部
		// 正好拼上了同一 logical line 的中缝；合并后只保留真正外侧还没补齐的 clipped 边。
		lastOlder.ClippedBefore = lastOlder.ClippedBefore
		lastOlder.ClippedAfter = firstExisting.ClippedAfter
		rest = rest[1:]
	}
	return append(merged, rest...)
}

func mergeAppendedHistoryLogicalLines(existing []HistoryLogicalLine, newer []HistoryLogicalLine) []HistoryLogicalLine {
	if len(existing) == 0 {
		return cloneHistoryLogicalLines(newer)
	}
	if len(newer) == 0 {
		return cloneHistoryLogicalLines(existing)
	}
	merged := cloneHistoryLogicalLines(existing)
	rest := cloneHistoryLogicalLines(newer)
	lastExisting := &merged[len(merged)-1]
	firstNewer := rest[0]
	if sameHistoryLogicalLineSource(*lastExisting, firstNewer) &&
		lastExisting.ClippedAfter &&
		firstNewer.ClippedBefore {
		lastExisting.Text += firstNewer.Text
		lastExisting.Cells = append(lastExisting.Cells, cloneHistoryCells(firstNewer.Cells)...)
		if firstNewer.TailFill != nil {
			lastExisting.TailFill = cloneHistoryCellStyle(firstNewer.TailFill)
		}
		lastExisting.LiveTail = lastExisting.LiveTail || firstNewer.LiveTail
		lastExisting.ClippedAfter = firstNewer.ClippedAfter
		rest = rest[1:]
	}
	return append(merged, rest...)
}

func windowRowsForCols(window HistoryWindow, lines []HistoryLogicalLine, cols int) ([]HistoryRow, []HistoryLineSpan) {
	if len(window.SourceLines) > 0 && cols == window.Cols && len(window.Rows) > 0 {
		return window.Rows, window.Lines
	}
	return ReflowHistoryLogicalLines(lines, cols)
}

func historyWindowSourceLinesOwned(window HistoryWindow) []HistoryLogicalLine {
	if len(window.SourceLines) > 0 {
		return window.SourceLines
	}
	if len(window.Rows) == 0 {
		return nil
	}
	return historyRowsToLogicalLines(window.Rows, window.Lines)
}

func historyRowsToLogicalLines(rows []HistoryRow, spans []HistoryLineSpan) []HistoryLogicalLine {
	if len(rows) == 0 {
		return nil
	}
	lines := make([]HistoryLogicalLine, 0, len(rows))
	for index, row := range rows {
		if len(lines) > 0 && sameHistoryRowLogicalLineSource(lines[len(lines)-1], row) {
			lines[len(lines)-1].Text += row.Text
			lines[len(lines)-1].Cells = append(lines[len(lines)-1].Cells, cloneHistoryCells(row.Cells)...)
			if row.TailFill != nil {
				lines[len(lines)-1].TailFill = cloneHistoryCellStyle(row.TailFill)
			}
			if row.Segment != "" {
				lines[len(lines)-1].Segment = row.Segment
			}
			lines[len(lines)-1].LiveTail = lines[len(lines)-1].LiveTail || row.LiveTail
			continue
		}
		span, hasSpan := historySpanForRow(spans, index, row)
		lines = append(lines, HistoryLogicalLine{
			Text:               row.Text,
			Cells:              cloneHistoryCells(row.Cells),
			LineID:             row.LineID,
			Kind:               row.Kind,
			Segment:            row.Segment,
			SessionID:          row.SessionID,
			FrameID:            row.FrameID,
			FixedGrid:          row.FixedGrid,
			ScreenCols:         row.ScreenCols,
			ProjectionRowIndex: row.ProjectionRowIndex,
			TailFill:           cloneHistoryCellStyle(row.TailFill),
			LiveTail:           row.LiveTail,
			ClippedBefore:      hasSpan && span.ClippedBefore,
			ClippedAfter:       hasSpan && span.ClippedAfter,
		})
	}
	return lines
}

func historySpanForRow(spans []HistoryLineSpan, rowIndex int, row HistoryRow) (HistoryLineSpan, bool) {
	for _, span := range spans {
		if span.LineID != 0 && row.LineID != 0 && span.LineID != row.LineID {
			continue
		}
		if rowIndex < span.StartRow || rowIndex > span.EndRow {
			continue
		}
		if span.Kind != "" && span.Kind != row.Kind {
			continue
		}
		if span.Segment != "" && span.Segment != row.Segment {
			continue
		}
		if span.SessionID != 0 && span.SessionID != row.SessionID {
			continue
		}
		if span.FrameID != 0 && span.FrameID != row.FrameID {
			continue
		}
		if span.FixedGrid != row.FixedGrid {
			continue
		}
		if span.FixedGrid && span.ScreenCols != 0 && span.ScreenCols != row.ScreenCols {
			continue
		}
		return span, true
	}
	return HistoryLineSpan{}, false
}

func sameHistoryRowLogicalLineSource(line HistoryLogicalLine, row HistoryRow) bool {
	return line.LineID != 0 &&
		line.LineID == row.LineID &&
		line.Kind == row.Kind &&
		line.Segment == row.Segment &&
		line.SessionID == row.SessionID &&
		line.FrameID == row.FrameID &&
		line.FixedGrid == row.FixedGrid &&
		(!line.FixedGrid || line.ScreenCols == row.ScreenCols)
}

func (store HistoryStore) EnsureSourceLines() HistoryStore {
	if len(store.SourceLines) > 0 || len(store.Rows) == 0 {
		return store
	}
	store.SourceLines = historyRowsToLogicalLines(store.Rows, store.Lines)
	return store
}

func (store HistoryStore) TrimRows(startRow int, endRow int) (HistoryStore, HistoryTrimResult) {
	totalRows := len(store.Rows)
	if totalRows == 0 {
		return store, HistoryTrimResult{}
	}
	if startRow < 0 {
		startRow = 0
	}
	if endRow >= totalRows {
		endRow = totalRows - 1
	}
	if startRow <= 0 && endRow >= totalRows-1 {
		return store, HistoryTrimResult{}
	}
	if startRow > endRow {
		return store, HistoryTrimResult{}
	}
	store = store.EnsureSourceLines()
	if len(store.SourceLines) == 0 {
		return store, HistoryTrimResult{}
	}
	startLine := historySourceLineIndexForRow(store.SourceLines, store.Rows[startRow])
	endLine := historySourceLineIndexForRow(store.SourceLines, store.Rows[endRow])
	if startLine < 0 || endLine < startLine {
		return store, HistoryTrimResult{}
	}
	firstKeptRow := historyFirstRowIndexForSource(store.Rows, store.SourceLines[startLine])
	lastKeptRow := historyLastRowIndexForSource(store.Rows, store.SourceLines[endLine])
	if firstKeptRow < 0 || lastKeptRow < firstKeptRow {
		return store, HistoryTrimResult{}
	}
	result := HistoryTrimResult{
		DroppedRowsBefore:  firstKeptRow,
		DroppedRowsAfter:   totalRows - lastKeptRow - 1,
		DroppedLinesBefore: startLine,
		DroppedLinesAfter:  len(store.SourceLines) - endLine - 1,
	}
	boundaryLast := store.Boundary.LastLineID
	// 中文说明：copy/history 的本地 rows 只是当前窗口投影，不是历史 truth。
	// 裁剪时重新分配 slice，确保被丢弃窗口的 backing array 和 cell payload 可以被 GC。
	source := cloneHistoryLogicalLines(store.SourceLines[startLine : endLine+1])
	store.SourceLines = source
	store.Rows, store.Lines = ReflowHistoryLogicalLines(source, store.Cols)
	if len(store.Rows) > 0 {
		store.Boundary.FirstLineID = store.Rows[0].LineID
		if boundaryLast == 0 {
			boundaryLast = store.Rows[len(store.Rows)-1].LineID
		}
		store.Boundary.LastLineID = boundaryLast
		// 中文说明：本地裁剪不能把 core projection cursor 重建成 row-in-line。
		// ProjectionRowIndex 是 core 发来的 authoritative row index；旧协议缺字段时
		// 才退回按 source/core row count 平移。
		store.Cursor = cursorBeforeFirstLocalRow(store.Rows, store.Cursor, result.DroppedLinesBefore)
	}
	return store, result
}

func cursorBeforeFirstLocalRow(rows []HistoryRow, previous HistoryCursor, droppedRowsBefore int) HistoryCursor {
	if len(rows) == 0 {
		return HistoryCursor{}
	}
	first := rows[0]
	if first.LineID == 0 {
		return HistoryCursor{}
	}
	cursor := previous
	cursor.BeforeLineID = first.LineID
	cursor.BeforeRowInLine = first.RowInLine
	if first.ProjectionRowIndex > 0 || previous.BeforeRowIndex == 0 && droppedRowsBefore == 0 {
		cursor.BeforeRowIndex = first.ProjectionRowIndex
	} else {
		cursor.BeforeRowIndex = previous.BeforeRowIndex + droppedRowsBefore
	}
	cursor.Valid = previous.Valid || cursor.BeforeRowIndex > 0
	if first.Segment != "" {
		cursor.Segment = first.Segment
	}
	return cursor
}

func historyFirstRowIndexForSource(rows []HistoryRow, source HistoryLogicalLine) int {
	for index, row := range rows {
		if sameHistoryRowLogicalLineSource(source, row) {
			return index
		}
	}
	return -1
}

func historyLastRowIndexForSource(rows []HistoryRow, source HistoryLogicalLine) int {
	for index := len(rows) - 1; index >= 0; index-- {
		if sameHistoryRowLogicalLineSource(source, rows[index]) {
			return index
		}
	}
	return -1
}

func historySourceLineIndexForRow(lines []HistoryLogicalLine, row HistoryRow) int {
	for index, line := range lines {
		if sameHistoryRowLogicalLineSource(line, row) {
			return index
		}
	}
	return -1
}

func (store CopyModeStore) BindLatest(paneID string, viewID string, terminalID string, requestID RequestID, cols int, rows int, enteringLive LiveSurfaceSnapshot) CopyModeStore {
	// 中文说明：进入 copy/history 分两步。latest 请求飞行期间只拦截输入，
	// 不把 pane 内容切成 pending 占位；authoritative window 回来后才 Active。
	store.Active = false
	store.Entering = true
	store.Phase = CopyModeEnteringLivePreview
	store.PaneID = paneID
	store.ViewID = viewID
	store.TerminalID = terminalID
	store.EnteringLive = cloneLiveSurfaceSnapshotPtr(enteringLive)
	store.EnteringScrollDelta = 0
	store.RequestID = requestID
	store.ProjectionRequestID = 0
	store.Materialized = CopyModeMaterializedProjectionState{}
	store.BoundCols = cols
	store.ViewRows = rows
	store.Empty = true
	return store
}

// BindProjectionRequest 记录 copy-entry materialized 请求边界。
// 该请求不创建 frozen token，也不占用 HistoryStore.Pending；response 只能按此 id
// 回到 AcceptMaterializedProjection，不能复用 latest/older 的 stale guard。
func (store CopyModeStore) BindProjectionRequest(requestID RequestID) CopyModeStore {
	store.ProjectionRequestID = requestID
	return store
}

// PhaseKind 返回 copy mode 的有效阶段。
// 旧测试和旧恢复态可能只填 Active/Entering/BoundToken，因此这里做只读推导；
// 新写入路径仍必须显式设置 Phase，避免 materialized/frozen 语义混淆。
func (store CopyModeStore) PhaseKind() CopyModePhase {
	if store.Phase != CopyModePhaseNone {
		return store.Phase
	}
	if store.Entering {
		return CopyModeEnteringLivePreview
	}
	if store.Active && store.BoundToken != "" {
		return CopyModeFrozenHistory
	}
	if store.Active && store.Materialized.Valid {
		return CopyModeMaterializedProjection
	}
	if store.Active {
		return CopyModeFrozenHistory
	}
	return CopyModePhaseNone
}

// InputActive 表示 copy mode 是否应该吞掉当前 view 输入。
// preview/materialized/frozen 都属于 copy 交互上下文；只有 none 可以把输入继续交给 terminal。
func (store CopyModeStore) InputActive() bool {
	return store.PhaseKind() != CopyModePhaseNone || store.Active || store.Entering
}

// HistoryRenderable 表示 copy-history renderer 可以消费 HistoryStore rows。
// preview 仍走 live preview 分支；materialized 和 frozen 都必须先通过绑定校验。
func (store CopyModeStore) HistoryRenderable() bool {
	phase := store.PhaseKind()
	return phase == CopyModeMaterializedProjection || phase == CopyModeFrozenHistory
}

// CanSelect 表示当前 projection 允许在已接纳 rows 内移动 cursor/选区。
func (store CopyModeStore) CanSelect() bool {
	switch store.PhaseKind() {
	case CopyModeMaterializedProjection:
		return store.Materialized.Capabilities.Selectable
	case CopyModeFrozenHistory:
		return store.Active
	default:
		return false
	}
}

// CanCopy 表示当前 projection 允许复制本地已接纳 rows。
// frozen token 若需要跨本地裁剪窗口回 backend，仍由 app 层 SelectionNeedsBackend 继续校验 token。
func (store CopyModeStore) CanCopy() bool {
	switch store.PhaseKind() {
	case CopyModeMaterializedProjection:
		return store.Materialized.Capabilities.Copyable
	case CopyModeFrozenHistory:
		return store.Active
	default:
		return false
	}
}

// CanSearch 表示当前 projection 允许本地搜索已接纳 rows。
func (store CopyModeStore) CanSearch() bool {
	switch store.PhaseKind() {
	case CopyModeMaterializedProjection:
		return store.Materialized.Capabilities.Searchable
	case CopyModeFrozenHistory:
		return store.Active
	default:
		return false
	}
}

// CanPageHistory 表示当前 projection 允许使用 authoritative older/newer/oldest 合同。
// materialized projection 即使有 rows，也只有 core 明确给 pageable 能力时才能跨页；
// frozen token 默认沿用既有分页合同。
func (store CopyModeStore) CanPageHistory() bool {
	switch store.PhaseKind() {
	case CopyModeMaterializedProjection:
		return store.Materialized.Capabilities.Pageable && !store.Materialized.CatchupPending
	case CopyModeFrozenHistory:
		return store.Active && store.BoundToken != ""
	default:
		return false
	}
}

func (store CopyModeStore) acceptHistoryRows(window HistoryWindow, cols int, totalRows int, enteringLive *LiveSurfaceSnapshot, pendingScroll int) CopyModeStore {
	store.PaneID = window.PaneID
	store.ViewID = window.ViewID
	store.TerminalID = window.TerminalID
	if cols <= 0 {
		cols = window.Cols
	}
	store.BoundCols = cols
	if totalRows <= 0 {
		totalRows = len(window.Rows)
	}
	if totalRows > 0 {
		// 中文说明：copy cursor 的 truth 是 authoritative latest window 的最新尾行；
		// live snapshot 只能在仍包含尾行时修正首屏 viewport，不能把 cursor 改锚到
		// primary current frame 的第一行，否则鼠标轻滚会从顶部开始走。
		visibleRows := copyVisibleRowsForStore(store)
		cursorRow := totalRows - 1
		viewportTop := maxCopyInt(0, totalRows-visibleRows)
		viewportTopPadding := 0
		if liveViewport, ok := liveSurfaceMatchedViewport(window.Rows, enteringLive, visibleRows); ok &&
			cursorRow >= liveViewport.Top &&
			cursorRow < liveViewport.Top+copyVisibleHistoryRowsAfterPadding(visibleRows, liveViewport.TopPadding) {
			// 中文说明：live snapshot 只作为进入 copy/history 前的屏幕锚点参照；
			// 真正渲染、复制和搜索仍全部使用 authoritative HistoryWindow rows。
			viewportTop = liveViewport.Top
			viewportTopPadding = liveViewport.TopPadding
		}
		store.Cursor = CopyPosition{Row: cursorRow}
		store.ViewportTop = viewportTop
		store.ViewportTopPadding = viewportTopPadding
		if pendingScroll != 0 {
			store = store.ScrollCursor(pendingScroll, totalRows)
		}
	} else {
		store.ViewportTop = 0
		store.ViewportTopPadding = 0
		store.Cursor = CopyPosition{}
	}
	store.Matches = nil
	store.ActiveMatch = 0
	store.Empty = totalRows == 0
	return store
}

func (store CopyModeStore) AcceptLatest(window HistoryWindow, cols int, totalRows int) CopyModeStore {
	store.Active = true
	store.Entering = false
	store.Phase = CopyModeFrozenHistory
	enteringLive := store.EnteringLive
	store.EnteringLive = nil
	pendingScroll := store.EnteringScrollDelta
	store.EnteringScrollDelta = 0
	store.RequestID = 0
	store.ProjectionRequestID = 0
	store.Materialized = CopyModeMaterializedProjectionState{}
	store.BoundToken = window.Token
	return store.acceptHistoryRows(window, cols, totalRows, enteringLive, pendingScroll)
}

// AcceptMaterializedProjection 激活 copy-entry projection 阶段。
// 它只绑定 core 已应用 frontier 的 rows 和能力位，不写 frozen token；older/newer/search/copy
// 必须继续受 Capabilities 和 CatchupPending 约束。
func (store CopyModeStore) AcceptMaterializedProjection(window HistoryWindow, cols int, totalRows int, projection CopyModeMaterializedProjectionState) CopyModeStore {
	if store.PhaseKind() == CopyModeFrozenHistory {
		return store
	}
	store.Active = true
	store.Entering = false
	store.Phase = CopyModeMaterializedProjection
	enteringLive := store.EnteringLive
	store.EnteringLive = nil
	pendingScroll := store.EnteringScrollDelta
	store.EnteringScrollDelta = 0
	store.BoundToken = ""
	store.RequestID = 0
	store.ProjectionRequestID = 0
	projection.Valid = true
	store.Materialized = projection
	return store.acceptHistoryRows(window, cols, totalRows, enteringLive, pendingScroll)
}

func (store CopyModeStore) AcceptOldest(window HistoryWindow, cols int, totalRows int) CopyModeStore {
	store.Active = true
	store.Entering = false
	store.Phase = CopyModeFrozenHistory
	store.EnteringLive = nil
	store.RequestID = 0
	store.ProjectionRequestID = 0
	store.Materialized = CopyModeMaterializedProjectionState{}
	store.PaneID = window.PaneID
	store.ViewID = window.ViewID
	store.TerminalID = window.TerminalID
	store.BoundToken = window.Token
	if cols <= 0 {
		cols = window.Cols
	}
	store.BoundCols = cols
	// 中文说明：oldest 是用户显式跳到最老页，必须从第 0 行开始显示；
	// 不能复用 latest 的尾部定位，否则 `g` 会跳过真正最老的第一屏。
	store.ViewportTop = 0
	store.ViewportTopPadding = 0
	store.Cursor = CopyPosition{}
	store.Matches = nil
	store.ActiveMatch = 0
	store.Empty = totalRows == 0
	return store
}

func (store CopyModeStore) AcceptOlder(insertedRows int, before HistoryStore, after HistoryStore, window HistoryWindow, cols int) CopyModeStore {
	store.Phase = CopyModeFrozenHistory
	store.Active = true
	store.Entering = false
	store.Materialized = CopyModeMaterializedProjectionState{}
	if store.Query == "" && copyModeCanShiftSimpleOlderPrepend(insertedRows, before, after) {
		store = store.shiftAfterOlderPrepend(insertedRows)
	} else if len(before.Rows) > 0 && len(after.Rows) > 0 {
		// 中文说明：older prepend 可能不仅是“顶部多了几行”，也可能在 boundary overlap
		// 处把同一 logical line 的 partial source 合并成新的本地 rows。这里统一按
		// before/after frozen history 做内容重绑，避免 cursor/selection 只按行号平移后偏到错误内容。
		store = store.RebindToReflowedHistory(before, after)
	} else if insertedRows > 0 {
		store = store.shiftAfterOlderPrepend(insertedRows)
	}
	store.BoundToken = window.Token
	if cols <= 0 {
		cols = window.Cols
	}
	store.BoundCols = cols
	store.Empty = false
	store.ViewportTopPadding = 0
	return store
}

func copyModeCanShiftSimpleOlderPrepend(insertedRows int, before HistoryStore, after HistoryStore) bool {
	if insertedRows <= 0 || len(before.Rows) == 0 || len(after.Rows) == 0 {
		return false
	}
	if before.Cols != after.Cols || insertedRows+len(before.Rows) != len(after.Rows) {
		return false
	}
	return historyRowsSameAnchor(before.Rows[0], after.Rows[insertedRows]) &&
		historyRowsSameAnchor(before.Rows[len(before.Rows)-1], after.Rows[len(after.Rows)-1])
}

func historyRowsSameAnchor(left HistoryRow, right HistoryRow) bool {
	return left.LineID == right.LineID &&
		left.RowInLine == right.RowInLine &&
		left.Kind == right.Kind &&
		left.Segment == right.Segment &&
		left.SessionID == right.SessionID &&
		left.FrameID == right.FrameID &&
		left.FixedGrid == right.FixedGrid &&
		(!left.FixedGrid || left.ScreenCols == right.ScreenCols) &&
		left.ProjectionRowIndex == right.ProjectionRowIndex &&
		left.Text == right.Text &&
		left.ClippedStart == right.ClippedStart &&
		left.ClippedEnd == right.ClippedEnd &&
		historyTailFillSameAnchor(left.TailFill, right.TailFill) &&
		historyCellsSameAnchor(left.Cells, right.Cells)
}

func historyTailFillSameAnchor(left *HistoryCellStyle, right *HistoryCellStyle) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func historyCellsSameAnchor(left []HistoryCell, right []HistoryCell) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (store CopyModeStore) shiftAfterOlderPrepend(insertedRows int) CopyModeStore {
	if insertedRows <= 0 {
		return store
	}
	store.ViewportTop += insertedRows
	store.Cursor.Row += insertedRows
	if store.Mark != nil {
		mark := *store.Mark
		mark.Row += insertedRows
		store.Mark = &mark
	}
	if store.Selection != nil {
		selection := *store.Selection
		selection.Anchor.Row += insertedRows
		selection.Focus.Row += insertedRows
		store.Selection = &selection
	}
	return store
}

func (store CopyModeStore) ApplyDeferredOlderScroll(rows int, totalRows int) CopyModeStore {
	if rows <= 0 {
		return store.FollowCursor(totalRows)
	}
	// older prepend 已经把原 visible top 重新锚到同一条 logical line；
	// 这里继续移动 copy cursor，保持“请求按页、浏览按光标”的 tmux 式语义。
	return store.ScrollCursor(-rows, totalRows)
}

func (store CopyModeStore) ApplyHistoryTrim(trim HistoryTrimResult, totalRows int) CopyModeStore {
	store.ViewportTopPadding = 0
	if trim.DroppedRowsBefore <= 0 && trim.DroppedRowsAfter <= 0 {
		return store.Scroll(0, totalRows)
	}
	shift := trim.DroppedRowsBefore
	if shift > 0 {
		store.ViewportTop -= shift
	}
	store.Cursor = shiftCopyPositionAfterTrim(store.Cursor, shift, totalRows)
	if store.Mark != nil {
		mark := shiftCopyPositionAfterTrim(*store.Mark, shift, totalRows)
		store.Mark = &mark
	}
	if store.Selection != nil {
		selection := *store.Selection
		selection.Anchor = shiftCopyPositionAfterTrim(selection.Anchor, shift, totalRows)
		selection.Focus = shiftCopyPositionAfterTrim(selection.Focus, shift, totalRows)
		store.Selection = &selection
	}
	store.Matches = shiftCopyMatchesAfterTrim(store.Matches, shift, totalRows)
	if store.ActiveMatch >= len(store.Matches) {
		store.ActiveMatch = maxCopyInt(0, len(store.Matches)-1)
	}
	store.ViewportTop = clampCopyInt(store.ViewportTop, 0, maxCopyInt(0, totalRows-copyVisibleRowsForStore(store)))
	return store.FollowCursor(totalRows)
}

func shiftCopyPositionAfterTrim(pos CopyPosition, droppedBefore int, totalRows int) CopyPosition {
	pos.Row -= droppedBefore
	if totalRows <= 0 {
		return CopyPosition{}
	}
	pos.Row = clampCopyInt(pos.Row, 0, totalRows-1)
	return pos
}

func shiftCopyMatchesAfterTrim(matches []CopyMatch, droppedBefore int, totalRows int) []CopyMatch {
	if len(matches) == 0 || totalRows <= 0 {
		return nil
	}
	out := make([]CopyMatch, 0, len(matches))
	for _, match := range matches {
		match.StartRow -= droppedBefore
		match.EndRow -= droppedBefore
		if match.EndRow < 0 || match.StartRow >= totalRows {
			continue
		}
		match.StartRow = clampCopyInt(match.StartRow, 0, totalRows-1)
		match.EndRow = clampCopyInt(match.EndRow, 0, totalRows-1)
		out = append(out, match)
	}
	return out
}

func (store CopyModeStore) Resize(cols int, rows int) CopyModeStore {
	store.BoundCols = cols
	store.ViewRows = rows
	store.ViewportTopPadding = 0
	return store
}

// RebindToReflowedHistory 在 frozen source 按新 cols 本地重排后，把 copy mode
// 的交互态重新映射回同一段 logical-line 内容，而不是继续沿用旧 visual row/col。
func (store CopyModeStore) RebindToReflowedHistory(before HistoryStore, after HistoryStore) CopyModeStore {
	store.BoundCols = after.Cols
	if len(after.Rows) == 0 {
		store.ViewportTop = 0
		store.ViewportTopPadding = 0
		store.Cursor = CopyPosition{}
		store.Mark = nil
		store.Selection = nil
		store.Matches = nil
		store.ActiveMatch = 0
		store.Empty = true
		return store
	}
	store.Empty = false
	store.ViewportTopPadding = 0
	store.ViewportTop = reflowViewportTop(before, after, store.ViewportTop)
	store.Cursor = reflowCopyPosition(before, after, store.Cursor)
	if store.Mark != nil {
		mark := reflowCopyPosition(before, after, *store.Mark)
		store.Mark = &mark
	}
	if store.Selection != nil {
		selection := *store.Selection
		selection.Anchor = reflowCopyPosition(before, after, selection.Anchor)
		selection.Focus = reflowCopyPosition(before, after, selection.Focus)
		store.Selection = &selection
	}
	if store.Query != "" {
		matches := FindCopyMatches(after, store.Query)
		store.Matches = cloneCopyMatches(matches)
		store.ActiveMatch = reflowActiveMatchIndex(after, store.Cursor, matches)
	} else {
		store.Matches = nil
		store.ActiveMatch = 0
	}
	return store
}

func (store CopyModeStore) SetViewRows(rows int) CopyModeStore {
	store.ViewRows = rows
	store.ViewportTopPadding = 0
	return store
}

func (store CopyModeStore) MoveCursor(pos CopyPosition) CopyModeStore {
	store.ViewportTopPadding = 0
	store.Cursor = pos
	if store.Mark != nil {
		selection := CopySelection{Anchor: *store.Mark, Focus: pos}
		if store.Selection != nil && store.Selection.Anchor == *store.Mark {
			selection.LogicalAnchor = store.Selection.LogicalAnchor
		}
		store.Selection = &selection
	}
	return store
}

// ScrollCursor 是 copy/history 的主滚动语义：用户滚动的是 copy cursor。
// cursor 还在可见区内时只移动 cursor；到达边缘后，viewport 才跟随移动。
func (store CopyModeStore) ScrollCursor(delta int, totalRows int) CopyModeStore {
	store.ViewportTopPadding = 0
	if totalRows <= 0 {
		store.ViewportTop = 0
		store.Cursor = CopyPosition{}
		return store
	}
	nextRow := clampCopyInt(store.Cursor.Row+delta, 0, totalRows-1)
	store = store.MoveCursor(CopyPosition{Row: nextRow, Col: store.Cursor.Col})
	return store.FollowCursor(totalRows)
}

// ScrollViewport 表达少数需要保持屏幕相对位置的内部 viewport 移动。
// 用户输入主路径应优先使用 ScrollCursor：history truth 继续来自 HistoryStore/core-v2，
// 这里只在进入态定位、兼容旧锚点或专门 viewport harness 中移动可见窗口。
func (store CopyModeStore) ScrollViewport(delta int, totalRows int) CopyModeStore {
	store.ViewportTopPadding = 0
	if totalRows <= 0 {
		store.ViewportTop = 0
		store.Cursor = CopyPosition{}
		return store
	}
	height := copyVisibleRowsForStore(store)
	if height <= 0 {
		height = 1
	}
	maxTop := maxCopyInt(0, totalRows-height)
	offset := store.Cursor.Row - store.ViewportTop
	if offset < 0 {
		offset = 0
	}
	if offset >= height {
		offset = height - 1
	}
	nextTop := clampCopyInt(store.ViewportTop+delta, 0, maxTop)
	store.ViewportTop = nextTop
	store.Cursor = CopyPosition{Row: clampCopyInt(nextTop+offset, 0, totalRows-1), Col: store.Cursor.Col}
	return store.Scroll(0, totalRows)
}

func (store CopyModeStore) FollowCursor(totalRows int) CopyModeStore {
	if totalRows <= 0 {
		store.ViewportTop = 0
		return store
	}
	height := copyVisibleRowsForStore(store)
	if store.Cursor.Row < store.ViewportTop {
		store.ViewportTop = store.Cursor.Row
	}
	if store.Cursor.Row >= store.ViewportTop+height {
		store.ViewportTop = store.Cursor.Row - height + 1
	}
	return store.Scroll(0, totalRows)
}

func (store CopyModeStore) SetMark(pos CopyPosition) CopyModeStore {
	store.Mark = &pos
	store.Selection = &CopySelection{Anchor: pos, Focus: pos}
	return store
}

func (store CopyModeStore) RefreshLogicalSelection(history HistoryStore) CopyModeStore {
	if store.Selection == nil {
		return store
	}
	selection := *store.Selection
	selection.LogicalAnchor = CopyLogicalPositionForPosition(history, selection.Anchor)
	selection.LogicalFocus = CopyLogicalPositionForPosition(history, selection.Focus)
	store.Selection = &selection
	return store
}

func (store CopyModeStore) EnsureLogicalSelection(history HistoryStore) CopyModeStore {
	if store.Selection == nil {
		return store
	}
	selection := *store.Selection
	if !selection.LogicalAnchor.Valid {
		selection.LogicalAnchor = CopyLogicalPositionForPosition(history, selection.Anchor)
	}
	if !selection.LogicalFocus.Valid {
		selection.LogicalFocus = CopyLogicalPositionForPosition(history, selection.Focus)
	}
	store.Selection = &selection
	return store
}

func (store CopyModeStore) RefreshLogicalSelectionFocus(history HistoryStore) CopyModeStore {
	if store.Selection == nil {
		return store
	}
	selection := *store.Selection
	selection.LogicalFocus = CopyLogicalPositionForPosition(history, selection.Focus)
	store.Selection = &selection
	return store
}

func (store CopyModeStore) SelectionLogicalRange(history HistoryStore) (CopyLogicalPosition, CopyLogicalPosition, bool) {
	if store.Selection == nil {
		return CopyLogicalPosition{}, CopyLogicalPosition{}, false
	}
	start := store.Selection.LogicalAnchor
	if !start.Valid {
		start = CopyLogicalPositionForPosition(history, store.Selection.Anchor)
	}
	end := store.Selection.LogicalFocus
	if !end.Valid {
		end = CopyLogicalPositionForPosition(history, store.Selection.Focus)
	}
	if !start.Valid || !end.Valid {
		return CopyLogicalPosition{}, CopyLogicalPosition{}, false
	}
	return start, end, true
}

func (store CopyModeStore) SelectionNeedsBackend(history HistoryStore) bool {
	if store.Selection == nil || !store.Selection.LogicalAnchor.Valid || !store.Selection.LogicalFocus.Valid {
		return false
	}
	currentAnchor := CopyLogicalPositionForPosition(history, store.Selection.Anchor)
	currentFocus := CopyLogicalPositionForPosition(history, store.Selection.Focus)
	return !copyLogicalPositionSame(currentAnchor, store.Selection.LogicalAnchor) ||
		!copyLogicalPositionSame(currentFocus, store.Selection.LogicalFocus)
}

func (store CopyModeStore) SetQuery(query string, matches []CopyMatch) CopyModeStore {
	store.ViewportTopPadding = 0
	store.Query = query
	store.Matches = cloneCopyMatches(matches)
	store.ActiveMatch = 0
	if len(store.Matches) > 0 {
		store.Cursor = CopyPosition{Row: store.Matches[0].StartRow, Col: store.Matches[0].StartCol}
	}
	return store
}

// RefreshQueryMatches 在 history 更新后重算 query 命中，但尽量保留当前正在看的
// active match / cursor；只有找不到原命中时，才退回到第一个 match。
func (store CopyModeStore) RefreshQueryMatches(matches []CopyMatch) CopyModeStore {
	store.ViewportTopPadding = 0
	store.Matches = cloneCopyMatches(matches)
	if len(store.Matches) == 0 {
		store.ActiveMatch = 0
		return store
	}
	index := reflowActiveMatchIndexFromMatches(store.Cursor, store.Matches)
	store.ActiveMatch = index
	match := store.Matches[index]
	store.Cursor = CopyPosition{Row: match.StartRow, Col: match.StartCol}
	return store
}

func FindCopyMatches(history HistoryStore, query string) []CopyMatch {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	matches := make([]CopyMatch, 0)
	queryClusters := textGraphemeClusters(query)
	for _, span := range historyLineSpansForSearch(history) {
		clusters, boundaries := historySpanGraphemeBoundaries(history, span)
		for start := 0; start+len(queryClusters) <= len(clusters); start++ {
			if strings.Join(clusters[start:start+len(queryClusters)], "") != query {
				continue
			}
			startPos := boundaries[start]
			endPos := boundaries[start+len(queryClusters)]
			matches = append(matches, CopyMatch{
				StartRow: startPos.Row,
				StartCol: startPos.Col,
				EndRow:   endPos.Row,
				EndCol:   endPos.Col,
			})
		}
	}
	return matches
}

func historyLineSpansForSearch(history HistoryStore) []HistoryLineSpan {
	if len(history.Lines) > 0 {
		return cloneHistoryLineSpans(history.Lines)
	}
	if len(history.Rows) == 0 {
		return nil
	}
	spans := make([]HistoryLineSpan, 0, len(history.Rows))
	start := 0
	for row := 1; row < len(history.Rows); row++ {
		if sameHistoryRowsSource(history.Rows[start], history.Rows[row]) {
			continue
		}
		spans = append(spans, historyLineSpanForLocalRows(history.Rows, start, row-1))
		start = row
	}
	spans = append(spans, historyLineSpanForLocalRows(history.Rows, start, len(history.Rows)-1))
	return spans
}

func sameHistoryRowsSource(left HistoryRow, right HistoryRow) bool {
	return left.LineID != 0 &&
		left.LineID == right.LineID &&
		left.Kind == right.Kind &&
		left.Segment == right.Segment &&
		left.SessionID == right.SessionID &&
		left.FrameID == right.FrameID &&
		left.FixedGrid == right.FixedGrid &&
		(!left.FixedGrid || left.ScreenCols == right.ScreenCols)
}

func historyLineSpanForLocalRows(rows []HistoryRow, start int, end int) HistoryLineSpan {
	if start < 0 || start >= len(rows) {
		return HistoryLineSpan{}
	}
	row := rows[start]
	return HistoryLineSpan{
		LineID:             row.LineID,
		StartRow:           start,
		EndRow:             end,
		Kind:               row.Kind,
		Segment:            row.Segment,
		SessionID:          row.SessionID,
		FrameID:            row.FrameID,
		FixedGrid:          row.FixedGrid,
		ScreenCols:         row.ScreenCols,
		ProjectionRowIndex: row.ProjectionRowIndex,
	}
}

func historySpanGraphemeBoundaries(history HistoryStore, span HistoryLineSpan) ([]string, []CopyPosition) {
	if len(history.Rows) == 0 || span.StartRow < 0 || span.StartRow >= len(history.Rows) {
		return nil, nil
	}
	endRow := span.EndRow
	if endRow < span.StartRow {
		endRow = span.StartRow
	}
	if endRow >= len(history.Rows) {
		endRow = len(history.Rows) - 1
	}
	clusters := make([]string, 0)
	boundaries := make([]CopyPosition, 0)
	for rowIndex := span.StartRow; rowIndex <= endRow; rowIndex++ {
		row := history.Rows[rowIndex]
		rowClusters := textGraphemeClusters(row.Text)
		rowColumns := HistoryRowGraphemeDisplayColumns(row)
		for i, cluster := range rowClusters {
			boundaries = append(boundaries, CopyPosition{Row: rowIndex, Col: rowColumns[i]})
			clusters = append(clusters, cluster)
		}
		if rowIndex == endRow {
			boundaries = append(boundaries, CopyPosition{Row: rowIndex, Col: rowColumns[len(rowColumns)-1]})
		}
	}
	return clusters, boundaries
}

func HistoryRowGraphemeDisplayColumns(row HistoryRow) []int {
	if len(row.Cells) == 0 {
		return textClusterDisplayColumns(textGraphemeClusters(row.Text))
	}
	columns := []int{0}
	cursor := 0
	for _, cell := range row.Cells {
		clusters := textGraphemeClusters(cell.Text)
		if len(clusters) == 0 {
			cursor += HistoryCellDisplayWidth(cell)
			continue
		}
		natural := textClusterDisplayColumns(clusters)
		cellWidth := HistoryCellDisplayWidth(cell)
		naturalWidth := natural[len(natural)-1]
		for index := 1; index < len(natural); index++ {
			columns = append(columns, cursor+scaleDisplayColumn(natural[index], naturalWidth, cellWidth))
		}
		cursor += cellWidth
	}
	return columns
}

func HistoryRowDisplayWidth(row HistoryRow) int {
	if len(row.Cells) == 0 {
		return textDisplayWidth(row.Text)
	}
	width := 0
	for _, cell := range row.Cells {
		width += HistoryCellDisplayWidth(cell)
	}
	return width
}

func HistoryCellDisplayWidth(cell HistoryCell) int {
	if cell.Width > 0 {
		return cell.Width
	}
	return textDisplayWidth(cell.Text)
}

func HistoryCellDisplayWidthSum(cells []HistoryCell) int {
	width := 0
	for _, cell := range cells {
		width += HistoryCellDisplayWidth(cell)
	}
	return width
}

func HistoryRowSliceDisplay(row HistoryRow, from int, to int) string {
	width := HistoryRowDisplayWidth(row)
	from = clampCopyInt(from, 0, width)
	to = clampCopyInt(to, from, width)
	if to <= from {
		return ""
	}
	if len(row.Cells) == 0 {
		return textSliceDisplay(row.Text, from, to)
	}
	var builder strings.Builder
	cursor := 0
	for _, cell := range row.Cells {
		cellWidth := HistoryCellDisplayWidth(cell)
		next := cursor + cellWidth
		if rangesOverlap(cursor, next, from, to) {
			builder.WriteString(historyCellSliceDisplay(cell, from-cursor, to-cursor))
		}
		cursor = next
	}
	return builder.String()
}

func historyCellSliceDisplay(cell HistoryCell, from int, to int) string {
	width := HistoryCellDisplayWidth(cell)
	from = clampCopyInt(from, 0, width)
	to = clampCopyInt(to, from, width)
	if to <= from {
		return ""
	}
	textWidth := textDisplayWidth(cell.Text)
	textPart := ""
	if from < textWidth {
		textEnd := to
		if textEnd > textWidth {
			textEnd = textWidth
		}
		textPart = textSliceDisplay(cell.Text, from, textEnd)
	}
	pad := to - maxCopyInt(from, textWidth)
	if pad <= 0 {
		return textPart
	}
	return textPart + strings.Repeat(" ", pad)
}

func textClusterDisplayColumns(clusters []string) []int {
	columns := make([]int, len(clusters)+1)
	for index, cluster := range clusters {
		columns[index+1] = columns[index] + textDisplayWidth(cluster)
	}
	return columns
}

func scaleDisplayColumn(column int, naturalWidth int, authoritativeWidth int) int {
	if naturalWidth <= 0 || naturalWidth == authoritativeWidth {
		return column
	}
	return (column*authoritativeWidth + naturalWidth - 1) / naturalWidth
}

func textGraphemeClusters(text string) []string {
	if text == "" {
		return nil
	}
	clusters := make([]string, 0, len([]rune(text)))
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		clusters = append(clusters, graphemes.Str())
	}
	return clusters
}

func textDisplayWidth(text string) int {
	return xansi.StringWidth(strings.ReplaceAll(text, "\n", " "))
}

func textSliceDisplay(text string, from int, to int) string {
	if to <= from {
		return ""
	}
	var builder strings.Builder
	cursor := 0
	for _, cluster := range textGraphemeClusters(text) {
		width := textDisplayWidth(cluster)
		next := cursor + width
		if width == 0 {
			next = cursor
		}
		if rangesOverlap(cursor, maxCopyInt(next, cursor+1), from, to) {
			builder.WriteString(cluster)
		}
		cursor += width
	}
	return builder.String()
}

func rangesOverlap(leftFrom int, leftTo int, rightFrom int, rightTo int) bool {
	return leftFrom < rightTo && rightFrom < leftTo
}

func (store CopyModeStore) MoveMatch(delta int) CopyModeStore {
	if len(store.Matches) == 0 {
		return store
	}
	store.ActiveMatch += delta
	for store.ActiveMatch < 0 {
		store.ActiveMatch += len(store.Matches)
	}
	store.ActiveMatch %= len(store.Matches)
	match := store.Matches[store.ActiveMatch]
	store.Cursor = CopyPosition{Row: match.StartRow, Col: match.StartCol}
	return store
}

func (store CopyModeStore) Scroll(delta int, totalRows int) CopyModeStore {
	if delta != 0 {
		store.ViewportTopPadding = 0
	}
	maxTop := maxCopyInt(0, totalRows-copyVisibleRowsForStore(store))
	store.ViewportTop = clampCopyInt(store.ViewportTop+delta, 0, maxTop)
	return store
}

type liveViewportMatch struct {
	Top        int
	TopPadding int
}

func liveSurfaceMatchedViewport(rows []HistoryRow, enteringLive *LiveSurfaceSnapshot, visibleRows int) (liveViewportMatch, bool) {
	if enteringLive == nil || visibleRows <= 0 || len(rows) == 0 {
		return liveViewportMatch{}, false
	}
	liveRows, topPadding := normalizedEnteringLiveRows(*enteringLive, visibleRows)
	if len(liveRows) == 0 {
		return liveViewportMatch{}, false
	}
	historyRows := make([]string, len(rows))
	for i, row := range rows {
		historyRows[i] = normalizeHistoryViewportRow(row.Text)
	}
	start, ok := latestContiguousRowMatch(historyRows, liveRows)
	if !ok {
		return liveViewportMatch{}, false
	}
	visibleHistoryRows := copyVisibleHistoryRowsAfterPadding(visibleRows, topPadding)
	return liveViewportMatch{
		Top:        clampCopyInt(start, 0, maxCopyInt(0, len(rows)-visibleHistoryRows)),
		TopPadding: topPadding,
	}, true
}

func normalizedEnteringLiveRows(snapshot LiveSurfaceSnapshot, visibleRows int) ([]string, int) {
	rows := cloneStrings(snapshot.Lines)
	if len(rows) == 0 && len(snapshot.Screen) > 0 {
		rows = make([]string, 0, len(snapshot.Screen))
		for _, row := range snapshot.Screen {
			var builder strings.Builder
			for _, cell := range row {
				builder.WriteString(cell.Text)
			}
			rows = append(rows, builder.String())
		}
	}
	for i := range rows {
		rows[i] = normalizeHistoryViewportRow(rows[i])
	}
	if visibleRows > 0 && len(rows) > visibleRows {
		// 中文说明：只保留进入 history 前用户实际可见的 live viewport tail；
		// 更早的屏幕行必须走 authoritative history older，不可混入首屏锚点。
		rows = rows[len(rows)-visibleRows:]
	}
	return trimOuterEmptyHistoryViewportRows(rows)
}

func trimOuterEmptyHistoryViewportRows(rows []string) ([]string, int) {
	start := 0
	for start < len(rows) && rows[start] == "" {
		start++
	}
	end := len(rows)
	for end > start && rows[end-1] == "" {
		end--
	}
	if start == end {
		return nil, 0
	}
	return rows[start:end], start
}

func normalizeHistoryViewportRow(row string) string {
	return strings.TrimRight(row, " ")
}

func latestContiguousRowMatch(rows []string, pattern []string) (int, bool) {
	if len(pattern) == 0 || len(pattern) > len(rows) {
		return 0, false
	}
	for start := len(rows) - len(pattern); start >= 0; start-- {
		matched := true
		for offset := range pattern {
			if rows[start+offset] != pattern[offset] {
				matched = false
				break
			}
		}
		if matched {
			return start, true
		}
	}
	return 0, false
}

func cloneHistoryRows(rows []HistoryRow) []HistoryRow {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([]HistoryRow, len(rows))
	copy(cloned, rows)
	for i := range cloned {
		cloned[i].Cells = cloneHistoryCells(rows[i].Cells)
		cloned[i].TailFill = cloneHistoryCellStyle(rows[i].TailFill)
	}
	return cloned
}

func cloneHistoryLogicalLines(lines []HistoryLogicalLine) []HistoryLogicalLine {
	if len(lines) == 0 {
		return nil
	}
	cloned := make([]HistoryLogicalLine, len(lines))
	copy(cloned, lines)
	for i := range cloned {
		cloned[i].Cells = cloneHistoryCells(lines[i].Cells)
		cloned[i].TailFill = cloneHistoryCellStyle(lines[i].TailFill)
	}
	return cloned
}

func cloneHistoryCellStyle(style *HistoryCellStyle) *HistoryCellStyle {
	if style == nil {
		return nil
	}
	cloned := *style
	return &cloned
}

func cloneHistoryCells(cells []HistoryCell) []HistoryCell {
	if len(cells) == 0 {
		return nil
	}
	cloned := make([]HistoryCell, len(cells))
	copy(cloned, cells)
	return cloned
}

func cloneHistoryLineSpans(spans []HistoryLineSpan) []HistoryLineSpan {
	if len(spans) == 0 {
		return nil
	}
	cloned := make([]HistoryLineSpan, len(spans))
	copy(cloned, spans)
	return cloned
}

// ReflowHistoryLogicalLines 把冻结 logical-line source 重新排成本地可见 rows。
// 这是 TUI copy/history 的唯一重排路径；它不创造新的历史 truth，只产出 view rows。
func ReflowHistoryLogicalLines(lines []HistoryLogicalLine, cols int) ([]HistoryRow, []HistoryLineSpan) {
	if len(lines) == 0 {
		return nil, nil
	}
	if cols <= 0 {
		cols = 80
	}
	rows := make([]HistoryRow, 0, len(lines))
	spans := make([]HistoryLineSpan, 0, len(lines))
	for _, line := range lines {
		lineRows := reflowHistoryLogicalLine(line, cols)
		start := len(rows)
		rows = append(rows, lineRows...)
		end := len(rows) - 1
		if end < start {
			end = start
		}
		spans = append(spans, HistoryLineSpan{
			LineID:             line.LineID,
			StartRow:           start,
			EndRow:             end,
			Kind:               line.Kind,
			Segment:            line.Segment,
			SessionID:          line.SessionID,
			FrameID:            line.FrameID,
			FixedGrid:          line.FixedGrid,
			ScreenCols:         line.ScreenCols,
			ProjectionRowIndex: line.ProjectionRowIndex,
			ClippedBefore:      line.ClippedBefore,
			ClippedAfter:       line.ClippedAfter,
		})
	}
	return rows, spans
}

func reflowHistoryLogicalLine(line HistoryLogicalLine, cols int) []HistoryRow {
	cells := cloneHistoryCells(line.Cells)
	if isFixedGridHistoryRowKind(line.Kind) {
		return fixedGridHistoryRows(line, cells)
	}
	if len(cells) == 0 && line.Text != "" {
		return reflowPlainHistoryLogicalLine(line, cols)
	} else if len(cells) > 0 {
		cells = normalizeHistoryLogicalLineCells(cells, cols)
	}
	if len(cells) == 0 {
		return []HistoryRow{{LineID: line.LineID, Kind: line.Kind, Segment: line.Segment, SessionID: line.SessionID, FrameID: line.FrameID, FixedGrid: line.FixedGrid, ScreenCols: line.ScreenCols, ProjectionRowIndex: line.ProjectionRowIndex, LiveTail: line.LiveTail}}
	}
	rows := make([]HistoryRow, 0, 1)
	current := make([]HistoryCell, 0, len(cells))
	width := 0
	flush := func() {
		row := HistoryRow{
			Text:               historyCellsPlainTextForState(current),
			Cells:              cloneHistoryCells(current),
			LineID:             line.LineID,
			RowInLine:          len(rows),
			Kind:               line.Kind,
			Segment:            line.Segment,
			SessionID:          line.SessionID,
			FrameID:            line.FrameID,
			FixedGrid:          line.FixedGrid,
			ScreenCols:         line.ScreenCols,
			ProjectionRowIndex: line.ProjectionRowIndex,
			LiveTail:           line.LiveTail,
		}
		rows = append(rows, row)
		current = current[:0]
		width = 0
	}
	for _, cell := range cells {
		cellWidth := HistoryCellDisplayWidth(cell)
		if cellWidth <= 0 {
			continue
		}
		parts := []HistoryCell{cell}
		if cellWidth > cols || (width > 0 && width+cellWidth > cols) {
			// 中文说明：cell 小于 pane 宽但放不进当前行剩余列时，也要按
			// grapheme/padding 切开填满当前行；否则 ls 多列输出会提前换行。
			parts = splitHistoryCellForWrap(cell)
		}
		for _, part := range parts {
			partWidth := HistoryCellDisplayWidth(part)
			if partWidth <= 0 {
				continue
			}
			if width > 0 && width+partWidth > cols {
				flush()
			}
			current = append(current, part)
			width += partWidth
			if width >= cols {
				flush()
			}
		}
	}
	if len(current) > 0 || len(rows) == 0 {
		flush()
	}
	applyTailFillToLastHistoryRow(rows, line.TailFill)
	return rows
}

func isFixedGridHistoryRowKind(kind string) bool {
	return kind == HistoryRowKindScreenFrame || kind == HistoryRowKindArchivedScreenFrame || kind == HistoryRowKindAltScreenFrame
}

func fixedGridHistoryRows(line HistoryLogicalLine, cells []HistoryCell) []HistoryRow {
	if len(cells) == 0 && line.Text != "" {
		cells = []HistoryCell{{Text: line.Text, Width: textDisplayWidth(line.Text)}}
	}
	if len(cells) > 0 {
		cells = normalizeHistoryLogicalLineCells(cells, HistoryCellDisplayWidthSum(cells))
	}
	row := HistoryRow{
		Text:               historyCellsPlainTextForState(cells),
		Cells:              cloneHistoryCells(cells),
		LineID:             line.LineID,
		Kind:               line.Kind,
		Segment:            line.Segment,
		SessionID:          line.SessionID,
		FrameID:            line.FrameID,
		FixedGrid:          line.FixedGrid,
		ScreenCols:         line.ScreenCols,
		ProjectionRowIndex: line.ProjectionRowIndex,
		LiveTail:           line.LiveTail,
		TailFill:           cloneHistoryCellStyle(line.TailFill),
	}
	if row.Text == "" && line.Text != "" {
		row.Text = line.Text
	}
	return []HistoryRow{row}
}

func reflowPlainHistoryLogicalLine(line HistoryLogicalLine, cols int) []HistoryRow {
	if singleWidthASCIIText(line.Text) {
		return reflowPlainASCIIHistoryLogicalLine(line, cols)
	}
	clusters := textGraphemeClusters(line.Text)
	if len(clusters) == 0 {
		return []HistoryRow{{LineID: line.LineID, Kind: line.Kind, Segment: line.Segment, SessionID: line.SessionID, FrameID: line.FrameID, FixedGrid: line.FixedGrid, ScreenCols: line.ScreenCols, ProjectionRowIndex: line.ProjectionRowIndex, LiveTail: line.LiveTail}}
	}
	rows := make([]HistoryRow, 0, 1)
	var builder strings.Builder
	width := 0
	flush := func() {
		row := HistoryRow{
			Text:               builder.String(),
			LineID:             line.LineID,
			RowInLine:          len(rows),
			Kind:               line.Kind,
			Segment:            line.Segment,
			SessionID:          line.SessionID,
			FrameID:            line.FrameID,
			FixedGrid:          line.FixedGrid,
			ScreenCols:         line.ScreenCols,
			ProjectionRowIndex: line.ProjectionRowIndex,
			LiveTail:           line.LiveTail,
		}
		rows = append(rows, row)
		builder.Reset()
		width = 0
	}
	for _, cluster := range clusters {
		clusterWidth := textDisplayWidth(cluster)
		if clusterWidth <= 0 {
			continue
		}
		if width > 0 && width+clusterWidth > cols {
			flush()
		}
		builder.WriteString(cluster)
		width += clusterWidth
		if width >= cols {
			flush()
		}
	}
	if builder.Len() > 0 || len(rows) == 0 {
		flush()
	}
	applyTailFillToLastHistoryRow(rows, line.TailFill)
	return rows
}

func reflowPlainASCIIHistoryLogicalLine(line HistoryLogicalLine, cols int) []HistoryRow {
	if line.Text == "" {
		return []HistoryRow{{LineID: line.LineID, Kind: line.Kind, Segment: line.Segment, SessionID: line.SessionID, FrameID: line.FrameID, FixedGrid: line.FixedGrid, ScreenCols: line.ScreenCols, ProjectionRowIndex: line.ProjectionRowIndex, LiveTail: line.LiveTail}}
	}
	if cols <= 0 {
		cols = 80
	}
	rows := make([]HistoryRow, 0, (len(line.Text)+cols-1)/cols)
	for start := 0; start < len(line.Text); start += cols {
		end := start + cols
		if end > len(line.Text) {
			end = len(line.Text)
		}
		rows = append(rows, HistoryRow{
			Text:               line.Text[start:end],
			LineID:             line.LineID,
			RowInLine:          len(rows),
			Kind:               line.Kind,
			Segment:            line.Segment,
			SessionID:          line.SessionID,
			FrameID:            line.FrameID,
			FixedGrid:          line.FixedGrid,
			ScreenCols:         line.ScreenCols,
			ProjectionRowIndex: line.ProjectionRowIndex,
			LiveTail:           line.LiveTail,
		})
	}
	applyTailFillToLastHistoryRow(rows, line.TailFill)
	return rows
}

func singleWidthASCIIText(text string) bool {
	if text == "" {
		return true
	}
	for i := 0; i < len(text); i++ {
		if text[i] < 0x20 || text[i] >= 0x7f {
			return false
		}
	}
	return true
}

func applyTailFillToLastHistoryRow(rows []HistoryRow, fill *HistoryCellStyle) {
	if len(rows) == 0 || fill == nil {
		return
	}
	tail := *fill
	rows[len(rows)-1].TailFill = &tail
}

func splitHistoryLogicalLineText(text string) []HistoryCell {
	clusters := textGraphemeClusters(text)
	if len(clusters) == 0 {
		return nil
	}
	out := make([]HistoryCell, 0, len(clusters))
	for _, cluster := range clusters {
		width := textDisplayWidth(cluster)
		if width <= 0 {
			continue
		}
		out = append(out, HistoryCell{Text: cluster, Width: width})
	}
	return out
}

func normalizeHistoryLogicalLineCells(cells []HistoryCell, cols int) []HistoryCell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]HistoryCell, 0, len(cells))
	for _, cell := range cells {
		parts := splitHistoryCell(cell, cols)
		if len(parts) == 0 {
			continue
		}
		out = append(out, parts...)
	}
	return out
}

func splitHistoryCell(cell HistoryCell, cols int) []HistoryCell {
	width := HistoryCellDisplayWidth(cell)
	if width <= 0 {
		return nil
	}
	if cols <= 0 {
		cols = 80
	}
	if cell.Text == "" {
		return splitEmptyHistoryCellFootprint(cell, cols)
	}
	clusters := textGraphemeClusters(cell.Text)
	if len(clusters) == 0 {
		return nil
	}
	naturalWidth := textClusterDisplayColumns(clusters)[len(clusters)]
	if width <= cols && naturalWidth == width {
		return []HistoryCell{cell}
	}
	if width <= cols && naturalWidth != width {
		return []HistoryCell{cell}
	}
	if len(clusters) == 1 && naturalWidth >= width {
		next := cell
		next.Width = width
		return []HistoryCell{next}
	}
	return splitHistoryCellForWrap(cell)
}

func splitHistoryCellForWrap(cell HistoryCell) []HistoryCell {
	width := HistoryCellDisplayWidth(cell)
	if width <= 0 {
		return nil
	}
	if cell.Text == "" {
		return splitEmptyHistoryCellFootprint(cell, 1)
	}
	clusters := textGraphemeClusters(cell.Text)
	if len(clusters) == 0 {
		return nil
	}
	naturalWidth := textClusterDisplayColumns(clusters)[len(clusters)]
	if len(clusters) == 1 && naturalWidth == width {
		return []HistoryCell{cell}
	}
	out := make([]HistoryCell, 0, len(clusters)+1)
	for _, cluster := range clusters {
		next := cell
		next.Text = cluster
		next.Width = textDisplayWidth(cluster)
		if next.Width <= 0 {
			continue
		}
		out = append(out, next)
	}
	if pad := width - naturalWidth; pad > 0 {
		padding := cell
		padding.Text = strings.Repeat(" ", pad)
		padding.Width = pad
		padding.LinkURL = ""
		padding.LinkParams = ""
		out = append(out, padding)
	}
	return out
}

func splitEmptyHistoryCellFootprint(cell HistoryCell, cols int) []HistoryCell {
	width := HistoryCellDisplayWidth(cell)
	if width <= 0 {
		return nil
	}
	if cols <= 0 || cols > width {
		cols = width
	}
	out := make([]HistoryCell, 0, (width+cols-1)/cols)
	for remaining := width; remaining > 0; {
		partWidth := remaining
		if partWidth > cols {
			partWidth = cols
		}
		next := cell
		// 中文说明：空文本 terminal cell 仍有真实列宽，copy/history 需要把它投影成带样式空格。
		next.Text = strings.Repeat(" ", partWidth)
		next.Width = partWidth
		next.LinkURL = ""
		next.LinkParams = ""
		out = append(out, next)
		remaining -= partWidth
	}
	return out
}

func historyCellsPlainTextForState(cells []HistoryCell) string {
	var builder strings.Builder
	for _, cell := range cells {
		if cell.Text == "" {
			continue
		}
		builder.WriteString(cell.Text)
		if pad := HistoryCellDisplayWidth(cell) - textDisplayWidth(cell.Text); pad > 0 {
			builder.WriteString(strings.Repeat(" ", pad))
		}
	}
	return builder.String()
}

func cloneCopyMatches(matches []CopyMatch) []CopyMatch {
	if len(matches) == 0 {
		return nil
	}
	cloned := make([]CopyMatch, len(matches))
	copy(cloned, matches)
	return cloned
}

type historyLogicalOffset struct {
	lineID    uint64
	rowInLine int
	col       int
}

func reflowViewportTop(before HistoryStore, after HistoryStore, top int) int {
	if len(before.Rows) == 0 || len(after.Rows) == 0 {
		return 0
	}
	offset := historyLogicalOffsetForPosition(before, CopyPosition{Row: top, Col: 0})
	position := positionForHistoryLogicalOffset(after, offset)
	return clampCopyInt(position.Row, 0, maxCopyInt(0, len(after.Rows)-1))
}

func reflowCopyPosition(before HistoryStore, after HistoryStore, pos CopyPosition) CopyPosition {
	if len(after.Rows) == 0 {
		return CopyPosition{}
	}
	offset := historyLogicalOffsetForPosition(before, pos)
	offset.col += historyLogicalPrependedPrefixWidth(before, after, offset.lineID)
	return positionForHistoryLogicalOffset(after, offset)
}

func CopyLogicalPositionForPosition(history HistoryStore, pos CopyPosition) CopyLogicalPosition {
	offset := historyLogicalOffsetForPosition(history, pos)
	if offset.lineID == 0 {
		return CopyLogicalPosition{}
	}
	return CopyLogicalPosition{Valid: true, LineID: offset.lineID, Col: offset.col}
}

func copyLogicalPositionSame(left CopyLogicalPosition, right CopyLogicalPosition) bool {
	return left.Valid == right.Valid && left.LineID == right.LineID && left.Col == right.Col
}

func historyLogicalOffsetForPosition(history HistoryStore, pos CopyPosition) historyLogicalOffset {
	if len(history.Rows) == 0 {
		return historyLogicalOffset{}
	}
	row := clampCopyInt(pos.Row, 0, len(history.Rows)-1)
	current := history.Rows[row]
	col := clampCopyInt(pos.Col, 0, HistoryRowDisplayWidth(current))
	offset := col
	for cursor := row - 1; cursor >= 0; cursor-- {
		previous := history.Rows[cursor]
		if previous.LineID != current.LineID {
			break
		}
		offset += HistoryRowDisplayWidth(previous)
	}
	return historyLogicalOffset{
		lineID:    current.LineID,
		rowInLine: current.RowInLine,
		col:       offset,
	}
}

func positionForHistoryLogicalOffset(history HistoryStore, offset historyLogicalOffset) CopyPosition {
	if len(history.Rows) == 0 {
		return CopyPosition{}
	}
	if offset.lineID == 0 {
		return CopyPosition{}
	}
	remaining := maxCopyInt(0, offset.col)
	fallback := -1
	for rowIndex, row := range history.Rows {
		if row.LineID != offset.lineID {
			continue
		}
		if fallback < 0 {
			fallback = rowIndex
		}
		width := HistoryRowDisplayWidth(row)
		if remaining <= width {
			return CopyPosition{Row: rowIndex, Col: remaining}
		}
		remaining -= width
	}
	if fallback >= 0 {
		return CopyPosition{Row: fallback, Col: clampCopyInt(offset.col, 0, HistoryRowDisplayWidth(history.Rows[fallback]))}
	}
	last := len(history.Rows) - 1
	return CopyPosition{Row: last, Col: HistoryRowDisplayWidth(history.Rows[last])}
}

func reflowActiveMatchIndex(history HistoryStore, cursor CopyPosition, matches []CopyMatch) int {
	return reflowActiveMatchIndexFromMatches(cursor, matches)
}

func reflowActiveMatchIndexFromMatches(cursor CopyPosition, matches []CopyMatch) int {
	if len(matches) == 0 {
		return 0
	}
	best := 0
	for index, match := range matches {
		if match.StartRow == cursor.Row && match.StartCol == cursor.Col {
			return index
		}
		if copyMatchContainsPosition(match, cursor) {
			best = index
		}
	}
	return best
}

func copyMatchContainsPosition(match CopyMatch, pos CopyPosition) bool {
	if pos.Row < match.StartRow || pos.Row > match.EndRow {
		return false
	}
	if pos.Row == match.StartRow && pos.Col < match.StartCol {
		return false
	}
	if pos.Row == match.EndRow && pos.Col > match.EndCol {
		return false
	}
	return true
}

func historyLogicalPrependedPrefixWidth(before HistoryStore, after HistoryStore, lineID uint64) int {
	if lineID == 0 {
		return 0
	}
	beforeLine, ok := historyLogicalLineByID(before, lineID)
	if !ok || !beforeLine.ClippedBefore {
		return 0
	}
	afterLine, ok := historyLogicalLineByID(after, lineID)
	if !ok {
		return 0
	}
	return historyLogicalLinePrependedPrefixWidth(beforeLine, afterLine)
}

func historyLogicalLineByID(history HistoryStore, lineID uint64) (HistoryLogicalLine, bool) {
	lines := history.SourceLines
	if len(lines) == 0 && len(history.Rows) > 0 {
		lines = historyRowsToLogicalLines(history.Rows, history.Lines)
	}
	for _, line := range lines {
		if line.LineID == lineID {
			return line, true
		}
	}
	return HistoryLogicalLine{}, false
}

func historyLogicalLinePrependedPrefixWidth(before HistoryLogicalLine, after HistoryLogicalLine) int {
	if before.Text != "" && after.Text != "" && strings.HasSuffix(after.Text, before.Text) {
		prefix := strings.TrimSuffix(after.Text, before.Text)
		return textDisplayWidth(prefix)
	}
	if len(before.Cells) == 0 || len(after.Cells) == 0 {
		return 0
	}
	beforeIndex := len(before.Cells) - 1
	afterIndex := len(after.Cells) - 1
	for beforeIndex >= 0 && afterIndex >= 0 && historyCellsEqual(before.Cells[beforeIndex], after.Cells[afterIndex]) {
		beforeIndex--
		afterIndex--
	}
	width := 0
	for index := 0; index <= afterIndex; index++ {
		width += HistoryCellDisplayWidth(after.Cells[index])
	}
	return width
}

func historyCellsEqual(left HistoryCell, right HistoryCell) bool {
	return left == right
}

func rebaseExistingLineSpans(spans []HistoryLineSpan, delta int) []HistoryLineSpan {
	for i := range spans {
		spans[i].StartRow += delta
		spans[i].EndRow += delta
	}
	return spans
}

func appendRebasedHistoryLineSpans(out []HistoryLineSpan, spans []HistoryLineSpan, delta int) []HistoryLineSpan {
	for _, span := range spans {
		span.StartRow += delta
		span.EndRow += delta
		out = append(out, span)
	}
	return out
}

func clampCopyInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func maxCopyInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func copyVisibleHistoryRowsAfterPadding(visibleRows int, padding int) int {
	if visibleRows <= 0 {
		return 1
	}
	padding = clampCopyInt(padding, 0, visibleRows-1)
	return maxCopyInt(1, visibleRows-padding)
}

func copyVisibleRowsForStore(store CopyModeStore) int {
	if store.ViewRows > 0 {
		return maxCopyInt(1, store.ViewRows)
	}
	return 8
}
