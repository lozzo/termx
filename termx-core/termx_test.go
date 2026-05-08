package termx

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
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
		if streamMessageContainsText(msg, 80, 24, "integration") {
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

func TestServerListReturnsDistinctTerminalIDs(t *testing.T) {
	srv := NewServer()
	srv.terminals["1"] = &Terminal{
		id:        "1",
		name:      "one",
		command:   []string{"/bin/bash"},
		tags:      map[string]string{"group": "test"},
		size:      Size{Cols: 80, Rows: 24},
		state:     StateRunning,
		createdAt: time.Unix(1, 0).UTC(),
	}
	srv.terminals["3"] = &Terminal{
		id:        "3",
		name:      "three",
		command:   []string{"/bin/bash"},
		tags:      map[string]string{"group": "test"},
		size:      Size{Cols: 80, Rows: 24},
		state:     StateRunning,
		createdAt: time.Unix(3, 0).UTC(),
	}

	list, err := srv.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected two terminals, got %#v", list)
	}
	if list[0].ID != "1" || list[1].ID != "3" {
		t.Fatalf("expected distinct sorted terminal IDs [1 3], got [%s %s]", list[0].ID, list[1].ID)
	}
}

func TestServerDoesNotSpecialCaseRemoteRPCMethods(t *testing.T) {
	server := NewServer()
	for _, method := range []string{
		"remote.status",
		"remote.pair.start",
		"remote.local.enable",
		"remote.local.status",
		"remote.local.disable",
	} {
		t.Run(method, func(t *testing.T) {
			allocator := protocol.NewChannelAllocator()
			attachments := make(map[uint16]*sessionAttachment)
			var attachmentsMu sync.RWMutex
			result, code, err := server.handleRequest(
				context.Background(),
				"test",
				nil,
				allocator,
				attachments,
				&attachmentsMu,
				transportScope{},
				protocol.Request{ID: 1, Method: method, Params: json.RawMessage(`{}`)},
				func(uint16, uint8, []byte) error { return nil },
			)
			if err == nil {
				t.Fatalf("expected %s to be rejected, got code=%d result=%s", method, code, string(result))
			}
			if code != 400 || !strings.Contains(err.Error(), "unsupported method") {
				t.Fatalf("expected generic unsupported method for %s, got code=%d err=%v", method, code, err)
			}
		})
	}
}
