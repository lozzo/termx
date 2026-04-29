package protocol

import (
	"errors"
	"testing"
)

func TestDecodeFrameRejectsMalformedLength(t *testing.T) {
	frame := []byte{
		0x00, 0x03,
		TypeOutput,
		0x00, 0x00, 0x00, 0x03,
		'o', 'k',
	}

	if _, _, _, err := DecodeFrame(frame); err == nil {
		t.Fatal("expected malformed frame length error")
	}
}

func TestPayloadHelpersRoundTrip(t *testing.T) {
	cols, rows, err := DecodeResizePayload(EncodeResizePayload(120, 50))
	if err != nil {
		t.Fatalf("decode resize payload failed: %v", err)
	}
	if cols != 120 || rows != 50 {
		t.Fatalf("unexpected resize payload roundtrip: %dx%d", cols, rows)
	}

	dropped, err := DecodeSyncLostPayload(EncodeSyncLostPayload(42))
	if err != nil {
		t.Fatalf("decode sync-lost payload failed: %v", err)
	}
	if dropped != 42 {
		t.Fatalf("unexpected dropped bytes: %d", dropped)
	}

	code, err := DecodeClosedPayload(EncodeClosedPayload(-1))
	if err != nil {
		t.Fatalf("decode closed payload failed: %v", err)
	}
	if code != -1 {
		t.Fatalf("unexpected closed code: %d", code)
	}

	if _, _, err := DecodeResizePayload([]byte{1, 2, 3}); !errors.Is(err, ErrShortPayload) {
		t.Fatalf("expected ErrShortPayload for resize payload, got %v", err)
	}
	if _, err := DecodeSyncLostPayload([]byte{1, 2, 3}); !errors.Is(err, ErrShortPayload) {
		t.Fatalf("expected ErrShortPayload for sync-lost payload, got %v", err)
	}
	if _, err := DecodeClosedPayload([]byte{1, 2, 3}); !errors.Is(err, ErrShortPayload) {
		t.Fatalf("expected ErrShortPayload for closed payload, got %v", err)
	}
}
