package vt

import uv "github.com/charmbracelet/ultraviolet"

type screenDamageRecorder struct {
	damages       []Damage
	spanCells     []uv.Cell
	tailSpan      *SpanDamage
	tailSpanStart int
	tailSpanEnd   int
}

func (r *screenDamageRecorder) record(d Damage) {
	if r == nil || d == nil {
		return
	}
	if span, ok := d.(SpanDamage); ok {
		r.recordSpan(span)
		return
	}
	r.tailSpan = nil
	r.damages = append(r.damages, d)
}

func (r *screenDamageRecorder) recordSpanCell(x, y int, cell uv.Cell) {
	if r == nil {
		return
	}
	if r.tailSpan != nil && y == r.tailSpan.Y && x == r.tailSpanEnd {
		r.spanCells = append(r.spanCells, cell)
		r.refreshTailSpan()
		r.tailSpanEnd += cellDamageWidth(cell)
		return
	}
	start := len(r.spanCells)
	r.spanCells = append(r.spanCells, cell)
	span := &SpanDamage{X: x, Y: y, Cells: r.spanCells[start:len(r.spanCells)]}
	r.damages = append(r.damages, span)
	r.tailSpan = span
	r.tailSpanStart = start
	r.tailSpanEnd = x + cellDamageWidth(cell)
}

func (r *screenDamageRecorder) recordRepeatedSpan(x, y, count int, cell uv.Cell) {
	if r == nil || count <= 0 {
		return
	}
	start := len(r.spanCells)
	for i := 0; i < count; i++ {
		r.spanCells = append(r.spanCells, cell)
	}
	span := &SpanDamage{X: x, Y: y, Cells: r.spanCells[start:len(r.spanCells)]}
	r.damages = append(r.damages, span)
	r.tailSpan = span
	r.tailSpanStart = start
	r.tailSpanEnd = x + spanCellsWidth(r.spanCells[start:len(r.spanCells)])
}

func (r *screenDamageRecorder) recordScrollbackLine(y int, line uv.Line, wrapped bool) {
	if r == nil {
		return
	}
	r.tailSpan = nil
	cells := make([]uv.Cell, len(line))
	copy(cells, line)
	r.damages = append(r.damages, ScrollbackDamage{Y: y, Cells: cells, Wrapped: wrapped})
}

func (r *screenDamageRecorder) recordSpan(span SpanDamage) {
	if r == nil || len(span.Cells) == 0 {
		return
	}
	if r.mergeTrailingSpan(span) {
		return
	}
	start := len(r.spanCells)
	r.spanCells = append(r.spanCells, span.Cells...)
	next := &SpanDamage{
		X:     span.X,
		Y:     span.Y,
		Cells: r.spanCells[start:len(r.spanCells)],
	}
	r.damages = append(r.damages, next)
	r.tailSpan = next
	r.tailSpanStart = start
	r.tailSpanEnd = span.X + spanDamageWidth(span)
}

func (r *screenDamageRecorder) mergeTrailingSpan(next SpanDamage) bool {
	if r == nil || len(next.Cells) == 0 || r.tailSpan == nil || next.Y != r.tailSpan.Y || next.X != r.tailSpanEnd {
		return false
	}
	r.spanCells = append(r.spanCells, next.Cells...)
	r.refreshTailSpan()
	r.tailSpanEnd += spanDamageWidth(next)
	return true
}

func (r *screenDamageRecorder) refreshTailSpan() {
	if r == nil || r.tailSpan == nil || r.tailSpanStart < 0 || r.tailSpanStart > len(r.spanCells) {
		return
	}
	r.tailSpan.Cells = r.spanCells[r.tailSpanStart:len(r.spanCells)]
}

func (r *screenDamageRecorder) snapshot() []Damage {
	if r == nil || len(r.damages) == 0 {
		return nil
	}
	out := make([]Damage, len(r.damages))
	for i, damage := range r.damages {
		if span, ok := damage.(*SpanDamage); ok {
			out[i] = *span
			continue
		}
		out[i] = damage
	}
	return out
}

func spanDamageWidth(d SpanDamage) int {
	return spanCellsWidth(d.Cells)
}

func spanCellsWidth(cells []uv.Cell) int {
	width := 0
	for _, cell := range cells {
		if cell.Width > 0 {
			width += cell.Width
			continue
		}
		width++
	}
	if width <= 0 {
		return 1
	}
	return width
}

func cellDamageWidth(cell uv.Cell) int {
	if cell.Width > 0 {
		return cell.Width
	}
	return 1
}

func cloneDamageCell(cell *uv.Cell) uv.Cell {
	if cell == nil {
		return uv.Cell{Content: " ", Width: 1}
	}
	return *cell
}

func isClearDamageCell(cell *uv.Cell) bool {
	if cell == nil {
		return true
	}
	if cell.Content != "" && cell.Content != " " {
		return false
	}
	if cell.Width != 0 && cell.Width != 1 {
		return false
	}
	if !cell.Style.IsZero() {
		return false
	}
	if !cell.Link.IsZero() {
		return false
	}
	return true
}
