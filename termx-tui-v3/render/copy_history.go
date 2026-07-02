package render

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

type copySelectionRange struct {
	active bool
	start  state.CopyPosition
	end    state.CopyPosition
}

type copySearchRange struct {
	active bool
	match  state.CopyMatch
}

var copyHistorySelectionANSIStyle = ANSICellStyle{FG: "ansi:8", BG: "ansi:3"}

func copyHistoryLines(history state.HistoryStore, copyMode state.CopyModeStore) []Line {
	if len(history.Rows) == 0 {
		return nil
	}
	selection := normalizedCopySelection(copyMode)
	rows := copyVisibleRows(history, copyMode)
	width := copyHistoryRenderWidth(history, copyMode)
	topPadding := copyHistoryTopPadding(history, copyMode, rows)
	lines := make([]Line, 0, topPadding+len(rows))
	for i := 0; i < topPadding; i++ {
		lines = append(lines, Line{})
	}
	for _, visible := range rows {
		lines = append(lines, copyHistoryLine(history.Rows[visible], visible, selection, activeSearchRange(copyMode, visible), width))
	}
	return lines
}

func copyHistoryLine(row state.HistoryRow, rowIndex int, selection copySelectionRange, search copySearchRange, width int) Line {
	cells := copyHistoryRowCells(row, rowIndex, selection, search)
	cells = copyHistoryApplySelectionFill(cells, rowIndex, selection, width)
	cells = copyHistoryApplyTailFill(cells, row, width)
	if row.ClippedEnd {
		cells = append(cells, styledCell(" ⇣", StyleMuted))
	}
	return Line{Cells: cells}
}

func CopyHistoryContentANSILine(history state.HistoryStore, copyMode state.CopyModeStore, rowIndex int, width int, theme Theme) string {
	return CopyHistoryContentANSILineAt(history, copyMode, rowIndex, width, 0, theme)
}

func CopyHistoryContentANSILineAt(history state.HistoryStore, copyMode state.CopyModeStore, rowIndex int, width int, lineX int, theme Theme) string {
	if width <= 0 {
		return ""
	}
	if rowIndex < 0 || rowIndex >= len(history.Rows) {
		return contentViewportBlankRun(width).Text
	}
	baseColumn := lineX + 1
	line := copyHistoryLine(history.Rows[rowIndex], rowIndex, normalizedCopySelection(copyMode), activeSearchRange(copyMode, rowIndex), width)
	return ensureANSIReset(contentViewportFitLine(line, width).ansiString(theme.WithFallback(), baseColumn))
}

func copyHistorySearchLine(history state.HistoryStore, copyMode state.CopyModeStore, totalRows int) Line {
	query := copyMode.Query
	if query == "" {
		return Line{Cells: []Cell{
			styledCell("⌕ search ", StyleMuted),
			styledCell("[/ query]", StyleMuted),
			NewCell(" "),
			styledCell(fmt.Sprintf(" rows:%d ", totalRows), StyleMuted),
			copyHistoryOlderToken(history),
		}}
	}
	return Line{Cells: []Cell{
		styledCell("⌕ search ", StyleMuted),
		styledCell(query, StyleAccent),
		NewCell(" "),
		styledCell(fmt.Sprintf(" match:%d/%d ", activeCopyMatchOrdinal(copyMode), len(copyMode.Matches)), StyleMuted),
		copyHistoryOlderToken(history),
	}}
}

// clipped-start 只表达窗口边界，不再给普通 logical line 起始/续行加工程 marker。
func copyHistoryPrefixCells(row state.HistoryRow) []Cell {
	if row.ClippedStart {
		return []Cell{styledCell("⇡ ", StyleMuted)}
	}
	return nil
}

func copyHistoryTextCells(text string, row int, selection copySelectionRange, search copySearchRange) []Cell {
	if text == "" {
		return []Cell{NewCell("")}
	}
	if !selection.active && !search.active {
		return []Cell{NewCell(text)}
	}
	width := DisplayWidth(text)
	segments := make([]Cell, 0, 5)
	cursor := 0
	for cursor < width {
		nextBreak, style := copyHistoryNextStyleBreak(row, cursor, width, selection, search)
		if nextBreak <= cursor {
			nextBreak = cursor + 1
		}
		textPart := SliceCells(text, cursor, nextBreak)
		if textPart == "" {
			cursor = nextBreak
			continue
		}
		if style != "" {
			segments = append(segments, copyHistoryHighlightCell(textPart, style, "", ""))
		} else {
			segments = append(segments, NewCell(textPart))
		}
		cursor = nextBreak
	}
	if len(segments) == 0 {
		return []Cell{NewCell(text)}
	}
	return segments
}

