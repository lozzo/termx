package bridge

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/lozzow/termx/termx-testkit"
)

func TestClientBoundaryDoesNotExposeRemoteCapabilities(t *testing.T) {
	want := []string{
		"Attach",
		"Close",
		"Create",
		"EnsureResize",
		"Events",
		"GridViewport",
		"HistoryWindow",
		"Input",
		"Kill",
		"List",
		"Remove",
		"Resize",
		"Restart",
		"SetMetadata",
		"SetTags",
		"Snapshot",
		"Stream",
		"StreamReady",
	}
	assertMethodSet(t, reflect.TypeOf((*Client)(nil)).Elem(), want)
	protocolWant := append(slices.Clone(want), "StorageDelete", "StorageGet", "StorageList", "StoragePut")
	slices.Sort(protocolWant)
	assertMethodSet(t, reflect.TypeOf((*ProtocolClient)(nil)), protocolWant)

	storageWant := []string{"StorageDelete", "StorageGet", "StorageList", "StoragePut"}
	assertMethodSet(t, reflect.TypeOf((*StorageClient)(nil)).Elem(), storageWant)
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

	daemon := testkit.StartDaemon(t, ctx, "termx.sock")
	client := daemon.NewClient(t, ctx)

	adapted := NewProtocolClient(client)
	listed, err := adapted.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if listed == nil {
		t.Fatal("expected non-nil list result")
	}
}
