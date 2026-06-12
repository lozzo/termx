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

// HistoryLogicalLine 是 copy/history 冻结快照里的单条 logical line payload。
// TUI 只把它当作 authoritative source，再按当前 pane 宽度本地重排成 Rows。
type HistoryLogicalLine struct {
	Text   string
	Cells  []HistoryCell
	LineID uint64
	// clipped 标记表达这条 frozen source 是否只是 authoritative logical line
	// 的局部片段；本地 reflow 时必须继续保留，不能把 partial 片段误当成完整行。
	ClippedBefore bool
	ClippedAfter  bool
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
	ViewID     string
	PaneID     string
	TerminalID string
	Token      string
	Cols       int
	SourceLines []HistoryLogicalLine
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
	// Start/End 使用 authoritative row + display cell column，允许同一 logical line
	// 的匹配跨越本地 reflow 产生的多个 visual row。
	StartRow int
	StartCol int
	EndRow   int
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
	default:
		return store, 0, ErrHistoryWindowMismatch
	}
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
	store.SourceLines = historyWindowSourceLines(window)
	store.Rows, store.Lines = ReflowHistoryLogicalLines(store.SourceLines, cols)
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
	store.SourceLines = mergePrependedHistoryLogicalLines(historyWindowSourceLines(window), existing)
	store.Rows, store.Lines = ReflowHistoryLogicalLines(store.SourceLines, store.Cols)
	store.Token = window.Token
	store.Cursor = window.Cursor
	store.Generation = window.Generation
	store.Boundary.FirstLineID = window.Boundary.FirstLineID
	store.HasMore = window.HasMore
	store.Exhausted = ExhaustedMarker{}
	return store
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
	if lastOlder.LineID != 0 &&
		lastOlder.LineID == firstExisting.LineID &&
		lastOlder.ClippedAfter &&
		firstExisting.ClippedBefore {
		lastOlder.Text += firstExisting.Text
		lastOlder.Cells = append(lastOlder.Cells, cloneHistoryCells(firstExisting.Cells)...)
		lastOlder.ClippedBefore = lastOlder.ClippedBefore || firstExisting.ClippedBefore
		lastOlder.ClippedAfter = lastOlder.ClippedAfter || firstExisting.ClippedAfter
		rest = rest[1:]
	}
	return append(merged, rest...)
}

func historyWindowSourceLines(window HistoryWindow) []HistoryLogicalLine {
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
	spansByLineID := make(map[uint64]HistoryLineSpan, len(spans))
	for _, span := range spans {
		if span.LineID == 0 {
			continue
		}
		spansByLineID[span.LineID] = span
	}
	lines := make([]HistoryLogicalLine, 0, len(rows))
	for _, row := range rows {
		if len(lines) > 0 && lines[len(lines)-1].LineID == row.LineID {
			lines[len(lines)-1].Text += row.Text
			lines[len(lines)-1].Cells = append(lines[len(lines)-1].Cells, cloneHistoryCells(row.Cells)...)
			continue
		}
		span, hasSpan := spansByLineID[row.LineID]
		lines = append(lines, HistoryLogicalLine{
			Text:          row.Text,
			Cells:         cloneHistoryCells(row.Cells),
			LineID:        row.LineID,
			ClippedBefore: hasSpan && span.ClippedBefore,
			ClippedAfter:  hasSpan && span.ClippedAfter,
		})
	}
	return lines
}

