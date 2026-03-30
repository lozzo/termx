package bridge

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx"
	"github.com/lozzow/termx/protocol"
	unixtransport "github.com/lozzow/termx/transport/unix"
)

func TestProtocolClientList(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	socketPath := filepath.Join(t.TempDir(), "termx.sock")
	srv := termx.NewServer(termx.WithSocketPath(socketPath))
	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe(ctx)
	}()
	defer func() {
		cancel()
		_ = srv.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("server did not stop in time")
		}
	}()

	var transport *unixtransport.Transport
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for {
		transport, err = unixtransport.Dial(socketPath)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	client := protocol.NewClient(transport)
	defer client.Close()

	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}

	adapted := NewProtocolClient(client)
	listed, err := adapted.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if listed == nil {
		t.Fatal("expected non-nil list result")
	}
}
