package canvas

import (
	"reflect"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tui/domain/types"
)

func TestCellKeepsMinimalTask3Shape(t *testing.T) {
	cellType := reflect.TypeOf(Cell{})
	if got := cellType.NumField(); got != 3 {
		t.Fatalf("unexpected field count: %d", got)
	}

	want := []string{"Content", "Width", "Style"}
	for i, name := range want {
		if got := cellType.Field(i).Name; got != name {
			t.Fatalf("unexpected field %d: %q", i, got)
		}
	}
}

func TestCanvasDrawRespectsClipping(t *testing.T) {
	canvas := New(8, 4)
	canvas.Fill(types.Rect{X: 0, Y: 0, W: 8, H: 4}, BlankCell())
	canvas.DrawText(types.Rect{X: 6, Y: 1, W: 2, H: 1}, 6, 1, "hello", DrawStyle{})

	got := canvas.Lines()
	if got[1] != "      he" {
		t.Fatalf("unexpected clipped row: %q", got[1])
	}
}

func TestCanvasDrawOrderLetsFloatingOverwriteTiled(t *testing.T) {
	canvas := New(6, 3)
	canvas.DrawText(types.Rect{X: 0, Y: 1, W: 6, H: 1}, 0, 1, "tiled-", DrawStyle{})
	canvas.DrawText(types.Rect{X: 2, Y: 1, W: 3, H: 1}, 2, 1, "TOP", DrawStyle{})

	if got := canvas.Lines()[1]; got != "tiTOP-" {
		t.Fatalf("unexpected row: %q", got)
	}
}

func TestCanvasDrawTextDropsWideGlyphWhenLeftClipped(t *testing.T) {
	canvas := New(2, 1)
	canvas.Fill(types.Rect{X: 0, Y: 0, W: 2, H: 1}, BlankCell())
	canvas.DrawText(types.Rect{X: 1, Y: 0, W: 2, H: 1}, 0, 0, "界a", DrawStyle{})

	if got := canvas.Lines()[0]; got != "  " {
		t.Fatalf("unexpected row: %q", got)
	}
}

func TestCanvasDrawTextDropsWideGlyphWhenClipIsNarrowerThanCell(t *testing.T) {
	canvas := New(2, 1)
	canvas.Fill(types.Rect{X: 0, Y: 0, W: 2, H: 1}, BlankCell())
	canvas.DrawText(types.Rect{X: 0, Y: 0, W: 1, H: 1}, 0, 0, "界a", DrawStyle{})

	if got := canvas.Lines()[0]; got != "  " {
		t.Fatalf("unexpected row: %q", got)
	}
}

func TestCanvasSetKeepsRenderedWidthInsideCanvas(t *testing.T) {
	canvas := New(2, 1)
	canvas.Fill(types.Rect{X: 0, Y: 0, W: 2, H: 1}, BlankCell())
	canvas.Set(1, 0, Cell{Content: "界", Width: 2})

	got := canvas.Lines()[0]
	if width := xansi.StringWidth(got); width != 2 {
		t.Fatalf("unexpected display width: %d for %q", width, got)
	}
	if got != "  " {
		t.Fatalf("unexpected row: %q", got)
	}
}
