package clientapi

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core"
	"github.com/lozzow/termx/termx-core/protocol"
	unixtransport "github.com/lozzow/termx/termx-core/transport/unix"
)

func TestClientAPIBoundaryDoesNotExposeRemoteCapabilities(t *testing.T) {
	want := []string{
		"AcquireSessionLease",
		"ApplySession",
		"Attach",
		"AttachSession",
		"Close",
		"Create",
		"CreateSession",
		"DetachSession",
		"EnsureResize",
		"Events",
		"GetSession",
		"Input",
		"Kill",
		"List",
		"ListSessions",
		"ReleaseSessionLease",
		"Remove",
		"ReplaceSession",
		"Resize",
		"Restart",
		"SetMetadata",
		"SetTags",
		"Snapshot",
		"Stream",
		"UpdateSessionView",
	}
	assertMethodSet(t, reflect.TypeOf((*Client)(nil)).Elem(), want)
	assertMethodSet(t, reflect.TypeOf((*ProtocolClient)(nil)), want)
}

func assertMethodSet(t *testing.T, typ reflect.Type, want []string) {
	t.Helper()
	got := make([]string, 0, typ.NumMethod())
	for i := 0; i < typ.NumMethod(); i++ {
		got = append(got, typ.Method(i).Name)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("%s public methods changed:\n got: %v\nwant: %v", typ, got, want)
	}
}

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
