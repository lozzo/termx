package unix

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/transport"
)

func TestListenerDialRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.sock")
	listener, err := NewListener(path)
	if err != nil {
		t.Fatalf("new listener failed: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	accepted := make(chan transport.Transport, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		accepted <- conn
	}()

	client, err := Dial(path)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer client.Close()

	var server transport.Transport
	select {
	case server = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for accept")
	}
	defer server.Close()

	if err := client.Send([]byte("hello")); err != nil {
		t.Fatalf("client send failed: %v", err)
	}
	got, err := server.Recv()
	if err != nil {
		t.Fatalf("server recv failed: %v", err)
	}
	if !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("unexpected server payload: %q", string(got))
	}

	if err := server.Send([]byte("world")); err != nil {
		t.Fatalf("server send failed: %v", err)
	}
	got, err = client.Recv()
	if err != nil {
		t.Fatalf("client recv failed: %v", err)
	}
	if !bytes.Equal(got, []byte("world")) {
		t.Fatalf("unexpected client payload: %q", string(got))
	}

	if err := client.Close(); err != nil {
		t.Fatalf("client close failed: %v", err)
	}
	select {
	case <-client.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for client done channel")
	}
}

func TestListenerAcceptContextCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.sock")
	listener, err := NewListener(path)
	if err != nil {
		t.Fatalf("new listener failed: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := listener.Accept(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
