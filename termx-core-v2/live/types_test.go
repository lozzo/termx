package live

import (
	"fmt"
	"strings"
	"testing"
)

func TestSurfaceSizeValid(t *testing.T) {
	if !(SurfaceSize{Cols: 80, Rows: 24}).Valid() {
		t.Fatal("expected positive size to be valid")
	}
	if (SurfaceSize{Cols: 0, Rows: 24}).Valid() {
		t.Fatal("expected zero cols to be invalid")
	}
}

func TestSurfaceTrackKeepsScreenSizeCursorAndStyle(t *testing.T) {
	surface := NewSurfaceTrack(SurfaceSize{Cols: 10, Rows: 2})
	surface.Write("one\rT\x1b[31mR\x1b[0m")
	snapshot := surface.Snapshot()

	if snapshot.Size != (SurfaceSize{Cols: 10, Rows: 2}) || len(snapshot.Screen.Cells) != 2 {
		t.Fatalf("expected size-bound screen snapshot, got %#v", snapshot)
	}
	if got := vtermRowText(snapshot.Screen.Cells[0]); got[:3] != "TRe" {
		t.Fatalf("expected CR overwrite to stay in live grid, got %q", got)
	}
	if snapshot.Screen.Cells[0][1].Style.FG != "ansi:1" {
		t.Fatalf("expected ANSI style in live cell matrix, got %#v", snapshot.Screen.Cells[0][1])
	}
	if !snapshot.Cursor.Visible || snapshot.Cursor.Row != 0 || snapshot.Cursor.Col != 2 {
		t.Fatalf("expected live cursor from vterm, got %#v", snapshot.Cursor)
	}

	surface.Resize(SurfaceSize{Cols: 5, Rows: 1})
	snapshot = surface.Snapshot()
	if snapshot.Size != (SurfaceSize{Cols: 5, Rows: 1}) || len(snapshot.Screen.Cells) != 1 {
		t.Fatalf("unexpected resized snapshot %#v", snapshot)
	}
}

func TestSurfaceTrackLargeWriteKeepsLatestScreen(t *testing.T) {
	surface := NewSurfaceTrack(SurfaceSize{Cols: 80, Rows: 4})
	var out strings.Builder
	for i := 0; out.Len() <= 64*1024; i++ {
		fmt.Fprintf(&out, "%06d stress payload payload payload payload payload\n", i)
	}
	out.WriteString("999999 stress latest-tail")

	surface.Write(out.String())
	rows := surface.Rows()

	if !strings.Contains(strings.Join(rows, "\n"), "999999 stress latest-tail") {
		t.Fatalf("expected large live write to keep latest tail, got %#v", rows)
	}
}
