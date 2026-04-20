package vt

import uv "github.com/charmbracelet/ultraviolet"

// Damage represents a damaged area.
type Damage interface {
	// Bounds returns the bounds of the damaged area.
	Bounds() uv.Rectangle
}

// CellDamage represents a damaged cell.
type CellDamage struct {
	X, Y  int
	Width int
}

// Bounds returns the bounds of the damaged area.
func (d CellDamage) Bounds() uv.Rectangle {
	return uv.Rect(d.X, d.Y, d.Width, 1)
}

// SpanDamage represents a sequence of cells written on a single row.
type SpanDamage struct {
	X, Y  int
	Cells []uv.Cell
}

// Bounds returns the bounds of the damaged area.
func (d SpanDamage) Bounds() uv.Rectangle {
	width := 0
	for _, cell := range d.Cells {
		if cell.Width > 0 {
			width += cell.Width
			continue
		}
		width++
	}
	if width <= 0 {
		width = 1
	}
	return uv.Rect(d.X, d.Y, width, 1)
}

// RectDamage represents a damaged rectangle.
type RectDamage uv.Rectangle

// Bounds returns the bounds of the damaged area.
func (d RectDamage) Bounds() uv.Rectangle {
	return uv.Rectangle(d)
}

// X returns the x-coordinate of the damaged area.
func (d RectDamage) X() int {
	return uv.Rectangle(d).Min.X
}

// Y returns the y-coordinate of the damaged area.
func (d RectDamage) Y() int {
	return uv.Rectangle(d).Min.Y
}

// Width returns the width of the damaged area.
func (d RectDamage) Width() int {
	return uv.Rectangle(d).Dx()
}

// Height returns the height of the damaged area.
func (d RectDamage) Height() int {
	return uv.Rectangle(d).Dy()
}

// ClearDamage represents a cleared rectangle.
type ClearDamage uv.Rectangle

// Bounds returns the bounds of the damaged area.
func (d ClearDamage) Bounds() uv.Rectangle {
	return uv.Rectangle(d)
}

// X returns the x-coordinate of the damaged area.
func (d ClearDamage) X() int {
	return uv.Rectangle(d).Min.X
}

// Y returns the y-coordinate of the damaged area.
func (d ClearDamage) Y() int {
	return uv.Rectangle(d).Min.Y
}

// Width returns the width of the damaged area.
func (d ClearDamage) Width() int {
	return uv.Rectangle(d).Dx()
}

// Height returns the height of the damaged area.
func (d ClearDamage) Height() int {
	return uv.Rectangle(d).Dy()
}

// ScreenDamage represents a damaged screen.
type ScreenDamage struct {
	Width, Height int
}

// Bounds returns the bounds of the damaged area.
func (d ScreenDamage) Bounds() uv.Rectangle {
	return uv.Rect(0, 0, d.Width, d.Height)
}

// MoveDamage represents a moved area.
// The area is moved from the source to the destination.
type MoveDamage struct {
	Src, Dst uv.Rectangle
}

// Bounds returns the union of the source and destination rectangles.
func (d MoveDamage) Bounds() uv.Rectangle {
	return d.Src.Union(d.Dst)
}

// ScrollDamage represents a scrolled area.
// The area is scrolled by the given deltas.
type ScrollDamage struct {
	uv.Rectangle
	Dx, Dy int
}

// Bounds returns the damaged rectangle.
func (d ScrollDamage) Bounds() uv.Rectangle {
	return d.Rectangle
}
