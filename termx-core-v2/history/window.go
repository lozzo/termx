package history

import (
	"errors"
	"fmt"
	"strings"
)

// HistoryWindowOp 表达 TUI 应如何合并 authoritative window。
type HistoryWindowOp string

const (
	HistoryWindowReplace HistoryWindowOp = "replace"
	HistoryWindowPrepend HistoryWindowOp = "prepend"
)

// WindowToken 绑定 HistoryTrack generation、cols 与 logical boundary，用于
// TUI stale response guard。
type WindowToken string

// HistoryCursor 是 older pagination 的 committed history 边界。
type HistoryCursor struct {
	Valid           bool
	BeforeLineID    LogicalLineID
	BeforeRowInLine int
}

// HistoryWindowRequest 是 HistoryTrack 投影请求。
type HistoryWindowRequest struct {
	Cols   int
	Rows   int
	Cursor HistoryCursor
}

// HistoryWindow 是 core-v2 输出给 TUI 和协议层的 authoritative projection。
type HistoryWindow struct {
	Token       WindowToken
	Op          HistoryWindowOp
	Cols        int
	Rows        []VisualRow
	Spans       []LogicalLineSpan
	Cursor      HistoryCursor
	HasMore     bool
	Generation  Generation
	FirstLineID LogicalLineID
	LastLineID  LogicalLineID
	LoadedLines int
	TotalLines  int
}

// VisualRow 是某个 logical line 在指定 cols 下的派生行，不是历史 truth。
type VisualRow struct {
	Text           string
	LineID         LogicalLineID
	RowInLine      int
	ClippedBefore  bool
	ClippedAfter   bool
	LineGeneration Generation
}

// LogicalLineSpan 记录 window 中某条 logical line 覆盖的 visual row 范围。
type LogicalLineSpan struct {
	LineID         LogicalLineID
	FirstRow       int
	LastRow        int
	ClippedBefore  bool
	ClippedAfter   bool
	LineGeneration Generation
}

var ErrInvalidWindowSize = errors.New("invalid history window size")

func (track *HistoryTrack) LatestWindow(req HistoryWindowRequest) (HistoryWindow, error) {
	if err := validateWindowRequest(req); err != nil {
		return HistoryWindow{}, err
	}
	rows := track.projectLatestRows(req.Cols)
	selectionStart := tailStart(len(rows), req.Rows)
	selected := rows[selectionStart:]
	spans, visualRows, firstLine, lastLine := buildWindowRows(selected)
	cursor := latestCursor(rows, selectionStart)
	return HistoryWindow{
		Token:       makeWindowToken(track.generation, req.Cols, firstLine, lastLine, cursor),
		Op:          HistoryWindowReplace,
		Cols:        req.Cols,
		Rows:        visualRows,
		Spans:       spans,
		Cursor:      cursor,
		HasMore:     cursor.Valid,
		Generation:  track.generation,
		FirstLineID: firstLine,
		LastLineID:  lastLine,
		LoadedLines: len(spans),
		TotalLines:  len(track.committed.IDs()),
	}, nil
}

func (track *HistoryTrack) OlderWindow(req HistoryWindowRequest) (HistoryWindow, error) {
	if err := validateWindowRequest(req); err != nil {
		return HistoryWindow{}, err
	}
	rows := track.projectCommittedRows(req.Cols)
	boundary := cursorBoundaryIndex(rows, req.Cursor)
	if boundary < 0 {
		return track.emptyWindow(HistoryWindowPrepend, req.Cols), nil
	}
	candidates := rows[:boundary]
	selectionStart := tailStart(len(candidates), req.Rows)
	selected := candidates[selectionStart:]
	spans, visualRows, firstLine, lastLine := buildWindowRows(selected)
	cursor := cursorBeforeSelectedRow(candidates, selectionStart)
	return HistoryWindow{
		Token:       makeWindowToken(track.generation, req.Cols, firstLine, lastLine, cursor),
		Op:          HistoryWindowPrepend,
		Cols:        req.Cols,
		Rows:        visualRows,
		Spans:       spans,
		Cursor:      cursor,
		HasMore:     cursor.Valid,
		Generation:  track.generation,
		FirstLineID: firstLine,
		LastLineID:  lastLine,
		LoadedLines: len(spans),
		TotalLines:  len(track.committed.IDs()),
	}, nil
}

func (track *HistoryTrack) emptyWindow(op HistoryWindowOp, cols int) HistoryWindow {
	return HistoryWindow{
		Token:      makeWindowToken(track.generation, cols, 0, 0, HistoryCursor{}),
		Op:         op,
		Cols:       cols,
		Generation: track.generation,
		TotalLines: len(track.committed.IDs()),
	}
}

func validateWindowRequest(req HistoryWindowRequest) error {
	if req.Cols <= 0 || req.Rows <= 0 {
		return ErrInvalidWindowSize
	}
	return nil
}

func (track *HistoryTrack) projectLatestRows(cols int) []projectedRow {
	ids := track.committed.IDs()
	for _, id := range track.frontier.IDs() {
		if !track.frontier.IsHidden(id) && !containsLineID(ids, id) {
			ids = append(ids, id)
		}
	}
	return track.projectRows(ids, cols)
}

func (track *HistoryTrack) projectCommittedRows(cols int) []projectedRow {
	return track.projectRows(track.committed.IDs(), cols)
}

