package runtime

import (
	"context"
	"testing"

	"github.com/lozzow/termx/protocol"
)

func TestResizeTerminalUpdatesSnapshotSizeForFloatingPaneBinding(t *testing.T) {
	ctx := context.Background()
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 21, Mode: "collaborator"}
	client.snapshotByTerminal["term-float"] = snapshotWithLines("term-float", 8, 4, []string{"seed"})

	rt := New(client)
	if _, err := rt.AttachTerminal(ctx, "float-1", "term-float", "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if _, err := rt.LoadSnapshot(ctx, "term-float", 0, 10); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	if err := rt.ResizeTerminal(ctx, "float-1", "term-float", 50, 16); err != nil {
		t.Fatalf("resize terminal: %v", err)
	}

	if len(client.resizeCalls) != 1 {
		t.Fatalf("expected 1 resize call, got %d", len(client.resizeCalls))
	}
	call := client.resizeCalls[0]
	if call.channel != 21 || call.cols != 50 || call.rows != 16 {
		t.Fatalf("unexpected resize call: %+v", call)
	}
	stored := rt.Registry().Get("term-float")
	if stored == nil || stored.Snapshot == nil {
		t.Fatal("expected floating terminal snapshot after resize")
	}
	if stored.Snapshot.Size.Cols != 50 || stored.Snapshot.Size.Rows != 16 {
		t.Fatalf("expected floating snapshot size 50x16, got %dx%d", stored.Snapshot.Size.Cols, stored.Snapshot.Size.Rows)
	}
}
