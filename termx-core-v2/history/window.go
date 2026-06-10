package history

import (
	"errors"
	"fmt"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
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
	TotalRows   int
	TotalLines  int
}

// VisualRow 是某个 logical line 在指定 cols 下的派生行，不是历史 truth。
type VisualRow struct {
	Text           string
	Cells          []Cell
	LineID         LogicalLineID
	RowInLine      int
	Committed      bool
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
		TotalRows:   len(rows),
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
		TotalRows:   len(rows),
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
			row.Committed = rows[rowIndex].committed
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
	cells := normalizeProjectionCells(line.Cells, cols)
	if len(cells) == 0 {
		return []VisualRow{{
			LineID:         line.ID,
			LineGeneration: line.Generation,
		}}
	}
	rows := make([]VisualRow, 0)
	var rowCells []Cell
	rowWidth := 0
	for _, cell := range cells {
		rows, rowCells, rowWidth = appendCellToVisualRows(line, cell, cols, rows, rowCells, rowWidth)
	}
	if len(rowCells) > 0 {
		rows = append(rows, visualRowFromCells(line, len(rows), rowCells))
	}
	return rows
}

func appendCellToVisualRows(
	line LogicalLine,
	cell Cell,
	cols int,
	rows []VisualRow,
	rowCells []Cell,
	rowWidth int,
) ([]VisualRow, []Cell, int) {
	flush := func() {
		if len(rowCells) == 0 {
			return
		}
		rows = append(rows, visualRowFromCells(line, len(rows), rowCells))
		rowCells = nil
		rowWidth = 0
	}
	width := cellWidth(cell)
	if width <= 0 {
		return rows, rowCells, rowWidth
	}
	if rowWidth+width <= cols {
		rowCells = append(rowCells, cell)
		rowWidth += width
		if rowWidth >= cols {
			flush()
		}
		return rows, rowCells, rowWidth
	}
	for _, part := range splitMeasuredCell(cell) {
		partWidth := cellWidth(part)
		if partWidth <= 0 {
			continue
		}
		if rowWidth > 0 && rowWidth+partWidth > cols {
			flush()
		}
		if partWidth > cols {
			part.Width = cols
			partWidth = cols
		}
		rowCells = append(rowCells, part)
		rowWidth += partWidth
		if rowWidth >= cols {
			flush()
		}
	}
	return rows, rowCells, rowWidth
}

func visualRowFromCells(line LogicalLine, rowIndex int, cells []Cell) VisualRow {
	return VisualRow{
		Text:           lineTextFromCells(cells),
		Cells:          cloneCells(cells),
		LineID:         line.ID,
		RowInLine:      rowIndex,
		LineGeneration: line.Generation,
	}
}

func normalizeProjectionCells(cells []Cell, _ int) []Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]Cell, 0, len(cells))
	for _, cell := range cells {
		if cell.Text == "" && cell.Width <= 0 {
			continue
		}
		if cell.Width <= 0 {
			out = append(out, expandUnmeasuredCell(cell)...)
			continue
		}
		out = append(out, cell)
	}
	return out
}

func splitMeasuredCell(cell Cell) []Cell {
	clusters := textClusters(cell.Text)
	if len(clusters) == 0 {
		return nil
	}
	out := make([]Cell, 0, len(clusters))
	for _, cluster := range clusters {
		next := cell
		next.Text = cluster
		next.Width = textDisplayWidth(cluster)
		out = append(out, next)
	}
	return out
}

func expandUnmeasuredCell(cell Cell) []Cell {
	clusters := textClusters(cell.Text)
	if len(clusters) == 0 {
		return nil
	}
	out := make([]Cell, 0, len(clusters))
	for _, cluster := range clusters {
		next := cell
		next.Text = cluster
		next.Width = textDisplayWidth(cluster)
		out = append(out, next)
	}
	return out
}

func cellWidth(cell Cell) int {
	if cell.Width > 0 {
		return cell.Width
	}
	return len([]rune(cell.Text))
}

func textClusters(text string) []string {
	if text == "" {
		return nil
	}
	graphemes := uniseg.NewGraphemes(text)
	var clusters []string
	for graphemes.Next() {
		clusters = append(clusters, graphemes.Str())
	}
	return clusters
}

func textDisplayWidth(text string) int {
	width := xansi.StringWidth(text)
	if width <= 0 && text != "" {
		width = 1
	}
	return width
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
