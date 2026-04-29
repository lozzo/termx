package vt

import uv "github.com/charmbracelet/ultraviolet"

type screenDamageRecorder struct {
	damages []Damage
}

func (r *screenDamageRecorder) record(d Damage) {
	if r == nil || d == nil {
		return
	}
	if span, ok := d.(SpanDamage); ok {
		span = cloneSpanDamage(span)
		if merged := r.mergeTrailingSpan(span); merged {
			return
		}
		r.damages = append(r.damages, span)
		return
	}
	r.damages = append(r.damages, cloneDamage(d))
}

func (r *screenDamageRecorder) mergeTrailingSpan(next SpanDamage) bool {
	if r == nil || len(r.damages) == 0 {
		return false
	}
	prev, ok := r.damages[len(r.damages)-1].(SpanDamage)
	if !ok || prev.Y != next.Y {
		return false
	}
	prevWidth := spanDamageWidth(prev)
	if prev.X+prevWidth != next.X {
		return false
	}
	prev.Cells = append(prev.Cells, next.Cells...)
	r.damages[len(r.damages)-1] = prev
	return true
}

func (r *screenDamageRecorder) snapshot() []Damage {
	if r == nil || len(r.damages) == 0 {
		return nil
	}
	out := make([]Damage, len(r.damages))
	for i, damage := range r.damages {
		out[i] = cloneDamage(damage)
	}
	return out
}

func cloneDamage(d Damage) Damage {
	switch value := d.(type) {
	case SpanDamage:
		return cloneSpanDamage(value)
	case CellDamage:
		return value
	case RectDamage:
		return value
	case ClearDamage:
		return value
	case ScreenDamage:
		return value
	case MoveDamage:
		return value
	case ScrollDamage:
		return value
	default:
		return d
	}
}

func cloneSpanDamage(d SpanDamage) SpanDamage {
	if len(d.Cells) == 0 {
		return d
	}
	d.Cells = append([]uv.Cell(nil), d.Cells...)
	return d
}

func spanDamageWidth(d SpanDamage) int {
	width := 0
	for _, cell := range d.Cells {
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

func cloneDamageCell(cell *uv.Cell) uv.Cell {
	if cell == nil {
		return uv.Cell{Content: " ", Width: 1}
	}
	return *cell.Clone()
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