func copyHistoryNextStyleBreak(row int, cursor int, lineWidth int, selection copySelectionRange, search copySearchRange) (int, StyleToken) {
	nextBreak := lineWidth
	style := StyleToken("")
	if selection.active && row >= selection.start.Row && row <= selection.end.Row {
		from, to := selectionColumnsForRow(selection, row, lineWidth)
		if cursor < from {
			nextBreak = minInt(nextBreak, from)
		} else if cursor < to {
			nextBreak = minInt(nextBreak, to)
			style = StyleAccent
		}
	}
	if search.active {
		from, to, ok := searchColumnsForRow(search.match, row, lineWidth)
		if !ok {
			return nextBreak, style
		}
		if cursor < from {
			nextBreak = minInt(nextBreak, from)
		} else if cursor < to {
			nextBreak = minInt(nextBreak, to)
			if style == "" {
				style = StyleWarning
			}
		}
	}
	return nextBreak, style
}

func copyHistoryStyledTextCells(text string, width int, base ANSICellStyle, linkURL string, linkParams string, row int, from int, selection copySelectionRange, search copySearchRange) []Cell {
	if width <= 0 {
		width = DisplayWidth(text)
	}
	if width <= 0 {
		return nil
	}
	segments := make([]Cell, 0, 3)
	globalCursor := from
	globalEnd := from + width
	for globalCursor < globalEnd {
		nextBreak, style := copyHistoryNextCellStyleBreak(row, globalCursor, globalEnd, maxInt(globalEnd, selectionLineWidth(selection, row)), selection, search)
		if nextBreak <= globalCursor {
			nextBreak = globalCursor + 1
		}
		part := copyHistorySlicePaddedCellText(text, width, globalCursor-from, nextBreak-from)
		if part == "" {
			globalCursor = nextBreak
			continue
		}
		renderWidth := DisplayWidth(part)
		if renderWidth <= 0 {
			renderWidth = len([]rune(part))
		}
		if style != "" {
			segments = append(segments, copyHistoryHighlightCell(part, style, linkURL, linkParams))
		} else {
			segments = append(segments, Cell{Text: SafeLine(part), Width: renderWidth, ANSIStyle: base, LinkURL: linkURL, LinkParams: linkParams, TerminalContent: true, Safe: true})
		}
		globalCursor = nextBreak
	}
	return segments
}

func copyHistoryHighlightCell(text string, style StyleToken, linkURL string, linkParams string) Cell {
	cell := Cell{Text: SafeLine(text), Width: DisplayWidth(text), LinkURL: linkURL, LinkParams: linkParams, TerminalContent: true, Safe: true}
	if style == StyleAccent {
		cell.ANSIStyle = copyHistorySelectionANSIStyle
		return cell
	}
	cell.Style = style
	return cell
}

func copyHistorySlicePaddedCellText(text string, width int, from int, to int) string {
	from = clampCopyColumn(from, 0, width)
	to = clampCopyColumn(to, from, width)
	if to <= from {
		return ""
	}
	textWidth := DisplayWidth(text)
	part := ""
	if from < textWidth {
		part = SliceCells(text, from, minInt(to, textWidth))
	}
	if pad := to - maxInt(from, textWidth); pad > 0 {
		part += strings.Repeat(" ", pad)
	}
	return part
}

func copyHistoryNextCellStyleBreak(row int, cursor int, cellEnd int, lineWidth int, selection copySelectionRange, search copySearchRange) (int, StyleToken) {
	nextBreak := cellEnd
	style := StyleToken("")
	if selection.active && row >= selection.start.Row && row <= selection.end.Row {
		from, to := selectionColumnsForRow(selection, row, lineWidth)
		if cursor < from {
			nextBreak = minInt(nextBreak, from)
		} else if cursor < to {
			nextBreak = minInt(nextBreak, to)
			style = StyleAccent
		}
	}
	if search.active {
		from, to, ok := searchColumnsForRow(search.match, row, lineWidth)
		if !ok {
			if nextBreak > cellEnd {
				nextBreak = cellEnd
			}
			return nextBreak, style
		}
		if cursor < from {
			nextBreak = minInt(nextBreak, from)
		} else if cursor < to {
			nextBreak = minInt(nextBreak, to)
			if style == "" {
				style = StyleWarning
			}
		}
	}
	if nextBreak > cellEnd {
		nextBreak = cellEnd
	}
	return nextBreak, style
}

