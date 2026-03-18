package protocol

import (
	"bytes"
	"errors"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteFrame(3, TypeOutput, []byte("hello")); err != nil {
		t.Fatalf("write frame failed: %v", err)
	}

	dec := NewDecoder(&buf)
	ch, typ, payload, err := dec.ReadFrame()
	if err != nil {
		t.Fatalf("read frame failed: %v", err)
	}
	if ch != 3 || typ != TypeOutput || string(payload) != "hello" {
		t.Fatalf("unexpected frame: %d %d %q", ch, typ, string(payload))
	}
}

func TestFrameRejectsOversizePayload(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	err := enc.WriteFrame(1, TypeOutput, make([]byte, MaxFrameSize+1))
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("expected ErrFrameTooLarge, got %v", err)
	}
}
