package state

import (
	"errors"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

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
	PaneID          string
	ViewID          string
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
	Cells        []HistoryCell
	LineID       uint64
	RowInLine    int
	ClippedStart bool
	ClippedEnd   bool
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
	ViewID       string
	PaneID       string
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
	ViewID     string
	PaneID     string
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

// OlderRequestState 汇总 authoritative window 与 pending/exhausted guard 下的 older 请求状态。
type OlderRequestState string

const (
	OlderRequestReady     OlderRequestState = "ready"
	OlderRequestPending   OlderRequestState = "pending"
	OlderRequestExhausted OlderRequestState = "exhausted"
	OlderRequestMissing   OlderRequestState = "missing"
)

// CopyModeStore 只保存 copy mode 交互态。
type CopyModeStore struct {
	Active      bool
	PaneID      string
	ViewID      string
	TerminalID  string
	ViewportTop int
	ViewRows    int
	Cursor      CopyPosition
	Mark        *CopyPosition
	Selection   *CopySelection
	Query       string
	Matches     []CopyMatch
	ActiveMatch int
	BoundToken  string
	BoundCols   int
	RequestID   RequestID
	Empty       bool
}

type CopyPosition struct {
	Row int
	// Col 是 authoritative visual row 内的 display cell column，不是 rune index。
	Col int
}

type CopyMatch struct {
	Row int
	// StartCol/EndCol 使用 display cell column，和 CopyPosition.Col 保持一致。
	StartCol int
	EndCol   int
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
	store.ViewID = ""
	store.PaneID = ""
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

func (store HistoryStore) OlderRequestState() OlderRequestState {
	if store.Pending != nil {
		return OlderRequestPending
	}
	if store.Exhausted.Valid &&
		store.Exhausted.Token == store.Token &&
		store.Exhausted.Cols == store.Cols &&
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
	if pending.ViewID != "" && pending.ViewID != window.ViewID {
		return ErrStaleHistoryResponse
	}
	if pending.PaneID != "" && window.PaneID != "" && pending.PaneID != window.PaneID {
		return ErrStaleHistoryResponse
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
	store.ViewID = window.ViewID
	store.PaneID = window.PaneID
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

func (store CopyModeStore) BindLatest(paneID string, viewID string, terminalID string, requestID RequestID, cols int, rows int) CopyModeStore {
	store.Active = true
	store.PaneID = paneID
	store.ViewID = viewID
	store.TerminalID = terminalID
	store.RequestID = requestID
	store.BoundCols = cols
	store.ViewRows = rows
	store.Empty = true
	return store
}

func (store CopyModeStore) AcceptLatest(window HistoryWindow) CopyModeStore {
	store.PaneID = window.PaneID
	store.ViewID = window.ViewID
	store.TerminalID = window.TerminalID
	store.BoundToken = window.Token
	store.BoundCols = window.Cols
	store.ViewportTop = 0
	store.Cursor = CopyPosition{}
	store.Matches = nil
	store.ActiveMatch = 0
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

func (store CopyModeStore) Resize(cols int, rows int) CopyModeStore {
	store.BoundCols = cols
	store.ViewRows = rows
	store.BoundToken = ""
	store.ViewportTop = 0
	store.Cursor = CopyPosition{}
	store.Mark = nil
	store.Selection = nil
	store.Query = ""
	store.Matches = nil
	store.ActiveMatch = 0
	store.Empty = true
	return store
}

func (store CopyModeStore) SetViewRows(rows int) CopyModeStore {
	store.ViewRows = rows
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

func (store CopyModeStore) SetQuery(query string, matches []CopyMatch) CopyModeStore {
	store.Query = query
	store.Matches = cloneCopyMatches(matches)
	store.ActiveMatch = 0
	if len(store.Matches) > 0 {
		store.Cursor = CopyPosition{Row: store.Matches[0].Row, Col: store.Matches[0].StartCol}
	}
	return store
}

func FindCopyMatches(history HistoryStore, query string) []CopyMatch {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	matches := make([]CopyMatch, 0)
	queryClusters := textGraphemeClusters(query)
	for rowIndex, row := range history.Rows {
		textClusters := textGraphemeClusters(row.Text)
		displayColumns := HistoryRowGraphemeDisplayColumns(row)
		for start := 0; start+len(queryClusters) <= len(textClusters); start++ {
			if strings.Join(textClusters[start:start+len(queryClusters)], "") == query {
				matches = append(matches, CopyMatch{
					Row:      rowIndex,
					StartCol: displayColumns[start],
					EndCol:   displayColumns[start+len(queryClusters)],
				})
			}
		}
	}
	return matches
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
			builder.WriteString(textSliceDisplay(cell.Text, from-cursor, to-cursor))
		}
		cursor = next
	}
	return builder.String()
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
	store.Cursor = CopyPosition{Row: match.Row, Col: match.StartCol}
	return store
}

func (store CopyModeStore) Scroll(delta int, totalRows int) CopyModeStore {
	maxTop := maxCopyInt(0, totalRows-copyVisibleRowsForStore(store))
	store.ViewportTop = clampCopyInt(store.ViewportTop+delta, 0, maxTop)
	return store
}

func cloneHistoryRows(rows []HistoryRow) []HistoryRow {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([]HistoryRow, len(rows))
	copy(cloned, rows)
	for i := range cloned {
		cloned[i].Cells = cloneHistoryCells(rows[i].Cells)
	}
	return cloned
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

func cloneCopyMatches(matches []CopyMatch) []CopyMatch {
	if len(matches) == 0 {
		return nil
	}
	cloned := make([]CopyMatch, len(matches))
	copy(cloned, matches)
	return cloned
}

func rebaseExistingLineSpans(spans []HistoryLineSpan, delta int) []HistoryLineSpan {
	for i := range spans {
		spans[i].StartRow += delta
		spans[i].EndRow += delta
	}
	return spans
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

func copyVisibleRowsForStore(store CopyModeStore) int {
	if store.ViewRows > 2 {
		return maxCopyInt(1, store.ViewRows-2)
	}
	return 8
}