func (track *HistoryTrack) projectRows(ids []LogicalLineID, cols int) []projectedRow {
	var rows []projectedRow
	for _, id := range ids {
		line, ok := track.store.Line(id)
		if !ok {
			continue
		}
		lineRows := projectLine(line, cols)
		for _, row := range lineRows {
			rows = append(rows, projectedRow{
				row:          row,
				lineRowCount: len(lineRows),
				committed:    track.committed.Contains(id),
			})
		}
	}
	return rows
}

type projectedRow struct {
	row          VisualRow
	lineRowCount int
	committed    bool
}

func tailStart(totalRows int, maxRows int) int {
	if totalRows <= maxRows {
		return 0
	}
	return totalRows - maxRows
}

func latestCursor(rows []projectedRow, selectionStart int) HistoryCursor {
	if len(rows) == 0 {
		return HistoryCursor{}
	}
	for i := selectionStart; i < len(rows); i++ {
		if !rows[i].committed {
			continue
		}
		if hasCommittedRowBefore(rows, i) {
			return cursorFromRow(rows[i])
		}
		return HistoryCursor{}
	}
	if hasAnyCommittedRow(rows) {
		return HistoryCursor{Valid: true}
	}
	return HistoryCursor{}
}

func cursorBeforeSelectedRow(rows []projectedRow, selectionStart int) HistoryCursor {
	if len(rows) == 0 || selectionStart <= 0 {
		return HistoryCursor{}
	}
	return cursorFromRow(rows[selectionStart])
}

func cursorFromRow(row projectedRow) HistoryCursor {
	return HistoryCursor{
		Valid:           true,
		BeforeLineID:    row.row.LineID,
		BeforeRowInLine: row.row.RowInLine,
	}
}

func cursorBoundaryIndex(rows []projectedRow, cursor HistoryCursor) int {
	if !cursor.Valid {
		return -1
	}
	if cursor.BeforeLineID == 0 {
		return len(rows)
	}
	for i, row := range rows {
		if row.row.LineID == cursor.BeforeLineID && row.row.RowInLine == cursor.BeforeRowInLine {
			return i
		}
	}
	return -1
}

func buildWindowRows(rows []projectedRow) ([]LogicalLineSpan, []VisualRow, LogicalLineID, LogicalLineID) {
	if len(rows) == 0 {
		return nil, nil, 0, 0
	}
	visualRows := make([]VisualRow, len(rows))
	spans := make([]LogicalLineSpan, 0)
	firstLine := rows[0].row.LineID
	lastLine := rows[len(rows)-1].row.LineID
	for i := 0; i < len(rows); {
		lineID := rows[i].row.LineID
		start := i
		end := i
		for end+1 < len(rows) && rows[end+1].row.LineID == lineID {
			end++
		}
		clippedBefore := rows[start].row.RowInLine > 0
		clippedAfter := rows[end].row.RowInLine < rows[end].lineRowCount-1
		for rowIndex := start; rowIndex <= end; rowIndex++ {
			row := rows[rowIndex].row
			row.ClippedBefore = clippedBefore
			row.ClippedAfter = clippedAfter
			visualRows[rowIndex] = row
		}
		spans = append(spans, LogicalLineSpan{
			LineID:         lineID,
			FirstRow:       start,
			LastRow:        end,
			ClippedBefore:  clippedBefore,
			ClippedAfter:   clippedAfter,
			LineGeneration: rows[start].row.LineGeneration,
		})
		i = end + 1
	}
	return spans, visualRows, firstLine, lastLine
}

func projectLine(line LogicalLine, cols int) []VisualRow {
	text := lineTextFromCells(line.Cells)
	runes := []rune(text)
	if len(runes) == 0 {
		return []VisualRow{{
			LineID:         line.ID,
			LineGeneration: line.Generation,
		}}
	}
	rows := make([]VisualRow, 0, (len(runes)+cols-1)/cols)
	for start := 0; start < len(runes); start += cols {
		end := start + cols
		if end > len(runes) {
			end = len(runes)
		}
		rows = append(rows, VisualRow{
			Text:           string(runes[start:end]),
			LineID:         line.ID,
			RowInLine:      len(rows),
			LineGeneration: line.Generation,
		})
	}
	return rows
}

func lineTextFromCells(cells []Cell) string {
	var builder strings.Builder
	for _, cell := range cells {
		builder.WriteString(cell.Text)
	}
	return builder.String()
}

func makeWindowToken(
	generation Generation,
	cols int,
	firstLine LogicalLineID,
	lastLine LogicalLineID,
	cursor HistoryCursor,
) WindowToken {
	return WindowToken(fmt.Sprintf(
		"g%d:c%d:f%d:l%d:cv%t:b%d:r%d",
		generation,
		cols,
		firstLine,
		lastLine,
		cursor.Valid,
		cursor.BeforeLineID,
		cursor.BeforeRowInLine,
	))
}

func hasCommittedRowBefore(rows []projectedRow, index int) bool {
	for i := 0; i < index; i++ {
		if rows[i].committed {
			return true
		}
	}
	return false
}

func hasAnyCommittedRow(rows []projectedRow) bool {
	for _, row := range rows {
		if row.committed {
			return true
		}
	}
	return false
}

func containsLineID(ids []LogicalLineID, needle LogicalLineID) bool {
	for _, id := range ids {
		if id == needle {
			return true
		}
	}
	return false
}
