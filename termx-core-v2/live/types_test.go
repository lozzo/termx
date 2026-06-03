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
