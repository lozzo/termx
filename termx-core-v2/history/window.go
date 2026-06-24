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
	HistoryWindowAppend  HistoryWindowOp = "append"
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
	TailFill       *RowTailFill
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
	selected, hasMore := track.projectLatestTailRows(req.Cols, req.Rows)
	spans, visualRows, firstLine, lastLine := buildWindowRows(selected)
	cursor := latestTailCursor(selected, hasMore)
	return HistoryWindow{
		Token:       makeWindowToken(track.generation, req.Cols, firstLine, lastLine, cursor),
		Op:          HistoryWindowReplace,
		Cols:        req.Cols,
		Rows:        visualRows,
		Spans:       spans,
		Cursor:      cursor,
		HasMore:     hasMore,
		Generation:  track.generation,
		FirstLineID: firstLine,
		LastLineID:  lastLine,
		LoadedLines: len(spans),
		TotalRows:   len(selected),
		TotalLines:  track.committed.Len(),
	}, nil
}

func (track *HistoryTrack) OlderWindow(req HistoryWindowRequest) (HistoryWindow, error) {
	if err := validateWindowRequest(req); err != nil {
		return HistoryWindow{}, err
	}
	selected, cursor, hasMore, ok := track.projectOlderRowsBeforeCursor(req.Cols, req.Rows, req.Cursor)
	if !ok {
		return track.emptyWindow(HistoryWindowPrepend, req.Cols), nil
	}
	spans, visualRows, firstLine, lastLine := buildWindowRows(selected)
	return HistoryWindow{
		Token:       makeWindowToken(track.generation, req.Cols, firstLine, lastLine, cursor),
		Op:          HistoryWindowPrepend,
		Cols:        req.Cols,
		Rows:        visualRows,
		Spans:       spans,
		Cursor:      cursor,
		HasMore:     hasMore,
		Generation:  track.generation,
		FirstLineID: firstLine,
		LastLineID:  lastLine,
		LoadedLines: len(spans),
		TotalRows:   len(selected),
		TotalLines:  track.committed.Len(),
	}, nil
}

// CommittedCursorValid 返回 cursor 是否仍然命中当前 cols 下的 committed
// history projection boundary。它只校验 authoritative committed 投影，不依赖
// 当前 viewport rows，因此不会把纯高度变化误判成 stale。
func (track *HistoryTrack) CommittedCursorValid(cols int, cursor HistoryCursor) bool {
	if cols <= 0 || !cursor.Valid {
		return false
	}
	rows := track.projectCommittedRows(cols)
	if len(rows) == 0 {
		return false
	}
	return cursorBoundaryIndex(rows, cursor) >= 0
}