func selectionLineWidth(selection copySelectionRange, row int) int {
	if !selection.active || row < selection.start.Row || row > selection.end.Row {
		return 0
	}
	width := 0
	if row == selection.start.Row {
		width = maxInt(width, selection.start.Col)
	}
	if row == selection.end.Row {
		width = maxInt(width, selection.end.Col)
	}
	return width
}

func copyHistoryRowCells(row state.HistoryRow, rowIndex int, selection copySelectionRange, search copySearchRange) []Cell {
	prefix := copyHistoryPrefixCells(row)
	if len(row.Cells) == 0 {
		return append(prefix, copyHistoryTextCells(row.Text, rowIndex, selection, search)...)
	}
	out := make([]Cell, 0, len(prefix)+len(row.Cells))
	out = append(out, prefix...)
	cursor := 0
	for _, historyCell := range row.Cells {
		cellWidth := state.HistoryCellDisplayWidth(historyCell)
		out = append(out, renderCellsFromHistory(historyCell, rowIndex, cursor, selection, search)...)
		cursor += cellWidth
	}
	if len(out) == 0 {
		return []Cell{NewCell(row.Text)}
	}
	return out
}

func copyHistoryRenderWidth(history state.HistoryStore, copyMode state.CopyModeStore) int {
	if copyMode.BoundCols > 0 {
		return copyMode.BoundCols
	}
	return history.Cols
}

func copyHistoryApplySelectionFill(cells []Cell, row int, selection copySelectionRange, width int) []Cell {
	if !selection.active || width <= 0 || row < selection.start.Row || row > selection.end.Row {
		return cells
	}
	lineWidth := copyHistoryCellsWidth(cells)
	from, to := selectionColumnsForRow(selection, row, width)
	if to <= lineWidth {
		return cells
	}
	out := append([]Cell(nil), cells...)
	if from > lineWidth {
		out = append(out, NewCell(strings.Repeat(" ", from-lineWidth)))
		lineWidth = from
	}
	// 中文说明：这里补的是 copy mode 选区的显示背景，不能进入 SelectedText。
	out = append(out, copyHistorySelectionBlankCell(to-lineWidth))
	return out
}

func copyHistoryApplyTailFill(cells []Cell, row state.HistoryRow, width int) []Cell {
	if row.TailFill == nil || width <= 0 {
		return cells
	}
	lineWidth := copyHistoryCellsWidth(cells)
	padWidth := width - lineWidth
	if padWidth <= 0 {
		return cells
	}
	style := ansiStyleFromHistory(*row.TailFill)
	if style == (ANSICellStyle{}) {
		return cells
	}
	out := make([]Cell, 0, len(cells)+1)
	out = append(out, cells...)
	// 中文说明：TailFill 是当前 visual row 的行尾背景，不是 logical text；
	// 这里只做 display-only 填充，copy/search/reflow 不消费这段空白。
	out = append(out, Cell{
		Text:            strings.Repeat(" ", padWidth),
		Width:           padWidth,
		ANSIStyle:       style,
		TerminalContent: true,
		Safe:            true,
	})
	return out
}

func copyHistorySelectionBlankCell(width int) Cell {
	if width <= 0 {
		return NewCell("")
	}
	return Cell{
		Text:            strings.Repeat(" ", width),
		Width:           width,
		ANSIStyle:       copyHistorySelectionANSIStyle,
		TerminalContent: true,
		Safe:            true,
	}
}

func copyHistoryCellsWidth(cells []Cell) int {
	width := 0
	for _, cell := range cells {
		width += maxInt(0, cell.Width)
	}
	return width
}

func renderCellsFromHistory(cell state.HistoryCell, row int, from int, selection copySelectionRange, search copySearchRange) []Cell {
	text := SafeLine(cell.Text)
	width := cell.Width
	if width <= 0 {
		width = DisplayWidth(text)
	}
	if width <= 0 {
		width = len([]rune(text))
	}
	if selection.active || search.active {
		return copyHistoryStyledTextCells(text, width, ansiStyleFromHistory(cell.Style), cell.LinkURL, cell.LinkParams, row, from, selection, search)
	}
	if copyHistoryDefaultBlankCell(cell) {
		// 中文说明：default blank 是 terminal 的无内容背景，不是历史内容自身的显式样式；
		// copy/history 展示时交给 viewport 背景承载，避免整行空白被画成黑块。
		return []Cell{NewCell(strings.Repeat(" ", width))}
	}
	return []Cell{{
		Text:            text,
		Width:           width,
		ANSIStyle:       ansiStyleFromHistory(cell.Style),
		LinkURL:         cell.LinkURL,
		LinkParams:      cell.LinkParams,
		TerminalContent: true,
		Safe:            true,
	}}
}

