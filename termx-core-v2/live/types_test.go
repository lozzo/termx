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

func TestSurfaceTrackPreservesAltScreenFrameOnExit(t *testing.T) {
	surface := NewSurfaceTrack(SurfaceSize{Cols: 20, Rows: 3})
	surface.Write("primary")
	surface.Write("\x1b[?1049h\x1b[2Jalt-final\x1b[?1049l")

	snapshot := surface.Snapshot()
	if snapshot.Modes.AlternateScreen || snapshot.Screen.IsAlternateScreen {
		t.Fatalf("expected preserved frame to become primary live screen, got modes=%#v screen=%#v", snapshot.Modes, snapshot.Screen)
	}
	if got := strings.Join(surface.Rows(), "\n"); !strings.Contains(got, "alt-final") || strings.Contains(got, "primary") {
		t.Fatalf("expected alt final frame to remain visible without restoring primary, got %q", got)
	}
}

func TestSurfaceTrackPreservesAltScreenFrameWhenExitSequenceIsSplit(t *testing.T) {
	surface := NewSurfaceTrack(SurfaceSize{Cols: 20, Rows: 3})
	surface.Write("\x1b[?1049h\x1b[2Jsplit-final")
	surface.Write("\x1b[?104")
	surface.Write("9l")

	if got := strings.Join(surface.Rows(), "\n"); !strings.Contains(got, "split-final") {
		t.Fatalf("expected split alt exit sequence to preserve final frame, got %q", got)
	}
	if surface.Snapshot().Modes.AlternateScreen {
		t.Fatal("expected split alt exit to leave alternate screen mode")
	}
}
