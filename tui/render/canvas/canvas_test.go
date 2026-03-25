package canvas

import (
	"testing"

	"github.com/lozzow/termx/tui/domain/types"
)

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