func copyHistoryDefaultBlankCell(cell state.HistoryCell) bool {
	return cell.LinkURL == "" &&
		cell.LinkParams == "" &&
		cell.Style == (state.HistoryCellStyle{}) &&
		strings.TrimSpace(cell.Text) == ""
}

func ansiStyleFromHistory(style state.HistoryCellStyle) ANSICellStyle {
	return ANSICellStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
}

func selectionColumnsForRow(selection copySelectionRange, row int, lineLen int) (int, int) {
	from := 0
	to := lineLen
	if row == selection.start.Row {
		from = clampCopyColumn(selection.start.Col, 0, lineLen)
	}
	if row == selection.end.Row {
		to = clampCopyColumn(selection.end.Col, 0, lineLen)
	}
	if from > to {
		from, to = to, from
	}
	return from, to
}

func normalizedCopySelection(copyMode state.CopyModeStore) copySelectionRange {
	if copyMode.Selection == nil {
		return copySelectionRange{}
	}
	start := copyMode.Selection.Anchor
	end := copyMode.Selection.Focus
	if copyPositionAfter(start, end) {
		start, end = end, start
	}
	return copySelectionRange{active: true, start: start, end: end}
}

func copyHistoryCursor(history state.HistoryStore, copyMode state.CopyModeStore) Cursor {
	if !copyMode.Active || len(history.Rows) == 0 {
		return Cursor{}
	}
	row := clampCopyColumn(copyMode.Cursor.Row, 0, len(history.Rows)-1)
	visibleTop := copyHistoryViewportTop(history, copyMode)
	visibleRows := copyVisibleRows(history, copyMode)
	topPadding := copyHistoryTopPadding(history, copyMode, visibleRows)
	visibleRow := row - visibleTop
	if visibleRow < 0 {
		visibleRow = 0
	}
	if len(visibleRows) > 0 && visibleRow >= len(visibleRows) {
		visibleRow = len(visibleRows) - 1
	}
	col := clampCopyColumn(copyMode.Cursor.Col, 0, state.HistoryRowDisplayWidth(history.Rows[row]))
	return Cursor{
		Visible: true,
		Row:     topPadding + visibleRow,
		Col:     copyHistoryPrefixWidth(history.Rows[row]) + col,
		Shape:   CursorShapeBlock,
	}
}

func copyHistoryStatus(history state.HistoryStore, copyMode state.CopyModeStore) string {
	if copyMode.Empty {
		return "copy: empty"
	}
	if len(history.Rows) == 0 {
		return "copy"
	}
	row := clampCopyColumn(copyMode.Cursor.Row, 0, len(history.Rows)-1)
	historyRow := history.Rows[row]
	span := copyLineSpanForRow(history, row)
	visible := minInt(copyHistoryVisibleHeight(copyMode), len(history.Rows))
	status := fmt.Sprintf("copy row %d/%d line:%d part:%d cols:%d", row+1, len(history.Rows), historyRow.LineID, historyRow.RowInLine+1, history.Cols)
	if span.LineID != 0 {
		status += fmt.Sprintf(" span:%d-%d", span.StartRow+1, span.EndRow+1)
	}
	if copyMode.PhaseKind() == state.CopyModeMaterializedProjection {
		status += " projection:materialized"
		if copyMode.Materialized.CatchupPending {
			status += " catchup:pending"
		}
	}
	if copyMode.Query != "" {
		status += fmt.Sprintf(" search:%q %d/%d", copyMode.Query, activeCopyMatchOrdinal(copyMode), len(copyMode.Matches))
	} else {
		status += " search:/"
	}
	status += " " + copyHistoryOlderLabel(history)
	status += " " + copyHistoryBottomLabel(history, copyMode, visible)
	status += " " + copyHistoryBoundarySummary(history, copyMode)
	return status
}

