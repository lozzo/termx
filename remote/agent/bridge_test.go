package agent

import (
	"context"
	"testing"
	"time"

	"github.com/lozzow/termx/transport/memory"
)

func TestBridgeProxiesFramesBothDirections(t *testing.T) {
	clientLocal, bridgeLocal := memory.NewPair()
	bridgeRemote, clientRemote := memory.NewPair()
	defer clientLocal.Close()
	defer bridgeLocal.Close()
	defer bridgeRemote.Close()
	defer clientRemote.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Proxy(ctx, bridgeLocal, bridgeRemote)
	}()

	if err := clientLocal.Send([]byte("from-local")); err != nil {
		t.Fatalf("local send failed: %v", err)
	}
	gotRemote, err := clientRemote.Recv()
	if err != nil {
		t.Fatalf("remote recv failed: %v", err)
	}
	if string(gotRemote) != "from-local" {
		t.Fatalf("unexpected remote payload: %q", string(gotRemote))
	}

	if err := clientRemote.Send([]byte("from-remote")); err != nil {
		t.Fatalf("remote send failed: %v", err)
	}
	gotLocal, err := clientLocal.Recv()
	if err != nil {
		t.Fatalf("local recv failed: %v", err)
	}
	if string(gotLocal) != "from-remote" {
		t.Fatalf("unexpected local payload: %q", string(gotLocal))
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("proxy returned error after cancel: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for proxy shutdown")
	}
}
