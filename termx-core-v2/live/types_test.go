package live

import "testing"

func TestSurfaceSizeValid(t *testing.T) {
	if !(SurfaceSize{Cols: 80, Rows: 24}).Valid() {
		t.Fatal("expected positive size to be valid")
	}
	if (SurfaceSize{Cols: 0, Rows: 24}).Valid() {
		t.Fatal("expected zero cols to be invalid")
	}
}

func TestSurfaceTrackWritesAndTrimsRows(t *testing.T) {
	surface := NewSurfaceTrack(SurfaceSize{Cols: 80, Rows: 2})
	surface.Write("one\ntwo\nthree")
	rows := surface.Rows()
	if len(rows) != 2 || rows[0] != "two" || rows[1] != "three" {
		t.Fatalf("unexpected rows %#v", rows)
	}
	surface.Resize(SurfaceSize{Cols: 80, Rows: 1})
	rows = surface.Rows()
	if len(rows) != 1 || rows[0] != "three" {
		t.Fatalf("unexpected resized rows %#v", rows)
	}
}