func copyHistoryHitRegions(history state.HistoryStore, copyMode state.CopyModeStore) []HitRegion {
	rows := copyVisibleRows(history, copyMode)
	topPadding := copyHistoryTopPadding(history, copyMode, rows)
	regions := make([]HitRegion, len(rows))
	for i, rowIndex := range rows {
		row := history.Rows[rowIndex]
		prefixWidth := copyHistoryPrefixWidth(row)
		rowWidth := state.HistoryRowDisplayWidth(row)
		if rowWidth == 0 {
			rowWidth = 1
		}
		regions[i] = HitRegion{
			Kind:   HitRegionHistoryRow,
			Rect:   Rect{X: prefixWidth, Y: topPadding + i, W: rowWidth, H: 1},
			LineID: row.LineID,
			Row:    rowIndex,
		}
	}
	return regions
}

func copyVisibleRows(history state.HistoryStore, copyMode state.CopyModeStore) []int {
	if len(history.Rows) == 0 {
		return nil
	}
	top := copyHistoryViewportTop(history, copyMode)
	height := copyHistoryVisibleHeight(copyMode)
	if padding := copyHistoryTopPaddingForViewport(history, copyMode, top); padding > 0 && padding < height {
		height -= padding
	}
	if height <= 0 || top+height > len(history.Rows) {
		height = len(history.Rows) - top
	}
	rows := make([]int, 0, height)
	for i := 0; i < height; i++ {
		rows = append(rows, top+i)
	}
	return rows
}

func copyHistoryTopPadding(history state.HistoryStore, copyMode state.CopyModeStore, visibleRows []int) int {
	if len(visibleRows) == 0 || copyMode.Query != "" || copyMode.ViewRows <= 0 {
		return 0
	}
	top := visibleRows[0]
	return copyHistoryTopPaddingForViewport(history, copyMode, top)
}

func copyHistoryTopPaddingForViewport(history state.HistoryStore, copyMode state.CopyModeStore, top int) int {
	if copyMode.Query != "" || copyMode.ViewRows <= 0 {
		return 0
	}
	if top != copyHistoryViewportTop(history, copyMode) || top < 0 || top >= len(history.Rows) {
		return 0
	}
	row := history.Rows[top]
	if !row.FixedGrid || row.Kind != state.HistoryRowKindScreenFrame || row.Segment != state.HistoryCursorSegmentCurrentPrimaryFrame || !row.ScreenRowSet || row.ScreenRow <= 0 {
		return 0
	}
	// 中文说明：这是 display projection 的 current-frame 屏幕锚点，truth source
	// 是 core history window 的 ScreenRow；TUI 不能用 live snapshot 反推该值。
	if previousCurrentFrameRow(history.Rows, top, row) {
		return 0
	}
	if row.ScreenRow >= copyMode.ViewRows {
		return 0
	}
	return row.ScreenRow
}

func previousCurrentFrameRow(rows []state.HistoryRow, top int, row state.HistoryRow) bool {
	for index := top - 1; index >= 0; index-- {
		previous := rows[index]
		if previous.FrameID != row.FrameID || previous.SessionID != row.SessionID || previous.Segment != row.Segment || previous.Kind != row.Kind {
			return false
		}
		return true
	}
	return false
}

func copyHistoryVisibleHeight(copyMode state.CopyModeStore) int {
	if copyMode.ViewRows > 0 {
		return maxInt(1, copyMode.ViewRows)
	}
	return 8
}

func copyHistoryViewportTop(history state.HistoryStore, copyMode state.CopyModeStore) int {
	return clampCopyColumn(copyMode.ViewportTop, 0, maxInt(0, len(history.Rows)-1))
}

func activeSearchRange(copyMode state.CopyModeStore, row int) copySearchRange {
	if copyMode.Query == "" || len(copyMode.Matches) == 0 {
		return copySearchRange{}
	}
	index := clampCopyColumn(copyMode.ActiveMatch, 0, len(copyMode.Matches)-1)
	match := copyMode.Matches[index]
	if row < match.StartRow || row > match.EndRow {
		return copySearchRange{}
	}
	return copySearchRange{active: true, match: match}
}

func searchColumnsForRow(match state.CopyMatch, row int, lineLen int) (int, int, bool) {
	if row < match.StartRow || row > match.EndRow {
		return 0, 0, false
	}
	from := 0
	to := lineLen
	if row == match.StartRow {
		from = clampCopyColumn(match.StartCol, 0, lineLen)
	}
	if row == match.EndRow {
		to = clampCopyColumn(match.EndCol, from, lineLen)
	}
	if from > to {
		from, to = to, from
	}
	return from, to, true
}

