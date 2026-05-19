package render

import (
	"fmt"
	"hash/fnv"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-shared/gridtrace"
	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type renderTraceRowSummary struct {
	text           string
	visibleText    string
	cells          int
	cols           int
	visibleCols    int
	styled         int
	visibleStyled  int
	wide           int
	trailingSpaces int
	styles         string
	hash           string
	visibleHash    string
}

func traceRenderProtocolRow(event string, rect workbench.Rect, targetY int, offsetX int, row []protocol.Cell) {
	if !gridtrace.Enabled() {
		return
	}
	summary := renderSummaryFromProtocolCells(row, rect, offsetX)
	if !renderTraceShouldLog(summary) {
		return
	}
	traceRenderLogSummary(event, summary, "target_y", targetY, "rect_x", rect.X, "rect_w", rect.W, "offset_x", offsetX)
}

func traceRenderVTermRow(event string, rect workbench.Rect, targetY int, offsetX int, row []localvterm.Cell) {
	if !gridtrace.Enabled() {
		return
	}
	summary := renderSummaryFromVTermCells(row, rect, offsetX)
	if !renderTraceShouldLog(summary) {
		return
	}
	traceRenderLogSummary(event, summary, "target_y", targetY, "rect_x", rect.X, "rect_w", rect.W, "offset_x", offsetX)
}

func traceRenderLogSummary(event string, summary renderTraceRowSummary, kv ...any) {
	args := append(kv,
		"cells", summary.cells,
		"cols", summary.cols,
		"visible_cols", summary.visibleCols,
		"styled", summary.styled,
		"visible_styled", summary.visibleStyled,
		"wide", summary.wide,
		"trailing_spaces", summary.trailingSpaces,
		"styles", summary.styles,
		"hash", summary.hash,
		"visible_hash", summary.visibleHash,
		"text", summary.text,
		"visible_text", summary.visibleText,
	)
	gridtrace.LogLimited(event, 2000, args...)
}

func traceRenderCanvasRow(event string, rowIndex int, row string) {
	if !gridtrace.Enabled() {
		return
	}
	plain := xansi.Strip(row)
	if !renderTraceTextShouldLog(plain) && !strings.ContainsAny(row, "█▀▄▌▐") {
		return
	}
	gridtrace.LogLimited(event, 3000,
		"row", rowIndex,
		"bytes", len(row),
		"plain_bytes", len(plain),
		"plain_cols", xansi.StringWidth(plain),
		"blocks", renderBlockGlyphCount(plain),
		"hash", renderTraceHash(row),
		"plain_hash", renderTraceHash(plain),
		"raw_text", gridtrace.Short(row, 220),
		"plain_text", gridtrace.Short(plain, 220),
	)
}

func renderTraceShouldLog(summary renderTraceRowSummary) bool {
	if summary.styled > 0 || summary.wide > 0 || summary.trailingSpaces > 0 || summary.cols != summary.visibleCols {
		return true
	}
	return renderTraceTextShouldLog(summary.text)
}

func renderTraceTextShouldLog(text string) bool {
	return strings.ContainsAny(text, "█▀▄▌▐") ||
		strings.Contains(text, "QR") ||
		strings.Contains(text, "TERMX") ||
		strings.Contains(text, "PROMPT") ||
		strings.Contains(text, "remote pair") ||
		strings.Contains(text, "uri:") ||
		strings.Contains(text, "expires_at") ||
		strings.Contains(text, "000100") ||
		strings.Contains(text, "001000")
}

func renderBlockGlyphCount(text string) int {
	count := 0
	for _, r := range text {
		switch r {
		case '█', '▀', '▄', '▌', '▐':
			count++
		}
	}
	return count
}

func renderSummaryFromProtocolCells(row []protocol.Cell, rect workbench.Rect, offsetX int) renderTraceRowSummary {
	summary := renderTraceRowSummary{cells: len(row)}
	styleSeen := map[string]bool{}
	var text strings.Builder
	var visible strings.Builder
	for col, index := 0, 0; index < len(row); index++ {
		cell := drawCellFromProtocolCell(row[index])
		if cell.Continuation {
			continue
		}
		if cell.Content == "" {
			cell.Content = " "
			cell.Width = 1
		}
		if cell.Width > 1 {
			summary.wide++
		}
		text.WriteString(cell.Content)
		summary.cols += maxInt(1, cell.Width)
		if cell.Style != (drawStyle{}) {
			summary.styled++
			styleSeen[renderDrawStyle(cell.Style)] = true
		}
		targetX := rect.X + offsetX + col
		if targetX >= rect.X && targetX+cell.Width <= rect.X+rect.W {
			visible.WriteString(cell.Content)
			summary.visibleCols += maxInt(1, cell.Width)
			if cell.Style != (drawStyle{}) {
				summary.visibleStyled++
			}
		}
		col += maxInt(1, cell.Width)
	}
	return renderFinalizeSummary(summary, text.String(), visible.String(), styleSeen)
}

func renderSummaryFromVTermCells(row []localvterm.Cell, rect workbench.Rect, offsetX int) renderTraceRowSummary {
	summary := renderTraceRowSummary{cells: len(row)}
	styleSeen := map[string]bool{}
	var text strings.Builder
	var visible strings.Builder
	for col, index := 0, 0; index < len(row); index++ {
		cell := drawCellFromVTermCell(row[index])
		if cell.Continuation {
			continue
		}
		if cell.Content == "" {
			cell.Content = " "
			cell.Width = 1
		}
		if cell.Width > 1 {
			summary.wide++
		}
		text.WriteString(cell.Content)
		summary.cols += maxInt(1, cell.Width)
		if cell.Style != (drawStyle{}) {
			summary.styled++
			styleSeen[renderDrawStyle(cell.Style)] = true
		}
		targetX := rect.X + offsetX + col
		if targetX >= rect.X && targetX+cell.Width <= rect.X+rect.W {
			visible.WriteString(cell.Content)
			summary.visibleCols += maxInt(1, cell.Width)
			if cell.Style != (drawStyle{}) {
				summary.visibleStyled++
			}
		}
		col += maxInt(1, cell.Width)
	}
	return renderFinalizeSummary(summary, text.String(), visible.String(), styleSeen)
}

func renderFinalizeSummary(summary renderTraceRowSummary, text, visible string, styleSeen map[string]bool) renderTraceRowSummary {
	summary.text = gridtrace.Short(text, 220)
	summary.visibleText = gridtrace.Short(visible, 220)
	summary.trailingSpaces = renderTrailingSpaces(text)
	summary.styles = renderJoinStyleKeys(styleSeen)
	summary.hash = renderTraceHash(text)
	summary.visibleHash = renderTraceHash(visible)
	return summary
}

func renderDrawStyle(style drawStyle) string {
	return fmt.Sprintf("fg:%s,bg:%s,b:%t,i:%t,u:%t,rv:%t", style.FG, style.BG, style.Bold, style.Italic, style.Underline, style.Reverse)
}

func renderJoinStyleKeys(values map[string]bool) string {
	if len(values) == 0 {
		return ""
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	return gridtrace.Short(strings.Join(out, "|"), 220)
}

func renderTrailingSpaces(text string) int {
	count := 0
	for i := len(text); i > 0; {
		r, size := renderLastRune(text[:i])
		if r != ' ' {
			break
		}
		count++
		i -= size
	}
	return count
}

func renderLastRune(text string) (rune, int) {
	for i := len(text) - 1; i >= 0; i-- {
		if text[i] < 0x80 || text[i]&0xc0 == 0xc0 {
			r := []rune(text[i:])
			if len(r) == 0 {
				return 0, 1
			}
			return r[0], len(text) - i
		}
	}
	return 0, 1
}

func renderTraceHash(text string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	return fmt.Sprintf("%016x", h.Sum64())
}