func (store HistoryStore) EnsureSourceLines() HistoryStore {
	if len(store.SourceLines) > 0 || len(store.Rows) == 0 {
		return store
	}
	store.SourceLines = historyRowsToLogicalLines(store.Rows, store.Lines)
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

func (store CopyModeStore) AcceptLatest(window HistoryWindow, cols int) CopyModeStore {
	store.PaneID = window.PaneID
	store.ViewID = window.ViewID
	store.TerminalID = window.TerminalID
	store.BoundToken = window.Token
	if cols <= 0 {
		cols = window.Cols
	}
	store.BoundCols = cols
	store.ViewportTop = 0
	store.Cursor = CopyPosition{}
	store.Matches = nil
	store.ActiveMatch = 0
	store.Empty = len(window.SourceLines) == 0
	return store
}

func (store CopyModeStore) AcceptOlder(insertedRows int, window HistoryWindow, cols int) CopyModeStore {
	if insertedRows > 0 {
		store.ViewportTop += insertedRows
		store.Cursor.Row += insertedRows
		if store.Mark != nil {
			mark := *store.Mark
			mark.Row += insertedRows
			store.Mark = &mark
		}
		if store.Selection != nil {
			store.Selection = &CopySelection{
				Anchor: CopyPosition{
					Row: store.Selection.Anchor.Row + insertedRows,
					Col: store.Selection.Anchor.Col,
				},
				Focus: CopyPosition{
					Row: store.Selection.Focus.Row + insertedRows,
					Col: store.Selection.Focus.Col,
				},
			}
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

func (store CopyModeStore) Resize(cols int, rows int) CopyModeStore {
	store.BoundCols = cols
	store.ViewRows = rows
	return store
}

// RebindToReflowedHistory 在 frozen source 按新 cols 本地重排后，把 copy mode
// 的交互态重新映射回同一段 logical-line 内容，而不是继续沿用旧 visual row/col。
func (store CopyModeStore) RebindToReflowedHistory(before HistoryStore, after HistoryStore) CopyModeStore {
	store.BoundCols = after.Cols
	if len(after.Rows) == 0 {
		store.ViewportTop = 0
		store.Cursor = CopyPosition{}
		store.Mark = nil
		store.Selection = nil
		store.Matches = nil
		store.ActiveMatch = 0
		store.Empty = true
		return store
	}
	store.Empty = false
	store.ViewportTop = reflowViewportTop(before, after, store.ViewportTop)
	store.Cursor = reflowCopyPosition(before, after, store.Cursor)
	if store.Mark != nil {
		mark := reflowCopyPosition(before, after, *store.Mark)
		store.Mark = &mark
	}
	if store.Selection != nil {
		store.Selection = &CopySelection{
			Anchor: reflowCopyPosition(before, after, store.Selection.Anchor),
			Focus:  reflowCopyPosition(before, after, store.Selection.Focus),
		}
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
		store.Cursor = CopyPosition{Row: store.Matches[0].StartRow, Col: store.Matches[0].StartCol}
	}
	return store
}

// RefreshQueryMatches 在 history 更新后重算 query 命中，但尽量保留当前正在看的
// active match / cursor；只有找不到原命中时，才退回到第一个 match。
func (store CopyModeStore) RefreshQueryMatches(matches []CopyMatch) CopyModeStore {
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
	current := history.Rows[0].LineID
	for row := 1; row < len(history.Rows); row++ {
		if history.Rows[row].LineID == current {
			continue
		}
		spans = append(spans, HistoryLineSpan{LineID: current, StartRow: start, EndRow: row - 1})
		start = row
		current = history.Rows[row].LineID
	}
	spans = append(spans, HistoryLineSpan{LineID: current, StartRow: start, EndRow: len(history.Rows) - 1})
	return spans
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
	store.Cursor = CopyPosition{Row: match.StartRow, Col: match.StartCol}
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

func cloneHistoryLogicalLines(lines []HistoryLogicalLine) []HistoryLogicalLine {
	if len(lines) == 0 {
		return nil
	}
	cloned := make([]HistoryLogicalLine, len(lines))
	copy(cloned, lines)
	for i := range cloned {
		cloned[i].Cells = cloneHistoryCells(lines[i].Cells)
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
			LineID:        line.LineID,
			StartRow:      start,
			EndRow:        end,
			ClippedBefore: line.ClippedBefore,
			ClippedAfter:  line.ClippedAfter,
		})
	}
	return rows, spans
}

func reflowHistoryLogicalLine(line HistoryLogicalLine, cols int) []HistoryRow {
	cells := cloneHistoryCells(line.Cells)
	if len(cells) == 0 && line.Text != "" {
		cells = splitHistoryLogicalLineText(line.Text)
	} else if len(cells) > 0 {
		cells = normalizeHistoryLogicalLineCells(cells)
	}
	if len(cells) == 0 {
		return []HistoryRow{{LineID: line.LineID}}
	}
	rows := make([]HistoryRow, 0, 1)
	current := make([]HistoryCell, 0, len(cells))
	width := 0
	flush := func() {
		row := HistoryRow{
			Text:      historyCellsPlainTextForState(current),
			Cells:     cloneHistoryCells(current),
			LineID:    line.LineID,
			RowInLine: len(rows),
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
		if width > 0 && width+cellWidth > cols {
			flush()
		}
		current = append(current, cell)
		width += cellWidth
		if width >= cols {
			flush()
		}
	}
	if len(current) > 0 || len(rows) == 0 {
		flush()
	}
	return rows
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

func normalizeHistoryLogicalLineCells(cells []HistoryCell) []HistoryCell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]HistoryCell, 0, len(cells))
	for _, cell := range cells {
		parts := splitHistoryCell(cell)
		if len(parts) == 0 {
			continue
		}
		out = append(out, parts...)
	}
	return out
}

func splitHistoryCell(cell HistoryCell) []HistoryCell {
	width := HistoryCellDisplayWidth(cell)
	if width <= 0 {
		return nil
	}
	clusters := textGraphemeClusters(cell.Text)
	if len(clusters) == 0 {
		return nil
	}
	naturalWidth := textClusterDisplayColumns(clusters)[len(clusters)]
	if len(clusters) == 1 && naturalWidth == width {
		return []HistoryCell{cell}
	}
	out := make([]HistoryCell, 0, len(clusters))
	for _, cluster := range clusters {
		next := cell
		next.Text = cluster
		next.Width = textDisplayWidth(cluster)
		if next.Width <= 0 {
			continue
		}
		out = append(out, next)
	}
	return out
}

func historyCellsPlainTextForState(cells []HistoryCell) string {
	var builder strings.Builder
	for _, cell := range cells {
		builder.WriteString(cell.Text)
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
	lineID   uint64
	rowInLine int
	col      int
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
	return positionForHistoryLogicalOffset(after, offset)
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
