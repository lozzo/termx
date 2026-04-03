package vterm

import (
	"testing"
)

func TestVTermTitleCallback(t *testing.T) {
	var capturedTitle string
	titleHandler := func(title string) {
		capturedTitle = title
	}

	vt := New(80, 24, 1000, nil)
	vt.SetTitleHandler(titleHandler)

	// OSC 2 ; title ST (where ST is ESC \)
	_, err := vt.Write([]byte("\x1b]2;Test Title\x1b\\"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if capturedTitle != "Test Title" {
		t.Errorf("Expected title 'Test Title', got '%s'", capturedTitle)
	}
}

func TestVTermTitleCallbackNotSetDoesNotPanic(t *testing.T) {
	vt := New(80, 24, 1000, nil)
	// Don't set title handler

	// Should not panic
	_, err := vt.Write([]byte("\x1b]2;Test Title\x1b\\"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}
