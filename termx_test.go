package termx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestServerCreateListTagsSubscribeSnapshotAndRemoval(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(
		WithDefaultKeepAfterExit(200*time.Millisecond),
		WithDefaultScrollback(128),
	)

	eventsCtx, cancelEvents := context.WithCancel(ctx)
	defer cancelEvents()
	events := srv.Events(eventsCtx)

	term, err := srv.Create(ctx, CreateOptions{
		Name:    "dev",
		Command: []string{"bash", "--noprofile", "--norc"},
		Tags:    map[string]string{"group": "dev"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	select {
	case evt := <-events:
		if evt.Type != EventTerminalCreated || evt.TerminalID != term.ID {
			t.Fatalf("unexpected created event: %#v", evt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for create event")
	}

	list, err := srv.List(ctx, ListOptions{Tags: map[string]string{"group": "dev"}})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != term.ID {
		t.Fatalf("unexpected list result: %#v", list)
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	stream, err := srv.Subscribe(streamCtx, term.ID)
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	if err := srv.SendKeys(ctx, term.ID, "echo integration", "Enter"); err != nil {
		t.Fatalf("send keys failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msg := <-stream
		if msg.Type == StreamOutput && strings.Contains(string(msg.Output), "integration") {
			break
		}
	}

	snap, err := srv.Snapshot(ctx, term.ID, SnapshotOptions{ScrollbackLimit: 50})
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if !snapshotContains(snap, "integration") {
		t.Fatalf("snapshot missing output: %#v", snap)
	}

	if err := srv.SetTags(ctx, term.ID, map[string]string{"status": "idle"}); err != nil {
		t.Fatalf("set tags failed: %v", err)
	}
	got, err := srv.Get(ctx, term.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.Tags["group"] != "dev" || got.Tags["status"] != "idle" {
		t.Fatalf("unexpected tags: %#v", got.Tags)
	}

	if err := srv.Kill(ctx, term.ID); err != nil {
		t.Fatalf("kill failed: %v", err)
	}

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msg := <-stream
		if msg.Type == StreamClosed {
			goto removedCheck
		}
	}
	t.Fatal("timed out waiting for stream close")

removedCheck:
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, err := srv.Get(ctx, term.ID)
		if errors.Is(err, ErrNotFound) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatal("terminal was not auto-removed")
}