func (track *HistoryTrack) emptyWindow(op HistoryWindowOp, cols int) HistoryWindow {
	return HistoryWindow{
		Token:      makeWindowToken(track.generation, cols, 0, 0, HistoryCursor{}),
		Op:         op,
		Cols:       cols,
		Generation: track.generation,
		TotalLines: track.committed.Len(),
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
	ids = track.appendLatestHistoryFrontierIDs(ids)
	return track.projectRows(ids, cols)
}

func (track *HistoryTrack) projectLatestTailRows(cols int, maxRows int) ([]projectedRow, bool) {
	ids := track.latestLineIDs()
	rows := make([]projectedRow, 0, maxRows)
	hasMore := false
	for i := len(ids) - 1; i >= 0; i-- {
		lineRows, ok := track.projectLineRows(ids[i], cols)
		if !ok {
			continue
		}
		for rowIndex := len(lineRows) - 1; rowIndex >= 0; rowIndex-- {
			if len(rows) >= maxRows {
				hasMore = true
				break
			}
			rows = append(rows, lineRows[rowIndex])
		}
		if hasMore {
			break
		}
	}
	reverseProjectedRows(rows)
	return rows, hasMore
}

func (track *HistoryTrack) projectOlderRowsBeforeCursor(cols int, maxRows int, cursor HistoryCursor) ([]projectedRow, HistoryCursor, bool, bool) {
	if !cursor.Valid {
		return nil, HistoryCursor{}, false, false
	}
	ids := track.committed.IDs()
	rows := make([]projectedRow, 0, maxRows)
	hasMore := false
	startLineIndex, startRowIndex, ok := track.cursorStartPosition(ids, cols, cursor)
	if !ok {
		return nil, HistoryCursor{}, false, false
	}
	for lineIndex := startLineIndex; lineIndex >= 0; lineIndex-- {
		lineRows, ok := track.projectLineRows(ids[lineIndex], cols)
		if !ok {
			continue
		}
		rowIndex := len(lineRows) - 1
		if lineIndex == startLineIndex {
			rowIndex = startRowIndex
		}
		for ; rowIndex >= 0; rowIndex-- {
			if len(rows) >= maxRows {
				hasMore = true
				break
			}
			rows = append(rows, lineRows[rowIndex])
		}
		if hasMore {
			break
		}
	}
	reverseProjectedRows(rows)
	nextCursor := HistoryCursor{}
	if hasMore && len(rows) > 0 {
		nextCursor = cursorFromRow(rows[0])
	}
	return rows, nextCursor, hasMore, true
}

func latestTailCursor(rows []projectedRow, hasMore bool) HistoryCursor {
	if !hasMore || len(rows) == 0 {
		return HistoryCursor{}
	}
	for _, row := range rows {
		if row.committed {
			return cursorFromRow(row)
		}
	}
	// latest 只返回 mutable tail 时，older 的边界就是 committed history 的尾部。
	return HistoryCursor{Valid: true}
}

func (track *HistoryTrack) latestLineIDs() []LogicalLineID {
	ids := track.committed.IDs()
	return track.appendLatestHistoryFrontierIDs(ids)
}

func (track *HistoryTrack) appendLatestHistoryFrontierIDs(ids []LogicalLineID) []LogicalLineID {
	// 中文说明：primary fullscreen frame 的 frontier 是当前帧投影，可以在
	// latest/history 中可见；是否计入滚动历史只由 committed index 决定。
	for _, id := range track.frontier.IDs() {
		if !track.frontier.IsHidden(id) && !containsLineID(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

func (track *HistoryTrack) projectLineRows(id LogicalLineID, cols int) ([]projectedRow, bool) {
	line, ok := track.store.Line(id)
	if !ok {
		return nil, false
	}
	lineRows := projectLine(line, cols)
	projected := make([]projectedRow, len(lineRows))
	for i, row := range lineRows {
		projected[i] = projectedRow{
			row:          row,
			lineRowCount: len(lineRows),
			committed:    track.committed.Contains(id),
		}
	}
	return projected, true
}

func (track *HistoryTrack) cursorStartPosition(ids []LogicalLineID, cols int, cursor HistoryCursor) (int, int, bool) {
	if cursor.BeforeLineID == 0 {
		if len(ids) == 0 {
			return -1, -1, false
		}
		lineRows, ok := track.projectLineRows(ids[len(ids)-1], cols)
		if !ok || len(lineRows) == 0 {
			return len(ids) - 2, -1, true
		}
		return len(ids) - 1, len(lineRows) - 1, true
	}
	for lineIndex := len(ids) - 1; lineIndex >= 0; lineIndex-- {
		if ids[lineIndex] != cursor.BeforeLineID {
			continue
		}
		lineRows, ok := track.projectLineRows(ids[lineIndex], cols)
		if !ok {
			return -1, -1, false
		}
		for rowIndex, row := range lineRows {
			if row.row.RowInLine == cursor.BeforeRowInLine {
				return lineIndex, rowIndex - 1, true
			}
		}
		return -1, -1, false
	}
	return -1, -1, false
}

func reverseProjectedRows(rows []projectedRow) {
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}
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
	if line.TailFill != nil && len(rows) > 0 {
		rows[len(rows)-1].TailFill = cloneRowTailFill(line.TailFill)
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
	row := VisualRow{
		Text:           lineTextFromCells(cells),
		Cells:          cloneCells(cells),
		LineID:         line.ID,
		RowInLine:      rowIndex,
		LineGeneration: line.Generation,
	}
	return row
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
	width := cellWidth(cell)
	if width <= 0 {
		return nil
	}
	if cell.Text == "" {
		return blankFootprintCells(cell, width)
	}
	clusters := textClusters(cell.Text)
	if len(clusters) == 0 {
		return nil
	}
	naturalWidth := 0
	for _, cluster := range clusters {
		naturalWidth += textDisplayWidth(cluster)
	}
	capacity := len(clusters)
	if width > naturalWidth {
		capacity += width - naturalWidth
	}
	out := make([]Cell, 0, capacity)
	for _, cluster := range clusters {
		next := cell
		next.Text = cluster
		next.Width = textDisplayWidth(cluster)
		out = append(out, next)
	}
	if pad := width - naturalWidth; pad > 0 {
		padding := cell
		padding.Text = ""
		padding.Width = pad
		padding.LinkURL = ""
		padding.LinkParams = ""
		out = append(out, blankFootprintCells(padding, pad)...)
	}
	return out
}

func blankFootprintCells(cell Cell, width int) []Cell {
	if width <= 0 {
		return nil
	}
	out := make([]Cell, 0, width)
	for i := 0; i < width; i++ {
		next := cell
		// 中文说明：history payload 里只有 footprint 没有文本的格子，投影和 mutation 都必须按真实空格处理。
		next.Text = " "
		next.Width = 1
		next.LinkURL = ""
		next.LinkParams = ""
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
		if pad := cellWidth(cell) - textDisplayWidth(cell.Text); pad > 0 {
			builder.WriteString(strings.Repeat(" ", pad))
		}
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