func copyHistoryScrollbarLine(history state.HistoryStore, copyMode state.CopyModeStore, visible int) Line {
	total := len(history.Rows)
	top := copyHistoryViewportTop(history, copyMode)
	thumb := "█"
	if total > 0 && visible < total {
		ratio := float64(top) / float64(maxInt(1, total-visible))
		switch {
		case ratio <= 0:
			thumb = "▁"
		case ratio >= 1:
			thumb = "▔"
		default:
			thumb = "█"
		}
	}
	return Line{Cells: []Cell{
		styledCell("SCROLL ", StyleMuted),
		styledCell(thumb, StyleAccent),
		NewCell(" "),
		styledCell(fmt.Sprintf("%d-%d/%d", top+1, minInt(total, top+visible), total), StyleMuted),
		NewCell(" "),
		copyHistoryBottomToken(history, copyMode, visible),
		NewCell(" "),
		styledCell(copyHistoryBoundarySummary(history, copyMode), StyleMuted),
	}}
}

func copyHistoryOlderToken(history state.HistoryStore) Cell {
	return styledCell(copyHistoryOlderLabel(history), copyHistoryOlderStyle(history))
}

func copyHistoryOlderLabel(history state.HistoryStore) string {
	switch history.OlderRequestState() {
	case state.OlderRequestPending:
		return "older:loading"
	case state.OlderRequestExhausted:
		return "older:top"
	case state.OlderRequestReady:
		if !history.HasMore {
			return "older:ready"
		}
		return "older:more"
	default:
		return "older:top"
	}
}

func copyHistoryOlderStyle(history state.HistoryStore) StyleToken {
	if history.OlderRequestState() == state.OlderRequestPending {
		return StyleWarning
	}
	if history.OlderRequestState() == state.OlderRequestReady {
		return StyleAccent
	}
	return StyleMuted
}

func copyHistoryBottomToken(history state.HistoryStore, copyMode state.CopyModeStore, visible int) Cell {
	return styledCell(copyHistoryBottomLabel(history, copyMode, visible), copyHistoryBottomStyle(history, copyMode, visible))
}

func copyHistoryBottomLabel(history state.HistoryStore, copyMode state.CopyModeStore, visible int) string {
	total := len(history.Rows)
	top := copyHistoryViewportTop(history, copyMode)
	if total == 0 || top+visible >= total {
		return "latest"
	}
	return "loaded"
}

func copyHistoryBottomStyle(history state.HistoryStore, copyMode state.CopyModeStore, visible int) StyleToken {
	if copyHistoryBottomLabel(history, copyMode, visible) == "loaded" {
		return StyleAccent
	}
	return StyleMuted
}

func copyHistoryPrefixWidth(row state.HistoryRow) int {
	width := 0
	for _, cell := range copyHistoryPrefixCells(row) {
		width += cell.Width
	}
	return width
}

func copyHistoryBoundarySummary(history state.HistoryStore, copyMode state.CopyModeStore) string {
	first := history.Boundary.FirstLineID
	last := history.Boundary.LastLineID
	if first == 0 && len(history.Rows) > 0 {
		first = history.Rows[0].LineID
	}
	if last == 0 && len(history.Rows) > 0 {
		last = history.Rows[len(history.Rows)-1].LineID
	}
	if first == 0 && last == 0 {
		return "lines:-"
	}
	top := copyHistoryViewportTop(history, copyMode)
	visible := copyHistoryVisibleHeight(copyMode)
	from := top + 1
	to := minInt(len(history.Rows), top+visible)
	return fmt.Sprintf("lines:%d-%d view:%d-%d", first, last, from, to)
}

func copyLineSpanForRow(history state.HistoryStore, row int) state.HistoryLineSpan {
	for _, span := range history.Lines {
		if row >= span.StartRow && row <= span.EndRow {
			return span
		}
	}
	return state.HistoryLineSpan{}
}

func activeCopyMatchOrdinal(copyMode state.CopyModeStore) int {
	if len(copyMode.Matches) == 0 {
		return 0
	}
	return clampCopyColumn(copyMode.ActiveMatch, 0, len(copyMode.Matches)-1) + 1
}

func styledCell(text string, style StyleToken) Cell {
	return Cell{Text: SafeLine(text), Width: DisplayWidth(text), Style: style, Safe: true}
}

func copyPositionAfter(left state.CopyPosition, right state.CopyPosition) bool {
	if left.Row != right.Row {
		return left.Row > right.Row
	}
	return left.Col > right.Col
}

func clampCopyColumn(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
