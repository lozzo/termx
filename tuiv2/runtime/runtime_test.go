package runtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/bridge"
	unixtransport "github.com/lozzow/termx/transport/unix"
)

func newTestRuntime(t *testing.T) (*Runtime, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	socketPath := filepath.Join(t.TempDir(), "termx.sock")
	srv := termx.NewServer(termx.WithSocketPath(socketPath))
	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		_ = srv.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("server did not stop in time")
		}
	})

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
	t.Cleanup(func() { _ = client.Close() })

	helloCtx, helloCancel := context.WithTimeout(ctx, 2*time.Second)
	defer helloCancel()
	if err := client.Hello(helloCtx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}

	return New(bridge.NewProtocolClient(client)), ctx
}

func TestRuntimeListTerminalsSyncsRegistry(t *testing.T) {
	rt, ctx := newTestRuntime(t)

	created, err := rt.client.Create(ctx, []string{"sh"}, "demo", protocol.Size{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	terminals, err := rt.ListTerminals(ctx)
	if err != nil {
		t.Fatalf("list terminals: %v", err)
	}
	if len(terminals) != 1 {
		t.Fatalf("expected 1 terminal, got %d", len(terminals))
	}
	stored := rt.Registry().Get(created.TerminalID)
	if stored == nil {
		t.Fatalf("expected terminal %q in registry", created.TerminalID)
	}
	if stored.Name != "demo" {
		t.Fatalf("expected name demo, got %q", stored.Name)
	}
}

func TestRuntimeAttachSnapshotInputAndResize(t *testing.T) {
	rt, ctx := newTestRuntime(t)

	created, err := rt.client.Create(ctx, []string{"sh"}, "demo", protocol.Size{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	terminal, err := rt.AttachTerminal(ctx, "pane-1", created.TerminalID, "collaborator")
	if err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if terminal.Channel == 0 {
		t.Fatal("expected non-zero channel")
	}
	binding := rt.Binding("pane-1")
	if binding == nil || !binding.Connected {
		t.Fatal("expected connected pane binding")
	}

	snapshot, err := rt.LoadSnapshot(ctx, created.TerminalID, 0, 10)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if terminal.Snapshot == nil {
		t.Fatal("expected snapshot cached on terminal runtime")
	}

	if err := rt.SendInput(ctx, "pane-1", []byte("echo hi\n")); err != nil {
		t.Fatalf("send input: %v", err)
	}
	if err := rt.ResizeTerminal(ctx, "pane-1", 100, 40); err != nil {
		t.Fatalf("resize terminal: %v", err)
	}
}
