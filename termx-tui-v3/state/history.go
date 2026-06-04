package state

import "errors"

// RequestID 是 TUI 本地请求 id，只用于关联 async response。
type RequestID uint64

// HistoryRequestKind 区分 latest replace 和 older prepend。
type HistoryRequestKind string

const (
	HistoryRequestLatest HistoryRequestKind = "latest"
	HistoryRequestOlder  HistoryRequestKind = "older"
)

// HistoryWindowOp 是 core-v2 authoritative window 合并方式。
type HistoryWindowOp string

const (
	HistoryWindowReplace HistoryWindowOp = "replace"
	HistoryWindowPrepend HistoryWindowOp = "prepend"
)

// HistoryCursor 是 older pagination 的 logical boundary。
type HistoryCursor struct {
	Valid           bool
	BeforeLineID    uint64
	BeforeRowInLine int
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
	TerminalID      string
	Cols            int
	Token           string
	Generation      uint64
	Cursor          HistoryCursor
	Boundary        HistoryBoundary
	BoundCopyModeID uint64
}

// HistoryRow 是 authoritative HistoryWindow 的 visual row 投影。
type HistoryRow struct {
	Text         string
	LineID       uint64
	RowInLine    int
	ClippedStart bool
	ClippedEnd   bool
}

// HistoryLineSpan 是 authoritative window 中 logical line 到 visual rows 的映射。
type HistoryLineSpan struct {
	LineID        uint64
	StartRow      int
	EndRow        int
	ClippedBefore bool
	ClippedAfter  bool
}

// HistoryWindow 是 state 层使用的 core-v2 authoritative window DTO。
type HistoryWindow struct {
	TerminalID   string
	Token        string
	Op           HistoryWindowOp
	Cols         int
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
	TerminalID string
	Token      string
	Cols       int
	Rows       []HistoryRow
	Lines      []HistoryLineSpan
	Cursor     HistoryCursor
	Generation uint64
	Boundary   HistoryBoundary
	HasMore    bool
	Exhausted  ExhaustedMarker
	Pending    *HistoryPendingRequest
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

// CopyModeStore 只保存 copy mode 交互态。
type CopyModeStore struct {
	Active      bool
	PaneID      string
	TerminalID  string
	ViewportTop int
	Cursor      CopyPosition
	Mark        *CopyPosition
	Selection   *CopySelection
	BoundToken  string
	BoundCols   int
	RequestID   RequestID
	Empty       bool
}

type CopyPosition struct {
	Row int
	Col int
}

type CopySelection struct {
	Anchor CopyPosition
	Focus  CopyPosition
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
		store = store.replace(window)
		return store, len(window.Rows), nil
	case HistoryWindowPrepend:
		inserted := len(window.Rows)
		if inserted == 0 && !window.HasMore {
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
		return store, inserted, nil
	default:
		return store, 0, ErrHistoryWindowMismatch
	}
}

func (store HistoryStore) InvalidateWindow() HistoryStore {
	store.Token = ""
	store.Cols = 0
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

func validateWindowAgainstPending(pending HistoryPendingRequest, window HistoryWindow) error {
	if pending.TerminalID != "" && pending.TerminalID != window.TerminalID {
		return ErrHistoryWindowMismatch
	}
	if pending.Cols != 0 && pending.Cols != window.Cols {
		return ErrHistoryWindowMismatch
	}
	switch pending.Kind {
	case HistoryRequestLatest:
		if window.Op != HistoryWindowReplace {
			return ErrHistoryWindowMismatch
		}
	case HistoryRequestOlder:
		if window.Op != HistoryWindowPrepend {
			return ErrHistoryWindowMismatch
		}
		if pending.Token != "" && pending.Token != window.Token {
			return ErrStaleHistoryResponse
		}
		if pending.Generation != 0 && pending.Generation != window.Generation {
			return ErrStaleHistoryResponse
		}
		if len(window.Rows) != 0 && pending.Boundary.LastLineID != 0 && pending.Boundary.LastLineID != window.Boundary.LastLineID {
			return ErrStaleHistoryResponse
		}
	default:
		return ErrHistoryWindowMismatch
	}
	return nil
}

func (store HistoryStore) replace(window HistoryWindow) HistoryStore {
	store.TerminalID = window.TerminalID
	store.Token = window.Token
	store.Cols = window.Cols
	store.Rows = cloneHistoryRows(window.Rows)
	store.Lines = cloneHistoryLineSpans(window.Lines)
	store.Cursor = window.Cursor
	store.Generation = window.Generation
	store.Boundary = window.Boundary
	store.HasMore = window.HasMore
	store.Exhausted = ExhaustedMarker{}
	return store
}

func (store HistoryStore) prepend(window HistoryWindow) HistoryStore {
	store.Rows = append(cloneHistoryRows(window.Rows), store.Rows...)
	store.Lines = append(cloneHistoryLineSpans(window.Lines), rebaseExistingLineSpans(cloneHistoryLineSpans(store.Lines), len(window.Rows))...)
	store.Token = window.Token
	store.Cursor = window.Cursor
	store.Generation = window.Generation
	store.Boundary.FirstLineID = window.Boundary.FirstLineID
	store.HasMore = window.HasMore
	store.Exhausted = ExhaustedMarker{}
	return store
}

func (store CopyModeStore) BindLatest(terminalID string, requestID RequestID, cols int) CopyModeStore {
	store.Active = true
	store.TerminalID = terminalID
	store.RequestID = requestID
	store.BoundCols = cols
	store.Empty = true
	return store
}

func (store CopyModeStore) AcceptLatest(window HistoryWindow) CopyModeStore {
	store.TerminalID = window.TerminalID
	store.BoundToken = window.Token
	store.BoundCols = window.Cols
	store.ViewportTop = 0
	store.Cursor = CopyPosition{}
	store.Empty = len(window.Rows) == 0
	return store
}

func (store CopyModeStore) AcceptOlder(insertedRows int, window HistoryWindow) CopyModeStore {
	if insertedRows > 0 {
		store.ViewportTop += insertedRows
	}
	store.BoundToken = window.Token
	store.BoundCols = window.Cols
	store.Empty = false
	return store
}

func (store CopyModeStore) Resize(cols int) CopyModeStore {
	store.BoundCols = cols
	store.BoundToken = ""
	store.ViewportTop = 0
	store.Cursor = CopyPosition{}
	store.Mark = nil
	store.Selection = nil
	store.Empty = true
	return store
}

func (store CopyModeStore) MoveCursor(pos CopyPosition) CopyModeStore {
	store.Cursor = pos
	if store.Mark != nil {
		selection := CopySelection{Anchor: *store.Mark, Focus: pos}
		store.Selection = &selection
	}
	return store
}

func (store CopyModeStore) SetMark(pos CopyPosition) CopyModeStore {
	store.Mark = &pos
	store.Selection = &CopySelection{Anchor: pos, Focus: pos}
	return store
}

func cloneHistoryRows(rows []HistoryRow) []HistoryRow {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([]HistoryRow, len(rows))
	copy(cloned, rows)
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

func rebaseExistingLineSpans(spans []HistoryLineSpan, delta int) []HistoryLineSpan {
	for i := range spans {
		spans[i].StartRow += delta
		spans[i].EndRow += delta
	}
	return spans
}
