package workbench

import "testing"

func TestFramedPaneContentRectUsesFullInteriorWidth(t *testing.T) {
	content, ok := FramedPaneContentRect(Rect{X: 3, Y: 4, W: 20, H: 8}, false, false)
	if !ok {
		t.Fatal("expected content rect")
	}
	want := Rect{X: 4, Y: 5, W: 18, H: 6}
	if content != want {
		t.Fatalf("expected %#v, got %#v", want, content)
	}
}

func TestFramedPaneContentRectKeepsDistinctEdgesWithFullInteriorWidth(t *testing.T) {
	content, ok := FramedPaneContentRect(Rect{X: 20, Y: 10, W: 20, H: 8}, true, true)
	if !ok {
		t.Fatal("expected content rect")
	}
	want := Rect{X: 21, Y: 11, W: 18, H: 6}
	if content != want {
		t.Fatalf("expected %#v, got %#v", want, content)
	}
}
