package state

import (
	"errors"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

// RequestID 是 TUI 本地请求 id，只用于把 async response 关联回 reducer。
type RequestID uint64

// Valid 表示 request id 能否对应一个 in-flight 请求。
func (id RequestID) Valid() bool {
	return id != 0
}

type HistoryRequestKind string

const (
	HistoryRequestLatest HistoryRequestKind = "latest"
	HistoryRequestOlder  HistoryRequestKind = "older"
	HistoryRequestNewer  HistoryRequestKind = "newer"
	HistoryRequestOldest HistoryRequestKind = "oldest"
)

type HistoryWindowOp string

const (
	HistoryWindowReplace HistoryWindowOp = "replace"
	HistoryWindowPrepend HistoryWindowOp = "prepend"
	HistoryWindowAppend  HistoryWindowOp = "append"
)

const (
	HistoryCursorSegmentCommitted            = "committed"
	HistoryCursorSegmentCurrentPrimaryFrame  = "current-primary-frame"
	HistoryCursorSegmentArchivedPrimaryFrame = "archived-primary-frame"
	HistoryCursorSegmentCurrentAltFrame      = "current-alt-frame"
)

type HistoryCursor struct {
	Valid           bool
	BeforeLineID    uint64
	BeforeRowInLine int
	// BeforeRowIndex 来自 core authoritative projection，不能由本地 visual row 推断。
	BeforeRowIndex int
	Segment        string
}

type HistoryBoundary struct {
	FirstLineID uint64
	LastLineID  uint64
}

type HistoryPendingRequest struct {
	ID         RequestID
	Kind       HistoryRequestKind
	PaneID     string
	ViewID     string
	TerminalID string
	Cols       int
	Token      string
	Generation uint64
	Cursor     HistoryCursor
	Boundary   HistoryBoundary
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

type HistoryCell struct {
	Text       string
	Width      int
	Style      HistoryCellStyle
	LinkURL    string
	LinkParams string
}

type HistoryRow struct {
	Text               string
	Cells              []HistoryCell
	TailFill           *HistoryCellStyle
	LineID             uint64
	RowInLine          int
	Kind               string
	Segment            string
	SessionID          uint64
	FrameID            uint64
	FixedGrid          bool
	ScreenCols         int
	ProjectionRowIndex int
	ClippedStart       bool
	ClippedEnd         bool
	LiveTail           bool
}

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
	ClippedBefore      bool
	ClippedAfter       bool
}

// HistoryWindow 是 state 层的 core-v2 authoritative window DTO。
type HistoryWindow struct {
	PaneID       string
	ViewID       string
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

// HistoryStore 只保存已接纳的 authoritative history window 和 pending guard。
// 历史 truth 仍在 core-v2；这里不能从 live surface 或 snapshot 合成历史。
type HistoryStore struct {
	PaneID      string
	ViewID      string
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

type ExhaustedMarker struct {
	Valid     bool
	RequestID RequestID
	Token     string
	Cols      int
	Cursor    HistoryCursor
	Boundary  HistoryBoundary
}

type OlderRequestState string

const (
	OlderRequestReady     OlderRequestState = "ready"
	OlderRequestPending   OlderRequestState = "pending"
	OlderRequestExhausted OlderRequestState = "exhausted"
	OlderRequestMissing   OlderRequestState = "missing"
)

type CopyPosition struct {
	Row int
	Col int
}

type CopyLogicalPosition struct {
	Valid  bool
	LineID uint64
	Col    int
}

type CopySelection struct {
	Anchor        CopyPosition
	Focus         CopyPosition
	LogicalAnchor CopyLogicalPosition
	LogicalFocus  CopyLogicalPosition
}

type CopyModeStore struct {
	Active              bool
	Entering            bool
	PaneID              string
	ViewID              string
	TerminalID          string
	EnteringLive        *LiveSurfaceSnapshot
	EnteringScrollDelta int
	ViewportTop         int
	ViewRows            int
	Cursor              CopyPosition
	Mark                *CopyPosition
	Selection           *CopySelection
	BoundToken          string
	BoundCols           int
	RequestID           RequestID
	Empty               bool
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
		store = store.replace(window, pending.Cols)
		return store, len(window.Rows), nil
	case HistoryWindowPrepend:
		beforeRows := len(store.Rows)
		if len(window.Rows) == 0 && !window.HasMore {
			store.Exhausted = ExhaustedMarker{Valid: true, RequestID: requestID, Token: pending.Token, Cols: pending.Cols, Cursor: pending.Cursor, Boundary: pending.Boundary}
			return store, 0, nil
		}
		store = store.prepend(window)
		inserted := len(store.Rows) - beforeRows
		if inserted < 0 {
			inserted = 0
		}
		return store, inserted, nil
	default:
		return store, 0, ErrHistoryWindowMismatch
	}
}

func (store HistoryStore) InvalidateWindow() HistoryStore {
	store.PaneID = ""
	store.ViewID = ""
	store.TerminalID = ""
	store.Token = ""
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

func validateWindowAgainstPending(pending HistoryPendingRequest, window HistoryWindow) error {
	if pending.TerminalID != "" && pending.TerminalID != window.TerminalID {
		return ErrHistoryWindowMismatch
	}
	if pending.ViewID != "" && window.ViewID != "" && pending.ViewID != window.ViewID {
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
	default:
		return ErrHistoryWindowMismatch
	}
	return nil
}

func (store HistoryStore) replace(window HistoryWindow, cols int) HistoryStore {
	store.PaneID = window.PaneID
	store.ViewID = window.ViewID
	store.TerminalID = window.TerminalID
	store.Token = window.Token
	if cols <= 0 {
		cols = window.Cols
	}
	store.Cols = cols
	store.SourceLines = cloneHistoryLogicalLines(window.SourceLines)
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
	store.SourceLines = append(cloneHistoryLogicalLines(window.SourceLines), cloneHistoryLogicalLines(store.SourceLines)...)
	store.Rows = append(cloneHistoryRows(window.Rows), cloneHistoryRows(store.Rows)...)
	inserted := len(window.Rows)
	store.Lines = append(cloneHistoryLineSpans(window.Lines), rebaseHistoryLineSpans(store.Lines, inserted)...)
	store.Token = window.Token
	store.Cursor = window.Cursor
	store.Generation = window.Generation
	if window.Boundary.FirstLineID != 0 {
		store.Boundary.FirstLineID = window.Boundary.FirstLineID
	}
	if store.Boundary.LastLineID == 0 {
		store.Boundary.LastLineID = window.Boundary.LastLineID
	}
	store.HasMore = window.HasMore
	store.Exhausted = ExhaustedMarker{}
	return store
}

func (store CopyModeStore) BindLatest(paneID string, viewID string, terminalID string, requestID RequestID, cols int, rows int, enteringLive LiveSurfaceSnapshot) CopyModeStore {
	store.Active = false
	store.Entering = true
	store.PaneID = paneID
	store.ViewID = viewID
	store.TerminalID = terminalID
	store.EnteringLive = cloneLiveSurfaceSnapshotPtr(enteringLive)
	store.EnteringScrollDelta = 0
	store.RequestID = requestID
	store.BoundCols = cols
	store.ViewRows = rows
	store.Empty = true
	return store
}

func (store CopyModeStore) AcceptLatest(window HistoryWindow, cols int, totalRows int) CopyModeStore {
	store.Active = true
	store.Entering = false
	store.EnteringLive = nil
	store.PaneID = window.PaneID
	store.ViewID = window.ViewID
	store.TerminalID = window.TerminalID
	store.BoundToken = window.Token
	if cols <= 0 {
		cols = window.Cols
	}
	store.BoundCols = cols
	if totalRows <= 0 {
		totalRows = len(window.Rows)
	}
	if totalRows > 0 {
		visible := copyVisibleRowsForStore(store)
		store.Cursor = CopyPosition{Row: totalRows - 1}
		store.ViewportTop = maxHistoryInt(0, totalRows-visible)
	} else {
		store.Cursor = CopyPosition{}
		store.ViewportTop = 0
	}
	store.Empty = totalRows == 0
	return store
}

func (store CopyModeStore) AcceptOlder(insertedRows int, _ HistoryStore, _ HistoryStore, window HistoryWindow, cols int) CopyModeStore {
	if insertedRows > 0 {
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
	}
	store.BoundToken = window.Token
	if cols <= 0 {
		cols = window.Cols
	}
	store.BoundCols = cols
	store.Empty = false
	return store
}

func (store CopyModeStore) Scroll(delta int, totalRows int) CopyModeStore {
	maxTop := maxHistoryInt(0, totalRows-copyVisibleRowsForStore(store))
	store.ViewportTop = clampHistoryInt(store.ViewportTop+delta, 0, maxTop)
	return store
}

func HistoryRowDisplayWidth(row HistoryRow) int {
	if len(row.Cells) == 0 {
		return xansi.StringWidth(strings.ReplaceAll(row.Text, "\n", " "))
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
	return xansi.StringWidth(strings.ReplaceAll(cell.Text, "\n", " "))
}

func cloneHistoryLogicalLines(values []HistoryLogicalLine) []HistoryLogicalLine {
	if len(values) == 0 {
		return nil
	}
	out := make([]HistoryLogicalLine, len(values))
	for i, value := range values {
		out[i] = value
		out[i].Cells = cloneHistoryCells(value.Cells)
		out[i].TailFill = cloneHistoryCellStyle(value.TailFill)
	}
	return out
}

func cloneHistoryRows(values []HistoryRow) []HistoryRow {
	if len(values) == 0 {
		return nil
	}
	out := make([]HistoryRow, len(values))
	for i, value := range values {
		out[i] = value
		out[i].Cells = cloneHistoryCells(value.Cells)
		out[i].TailFill = cloneHistoryCellStyle(value.TailFill)
	}
	return out
}

func cloneHistoryCells(values []HistoryCell) []HistoryCell {
	if len(values) == 0 {
		return nil
	}
	out := make([]HistoryCell, len(values))
	copy(out, values)
	return out
}

func cloneHistoryCellStyle(value *HistoryCellStyle) *HistoryCellStyle {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneHistoryLineSpans(values []HistoryLineSpan) []HistoryLineSpan {
	if len(values) == 0 {
		return nil
	}
	out := make([]HistoryLineSpan, len(values))
	copy(out, values)
	return out
}

func rebaseHistoryLineSpans(values []HistoryLineSpan, offset int) []HistoryLineSpan {
	if len(values) == 0 {
		return nil
	}
	out := cloneHistoryLineSpans(values)
	for i := range out {
		out[i].StartRow += offset
		out[i].EndRow += offset
	}
	return out
}

func copyVisibleRowsForStore(store CopyModeStore) int {
	if store.ViewRows <= 0 {
		return 1
	}
	return store.ViewRows
}

func clampHistoryInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func maxHistoryInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
