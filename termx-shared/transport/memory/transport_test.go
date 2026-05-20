package memory

import (
	"bytes"
	"io"
	"testing"
)

func TestPairRoundTrip(t *testing.T) {
	client, server := NewPair()
	defer client.Close()
	defer server.Close()

	payload := []byte("hello")
	if err := client.Send(payload); err != nil {
		t.Fatalf("client send failed: %v", err)
	}

	got, err := server.Recv()
	if err != nil {
		t.Fatalf("server recv failed: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected payload: %q", string(got))
	}
}

func TestSendAfterLocalCloseReturnsEOF(t *testing.T) {
	client, server := NewPair()
	defer server.Close()

	if err := client.Close(); err != nil {
		t.Fatalf("client close failed: %v", err)
	}

	if err := client.Send([]byte("hello")); err == nil || err != io.EOF {
		t.Fatalf("expected EOF after local close, got %v", err)
	}
}
