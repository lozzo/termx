package termx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/transport/memory"
	unixtransport "github.com/lozzow/termx/transport/unix"
)

func TestServerCreateRejectsInvalidCommandAndDuplicateID(t *testing.T) {
	srv := NewServer()

	if _, err := srv.Create(context.Background(), CreateOptions{}); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("expected ErrInvalidCommand, got %v", err)
	}

	_, err := srv.Create(context.Background(), CreateOptions{
		ID:      "dup00001",
		Command: []string{"bash", "--noprofile", "--norc"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	if _, err := srv.Create(context.Background(), CreateOptions{
		ID:      "dup00001",
		Command: []string{"bash", "--noprofile", "--norc"},
	}); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("expected ErrDuplicateID, got %v", err)
	}
}

func TestServerCreateRejectsDuplicateName(t *testing.T) {
	srv := NewServer()
	ctx := context.Background()

	_, err := srv.Create(ctx, CreateOptions{
		ID:      "dupname01",
		Name:    "shell",
		Command: []string{"bash", "--noprofile", "--norc"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	if _, err := srv.Create(ctx, CreateOptions{
		ID:      "dupname02",
		Name:    "shell",
		Command: []string{"bash", "--noprofile", "--norc"},
	}); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("expected ErrDuplicateName, got %v", err)
	}
}

func TestServerSetMetadataRejectsDuplicateName(t *testing.T) {
	srv := NewServer()
	ctx := context.Background()

	first, err := srv.Create(ctx, CreateOptions{
		ID:      "meta-dup-1",
		Name:    "shell",
		Command: []string{"bash", "--noprofile", "--norc"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create first terminal: %v", err)
	}
	second, err := srv.Create(ctx, CreateOptions{
		ID:      "meta-dup-2",
		Name:    "logs",
		Command: []string{"bash", "--noprofile", "--norc"},
	})
	if err != nil {
		t.Fatalf("create second terminal: %v", err)
	}

	if err := srv.SetMetadata(ctx, second.ID, first.Name, nil); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("expected ErrDuplicateName, got %v", err)
	}
}

func TestServerListFiltersByStateAndAllowsTagRemoval(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(WithDefaultKeepAfterExit(2 * time.Second))

	term, err := srv.Create(ctx, CreateOptions{
		ID:             "filter01",
		Command:        []string{"bash", "--noprofile", "--norc"},
		Tags:           map[string]string{"group": "dev", "status": "busy"},
		KeepAfterExit:  2 * time.Second,
		ScrollbackSize: 64,
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	running := StateRunning
	list, err := srv.List(ctx, ListOptions{
		State: &running,
		Tags:  map[string]string{"group": "dev"},
	})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != term.ID {
		t.Fatalf("unexpected running list result: %#v", list)
	}

	if err := srv.SetTags(ctx, term.ID, map[string]string{"group": "", "status": "idle"}); err != nil {
		t.Fatalf("set tags failed: %v", err)
	}

	got, err := srv.Get(ctx, term.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if _, ok := got.Tags["group"]; ok {
		t.Fatalf("expected group tag to be removed, got %#v", got.Tags)
	}
	if got.Tags["status"] != "idle" {
		t.Fatalf("expected status tag to be updated, got %#v", got.Tags)
	}

	list, err = srv.List(ctx, ListOptions{Tags: map[string]string{"group": "dev"}})
	if err != nil {
		t.Fatalf("list after tag removal failed: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list after tag removal, got %#v", list)
	}

	if err := srv.Kill(ctx, term.ID); err != nil {
		t.Fatalf("kill failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err = srv.Get(ctx, term.ID)
		if err == nil && got.State == StateExited {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got == nil || got.State != StateExited {
		t.Fatalf("expected exited state, got %#v err=%v", got, err)
	}

	exited := StateExited
	list, err = srv.List(ctx, ListOptions{State: &exited})
	if err != nil {
		t.Fatalf("list exited failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != term.ID {
		t.Fatalf("unexpected exited list result: %#v", list)
	}
}

func TestServerRevokeCollaboratorsPublishesEvent(t *testing.T) {
	ctx := context.Background()
	srv := NewServer()

	term, err := srv.Create(ctx, CreateOptions{
		ID:      "revoke01",
		Command: []string{"bash", "--noprofile", "--norc"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	eventsCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := srv.Events(eventsCtx, WithTerminalFilter(term.ID), WithTypeFilter(EventCollaboratorsRevoked))

	if err := srv.RevokeCollaborators(ctx, term.ID); err != nil {
		t.Fatalf("revoke collaborators failed: %v", err)
	}

	select {
	case evt := <-events:
		if evt.Type != EventCollaboratorsRevoked || evt.TerminalID != term.ID {
			t.Fatalf("unexpected event: %#v", evt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for revoke event")
	}
}

func TestServerRevokeCollaboratorsDowngradesActiveAttachments(t *testing.T) {
	ctx := context.Background()
	srv := NewServer()

	term, err := srv.Create(ctx, CreateOptions{
		ID:      "revoke02",
		Command: []string{"bash", "--noprofile", "--norc"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }

	mustAttachChannel(t, srv, ctx, "memory-a", allocator, attachments, &attachmentsMu, term.ID, string(ModeCollaborator), sendFrame)
	mustAttachChannel(t, srv, ctx, "memory-b", allocator, attachments, &attachmentsMu, term.ID, string(ModeObserver), sendFrame)

	attached, err := srv.Attached(ctx, term.ID)
	if err != nil {
		t.Fatalf("attached before revoke failed: %v", err)
	}
	if !hasAttachedMode(attached, string(ModeCollaborator)) || !hasAttachedMode(attached, string(ModeObserver)) {
		t.Fatalf("unexpected attached modes before revoke: %#v", attached)
	}

	if err := srv.RevokeCollaborators(ctx, term.ID); err != nil {
		t.Fatalf("revoke collaborators failed: %v", err)
	}

	attached, err = srv.Attached(ctx, term.ID)
	if err != nil {
		t.Fatalf("attached after revoke failed: %v", err)
	}
	if len(attached) != 2 {
		t.Fatalf("unexpected attachment count after revoke: %#v", attached)
	}
	for _, info := range attached {
		if info.Mode != string(ModeObserver) {
			t.Fatalf("expected all attachments to be observers after revoke, got %#v", attached)
		}
	}
}

func TestHandleTransportEventsSubscriptionDeliversFilteredEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewServer()
	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	done := make(chan error, 1)
	go func() {
		done <- srv.handleTransport(ctx, serverTransport, "memory")
	}()

	client := protocol.NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	events, err := client.Events(ctx, protocol.EventsParams{
		TerminalID: "term-1",
		Types:      []protocol.EventType{protocol.EventTerminalRemoved},
	})
	if err != nil {
		t.Fatalf("events subscribe failed: %v", err)
	}

	srv.events.Publish(Event{
		Type:       EventTerminalCreated,
		TerminalID: "term-1",
		Timestamp:  time.Now().UTC(),
		Created: &TerminalCreatedData{
			Name:    "ignored",
			Command: []string{"bash"},
			Size:    Size{Cols: 80, Rows: 24},
		},
	})
	srv.events.Publish(Event{
		Type:       EventTerminalRemoved,
		TerminalID: "term-2",
		Timestamp:  time.Now().UTC(),
		Removed:    &TerminalRemovedData{Reason: "expired"},
	})
	srv.events.Publish(Event{
		Type:       EventTerminalRemoved,
		TerminalID: "term-1",
		Timestamp:  time.Now().UTC(),
		Removed:    &TerminalRemovedData{Reason: "expired"},
	})

	select {
	case evt := <-events:
		if evt.Type != protocol.EventTerminalRemoved || evt.TerminalID != "term-1" {
			t.Fatalf("unexpected event: %#v", evt)
		}
		if evt.Removed == nil || evt.Removed.Reason != "expired" {
			t.Fatalf("unexpected removed payload: %#v", evt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for filtered event")
	}

	_ = client.Close()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("handleTransport failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for transport shutdown")
	}
}

func TestHandleTransportEventsSubscriptionDeliversSessionEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewServer()
	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	done := make(chan error, 1)
	go func() {
		done <- srv.handleTransport(ctx, serverTransport, "memory")
	}()

	client := protocol.NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	events, err := client.Events(ctx, protocol.EventsParams{
		SessionID: "main",
		Types:     []protocol.EventType{protocol.EventSessionUpdated},
	})
	if err != nil {
		t.Fatalf("events subscribe failed: %v", err)
	}

	srv.events.Publish(Event{
		Type:      EventSessionUpdated,
		SessionID: "main",
		Timestamp: time.Now().UTC(),
		Session: &SessionEventData{
			Revision: 2,
			ViewID:   "view-1",
		},
	})

	select {
	case evt := <-events:
		if evt.Type != protocol.EventSessionUpdated || evt.SessionID != "main" {
			t.Fatalf("unexpected event: %#v", evt)
		}
		if evt.Session == nil || evt.Session.Revision != 2 || evt.Session.ViewID != "view-1" {
			t.Fatalf("unexpected session payload: %#v", evt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for session event")
	}

	_ = client.Close()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("handleTransport failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for transport shutdown")
	}
}

func TestServerShutdownClosesEventsAndRejectsCreate(t *testing.T) {
	srv := NewServer()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := srv.Events(ctx)

	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected events channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for events channel to close")
	}

	if _, err := srv.Create(context.Background(), CreateOptions{Command: []string{"bash"}}); !errors.Is(err, ErrServerClosed) {
		t.Fatalf("expected ErrServerClosed, got %v", err)
	}
}

func TestHandleTransportSendsProtocolErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewServer()
	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	done := make(chan error, 1)
	go func() {
		done <- srv.handleTransport(ctx, serverTransport, "memory")
	}()

	sendControlFrame := func(typ uint8, payload []byte) {
		t.Helper()
		frame, err := protocol.EncodeFrame(0, typ, payload)
		if err != nil {
			t.Fatalf("encode frame failed: %v", err)
		}
		if err := clientTransport.Send(frame); err != nil {
			t.Fatalf("send frame failed: %v", err)
		}
	}

	expectProtocolError := func(wantID uint64, wantCode int) {
		t.Helper()
		_, typ, payload := recvDecodedFrame(t, clientTransport)
		if typ != protocol.TypeError {
			t.Fatalf("expected error frame, got type %d", typ)
		}
		var msg protocol.ErrorMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("unmarshal error payload failed: %v", err)
		}
		if msg.ID != wantID || msg.Error.Code != wantCode {
			t.Fatalf("unexpected protocol error: %#v", msg)
		}
	}

	helloPayload, _ := json.Marshal(protocol.Hello{Version: protocol.Version, Client: "test"})
	sendControlFrame(protocol.TypeHello, helloPayload)
	_, typ, payload := recvDecodedFrame(t, clientTransport)
	if typ != protocol.TypeHello {
		t.Fatalf("expected hello response, got type %d", typ)
	}
	var hello protocol.Hello
	if err := json.Unmarshal(payload, &hello); err != nil {
		t.Fatalf("unmarshal hello failed: %v", err)
	}
	if hello.Server != "termx" || hello.Version != protocol.Version {
		t.Fatalf("unexpected hello response: %#v", hello)
	}

	sendControlFrame(protocol.TypeRequest, []byte("{"))
	expectProtocolError(0, 400)

	reqPayload, _ := json.Marshal(protocol.Request{
		ID:     1,
		Method: "unsupported",
		Params: json.RawMessage(`{}`),
	})
	sendControlFrame(protocol.TypeRequest, reqPayload)
	expectProtocolError(1, 400)

	reqPayload, _ = json.Marshal(protocol.Request{
		ID:     2,
		Method: "kill",
		Params: json.RawMessage(`{"terminal_id":"missing"}`),
	})
	sendControlFrame(protocol.TypeRequest, reqPayload)
	expectProtocolError(2, 404)

	_ = clientTransport.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for transport handler to exit")
	}
}

func TestServerListenAndServeOverUnixSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewServer(WithSocketPath(path))
	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe(ctx)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("socket did not appear: %v", err)
	}

	conn, err := unixtransport.Dial(path)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	client := protocol.NewClient(conn)
	defer client.Close()

	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}
	list, err := client.List(ctx)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list.Terminals) != 0 {
		t.Fatalf("expected empty terminal list, got %#v", list.Terminals)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("client close failed: %v", err)
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("listen and serve failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server shutdown")
	}
}

func TestServerListenAndServeShutdownDoesNotHangWithIdleClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.sock")
	serverCtx, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()

	srv := NewServer(WithSocketPath(path))
	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe(serverCtx)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("socket did not appear: %v", err)
	}

	conn, err := unixtransport.Dial(path)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	client := protocol.NewClient(conn)
	defer client.Close()

	clientCtx, cancelClient := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelClient()
	if err := client.Hello(clientCtx, protocol.Hello{Version: protocol.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	cancelServer()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("listen and serve failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server shutdown with idle client")
	}
}

func TestHandleRequestDetachReleasesChannelOnce(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(WithDefaultKeepAfterExit(2 * time.Second))

	info, err := srv.Create(ctx, CreateOptions{
		ID:      "attach01",
		Command: []string{"bash", "--noprofile", "--norc"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }

	first := mustAttachChannel(t, srv, ctx, "memory", allocator, attachments, &attachmentsMu, info.ID, string(ModeCollaborator), sendFrame)

	attached, err := srv.Attached(ctx, info.ID)
	if err != nil {
		t.Fatalf("attached failed: %v", err)
	}
	if len(attached) != 1 || attached[0].Mode != string(ModeCollaborator) {
		t.Fatalf("unexpected attachments: %#v", attached)
	}

	if _, _, err := srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     2,
		Method: "detach",
		Params: json.RawMessage(`{"terminal_id":"attach01"}`),
	}, sendFrame); err != nil {
		t.Fatalf("detach failed: %v", err)
	}

	waitFor(t, 3*time.Second, func() bool {
		attached, err := srv.Attached(ctx, info.ID)
		return err == nil && len(attached) == 0
	})

	second := mustAttachChannel(t, srv, ctx, "memory", allocator, attachments, &attachmentsMu, info.ID, string(ModeCollaborator), sendFrame)
	if second != first {
		t.Fatalf("expected freed channel to be reused, got %d then %d", first, second)
	}

	third := mustAttachChannel(t, srv, ctx, "memory", allocator, attachments, &attachmentsMu, info.ID, string(ModeCollaborator), sendFrame)
	if third == second {
		t.Fatalf("expected unique in-use channel, got duplicate %d", third)
	}
}

func TestObserverAttachCannotWriteOrResize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewServer(WithDefaultScrollback(128))
	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	done := make(chan error, 1)
	go func() {
		done <- srv.handleTransport(ctx, serverTransport, "memory")
	}()

	client := protocol.NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	created, err := client.Create(ctx, protocol.CreateParams{
		ID:      "observer01",
		Command: []string{"bash", "--noprofile", "--norc"},
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	attach, err := client.Attach(ctx, created.TerminalID, string(ModeObserver))
	if err != nil {
		t.Fatalf("attach observer failed: %v", err)
	}

	if err := client.Input(ctx, attach.Channel, []byte("echo should-not-run\n")); err != nil {
		t.Fatalf("observer input send failed: %v", err)
	}
	if err := client.Resize(ctx, attach.Channel, 120, 50); err != nil {
		t.Fatalf("observer resize send failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	snap, err := client.Snapshot(ctx, created.TerminalID, 0, 50)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if protocolSnapshotContains(snap, "should-not-run") {
		t.Fatalf("observer input unexpectedly reached terminal: %#v", snap)
	}
	if snap.Size.Cols != 80 || snap.Size.Rows != 24 {
		t.Fatalf("observer resize unexpectedly changed size: %#v", snap.Size)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("client close failed: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for handler exit")
	}
}

func TestCollaboratorResizeLockedTerminalIsIgnored(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewServer(WithDefaultScrollback(128))
	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	done := make(chan error, 1)
	go func() {
		done <- srv.handleTransport(ctx, serverTransport, "memory")
	}()

	client := protocol.NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	created, err := client.Create(ctx, protocol.CreateParams{
		ID:      "lock-resize-01",
		Command: []string{"bash", "--noprofile", "--norc"},
		Tags:    map[string]string{"termx.size_lock": "lock"},
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	attach, err := client.Attach(ctx, created.TerminalID, string(ModeCollaborator))
	if err != nil {
		t.Fatalf("attach collaborator failed: %v", err)
	}

	if err := client.Resize(ctx, attach.Channel, 120, 50); err != nil {
		t.Fatalf("resize send failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	snap, err := client.Snapshot(ctx, created.TerminalID, 0, 20)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if snap.Size.Cols != 80 || snap.Size.Rows != 24 {
		t.Fatalf("expected locked size to stay 80x24, got %#v", snap.Size)
	}

	list, err := client.List(ctx)
	if err != nil {
		t.Fatalf("expected transport to stay alive after locked resize, got %v", err)
	}
	if len(list.Terminals) != 1 || list.Terminals[0].ID != created.TerminalID {
		t.Fatalf("unexpected list result after locked resize: %#v", list.Terminals)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for handler exit")
	}
}

func TestHandleRequestKillDeniedForObserverAttachment(t *testing.T) {
	ctx := context.Background()
	srv := NewServer()

	info, err := srv.Create(ctx, CreateOptions{
		ID:      "observer-kill-01",
		Command: []string{"bash", "--noprofile", "--norc"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }

	mustAttachChannel(t, srv, ctx, "memory", allocator, attachments, &attachmentsMu, info.ID, string(ModeObserver), sendFrame)

	_, code, err := srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     1,
		Method: "kill",
		Params: mustJSON(t, protocol.GetParams{TerminalID: info.ID}),
	}, sendFrame)
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied, got %v", err)
	}
	if code != 403 {
		t.Fatalf("expected 403 for observer kill, got %d", code)
	}
}

func TestHandleRequestKillAllowedForCollaboratorAttachment(t *testing.T) {
	ctx := context.Background()
	srv := NewServer()

	info, err := srv.Create(ctx, CreateOptions{
		ID:      "collab-kill-01",
		Command: []string{"bash", "--noprofile", "--norc"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }

	mustAttachChannel(t, srv, ctx, "memory", allocator, attachments, &attachmentsMu, info.ID, string(ModeCollaborator), sendFrame)

	_, code, err := srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     1,
		Method: "kill",
		Params: mustJSON(t, protocol.GetParams{TerminalID: info.ID}),
	}, sendFrame)
	if err != nil {
		t.Fatalf("expected collaborator kill to succeed, got %v", err)
	}
	if code != 0 {
		t.Fatalf("expected success code 0, got %d", code)
	}
}

func TestRevokeCollaboratorsTurnsCollaboratorChannelReadOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewServer(WithDefaultScrollback(128))
	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	done := make(chan error, 1)
	go func() {
		done <- srv.handleTransport(ctx, serverTransport, "memory")
	}()

	client := protocol.NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	created, err := client.Create(ctx, protocol.CreateParams{
		ID:      "revokech1",
		Command: []string{"bash", "--noprofile", "--norc"},
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	attach, err := client.Attach(ctx, created.TerminalID, string(ModeCollaborator))
	if err != nil {
		t.Fatalf("attach collaborator failed: %v", err)
	}

	if err := client.Input(ctx, attach.Channel, []byte("echo before-revoke\n")); err != nil {
		t.Fatalf("pre-revoke input failed: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool {
		snap, err := client.Snapshot(ctx, created.TerminalID, 0, 50)
		return err == nil && protocolSnapshotContains(snap, "before-revoke")
	})

	if err := srv.RevokeCollaborators(ctx, created.TerminalID); err != nil {
		t.Fatalf("revoke collaborators failed: %v", err)
	}

	attached, err := srv.Attached(ctx, created.TerminalID)
	if err != nil {
		t.Fatalf("attached failed: %v", err)
	}
	if len(attached) != 1 || attached[0].Mode != string(ModeObserver) {
		t.Fatalf("expected collaborator to be downgraded, got %#v", attached)
	}

	if err := client.Input(ctx, attach.Channel, []byte("echo after-revoke\n")); err != nil {
		t.Fatalf("post-revoke input send failed: %v", err)
	}
	if err := client.Resize(ctx, attach.Channel, 120, 50); err != nil {
		t.Fatalf("post-revoke resize send failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	snap, err := client.Snapshot(ctx, created.TerminalID, 0, 50)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if protocolSnapshotContains(snap, "after-revoke") {
		t.Fatalf("post-revoke input unexpectedly reached terminal: %#v", snap)
	}
	if snap.Size.Cols != 80 || snap.Size.Rows != 24 {
		t.Fatalf("post-revoke resize unexpectedly changed size: %#v", snap.Size)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("client close failed: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for handler exit")
	}
}

func TestServerKillExitedRemovesImmediately(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(WithDefaultKeepAfterExit(10 * time.Second))

	info, err := srv.Create(ctx, CreateOptions{
		ID:      "exit01",
		Command: []string{"bash", "-lc", "exit 0"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool {
		got, err := srv.Get(ctx, info.ID)
		return err == nil && got.State == StateExited
	})

	if err := srv.Kill(ctx, info.ID); err != nil {
		t.Fatalf("kill on exited terminal failed: %v", err)
	}
	if _, err := srv.Get(ctx, info.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected terminal removal after kill, got %v", err)
	}
}

func TestServerRestartExitedTerminalReusesID(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(WithDefaultKeepAfterExit(10 * time.Second))

	flagPath := filepath.Join(t.TempDir(), "restart-flag")
	command := fmt.Sprintf("if [ -f %q ]; then printf 'restart_pass_2\\n'; cat; else touch %q; printf 'restart_pass_1\\n'; exit 0; fi", flagPath, flagPath)
	info, err := srv.Create(ctx, CreateOptions{
		ID:      "restart01",
		Command: []string{"bash", "-lc", command},
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool {
		got, err := srv.Get(ctx, info.ID)
		return err == nil && got.State == StateExited
	})
	waitForSnapshotContains(t, srv, info.ID, "restart_pass_1")

	if err := srv.Restart(ctx, info.ID); err != nil {
		t.Fatalf("restart failed: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool {
		got, err := srv.Get(ctx, info.ID)
		return err == nil && got.State == StateRunning
	})
	waitForSnapshotContains(t, srv, info.ID, "restart_pass_2")

	if err := srv.WriteInput(ctx, info.ID, []byte("restart_echo\n")); err != nil {
		t.Fatalf("write input after restart failed: %v", err)
	}
	waitForSnapshotContains(t, srv, info.ID, "restart_echo")
}

func TestServerRemoveCleansAttachmentsAndClosesTerminal(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(WithDefaultScrollback(128))

	info, err := srv.Create(ctx, CreateOptions{
		ID:      "remove01",
		Command: []string{"bash", "--noprofile", "--norc"},
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}
	term, err := srv.getTerminal(info.ID)
	if err != nil {
		t.Fatalf("get terminal failed: %v", err)
	}

	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }

	channel := mustAttachChannel(t, srv, ctx, "memory", allocator, attachments, &attachmentsMu, info.ID, string(ModeCollaborator), sendFrame)
	if channel == 0 {
		t.Fatal("expected attachment channel")
	}

	_, code, err := srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     1,
		Method: "remove",
		Params: mustJSON(t, protocol.GetParams{TerminalID: info.ID}),
	}, sendFrame)
	if err != nil {
		t.Fatalf("expected remove to succeed, got %v", err)
	}
	if code != 0 {
		t.Fatalf("expected success code 0, got %d", code)
	}
	if _, err := srv.Get(ctx, info.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected removed terminal to leave registry, got %v", err)
	}
	attachmentsMu.RLock()
	_, ok := attachments[channel]
	attachmentsMu.RUnlock()
	if ok {
		t.Fatal("expected remove to cleanup transport attachment")
	}
	select {
	case <-term.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("expected remove to close terminal")
	}
}

func TestServerRemovePublishesSingleRemovedEventWithoutExitedPollution(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(WithDefaultScrollback(128), WithDefaultKeepAfterExit(0))

	info, err := srv.Create(ctx, CreateOptions{
		ID:      "remove-seq-01",
		Command: []string{"bash", "--noprofile", "--norc"},
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}
	term, err := srv.getTerminal(info.ID)
	if err != nil {
		t.Fatalf("get terminal failed: %v", err)
	}

	eventsCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := srv.Events(eventsCtx, WithTerminalFilter(info.ID))

	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }
	mustAttachChannel(t, srv, ctx, "memory", allocator, attachments, &attachmentsMu, info.ID, string(ModeCollaborator), sendFrame)

	_, code, err := srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     1,
		Method: "remove",
		Params: mustJSON(t, protocol.GetParams{TerminalID: info.ID}),
	}, sendFrame)
	if err != nil {
		t.Fatalf("expected remove to succeed, got %v", err)
	}
	if code != 0 {
		t.Fatalf("expected success code 0, got %d", code)
	}

	select {
	case <-term.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("expected remove to close terminal")
	}

	collected := make([]Event, 0, 4)
	timer := time.NewTimer(300 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case evt := <-events:
			collected = append(collected, evt)
		case <-timer.C:
			goto VERIFY
		}
	}

VERIFY:
	removedCount := 0
	expiredRemovedCount := 0
	exitedCount := 0
	for _, evt := range collected {
		switch evt.Type {
		case EventTerminalRemoved:
			removedCount++
			if evt.Removed != nil && evt.Removed.Reason == "expired" {
				expiredRemovedCount++
			}
		case EventTerminalStateChanged:
			exitedCount++
		}
	}
	if removedCount != 1 {
		t.Fatalf("expected exactly one removed event, got %#v", collected)
	}
	if expiredRemovedCount != 0 {
		t.Fatalf("expected remove to avoid expired duplicate event, got %#v", collected)
	}
	if exitedCount != 0 {
		t.Fatalf("expected remove to avoid exited state pollution, got %#v", collected)
	}
	if collected[0].Type != EventTerminalRemoved || collected[0].Removed == nil || collected[0].Removed.Reason != "removed" {
		t.Fatalf("expected first event to be removed(reason=removed), got %#v", collected)
	}
}

func TestServerOptionsAndHelpers(t *testing.T) {
	logger := discardWriter{}
	slogLogger := slog.New(slog.NewTextHandler(logger, nil))
	srv := NewServer(
		WithDefaultSize(132, 43),
		WithLogger(slogLogger),
	)

	if srv.cfg.defaultSize != (Size{Cols: 132, Rows: 43}) {
		t.Fatalf("unexpected default size: %#v", srv.cfg.defaultSize)
	}
	if srv.cfg.logger != slogLogger {
		t.Fatal("expected custom logger to be stored")
	}

	if !matchTags(map[string]string{"group": "dev", "role": "shell"}, map[string]string{"group": "dev"}) {
		t.Fatal("expected tag match")
	}
	if matchTags(map[string]string{"group": "dev"}, map[string]string{"group": "ops"}) {
		t.Fatal("expected tag mismatch")
	}
	if got := protocolErrorCode(ErrNotFound); got != 404 {
		t.Fatalf("unexpected not-found code: %d", got)
	}
	if got := protocolErrorCode(ErrDuplicateID); got != 409 {
		t.Fatalf("unexpected duplicate-id code: %d", got)
	}
	if got := protocolErrorCode(ErrPermissionDenied); got != 403 {
		t.Fatalf("unexpected permission-denied code: %d", got)
	}
	if got := protocolErrorCode(ErrInvalidCommand); got != 400 {
		t.Fatalf("unexpected invalid-command code: %d", got)
	}
	if got := protocolErrorCode(ErrTerminalExited); got != 400 {
		t.Fatalf("unexpected terminal-exited code: %d", got)
	}
	if got := protocolErrorCode(errors.New("boom")); got != 500 {
		t.Fatalf("unexpected default protocol code: %d", got)
	}
}

func TestServerResizeAndWriteInputValidateState(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(WithDefaultKeepAfterExit(10 * time.Second))

	info, err := srv.Create(ctx, CreateOptions{
		ID:      "resize01",
		Command: []string{"bash", "--noprofile", "--norc"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	if err := srv.Resize(ctx, info.ID, 0, 24); err == nil {
		t.Fatal("expected invalid resize error")
	}

	if err := srv.Kill(ctx, info.ID); err != nil {
		t.Fatalf("kill failed: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool {
		got, err := srv.Get(ctx, info.ID)
		return err == nil && got.State == StateExited
	})

	if err := srv.WriteInput(ctx, info.ID, []byte("echo nope\n")); !errors.Is(err, ErrTerminalExited) {
		t.Fatalf("expected ErrTerminalExited for write, got %v", err)
	}
	if err := srv.Resize(ctx, info.ID, 120, 40); !errors.Is(err, ErrTerminalExited) {
		t.Fatalf("expected ErrTerminalExited for resize, got %v", err)
	}
}

func TestServerSubscribeMultipleClientsReceiveSameOutput(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(WithDefaultKeepAfterExit(2 * time.Second))

	info, err := srv.Create(ctx, CreateOptions{
		ID:      "multiout1",
		Command: []string{"bash", "--noprofile", "--norc"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	subCtx1, cancel1 := context.WithCancel(ctx)
	defer cancel1()
	subCtx2, cancel2 := context.WithCancel(ctx)
	defer cancel2()

	stream1, err := srv.Subscribe(subCtx1, info.ID)
	if err != nil {
		t.Fatalf("subscribe 1 failed: %v", err)
	}
	stream2, err := srv.Subscribe(subCtx2, info.ID)
	if err != nil {
		t.Fatalf("subscribe 2 failed: %v", err)
	}

	if err := srv.SendKeys(ctx, info.ID, "echo fanout-check", "Enter"); err != nil {
		t.Fatalf("send keys failed: %v", err)
	}

	expectStreamContains(t, stream1, "fanout-check")
	expectStreamContains(t, stream2, "fanout-check")
}

func TestDefaultSocketPathPrefersRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
	got := defaultSocketPath()
	if want := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "termx.sock"); got != want {
		t.Fatalf("unexpected socket path: got %q want %q", got, want)
	}
}

func TestServerSendKeysSpecialSequences(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(WithDefaultKeepAfterExit(10 * time.Second))

	info, err := srv.Create(ctx, CreateOptions{
		ID:      "keyspec1",
		Command: []string{"cat", "-vet"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := srv.Subscribe(streamCtx, info.ID)
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	if err := srv.SendKeys(ctx, info.ID, "A", "Tab", "Escape", "B", "Enter", "Ctrl-D"); err != nil {
		t.Fatalf("send keys failed: %v", err)
	}

	expectStreamContains(t, stream, "A^I^[B$")
}

func TestServerSendKeysCtrlCStopsForegroundProcess(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(WithDefaultKeepAfterExit(10 * time.Second))

	info, err := srv.Create(ctx, CreateOptions{
		ID:      "keyctrlc1",
		Command: []string{"cat"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	if err := srv.SendKeys(ctx, info.ID, "Ctrl-C"); err != nil {
		t.Fatalf("send ctrl-c failed: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool {
		got, err := srv.Get(ctx, info.ID)
		return err == nil && got.State == StateExited
	})
}

func TestHandleRequestGetResizeSetTagsMetadataAndSnapshot(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(WithDefaultKeepAfterExit(10 * time.Second))

	info, err := srv.Create(ctx, CreateOptions{
		ID:      "reqpath01",
		Command: []string{"bash", "--noprofile", "--norc"},
		Tags:    map[string]string{"group": "dev"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }

	result, _, err := srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     1,
		Method: "get",
		Params: mustJSON(t, protocol.GetParams{TerminalID: info.ID}),
	}, sendFrame)
	if err != nil {
		t.Fatalf("get request failed: %v", err)
	}
	var got protocol.TerminalInfo
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal get result failed: %v", err)
	}
	if got.ID != info.ID || got.Tags["group"] != "dev" {
		t.Fatalf("unexpected get result: %#v", got)
	}

	if _, _, err := srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     2,
		Method: "resize",
		Params: mustJSON(t, protocol.ResizeParams{TerminalID: info.ID, Cols: 100, Rows: 40}),
	}, sendFrame); err != nil {
		t.Fatalf("resize request failed: %v", err)
	}

	if _, _, err := srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     3,
		Method: "set_tags",
		Params: mustJSON(t, protocol.SetTagsParams{TerminalID: info.ID, Tags: map[string]string{"status": "idle"}}),
	}, sendFrame); err != nil {
		t.Fatalf("set_tags request failed: %v", err)
	}

	if _, _, err := srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     4,
		Method: "set_metadata",
		Params: mustJSON(t, protocol.SetMetadataParams{TerminalID: info.ID, Name: "dev-shell", Tags: map[string]string{"status": "idle", "team": "infra"}}),
	}, sendFrame); err != nil {
		t.Fatalf("set_metadata request failed: %v", err)
	}

	result, _, err = srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     30,
		Method: "get",
		Params: mustJSON(t, protocol.GetParams{TerminalID: info.ID}),
	}, sendFrame)
	if err != nil {
		t.Fatalf("get after update failed: %v", err)
	}
	got = protocol.TerminalInfo{}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal updated get result failed: %v", err)
	}
	if got.Size != (protocol.Size{Cols: 100, Rows: 40}) || got.Tags["status"] != "idle" || got.Tags["team"] != "infra" || got.Tags["group"] != "" || got.Name != "dev-shell" {
		t.Fatalf("stale get result after updates: %#v", got)
	}

	if err := srv.WriteInput(ctx, info.ID, []byte("echo request-path\n")); err != nil {
		t.Fatalf("write input failed: %v", err)
	}
	waitForSnapshotContains(t, srv, info.ID, "request-path")

	result, _, err = srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     4,
		Method: "snapshot",
		Params: mustJSON(t, protocol.SnapshotParams{TerminalID: info.ID, ScrollbackLimit: 50}),
	}, sendFrame)
	if err != nil {
		t.Fatalf("snapshot request failed: %v", err)
	}
	var snap protocol.Snapshot
	if err := json.Unmarshal(result, &snap); err != nil {
		t.Fatalf("unmarshal snapshot result failed: %v", err)
	}
	if snap.TerminalID != info.ID || !protocolSnapshotContains(&snap, "request-path") {
		t.Fatalf("unexpected snapshot result: %#v", snap)
	}

	gotInfo, err := srv.Get(ctx, info.ID)
	if err != nil {
		t.Fatalf("get after requests failed: %v", err)
	}
	if gotInfo.Size != (Size{Cols: 100, Rows: 40}) || gotInfo.Tags["status"] != "idle" {
		t.Fatalf("unexpected terminal state after requests: %#v", gotInfo)
	}
}

func TestHandleRequestListCacheInvalidatesOnSetTags(t *testing.T) {
	ctx := context.Background()
	srv := NewServer()
	srv.terminals["cached01"] = &Terminal{
		id:      "cached01",
		name:    "cached01",
		command: []string{"bash"},
		tags:    map[string]string{"group": "dev"},
		size:    Size{Cols: 80, Rows: 24},
		state:   StateRunning,
	}

	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }

	result, _, err := srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     1,
		Method: "list",
		Params: mustJSON(t, struct{}{}),
	}, sendFrame)
	if err != nil {
		t.Fatalf("initial list request failed: %v", err)
	}

	var before protocol.ListResult
	if err := json.Unmarshal(result, &before); err != nil {
		t.Fatalf("unmarshal initial list failed: %v", err)
	}
	if len(before.Terminals) != 1 || before.Terminals[0].Tags["group"] != "dev" {
		t.Fatalf("unexpected initial list result: %#v", before)
	}

	if err := srv.SetTags(ctx, "cached01", map[string]string{"role": "shell"}); err != nil {
		t.Fatalf("set tags failed: %v", err)
	}

	result, _, err = srv.handleRequest(ctx, "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
		ID:     2,
		Method: "list",
		Params: mustJSON(t, struct{}{}),
	}, sendFrame)
	if err != nil {
		t.Fatalf("updated list request failed: %v", err)
	}

	var after protocol.ListResult
	if err := json.Unmarshal(result, &after); err != nil {
		t.Fatalf("unmarshal updated list failed: %v", err)
	}
	if len(after.Terminals) != 1 {
		t.Fatalf("unexpected updated list size: %#v", after)
	}
	if after.Terminals[0].Tags["group"] != "dev" || after.Terminals[0].Tags["role"] != "shell" {
		t.Fatalf("stale cached list result: %#v", after)
	}
}

func TestServerShutdownClosesActiveTerminal(t *testing.T) {
	ctx := context.Background()
	srv := NewServer()

	info, err := srv.Create(ctx, CreateOptions{
		ID:      "shutdown1",
		Command: []string{"bash", "--noprofile", "--norc"},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	term, err := srv.getTerminal(info.ID)
	if err != nil {
		t.Fatalf("get terminal failed: %v", err)
	}

	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	select {
	case <-term.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for terminal shutdown")
	}
}

func TestHandleRequestRejectsMalformedParams(t *testing.T) {
	srv := NewServer()
	allocator := protocol.NewChannelAllocator()
	attachments := make(map[uint16]*sessionAttachment)
	var attachmentsMu sync.RWMutex
	sendFrame := func(uint16, uint8, []byte) error { return nil }

	methods := []string{"create", "get", "kill", "resize", "set_tags", "set_metadata", "snapshot", "attach", "detach"}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			_, code, err := srv.handleRequest(context.Background(), "memory", nil, allocator, attachments, &attachmentsMu, protocol.Request{
				ID:     1,
				Method: method,
				Params: json.RawMessage(`{`),
			}, sendFrame)
			if err == nil || code != 400 {
				t.Fatalf("expected 400 for method %q, got code=%d err=%v", method, code, err)
			}
		})
	}
}

func TestHandleTransportMalformedHelloReturnsProtocolError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewServer()
	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	done := make(chan error, 1)
	go func() {
		done <- srv.handleTransport(ctx, serverTransport, "memory")
	}()

	frame, err := protocol.EncodeFrame(0, protocol.TypeHello, []byte("{"))
	if err != nil {
		t.Fatalf("encode frame failed: %v", err)
	}
	if err := clientTransport.Send(frame); err != nil {
		t.Fatalf("send frame failed: %v", err)
	}

	_, typ, payload := recvDecodedFrame(t, clientTransport)
	if typ != protocol.TypeError {
		t.Fatalf("expected protocol error frame, got %d", typ)
	}
	var msg protocol.ErrorMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("unmarshal protocol error failed: %v", err)
	}
	if msg.Error.Code != 400 {
		t.Fatalf("unexpected protocol error: %#v", msg)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for transport handler exit")
	}
}

func TestHandleTransportBadResizePayloadIgnoredThenValidResizeWorks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewServer(WithDefaultScrollback(128))
	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	done := make(chan error, 1)
	go func() {
		done <- srv.handleTransport(ctx, serverTransport, "memory")
	}()

	client := protocol.NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	created, err := client.Create(ctx, protocol.CreateParams{
		ID:      "badresize1",
		Command: []string{"bash", "--noprofile", "--norc"},
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	attach, err := client.Attach(ctx, created.TerminalID, string(ModeCollaborator))
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}

	raw, err := protocol.EncodeFrame(attach.Channel, protocol.TypeResize, []byte{1, 2, 3})
	if err != nil {
		t.Fatalf("encode bad resize frame failed: %v", err)
	}
	if err := clientTransport.Send(raw); err != nil {
		t.Fatalf("send bad resize frame failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	snap, err := client.Snapshot(ctx, created.TerminalID, 0, 10)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if snap.Size.Cols != 80 || snap.Size.Rows != 24 {
		t.Fatalf("bad resize payload unexpectedly changed size: %#v", snap.Size)
	}

	if err := client.Resize(ctx, attach.Channel, 120, 50); err != nil {
		t.Fatalf("valid resize failed: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool {
		snap, err := client.Snapshot(ctx, created.TerminalID, 0, 10)
		return err == nil && snap.Size.Cols == 120 && snap.Size.Rows == 50
	})

	if err := client.Close(); err != nil {
		t.Fatalf("client close failed: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for transport handler exit")
	}
}

func TestHandleTransportSendsClosedFrameOnTerminalExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewServer(WithDefaultScrollback(128))
	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	done := make(chan error, 1)
	go func() {
		done <- srv.handleTransport(ctx, serverTransport, "memory")
	}()

	client := protocol.NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	created, err := client.Create(ctx, protocol.CreateParams{
		ID:      "closedfrm",
		Command: []string{"bash", "--noprofile", "--norc"},
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	attach, err := client.Attach(ctx, created.TerminalID, string(ModeCollaborator))
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}

	stream, stop := client.Stream(attach.Channel)
	defer stop()

	if err := client.Input(ctx, attach.Channel, []byte("exit\n")); err != nil {
		t.Fatalf("input failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msg := <-stream
		if msg.Type == protocol.TypeClosed {
			code, err := protocol.DecodeClosedPayload(msg.Payload)
			if err != nil {
				t.Fatalf("decode closed payload failed: %v", err)
			}
			if code != 0 && code != -1 {
				t.Fatalf("unexpected closed code: %d", code)
			}
			return
		}
	}
	t.Fatal("timed out waiting for closed frame")
}

func TestUtilityHelpers(t *testing.T) {
	if n, err := (discardWriter{}).Write([]byte("abc")); err != nil || n != 3 {
		t.Fatalf("unexpected discardWriter result: n=%d err=%v", n, err)
	}

	rows := [][]Cell{{{Content: "a"}}, {{Content: "b"}}}
	cloned := cloneRows(rows)
	cloned[0][0].Content = "z"
	if rows[0][0].Content != "a" {
		t.Fatalf("cloneRows did not deep copy: %#v", rows)
	}

	term := &Terminal{id: "helper01"}
	if term.ID() != "helper01" {
		t.Fatalf("unexpected terminal ID: %s", term.ID())
	}
}

func recvDecodedFrame(t *testing.T, tr *memory.Transport) (uint16, uint8, []byte) {
	t.Helper()
	frame, err := tr.Recv()
	if err != nil {
		t.Fatalf("recv failed: %v", err)
	}
	channel, typ, payload, err := protocol.DecodeFrame(frame)
	if err != nil {
		t.Fatalf("decode frame failed: %v", err)
	}
	return channel, typ, payload
}

func mustAttachChannel(
	t *testing.T,
	srv *Server,
	ctx context.Context,
	remote string,
	allocator *protocol.ChannelAllocator,
	attachments map[uint16]*sessionAttachment,
	attachmentsMu *sync.RWMutex,
	terminalID string,
	mode string,
	sendFrame func(uint16, uint8, []byte) error,
) uint16 {
	t.Helper()
	result, _, err := srv.handleRequest(ctx, remote, nil, allocator, attachments, attachmentsMu, protocol.Request{
		ID:     1,
		Method: "attach",
		Params: mustJSON(t, protocol.AttachParams{TerminalID: terminalID, Mode: mode}),
	}, sendFrame)
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	var attach protocol.AttachResult
	if err := json.Unmarshal(result, &attach); err != nil {
		t.Fatalf("unmarshal attach result failed: %v", err)
	}
	return attach.Channel
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	return data
}

func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func expectStreamContains(t *testing.T, ch <-chan StreamMessage, needle string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msg := <-ch
		if streamMessageContainsText(msg, 80, 24, needle) {
			return
		}
	}
	t.Fatalf("timed out waiting for stream output %q", needle)
}

func waitForSnapshotContains(t *testing.T, srv *Server, terminalID, needle string) {
	t.Helper()
	waitFor(t, 5*time.Second, func() bool {
		snap, err := srv.Snapshot(context.Background(), terminalID, SnapshotOptions{ScrollbackLimit: 50})
		return err == nil && snapshotContains(snap, needle)
	})
}

func hasAttachedMode(attached []AttachInfo, mode string) bool {
	for _, info := range attached {
		if info.Mode == mode {
			return true
		}
	}
	return false
}
