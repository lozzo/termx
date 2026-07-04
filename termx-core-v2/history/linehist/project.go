package linehist

import (
	"strings"
	"time"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/termx-core-v2/history"
	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

// 本文件是 linehist 的查询投影层：把宽度无关的 logical line 按请求 cols
// 重新换行成 history.HistoryRow，并提供 window/cursor/boundary 的领域帮助函数。
// 旧 history 包的同名 helper 是未导出实现；R433 重做不修改旧包，这里按
// 相同对外语义（TUI/protocol 兼容）独立实现。

// cellsFromRuns 把 styled runs 展开成逐 grapheme 的 history cells。
// 宽度由 grapheme cluster 派生（与 vterm 写屏时的 GraphemeWidth 一致），
// 投影换行必须按 cell 宽度进行，不能按字节数。
func cellsFromRuns(runs []Run) []history.Cell {
	var out []history.Cell
	for _, run := range runs {
		text := run.Text
		for text != "" {
			cluster, width := xansi.FirstGraphemeCluster(text, xansi.GraphemeWidth)
			if cluster == "" {
				break
			}
			out = append(out, history.Cell{
				Text:       cluster,
				Width:      width,
				Style:      run.Style,
				LinkURL:    run.LinkURL,
				LinkParams: run.LinkParams,
			})
			text = text[len(cluster):]
		}
	}
	return out
}

// cellsFromVTermCells 把 emulator 当前屏的一行 cells 规整成 history cells。
// 宽字符 continuation 占位（无文本且 width<=0）跳过；纯 blank 占位展开成
// 空格，保持列宽语义，与 runsFromScrollOut 的落盘规整一致。
func cellsFromVTermCells(cells []vterm.Cell) []history.Cell {
	var out []history.Cell
	for _, cell := range cells {
		text := cell.Content
		width := cell.Width
		if text == "" {
			if width <= 0 {
				continue
			}
			text = strings.Repeat(" ", width)
		}
		if width <= 0 {
			width = 0
		}
		linkURL := cell.LinkURL
		linkParams := cell.LinkParams
		if linkURL == "" && linkParams == "" {
			linkURL = cell.Style.LinkURL
			linkParams = cell.Style.LinkParams
		}
		out = append(out, history.Cell{
			Text:       text,
			Width:      width,
			Style:      historyStyleFromVTerm(cell.Style),
			LinkURL:    linkURL,
			LinkParams: linkParams,
		})
	}
	return out
}

// wrapCells 把一条 logical line 的 cells 按 cols 贪心换行。
// 宽字符整 cell 换行（不劈开）；单 cell 超过 cols 时独占一行保证前进；
// 空行产出一个空 row，保持段落间距。
func wrapCells(cells []history.Cell, cols int) [][]history.Cell {
	if cols <= 0 {
		return [][]history.Cell{cells}
	}
	var rows [][]history.Cell
	var row []history.Cell
	width := 0
	for _, cell := range cells {
		w := cell.Width
		if w < 0 {
			w = 0
		}
		if len(row) > 0 && width+w > cols {
			rows = append(rows, row)
			row = nil
			width = 0
		}
		row = append(row, cell)
		width += w
	}
	rows = append(rows, row)
	return rows
}

// countWrappedRows 只统计 wrapCells 会产出的行数，不 materialize cells。
// 冷段 row 索引（prefix sum）构建走这里，避免整文件 cells 分配。
// 换行判定必须与 wrapCells 完全一致。
func countWrappedRows(runs []Run, cols int) int {
	if cols <= 0 {
		return 1
	}
	rows := 1
	width := 0
	cellsInRow := 0
	for _, run := range runs {
		text := run.Text
		for text != "" {
			cluster, w := xansi.FirstGraphemeCluster(text, xansi.GraphemeWidth)
			if cluster == "" {
				break
			}
			text = text[len(cluster):]
			if w < 0 {
				w = 0
			}
			if cellsInRow > 0 && width+w > cols {
				rows++
				width = 0
				cellsInRow = 0
			}
			width += w
			cellsInRow++
		}
	}
	return rows
}

// normalizedLimit 与旧 history window 合同一致：非法 limit 回退 100。
func normalizedLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	return limit
}

func cloneRows(rows []history.HistoryRow) []history.HistoryRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]history.HistoryRow, len(rows))
	for i, row := range rows {
		out[i] = row
		if len(row.Cells) > 0 {
			cells := make([]history.Cell, len(row.Cells))
			copy(cells, row.Cells)
			out[i].Cells = cells
		}
	}
	return out
}

// annotateProjectionRowIndexes 把绝对 row index 写回 page rows。
// TUI 裁剪本地窗口后用 ProjectionRowIndex 重建 older cursor，它必须是
// authoritative projection 中的稠密绝对行号。
func annotateProjectionRowIndexes(rows []history.HistoryRow, start int) {
	for index := range rows {
		rows[index].ProjectionRowIndex = start + index
	}
}

func boundaryForRows(rows []history.HistoryRow, generation history.Generation, token history.HistoryToken) history.HistoryBoundary {
	if len(rows) == 0 {
		return history.HistoryBoundary{Cursor: history.HistoryCursor{Generation: generation, Token: token}}
	}
	return history.HistoryBoundary{
		FirstLineID: rows[0].LineID,
		LastLineID:  rows[len(rows)-1].LineID,
		Cursor: history.HistoryCursor{
			Segment:    rows[0].Segment,
			SessionID:  rows[0].SessionID,
			FrameID:    rows[0].FrameID,
			LineID:     rows[0].LineID,
			Generation: generation,
			Token:      token,
			Valid:      true,
		},
	}
}

func cursorBeforeIndex(row history.HistoryRow, beforeIndex int, generation history.Generation, token history.HistoryToken, valid bool) history.HistoryCursor {
	return history.HistoryCursor{
		Segment:        row.Segment,
		SessionID:      row.SessionID,
		FrameID:        row.FrameID,
		LineID:         row.LineID,
		RowInLine:      row.RowInLine,
		BeforeRowIndex: beforeIndex,
		Generation:     generation,
		Token:          token,
		Valid:          valid,
	}
}

func spansForRows(rows []history.HistoryRow) []history.HistoryLineSpan {
	spans := make([]history.HistoryLineSpan, 0, len(rows))
	for i, row := range rows {
		spans = append(spans, history.HistoryLineSpan{
			StartRow:      i,
			EndRow:        i,
			Kind:          row.Kind,
			Segment:       row.Segment,
			LogicalLineID: row.LineID,
			SessionID:     row.SessionID,
			FrameID:       row.FrameID,
			ScreenRow:     row.ScreenRow,
			ScreenRowSet:  row.ScreenRowSet,
		})
	}
	return spans
}

func rowText(cells []history.Cell) string {
	var out strings.Builder
	for _, cell := range cells {
		out.WriteString(cell.Text)
	}
	return out.String()
}

func buildWindow(req history.HistoryWindowRequest, rows []history.HistoryRow, total int, op history.HistoryWindowOp, boundary history.HistoryBoundary, generation history.Generation, hasMore bool) history.HistoryWindow {
	if total < len(rows) {
		total = len(rows)
	}
	return history.HistoryWindow{
		TerminalID:   req.TerminalID,
		Token:        req.Token,
		Op:           op,
		Cols:         req.Cols,
		Rows:         cloneRows(rows),
		Lines:        spansForRows(rows),
		Generation:   generation,
		Boundary:     boundary,
		HasMore:      hasMore,
		LogicalTotal: total,
		Timestamp:    time.Now().UTC(),
	}
}

func clampInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
