package runtime

import (
	"reflect"
	"testing"

	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestVisibleSurfaceHoldsStableVTermSnapshot(t *testing.T) {
	vt := localvterm.New(16, 3, 20, nil)
	if _, err := vt.Write([]byte("alpha\r\nbeta\r\ngamma")); err != nil {
		t.Fatalf("seed vterm: %v", err)
	}
	terminal := &TerminalRuntime{
		TerminalID:     "term-1",
		VTerm:          vt,
		SurfaceVersion: 1,
	}
	surface := visibleSurface(terminal)
	if surface == nil {
		t.Fatal("expected visible surface")
	}
	base := surface.ScrollbackRows()
	beforeRows := []string{
		rowText(surface.Row(base)),
		rowText(surface.Row(base + 1)),
		rowText(surface.Row(base + 2)),
	}

	if _, err := vt.Write([]byte("\r\ndelta\r\nepsilon\r\n100000 final\r\npython total")); err != nil {
		t.Fatalf("mutate vterm: %v", err)
	}
	afterRows := []string{
		rowText(surface.Row(base)),
		rowText(surface.Row(base + 1)),
		rowText(surface.Row(base + 2)),
	}
	if !reflect.DeepEqual(afterRows, beforeRows) {
		t.Fatalf("expected surface snapshot rows to remain stable, got %#v want %#v", afterRows, beforeRows)
	}
	if got, want := beforeRows, []string{"alpha", "beta", "gamma"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected seeded rows: got %#v want %#v", got, want)
	}
}
