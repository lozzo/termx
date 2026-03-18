package memory

import (
	"bytes"
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
