package state

import (
	"errors"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
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

// HistoryTrimResult 描述一次 TUI 本地 history window 裁剪。
type HistoryTrimResult struct {
	DroppedRowsBefore  int
	DroppedRowsAfter   int
	DroppedLinesBefore int
	DroppedLinesAfter  int
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

type NewerRequestState string

const (
	NewerRequestReady   NewerRequestState = "ready"
	NewerRequestPending NewerRequestState = "pending"
	NewerRequestMissing NewerRequestState = "missing"
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
	store.Exhausted = ExhaustedMarker{}
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
	case HistoryRequestOldest:
		if window.Op != HistoryWindowReplace {
			return ErrHistoryWindowMismatch
		}
		if pending.Token != "" && pending.Token != window.Token {
			return ErrStaleHistoryResponse
		}
		if pending.Generation != 0 && pending.Generation != window.Generation {
			return ErrStaleHistoryResponse
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
		if len(window.Rows) != 0 && pending.Boundary.FirstLineID != 0 && pending.Boundary.FirstLineID != window.Boundary.FirstLineID {
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
	store.SourceLines = historyWindowSourceLinesOwned(window)
	if len(window.SourceLines) > 0 && cols == window.Cols && len(window.Rows) > 0 {
		store.Rows = cloneHistoryRows(window.Rows)
		store.Lines = cloneHistoryLineSpans(window.Lines)
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
	store.Cursor = window.Cursor
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
	rows := make([]HistoryRow, 0, len(olderRows)+len(existingRows))
	rows = append(rows, olderRows...)
	rows = append(rows, existingRows...)
	spans := make([]HistoryLineSpan, 0, len(olderSpans)+len(existingSpans))
	spans = append(spans, olderSpans...)
	if len(existingSpans) == 0 && len(existingRows) > 0 {
		existingSpans = historyLineSpansForRows(existingRows)
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
		existingSpans = historyLineSpansForRows(existingRows)
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
		// 中文说明：boundary overlap 表示同一 logical line 的两个 partial source
		// 在本地窗口边界拼接，合并后只保留外侧 clipped 标记。
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
		return cloneHistoryRows(window.Rows), cloneHistoryLineSpans(window.Lines)
	}
	return ReflowHistoryLogicalLines(lines, cols)
}

func historyWindowSourceLinesOwned(window HistoryWindow) []HistoryLogicalLine {
	if len(window.SourceLines) > 0 {
		return cloneHistoryLogicalLines(window.SourceLines)
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
	// 中文说明：copy/history 的本地 rows 是当前窗口投影，不是历史 truth。
	// 裁剪时重新分配 slice，让被丢弃窗口的 backing array 和 cell payload 可被 GC。
	source := cloneHistoryLogicalLines(store.SourceLines[startLine : endLine+1])
	store.SourceLines = source
	store.Rows, store.Lines = ReflowHistoryLogicalLines(source, store.Cols)
	if len(store.Rows) > 0 {
		store.Boundary.FirstLineID = store.Rows[0].LineID
		if boundaryLast == 0 {
			boundaryLast = store.Rows[len(store.Rows)-1].LineID
		}
		store.Boundary.LastLineID = boundaryLast
		// 中文说明：本地裁剪不能把 core projection cursor 重建成 row-in-line；
		// 必须优先使用 core 发来的 ProjectionRowIndex。
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

func (store CopyModeStore) MoveCursor(pos CopyPosition) CopyModeStore {
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

func (store CopyModeStore) SetMark(pos CopyPosition) CopyModeStore {
	store.Mark = &pos
	store.Selection = &CopySelection{Anchor: pos, Focus: pos}
	return store
}

// 复制选择必须保留 core 侧 logical 坐标，避免本地窗口裁剪后丢失真实选择范围。
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
	return textDisplayWidth(cell.Text)
}

func historyCellDisplayWidthSum(cells []HistoryCell) int {
	width := 0
	for _, cell := range cells {
		width += HistoryCellDisplayWidth(cell)
	}
	return width
}

// ReflowHistoryLogicalLines 把 core authoritative logical-line source 重排成本地 rows。
// 它只产生 TUI 投影，不能创建或修改历史 truth。
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
	}
	if len(cells) > 0 {
		cells = normalizeHistoryLogicalLineCells(cells, cols)
	}
	if len(cells) == 0 {
		return []HistoryRow{historyRowFromLogicalLine(line, 0, nil, "")}
	}
	rows := make([]HistoryRow, 0, 1)
	current := make([]HistoryCell, 0, len(cells))
	width := 0
	flush := func() {
		text := historyCellsPlainTextForState(current)
		rows = append(rows, historyRowFromLogicalLine(line, len(rows), cloneHistoryCells(current), text))
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
			// 中文说明：cell 放不进当前行剩余列时，按 grapheme/padding 切开；
			// 否则多列输出会提前换行，破坏 copy/history 的本地重排。
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

func historyRowFromLogicalLine(line HistoryLogicalLine, rowInLine int, cells []HistoryCell, text string) HistoryRow {
	if text == "" && len(cells) == 0 {
		text = line.Text
	}
	return HistoryRow{
		Text:               text,
		Cells:              cells,
		LineID:             line.LineID,
		RowInLine:          rowInLine,
		Kind:               line.Kind,
		Segment:            line.Segment,
		SessionID:          line.SessionID,
		FrameID:            line.FrameID,
		FixedGrid:          line.FixedGrid,
		ScreenCols:         line.ScreenCols,
		ProjectionRowIndex: line.ProjectionRowIndex,
		LiveTail:           line.LiveTail,
	}
}

func isFixedGridHistoryRowKind(kind string) bool {
	return kind == "screen-frame" || kind == "archived-screen-frame" || kind == "alt-screen-frame"
}

func fixedGridHistoryRows(line HistoryLogicalLine, cells []HistoryCell) []HistoryRow {
	if len(cells) == 0 && line.Text != "" {
		cells = []HistoryCell{{Text: line.Text, Width: textDisplayWidth(line.Text)}}
	}
	if len(cells) > 0 {
		cells = normalizeHistoryLogicalLineCells(cells, historyCellDisplayWidthSum(cells))
	}
	row := historyRowFromLogicalLine(line, 0, cloneHistoryCells(cells), historyCellsPlainTextForState(cells))
	row.FixedGrid = true
	if line.FixedGrid {
		row.FixedGrid = true
	}
	row.TailFill = cloneHistoryCellStyle(line.TailFill)
	return []HistoryRow{row}
}

func reflowPlainHistoryLogicalLine(line HistoryLogicalLine, cols int) []HistoryRow {
	if singleWidthASCIIText(line.Text) {
		return reflowPlainASCIIHistoryLogicalLine(line, cols)
	}
	clusters := textGraphemeClusters(line.Text)
	if len(clusters) == 0 {
		return []HistoryRow{historyRowFromLogicalLine(line, 0, nil, "")}
	}
	rows := make([]HistoryRow, 0, 1)
	var builder strings.Builder
	width := 0
	flush := func() {
		rows = append(rows, historyRowFromLogicalLine(line, len(rows), nil, builder.String()))
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
		return []HistoryRow{historyRowFromLogicalLine(line, 0, nil, "")}
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
		rows = append(rows, historyRowFromLogicalLine(line, len(rows), nil, line.Text[start:end]))
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

func normalizeHistoryLogicalLineCells(cells []HistoryCell, cols int) []HistoryCell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]HistoryCell, 0, len(cells))
	for _, cell := range cells {
		parts := splitHistoryCell(cell, cols)
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
		// 中文说明：空文本 terminal cell 仍有真实列宽，history reflow 要把它投影成带样式空格。
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

func textClusterDisplayColumns(clusters []string) []int {
	columns := make([]int, len(clusters)+1)
	for index, cluster := range clusters {
		columns[index+1] = columns[index] + textDisplayWidth(cluster)
	}
	return columns
}

type historyLogicalOffset struct {
	lineID uint64
	col    int
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

// 同一 logical line 可能被本地列宽重排成多行，需要累加同源 row 的显示宽度。
func historyLogicalOffsetForPosition(history HistoryStore, pos CopyPosition) historyLogicalOffset {
	if len(history.Rows) == 0 {
		return historyLogicalOffset{}
	}
	rowIndex := clampHistoryInt(pos.Row, 0, len(history.Rows)-1)
	current := history.Rows[rowIndex]
	if current.LineID == 0 {
		return historyLogicalOffset{}
	}
	col := clampHistoryInt(pos.Col, 0, HistoryRowDisplayWidth(current))
	offset := col
	for cursor := rowIndex - 1; cursor >= 0; cursor-- {
		previous := history.Rows[cursor]
		if !sameHistoryRowsSource(previous, current) {
			break
		}
		offset += HistoryRowDisplayWidth(previous)
	}
	return historyLogicalOffset{
		lineID: current.LineID,
		col:    offset,
	}
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

func appendRebasedHistoryLineSpans(out []HistoryLineSpan, spans []HistoryLineSpan, offset int) []HistoryLineSpan {
	for _, span := range spans {
		span.StartRow += offset
		span.EndRow += offset
		out = append(out, span)
	}
	return out
}

func historyLineSpansForRows(rows []HistoryRow) []HistoryLineSpan {
	if len(rows) == 0 {
		return nil
	}
	spans := make([]HistoryLineSpan, 0, len(rows))
	start := 0
	for index := 1; index < len(rows); index++ {
		if sameHistoryRowsSource(rows[start], rows[index]) {
			continue
		}
		spans = append(spans, historyLineSpanForLocalRows(rows, start, index-1))
		start = index
	}
	spans = append(spans, historyLineSpanForLocalRows(rows, start, len(rows)-1))
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
